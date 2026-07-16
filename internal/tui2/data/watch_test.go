package data

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// tempWorkspace builds a .orun-shaped tree with every watchable dir.
func tempWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{
		"refs", "plans",
		filepath.Join("objectmodel", "refs", "catalogs"),
		filepath.Join("agents", "live"),
	} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func expectDelta(t *testing.T, ch <-chan Delta, topic Topic, within time.Duration) Delta {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case d, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed while waiting for %s", topic)
			}
			if d.Topic == topic {
				return d
			}
			// Other topics may fire (e.g. directory-creation noise); keep
			// draining until ours arrives.
		case <-deadline:
			t.Fatalf("no %s delta within %v", topic, within)
		}
	}
}

// TestWatcherClassifiesEvents: a ref write is a runs delta, a live-registry
// write is a sessions delta, a catalog ref write is a catalog delta.
func TestWatcherClassifiesEvents(t *testing.T) {
	root := tempWorkspace(t)
	w := newWatcher(root)
	defer w.close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := w.subscribe(ctx, nil)
	time.Sleep(50 * time.Millisecond) // let the fs watcher arm

	if err := os.WriteFile(filepath.Join(root, "refs", "latest-execution.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := expectDelta(t, ch, TopicRuns, 2*time.Second)
	if d.Degraded {
		t.Fatal("watcher path must not report degraded")
	}

	if err := os.WriteFile(filepath.Join(root, "agents", "live", "as_1.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	expectDelta(t, ch, TopicSessions, 2*time.Second)

	if err := os.WriteFile(filepath.Join(root, "objectmodel", "refs", "catalogs", "current"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	expectDelta(t, ch, TopicCatalog, 2*time.Second)
}

// TestWatcherDebouncesStorms: a rename-heavy write burst coalesces into a
// handful of deltas, not one per file.
func TestWatcherDebouncesStorms(t *testing.T) {
	root := tempWorkspace(t)
	w := newWatcher(root)
	defer w.close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := w.subscribe(ctx, []Topic{TopicRuns})
	time.Sleep(50 * time.Millisecond)

	for i := 0; i < 40; i++ {
		_ = os.WriteFile(filepath.Join(root, "refs", "r.json"), []byte{byte(i)}, 0o644)
	}
	// Drain for a settle window and count.
	count := 0
	timeout := time.After(1500 * time.Millisecond)
drain:
	for {
		select {
		case <-ch:
			count++
		case <-timeout:
			break drain
		}
	}
	if count == 0 {
		t.Fatal("storm produced no delta")
	}
	if count > 6 {
		t.Fatalf("storm produced %d deltas; debounce is not working", count)
	}
}

func TestSubscribeTopicFilter(t *testing.T) {
	root := tempWorkspace(t)
	w := newWatcher(root)
	defer w.close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := w.subscribe(ctx, []Topic{TopicSessions})
	time.Sleep(50 * time.Millisecond)

	_ = os.WriteFile(filepath.Join(root, "refs", "r.json"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "agents", "live", "s.json"), []byte("x"), 0o644)

	d := expectDelta(t, ch, TopicSessions, 2*time.Second)
	if d.Topic != TopicSessions {
		t.Fatalf("got %s", d.Topic)
	}
	select {
	case d := <-ch:
		if d.Topic != TopicSessions {
			t.Fatalf("filtered subscriber received %s", d.Topic)
		}
	case <-time.After(300 * time.Millisecond):
	}
}

func TestSubscribeCancelClosesChannel(t *testing.T) {
	w := newWatcher(t.TempDir())
	defer w.close()
	ctx, cancel := context.WithCancel(context.Background())
	ch := w.subscribe(ctx, nil)
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected close, got delta")
		}
	case <-time.After(time.Second):
		t.Fatal("channel did not close on cancel")
	}
	// Emitting after close must not panic (the send/close race).
	w.emit(TopicRuns, false)
}

// TestEmitNeverBlocks: an unread subscriber cannot stall the emitter.
func TestEmitNeverBlocks(t *testing.T) {
	w := newWatcher(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = w.subscribe(ctx, nil)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 500; i++ {
			w.emit(TopicRuns, false)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("emit blocked on a slow subscriber")
	}
}

// TestFallbackTicksDegraded pins the degraded path: every topic ticks and
// deltas carry the flag the status line surfaces (risk R4).
func TestFallbackTicksDegraded(t *testing.T) {
	old := fallbackEvery
	fallbackEvery = 30 * time.Millisecond
	defer func() { fallbackEvery = old }()

	w := newWatcher(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := w.subscribe(ctx, nil)
	go w.runFallback(ctx)

	seen := map[Topic]bool{}
	deadline := time.After(2 * time.Second)
	for len(seen) < len(AllTopics) {
		select {
		case d := <-ch:
			if !d.Degraded {
				t.Fatal("fallback delta must be marked degraded")
			}
			seen[d.Topic] = true
		case <-deadline:
			t.Fatalf("fallback covered %d/%d topics", len(seen), len(AllTopics))
		}
	}
}

func TestClassifyLongestMatch(t *testing.T) {
	dirs := map[string]Topic{
		"/w/refs":             TopicRuns,
		"/w/objectmodel/refs": TopicCatalog,
	}
	if got := classify("/w/objectmodel/refs/catalogs/current", dirs); got != TopicCatalog {
		t.Fatalf("got %s", got)
	}
	if got := classify("/w/refs/latest.json", dirs); got != TopicRuns {
		t.Fatalf("got %s", got)
	}
	if got := classify("/elsewhere/x", dirs); got != "" {
		t.Fatalf("got %s", got)
	}
}

// TestMockSeedEmits: seeding fixtures notifies subscribers like a real
// workspace change.
func TestMockSeedEmits(t *testing.T) {
	m := SampleMock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, _ := m.Subscribe(ctx, TopicSessions)

	m.Seed(func(ms *MockSource) { ms.LiveSessions = ms.LiveSessions[:1] }, TopicSessions)
	expectDelta(t, ch, TopicSessions, time.Second)

	got, err := m.Sessions(ctx)
	if err != nil || len(got) != 1 {
		t.Fatalf("sessions = %v, %v", got, err)
	}
}

// TestLocalSourceEmptyWorkspace: a missing object model degrades to errors
// on model reads while sessions read empty — construction never fails.
func TestLocalSourceEmptyWorkspace(t *testing.T) {
	s := NewLocal(LocalConfig{OrunRoot: t.TempDir(), WorkspaceRoot: t.TempDir()})
	defer s.Close()
	ctx := context.Background()
	if _, err := s.Catalog(ctx); err == nil {
		t.Fatal("catalog on empty workspace should error (caller keeps prior render)")
	}
	if _, err := s.Runs(ctx); err == nil {
		t.Fatal("runs on empty workspace should error")
	}
	if entries, err := s.Sessions(ctx); err != nil || len(entries) != 0 {
		t.Fatalf("sessions = %v, %v; want empty, nil", entries, err)
	}
	if _, err := s.Subscribe(ctx); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
}
