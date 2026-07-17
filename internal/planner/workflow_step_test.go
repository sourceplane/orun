package planner

import (
	"os"
	"path/filepath"
	"strings"
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

func TestPinWorkflow(t *testing.T) {
	jp := &JobPlanner{}
	// No base dir: reference materialized without a digest or inspection.
	if _, d, _, err := jp.pinWorkflow("s", "wf.yaml", ""); err != nil || d != "" {
		t.Fatalf("no base dir should yield empty digest: %q err %v", d, err)
	}
	// Empty reference: empty digest.
	jp.WorkflowBaseDir = t.TempDir()
	if _, d, _, err := jp.pinWorkflow("s", "", ""); err != nil || d != "" {
		t.Fatalf("empty workflow should yield empty digest: %q err %v", d, err)
	}
}

func TestPlanJobs_StackShippedWorkflowResolvesAndMaterializes(t *testing.T) {
	intentDir := t.TempDir()
	sourceDir := t.TempDir() // the composition Stack's resolved root
	body := []byte("apiVersion: torkflow/v1\nkind: Workflow\nmetadata: { name: packaged }\n")
	if err := os.MkdirAll(filepath.Join(sourceDir, "wf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "wf", "notify.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	comps := compositionWith(model.Step{Name: "notify", Workflow: "wf/notify.yaml"})
	comps["svc"].SourceDir = sourceDir
	jp := NewJobPlanner(comps)
	jp.WorkflowBaseDir = intentDir // NOT carrying the workflow

	ji, err := jp.PlanJobs(legacyInstance("svc"))
	if err != nil {
		t.Fatalf("Stack-shipped workflow should resolve via the source root: %v", err)
	}
	plan := render.NewRenderer().RenderPlan(model.Metadata{Name: "p"}, ji, nil)
	step := plan.Jobs[0].Steps[0]

	// The pin is source-agnostic: same bytes, same digest as a local copy (S-7).
	if want := workflowbackend.DigestBytes(body); step.WorkflowDigest != want {
		t.Fatalf("digest parity broken: %q want %q", step.WorkflowDigest, want)
	}
	// The reference is rewritten to a content-addressed workspace path …
	if !strings.HasPrefix(step.Workflow, ".orun/workflows/") {
		t.Fatalf("packaged workflow not rewritten into the workspace: %q", step.Workflow)
	}
	// … and the bytes are materialized there for the runner.
	materialized, err := os.ReadFile(filepath.Join(intentDir, filepath.FromSlash(step.Workflow)))
	if err != nil || string(materialized) != string(body) {
		t.Fatalf("materialized copy missing or diverged: %v", err)
	}
	// Determinism: recompiling yields the same reference and digest.
	ji2, err := jp.PlanJobs(legacyInstance("svc"))
	if err != nil {
		t.Fatal(err)
	}
	plan2 := render.NewRenderer().RenderPlan(model.Metadata{Name: "p"}, ji2, nil)
	if plan2.Jobs[0].Steps[0].Workflow != step.Workflow || plan2.Metadata.Checksum != plan.Metadata.Checksum {
		t.Fatalf("packaged resolution not deterministic")
	}
}

func TestPlanJobs_LocalWorkflowWinsOverPackaged(t *testing.T) {
	intentDir := t.TempDir()
	sourceDir := t.TempDir()
	local := []byte("apiVersion: torkflow/v1\nkind: Workflow\nmetadata: { name: local }\n")
	packaged := []byte("apiVersion: torkflow/v1\nkind: Workflow\nmetadata: { name: packaged }\n")
	if err := os.WriteFile(filepath.Join(intentDir, "wf.yaml"), local, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "wf.yaml"), packaged, 0o644); err != nil {
		t.Fatal(err)
	}
	comps := compositionWith(model.Step{Name: "s", Workflow: "wf.yaml"})
	comps["svc"].SourceDir = sourceDir
	jp := NewJobPlanner(comps)
	jp.WorkflowBaseDir = intentDir

	ji, err := jp.PlanJobs(legacyInstance("svc"))
	if err != nil {
		t.Fatal(err)
	}
	plan := render.NewRenderer().RenderPlan(model.Metadata{Name: "p"}, ji, nil)
	step := plan.Jobs[0].Steps[0]
	if step.Workflow != "wf.yaml" || step.WorkflowDigest != workflowbackend.DigestBytes(local) {
		t.Fatalf("the repo's own workflow must win over the packaged one: %+v", step)
	}
}

const connWorkflow = `apiVersion: torkflow/v1
kind: Workflow
metadata: { name: notify }
spec:
  steps:
    - name: Notify
      actionRef: chat.postMessage
      connection: chat-main
`

func TestPlanJobs_GrantMaterializesIntoPlan(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wf.yaml"), []byte(connWorkflow), 0o644); err != nil {
		t.Fatal(err)
	}
	jp := NewJobPlanner(compositionWith(model.Step{
		Name:     "notify",
		Workflow: "wf.yaml",
		Connections: map[string]map[string]string{
			"chat-main": {"token": "secret://acme/api/prod/CHAT_TOKEN"},
		},
	}))
	jp.WorkflowBaseDir = dir

	ji, err := jp.PlanJobs(legacyInstance("svc"))
	if err != nil {
		t.Fatalf("PlanJobs: %v", err)
	}
	plan := render.NewRenderer().RenderPlan(model.Metadata{Name: "p"}, ji, nil)
	got := plan.Jobs[0].Steps[0].Connections
	if got["chat-main"]["token"] != "secret://acme/api/prod/CHAT_TOKEN" {
		t.Fatalf("grant not materialized into the plan: %#v", got)
	}
}

func TestPlanJobs_MissingGrantIsCompileError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wf.yaml"), []byte(connWorkflow), 0o644); err != nil {
		t.Fatal(err)
	}
	jp := NewJobPlanner(compositionWith(model.Step{Name: "notify", Workflow: "wf.yaml"}))
	jp.WorkflowBaseDir = dir
	_, err := jp.PlanJobs(legacyInstance("svc"))
	if err == nil {
		t.Fatalf("a workflow declaring connections must fail to compile without a grant")
	}
	// S-8: the error writes the migration for you.
	if !strings.Contains(err.Error(), "connections:") || !strings.Contains(err.Error(), "chat-main") {
		t.Fatalf("compile error should print the block to paste, got: %v", err)
	}
}

