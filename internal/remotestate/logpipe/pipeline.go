// Package logpipe buffers remote-state log chunks so a backend that goes
// unreachable mid-run never strands a runner (orun-cloud design §7, row 5:
// "buffers log chunks (bounded, memory + spill file), retries with backoff;
// exits non-zero with 'state may be stale on the server' if buffers can't
// drain").
//
// The runner hands every step's log block to Append, which queues it and
// returns: the upload happens beside the run, on a worker per job, so a
// step never waits for its block to land and the jobs of a lane never
// serialise on one upload (specs/orun-sync-efficiency F8). Upload failures
// are absorbed: chunks accumulate in a bounded in-memory queue and overflow
// to a spill file on disk, and each Append (and the final Close)
// opportunistically re-drains — oldest first — once the backend recovers.
// The caller learns whether anything was lost from Close's Report and
// surfaces the non-zero-exit warning.
package logpipe

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sourceplane/orun/internal/statebackend"
)

// Uploader sends one already-formed log block for (runID, jobID). It is the
// backend's AppendStepLog; logpipe adds the buffering/spill/retry around it.
type Uploader func(ctx context.Context, runID, jobID, content string, step *statebackend.LogStep) error

// Defaults.
const (
	defaultMaxMemBytes  = 256 * 1024 // spill to disk once pending memory exceeds this
	defaultRetryBackoff = 2 * time.Second
	defaultMaxInFlight  = 4 // uploads in flight across jobs; one per job at most
	// maxRetryAfterWait bounds how long a worker honours a server's Retry-After
	// on the spot before leaving the chunk to the backoff path.
	maxRetryAfterWait = 10 * time.Second
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
	// MaxInFlight bounds the uploads in flight across jobs (a job's own chunks
	// always go one after another, in order). Default 4.
	MaxInFlight int
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
	// Step is the chunk's step coordinate (saas-step-logs SL-R1), spilled with
	// it so an outage does not cost the attribution.
	//
	// `omitempty` and a pointer are what make the spill format backward
	// compatible BOTH ways: a file written by a binary that predates this field
	// decodes with Step nil and uploads exactly as it always did, and a file
	// written by this binary is still readable by the older decoder, which
	// ignores the key. A spill file outlives the binary that wrote it — that is
	// the whole point of one — so neither direction is hypothetical.
	Step *statebackend.LogStep `json:"s,omitempty"`
	// n is the arrival order across jobs: what the spill file is written in,
	// so a drain of it keeps every job's order.
	n uint64
}

func (e entry) size() int { return len(e.RunID) + len(e.JobID) + len(e.Content) }

// jobQueue is one job's chunks in arrival order, and whether a worker is
// uploading them.
type jobQueue struct {
	entries []entry
	running bool
}

