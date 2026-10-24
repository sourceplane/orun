package logpipe

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/redact"
	"github.com/sourceplane/orun/internal/statebackend"
)

// saas-live-runs LR8: a running step's output reaches the remote log while
// the step runs, masked, and never with a secret split across two pieces.

type pieces struct {
	mu  sync.Mutex
	got []string
}

func (p *pieces) emit(s string) { p.mu.Lock(); p.got = append(p.got, s); p.mu.Unlock() }
func (p *pieces) all() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string{}, p.got...)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestStepStreamSendsCompleteLinesWhileTheStepRuns(t *testing.T) {
	var p pieces
	s := NewStepStream(p.emit, nil, StepStreamOptions{Interval: 10 * time.Millisecond})
	_, _ = s.Write([]byte("installing\nhalf a li"))
	waitFor(t, func() bool { return len(p.all()) == 1 })
	if got := p.all(); got[0] != "installing\n" {
		t.Fatalf("first piece %q", got[0])
	}
	_, _ = s.Write([]byte("ne\n"))
	waitFor(t, func() bool { return len(p.all()) == 2 })
	if sent := s.Finish(); sent != "installing\nhalf a line\n" {
		t.Fatalf("sent %q", sent)
	}
	_, _ = s.Write([]byte("after finish\n")) // ignored
	if len(p.all()) != 2 {
		t.Fatal("wrote after Finish")
	}
}

func TestStepStreamSendsALongLineAtTheSizeBound(t *testing.T) {
	var p pieces
	s := NewStepStream(p.emit, nil, StepStreamOptions{Interval: time.Hour, MaxBytes: 16})
	_, _ = s.Write([]byte(strings.Repeat("x", 40)))
	if got := p.all(); len(got) != 1 || got[0] != strings.Repeat("x", 40) {
		t.Fatalf("pieces %q", got)
	}
	sent, rest := s.FinishRest()
	if sent != strings.Repeat("x", 40) || rest != "" {
		t.Fatalf("sent %q rest %q", sent, rest)
	}
}

func TestStepStreamNeverSplitsASecretAndMasksAsTheWholeDoes(t *testing.T) {
	r := redact.New()
	r.Add("s3cr3t-token-value", "line1-of-key\nline2-of-key")
	whole := "start\nthe s3cr3t-token-value is here\nkey: line1-of-key\nline2-of-key done\n" + strings.Repeat("y", 50) + "s3cr3t-token-value\nend"
	for _, size := range []int{1, 2, 5, 13, 64} {
		var p pieces
		s := NewStepStream(p.emit, r, StepStreamOptions{Interval: time.Millisecond, MaxBytes: 8})
		for i := 0; i < len(whole); i += size {
			end := i + size
			if end > len(whole) {
				end = len(whole)
			}
			_, _ = s.Write([]byte(whole[i:end]))
			time.Sleep(time.Millisecond)
		}
		sent, rest := s.FinishRest()
		if sent+rest != r.Filter(whole) {
			t.Fatalf("size %d: sent+rest %q != %q", size, sent+rest, r.Filter(whole))
		}
		for _, piece := range p.all() {
			for _, frag := range []string{"s3cr3t", "token-value", "line1-of", "of-key"} {
				if strings.Contains(piece, frag) {
					t.Fatalf("size %d: piece %q carries part of a secret", size, piece)
				}
			}
		}
	}
}

func TestStepTail(t *testing.T) {
	cases := []struct{ sent, final, want string }{
		{"", "all of it", "all of it"},
		{"a\nb\n", "a\nb", ""},
		{"a\n", "a\nb\nc", "b\nc"},
		{"a\nb", "a\nbc", "c"},
		{"a\n\n", "a", ""},
		{"attempt 1\n", "attempt 2", "--- orun: the step's recorded output ---\nattempt 2"},
		{"partial", "other", "\n--- orun: the step's recorded output ---\nother"},
	}
	for _, c := range cases {
		if got := StepTail(c.sent, c.final); got != c.want {
			t.Errorf("StepTail(%q, %q) = %q, want %q", c.sent, c.final, got, c.want)
		}
	}
}

type recUploader struct {
	mu    sync.Mutex
	calls []string
	fail  int // fail this many uploads first
	delay time.Duration
}

func (u *recUploader) up(ctx context.Context, runID, jobID, content string, step *statebackend.LogStep) error {
	if u.delay > 0 {
		time.Sleep(u.delay)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.fail > 0 {
		u.fail--
		return errors.New("backend down")
	}
	ev := ""
	if step != nil {
		ev = step.Event
	}
	u.calls = append(u.calls, ev+":"+content)
	return nil
}

func (u *recUploader) got() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string{}, u.calls...)
}

func TestAStepsEmptyStartAndEndStillGoOut(t *testing.T) {
	u := &recUploader{}
	p := New(u.up, Options{})
	ctx := context.Background()
	p.Append(ctx, "r", "j", "", &statebackend.LogStep{StepID: "s", Event: statebackend.LogStepStart})
	p.Append(ctx, "r", "j", "", &statebackend.LogStep{StepID: "s", Event: statebackend.LogStepChunk}) // nothing
	p.Append(ctx, "r", "j", "", nil)                                                                  // nothing
	p.Append(ctx, "r", "j", "", &statebackend.LogStep{StepID: "s", Event: statebackend.LogStepEnd, Status: "succeeded"})
	_ = p.Close(ctx)
	if got := u.got(); strings.Join(got, ",") != "start:,end:" {
		t.Fatalf("uploads %q", got)
	}
}

func TestAFailedUploadIsRetriedOnATimer(t *testing.T) {
	u := &recUploader{fail: 1}
	p := New(u.up, Options{RetryBackoff: 20 * time.Millisecond})
	p.Append(context.Background(), "r", "j", "last chunk", nil)
	// No further Append: the retry timer alone delivers it.
	waitFor(t, func() bool { return len(u.got()) == 1 })
	_ = p.Close(context.Background())
}

func TestDrainWaitsForTheJobsChunksBeforeComplete(t *testing.T) {
	u := &recUploader{delay: 30 * time.Millisecond}
	p := New(u.up, Options{})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		p.Append(ctx, "r", "j", "chunk", nil)
	}
	if !p.Drain(ctx, "r", "j", 2*time.Second) {
		t.Fatal("Drain gave up")
	}
	if len(u.got()) != 3 {
		t.Fatalf("drained before the uploads landed: %d", len(u.got()))
	}
	// A job with nothing queued drains at once.
	if !p.Drain(ctx, "r", "other", time.Second) {
		t.Fatal("empty Drain")
	}
	_ = p.Close(ctx)
}

func TestDrainGivesUpAtItsBoundWhenTheBackendIsDown(t *testing.T) {
	u := &recUploader{fail: 1 << 30}
	p := New(u.up, Options{RetryBackoff: time.Hour})
	ctx := context.Background()
	p.Append(ctx, "r", "j", "chunk", nil)
	start := time.Now()
	if p.Drain(ctx, "r", "j", 80*time.Millisecond) {
		t.Fatal("Drain claimed success")
	}
	if waited := time.Since(start); waited < 70*time.Millisecond || waited > time.Second {
		t.Fatalf("Drain waited %v", waited)
	}
	if rep := p.Close(ctx); rep.Undrained != 1 {
		t.Fatalf("report %+v", rep)
	}
}
