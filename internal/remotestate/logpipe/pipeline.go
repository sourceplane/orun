// Package logpipe buffers remote-state log chunks so a backend that goes
// unreachable mid-run never strands a runner (orun-cloud design §7, row 5:
// "buffers log chunks (bounded, memory + spill file), retries with backoff;
// exits non-zero with 'state may be stale on the server' if buffers can't
// drain").
//
// The runner hands every step's log block to Append, which absorbs upload
// failures: chunks accumulate in a bounded in-memory queue and overflow to a
// spill file on disk, and each Append (and the final Close) opportunistically
// re-drains — oldest first — once the backend recovers. Append never blocks the
// run on a transport error; the caller learns whether anything was lost from
// Close's Report and surfaces the non-zero-exit warning.
package logpipe

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
)

// Uploader sends one already-formed log block for (runID, jobID). It is the
// backend's AppendStepLog; logpipe adds the buffering/spill/retry around it.
type Uploader func(ctx context.Context, runID, jobID, content string) error

// Defaults.
const (
	defaultMaxMemBytes  = 256 * 1024 // spill to disk once pending memory exceeds this
	defaultRetryBackoff = 2 * time.Second
)

// Options configures a Pipeline. The zero value is usable (memory-only, default
// bound + backoff); production sets SpillPath so overflow survives in a file.
type Options struct {
	// MaxMemBytes is the in-memory pending cap; once exceeded, pending entries
	// move to the spill file (when SpillPath is set). Default 256 KiB.
	MaxMemBytes int
	// SpillPath is the ndjson overflow/persistence file. Empty keeps everything
	// in memory (the bound is then advisory — nothing is dropped).
	SpillPath string
	// RetryBackoff is the minimum gap between flush attempts after a failure, so
	// a down backend isn't hammered on every step. Default 2s. Close ignores it.
	RetryBackoff time.Duration
	// Now is injectable for tests; defaults to time.Now.
	Now func() time.Time
}

// Report is the outcome at Close. Undrained > 0 means logs could not be
// delivered; the caller should warn ("state may be stale on the server") and
// exit non-zero. When SpillPath is set the undrained bytes persist there.
type Report struct {
	Undrained      int
	UndrainedBytes int
	SpillPath      string
}

type entry struct {
	RunID   string `json:"r"`
	JobID   string `json:"j"`
	Content string `json:"c"`
}

func (e entry) size() int { return len(e.RunID) + len(e.JobID) + len(e.Content) }

// Pipeline is safe for concurrent Append from multiple job goroutines.
//
// Ordering invariant: spilled entries (on disk) are always OLDER than in-memory
// `pending`, and a flush always drains the spill file before pending — so the
// assembled per-job log preserves step order even across an outage.
type Pipeline struct {
	up      Uploader
	maxMem  int
	spill   string
	backoff time.Duration
	now     func() time.Time

	mu           sync.Mutex
	pending      []entry
	pendingBytes int
	spilledCount int
	spilledBytes int
	lastFailAt   time.Time
	everFailed   bool
}