// Pipeline is safe for concurrent Append from multiple job goroutines.
//
// Ordering invariant: spilled entries (on disk) are always OLDER than in-memory
// pending, and a drain always empties the spill file before any pending
// entry is uploaded — so the assembled per-job log preserves step order even
// across an outage. While the spill file holds entries, or the backend is in
// backoff, no per-job worker runs; the serial recovery drain goes first.
type Pipeline struct {
	up          Uploader
	maxMem      int
	spill       string
	backoff     time.Duration
	maxInFlight int
	now         func() time.Time

	mu           sync.Mutex
	idle         *sync.Cond // signalled when a worker or the recovery drain finishes
	queues       map[string]*jobQueue
	seq          uint64
	pendingBytes int
	spilledCount int
	spilledBytes int
	lastFailAt   time.Time
	everFailed   bool
	retryNotBef  time.Time // a server's Retry-After, honoured over the backoff
	inFlight     int
	draining     bool // the serial recovery drain is running
	closing      bool
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
	inFlight := opts.MaxInFlight
	if inFlight <= 0 {
		inFlight = defaultMaxInFlight
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	p := &Pipeline{up: up, maxMem: maxMem, spill: opts.SpillPath, backoff: backoff, maxInFlight: inFlight, now: now, queues: map[string]*jobQueue{}}
	p.idle = sync.NewCond(&p.mu)
	return p
}

func queueKey(runID, jobID string) string { return runID + "\x00" + jobID }

// Append queues content for (runID, jobID) and returns; the upload happens
// on the job's worker. Empty content is a no-op. It never returns an error —
// delivery failures are absorbed and reported by Close.
func (p *Pipeline) Append(ctx context.Context, runID, jobID, content string, step *statebackend.LogStep) {
	if content == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	p.seq++
	q := p.queues[queueKey(runID, jobID)]
	if q == nil {
		q = &jobQueue{}
		p.queues[queueKey(runID, jobID)] = q
	}
	e := entry{RunID: runID, JobID: jobID, Content: content, Step: step, n: p.seq}
	q.entries = append(q.entries, e)
	p.pendingBytes += e.size()
	p.spillOverflowLocked()
	p.kickLocked(ctx)
}

// kickLocked starts whatever may run now: the serial recovery drain when the
// spill file holds entries, else a worker per job with chunks waiting, up to
// the in-flight bound. Nothing starts within the backoff of a failure (the
// next Append or Close tries again), nor once Close began. mu held.
func (p *Pipeline) kickLocked(ctx context.Context) {
	if p.closing || p.draining {
		return
	}
	if p.everFailed && p.now().Sub(p.lastFailAt) < p.backoff {
		return
	}
	if !p.retryNotBef.IsZero() && p.now().Before(p.retryNotBef) {
		return
	}
	if p.spilledCount > 0 {
		p.draining = true
		go p.recover(ctx)
		return
	}
	for key, q := range p.queues {
		if p.inFlight >= p.maxInFlight {
			return
		}
		if q.running || len(q.entries) == 0 {
			continue
		}
		q.running = true
		p.inFlight++
		go p.work(ctx, key)
	}
}

// work uploads one job's chunks in order until its queue is empty or an
// upload fails; a failure leaves the rest queued for the backoff path.
func (p *Pipeline) work(ctx context.Context, key string) {
	defer func() {
		p.mu.Lock()
		p.queues[key].running = false
		p.inFlight--
		p.kickLocked(ctx) // a queue that waited on the in-flight bound
		p.idle.Broadcast()
		p.mu.Unlock()
	}()
	for {
		p.mu.Lock()
		q := p.queues[key]
		if len(q.entries) == 0 {
			p.mu.Unlock()
			return
		}
		e := q.entries[0]
		p.mu.Unlock()

		if err := p.upload(ctx, e); err != nil {
			p.mu.Lock()
			p.markFailedLocked(err)
			p.mu.Unlock()
			return
		}
		p.mu.Lock()
		if len(q.entries) > 0 && q.entries[0].n == e.n {
			q.entries = q.entries[1:]
			p.pendingBytes -= e.size()
			if p.pendingBytes < 0 {
				p.pendingBytes = 0
			}
		}
		p.mu.Unlock()
	}
}

// retryAfterer is what a rate-limited upload error says about when to try
// again (remotestate's APIError carries the server's Retry-After).
type retryAfterer interface {
	RetryAfterHint() time.Duration
}

// upload sends one entry, honouring a Retry-After hint on the spot (once,
// bounded) so a burst of appends that meets the limiter rides through it
// instead of falling into the backoff and the spill.
func (p *Pipeline) upload(ctx context.Context, e entry) error {
	err := p.up(ctx, e.RunID, e.JobID, e.Content, e.Step)
	if err == nil {
		return nil
	}
	var ra retryAfterer
	if errors.As(err, &ra) {
		if wait := ra.RetryAfterHint(); wait > 0 && wait <= maxRetryAfterWait {
			select {
			case <-ctx.Done():
				return err
			case <-time.After(wait):
			}
			return p.up(ctx, e.RunID, e.JobID, e.Content, e.Step)
		}
	}
	return err
}

// recover is the serial drain after a spill: the spill file oldest-first,
// then the per-job workers for what is queued in memory.
func (p *Pipeline) recover(ctx context.Context) {
	sent, sentBytes, done, err := p.flushSpill(ctx)
	p.mu.Lock()
	p.noteSpillDrainLocked(sent, sentBytes, done)
	p.draining = false
	if err != nil {
		p.markFailedLocked(err)
	} else {
		p.kickLocked(ctx)
	}
	p.idle.Broadcast()
	p.mu.Unlock()
}

// noteSpillDrainLocked accounts what a spill drain delivered. mu held.
func (p *Pipeline) noteSpillDrainLocked(sent, sentBytes int, done bool) {
	p.spilledCount -= sent
	p.spilledBytes -= sentBytes
	if done || p.spilledCount < 0 {
		p.spilledCount, p.spilledBytes = 0, 0
	}
}

// Close waits for the uploads in flight, makes a final (backoff-ignoring)
// drain attempt and reports what, if anything, could not be delivered. Any
// undrained pending is persisted to the spill file first so the bytes
// survive process exit.
func (p *Pipeline) Close(ctx context.Context) Report {
	p.mu.Lock()
	p.closing = true
	for p.inFlight > 0 || p.draining {
		p.idle.Wait()
	}
	defer p.mu.Unlock()

	_ = p.flushLocked(ctx)

	if (p.spilledCount > 0 || p.pendingCountLocked() > 0) && p.spill != "" {
		// Persist any still-pending memory entries so the undrained bytes are on
		// disk for the user to recover/inspect.
		_ = p.drainPendingToSpillLocked()
	}

	rep := Report{
		Undrained:      p.spilledCount + p.pendingCountLocked(),
		UndrainedBytes: p.spilledBytes + p.pendingBytes,
	}
	if rep.Undrained > 0 && p.spill != "" {
		rep.SpillPath = p.spill
	}
	return rep
}

// waitIdle blocks until no worker or recovery drain is running (tests).
func (p *Pipeline) waitIdle() {
	p.mu.Lock()
	for p.inFlight > 0 || p.draining {
		p.idle.Wait()
	}
	p.mu.Unlock()
}

func (p *Pipeline) pendingCountLocked() int {
	n := 0
	for _, q := range p.queues {
		n += len(q.entries)
	}
	return n
}

// pendingInOrderLocked is every queued entry across jobs in arrival order.
func (p *Pipeline) pendingInOrderLocked() []entry {
	var all []entry
	for _, q := range p.queues {
		all = append(all, q.entries...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].n < all[j].n })
	return all
}

