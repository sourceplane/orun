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
