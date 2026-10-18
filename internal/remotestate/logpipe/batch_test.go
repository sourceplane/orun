package logpipe

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/statebackend"
)

// A job's queue goes in one call (saas-live-runs LR8b). On production a
// streaming job's chunks went one request each, one after another, and its log
// fell behind the job: every job's drain before :complete timed out.

// batchRecorder is a BatchUploader that records each call and can store only
// part of a batch, once.
type batchRecorder struct {
	mu        sync.Mutex
	calls     [][]string // per call: "job:content" of each chunk
	partOnce  int        // the next call stores this many and fails (0: none)
	failAll   bool
	delivered []string
}

func (b *batchRecorder) up(_ context.Context, _, jobID string, chunks []statebackend.LogChunk) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var call []string
	for _, c := range chunks {
		call = append(call, jobID+":"+c.Content)
	}
	b.calls = append(b.calls, call)
	if b.failAll {
		return 0, errors.New("backend unreachable")
	}
	n := len(chunks)
	var err error
	if b.partOnce > 0 {
		n, err = b.partOnce, errors.New("connection reset")
		b.partOnce = 0
	}
	b.delivered = append(b.delivered, call[:n]...)
	return n, err
}

func (b *batchRecorder) snapshot() ([][]string, []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([][]string(nil), b.calls...), append([]string(nil), b.delivered...)
}

