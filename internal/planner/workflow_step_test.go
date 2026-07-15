package planner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/render"
	"github.com/sourceplane/orun/internal/workflowbackend"
)

// legacyInstance is a component instance with no profile — resolveJobsForProfile
// then uses the composition's DefaultJob with all steps.
func legacyInstance(compType string) map[string][]*model.ComponentInstance {
	return map[string][]*model.ComponentInstance{
		"prod": {{
			ComponentName: "api",
			Environment:   "prod",
			Type:          compType,
			Enabled:       true,
		}},
	}
}

func compositionWith(steps ...model.Step) map[string]*CompositionInfo {
	job := &model.JobSpec{Name: "deploy", Steps: steps}
	return map[string]*CompositionInfo{
		"svc": {Type: "svc", DefaultJob: job, JobMap: map[string]*model.JobSpec{"deploy": job}},
	}
}

func TestPlanJobs_WorkflowStepPinsDigest(t *testing.T) {
	dir := t.TempDir()
	body := []byte("apiVersion: torkflow/v1\nkind: Workflow\nmetadata: { name: notify }\n")
	if err := os.WriteFile(filepath.Join(dir, "notify.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	jp := NewJobPlanner(compositionWith(model.Step{
		Name:     "notify",
		Workflow: "notify.yaml",
		With:     map[string]interface{}{"channel": "ops"},
	}))
	jp.WorkflowBaseDir = dir

	jobInstances, err := jp.PlanJobs(legacyInstance("svc"))
	if err != nil {
		t.Fatalf("PlanJobs: %v", err)
	}
	plan := render.NewRenderer().RenderPlan(model.Metadata{Name: "p"}, jobInstances, nil)
	if len(plan.Jobs) != 1 || len(plan.Jobs[0].Steps) != 1 {
		t.Fatalf("expected 1 job / 1 step, got %d jobs", len(plan.Jobs))
	}
	step := plan.Jobs[0].Steps[0]
	if step.Workflow != "notify.yaml" {
		t.Errorf("plan step workflow = %q, want notify.yaml", step.Workflow)
	}
	if want := workflowbackend.DigestBytes(body); step.WorkflowDigest != want {
		t.Errorf("workflowDigest = %q, want %q", step.WorkflowDigest, want)
	}
	if step.Run != "" || step.Use != "" {
		t.Errorf("workflow step should not carry run/use: run=%q use=%q", step.Run, step.Use)
	}
}

func TestPlanJobs_WorkflowPlanIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wf.yaml"), []byte("apiVersion: torkflow/v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	build := func() string {
		jp := NewJobPlanner(compositionWith(model.Step{Name: "s", Workflow: "wf.yaml"}))
		jp.WorkflowBaseDir = dir
		ji, err := jp.PlanJobs(legacyInstance("svc"))
		if err != nil {
			t.Fatalf("PlanJobs: %v", err)
		}
		plan := render.NewRenderer().RenderPlan(model.Metadata{Name: "p"}, ji, nil)
		return plan.Metadata.Checksum
	}
	if a, b := build(), build(); a == "" || a != b {
		t.Fatalf("plan checksum not deterministic: %q vs %q", a, b)
	}
}

func TestPlanJobs_MutualExclusionIsCompileError(t *testing.T) {
	jp := NewJobPlanner(compositionWith(model.Step{
		Name:     "bad",
		Run:      "echo hi",
		Workflow: "wf.yaml",
	}))
	jp.WorkflowBaseDir = t.TempDir()
	if _, err := jp.PlanJobs(legacyInstance("svc")); err == nil {
		t.Fatalf("expected compile error for a step setting both run and workflow")
	}
}

func TestPlanJobs_MissingWorkflowFileIsCompileError(t *testing.T) {
	jp := NewJobPlanner(compositionWith(model.Step{Name: "s", Workflow: "absent.yaml"}))
	jp.WorkflowBaseDir = t.TempDir()
	if _, err := jp.PlanJobs(legacyInstance("svc")); err == nil {
		t.Fatalf("expected compile error for an unresolvable workflow reference")
	}
}

func TestPinWorkflowDigest(t *testing.T) {
	jp := &JobPlanner{}
	// No base dir: reference materialized without a digest.
	if d, err := jp.pinWorkflowDigest("s", "wf.yaml"); err != nil || d != "" {
		t.Fatalf("no base dir should yield empty digest: %q err %v", d, err)
	}
	// Empty reference: empty digest.
	jp.WorkflowBaseDir = t.TempDir()
	if d, err := jp.pinWorkflowDigest("s", ""); err != nil || d != "" {
		t.Fatalf("empty workflow should yield empty digest: %q err %v", d, err)
	}
}