// New returns a Pipeline wrapping up. up must be non-nil.
func New(up Uploader, opts Options) *Pipeline {
	maxMem := opts.MaxMemBytes
	if maxMem <= 0 {
		maxMem = defaultMaxMemBytes
	}
	backoff := opts.RetryBackoff
	if backoff <= 0 {
		backoff = defaultRetryBackoff
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Pipeline{up: up, maxMem: maxMem, spill: opts.SpillPath, backoff: backoff, now: now}
}

// Append buffers content for (runID, jobID) and opportunistically flushes.
// Empty content is a no-op. It never returns an error — delivery failures are
// absorbed and reported by Close.
func (p *Pipeline) Append(ctx context.Context, runID, jobID, content string) {
	if content == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	p.pending = append(p.pending, entry{RunID: runID, JobID: jobID, Content: content})
	p.pendingBytes += len(runID) + len(jobID) + len(content)
	p.spillOverflowLocked()

	// Retry with backoff: skip a flush attempt if we failed too recently, so a
	// down backend isn't hit on every step. The next Append (or Close) retries.
	if p.everFailed && p.now().Sub(p.lastFailAt) < p.backoff {
		return
	}
	_ = p.flushLocked(ctx)
}

// Close makes a final (backoff-ignoring) drain attempt and reports what, if
// anything, could not be delivered. Any undrained pending is persisted to the
// spill file first so the bytes survive process exit.
func (p *Pipeline) Close(ctx context.Context) Report {
	p.mu.Lock()
	defer p.mu.Unlock()

	_ = p.flushLocked(ctx)

	if (p.spilledCount > 0 || len(p.pending) > 0) && p.spill != "" {
		// Persist any still-pending memory entries so the undrained bytes are on
		// disk for the user to recover/inspect.
		_ = p.drainPendingToSpillLocked()
	}

	rep := Report{
		Undrained:      p.spilledCount + len(p.pending),
		UndrainedBytes: p.spilledBytes + p.pendingBytes,
	}
	if rep.Undrained > 0 && p.spill != "" {
		rep.SpillPath = p.spill
	}
	return rep
}

// spillOverflowLocked moves pending entries to the spill file once memory
// exceeds the bound. No-op when no spill file is configured.
func (p *Pipeline) spillOverflowLocked() {
	if p.spill == "" || p.pendingBytes <= p.maxMem {
		return
	}
	_ = p.drainPendingToSpillLocked()
}

// drainPendingToSpillLocked appends all in-memory pending entries to the spill
// file (oldest-first) and clears memory. Pending is always newer than what's
// already spilled, so appending preserves global order.
func (p *Pipeline) drainPendingToSpillLocked() error {
	if len(p.pending) == 0 {
		return nil
	}
	f, err := os.OpenFile(p.spill, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, e := range p.pending {
		b, _ := json.Marshal(e)
		if _, err := w.Write(append(b, '\n')); err != nil {
			_ = w.Flush()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	p.spilledCount += len(p.pending)
	p.spilledBytes += p.pendingBytes
	p.pending = nil
	p.pendingBytes = 0
	return nil
}

// flushLocked drains the spill file (oldest) then in-memory pending, in order,
// stopping at the first upload error. On any failure it records the backoff
// timestamp. Returns the upload error, or nil when everything drained.
func (p *Pipeline) flushLocked(ctx context.Context) error {
	if p.spilledCount > 0 {
		if err := p.flushSpillLocked(ctx); err != nil {
			p.markFailed()
			return err
		}
	}
	for len(p.pending) > 0 {
		e := p.pending[0]
		if err := p.up(ctx, e.RunID, e.JobID, e.Content); err != nil {
			p.markFailed()
			return err
		}
		p.pending = p.pending[1:]
		p.pendingBytes -= e.size()
	}
	if p.pendingBytes < 0 {
		p.pendingBytes = 0
	}
	return nil
}

// flushSpillLocked uploads spilled entries oldest-first; on a mid-file failure
// it rewrites the spill file with the unsent remainder so nothing is lost or
// reordered. On full success it removes the file.
func (p *Pipeline) flushSpillLocked(ctx context.Context) error {
	data, err := os.ReadFile(p.spill)
	if err != nil {
		if os.IsNotExist(err) {
			p.spilledCount, p.spilledBytes = 0, 0
			return nil
		}
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	sent := 0
	var uploadErr error
	for _, line := range lines {
		if line == "" {
			sent++
			continue
		}
		var e entry
		if json.Unmarshal([]byte(line), &e) != nil {
			sent++ // unparseable line: drop it rather than wedge the pipe
			continue
		}
		if err := p.up(ctx, e.RunID, e.JobID, e.Content); err != nil {
			uploadErr = err
			break
		}
		sent++
		p.spilledCount--
		p.spilledBytes -= e.size()
	}
	if uploadErr != nil {
		// Persist the unsent remainder back to the spill file.
		remainder := strings.Join(lines[sent:], "\n")
		if remainder != "" {
			remainder += "\n"
		}
		_ = os.WriteFile(p.spill, []byte(remainder), 0o600)
		return uploadErr
	}
	// Everything drained.
	_ = os.Remove(p.spill)
	p.spilledCount, p.spilledBytes = 0, 0
	return nil
}

func (p *Pipeline) markFailed() {
	p.everFailed = true
	p.lastFailAt = p.now()
}
