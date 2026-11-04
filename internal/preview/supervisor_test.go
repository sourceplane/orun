package preview

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeOps scripts what the checkout holds and how its preview behaves.
type fakeOps struct {
	mu        sync.Mutex
	plan      *Plan
	detectErr error
	started   State
	startErr  error
	health    State
	statusErr error
	starts    int
	stops     int
}

func (f *fakeOps) detect(string) (*Plan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.plan, f.detectErr
}

func (f *fakeOps) start(context.Context, *Plan, time.Duration) (State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	return f.started, f.startErr
}

func (f *fakeOps) status(string) (State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.health, f.statusErr
}

func (f *fakeOps) stop(string) (State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	return State{State: StateStopped}, nil
}

func (f *fakeOps) set(fn func(f *fakeOps)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) emit(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

// states renders the events as "state[:port][ detail]", comma separated.
func (r *recorder) states() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.events {
		s := e.State
		if e.Port > 0 {
			s += fmt.Sprintf(":%d", e.Port)
		}
		if e.Detail != "" {
			s += " " + e.Detail
		}
		out = append(out, s)
	}
	return strings.Join(out, ",")
}

func webPlan(port int) *Plan {
	return &Plan{Root: "/repo", Components: []Component{{Name: "web", Run: []string{"vite"}, Port: port, Path: "/"}}}
}

func newTestSupervisor(f *fakeOps) (*Supervisor, *recorder) {
	r := &recorder{}
	s := NewSupervisor("/repo", r.emit)
	s.ops = f
	s.DetectEvery = 30 * time.Second
	s.Backoff = 5 * time.Second
	s.Timeout = 60 * time.Second
	s.MaxTries = 3
	return s, r
}

var notYet = fmt.Errorf("%w: no intent.yaml", ErrNotDeclared)

func TestSupervisorStartsOnceDeclaredAndRestarts(t *testing.T) {
	ctx := context.Background()
	f := &fakeOps{detectErr: notYet}
	s, r := newTestSupervisor(f)
	l := &loop{}
	now := time.Unix(1_000_000, 0)
	s.clock = func() time.Time { return now }

	// No code yet: nothing runs, nothing is said.
	s.step(ctx, l, now, false)
	if r.states() != "" || f.starts != 0 {
		t.Fatalf("before the declaration: %q, %d starts", r.states(), f.starts)
	}
	// Before the next detection the checkout is not read again.
	f.set(func(f *fakeOps) { f.detectErr, f.plan, f.started = nil, webPlan(4173), State{State: StateReady} })
	s.step(ctx, l, now.Add(5*time.Second), false)
	if f.starts != 0 {
		t.Fatal("re-read the declaration before DetectEvery")
	}
	now = now.Add(31 * time.Second)
	s.step(ctx, l, now, false)
	if got := r.states(); got != "starting:4173,ready:4173" {
		t.Fatalf("events = %q", got)
	}
	// Healthy checks say nothing new.
	f.set(func(f *fakeOps) { f.health = State{State: StateReady} })
	for i := 0; i < 3; i++ {
		now = now.Add(5 * time.Second)
		s.step(ctx, l, now, false)
	}
	if got := r.states(); got != "starting:4173,ready:4173" || f.starts != 1 {
		t.Fatalf("events = %q, starts %d", got, f.starts)
	}

	// The server exits: it is restarted after the backoff.
	f.set(func(f *fakeOps) { f.health = State{State: StateFailed, Detail: "web exited"} })
	now = now.Add(5 * time.Second)
	s.step(ctx, l, now, false)
	if f.starts != 1 || f.stops != 1 {
		t.Fatalf("restarted before the backoff: starts %d stops %d", f.starts, f.stops)
	}
	now = now.Add(5 * time.Second)
	s.step(ctx, l, now, false)
	if got := r.states(); got != "starting:4173,ready:4173,starting:4173,ready:4173" || f.starts != 2 {
		t.Fatalf("events = %q, starts %d", got, f.starts)
	}
}

