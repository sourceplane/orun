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
	"fmt"
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

// BatchUploader sends a job's queued chunks in one call and returns how many,
// from the first, it stored (saas-live-runs LR8b): the backend's
// AppendStepLogs. Fewer than it was given comes with the error that stopped
// it; the pipeline keeps the rest queued.
type BatchUploader func(ctx context.Context, runID, jobID string, chunks []statebackend.LogChunk) (int, error)

// Defaults.
const (
	defaultMaxMemBytes  = 256 * 1024 // spill to disk once pending memory exceeds this
	defaultRetryBackoff = 2 * time.Second
	defaultMaxInFlight  = 4 // uploads in flight across jobs; one per job at most
	// maxRetryAfterWait bounds how long a worker honours a server's Retry-After
	// on the spot before leaving the chunk to the backoff path.
	maxRetryAfterWait = 10 * time.Second
	// A batch (LR8b) is at most what one batched append may carry: the server
	// takes 64 entries and 1 MiB of content; the backend splits further if a
	// chunk alone is bigger.
	maxBatchEntries = 64
	maxBatchBytes   = 768 * 1024
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
	// Batch, when set, sends a job's queued chunks in one call (saas-live-runs
	// LR8b). A streaming step queues a start, a chunk every couple of seconds
	// and an end; one request each falls behind the job, so a job's worker
	// sends whatever is queued for it as one batch, and the final flush and
	// the spill drain send one batch per job. Nil keeps one Uploader call per
	// chunk.
	Batch BatchUploader
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
	batch       BatchUploader
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
	// retryTimer re-tries a failed upload once the backoff (or a server's
	// Retry-After) is over, so waiting chunks do not wait for the next
	// Append (saas-live-runs LR8): a step's last chunk would otherwise sit
	// until the job's next step, or the run's end.
	retryTimer *time.Timer
	// ctx is the context uploads started by the retry timer run under: the
	// first Append's.
	ctx context.Context
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
	p := &Pipeline{up: up, batch: opts.Batch, maxMem: maxMem, spill: opts.SpillPath, backoff: backoff, maxInFlight: inFlight, now: now, queues: map[string]*jobQueue{}}
	p.idle = sync.NewCond(&p.mu)
	return p
}

func queueKey(runID, jobID string) string { return runID + "\x00" + jobID }

