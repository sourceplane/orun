package runner

import (
	"fmt"
	"io"
	"os"
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

type fakeChecks struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeChecks) Start(jobID, name string) {
	f.mu.Lock()
	f.calls = append(f.calls, "start "+jobID)
	f.mu.Unlock()
}

func (f *fakeChecks) Finish(jobID, name, conclusion, _ string) {
	f.mu.Lock()
	f.calls = append(f.calls, "finish "+jobID+" "+conclusion)
	f.mu.Unlock()
}

func (f *fakeChecks) sorted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.calls...)
	sort.Strings(out)
	return out
}

func TestLane_PostsOneCheckPerJob(t *testing.T) {
	step := []model.PlanStep{{ID: "run", Name: "run"}}
	plan := &model.Plan{Jobs: []model.PlanJob{
		{ID: "x", Component: "x", Lane: "L", Steps: step},
		{ID: "y", Component: "y", Lane: "L", DependsOn: []string{"x"}, Steps: step},
		{ID: "z", Component: "z", Lane: "L", Steps: step},
		{ID: "w", Component: "w", Lane: "L", Steps: step},
	}, Lanes: []model.PlanLane{{ID: "L", Jobs: []string{"x", "y", "z", "w"}, Concurrency: 2}}}
	ex := &laneExecutor{fail: map[string]bool{"x": true}}
	r := laneRunner(t, ex, "L", []string{"x", "y", "z", "w"}, 2)
	r.ResumeJobs = map[string]*execmodel.JobState{"w": {Status: "completed"}}
	fc := &fakeChecks{}
	r.Checks = fc
	_ = r.Run(plan)
	want := []string{"finish w success", "finish x failure", "finish y cancelled", "finish z success", "start x", "start z"}
	if got := fc.sorted(); strings.Join(got, ";") != strings.Join(want, ";") {
		t.Fatalf("checks = %v", got)
	}
}

func TestLane_SingleJobLanePostsNoCheck(t *testing.T) {
	ex := &laneExecutor{}
	r := laneRunner(t, ex, "A", []string{"a1"}, 1)
	fc := &fakeChecks{}
	r.Checks = fc
	if err := r.Run(twoLanePlan()); err != nil {
		t.Fatal(err)
	}
	if len(fc.calls) != 0 {
		t.Fatalf("a single-job lane is its own check: %v", fc.calls)
	}
}

func TestLaneSummaryMarkdown(t *testing.T) {
	md := laneSummaryMarkdown("stage/auto-1", []*laneRecord{
		{job: model.PlanJob{ID: "a", CheckName: "a · stage · Deploy"}, status: laneSucceeded, duration: 3 * time.Second, waited: []string{"a.dev"}},
		{job: model.PlanJob{ID: "b"}, status: laneBlocked},
	})
	for _, want := range []string{"### Lane `stage/auto-1`", "| a · stage · Deploy | ✅ succeeded |", "| a.dev |", "| b | ⊘ blocked | — | — |"} {
		if !strings.Contains(md, want) {
			t.Fatalf("summary missing %q:\n%s", want, md)
		}
	}
}

func TestLane_GHAHoldsEachJobsStepsTogether(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_STEP_SUMMARY", t.TempDir()+"/summary.md")
	steps := []model.PlanStep{{ID: "one", Name: "one"}, {ID: "two", Name: "two"}}
	plan := &model.Plan{Jobs: []model.PlanJob{
		{ID: "x", Component: "x", Environment: "stage", CheckName: "x · stage · Deploy", Lane: "L", Steps: steps},
		{ID: "y", Component: "y", Environment: "stage", CheckName: "y · stage · Deploy", Lane: "L", Steps: steps},
	}, Lanes: []model.PlanLane{{ID: "L", Jobs: []string{"x", "y"}, Concurrency: 2}}}
	var out strings.Builder
	var mu sync.Mutex
	r := NewRunner(t.TempDir(), false, writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return out.Write(p)
	}), io.Discard, false, "", false, false, &laneExecutor{}, executor.RuntimeContext{}, "exec_lane", 2, nil, "")
	r.LaneID, r.LaneJobs = "L", []string{"x", "y"}
	if err := r.Run(plan); err != nil {
		t.Fatal(err)
	}
	// Each job's two step groups are adjacent: no other job's group between.
	var owners []string
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "::group::") {
			owners = append(owners, strings.SplitN(strings.TrimPrefix(line, "::group::"), " ", 2)[0])
		}
	}
	if len(owners) != 4 || owners[0] != owners[1] || owners[2] != owners[3] || owners[0] == owners[2] {
		t.Fatalf("step groups interleave or lack the job label: %v\n%s", owners, out.String())
	}
	data, _ := os.ReadFile(os.Getenv("GITHUB_STEP_SUMMARY"))
	if !strings.Contains(string(data), "### Lane `L`") {
		t.Fatalf("no step summary: %s", data)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// A job that fails in a lane holding step groups must still print them: the
// failed step's group, its error annotation and the job footer. They were
// buffered and never flushed (cirrus-baseline run 38514815544: projects-worker
// failed with nothing of its own in the log).
func TestLane_GHAPrintsAFailedJobsHeldSteps(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_STEP_SUMMARY", t.TempDir()+"/summary.md")
	steps := []model.PlanStep{{ID: "setup", Name: "setup"}}
	plan := &model.Plan{Jobs: []model.PlanJob{
		{ID: "ok", Component: "ok", Environment: "stage", CheckName: "ok · stage · Deploy", Lane: "L", Steps: steps},
		{ID: "bad", Component: "bad", Environment: "stage", CheckName: "bad · stage · Deploy", Lane: "L", Steps: steps},
	}, Lanes: []model.PlanLane{{ID: "L", Jobs: []string{"ok", "bad"}, Concurrency: 2}}}
	var out strings.Builder
	var mu sync.Mutex
	r := NewRunner(t.TempDir(), false, writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return out.Write(p)
	}), io.Discard, false, "", false, false, &laneExecutor{fail: map[string]bool{"bad": true}}, executor.RuntimeContext{}, "exec_lane", 2, nil, "")
	r.LaneID, r.LaneJobs = "L", []string{"ok", "bad"}
	_ = r.Run(plan)
	log := out.String()
	for _, want := range []string{
		"::group::bad · stage · Deploy › ✕  01 setup",
		"boom",
		"::error",
		"✕ bad failed",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("failed job's output missing %q:\n%s", want, log)
		}
	}
}

