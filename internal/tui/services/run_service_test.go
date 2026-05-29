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

func TestValidateRunRequest_RejectsNonDryRun(t *testing.T) {
	err := validateRunRequest(RunRequest{Plan: &model.Plan{}, DryRun: false})
	if err == nil || !strings.Contains(err.Error(), "only DryRun=true") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRunRequest_RejectsRemoteState(t *testing.T) {
	err := validateRunRequest(RunRequest{Plan: &model.Plan{}, DryRun: true, RemoteState: true})
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
}
