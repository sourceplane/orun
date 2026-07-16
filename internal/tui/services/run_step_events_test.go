package services

import (
	"context"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/model"
)

// TestRunPlanEmitsStepEvents pins the step-level stream (specs/orun-tui-v2
// §8): a dispatched run emits step_started/step_completed between its job
// events, so live views never re-read the working tree on a timer.
func TestRunPlanEmitsStepEvents(t *testing.T) {
	svc := NewLiveOrunService(LiveServiceConfig{IntentRoot: t.TempDir()})
	plan := &model.Plan{
		Metadata: model.PlanMetadata{Name: "step-events"},
		Jobs: []model.PlanJob{{
			ID: "a@deploy", Name: "a", Component: "a", Environment: "dev",
			Steps: []model.PlanStep{
				{ID: "build", Name: "build", Run: "true"},
				{ID: "test", Name: "test", Run: "true"},
			},
		}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ch, err := svc.RunPlan(ctx, RunRequest{Plan: plan, DryRun: true})
	if err != nil {
		t.Fatalf("RunPlan: %v", err)
	}

	var kinds []RunEventKind
	steps := map[string]string{}
	for ev := range ch {
		kinds = append(kinds, ev.Kind)
		if ev.Kind == RunEventStepStarted || ev.Kind == RunEventStepCompleted {
			if ev.StepID == "" || ev.JobID != "a@deploy" || ev.Component != "a" {
				t.Fatalf("step event missing identity: %+v", ev)
			}
			steps[ev.StepID+"/"+string(ev.Kind)] = ev.Status
		}
	}

	for _, want := range []string{
		"build/" + string(RunEventStepStarted),
		"build/" + string(RunEventStepCompleted),
		"test/" + string(RunEventStepStarted),
		"test/" + string(RunEventStepCompleted),
	} {
		if _, ok := steps[want]; !ok {
			t.Fatalf("missing %s (kinds seen: %v)", want, kinds)
		}
	}
	if steps["build/"+string(RunEventStepCompleted)] != "completed" {
		t.Fatalf("dry-run step status = %q", steps["build/"+string(RunEventStepCompleted)])
	}
}
