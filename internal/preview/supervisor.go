package preview

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Event is one preview_changed payload.
type Event struct {
	State     string
	Port      int
	Component string
	Path      string
	Detail    string
}

// Payload is the event's session-event payload; empty fields are left out.
func (e Event) Payload() map[string]any {
	p := map[string]any{"state": e.State}
	if e.Port > 0 {
		p["port"] = e.Port
	}
	if e.Component != "" {
		p["component"] = e.Component
	}
	if e.Path != "" {
		p["path"] = e.Path
	}
	if e.Detail != "" {
		p["detail"] = e.Detail
	}
	return p
}

// Supervisor keeps a checkout's declared preview running for a coding session
// (PB-12). It re-reads the declaration every DetectEvery and whenever poked
// (a turn ended), so a preview that lands mid-session starts once it does;
// it restarts a preview that exits, with backoff, and gives up after
// MaxTries failed starts in a row until the declaration changes or a turn
// ends. Each change is reported through Emit, once. A preview someone
// stopped by hand stays stopped until the next turn ends. When Run's context
// ends, it stops the preview and reports `stopped`.
type Supervisor struct {
	Root        string
	Emit        func(Event)
	Logf        func(format string, args ...any)
	DetectEvery time.Duration // default 30s
	CheckEvery  time.Duration // default 5s
	Timeout     time.Duration // per start, per component; default DefaultTimeout
	MaxTries    int           // default 3
	Backoff     time.Duration // first retry delay, doubling; default 5s

	ops   ops
	clock func() time.Time
	poke  chan struct{}
	done  chan struct{}
}

// ops is what the supervisor does to the checkout; tests swap it.
type ops interface {
	detect(root string) (*Plan, error)
	start(ctx context.Context, p *Plan, timeout time.Duration) (State, error)
	status(root string) (State, error)
	stop(root string) (State, error)
}

type realOps struct{}

func (realOps) detect(root string) (*Plan, error) { return Detect(root) }
func (realOps) start(ctx context.Context, p *Plan, timeout time.Duration) (State, error) {
	return Start(ctx, p, Options{Timeout: timeout})
}
func (realOps) status(root string) (State, error) { return Status(root) }
func (realOps) stop(root string) (State, error)   { return Stop(root) }

// NewSupervisor supervises the checkout at root, reporting through emit.
func NewSupervisor(root string, emit func(Event)) *Supervisor {
	return &Supervisor{Root: root, Emit: emit, ops: realOps{}, clock: time.Now, poke: make(chan struct{}, 1), done: make(chan struct{})}
}

// Poke asks for a re-check now: a turn ended. It never blocks.
func (s *Supervisor) Poke() {
	select {
	case s.poke <- struct{}{}:
	default:
	}
}

// Wait blocks until Run has returned, for at most d.
func (s *Supervisor) Wait(d time.Duration) {
	select {
	case <-s.done:
	case <-time.After(d):
	}
}

// loop is Run's state between steps.
type loop struct {
	last       Event     // last emitted
	running    bool      // a preview this supervisor started is up
	plan       *Plan     // the current declaration
	planFP     string    // its fingerprint
	runningFP  string    // the fingerprint of what runs
	tries      int       // failed starts in a row
	retryAt    time.Time // no start before then
	gaveUp     bool      // MaxTries reached
	held       bool      // stopped by hand
	silentAt   time.Time // when a running preview stopped answering
	nextDetect time.Time
	detectErr  string // last unreadable-declaration error logged
}

// Run supervises until ctx ends, then stops the preview.
func (s *Supervisor) Run(ctx context.Context) {
	defer close(s.done)
	l := &loop{}
	for {
		now := s.clock()
		poked := false
		wait := s.checkEvery()
		if !l.retryAt.IsZero() && l.retryAt.After(now) && l.retryAt.Sub(now) < wait {
			wait = l.retryAt.Sub(now)
		}
		if l.nextDetect.IsZero() {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			s.shutdown(l)
			return
		case <-s.poke:
			poked = true
		case <-timer.C:
		}
		timer.Stop()
		s.step(ctx, l, s.clock(), poked)
	}
}

// step is one pass: re-read the declaration when due, check what runs, and
// start what should be running.
func (s *Supervisor) step(ctx context.Context, l *loop, now time.Time, poked bool) {
	if poked {
		l.held, l.gaveUp, l.tries, l.retryAt = false, false, 0, time.Time{}
	}
	if poked || !now.Before(l.nextDetect) {
		l.nextDetect = now.Add(s.detectEvery())
		if !s.redetect(l) {
			return
		}
	}
	if l.plan == nil {
		return
	}
	if l.running {
		s.check(l, now)
		if l.running {
			return
		}
	}
	if l.held || l.gaveUp || now.Before(l.retryAt) || ctx.Err() != nil {
		return
	}
	s.start(ctx, l)
}

