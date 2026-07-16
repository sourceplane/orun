package runner

import (
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
)

type stepFailExecutor struct{ failStep string }

func (stepFailExecutor) Name() string                       { return "fake" }
func (stepFailExecutor) Prepare(executor.ExecContext) error { return nil }
func (stepFailExecutor) Cleanup(executor.ExecContext) error { return nil }
func (e stepFailExecutor) RunStep(_ executor.ExecContext, _ model.PlanJob, s model.PlanStep) (string, error) {
	if s.ID == e.failStep {
		return "boom", fmt.Errorf("step exploded")
	}
	return "ok", nil
}

// stepEventLog records the OnStepStart/AfterStepTerminal stream.
type stepEventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *stepEventLog) add(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, fmt.Sprintf(format, args...))
}

func (l *stepEventLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

func hookedRunner(t *testing.T, ex executor.Executor, dryRun bool) (*Runner, *stepEventLog) {
	t.Helper()
	r := NewRunner(t.TempDir(), false, io.Discard, io.Discard, dryRun, "", false, false,
		ex, executor.RuntimeContext{}, "exec_stephooks", 1, nil, "")
	log := &stepEventLog{}
	r.Hooks = &RunnerHooks{
		OnStepStart: func(jobID, stepID string, index, total int) {
			log.add("start %s/%s %d/%d", jobID, stepID, index, total)
		},
		AfterStepTerminal: func(jobID, stepID, status string) {
			log.add("done %s/%s %s", jobID, stepID, status)
		},
	}
	return r, log
}

// TestStepHooksHappyPath pins the start→terminal stream for a multi-step
// job — the events the cockpit's live view renders instead of re-reading
// the working tree (specs/orun-tui-v2 §8).
func TestStepHooksHappyPath(t *testing.T) {
	plan := &model.Plan{Jobs: []model.PlanJob{{
		ID: "a@deploy", Name: "a", Component: "a",
		Steps: []model.PlanStep{{ID: "build", Name: "build"}, {ID: "test", Name: "test"}},
	}}}
	r, log := hookedRunner(t, okExecutor{}, false)
	if err := r.Run(plan); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := []string{
		"start a@deploy/build 1/2",
		"done a@deploy/build completed",
		"start a@deploy/test 2/2",
		"done a@deploy/test completed",
	}
	got := log.all()
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d = %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

// TestStepHooksFailure: a failing step emits terminal "failed" and later
// steps never start.
func TestStepHooksFailure(t *testing.T) {
	plan := &model.Plan{Jobs: []model.PlanJob{{
		ID: "a@deploy", Name: "a", Component: "a",
		Steps: []model.PlanStep{{ID: "build", Name: "build"}, {ID: "test", Name: "test"}},
	}}}
	r, log := hookedRunner(t, stepFailExecutor{failStep: "build"}, false)
	_ = r.Run(plan) // the run fails; the event stream is what we assert
	want := []string{
		"start a@deploy/build 1/2",
		"done a@deploy/build failed",
	}
	got := log.all()
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestStepHooksDryRun: dry-run steps start and complete immediately, so a
// live view renders the same shape as a real run.
func TestStepHooksDryRun(t *testing.T) {
	plan := &model.Plan{Jobs: []model.PlanJob{{
		ID: "a@deploy", Name: "a", Component: "a",
		Steps: []model.PlanStep{{ID: "build", Name: "build"}},
	}}}
	r, log := hookedRunner(t, okExecutor{}, true)
	if err := r.Run(plan); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := []string{
		"start a@deploy/build 1/1",
		"done a@deploy/build completed",
	}
	got := log.all()
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

// TestStepHooksNilSafe: a runner with no hooks (or partial hooks) must run
// exactly as before.
func TestStepHooksNilSafe(t *testing.T) {
	plan := &model.Plan{Jobs: []model.PlanJob{{
		ID: "a@deploy", Name: "a", Component: "a",
		Steps: []model.PlanStep{{ID: "build", Name: "build"}},
	}}}
	r := NewRunner(t.TempDir(), false, io.Discard, io.Discard, false, "", false, false,
		okExecutor{}, executor.RuntimeContext{}, "exec_stephooks_nil", 1, nil, "")
	r.Hooks = &RunnerHooks{} // present but empty
	if err := r.Run(plan); err != nil {
		t.Fatalf("run with empty hooks: %v", err)
	}
	r.Hooks = nil
	if err := r.Run(plan); err != nil {
		t.Fatalf("run with nil hooks: %v", err)
	}
}
