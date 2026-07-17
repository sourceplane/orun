package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/workflowbackend"
)

type fakeWFEngine struct {
	res    workflowbackend.Result
	err    error
	gotReq workflowbackend.Request
}

func (f *fakeWFEngine) Digest() string { return "sha256:fake" }
func (f *fakeWFEngine) Invoke(_ context.Context, req workflowbackend.Request) (workflowbackend.Result, error) {
	f.gotReq = req
	return f.res, f.err
}

func writeWF(t *testing.T) (dir, digest string) {
	t.Helper()
	dir = t.TempDir()
	body := []byte("apiVersion: torkflow/v1\nkind: Workflow\n")
	if err := os.WriteFile(filepath.Join(dir, "wf.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, workflowbackend.DigestBytes(body)
}

func TestRunWorkflowStepSuccess(t *testing.T) {
	dir, digest := writeWF(t)
	r := &Runner{WorkflowEngine: &fakeWFEngine{res: workflowbackend.Result{
		Status:  workflowbackend.StatusSuccess,
		Steps:   []workflowbackend.StepResult{{Name: "notify", Status: "success"}},
		Outputs: map[string]any{"prUrl": "https://example/pr/1"},
	}}}
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	step := model.PlanStep{Name: "s", Workflow: "wf.yaml", WorkflowDigest: digest}

	out, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "web@prod.deploy"}, step)
	if err != nil {
		t.Fatalf("runWorkflowStep: %v", err)
	}
	if !strings.Contains(out, "success") || !strings.Contains(out, "notify") {
		t.Fatalf("summary missing status/steps: %q", out)
	}
}

func TestRunWorkflowStepFailureIsError(t *testing.T) {
	dir, digest := writeWF(t)
	r := &Runner{WorkflowEngine: &fakeWFEngine{res: workflowbackend.Result{
		Status: workflowbackend.StatusFailed,
		Error:  "provider 4xx",
	}}}
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	step := model.PlanStep{Name: "s", Workflow: "wf.yaml", WorkflowDigest: digest}

	out, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step)
	if err == nil {
		t.Fatalf("a failed workflow step must return an error")
	}
	if !strings.Contains(out, "provider 4xx") {
		t.Fatalf("expected the failure summary to be returned as output, got %q", out)
	}
}

func TestRunWorkflowStepDigestMismatchIsError(t *testing.T) {
	dir, _ := writeWF(t)
	r := &Runner{WorkflowEngine: &fakeWFEngine{res: workflowbackend.Result{Status: workflowbackend.StatusSuccess}}}
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	// Pin a stale digest: the on-disk file no longer matches.
	step := model.PlanStep{Name: "s", Workflow: "wf.yaml", WorkflowDigest: "sha256:stale"}

	if _, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step); err == nil {
		t.Fatalf("expected an error when the pinned digest is stale")
	}
}

func TestRunWorkflowStepUnconfiguredEngineIsError(t *testing.T) {
	dir, digest := writeWF(t)
	t.Setenv(workflowbackend.EngineEnv, "") // no engine available
	r := &Runner{}                          // no injected engine
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	step := model.PlanStep{Name: "s", Workflow: "wf.yaml", WorkflowDigest: digest}

	if _, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step); err == nil {
		t.Fatalf("expected a clear error when no workflow engine is configured")
	}
}

func TestRunWorkflowStepGrantInjectsMappedOnly(t *testing.T) {
	dir, digest := writeWF(t)
	eng := &fakeWFEngine{res: workflowbackend.Result{Status: workflowbackend.StatusSuccess}}
	r := &Runner{WorkflowEngine: eng}
	const tokenRef = "secret://acme/api/prod/GITHUB_TOKEN"
	job := model.PlanJob{
		ID: "j",
		SecretRefs: []model.PlanSecretRef{
			{AsEnv: "GITHUB_TOKEN", Ref: tokenRef},
			{AsEnv: "UNRELATED", Ref: "secret://acme/api/prod/UNRELATED"},
		},
	}
	ec := executor.ExecContext{
		Context:      context.Background(),
		WorkspaceDir: dir,
		SecretEnv: map[string]string{
			"GITHUB_TOKEN": "ghp_s3cr3t",
			"UNRELATED":    "canary-value", // resolved for the job, NOT granted
		},
	}
	step := model.PlanStep{
		Name: "open-pr", Workflow: "wf.yaml", WorkflowDigest: digest,
		Connections: map[string]map[string]string{
			"vcs-app": {"token": tokenRef},
		},
	}

	out, _, err := r.runWorkflowStep(ec, job, step)
	if err != nil {
		t.Fatalf("runWorkflowStep: %v", err)
	}
	payload, ok := eng.gotReq.Connections["vcs-app"].(map[string]any)
	if !ok || payload["token"] != "ghp_s3cr3t" {
		t.Fatalf("granted credential not injected under its connection name: %#v", eng.gotReq.Connections)
	}
	// Invariant 10: the unmapped secret provably does not cross.
	blob, _ := json.Marshal(eng.gotReq)
	if strings.Contains(string(blob), "canary-value") || strings.Contains(string(blob), "UNRELATED") {
		t.Fatalf("ungranted secret crossed the boundary: %s", blob)
	}
	if strings.Contains(out, "ghp_s3cr3t") {
		t.Fatalf("credential leaked into the sealed workflow output: %q", out)
	}
}