func TestSupervisorGivesUpAndRetriesAfterATurn(t *testing.T) {
	ctx := context.Background()
	f := &fakeOps{plan: webPlan(4173), started: State{State: StateFailed, Detail: "web exited (exit status 1): boom"}, startErr: errors.New("boom")}
	s, r := newTestSupervisor(f)
	l := &loop{}
	now := time.Unix(1_000_000, 0)
	s.clock = func() time.Time { return now }

	s.step(ctx, l, now, false) // try 1
	now = now.Add(5 * time.Second)
	s.step(ctx, l, now, false) // try 2
	now = now.Add(9 * time.Second)
	s.step(ctx, l, now, false) // backoff doubled: not yet
	if f.starts != 2 {
		t.Fatalf("starts = %d, want 2 (backoff doubles)", f.starts)
	}
	now = now.Add(time.Second)
	s.step(ctx, l, now, false) // try 3: give up
	if got := r.states(); got != "starting:4173,failed:4173 web exited (exit status 1): boom" {
		t.Fatalf("events = %q", got)
	}
	// Given up: neither time nor a re-read of the same declaration retries.
	for i := 0; i < 10; i++ {
		now = now.Add(30 * time.Second)
		s.step(ctx, l, now, false)
	}
	if f.starts != 3 {
		t.Fatalf("starts after giving up = %d", f.starts)
	}
	// A turn ended: try again, and this time it comes up.
	f.set(func(f *fakeOps) { f.started, f.startErr = State{State: StateReady}, nil })
	s.step(ctx, l, now, true)
	if got := r.states(); !strings.HasSuffix(got, ",starting:4173,ready:4173") || f.starts != 4 {
		t.Fatalf("after a turn: %q, starts %d", got, f.starts)
	}
}

func TestSupervisorNewDeclarationAfterGivingUp(t *testing.T) {
	ctx := context.Background()
	f := &fakeOps{plan: webPlan(4173), started: State{State: StateFailed, Detail: "port 4173 is already in use, not by this preview"}}
	s, r := newTestSupervisor(f)
	s.MaxTries = 1
	l := &loop{}
	now := time.Unix(1_000_000, 0)
	s.clock = func() time.Time { return now }
	s.step(ctx, l, now, false)
	if got := r.states(); got != "starting:4173,failed:4173 port 4173 is already in use, not by this preview" {
		t.Fatalf("events = %q", got)
	}
	// The agent moves the port: the new declaration gets its own tries.
	f.set(func(f *fakeOps) { f.plan, f.started = webPlan(4174), State{State: StateReady} })
	s.step(ctx, l, now.Add(31*time.Second), false)
	if got := r.states(); !strings.HasSuffix(got, ",starting:4174,ready:4174") {
		t.Fatalf("events = %q", got)
	}
}

func TestSupervisorRestartsOnAChangedDeclaration(t *testing.T) {
	ctx := context.Background()
	f := &fakeOps{plan: webPlan(4173), started: State{State: StateReady}, health: State{State: StateReady}}
	s, r := newTestSupervisor(f)
	l := &loop{}
	now := time.Unix(1_000_000, 0)
	s.clock = func() time.Time { return now }
	s.step(ctx, l, now, false)
	f.set(func(f *fakeOps) { f.plan = webPlan(5173) })
	s.step(ctx, l, now.Add(31*time.Second), false)
	if got := r.states(); got != "starting:4173,ready:4173,starting:5173,ready:5173" || f.stops != 1 {
		t.Fatalf("events = %q, stops %d", got, f.stops)
	}
}

func TestSupervisorKeepsRunningThroughAnUnreadableDeclaration(t *testing.T) {
	ctx := context.Background()
	f := &fakeOps{plan: webPlan(4173), started: State{State: StateReady}, health: State{State: StateReady}}
	s, r := newTestSupervisor(f)
	l := &loop{}
	now := time.Unix(1_000_000, 0)
	s.clock = func() time.Time { return now }
	s.step(ctx, l, now, false)
	// The agent is mid-edit of component.yaml.
	f.set(func(f *fakeOps) {
		f.plan, f.detectErr = nil, errors.New("failed to parse component manifest apps/web/component.yaml: yaml: line 3")
	})
	s.step(ctx, l, now.Add(31*time.Second), false)
	if got := r.states(); got != "starting:4173,ready:4173" || f.stops != 0 {
		t.Fatalf("events = %q, stops %d", got, f.stops)
	}
}

func TestSupervisorReportsABadDeclaration(t *testing.T) {
	ctx := context.Background()
	f := &fakeOps{detectErr: errors.New("component web: spec.preview.port must be an integer from 1024 to 65535\nmore")}
	s, r := newTestSupervisor(f)
	l := &loop{}
	now := time.Unix(1_000_000, 0)
	s.clock = func() time.Time { return now }
	s.step(ctx, l, now, false)
	s.step(ctx, l, now.Add(31*time.Second), true)
	if got := r.states(); got != "failed component web: spec.preview.port must be an integer from 1024 to 65535" || f.starts != 0 {
		t.Fatalf("events = %q, starts %d", got, f.starts)
	}
}

