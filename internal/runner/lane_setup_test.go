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
	// Setup actions before the install are part of the prelude; the prelude
	// ends at the last leading lane-scoped step.
	withActions := model.PlanJob{Steps: []model.PlanStep{{ID: "node", Use: "actions/setup-node@v6"}, {ID: "pnpm", Use: "pnpm/action-setup@v4"}, {ID: "install", Scope: "lane"}, {ID: "build"}, {ID: "deploy"}}}
	if n := leadingLaneSteps(withActions); n != 3 {
		t.Fatalf("prelude with setup actions = %d, want 3", n)
	}
	// Actions alone, no lane step: no prelude — the job is staged first.
	actionsOnly := model.PlanJob{Steps: []model.PlanStep{{ID: "node", Use: "actions/setup-node@v6"}, {ID: "build"}}}
	if n := leadingLaneSteps(actionsOnly); n != 0 {
		t.Fatalf("prelude without a lane step = %d, want 0", n)
	}
}

// sharingExecutor is a countingExecutor that can hand a setup action's
// exports across jobs (executor.ExportSharer): every `use:` step exports
// TOOL=<use>, shareable unless the test says otherwise; imports are counted.
type sharingExecutor struct {
	*countingExecutor
	private bool
	imports map[string]int // job id → imported steps
}

func newSharingExecutor() *sharingExecutor {
	return &sharingExecutor{countingExecutor: newCountingExecutor(), imports: map[string]int{}}
}

func (e *sharingExecutor) RunStep(ctx executor.ExecContext, job model.PlanJob, step model.PlanStep) (string, error) {
	key := step.Run
	if step.Use != "" {
		key = step.Use
	}
	e.mu.Lock()
	e.runs[key]++
	e.dirs[key] = append(e.dirs[key], ctx.WorkDir)
	e.mu.Unlock()
	if e.fail[key] {
		return "boom", fmt.Errorf("failed")
	}
	return "ok: " + key, nil
}

func (e *sharingExecutor) LastStepExports(jobID string) (executor.StepExports, bool) {
	return executor.StepExports{Env: map[string]string{"TOOL": "node"}, Paths: []string{"/tools/node/bin"}, Outputs: map[string]string{"node-version": "22"}}, !e.private
}

func (e *sharingExecutor) ImportStepExports(_ executor.ExecContext, job model.PlanJob, step model.PlanStep, exports executor.StepExports) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if exports.Env["TOOL"] != "node" || len(exports.Paths) != 1 || exports.Outputs["node-version"] != "22" {
		return fmt.Errorf("job %s imported %+v", job.ID, exports)
	}
	e.imports[job.ID]++
	return nil
}

func setupActionJob(id string) model.PlanJob {
	return model.PlanJob{ID: id, Name: id, Component: id, Environment: "stage", Steps: []model.PlanStep{
		{ID: "node", Name: "node", Use: "actions/setup-node@v6", With: map[string]interface{}{"node-version": "22"}},
		{ID: "install", Name: "install", Scope: model.StepScopeLane, Run: "pnpm install"},
		{ID: "build", Name: "build", Run: "pnpm build " + id},
	}}
}

func TestLaneSetup_SetupActionRunsOncePerLaneAndItsExportsReachTheOtherJobs(t *testing.T) {
	ex := newSharingExecutor()
	plan := &model.Plan{Jobs: []model.PlanJob{setupActionJob("a"), setupActionJob("b"), setupActionJob("c")}}
	r := NewRunner(t.TempDir(), false, io.Discard, io.Discard, false, "", false, false,
		ex, executor.RuntimeContext{}, "exec_lane_setup", 3, nil, "")
	r.Isolation = IsolationNone
	if err := r.Run(plan); err != nil {
		t.Fatal(err)
	}
	if ex.runs["actions/setup-node@v6"] != 1 {
		t.Fatalf("the setup action ran %d times; once for the three jobs", ex.runs["actions/setup-node@v6"])
	}
	if ex.runs["pnpm install"] != 1 {
		t.Fatalf("the lane-scoped install still runs once: %d", ex.runs["pnpm install"])
	}
	imported := 0
	for _, n := range ex.imports {
		imported += n
	}
	if imported != 2 {
		t.Fatalf("the two other jobs import the owner's exports: %v", ex.imports)
	}
	st := r.SnapshotState()
	for _, id := range []string{"a", "b", "c"} {
		if st.Jobs[id].Status != "completed" || st.Jobs[id].Steps["node"] != "completed" {
			t.Fatalf("job %s: %+v (the shared action is completed for every job)", id, st.Jobs[id])
		}
	}
}

func TestLaneSetup_SetupActionWithPrivateExportsRunsPerJob(t *testing.T) {
	ex := newSharingExecutor()
	ex.private = true // the action installed under the owner's RUNNER_TEMP
	plan := &model.Plan{Jobs: []model.PlanJob{setupActionJob("a"), setupActionJob("b"), setupActionJob("c")}}
	r := NewRunner(t.TempDir(), false, io.Discard, io.Discard, false, "", false, false,
		ex, executor.RuntimeContext{}, "exec_lane_setup", 3, nil, "")
	r.Isolation = IsolationNone
	if err := r.Run(plan); err != nil {
		t.Fatal(err)
	}
	if ex.runs["actions/setup-node@v6"] != 3 {
		t.Fatalf("an action whose exports are the owner's alone runs in every job: ran %d", ex.runs["actions/setup-node@v6"])
	}
	if len(ex.imports) != 0 {
		t.Fatalf("nothing is imported: %v", ex.imports)
	}
	if ex.runs["pnpm install"] != 1 {
		t.Fatalf("the lane-scoped install still runs once: %d", ex.runs["pnpm install"])
	}
}

func TestLaneSetup_DifferentInputsAreDifferentSetupActions(t *testing.T) {
	ex := newSharingExecutor()
	b := setupActionJob("b")
	b.Steps[0].With = map[string]interface{}{"node-version": "20"}
	plan := &model.Plan{Jobs: []model.PlanJob{setupActionJob("a"), b}}
	r := NewRunner(t.TempDir(), false, io.Discard, io.Discard, false, "", false, false,
		ex, executor.RuntimeContext{}, "exec_lane_setup", 2, nil, "")
	r.Isolation = IsolationNone
	if err := r.Run(plan); err != nil {
		t.Fatal(err)
	}
	if ex.runs["actions/setup-node@v6"] != 2 {
		t.Fatalf("setup-node 22 and setup-node 20 are two actions: ran %d", ex.runs["actions/setup-node@v6"])
	}
}

func TestLaneSetup_SetupActionStaysPerJobWithoutAnExportSharer(t *testing.T) {
	ex := newCountingExecutor() // counts by step.Run: a use step counts under ""
	plan := &model.Plan{Jobs: []model.PlanJob{setupActionJob("a"), setupActionJob("b")}}
	r := NewRunner(t.TempDir(), false, io.Discard, io.Discard, false, "", false, false,
		ex, executor.RuntimeContext{}, "exec_lane_setup", 2, nil, "")
	r.Isolation = IsolationNone
	if err := r.Run(plan); err != nil {
		t.Fatal(err)
	}
	if ex.runs[""] != 2 {
		t.Fatalf("an executor that cannot hand exports over runs the action in every job: ran %d", ex.runs[""])
	}
	if ex.runs["pnpm install"] != 1 {
		t.Fatalf("the lane-scoped install runs once: %d", ex.runs["pnpm install"])
	}
}
