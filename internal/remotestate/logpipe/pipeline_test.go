package logpipe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sourceplane/orun/internal/statebackend"
	"time"
)

// recordingUploader captures delivered (jobID, content) in order, and can be
// toggled to fail (simulating a backend that's unreachable mid-run).
type recordingUploader struct {
	mu    sync.Mutex
	fail  bool
	got   []string // "job:content"
	steps []*statebackend.LogStep
	calls int
}

func (u *recordingUploader) up(_ context.Context, _ string, jobID, content string, step *statebackend.LogStep) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls++
	if u.fail {
		return errors.New("backend unreachable")
	}
	u.got = append(u.got, jobID+":"+content)
	u.steps = append(u.steps, step)
	return nil
}

func (u *recordingUploader) setFail(v bool) {
	u.mu.Lock()
	u.fail = v
	u.mu.Unlock()
}

func (u *recordingUploader) delivered() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.got...)
}

func TestPipeline_HappyPath_DeliversInOrder(t *testing.T) {
	u := &recordingUploader{}
	p := New(u.up, Options{})
	ctx := context.Background()

	p.Append(ctx, "run1", "jobA", "one\n", nil)
	p.Append(ctx, "run1", "jobA", "two\n", nil)
	rep := p.Close(ctx)

	if rep.Undrained != 0 {
		t.Fatalf("Undrained = %d, want 0", rep.Undrained)
	}
	got := u.delivered()
	want := []string{"jobA:one\n", "jobA:two\n"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("delivered = %v, want %v", got, want)
	}
}

func TestPipeline_BuffersWhileDown_DrainsOnRecover(t *testing.T) {
	u := &recordingUploader{}
	now := time.Unix(0, 0)
	p := New(u.up, Options{RetryBackoff: time.Second, Now: func() time.Time { return now }})
	ctx := context.Background()

	// Backend down: appends buffer, nothing delivered.
	u.setFail(true)
	p.Append(ctx, "r", "j", "a\n", nil)
	now = now.Add(2 * time.Second) // past backoff so the next Append retries
	p.Append(ctx, "r", "j", "b\n", nil)
	if n := len(u.delivered()); n != 0 {
		t.Fatalf("delivered %d while down, want 0", n)
	}

	// Backend recovers: the next Append drains the backlog in order, then the
	// new chunk.
	u.setFail(false)
	now = now.Add(2 * time.Second)
	p.Append(ctx, "r", "j", "c\n", nil)
	rep := p.Close(ctx)

	if rep.Undrained != 0 {
		t.Fatalf("Undrained = %d, want 0 after recover", rep.Undrained)
	}
	got := u.delivered()
	want := []string{"j:a\n", "j:b\n", "j:c\n"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("delivered = %v, want in-order %v", got, want)
	}
}

func TestPipeline_SpillsToFile_WhenMemoryBoundExceeded(t *testing.T) {
	u := &recordingUploader{}
	spill := filepath.Join(t.TempDir(), "spill.ndjson")
	// Tiny bound so the second append overflows to disk while the backend is down.
	p := New(u.up, Options{MaxMemBytes: 4, SpillPath: spill, RetryBackoff: time.Hour,
		Now: func() time.Time { return time.Unix(0, 0) }})
	ctx := context.Background()

	u.setFail(true)
	p.Append(ctx, "r", "j", "aaaaaa\n", nil) // exceeds bound → spilled
	p.Append(ctx, "r", "j", "bbbbbb\n", nil) // exceeds bound → spilled

	data, err := os.ReadFile(spill)
	if err != nil {
		t.Fatalf("spill file not written: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("spill file empty; expected overflow persisted")
	}

	// Recover and Close: the spilled chunks drain in order and the file is gone.
	u.setFail(false)
	rep := p.Close(ctx)
	if rep.Undrained != 0 {
		t.Fatalf("Undrained = %d, want 0", rep.Undrained)
	}
	got := u.delivered()
	if len(got) != 2 || got[0] != "j:aaaaaa\n" || got[1] != "j:bbbbbb\n" {
		t.Fatalf("delivered = %v, want spilled chunks in order", got)
	}
	if _, err := os.Stat(spill); !os.IsNotExist(err) {
		t.Fatalf("spill file should be removed after full drain, stat err = %v", err)
	}
}

func TestPipeline_UndrainedAtClose_PersistsAndReports(t *testing.T) {
	u := &recordingUploader{}
	spill := filepath.Join(t.TempDir(), "spill.ndjson")
	p := New(u.up, Options{SpillPath: spill, RetryBackoff: 0,
		Now: func() time.Time { return time.Unix(0, 0) }})
	ctx := context.Background()

	// Backend never recovers.
	u.setFail(true)
	p.Append(ctx, "r", "j", "x\n", nil)
	p.Append(ctx, "r", "j", "y\n", nil)

	rep := p.Close(ctx)
	if rep.Undrained != 2 {
		t.Fatalf("Undrained = %d, want 2", rep.Undrained)
	}
	if rep.SpillPath != spill {
		t.Fatalf("SpillPath = %q, want %q", rep.SpillPath, spill)
	}
	if rep.UndrainedBytes == 0 {
		t.Fatalf("UndrainedBytes = 0, want > 0")
	}
	// The undrained bytes must be persisted on disk for recovery.
	data, err := os.ReadFile(spill)
	if err != nil || len(data) == 0 {
		t.Fatalf("undrained content not persisted to spill file: data=%q err=%v", data, err)
	}
}

func TestPipeline_RetryBackoff_SuppressesHammering(t *testing.T) {
	u := &recordingUploader{}
	now := time.Unix(0, 0)
	p := New(u.up, Options{RetryBackoff: 10 * time.Second, Now: func() time.Time { return now }})
	ctx := context.Background()
	u.setFail(true)

	p.Append(ctx, "r", "j", "a\n", nil) // attempt 1 (fails)
	p.waitIdle()
	p.Append(ctx, "r", "j", "b\n", nil) // within backoff → no upload attempt
	p.Append(ctx, "r", "j", "c\n", nil) // within backoff → no upload attempt
	p.waitIdle()
	if n := u.attempts(); n != 1 {
		t.Fatalf("upload attempts = %d, want 1 (backoff should suppress the rest)", n)
	}

	now = now.Add(11 * time.Second)     // past backoff
	p.Append(ctx, "r", "j", "d\n", nil) // attempt 2 (fails)
	p.waitIdle()
	if n := u.attempts(); n != 2 {
		t.Fatalf("upload attempts = %d, want 2 after backoff elapsed", n)
	}
}

func (u *recordingUploader) attempts() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls
}