func TestSupervisorLeavesAHandStoppedPreview(t *testing.T) {
	ctx := context.Background()
	f := &fakeOps{plan: webPlan(4173), started: State{State: StateReady}, health: State{State: StateReady}}
	s, r := newTestSupervisor(f)
	l := &loop{}
	now := time.Unix(1_000_000, 0)
	s.clock = func() time.Time { return now }
	s.step(ctx, l, now, false)
	f.set(func(f *fakeOps) { f.health = State{State: StateStopped} })
	for i := 1; i <= 3; i++ {
		s.step(ctx, l, now.Add(time.Duration(i)*5*time.Second), false)
	}
	if got := r.states(); got != "starting:4173,ready:4173,stopped:4173" || f.starts != 1 {
		t.Fatalf("events = %q, starts %d", got, f.starts)
	}
	s.step(ctx, l, now.Add(20*time.Second), true)
	if got := r.states(); got != "starting:4173,ready:4173,stopped:4173,starting:4173,ready:4173" {
		t.Fatalf("after a turn: %q", got)
	}
}

func TestSupervisorRestartsASilentServer(t *testing.T) {
	ctx := context.Background()
	f := &fakeOps{plan: webPlan(4173), started: State{State: StateReady}, health: State{State: StateStarting, Detail: "web is running but 127.0.0.1:4173 does not answer"}}
	s, r := newTestSupervisor(f)
	l := &loop{}
	now := time.Unix(1_000_000, 0)
	s.clock = func() time.Time { return now }
	s.step(ctx, l, now, false)
	s.step(ctx, l, now.Add(5*time.Second), false)
	if got := r.states(); got != "starting:4173,ready:4173,starting:4173" || f.stops != 0 {
		t.Fatalf("a short silence: %q, stops %d", got, f.stops)
	}
	s.step(ctx, l, now.Add(70*time.Second), false) // silent past Timeout
	s.step(ctx, l, now.Add(76*time.Second), false) // after the backoff
	if f.stops != 1 || f.starts != 2 {
		t.Fatalf("stops %d starts %d, want a restart", f.stops, f.starts)
	}
}

func TestSupervisorStopsWhenTheDeclarationGoes(t *testing.T) {
	ctx := context.Background()
	f := &fakeOps{plan: webPlan(4173), started: State{State: StateReady}, health: State{State: StateReady}}
	s, r := newTestSupervisor(f)
	l := &loop{}
	now := time.Unix(1_000_000, 0)
	s.clock = func() time.Time { return now }
	s.step(ctx, l, now, false)
	f.set(func(f *fakeOps) { f.plan, f.detectErr = nil, notYet })
	s.step(ctx, l, now.Add(31*time.Second), false)
	if got := r.states(); got != "starting:4173,ready:4173,stopped:4173" || f.stops != 1 {
		t.Fatalf("events = %q, stops %d", got, f.stops)
	}
}

// Run end to end on fake ops: started, then stopped with the session.
func TestSupervisorRunStopsWithTheSession(t *testing.T) {
	f := &fakeOps{plan: webPlan(4173), started: State{State: StateReady}, health: State{State: StateReady}}
	s, r := newTestSupervisor(f)
	s.CheckEvery = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for r.states() != "starting:4173,ready:4173" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	s.Poke() // never blocks, even twice
	s.Poke()
	cancel()
	s.Wait(5 * time.Second)
	if got := r.states(); got != "starting:4173,ready:4173,stopped:4173" || f.stops != 1 {
		t.Fatalf("events = %q, stops %d", got, f.stops)
	}
	r.mu.Lock()
	last := r.events[len(r.events)-1]
	r.mu.Unlock()
	if last.Component != "web" || last.Path != "/" {
		t.Fatalf("stopped event = %+v", last)
	}
}

// A session with no preview says nothing, even as it ends.
func TestSupervisorRunSilentWithoutAPreview(t *testing.T) {
	f := &fakeOps{detectErr: notYet}
	s, r := newTestSupervisor(f)
	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)
	time.Sleep(20 * time.Millisecond)
	cancel()
	s.Wait(5 * time.Second)
	if got := r.states(); got != "" || f.stops != 0 {
		t.Fatalf("events = %q, stops %d", got, f.stops)
	}
}
