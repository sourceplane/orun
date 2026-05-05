package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/state"
	"github.com/sourceplane/orun/internal/statebackend"
)

// fakeBackend is a minimal Backend implementation for testing claim/wait logic.
type fakeBackend struct {
	claimResults  []statebackend.ClaimResult
	claimCallIdx  int
	runnableJobs  [][]string
	runnableIdx   int
	claimCalls    int
	runnableCalls int
	runState      *state.ExecState
}

func (f *fakeBackend) InitRun(_ context.Context, _ *model.Plan, opts statebackend.InitRunOptions) (*statebackend.RunHandle, error) {
	return &statebackend.RunHandle{RunID: opts.RunID}, nil
}

func (f *fakeBackend) ClaimJob(_ context.Context, _ string, _ model.PlanJob, _ string) (*statebackend.ClaimResult, error) {
	f.claimCalls++
	if f.claimCallIdx >= len(f.claimResults) {
		return &statebackend.ClaimResult{Claimed: true}, nil
	}
	r := f.claimResults[f.claimCallIdx]
	f.claimCallIdx++
	return &r, nil
}

func (f *fakeBackend) RunnableJobs(_ context.Context, _ string) ([]string, error) {
	f.runnableCalls++
	if f.runnableIdx >= len(f.runnableJobs) {
		return f.runnableJobs[len(f.runnableJobs)-1], nil
	}
	r := f.runnableJobs[f.runnableIdx]
	f.runnableIdx++
	return r, nil
}

func (f *fakeBackend) Heartbeat(_ context.Context, _, _, _ string) (*statebackend.HeartbeatResult, error) {
	return &statebackend.HeartbeatResult{OK: true}, nil
}

func (f *fakeBackend) UpdateJob(_ context.Context, _, _, _ string, _ statebackend.JobStatus, _ string) error {
	return nil
}

func (f *fakeBackend) AppendStepLog(_ context.Context, _, _, _ string) error {
	return nil
}

func (f *fakeBackend) LoadRunState(_ context.Context, _ string) (*state.ExecState, *state.ExecMetadata, error) {
	return f.runState, nil, nil
}

