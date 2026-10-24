package main

import (
	"context"
	"sync"
	"time"
)

// jobLeases is the per-job remote-state lease bookkeeping of one `orun run`.
// A lane runs several jobs in one process, so every transition is
// mutex-guarded.
type jobLeases struct {
	mu   sync.Mutex
	jobs map[string]*jobLease
	// leaseTTL is the lease length the server granted on the latest claim or
	// heartbeat (never a client-hardcoded value). It is the run's measure of
	// a dead runner: an upstream whose heartbeat is older than two leases
	// has lost its runner (specs/orun-runner-efficiency §5.6).
	leaseTTL time.Duration
}

// jobLease holds one job's lease state.
type jobLease struct {
	// heartbeatInterval is the cadence the server granted on claim (never a
	// client-hardcoded value); zero falls back to defaultHeartbeatInterval.
	heartbeatInterval time.Duration
	// stopBeat ends the job's heartbeat.
	stopBeat context.CancelFunc
	// preempted is set when a heartbeat observes the lease was lost while the
	// job was still live.
	preempted bool
	// done is set once the job reported a terminal state.
	done bool
}

func newJobLeases() *jobLeases { return &jobLeases{jobs: map[string]*jobLease{}} }

func (l *jobLeases) get(jobID string) *jobLease {
	j := l.jobs[jobID]
	if j == nil {
		j = &jobLease{}
		l.jobs[jobID] = j
	}
	return j
}

// observeLease records a server-granted lease length.
func (l *jobLeases) observeLease(seconds int) {
	if seconds <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.leaseTTL = time.Duration(seconds) * time.Second
}

// staleAfter is how old a running job's heartbeat may be before the job is
// taken to have lost its runner: two of the server's leases, or, before any
// claim told us the lease, the legacy coordinator's five minutes.
func (l *jobLeases) staleAfter() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.leaseTTL > 0 {
		return 2 * l.leaseTTL
	}
	return defaultStaleAfter
}

// defaultStaleAfter matches the legacy coordinator's HEARTBEAT_TIMEOUT_MS.
const defaultStaleAfter = 5 * time.Minute

func (l *jobLeases) setInterval(jobID string, d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.get(jobID).heartbeatInterval = d
}

// start returns the context the job's heartbeat runs under (ended by terminal
// or by jobCtx) and the interval to beat at.
func (l *jobLeases) start(jobID string, jobCtx context.Context) (context.Context, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	j := l.get(jobID)
	ctx, cancel := context.WithCancel(jobCtx)
	j.stopBeat = cancel
	j.done = false
	j.preempted = false
	return ctx, j.heartbeatInterval
}

// lost records a heartbeat's lease_lost. It reports whether the loss is real:
// a job that already reported terminal gets 409 on every later beat, and that
// is not a takeover.
func (l *jobLeases) lost(jobID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	j := l.get(jobID)
	if j.done {
		return false
	}
	j.preempted = true
	return true
}

// terminal stops the job's heartbeat and marks it done. It reports whether the
// lease was lost before that, in which case the terminal update must not be
// sent.
func (l *jobLeases) terminal(jobID string) (preempted bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	j := l.get(jobID)
	j.done = true
	if j.stopBeat != nil {
		j.stopBeat()
	}
	return j.preempted
}