// edgePlan is cirrus-baseline's edge phase in miniature: one lane holds the
// fleet and the API edge, the console waits on the edge from a lane of its
// own, and the edge is listed last.
func edgePlan() *model.Plan {
	step := []model.PlanStep{{ID: "run", Name: "run"}}
	return &model.Plan{
		Jobs: []model.PlanJob{
			{ID: "w1", Component: "w1", Lane: "F", Steps: step},
			{ID: "w2", Component: "w2", Lane: "F", Steps: step},
			{ID: "w3", Component: "w3", Lane: "F", Steps: step},
			{ID: "edge", Component: "edge", Lane: "F", Steps: step},
			{ID: "console", Component: "console", Lane: "C", DependsOn: []string{"edge"}, Steps: step},
		},
		Lanes: []model.PlanLane{
			{ID: "F", Jobs: []string{"w1", "w2", "w3", "edge"}, Concurrency: 1},
			{ID: "C", Jobs: []string{"console"}, Concurrency: 1},
		},
	}
}

// The slot goes to the job another lane waits on, not to the first listed.
func TestLane_SlotGoesToTheJobOthersWaitOn(t *testing.T) {
	for i := 0; i < 20; i++ {
		ex := &laneExecutor{}
		r := laneRunner(t, ex, "F", []string{"w1", "w2", "w3", "edge"}, 1)
		if err := r.Run(edgePlan()); err != nil {
			t.Fatal(err)
		}
		if ex.ran[0] != "edge" {
			t.Fatalf("run %d: ran %v; the edge must take the first slot", i, ex.ran)
		}
		if got := strings.Join(ex.ran[1:], ","); got != "w1,w2,w3" {
			t.Fatalf("run %d: ties must keep plan order, ran %v", i, ex.ran)
		}
	}
}

// Inside a lane the head of the longer chain goes first: at one slot, y1 → y2
// is two jobs behind y1 and x is one, so y1 runs before x although x is
// listed first.
func TestLane_SlotGoesToTheLongerChain(t *testing.T) {
	step := []model.PlanStep{{ID: "run", Name: "run"}}
	plan := &model.Plan{
		Jobs: []model.PlanJob{
			{ID: "x", Component: "x", Lane: "L", Steps: step},
			{ID: "y1", Component: "y", Lane: "L", Steps: step},
			{ID: "y2", Component: "y", Lane: "L", DependsOn: []string{"y1"}, Steps: step},
		},
		Lanes: []model.PlanLane{{ID: "L", Jobs: []string{"x", "y1", "y2"}, Concurrency: 1}},
	}
	for i := 0; i < 20; i++ {
		ex := &laneExecutor{}
		r := laneRunner(t, ex, "L", []string{"x", "y1", "y2"}, 1)
		if err := r.Run(plan); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(ex.ran, ","); got != "y1,x,y2" && got != "y1,y2,x" {
			t.Fatalf("run %d: ran %s; y1 must go first", i, got)
		}
	}
}

func TestCriticalPath(t *testing.T) {
	got := criticalPath(edgePlan().Jobs)
	want := map[string]int{"w1": 1, "w2": 1, "w3": 1, "edge": 2, "console": 1}
	for id, n := range want {
		if got[id] != n {
			t.Errorf("%s: %d, want %d", id, got[id], n)
		}
	}
}

// Every slot comes back, and no more jobs run at once than there are slots.
func TestSlotGate_NeverExceedsItsSlots(t *testing.T) {
	const jobs, slots = 40, 3
	g := newSlotGate(slots, jobs)
	var mu sync.Mutex
	inFlight, peak := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < jobs; i++ {
		tk := g.ticket(i%5, i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			tk.acquire()
			mu.Lock()
			inFlight++
			if inFlight > peak {
				peak = inFlight
			}
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			inFlight--
			mu.Unlock()
			tk.release()
		}()
	}
	wg.Wait()
	if peak > slots {
		t.Fatalf("peak %d > %d slots", peak, slots)
	}
	if g.free != slots {
		t.Fatalf("free %d after all released, want %d", g.free, slots)
	}
}
