package runner

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
)

// A whole plan at concurrency > 1 runs on the lane scheduler
// (specs/orun-runner-efficiency RE1). These tests pin what the batch loop it
// replaced got wrong: a job waited for the whole batch it was not in, and the
// component cap, defaulting to 1, admitted one component per batch.

// trackingExecutor sleeps for the duration in step.Run and records how many
// jobs and distinct components were in flight together, and when each job
// finished.
type trackingExecutor struct {
	mu            sync.Mutex
	inflight      map[string]int
	maxJobs       int
	maxComponents int
	finished      []string
	finishedAt    map[string]time.Time
	fail          map[string]bool
}

func newTrackingExecutor() *trackingExecutor {
	return &trackingExecutor{inflight: map[string]int{}, finishedAt: map[string]time.Time{}}
}

func (*trackingExecutor) Name() string                       { return "tracking" }
func (*trackingExecutor) Prepare(executor.ExecContext) error { return nil }
func (*trackingExecutor) Cleanup(executor.ExecContext) error { return nil }

func (e *trackingExecutor) RunStep(_ executor.ExecContext, job model.PlanJob, step model.PlanStep) (string, error) {
	e.mu.Lock()
	e.inflight[job.Component]++
	running := 0
	for _, n := range e.inflight {
		running += n
	}
	if running > e.maxJobs {
		e.maxJobs = running
	}
	if len(e.inflight) > e.maxComponents {
		e.maxComponents = len(e.inflight)
	}
	e.mu.Unlock()

	if d, err := time.ParseDuration(strings.TrimSpace(step.Run)); err == nil {
		time.Sleep(d)
	}

	e.mu.Lock()
	e.inflight[job.Component]--
	if e.inflight[job.Component] <= 0 {
		delete(e.inflight, job.Component)
	}
	e.finished = append(e.finished, job.ID)
	e.finishedAt[job.ID] = time.Now()
	e.mu.Unlock()
	if e.fail[job.ID] {
		return "boom", fmt.Errorf("failed")
	}
	return "ok", nil
}

func (e *trackingExecutor) ran() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := append([]string(nil), e.finished...)
	sortStrings(out)
	return strings.Join(out, ",")
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func sleepJob(id, component, sleep string, deps ...string) model.PlanJob {
	return model.PlanJob{ID: id, Name: id, Component: component, Environment: "stage", DependsOn: deps,
		Steps: []model.PlanStep{{ID: "run", Name: "run", Run: sleep}}}
}

func planRunner(t *testing.T, ex executor.Executor, concurrency int) *Runner {
	t.Helper()
	r := NewRunner(t.TempDir(), false, io.Discard, io.Discard, false, "", false, false,
		ex, executor.RuntimeContext{}, "exec_schedule", concurrency, nil, "")
	r.Isolation = IsolationNone
	return r
}

func TestSchedule_IndependentComponentsRunTogether(t *testing.T) {
	ex := newTrackingExecutor()
	plan := &model.Plan{Jobs: []model.PlanJob{
		sleepJob("a", "a", "120ms"), sleepJob("b", "b", "120ms"), sleepJob("c", "c", "120ms"), sleepJob("d", "d", "120ms"),
	}}
	r := planRunner(t, ex, 4)
	start := time.Now()
	if err := r.Run(plan); err != nil {
		t.Fatal(err)
	}
	if ex.maxJobs != 4 {
		t.Fatalf("at most %d jobs were in flight together; four independent jobs of four components at concurrency 4 must all run at once", ex.maxJobs)
	}
	if el := time.Since(start); el > 400*time.Millisecond {
		t.Fatalf("took %v; four 120 ms jobs at concurrency 4 ran in series", el)
	}
}

func TestSchedule_ChainStartsWhenItsUpstreamFinishes(t *testing.T) {
	ex := newTrackingExecutor()
	plan := &model.Plan{Jobs: []model.PlanJob{
		sleepJob("long", "x", "400ms"),
		sleepJob("b", "x", "40ms"),
		sleepJob("c", "x", "40ms", "b"),
		sleepJob("d", "x", "40ms", "c"),
		sleepJob("e", "x", "40ms", "d"),
	}}
	if err := planRunner(t, ex, 4).Run(plan); err != nil {
		t.Fatal(err)
	}
	if ex.ran() != "b,c,d,e,long" {
		t.Fatalf("ran %s", ex.ran())
	}
	if !ex.finishedAt["e"].Before(ex.finishedAt["long"]) {
		t.Fatal("the chain b→c→d→e finished after the 400 ms job beside it: each link waited for the batch instead of its upstream")
	}
}

func TestSchedule_ComponentCapHoldsNewComponents(t *testing.T) {
	ex := newTrackingExecutor()
	plan := &model.Plan{Jobs: []model.PlanJob{
		sleepJob("a1", "a", "30ms"), sleepJob("a2", "a", "30ms"),
		sleepJob("b1", "b", "30ms"), sleepJob("b2", "b", "30ms"),
	}}
	r := planRunner(t, ex, 4)
	r.ComponentConcurrency = 1
	if err := r.Run(plan); err != nil {
		t.Fatal(err)
	}
	if ex.ran() != "a1,a2,b1,b2" {
		t.Fatalf("ran %s", ex.ran())
	}
	if ex.maxComponents != 1 {
		t.Fatalf("%d components were in flight together under --component-concurrency 1", ex.maxComponents)
	}
}

func TestSchedule_JobRefusedWhenUpstreamNotCompleted(t *testing.T) {
	ex := newTrackingExecutor()
	plan := &model.Plan{Jobs: []model.PlanJob{sleepJob("x", "a", "1ms"), sleepJob("y", "b", "1ms", "x")}}
	r := planRunner(t, ex, 2)
	r.JobID = "y"
	err := r.Run(plan)
	if err == nil || !strings.Contains(err.Error(), "dependencies not completed") {
		t.Fatalf("want the refusal the sequential path gives, got %v", err)
	}
	if ex.ran() != "" {
		t.Fatalf("nothing may run: %s", ex.ran())
	}
}