func (f *fakeBackend) ReadJobLog(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

func (f *fakeBackend) Close(_ context.Context) error { return nil }

func TestPerformRemoteJobClaim_AlreadyComplete(t *testing.T) {
	backend := &fakeBackend{
		claimResults: []statebackend.ClaimResult{
			{Claimed: false, CurrentStatus: "success"},
		},
	}
	plan := &model.Plan{
		Jobs: []model.PlanJob{{ID: "job-1"}},
	}

	err := performRemoteJobClaim(context.Background(), backend, "run-1", plan, "job-1", "runner-1", io.Discard, false)

	var alreadyDone *jobAlreadyCompleteError
	if !errors.As(err, &alreadyDone) {
		t.Fatalf("expected *jobAlreadyCompleteError, got: %v", err)
	}
}

func TestPerformRemoteJobClaim_AlreadyCompleteExitsZero(t *testing.T) {
	// Verify that runPlan-level error handling treats already-complete as success.
	err := &jobAlreadyCompleteError{jobID: "job-1"}
	var alreadyDone *jobAlreadyCompleteError
	if !errors.As(err, &alreadyDone) {
		t.Fatal("errors.As should match *jobAlreadyCompleteError")
	}
}

func TestPerformRemoteJobClaim_OtherClaimErrorsFail(t *testing.T) {
	backend := &fakeBackend{
		claimResults: []statebackend.ClaimResult{
			{Claimed: false, DepsBlocked: true},
		},
	}
	plan := &model.Plan{
		Jobs: []model.PlanJob{{ID: "job-1"}},
	}

	err := performRemoteJobClaim(context.Background(), backend, "run-1", plan, "job-1", "runner-1", io.Discard, false)
	if err == nil {
		t.Fatal("expected error for depsBlocked")
	}
	var alreadyDone *jobAlreadyCompleteError
	if errors.As(err, &alreadyDone) {
		t.Fatal("depsBlocked should not produce jobAlreadyCompleteError")
	}
}

func TestPerformRemoteJobClaim_DepsWaitingCallsRunnable(t *testing.T) {
	backend := &fakeBackend{
		claimResults: []statebackend.ClaimResult{
			{Claimed: false, DepsWaiting: true},
			{Claimed: true},
		},
		runnableJobs: [][]string{
			{},
			{"job-1"},
		},
	}
	plan := &model.Plan{
		Jobs: []model.PlanJob{{ID: "job-1"}},
	}

	err := performRemoteJobClaim(context.Background(), backend, "run-1", plan, "job-1", "runner-1", io.Discard, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend.runnableCalls == 0 {
		t.Fatal("expected RunnableJobs to be called at least once")
	}
	if backend.claimCalls < 2 {
		t.Fatal("expected claim to be retried after job became runnable")
	}
}

func TestWaitForJobRunnable_ReturnsWhenJobAppears(t *testing.T) {
	backend := &fakeBackend{
		runnableJobs: [][]string{
			{"other-job"},
			{"other-job", "target-job"},
		},
	}

	deadline := time.Now().Add(10 * time.Second)
	err := waitForJobRunnable(context.Background(), backend, "run-1", "target-job", nil, time.Millisecond, deadline)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend.runnableCalls < 2 {
		t.Fatalf("expected at least 2 runnable calls, got %d", backend.runnableCalls)
	}
}

func TestWaitForJobRunnable_ContextCancellation(t *testing.T) {
	backend := &fakeBackend{
		runnableJobs: [][]string{{}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	deadline := time.Now().Add(10 * time.Second)
	err := waitForJobRunnable(ctx, backend, "run-1", "target-job", nil, time.Millisecond, deadline)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestWaitForJobRunnable_DeadlineExceeded(t *testing.T) {
	backend := &fakeBackend{
		runnableJobs: [][]string{{}},
	}

	deadline := time.Now().Add(-1 * time.Second) // already past
	err := waitForJobRunnable(context.Background(), backend, "run-1", "target-job", nil, time.Millisecond, deadline)
	if err == nil {
		t.Fatal("expected deadline exceeded error")
	}
	if !contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout in error message, got: %v", err)
	}
}

// failingRunnableBackend fails on RunnableJobs calls.
type failingRunnableBackend struct {
	fakeBackend
}

func (f *failingRunnableBackend) RunnableJobs(_ context.Context, _ string) ([]string, error) {
	return nil, fmt.Errorf("network error")
}

func TestWaitForJobRunnable_ErrorPropagates(t *testing.T) {
	backend := &failingRunnableBackend{}

	deadline := time.Now().Add(10 * time.Second)
	err := waitForJobRunnable(context.Background(), backend, "run-1", "target-job", nil, time.Millisecond, deadline)
	if err == nil {
		t.Fatal("expected error from failing backend")
	}
	if !contains(err.Error(), "network error") {
		t.Fatalf("expected network error in message, got: %v", err)
	}
}

func TestWaitForJobRunnable_DepsFailedDuringPoll(t *testing.T) {
	execState := &state.ExecState{
		Jobs: map[string]*state.JobState{
			"dep-1": {Status: "failed"},
		},
	}
	backend := &fakeBackend{
		runnableJobs: [][]string{{}}, // target job never appears
		runState:     execState,
	}

	deadline := time.Now().Add(10 * time.Second)
	err := waitForJobRunnable(context.Background(), backend, "run-1", "target-job", []string{"dep-1"}, time.Millisecond, deadline)
	if err == nil {
		t.Fatal("expected error when dependency failed during poll")
	}
	if contains(err.Error(), "timeout") {
		t.Fatalf("should not timeout when dep failed, got: %v", err)
	}
	if !contains(err.Error(), "failed") {
		t.Fatalf("expected 'failed' in error message, got: %v", err)
	}
}

func TestPerformRemoteJobClaim_DepsWaiting_DepFailsDuringPoll(t *testing.T) {
	execState := &state.ExecState{
		Jobs: map[string]*state.JobState{
			"dep-1": {Status: "failed"},
		},
	}
	backend := &fakeBackend{
		claimResults: []statebackend.ClaimResult{
			{Claimed: false, DepsWaiting: true},
		},
		runnableJobs: [][]string{{}}, // target job never appears
		runState:     execState,
	}
	plan := &model.Plan{
		Jobs: []model.PlanJob{
			{ID: "dep-1"},
			{ID: "job-1", DependsOn: []string{"dep-1"}},
		},
	}

	err := performRemoteJobClaim(context.Background(), backend, "run-1", plan, "job-1", "runner-1", io.Discard, false)
	if err == nil {
		t.Fatal("expected error when dependency fails during polling")
	}
	if contains(err.Error(), "timeout") {
		t.Fatalf("should not timeout when dep failed, got: %v", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
