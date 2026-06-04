package runner

import (
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
)

// recordingExecutor records every (job, step) it actually runs.
type recordingExecutor struct {
	mu  sync.Mutex
	ran []string
}

func (*recordingExecutor) Name() string                       { return "rec" }
func (*recordingExecutor) Prepare(executor.ExecContext) error { return nil }
func (*recordingExecutor) Cleanup(executor.ExecContext) error { return nil }
func (e *recordingExecutor) RunStep(_ executor.ExecContext, job model.PlanJob, step model.PlanStep) (string, error) {
	e.mu.Lock()
	e.ran = append(e.ran, job.ID+"/"+step.ID)
	e.mu.Unlock()
	return "ok", nil
}

// TestResumeSkipsCompletedJobs verifies the cross-run resume seed: a job seeded
// as already-succeeded is skipped (never executed) and counted complete, while
// the unfinished job still runs.
func TestResumeSkipsCompletedJobs(t *testing.T) {
	plan := &model.Plan{Jobs: []model.PlanJob{
		{ID: "a@deploy", Name: "a", Component: "a", Steps: []model.PlanStep{{ID: "build", Name: "build"}}},
		{ID: "b@deploy", Name: "b", Component: "b", Steps: []model.PlanStep{{ID: "build", Name: "build"}}},
	}}
	exec := &recordingExecutor{}
	r := NewRunner(t.TempDir(), false, io.Discard, io.Discard, false, "", false, false,
		exec, executor.RuntimeContext{}, "exec_resume", 1, nil, "")

	// Seed job a as already succeeded in a prior run.
	r.ResumeJobs = map[string]*execmodel.JobState{
		"a@deploy": {Status: "completed", Steps: map[string]string{"build": "completed"}},
	}

	if err := r.Run(plan); err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, s := range exec.ran {
		if strings.HasPrefix(s, "a@deploy/") {
			t.Fatalf("resumed job a@deploy was re-executed: ran=%v", exec.ran)
		}
	}
	ranB := false
	for _, s := range exec.ran {
		if s == "b@deploy/build" {
			ranB = true
		}
	}
	if !ranB {
		t.Fatalf("job b@deploy did not run: ran=%v", exec.ran)
	}

	// Final state: both jobs complete (a from the seed, b from the run).
	snap := r.SnapshotState()
	if snap == nil || snap.Jobs["a@deploy"] == nil || snap.Jobs["a@deploy"].Status != "completed" {
		t.Fatalf("resumed job a@deploy not completed: %+v", snap)
	}
	if snap.Jobs["b@deploy"] == nil || snap.Jobs["b@deploy"].Status != "completed" {
		t.Fatalf("job b@deploy not completed: %+v", snap.Jobs["b@deploy"])
	}
}