// redetect re-reads the declaration; false when there is nothing to run.
func (s *Supervisor) redetect(l *loop) bool {
	p, err := s.ops.detect(s.Root)
	switch {
	case errors.Is(err, ErrNotDeclared):
		if l.running {
			s.logf("no preview declared any more (%v); stopping it", err)
			s.stopRunning(l)
		}
		if l.last.State != "" && l.last.State != StateStopped {
			s.emit(l, s.eventFor(l, StateStopped, ""))
		}
		l.plan, l.planFP = nil, ""
		return false
	case err != nil:
		msg := oneLine(err.Error())
		if l.running {
			// The agent may be mid-edit: keep what runs.
			if msg != l.detectErr {
				s.logf("the declaration does not read (%s); keeping the running preview", msg)
			}
			l.detectErr = msg
			return true
		}
		l.detectErr = msg
		l.plan, l.planFP = nil, ""
		s.emit(l, Event{State: StateFailed, Detail: msg})
		return false
	}
	l.detectErr = ""
	fp := p.Fingerprint()
	if fp != l.planFP {
		// A new declaration deserves its own tries.
		l.tries, l.gaveUp, l.retryAt = 0, false, time.Time{}
	}
	l.plan, l.planFP = p, fp
	if l.running && fp != l.runningFP {
		s.logf("the declaration changed; restarting the preview")
		s.stopRunning(l)
		l.held = false
	}
	return true
}

// check folds the running preview's status.
func (s *Supervisor) check(l *loop, now time.Time) {
	st, err := s.ops.status(s.Root)
	switch {
	case err == nil && st.State == StateReady:
		l.silentAt = time.Time{}
		l.tries = 0
		s.emit(l, s.eventFor(l, StateReady, ""))
	case err == nil && st.State == StateStarting:
		if l.silentAt.IsZero() {
			l.silentAt = now
		}
		if now.Sub(l.silentAt) < s.timeout() {
			s.emit(l, s.eventFor(l, StateStarting, ""))
			return
		}
		s.logf("%s; restarting it", st.Detail)
		s.stopRunning(l)
		s.failedStart(l, now, st.Detail)
	case err == nil && st.State == StateStopped:
		s.logf("the preview was stopped by hand; leaving it until the next turn ends")
		l.running, l.held = false, true
		s.emit(l, s.eventFor(l, StateStopped, ""))
	default:
		detail := st.Detail
		if err != nil {
			detail = err.Error()
		}
		s.logf("the preview exited: %s", oneLine(detail))
		s.stopRunning(l)
		s.failedStart(l, now, detail)
	}
}

// start launches the declared preview and waits for it.
func (s *Supervisor) start(ctx context.Context, l *loop) {
	t := l.plan.Target()
	s.logf("starting %s on :%d (%d process(es))", t.Name, t.Port, len(l.plan.Components))
	s.emit(l, s.eventFor(l, StateStarting, ""))
	st, err := s.ops.start(ctx, l.plan, s.timeout())
	if ctx.Err() != nil {
		return
	}
	if err == nil && st.State == StateReady {
		l.running, l.runningFP = true, l.planFP
		l.tries, l.silentAt = 0, time.Time{}
		s.logf("%s is ready on :%d", t.Name, t.Port)
		s.emit(l, s.eventFor(l, StateReady, ""))
		return
	}
	detail := st.Detail
	if detail == "" && err != nil {
		detail = err.Error()
	}
	s.failedStart(l, s.clock(), detail)
}

// failedStart counts one failed start: retry after a backoff, or give up.
func (s *Supervisor) failedStart(l *loop, now time.Time, detail string) {
	l.tries++
	if l.tries >= s.maxTries() {
		l.gaveUp = true
		s.logf("giving up after %d tries in a row: %s", l.tries, oneLine(detail))
		s.emit(l, s.eventFor(l, StateFailed, oneLine(detail)))
		return
	}
	delay := s.backoff() << (l.tries - 1)
	l.retryAt = now.Add(delay)
	s.logf("start failed (try %d of %d): %s; retrying in %s", l.tries, s.maxTries(), oneLine(detail), delay)
}

func (s *Supervisor) stopRunning(l *loop) {
	if _, err := s.ops.stop(s.Root); err != nil && !errors.Is(err, ErrNoState) {
		s.logf("stop: %v", err)
	}
	l.running, l.silentAt = false, time.Time{}
}

// shutdown stops the preview as the session ends. It says so first: the
// session log closes soon after, and a slow server must not keep the event
// out of it.
func (s *Supervisor) shutdown(l *loop) {
	wasUp := l.running || l.last.State == StateStarting || l.last.State == StateReady
	if l.last.State != "" && l.last.State != StateStopped {
		s.emit(l, s.eventFor(l, StateStopped, ""))
	}
	if wasUp {
		s.stopRunning(l)
	}
}

// eventFor is an event about the current declaration.
func (s *Supervisor) eventFor(l *loop, state, detail string) Event {
	e := Event{State: state, Detail: detail}
	if l.plan != nil {
		t := l.plan.Target()
		e.Component, e.Port, e.Path = t.Name, t.Port, t.Path
	} else if state == StateStopped {
		e.Component, e.Port, e.Path = l.last.Component, l.last.Port, l.last.Path
	}
	return e
}

// emit reports e when it differs from the last report.
func (s *Supervisor) emit(l *loop, e Event) {
	if e == l.last {
		return
	}
	l.last = e
	if s.Emit != nil {
		s.Emit(e)
	}
}

func (s *Supervisor) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

func (s *Supervisor) detectEvery() time.Duration { return orDefault(s.DetectEvery, 30*time.Second) }
func (s *Supervisor) checkEvery() time.Duration  { return orDefault(s.CheckEvery, 5*time.Second) }
func (s *Supervisor) timeout() time.Duration     { return orDefault(s.Timeout, DefaultTimeout) }
func (s *Supervisor) backoff() time.Duration     { return orDefault(s.Backoff, 5*time.Second) }

func (s *Supervisor) maxTries() int {
	if s.MaxTries > 0 {
		return s.MaxTries
	}
	return 3
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// oneLine keeps the first line of a message, bounded for an event.
func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