// A step never waits for its block to land: Append returns while the
// upload is still in flight (specs/orun-sync-efficiency F8).
func TestPipeline_AppendNeverWaitsForTheUpload(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	var mu sync.Mutex
	var got []string
	up := func(_ context.Context, _ string, jobID, content string, _ *statebackend.LogStep) error {
		started <- struct{}{}
		<-release
		mu.Lock()
		got = append(got, jobID+":"+content)
		mu.Unlock()
		return nil
	}
	p := New(up, Options{})
	ctx := context.Background()
	done := make(chan struct{})
	go func() {
		p.Append(ctx, "r", "j", "one\n", nil)
		p.Append(ctx, "r", "j", "two\n", nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Append blocked on the upload")
	}
	<-started
	close(release)
	if rep := p.Close(ctx); rep.Undrained != 0 {
		t.Fatalf("Undrained = %d", rep.Undrained)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0] != "j:one\n" || got[1] != "j:two\n" {
		t.Fatalf("delivered = %v", got)
	}
}

// The jobs of a lane upload beside each other, each in its own order.
func TestPipeline_JobsUploadInParallel_EachInOrder(t *testing.T) {
	var mu sync.Mutex
	inFlight, peak := 0, 0
	perJob := map[string][]string{}
	up := func(_ context.Context, _ string, jobID, content string, _ *statebackend.LogStep) error {
		mu.Lock()
		inFlight++
		if inFlight > peak {
			peak = inFlight
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		inFlight--
		perJob[jobID] = append(perJob[jobID], content)
		mu.Unlock()
		return nil
	}
	p := New(up, Options{MaxInFlight: 4})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		for _, j := range []string{"a", "b", "c"} {
			p.Append(ctx, "r", j, j+string(rune('0'+i))+"\n", nil)
		}
	}
	if rep := p.Close(ctx); rep.Undrained != 0 {
		t.Fatalf("Undrained = %d", rep.Undrained)
	}
	mu.Lock()
	defer mu.Unlock()
	if peak < 2 {
		t.Fatalf("the jobs' uploads never overlapped: peak in flight = %d", peak)
	}
	for _, j := range []string{"a", "b", "c"} {
		if got := perJob[j]; len(got) != 3 || got[0] != j+"0\n" || got[1] != j+"1\n" || got[2] != j+"2\n" {
			t.Fatalf("job %s out of order: %v", j, got)
		}
	}
}

type rateLimited struct{ after time.Duration }

func (e rateLimited) Error() string                 { return "429 rate limited" }
func (e rateLimited) RetryAfterHint() time.Duration { return e.after }

// A 429 with a Retry-After is honoured on the spot: the chunk lands after
// the wait, without the backoff or the spill.
func TestPipeline_RetryAfterIsHonoured(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	var got []string
	up := func(_ context.Context, _ string, jobID, content string, _ *statebackend.LogStep) error {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return rateLimited{after: 10 * time.Millisecond}
		}
		got = append(got, jobID+":"+content)
		return nil
	}
	p := New(up, Options{RetryBackoff: time.Hour})
	ctx := context.Background()
	p.Append(ctx, "r", "j", "a\n", nil)
	p.Append(ctx, "r", "j", "b\n", nil)
	p.waitIdle()
	mu.Lock()
	if calls != 3 || len(got) != 2 {
		mu.Unlock()
		t.Fatalf("calls=%d delivered=%v; want the 429 retried after its hint and both chunks landed", calls, got)
	}
	mu.Unlock()
	if rep := p.Close(ctx); rep.Undrained != 0 {
		t.Fatalf("Undrained = %d", rep.Undrained)
	}
}

func TestPipeline_EmptyContent_NoOp(t *testing.T) {
	u := &recordingUploader{}
	p := New(u.up, Options{})
	p.Append(context.Background(), "r", "j", "", nil)
	if rep := p.Close(context.Background()); rep.Undrained != 0 {
		t.Fatalf("Undrained = %d, want 0", rep.Undrained)
	}
	if u.calls != 0 {
		t.Fatalf("calls = %d, want 0 for empty content", u.calls)
	}
}
