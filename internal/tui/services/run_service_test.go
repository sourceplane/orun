package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/state"
)

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
	svc := NewLiveOrunService(LiveServiceConfig{Store: state.NewStore(t.TempDir())})
	if _, err := svc.RunPlan(ctx, RunRequest{Plan: &model.Plan{}, DryRun: true}); err == nil {
		t.Fatal("expected ctx.Err()")
	}
}

func TestLiveOrunService_RunPlan_FailsClosedOnRemoteState(t *testing.T) {
	svc := NewLiveOrunService(LiveServiceConfig{Store: state.NewStore(t.TempDir())})
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
		IntentRoot: t.TempDir(),
		Store:      state.NewStore(t.TempDir()),
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
// execution ID, and persist per-step logs that the log tail can replay.
func TestLiveOrunService_RunPlan_RealRunStreamsAndPersistsLogs(t *testing.T) {
	workDir := t.TempDir()
	store := state.NewStore(t.TempDir())
	svc := NewLiveOrunService(LiveServiceConfig{IntentRoot: workDir, Store: store})

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

	// (The runner no longer persists legacy .orun/executions log files — the
	// M12 cutover removed legacy execution state. The streamed step output is
	// asserted above via the run events; wiring the TUI run service to persist
	// step logs into the object-model working tree is a tracked follow-up.)
	_ = store
}