// Append queues content for (runID, jobID) and returns; the upload happens
// on the job's worker. Empty content is a no-op, except for a step's start
// or end: the event is the point (saas-live-runs LR8). It never returns an
// error — delivery failures are absorbed and reported by Close.
func (p *Pipeline) Append(ctx context.Context, runID, jobID, content string, step *statebackend.LogStep) {
	if content == "" && (step == nil || step.Event == statebackend.LogStepChunk) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx == nil {
		p.ctx = ctx
	}

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
// retry timer, the next Append or Close tries again), nor once Close began.
// mu held.
func (p *Pipeline) kickLocked(ctx context.Context) {
	p.kick(ctx, false)
}

// kick is kickLocked; now ignores the failure backoff (not a server's
// Retry-After), for the retry timer and a Drain that cannot wait it out.
// mu held.
func (p *Pipeline) kick(ctx context.Context, now bool) {
	if p.closing || p.draining {
		return
	}
	if !now && p.everFailed && p.now().Sub(p.lastFailAt) < p.backoff {
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
// upload fails; a failure leaves the rest queued for the backoff path. With a
// BatchUploader, whatever is queued when the worker looks goes as one batch
// (LR8b), so a job that fell behind catches up in a round trip or two.
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
		batch := p.takeBatch(q.entries)
		p.mu.Unlock()

		n, err := p.uploadSome(ctx, batch)
		p.mu.Lock()
		p.dropSentLocked(q, batch[:n])
		if err != nil {
			p.markFailedLocked(err)
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()
	}
}

// takeBatch is the leading entries of a job's queue one call sends: all of
// them, up to the batch bounds, with a BatchUploader; else the first.
func (p *Pipeline) takeBatch(entries []entry) []entry {
	if p.batch == nil || len(entries) == 1 {
		return append([]entry(nil), entries[0])
	}
	n, size := 0, 0
	for n < len(entries) && n < maxBatchEntries {
		c := len(entries[n].Content)
		if n > 0 && size+c > maxBatchBytes {
			break
		}
		size += c
		n++
	}
	return append([]entry(nil), entries[:n]...)
}

// uploadSome sends one job's entries — one Uploader call for a single entry,
// one BatchUploader call for several — and returns how many, from the first,
// were stored. A server's Retry-After is honoured on the spot once, as for a
// single upload.
func (p *Pipeline) uploadSome(ctx context.Context, batch []entry) (int, error) {
	if len(batch) == 1 || p.batch == nil {
		if err := p.upload(ctx, batch[0]); err != nil {
			return 0, err
		}
		return 1, nil
	}
	chunks := make([]statebackend.LogChunk, len(batch))
	for i, e := range batch {
		chunks[i] = statebackend.LogChunk{Content: e.Content, Step: e.Step}
	}
	runID, jobID := batch[0].RunID, batch[0].JobID
	n, err := p.batch(ctx, runID, jobID, chunks)
	if err != nil && n == 0 && waitRetryAfter(ctx, err) {
		n, err = p.batch(ctx, runID, jobID, chunks)
	}
	if n < 0 {
		n = 0
	}
	if n > len(batch) {
		n = len(batch)
	}
	if err == nil && n < len(batch) {
		// Short of what it was given and no reason why: the rest waits for the
		// backoff like any failure, rather than a worker asking again at once.
		err = fmt.Errorf("log batch for %s: %d of %d chunks stored", jobID, n, len(batch))
	}
	return n, err
}

// dropSentLocked removes the entries just stored from the front of their
// job's queue. Only that job's worker (or Close, with no worker running)
// takes from the front, so they are still there. mu held.
func (p *Pipeline) dropSentLocked(q *jobQueue, sent []entry) {
	for _, e := range sent {
		if len(q.entries) == 0 || q.entries[0].n != e.n {
			return
		}
		q.entries = q.entries[1:]
		p.pendingBytes -= e.size()
	}
	if p.pendingBytes < 0 {
		p.pendingBytes = 0
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
	if waitRetryAfter(ctx, err) {
		return p.up(ctx, e.RunID, e.JobID, e.Content, e.Step)
	}
	return err
}

// waitRetryAfter waits out a short Retry-After the error carries and reports
// whether the caller should try again now.
func waitRetryAfter(ctx context.Context, err error) bool {
	var ra retryAfterer
	if !errors.As(err, &ra) {
		return false
	}
	wait := ra.RetryAfterHint()
	if wait <= 0 || wait > maxRetryAfterWait {
		return false
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(wait):
		return true
	}
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
	if p.retryTimer != nil {
		p.retryTimer.Stop()
		p.retryTimer = nil
	}
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
	// Each job's queue in order, the jobs in the order they first queued; with
	// a BatchUploader, a job's queue goes as one batch (LR8b).
	for _, q := range p.queuesInOrderLocked() {
		for len(q.entries) > 0 {
			batch := p.takeBatch(q.entries)
			n, err := p.uploadSome(ctx, batch)
			p.dropSentLocked(q, batch[:n])
			if err != nil {
				p.markFailedLocked(err)
				return err
			}
		}
	}
	return nil
}

// queuesInOrderLocked is every job queue holding entries, ordered by its
// oldest entry. mu held.
func (p *Pipeline) queuesInOrderLocked() []*jobQueue {
	var qs []*jobQueue
	for _, q := range p.queues {
		if len(q.entries) > 0 {
			qs = append(qs, q)
		}
	}
	sort.Slice(qs, func(i, j int) bool { return qs[i].entries[0].n < qs[j].entries[0].n })
	return qs
}

// flushSpill uploads spilled entries, each job's in order (the jobs in the
// order they first appear), a job's as batches with a BatchUploader (LR8b). On
// a failure it rewrites the spill file with what was not sent, in its order,
// so nothing is lost or reordered within a job. On full success it removes the
// file. It reports what it sent and whether the file is gone; the caller
// accounts it under mu (which this never takes, so it runs with or without the
// lock). Nothing else writes the spill file while it runs: Append does not
// spill during a drain, and Close waits for it.
func (p *Pipeline) flushSpill(ctx context.Context) (sent, sentBytes int, done bool, err error) {
	data, err := os.ReadFile(p.spill)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, true, nil
		}
		return 0, 0, false, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	type spilled struct {
		line int
		e    entry
	}
	var order []string
	byJob := map[string][]spilled{}
	parsed := make([]bool, len(lines))
	for i, line := range lines {
		if line == "" {
			continue
		}
		var e entry
		if json.Unmarshal([]byte(line), &e) != nil {
			continue // unparseable line: drop it rather than wedge the pipe
		}
		parsed[i] = true
		key := queueKey(e.RunID, e.JobID)
		if _, seen := byJob[key]; !seen {
			order = append(order, key)
		}
		byJob[key] = append(byJob[key], spilled{line: i, e: e})
	}
	isSent := make([]bool, len(lines))
	var uploadErr error
jobs:
	for _, key := range order {
		items := byJob[key]
		for len(items) > 0 {
			entries := make([]entry, len(items))
			for k, it := range items {
				entries[k] = it.e
			}
			batch := p.takeBatch(entries)
			n, upErr := p.uploadSome(ctx, batch)
			for k := 0; k < n; k++ {
				isSent[items[k].line] = true
				sent++
				sentBytes += items[k].e.size()
			}
			items = items[n:]
			if upErr != nil {
				uploadErr = upErr
				break jobs
			}
		}
	}
	if uploadErr != nil {
		// Persist the unsent remainder back to the spill file, in file order.
		var rest []string
		for i, line := range lines {
			if parsed[i] && !isSent[i] {
				rest = append(rest, line)
			}
		}
		remainder := strings.Join(rest, "\n")
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
// Retry-After when the error carries one, and arms the retry timer. mu held.
func (p *Pipeline) markFailedLocked(err error) {
	p.everFailed = true
	p.lastFailAt = p.now()
	p.retryNotBef = time.Time{}
	var ra retryAfterer
	if errors.As(err, &ra) {
		if wait := ra.RetryAfterHint(); wait > 0 {
			p.retryNotBef = p.now().Add(wait)
		}
	}
	p.armRetryLocked()
}

// armRetryLocked schedules one retry for when the backoff (or Retry-After)
// is over. A retry that fails again re-arms it; Close stops it. mu held.
func (p *Pipeline) armRetryLocked() {
	if p.retryTimer != nil || p.closing {
		return
	}
	delay := p.backoff
	if !p.retryNotBef.IsZero() {
		if d := p.retryNotBef.Sub(p.now()); d > delay {
			delay = d
		}
	}
	p.retryTimer = time.AfterFunc(delay, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.retryTimer = nil
		if p.closing {
			return
		}
		ctx := p.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		p.kick(ctx, true)
	})
}

// Drain waits, at most timeout, until every chunk queued for (runID, jobID)
// has been uploaded, and reports whether it was. A job drains its log before
// it reports :complete (saas-live-runs LR8): the server seals a finished
// job's log, and a reader that sees the job finished stops reading it, so a
// chunk that lands after either is a chunk nobody sees. It tries at once,
// whatever the backoff; after that a failed upload is retried by the retry
// timer. A server's Retry-After is still honoured.
func (p *Pipeline) Drain(ctx context.Context, runID, jobID string, timeout time.Duration) bool {
	if timeout <= 0 {
		return false
	}
	deadline := time.Now().Add(timeout)
	wake := time.AfterFunc(timeout, func() {
		p.mu.Lock()
		p.idle.Broadcast()
		p.mu.Unlock()
	})
	defer wake.Stop()
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		select {
		case <-ctx.Done():
			p.mu.Lock()
			p.idle.Broadcast()
			p.mu.Unlock()
		case <-stopWatch:
		}
	}()

	p.mu.Lock()
	defer p.mu.Unlock()
	key := queueKey(runID, jobID)
	kicked := false
	for {
		q := p.queues[key]
		if (q == nil || (len(q.entries) == 0 && !q.running)) && p.spilledCount == 0 && !p.draining {
			return true
		}
		if p.closing || ctx.Err() != nil || !time.Now().Before(deadline) {
			return false
		}
		if !kicked {
			p.kick(ctx, true)
			kicked = true
		}
		p.idle.Wait()
	}
}
