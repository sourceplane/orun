package logpipe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// recordingUploader captures delivered (jobID, content) in order, and can be
// toggled to fail (simulating a backend that's unreachable mid-run).
type recordingUploader struct {
	mu    sync.Mutex
	fail  bool
	got   []string // "job:content"
	calls int
}

func (u *recordingUploader) up(_ context.Context, _ string, jobID, content string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls++
	if u.fail {
		return errors.New("backend unreachable")
	}
	u.got = append(u.got, jobID+":"+content)
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

	p.Append(ctx, "run1", "jobA", "one\n")
	p.Append(ctx, "run1", "jobA", "two\n")
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
	p.Append(ctx, "r", "j", "a\n")
	now = now.Add(2 * time.Second) // past backoff so the next Append retries
	p.Append(ctx, "r", "j", "b\n")
	if n := len(u.delivered()); n != 0 {
		t.Fatalf("delivered %d while down, want 0", n)
	}

	// Backend recovers: the next Append drains the backlog in order, then the
	// new chunk.
	u.setFail(false)
	now = now.Add(2 * time.Second)
	p.Append(ctx, "r", "j", "c\n")
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
	p.Append(ctx, "r", "j", "aaaaaa\n") // exceeds bound → spilled
	p.Append(ctx, "r", "j", "bbbbbb\n") // exceeds bound → spilled

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
	p.Append(ctx, "r", "j", "x\n")
	p.Append(ctx, "r", "j", "y\n")

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

	p.Append(ctx, "r", "j", "a\n") // attempt 1 (fails)
	p.Append(ctx, "r", "j", "b\n") // within backoff → no upload attempt
	p.Append(ctx, "r", "j", "c\n") // within backoff → no upload attempt
	if u.calls != 1 {
		t.Fatalf("upload attempts = %d, want 1 (backoff should suppress the rest)", u.calls)
	}

	now = now.Add(11 * time.Second) // past backoff
	p.Append(ctx, "r", "j", "d\n")  // attempt 2 (fails)
	if u.calls != 2 {
		t.Fatalf("upload attempts = %d, want 2 after backoff elapsed", u.calls)
	}
}

func TestPipeline_EmptyContent_NoOp(t *testing.T) {
	u := &recordingUploader{}
	p := New(u.up, Options{})
	p.Append(context.Background(), "r", "j", "")
	if rep := p.Close(context.Background()); rep.Undrained != 0 {
		t.Fatalf("Undrained = %d, want 0", rep.Undrained)
	}
	if u.calls != 0 {
		t.Fatalf("calls = %d, want 0 for empty content", u.calls)
	}
}
