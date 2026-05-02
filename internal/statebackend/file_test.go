package statebackend

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/state"
)

func newTestBackend(t *testing.T, plan *model.Plan) (*FileStateBackend, string) {
	t.Helper()
	dir := t.TempDir()
	store := state.NewStore(dir)
	backend := NewFileStateBackend(store)
	ctx := context.Background()
	if _, err := backend.InitRun(ctx, plan, InitRunOptions{RunID: "test-exec"}); err != nil {
		t.Fatalf("InitRun: %v", err)
	}
	return backend, dir
}

func simplePlan(jobs ...model.PlanJob) *model.Plan {
	return &model.Plan{Jobs: jobs}
}

func TestClaimJob_FirstClaimSucceeds(t *testing.T) {
	t.Parallel()
	plan := simplePlan(model.PlanJob{ID: "job-a", Steps: []model.PlanStep{{Run: "echo hi"}}})
	backend, _ := newTestBackend(t, plan)

	result, err := backend.ClaimJob(context.Background(), "test-exec", plan.Jobs[0], "runner1")
	if err != nil {
		t.Fatalf("ClaimJob: %v", err)
	}
	if !result.Claimed {
		t.Fatal("expected first claim to succeed")
	}
}

func TestClaimJob_AlreadyCompleted(t *testing.T) {
	t.Parallel()
	plan := simplePlan(model.PlanJob{ID: "job-a", Steps: []model.PlanStep{{Run: "echo hi"}}})
	backend, _ := newTestBackend(t, plan)

	st := &state.ExecState{
		ExecID: "test-exec",
		Jobs: map[string]*state.JobState{
			"job-a": {Status: "completed", Steps: map[string]string{}},
		},
	}
	if err := backend.Store.SaveState("test-exec", st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	result, err := backend.ClaimJob(context.Background(), "test-exec", plan.Jobs[0], "runner1")
	if err != nil {
		t.Fatalf("ClaimJob: %v", err)
	}
	if result.Claimed {
		t.Fatal("expected claim to be rejected for completed job")
	}
	if result.CurrentStatus != "completed" {
		t.Fatalf("expected CurrentStatus=completed, got %s", result.CurrentStatus)
	}
}

func TestClaimJob_AlreadyRunning(t *testing.T) {
	t.Parallel()
	plan := simplePlan(model.PlanJob{ID: "job-a", Steps: []model.PlanStep{{Run: "echo hi"}}})
	backend, _ := newTestBackend(t, plan)

	st := &state.ExecState{
		ExecID: "test-exec",
		Jobs: map[string]*state.JobState{
			"job-a": {Status: "running", Steps: map[string]string{}},
		},
	}
	if err := backend.Store.SaveState("test-exec", st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	result, err := backend.ClaimJob(context.Background(), "test-exec", plan.Jobs[0], "runner2")
	if err != nil {
		t.Fatalf("ClaimJob: %v", err)
	}
	if result.Claimed {
		t.Fatal("expected claim to be rejected for running job")
	}
	if result.CurrentStatus != "running" {
		t.Fatalf("expected CurrentStatus=running, got %s", result.CurrentStatus)
	}
}

func TestClaimJob_FailedDependencyBlocks(t *testing.T) {
	t.Parallel()
	plan := simplePlan(
		model.PlanJob{ID: "job-a", Steps: []model.PlanStep{{Run: "echo hi"}}},
		model.PlanJob{ID: "job-b", DependsOn: []string{"job-a"}, Steps: []model.PlanStep{{Run: "echo hi"}}},
	)
	backend, _ := newTestBackend(t, plan)

	st := &state.ExecState{
		ExecID: "test-exec",
		Jobs: map[string]*state.JobState{
			"job-a": {Status: "failed", Steps: map[string]string{}},
		},
	}
	if err := backend.Store.SaveState("test-exec", st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	result, err := backend.ClaimJob(context.Background(), "test-exec", plan.Jobs[1], "runner1")
	if err != nil {
		t.Fatalf("ClaimJob: %v", err)
	}
	if result.Claimed {
		t.Fatal("expected claim to be rejected for blocked deps")
	}
	if !result.DepsBlocked {
		t.Fatal("expected DepsBlocked=true")
	}
}

func TestClaimJob_PendingDependencyWaits(t *testing.T) {
	t.Parallel()
	plan := simplePlan(
		model.PlanJob{ID: "job-a", Steps: []model.PlanStep{{Run: "echo hi"}}},
		model.PlanJob{ID: "job-b", DependsOn: []string{"job-a"}, Steps: []model.PlanStep{{Run: "echo hi"}}},
	)
	backend, _ := newTestBackend(t, plan)

	result, err := backend.ClaimJob(context.Background(), "test-exec", plan.Jobs[1], "runner1")
	if err != nil {
		t.Fatalf("ClaimJob: %v", err)
	}
	if result.Claimed {
		t.Fatal("expected claim to be rejected for pending deps")
	}
	if !result.DepsWaiting {
		t.Fatal("expected DepsWaiting=true")
	}
}

func TestClaimJob_ConcurrentOnlyOneWins(t *testing.T) {
	t.Parallel()
	plan := simplePlan(model.PlanJob{ID: "job-a", Steps: []model.PlanStep{{Run: "echo hi"}}})
	backend, _ := newTestBackend(t, plan)

	const N = 10
	results := make(chan bool, N)
	var wg sync.WaitGroup
	wg.Add(N)

	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			r, err := backend.ClaimJob(context.Background(), "test-exec", plan.Jobs[0], "runner")
			if err != nil {
				t.Errorf("ClaimJob: %v", err)
				results <- false
				return
			}
			results <- r.Claimed
		}()
	}
	wg.Wait()
	close(results)

	claimed := 0
	for c := range results {
		if c {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("expected exactly 1 claim to succeed, got %d", claimed)
	}
}

func TestUpdateJob_Success(t *testing.T) {
	t.Parallel()
	plan := simplePlan(model.PlanJob{ID: "job-a", Steps: []model.PlanStep{{Run: "echo hi"}}})
	backend, _ := newTestBackend(t, plan)

	backend.ClaimJob(context.Background(), "test-exec", plan.Jobs[0], "runner1")

	err := backend.UpdateJob(context.Background(), "test-exec", "job-a", "runner1", JobStatusSuccess, "")
	if err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}

	st, _ := backend.Store.LoadState("test-exec")
	if st.Jobs["job-a"].Status != "completed" {
		t.Fatalf("expected completed, got %s", st.Jobs["job-a"].Status)
	}
}

func TestUpdateJob_Failed(t *testing.T) {
	t.Parallel()
	plan := simplePlan(model.PlanJob{ID: "job-a", Steps: []model.PlanStep{{Run: "echo hi"}}})
	backend, _ := newTestBackend(t, plan)

	backend.ClaimJob(context.Background(), "test-exec", plan.Jobs[0], "runner1")

	err := backend.UpdateJob(context.Background(), "test-exec", "job-a", "runner1", JobStatusFailed, "oops")
	if err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}

	st, _ := backend.Store.LoadState("test-exec")
	if st.Jobs["job-a"].Status != "failed" {
		t.Fatalf("expected failed, got %s", st.Jobs["job-a"].Status)
	}
	if st.Jobs["job-a"].LastError != "oops" {
		t.Fatalf("expected error text 'oops', got %s", st.Jobs["job-a"].LastError)
	}
}