// spillOverflowLocked moves pending entries to the spill file once memory
// exceeds the bound. No-op when no spill file is configured, and while the
// spill file is being drained (the bound is advisory for that long).
func (p *Pipeline) spillOverflowLocked() {
	if p.spill == "" || p.pendingBytes <= p.maxMem || p.draining || p.inFlight > 0 {
		return
	}
	_ = p.drainPendingToSpillLocked()
}

// drainPendingToSpillLocked appends all in-memory pending entries to the spill
// file (oldest-first) and clears memory. Pending is always newer than what's
// already spilled, so appending preserves global order.
func (p *Pipeline) drainPendingToSpillLocked() error {
	all := p.pendingInOrderLocked()
	if len(all) == 0 {
		return nil
	}
	f, err := os.OpenFile(p.spill, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, e := range all {
		b, _ := json.Marshal(e)
		if _, err := w.Write(append(b, '\n')); err != nil {
			_ = w.Flush()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	p.spilledCount += len(all)
	p.spilledBytes += p.pendingBytes
	for _, q := range p.queues {
		q.entries = nil
	}
	p.pendingBytes = 0
	return nil
}

// flushLocked drains the spill file (oldest) then in-memory pending, in
// arrival order, stopping at the first upload error. On any failure it
// records the backoff timestamp. Returns the upload error, or nil when
// everything drained. mu held; the final drain at Close, when nothing else
// runs.
func (p *Pipeline) flushLocked(ctx context.Context) error {
	if p.spilledCount > 0 {
		sent, sentBytes, done, err := p.flushSpill(ctx)
		p.noteSpillDrainLocked(sent, sentBytes, done)
		if err != nil {
			p.markFailedLocked(err)
			return err
		}
	}
	for _, e := range p.pendingInOrderLocked() {
		if err := p.up(ctx, e.RunID, e.JobID, e.Content, e.Step); err != nil {
			p.markFailedLocked(err)
			return err
		}
		q := p.queues[queueKey(e.RunID, e.JobID)]
		if len(q.entries) > 0 && q.entries[0].n == e.n {
			q.entries = q.entries[1:]
		}
		p.pendingBytes -= e.size()
	}
	if p.pendingBytes < 0 {
		p.pendingBytes = 0
	}
	return nil
}

// flushSpill uploads spilled entries oldest-first; on a mid-file failure it
// rewrites the spill file with the unsent remainder so nothing is lost or
// reordered. On full success it removes the file. It reports what it sent
// and whether the file is gone; the caller accounts it under mu (which this
// never takes, so it runs with or without the lock). Nothing else writes the
// spill file while it runs: Append does not spill during a drain, and Close
// waits for it.
func (p *Pipeline) flushSpill(ctx context.Context) (sent, sentBytes int, done bool, err error) {
	data, err := os.ReadFile(p.spill)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, true, nil
		}
		return 0, 0, false, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	consumed := 0 // lines past, whether sent, blank or unparseable
	var uploadErr error
	for _, line := range lines {
		if line == "" {
			consumed++
			continue
		}
		var e entry
		if json.Unmarshal([]byte(line), &e) != nil {
			consumed++ // unparseable line: drop it rather than wedge the pipe
			continue
		}
		if err := p.upload(ctx, e); err != nil {
			uploadErr = err
			break
		}
		consumed++
		sent++
		sentBytes += e.size()
	}
	if uploadErr != nil {
		// Persist the unsent remainder back to the spill file.
		remainder := strings.Join(lines[consumed:], "\n")
		if remainder != "" {
			remainder += "\n"
		}
		_ = os.WriteFile(p.spill, []byte(remainder), 0o600)
		return sent, sentBytes, false, uploadErr
	}
	// Everything drained.
	_ = os.Remove(p.spill)
	return sent, sentBytes, true, nil
}

// markFailedLocked records the failure for the backoff, and a server's
// Retry-After when the error carries one. mu held.
func (p *Pipeline) markFailedLocked(err error) {
	p.everFailed = true
	p.lastFailAt = p.now()
	var ra retryAfterer
	if errors.As(err, &ra) {
		if wait := ra.RetryAfterHint(); wait > 0 {
			p.retryNotBef = p.now().Add(wait)
			return
		}
	}
	p.retryNotBef = time.Time{}
}
