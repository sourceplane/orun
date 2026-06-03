package runner

import (
	"io"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
)

type okExecutor struct{}

func (okExecutor) Name() string                       { return "fake" }
func (okExecutor) Prepare(executor.ExecContext) error { return nil }
func (okExecutor) Cleanup(executor.ExecContext) error { return nil }
func (okExecutor) RunStep(executor.ExecContext, model.PlanJob, model.PlanStep) (string, error) {
	return "ok", nil
}

// TestAfterStateUpdateHookCanSnapshot is a regression test for the deadlock
// where AfterStateUpdate (fired by updateState) called back into the runner via
// SnapshotState, which re-acquires r.stateMu. Firing the hook under the lock
// self-deadlocked. The hook must run outside the lock.
func TestAfterStateUpdateHookCanSnapshot(t *testing.T) {
	plan := &model.Plan{Jobs: []model.PlanJob{
		{ID: "a@deploy", Name: "a", Component: "a", Steps: []model.PlanStep{{ID: "build", Name: "build"}}},
		{ID: "b@deploy", Name: "b", Component: "b", Steps: []model.PlanStep{{ID: "build", Name: "build"}}},
	}}

	r := NewRunner(t.TempDir(), false, io.Discard, io.Discard, false, "", false, false,
		okExecutor{}, executor.RuntimeContext{}, "exec_deadlock", 2, nil, "")

	hookCalls := 0
	r.Hooks = &RunnerHooks{
		AfterStateUpdate: func() {
			// Calls back into the runner under no lock — would deadlock if the
			// hook fired while updateState held r.stateMu.
			if snap := r.SnapshotState(); snap != nil {
				hookCalls++
			}
		},
	}

	done := make(chan error, 1)
	go func() { done <- r.Run(plan) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Run deadlocked: AfterStateUpdate -> SnapshotState under r.stateMu")
	}
	if hookCalls == 0 {
		t.Fatal("AfterStateUpdate never fired with a non-nil snapshot")
	}
}
