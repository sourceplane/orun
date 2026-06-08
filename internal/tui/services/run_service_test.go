package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/model"
)

// TestRunnerNameForPlan is the regression guard for the cockpit run defect: a
// component whose steps are authored as GitHub Actions (uses:) must run under
// the github-actions emulator, not the local shell executor (which rejects
// uses: steps with empty output — failing the run and writing no logs).
func TestRunnerNameForPlan(t *testing.T) {
	local := &model.Plan{Jobs: []model.PlanJob{
		{ID: "j1", Steps: []model.PlanStep{{ID: "s1", Run: "echo hi"}}},
	}}
	if got := runnerNameForPlan(local); got != "local" {
		t.Errorf("run:-only plan = %q, want local", got)
	}

	gha := &model.Plan{Jobs: []model.PlanJob{
		{ID: "j1", Steps: []model.PlanStep{
			{ID: "s1", Run: "echo hi"},
			{ID: "s2", Use: "actions/setup-node@v4"},
		}},
	}}
	if got := runnerNameForPlan(gha); got != "github-actions" {
		t.Errorf("plan with a uses: step = %q, want github-actions", got)
	}

	if got := runnerNameForPlan(nil); got != "local" {
		t.Errorf("nil plan = %q, want local", got)
	}

	// The runtime context must match the chosen runner so executor + runtime
	// stay consistent.
	if rt := runtimeContextForRunner("github-actions"); rt.Runner != "github-actions" || rt.Environment != "ci" {
		t.Errorf("github-actions runtime = %+v, want runner=github-actions env=ci", rt)
	}
	if rt := runtimeContextForRunner("local"); rt.Runner != "local" || rt.Environment != "local" {
		t.Errorf("local runtime = %+v, want runner=local env=local", rt)
	}
}

func TestValidateRunRequest_RejectsNilPlan(t *testing.T) {
	err := validateRunRequest(RunRequest{DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "req.Plan is required") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRunRequest_AllowsRealRun(t *testing.T) {
	if err := validateRunRequest(RunRequest{Plan: &model.Plan{}, DryRun: false}); err != nil {
		t.Fatalf("real (non-dry) run should be allowed, got %v", err)
	}
}

func TestValidateRunRequest_RejectsRemoteState(t *testing.T) {
	err := validateRunRequest(RunRequest{Plan: &model.Plan{}, DryRun: true, RemoteState: true})
	if err == nil || !strings.Contains(err.Error(), "RemoteState=true is not supported") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRunRequest_RejectsRemoteStateRealRun(t *testing.T) {
	err := validateRunRequest(RunRequest{Plan: &model.Plan{}, DryRun: false, RemoteState: true})
	if err == nil || !strings.Contains(err.Error(), "RemoteState=true is not supported") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRunRequest_HappyPath(t *testing.T) {
	if err := validateRunRequest(RunRequest{Plan: &model.Plan{}, DryRun: true}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestLiveOrunService_RunPlan_RejectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc := NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: orunDir(t.TempDir())})
	if _, err := svc.RunPlan(ctx, RunRequest{Plan: &model.Plan{}, DryRun: true}); err == nil {
		t.Fatal("expected ctx.Err()")
	}
}

func TestLiveOrunService_RunPlan_FailsClosedOnRemoteState(t *testing.T) {
	svc := NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: orunDir(t.TempDir())})
	_, err := svc.RunPlan(context.Background(), RunRequest{
		Plan:        &model.Plan{},
		DryRun:      true,
		RemoteState: true,
	})
	if err == nil {
		t.Fatal("expected fail-closed error for RemoteState=true")
	}
}

func TestLiveOrunService_RunPlan_EmptyPlanEmitsRunDone(t *testing.T) {
	svc := NewLiveOrunService(LiveServiceConfig{
		IntentRoot:      t.TempDir(),
		ObjectModelRoot: orunDir(t.TempDir()),
	})
	plan := &model.Plan{
		Metadata: model.PlanMetadata{Name: "empty"},
		Jobs:     nil,
	}
	ch, err := svc.RunPlan(context.Background(), RunRequest{Plan: plan, DryRun: true})
	if err != nil {
		t.Fatalf("RunPlan: %v", err)
	}

	var got []RunEvent
	timeout := time.After(2 * time.Second)
loop:
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				break loop
			}
			got = append(got, ev)
		case <-timeout:
			t.Fatalf("timed out waiting for events; got=%v", got)
		}
	}
	if len(got) == 0 {
		t.Fatal("expected at least one event")
	}
	last := got[len(got)-1]
	if last.Kind != RunEventRunDone {
		t.Fatalf("last event = %v, want RunEventRunDone", last.Kind)
	}
	if last.ExecID == "" {
		t.Fatal("run_done event should carry the resolved ExecID")
	}
}

