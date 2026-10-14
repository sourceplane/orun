package runner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
)

// countingExecutor counts runs per command and records each run's work dir.
type countingExecutor struct {
	mu   sync.Mutex
	runs map[string]int
	dirs map[string][]string
	fail map[string]bool
}

func newCountingExecutor() *countingExecutor {
	return &countingExecutor{runs: map[string]int{}, dirs: map[string][]string{}}
}

func (*countingExecutor) Name() string                       { return "counting" }
func (*countingExecutor) Prepare(executor.ExecContext) error { return nil }
func (*countingExecutor) Cleanup(executor.ExecContext) error { return nil }
func (e *countingExecutor) RunStep(ctx executor.ExecContext, _ model.PlanJob, step model.PlanStep) (string, error) {
	e.mu.Lock()
	e.runs[step.Run]++
	e.dirs[step.Run] = append(e.dirs[step.Run], ctx.WorkDir)
	e.mu.Unlock()
	if e.fail[step.Run] {
		return "boom", fmt.Errorf("failed")
	}
	return "ok: " + step.Run, nil
}

func laneStepJob(id, component, path string) model.PlanJob {
	return model.PlanJob{ID: id, Name: id, Component: component, Environment: "stage", Path: path, Steps: []model.PlanStep{
		{ID: "install", Name: "install", Scope: model.StepScopeLane, Run: "pnpm install"},
		{ID: "build", Name: "build", Run: "pnpm build " + component},
	}}
}

func TestLaneSetup_RunsASharedStepOncePerLane(t *testing.T) {
	ex := newCountingExecutor()
	plan := &model.Plan{Jobs: []model.PlanJob{laneStepJob("a", "a", ""), laneStepJob("b", "b", ""), laneStepJob("c", "c", "")}}
	r := NewRunner(t.TempDir(), false, io.Discard, io.Discard, false, "", false, false,
		ex, executor.RuntimeContext{}, "exec_lane_setup", 3, nil, "")
	r.Isolation = IsolationNone
	if err := r.Run(plan); err != nil {
		t.Fatal(err)
	}
	if ex.runs["pnpm install"] != 1 {
		t.Fatalf("the lane-scoped install ran %d times; once for the three jobs", ex.runs["pnpm install"])
	}
	if ex.runs["pnpm build a"] != 1 || ex.runs["pnpm build b"] != 1 || ex.runs["pnpm build c"] != 1 {
		t.Fatalf("every job still runs its own steps: %v", ex.runs)
	}
	st := r.SnapshotState()
	for _, id := range []string{"a", "b", "c"} {
		if st.Jobs[id].Status != "completed" || st.Jobs[id].Steps["install"] != "completed" {
			t.Fatalf("job %s: %+v (the shared step is completed for every job)", id, st.Jobs[id])
		}
	}
}

func TestLaneSetup_DifferentWorkingDirsAreDifferentSteps(t *testing.T) {
	ex := newCountingExecutor()
	plan := &model.Plan{Jobs: []model.PlanJob{laneStepJob("web", "web", "apps/web"), laneStepJob("api", "api", "apps/api")}}
	root := t.TempDir()
	for _, d := range []string{"apps/web", "apps/api"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r := NewRunner(root, false, io.Discard, io.Discard, false, "", false, false,
		ex, executor.RuntimeContext{}, "exec_lane_setup", 2, nil, "")
	r.Isolation = IsolationNone
	if err := r.Run(plan); err != nil {
		t.Fatal(err)
	}
	if ex.runs["pnpm install"] != 2 {
		t.Fatalf("an install in apps/web and one in apps/api are two steps: ran %d", ex.runs["pnpm install"])
	}
}

func TestLaneSetup_SharedStepRunsInTheSourceTreeBeforeStaging(t *testing.T) {
	ex := newCountingExecutor()
	plan := &model.Plan{Jobs: []model.PlanJob{laneStepJob("a", "a", ""), laneStepJob("b", "b", "")}}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(root, false, io.Discard, io.Discard, false, "", false, false,
		ex, executor.RuntimeContext{}, "exec_lane_setup", 2, nil, "")
	r.Isolation = IsolationWorkspace
	if err := r.Run(plan); err != nil {
		t.Fatal(err)
	}
	if got := ex.dirs["pnpm install"]; len(got) != 1 || filepath.Clean(got[0]) != filepath.Clean(root) {
		t.Fatalf("the shared step runs once, in the source tree: %v", got)
	}
	for _, d := range append(ex.dirs["pnpm build a"], ex.dirs["pnpm build b"]...) {
		if !strings.Contains(d, filepath.Join(".orun", "runs", "exec_lane_setup")) {
			t.Fatalf("a job's own step runs in its staged tree: %s", d)
		}
	}
}

func TestLaneSetup_OwnersFailureFailsTheSharers(t *testing.T) {
	ex := newCountingExecutor()
	ex.fail = map[string]bool{"pnpm install": true}
	plan := &model.Plan{Jobs: []model.PlanJob{laneStepJob("a", "a", ""), laneStepJob("b", "b", "")}}
	r := NewRunner(t.TempDir(), false, io.Discard, io.Discard, false, "", false, false,
		ex, executor.RuntimeContext{}, "exec_lane_setup", 2, nil, "")
	r.Isolation = IsolationNone
	if err := r.Run(plan); err == nil {
		t.Fatal("a failed shared step fails the run")
	}
	if ex.runs["pnpm install"] != 1 || ex.runs["pnpm build a"] != 0 || ex.runs["pnpm build b"] != 0 {
		t.Fatalf("the install ran once and no build ran: %v", ex.runs)
	}
	st := r.SnapshotState()
	failed := 0
	for _, id := range []string{"a", "b"} {
		if st.Jobs[id].Status == "failed" {
			failed++
		}
		if strings.Contains(st.Jobs[id].LastError, "lane step failed in") && !strings.Contains(st.Jobs[id].LastError, "failed in a") && !strings.Contains(st.Jobs[id].LastError, "failed in b") {
			t.Fatalf("the sharer names the owner: %q", st.Jobs[id].LastError)
		}
	}
	if failed != 2 {
		t.Fatalf("both jobs fail: %+v %+v", st.Jobs["a"], st.Jobs["b"])
	}
}

func TestLeadingLaneSteps(t *testing.T) {
	job := model.PlanJob{Steps: []model.PlanStep{{ID: "a", Scope: "lane"}, {ID: "b", Scope: "lane"}, {ID: "c"}, {ID: "d", Scope: "lane"}}}
	if n := leadingLaneSteps(job); n != 2 {
		t.Fatalf("leading lane steps = %d", n)
	}
}
