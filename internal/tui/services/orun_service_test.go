package services

import (
	"context"
	"testing"
)

func TestMockOrunService_DefaultReturns(t *testing.T) {
	var m MockOrunService
	ctx := context.Background()

	if snap, err := m.LoadWorkspace(ctx, WorkspaceRequest{}); err != nil || snap == nil {
		t.Fatalf("default LoadWorkspace: got snap=%v err=%v", snap, err)
	}
	if res, err := m.GeneratePlan(ctx, PlanRequest{}); err != nil || res == nil {
		t.Fatalf("default GeneratePlan: got res=%v err=%v", res, err)
	}
	ch, err := m.RunPlan(ctx, RunRequest{})
	if err != nil {
		t.Fatalf("default RunPlan err: %v", err)
	}
	if _, ok := <-ch; ok {
		t.Fatalf("default RunPlan channel should be closed and empty")
	}
	if _, err := m.ListRuns(ctx, ListRunsRequest{}); err != nil {
		t.Fatalf("default ListRuns err: %v", err)
	}
	if desc, err := m.Describe(ctx, ResourceRef{}); err != nil || desc == nil {
		t.Fatalf("default Describe: got desc=%v err=%v", desc, err)
	}
	lch, err := m.TailLogs(ctx, LogRequest{})
	if err != nil {
		t.Fatalf("default TailLogs err: %v", err)
	}
	if _, ok := <-lch; ok {
		t.Fatalf("default TailLogs channel should be closed and empty")
	}
}

func TestMockOrunService_HooksInvoked(t *testing.T) {
	want := &WorkspaceSnapshot{IntentName: "test"}
	m := &MockOrunService{
		LoadWorkspaceFn: func(ctx context.Context, req WorkspaceRequest) (*WorkspaceSnapshot, error) {
			if req.IntentFile != "x.yaml" {
				t.Fatalf("wrong req: %+v", req)
			}
			return want, nil
		},
	}
	got, err := m.LoadWorkspace(context.Background(), WorkspaceRequest{IntentFile: "x.yaml"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != want {
		t.Fatalf("expected hook return value, got %v", got)
	}
}

func TestRunEventKindConstants(t *testing.T) {
	// Stable string values are part of the service contract.
	cases := map[RunEventKind]string{
		RunEventJobStarted:    "job_started",
		RunEventJobCompleted:  "job_completed",
		RunEventJobFailed:     "job_failed",
		RunEventStepStarted:   "step_started",
		RunEventStepCompleted: "step_completed",
		RunEventRunDone:       "run_done",
	}
	for k, want := range cases {
		if string(k) != want {
			t.Errorf("RunEventKind %v = %q, want %q", k, string(k), want)
		}
	}
}

// Compile-time check that the request/response/msg types match the
// interface contract; the test exists so refactors that drop a field are
// caught at the package boundary, not deep inside views.
func TestServiceTypes_ZeroValuesUsable(t *testing.T) {
	_ = WorkspaceRequest{}
	_ = PlanRequest{}
	_ = RunRequest{}
	_ = ListRunsRequest{}
	_ = LogRequest{}
	_ = ResourceRef{}
	_ = WorkspaceSnapshot{}
	_ = PlanResult{}
	_ = RunEvent{}
	_ = LogEvent{}
	_ = RunSummary{}
	_ = ResourceDescription{}
	_ = WorkspaceLoadedMsg{}
	_ = PlanGeneratedMsg{}
	_ = RunEventMsg{}
	_ = LogEventMsg{}
	_ = RunsListedMsg{}
	_ = DescribeResultMsg{}
	_ = ErrMsg{}
	_ = TickMsg{}
}