// gatedUploader is an Uploader whose first call waits for release, so the
// test can build a backlog behind it.
type gatedUploader struct {
	recordingUploader
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func newGated() *gatedUploader {
	return &gatedUploader{entered: make(chan struct{}), release: make(chan struct{})}
}

func (g *gatedUploader) up(ctx context.Context, runID, jobID, content string, step *statebackend.LogStep) error {
	first := false
	g.once.Do(func() { first = true })
	if first {
		close(g.entered)
		<-g.release
	}
	return g.recordingUploader.up(ctx, runID, jobID, content, step)
}

func TestABacklogGoesAsOneBatch(t *testing.T) {
	g, b := newGated(), &batchRecorder{}
	p := New(g.up, Options{Batch: b.up})
	ctx := context.Background()

	p.Append(ctx, "r", "j", "0\n", nil)
	<-g.entered // the first chunk is in flight; the rest queue behind it
	for _, c := range []string{"1\n", "2\n", "3\n", "4\n"} {
		p.Append(ctx, "r", "j", c, nil)
	}
	close(g.release)
	if !p.Drain(ctx, "r", "j", 5*time.Second) {
		t.Fatal("the job's log did not drain")
	}
	p.Close(ctx)

	calls, delivered := b.snapshot()
	if got := g.delivered(); len(got) != 1 || got[0] != "j:0\n" {
		t.Fatalf("single uploads = %v, want just the first chunk", got)
	}
	if len(calls) != 1 || strings.Join(calls[0], "") != "j:1\nj:2\nj:3\nj:4\n" {
		t.Fatalf("batch calls = %q, want the four queued chunks in one, in order", calls)
	}
	if len(delivered) != 4 {
		t.Fatalf("delivered = %v", delivered)
	}
}

func TestAPartWayBatchSendsOnlyTheRestAgain(t *testing.T) {
	g, b := newGated(), &batchRecorder{partOnce: 2}
	p := New(g.up, Options{Batch: b.up, RetryBackoff: 10 * time.Millisecond})
	ctx := context.Background()

	p.Append(ctx, "r", "j", "0\n", nil)
	<-g.entered
	for _, c := range []string{"1\n", "2\n", "3\n", "4\n"} {
		p.Append(ctx, "r", "j", c, nil)
	}
	close(g.release)
	if !p.Drain(ctx, "r", "j", 5*time.Second) {
		t.Fatal("the rest was not sent again")
	}
	p.Close(ctx)

	_, delivered := b.snapshot()
	if got := strings.Join(delivered, ""); got != "j:1\nj:2\nj:3\nj:4\n" {
		t.Fatalf("delivered = %q, want every chunk once, in order", got)
	}
}

func TestCloseSendsOneBatchPerJob(t *testing.T) {
	u, b := &recordingUploader{}, &batchRecorder{}
	now := time.Unix(0, 0)
	p := New(u.up, Options{Batch: b.up, RetryBackoff: time.Hour, Now: func() time.Time { return now }})
	ctx := context.Background()

	// Down for the first upload; the backoff then holds everything queued.
	u.setFail(true)
	p.Append(ctx, "r", "jobA", "a1\n", nil)
	p.waitIdle()
	for _, c := range [][2]string{{"jobB", "b1\n"}, {"jobA", "a2\n"}, {"jobB", "b2\n"}, {"jobA", "a3\n"}} {
		p.Append(ctx, "r", c[0], c[1], nil)
	}
	rep := p.Close(ctx)

	if rep.Undrained != 0 {
		t.Fatalf("Undrained = %d, want 0", rep.Undrained)
	}
	calls, _ := b.snapshot()
	want := []string{"jobA:a1\njobA:a2\njobA:a3\n", "jobB:b1\njobB:b2\n"}
	if len(calls) != 2 || strings.Join(calls[0], "") != want[0] || strings.Join(calls[1], "") != want[1] {
		t.Fatalf("Close's calls = %q, want one per job, oldest job first: %q", calls, want)
	}
}

func TestTheSpillDrainBatchesEachJobAndKeepsWhatItCouldNotSend(t *testing.T) {
	spill := filepath.Join(t.TempDir(), "spill.ndjson")
	var lines []string
	for _, e := range []entry{
		{RunID: "r", JobID: "jobA", Content: "a1\n"},
		{RunID: "r", JobID: "jobB", Content: "b1\n"},
		{RunID: "r", JobID: "jobA", Content: "a2\n"},
		{RunID: "r", JobID: "jobB", Content: "b2\n"},
		{RunID: "r", JobID: "jobB", Content: "b3\n"},
	} {
		raw, _ := json.Marshal(e)
		lines = append(lines, string(raw))
	}
	lines = append(lines[:2], append([]string{"not json"}, lines[2:]...)...)
	if err := os.WriteFile(spill, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// jobA's batch goes; jobB's stores one of three and fails.
	u := &recordingUploader{}
	b := &batchRecorder{}
	p := New(u.up, Options{Batch: b.up, SpillPath: spill})
	first := true
	p.batch = func(ctx context.Context, runID, jobID string, chunks []statebackend.LogChunk) (int, error) {
		if !first {
			b.partOnce = 1
		}
		first = false
		return b.up(ctx, runID, jobID, chunks)
	}

	sent, _, done, err := p.flushSpill(context.Background())
	if err == nil || done || sent != 3 {
		t.Fatalf("flushSpill = sent %d, done %v, err %v; want 3 sent and the failure", sent, done, err)
	}
	calls, _ := b.snapshot()
	if len(calls) != 2 || strings.Join(calls[0], "") != "jobA:a1\njobA:a2\n" || strings.Join(calls[1], "") != "jobB:b1\njobB:b2\njobB:b3\n" {
		t.Fatalf("calls = %q, want one batch per job", calls)
	}
	rest, _ := os.ReadFile(spill)
	var left []string
	for _, l := range strings.Split(strings.TrimSpace(string(rest)), "\n") {
		var e entry
		if json.Unmarshal([]byte(l), &e) != nil {
			t.Fatalf("the remainder holds a line that is not an entry: %q", l)
		}
		left = append(left, e.JobID+":"+e.Content)
	}
	if strings.Join(left, "") != "jobB:b2\njobB:b3\n" {
		t.Fatalf("remainder = %q, want jobB's two unsent chunks in order", left)
	}
}

func TestWithoutABatchUploaderEachChunkIsItsOwnCall(t *testing.T) {
	g := newGated()
	p := New(g.up, Options{})
	ctx := context.Background()

	p.Append(ctx, "r", "j", "0\n", nil)
	<-g.entered
	p.Append(ctx, "r", "j", "1\n", nil)
	p.Append(ctx, "r", "j", "2\n", nil)
	close(g.release)
	p.Close(ctx)

	if got := g.delivered(); strings.Join(got, "") != "j:0\nj:1\nj:2\n" || g.calls != 3 {
		t.Fatalf("delivered = %v in %d calls, want three calls in order", got, g.calls)
	}
}