func TestBuildConnectionPayloadsFailClosed(t *testing.T) {
	job := model.PlanJob{SecretRefs: []model.PlanSecretRef{{AsEnv: "T", Ref: "secret://a/b/c/T"}}}
	// A grant referencing a secret the job does not carry is a launch error.
	step := model.PlanStep{Name: "s", Connections: map[string]map[string]string{
		"conn": {"token": "secret://a/b/c/ABSENT"},
	}}
	if _, err := buildConnectionPayloads(job, step, map[string]string{"T": "v"}); err == nil {
		t.Fatalf("expected error for a grant outside the job's secretRefs")
	}
	// A granted-but-unresolved secret is a launch error too.
	step.Connections["conn"]["token"] = "secret://a/b/c/T"
	if _, err := buildConnectionPayloads(job, step, map[string]string{}); err == nil {
		t.Fatalf("expected error for an unresolved granted secret")
	}
	// No grant → nil payloads, no error.
	if got, err := buildConnectionPayloads(job, model.PlanStep{Name: "s"}, nil); err != nil || got != nil {
		t.Fatalf("no grant should yield nil: %v %v", got, err)
	}
}

func TestWorkflowEnginePinVerification(t *testing.T) {
	dir, digest := writeWF(t)
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	step := model.PlanStep{Name: "s", Workflow: "wf.yaml", WorkflowDigest: digest}

	// Matching pin: runs.
	okRunner := &Runner{
		WorkflowEngine:    &fakeWFEngine{res: workflowbackend.Result{Status: workflowbackend.StatusSuccess}},
		WorkflowEnginePin: "sha256:fake", // fakeWFEngine.Digest()
	}
	if _, _, err := okRunner.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step); err != nil {
		t.Fatalf("matching pin should run: %v", err)
	}

	// Mismatched pin: fail closed, engine never invoked.
	eng := &fakeWFEngine{res: workflowbackend.Result{Status: workflowbackend.StatusSuccess}}
	badRunner := &Runner{WorkflowEngine: eng, WorkflowEnginePin: "sha256:other"}
	_, _, err := badRunner.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step)
	if err == nil || !strings.Contains(err.Error(), "pin") {
		t.Fatalf("mismatched pin must fail closed: %v", err)
	}
	if eng.gotReq.Workflow != "" {
		t.Fatalf("engine must not be invoked under a mismatched pin")
	}
}

func TestWorkflowRunDirProvisioned(t *testing.T) {
	dir, digest := writeWF(t)
	eng := &fakeWFEngine{res: workflowbackend.Result{Status: workflowbackend.StatusSuccess}}
	r := &Runner{WorkflowEngine: eng, ExecID: "exec-1"}
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	step := model.PlanStep{Name: "s", Workflow: "wf.yaml", WorkflowDigest: digest}

	if _, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "web@prod.deploy"}, step); err != nil {
		t.Fatal(err)
	}
	if eng.gotReq.RunDir == "" || !strings.Contains(eng.gotReq.RunDir, ".orun") {
		t.Fatalf("runDir not provisioned under .orun: %q", eng.gotReq.RunDir)
	}
	if _, err := os.Stat(eng.gotReq.RunDir); err != nil {
		t.Fatalf("runDir does not exist: %v", err)
	}
}

func TestSubstituteWorkflowOutputs(t *testing.T) {
	outputs := map[string]map[string]any{
		"get-oncall": {"email": "sam@acme.dev"},
	}
	step := model.PlanStep{
		Name: "page",
		Run:  "./page.sh ${{ steps.get-oncall.outputs.email }}",
		Env:  map[string]interface{}{"ONCALL": "${{ steps.get-oncall.outputs.email }}", "N": 1},
	}
	got, err := substituteWorkflowOutputs(step, outputs)
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	if got.Run != "./page.sh sam@acme.dev" || got.Env["ONCALL"] != "sam@acme.dev" {
		t.Fatalf("substitution incomplete: %+v", got)
	}
	// Dangling reference fails closed.
	bad := model.PlanStep{Name: "b", Run: "${{ steps.get-oncall.outputs.absent }}"}
	if _, err := substituteWorkflowOutputs(bad, outputs); err == nil {
		t.Fatalf("dangling output reference must fail the step")
	}
	// A step with no references is untouched.
	plain := model.PlanStep{Name: "p", Run: "echo hi"}
	if got, err := substituteWorkflowOutputs(plain, nil); err != nil || got.Run != "echo hi" {
		t.Fatalf("plain step should pass through: %v", err)
	}
}

func TestResolveWorkflowPath(t *testing.T) {
	r := &Runner{}
	ec := executor.ExecContext{WorkspaceDir: "/ws", WorkDir: "/ws/svc"}
	if got := r.resolveWorkflowPath(ec, "wf/notify.yaml"); got != filepath.Join("/ws", "wf/notify.yaml") {
		t.Fatalf("relative path should resolve against workspace: %q", got)
	}
	if got := r.resolveWorkflowPath(ec, "/abs/wf.yaml"); got != "/abs/wf.yaml" {
		t.Fatalf("absolute path should pass through: %q", got)
	}
}
