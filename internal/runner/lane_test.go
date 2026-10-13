package runner

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
)

// laneExecutor records the jobs it runs and fails the ones in fail.
type laneExecutor struct {
	mu   sync.Mutex
	ran  []string
	fail map[string]bool
}

func (*laneExecutor) Name() string                       { return "lane" }
func (*laneExecutor) Prepare(executor.ExecContext) error { return nil }
func (*laneExecutor) Cleanup(executor.ExecContext) error { return nil }
func (e *laneExecutor) RunStep(_ executor.ExecContext, job model.PlanJob, _ model.PlanStep) (string, error) {
	e.mu.Lock()
	e.ran = append(e.ran, job.ID)
	e.mu.Unlock()
	if e.fail[job.ID] {
		return "boom", fmt.Errorf("failed")
	}
	return "ok", nil
}

func (e *laneExecutor) jobs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := append([]string(nil), e.ran...)
	sort.Strings(out)
	return out
}

// twoLanePlan: lane A holds a1 → a2; lane B holds b1 (waits on a2) and b2.
func twoLanePlan() *model.Plan {
	step := []model.PlanStep{{ID: "run", Name: "run"}}
	return &model.Plan{
		Jobs: []model.PlanJob{
			{ID: "a1", Component: "a", Lane: "A", Steps: step},
			{ID: "a2", Component: "a", Lane: "A", DependsOn: []string{"a1"}, Steps: step},
			{ID: "b1", Component: "b", Lane: "B", DependsOn: []string{"a2"}, Steps: step},
			{ID: "b2", Component: "b", Lane: "B", Steps: step},
		},
		Lanes: []model.PlanLane{
			{ID: "A", Jobs: []string{"a1", "a2"}, Concurrency: 2},
			{ID: "B", Jobs: []string{"b1", "b2"}, Concurrency: 1},
		},
	}
}

func laneRunner(t *testing.T, ex executor.Executor, lane string, jobs []string, concurrency int) *Runner {
	t.Helper()
	r := NewRunner(t.TempDir(), false, io.Discard, io.Discard, false, "", false, false,
		ex, executor.RuntimeContext{}, "exec_lane", concurrency, nil, "")
	r.LaneID, r.LaneJobs = lane, jobs
	return r
}

func TestLane_RunsOnlyItsJobsInDependencyOrder(t *testing.T) {
	ex := &laneExecutor{}
	r := laneRunner(t, ex, "A", []string{"a1", "a2"}, 2)
	if err := r.Run(twoLanePlan()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ex.ran, ","); got != "a1,a2" {
		t.Fatalf("ran %s", got)
	}
}

func TestLane_LocalStateRefusesMissingOutsideUpstream(t *testing.T) {
	ex := &laneExecutor{}
	r := laneRunner(t, ex, "B", []string{"b1", "b2"}, 1)
	err := r.Run(twoLanePlan())
	if err == nil || !strings.Contains(err.Error(), "run lane A first") || !strings.Contains(err.Error(), "a2") {
		t.Fatalf("want a refusal naming lane A, got %v", err)
	}
	if len(ex.ran) != 0 {
		t.Fatalf("nothing may run: %v", ex.ran)
	}
}

func TestLane_LocalStateRunsWhenUpstreamLaneCompleted(t *testing.T) {
	ex := &laneExecutor{}
	r := laneRunner(t, ex, "B", []string{"b1", "b2"}, 1)
	r.ResumeJobs = map[string]*execmodel.JobState{
		"a1": {Status: "completed"}, "a2": {Status: "completed"},
	}
	if err := r.Run(twoLanePlan()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ex.jobs(), ","); got != "b1,b2" {
		t.Fatalf("ran %s", got)
	}
}

// With remote state the outside upstream is awaited without holding a slot:
// at concurrency 1, b2 must finish while b1 is still waiting.
func TestLane_AwaitUpstreamHoldsNoSlot(t *testing.T) {
	ex := &laneExecutor{}
	r := laneRunner(t, ex, "B", []string{"b1", "b2"}, 1)
	var calls []string
	r.Hooks = &RunnerHooks{
		AwaitUpstream: func(jobID string, ups []string) error {
			calls = append(calls, jobID+"<-"+strings.Join(ups, ","))
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				ex.mu.Lock()
				done := len(ex.ran) > 0
				ex.mu.Unlock()
				if done {
					return nil
				}
				time.Sleep(5 * time.Millisecond)
			}
			return fmt.Errorf("b2 never ran while b1 waited: the wait held the only slot")
		},
	}
	if err := r.Run(twoLanePlan()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ex.ran, ","); got != "b2,b1" {
		t.Fatalf("ran %s", got)
	}
	if strings.Join(calls, ";") != "b1<-a2" {
		t.Fatalf("AwaitUpstream calls = %v", calls)
	}
}

func TestLane_AwaitUpstreamFailureFailsTheJob(t *testing.T) {
	ex := &laneExecutor{}
	r := laneRunner(t, ex, "B", []string{"b1", "b2"}, 2)
	r.Hooks = &RunnerHooks{AwaitUpstream: func(jobID string, _ []string) error {
		return fmt.Errorf("job %s: upstream a2 failed", jobID)
	}}
	err := r.Run(twoLanePlan())
	if err == nil || !strings.Contains(err.Error(), "upstream a2 failed") {
		t.Fatalf("want the upstream failure, got %v", err)
	}
	if got := strings.Join(ex.ran, ","); got != "b2" {
		t.Fatalf("the independent job still runs: %s", got)
	}
}

func TestLane_FailureStopsOnlyDependents(t *testing.T) {
	step := []model.PlanStep{{ID: "run", Name: "run"}}
	plan := &model.Plan{Jobs: []model.PlanJob{
		{ID: "x", Component: "x", Lane: "L", Steps: step},
		{ID: "y", Component: "y", Lane: "L", DependsOn: []string{"x"}, Steps: step},
		{ID: "z", Component: "z", Lane: "L", Steps: step},
	}, Lanes: []model.PlanLane{{ID: "L", Jobs: []string{"x", "y", "z"}, Concurrency: 1}}}
	ex := &laneExecutor{fail: map[string]bool{"x": true}}
	r := laneRunner(t, ex, "L", []string{"x", "y", "z"}, 1)
	if err := r.Run(plan); err == nil {
		t.Fatal("a failed job must fail the lane")
	}
	if got := strings.Join(ex.jobs(), ","); got != "x,z" {
		t.Fatalf("ran %s: y depends on the failed x, z does not", got)
	}
}

func TestLane_RetryRerunsOnlyUnfinishedJobs(t *testing.T) {
	ex := &laneExecutor{}
	r := laneRunner(t, ex, "A", []string{"a1", "a2"}, 2)
	r.Retry = true
	r.ResumeJobs = map[string]*execmodel.JobState{"a1": {Status: "completed"}}
	if err := r.Run(twoLanePlan()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ex.ran, ","); got != "a2" {
		t.Fatalf("ran %s", got)
	}
}