func TestSchedule_FilteredOutUpstreamMustBeCompleted(t *testing.T) {
	plan := &model.Plan{Jobs: []model.PlanJob{sleepJob("x", "a", "1ms"), sleepJob("y", "b", "1ms", "x")}}

	ex := newTrackingExecutor()
	r := planRunner(t, ex, 2)
	r.FilterComponents = []string{"b"}
	if err := r.Run(plan); err != nil {
		t.Fatal(err)
	}
	if ex.ran() != "" {
		t.Fatalf("y waits on x, which the filter left out and nothing completed: ran %s", ex.ran())
	}

	ex = newTrackingExecutor()
	r = planRunner(t, ex, 2)
	r.FilterComponents = []string{"b"}
	r.ResumeJobs = map[string]*execmodel.JobState{"x": {Status: "completed"}}
	if err := r.Run(plan); err != nil {
		t.Fatal(err)
	}
	if ex.ran() != "y" {
		t.Fatalf("with x completed in this run's state, y runs: ran %s", ex.ran())
	}
}

func TestSchedule_FailureStopsOnlyDependents(t *testing.T) {
	ex := newTrackingExecutor()
	ex.fail = map[string]bool{"x": true}
	plan := &model.Plan{Jobs: []model.PlanJob{
		sleepJob("x", "x", "1ms"), sleepJob("y", "y", "1ms", "x"), sleepJob("z", "z", "1ms"),
	}}
	if err := planRunner(t, ex, 2).Run(plan); err == nil {
		t.Fatal("a failed job must fail the run")
	}
	if ex.ran() != "x,z" {
		t.Fatalf("ran %s: y depends on the failed x, z does not", ex.ran())
	}
}

func TestComponentGate_ZeroIsNoCap(t *testing.T) {
	g := newComponentGate(0)
	done := make(chan struct{})
	go func() {
		for _, c := range []string{"a", "b", "c"} {
			g.enter(c, nil)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("a cap of 0 must admit every component")
	}
}

// ctxExecutor sleeps for the step's duration but stops when the run's
// context is cancelled, as a real process under exec.CommandContext does.
type ctxExecutor struct{ *trackingExecutor }

func (e ctxExecutor) RunStep(ctx executor.ExecContext, job model.PlanJob, step model.PlanStep) (string, error) {
	d, _ := time.ParseDuration(strings.TrimSpace(step.Run))
	select {
	case <-time.After(d):
		return e.trackingExecutor.RunStep(ctx, job, model.PlanStep{ID: step.ID, Run: "0ms"})
	case <-ctx.Context.Done():
		return "", ctx.Context.Err()
	}
}

// Cancelling the run's context (a SIGTERM on a spot runner) stops the
// running step, reports its job failed as interrupted, admits no further job
// and returns an error naming what is left (specs/orun-runner-efficiency §5.6).
func TestRun_InterruptStopsAdmittingAndNamesWhatIsLeft(t *testing.T) {
	for _, concurrency := range []int{1, 2} {
		ex := ctxExecutor{newTrackingExecutor()}
		plan := &model.Plan{Jobs: []model.PlanJob{
			sleepJob("a", "a", "2s"), sleepJob("b", "b", "2s", "a"), sleepJob("c", "c", "2s", "a"),
		}}
		r := planRunner(t, ex, concurrency)
		ctx, cancel := context.WithCancel(context.Background())
		r.Context = ctx
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()
		start := time.Now()
		err := r.Run(plan)
		if err == nil || !strings.Contains(err.Error(), "interrupted: 3 job(s)") {
			t.Fatalf("concurrency %d: want the interruption naming 3 jobs, got %v", concurrency, err)
		}
		if el := time.Since(start); el > time.Second {
			t.Fatalf("concurrency %d: the running step was not stopped: %v", concurrency, el)
		}
		st := r.SnapshotState()
		if js := st.Jobs["a"]; js == nil || js.Status != "failed" || !strings.Contains(js.LastError, "interrupted") {
			t.Fatalf("concurrency %d: the cut job reports failed as interrupted: %+v", concurrency, js)
		}
		if ex.ran() != "" {
			t.Fatalf("concurrency %d: no job finished: %s", concurrency, ex.ran())
		}
	}
}

// A step with no timeout of its own, none on its job and none on the plan is
// bounded by the runner's default (specs/orun-runner-efficiency §5.7).
func TestRun_DefaultTimeoutBoundsAStep(t *testing.T) {
	ex := ctxExecutor{newTrackingExecutor()}
	plan := &model.Plan{Jobs: []model.PlanJob{sleepJob("slow", "s", "5s")}}
	r := planRunner(t, ex, 1)
	r.DefaultTimeout = "100ms"
	start := time.Now()
	if err := r.Run(plan); err != nil {
		t.Fatalf("a timed-out step fails its job, not the run: %v", err)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("the default timeout did not bound the step: %v", el)
	}
	if js := r.SnapshotState().Jobs["slow"]; js == nil || js.Status != "failed" {
		t.Fatalf("job = %+v", js)
	}
	if got := r.resolveTimeout(model.PlanJob{Timeout: "2m"}, model.PlanStep{}); got != "2m" {
		t.Fatalf("a job's timeout wins over the default: %s", got)
	}
	if got := r.resolveTimeout(model.PlanJob{}, model.PlanStep{Timeout: "1m"}); got != "1m" {
		t.Fatalf("a step's timeout wins: %s", got)
	}
}