func TestConcurrentUpdates_StateValid(t *testing.T) {
	t.Parallel()
	jobs := make([]model.PlanJob, 10)
	for i := range jobs {
		jobs[i] = model.PlanJob{
			ID:    "job-" + string(rune('a'+i)),
			Steps: []model.PlanStep{{Run: "echo hi"}},
		}
	}
	plan := simplePlan(jobs...)
	backend, _ := newTestBackend(t, plan)

	var wg sync.WaitGroup
	wg.Add(len(jobs))
	for _, job := range jobs {
		go func(j model.PlanJob) {
			defer wg.Done()
			backend.ClaimJob(context.Background(), "test-exec", j, "runner")
			backend.UpdateJob(context.Background(), "test-exec", j.ID, "runner", JobStatusSuccess, "")
		}(job)
	}
	wg.Wait()

	raw, err := os.ReadFile(filepath.Join(backend.Store.ExecDir(), "test-exec", "state.json"))
	if err != nil {
		t.Fatalf("reading state.json: %v", err)
	}
	var st state.ExecState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("state.json is not valid JSON: %v", err)
	}
	for _, job := range jobs {
		js, ok := st.Jobs[job.ID]
		if !ok || js == nil {
			t.Fatalf("job %s missing from state", job.ID)
		}
		if js.Status != "completed" {
			t.Fatalf("job %s: expected completed, got %s", job.ID, js.Status)
		}
	}
}

func TestRunnableJobs_ReturnsReadyJobs(t *testing.T) {
	t.Parallel()
	plan := simplePlan(
		model.PlanJob{ID: "a", Steps: []model.PlanStep{{Run: "echo"}}},
		model.PlanJob{ID: "b", DependsOn: []string{"a"}, Steps: []model.PlanStep{{Run: "echo"}}},
		model.PlanJob{ID: "c", DependsOn: []string{"a"}, Steps: []model.PlanStep{{Run: "echo"}}},
		model.PlanJob{ID: "d", DependsOn: []string{"b", "c"}, Steps: []model.PlanStep{{Run: "echo"}}},
	)
	backend, _ := newTestBackend(t, plan)

	st := &state.ExecState{
		ExecID: "test-exec",
		Jobs: map[string]*state.JobState{
			"a": {Status: "completed", Steps: map[string]string{}},
		},
	}
	backend.Store.SaveState("test-exec", st)

	runnable, err := backend.RunnableJobs(context.Background(), "test-exec")
	if err != nil {
		t.Fatalf("RunnableJobs: %v", err)
	}

	runnableSet := map[string]bool{}
	for _, id := range runnable {
		runnableSet[id] = true
	}
	if !runnableSet["b"] || !runnableSet["c"] {
		t.Fatalf("expected b and c runnable, got %v", runnable)
	}
	if runnableSet["d"] {
		t.Fatal("d should not be runnable (deps b,c not done)")
	}
	if runnableSet["a"] {
		t.Fatal("a should not be runnable (already completed)")
	}
}

func TestClaimJob_LockTimeout(t *testing.T) {
	t.Parallel()
	plan := simplePlan(model.PlanJob{ID: "job-a", Steps: []model.PlanStep{{Run: "echo hi"}}})
	backend, _ := newTestBackend(t, plan)

	lockPath := backend.lockPath("test-exec")
	holder := NewFileLock(lockPath)
	if err := holder.Lock(context.Background()); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer holder.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_, err := backend.ClaimJob(ctx, "test-exec", plan.Jobs[0], "runner")
	if err == nil {
		t.Fatal("expected error from lock timeout")
	}
}