// TestLiveOrunService_RunPlan_RealRunStreamsAndPersistsLogs is the
// regression guard for the cockpit's core defect: a real (non-dry) run
// from the TUI must actually execute, emit per-job events stamped with the
// execution ID, and seal a native ExecutionRun whose per-step logs the log
// tail can replay from the object graph.
func TestLiveOrunService_RunPlan_RealRunStreamsAndPersistsLogs(t *testing.T) {
	workDir := t.TempDir()
	omDir := t.TempDir()
	svc := NewLiveOrunService(LiveServiceConfig{IntentRoot: workDir, ObjectModelRoot: orunDir(omDir)})

	plan := &model.Plan{
		Metadata: model.PlanMetadata{Name: "realrun"},
		Jobs: []model.PlanJob{{
			ID:          "demo.build",
			Name:        "build",
			Component:   "demo",
			Environment: "local",
			Steps: []model.PlanStep{{
				ID:   "echo",
				Name: "echo",
				Run:  "echo hello-from-real-run",
			}},
		}},
	}

	ch, err := svc.RunPlan(context.Background(), RunRequest{Plan: plan, DryRun: false})
	if err != nil {
		t.Fatalf("RunPlan: %v", err)
	}

	var got []RunEvent
	timeout := time.After(10 * time.Second)
loop:
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				break loop
			}
			got = append(got, ev)
		case <-timeout:
			t.Fatalf("timed out; got=%v", got)
		}
	}

	var execID string
	var started, completed, done bool
	for _, ev := range got {
		if ev.ExecID != "" {
			execID = ev.ExecID
		}
		switch ev.Kind {
		case RunEventJobStarted:
			started = true
		case RunEventJobCompleted:
			completed = true
		case RunEventRunDone:
			done = true
			if ev.Status != "completed" {
				t.Fatalf("run_done status = %q, want completed (events=%v)", ev.Status, got)
			}
		}
	}
	if !started || !completed || !done {
		t.Fatalf("missing lifecycle events: started=%v completed=%v done=%v (events=%v)",
			started, completed, done, got)
	}
	if execID == "" {
		t.Fatal("events should carry a non-empty ExecID")
	}

	// The run sealed a native ExecutionRun: it must surface in history and its
	// per-step log must be replayable from the object graph (the deferred
	// "TUI object-model log persistence" item, now closed).
	runs, err := svc.ListRuns(context.Background(), ListRunsRequest{})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	var found bool
	for _, r := range runs {
		if r.ExecID == execID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("sealed run %s not in history %+v", execID, runs)
	}

	ch2, err := svc.TailLogs(context.Background(), LogRequest{ExecID: execID, JobID: "demo.build"})
	if err != nil {
		t.Fatalf("TailLogs: %v", err)
	}
	var logBody string
	for ev := range ch2 {
		logBody += ev.Line + "\n"
	}
	if !strings.Contains(logBody, "hello-from-real-run") {
		t.Fatalf("sealed step log missing expected output; got %q", logBody)
	}
}