const outputsWF = `apiVersion: torkflow/v1
kind: Workflow
metadata: { name: oncall }
spec:
  outputs:
    email: "{{ Steps.Get.user.email }}"
  steps:
    - name: Get
      actionRef: core.js
`

func outputsFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "oncall.yaml"), []byte(outputsWF), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPlanJobs_ValidOutputRefCompiles(t *testing.T) {
	dir := outputsFixture(t)
	jp := NewJobPlanner(compositionWith(
		model.Step{Name: "get-oncall", Workflow: "oncall.yaml"},
		model.Step{Name: "page", Run: "./page.sh ${{ steps.get-oncall.outputs.email }}"},
	))
	jp.WorkflowBaseDir = dir
	ji, err := jp.PlanJobs(legacyInstance("svc"))
	if err != nil {
		t.Fatalf("valid output reference should compile: %v", err)
	}
	// The reference survives compile-time templating intact for the runner.
	plan := render.NewRenderer().RenderPlan(model.Metadata{Name: "p"}, ji, nil)
	if !strings.Contains(plan.Jobs[0].Steps[1].Run, "${{ steps.get-oncall.outputs.email }}") {
		t.Fatalf("output reference must survive compile: %q", plan.Jobs[0].Steps[1].Run)
	}
}

func TestPlanJobs_UndeclaredOutputRefIsCompileError(t *testing.T) {
	dir := outputsFixture(t)
	jp := NewJobPlanner(compositionWith(
		model.Step{Name: "get-oncall", Workflow: "oncall.yaml"},
		model.Step{Name: "page", Run: "./page.sh ${{ steps.get-oncall.outputs.phone }}"},
	))
	jp.WorkflowBaseDir = dir
	if _, err := jp.PlanJobs(legacyInstance("svc")); err == nil {
		t.Fatalf("a reference to an undeclared output must fail compilation")
	}
}

func TestPlanJobs_OutputRefToLaterOrUnknownStepIsCompileError(t *testing.T) {
	dir := outputsFixture(t)
	// Reference points at a step that comes LATER in the job.
	jp := NewJobPlanner(compositionWith(
		model.Step{Name: "page", Run: "./page.sh ${{ steps.get-oncall.outputs.email }}"},
		model.Step{Name: "get-oncall", Workflow: "oncall.yaml"},
	))
	jp.WorkflowBaseDir = dir
	if _, err := jp.PlanJobs(legacyInstance("svc")); err == nil {
		t.Fatalf("a reference to a later step must fail compilation")
	}
}

func TestPlanJobs_StaleGrantIsCompileError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wf.yaml"), []byte(connWorkflow), 0o644); err != nil {
		t.Fatal(err)
	}
	jp := NewJobPlanner(compositionWith(model.Step{
		Name:     "notify",
		Workflow: "wf.yaml",
		Connections: map[string]map[string]string{
			"chat-main": {"token": "secret://acme/api/prod/CHAT_TOKEN"},
			"misspeled": {"token": "secret://acme/api/prod/OTHER"},
		},
	}))
	jp.WorkflowBaseDir = dir
	if _, err := jp.PlanJobs(legacyInstance("svc")); err == nil {
		t.Fatalf("a grant naming an undeclared connection must fail to compile")
	}
}
