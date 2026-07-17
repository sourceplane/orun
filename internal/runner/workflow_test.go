package runner

import (
	"context"
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

	out, err := r.runWorkflowStep(ec, model.PlanJob{ID: "web@prod.deploy"}, step)
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

	out, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step)
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

	if _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step); err == nil {
		t.Fatalf("expected an error when the pinned digest is stale")
	}
}

func TestRunWorkflowStepUnconfiguredEngineIsError(t *testing.T) {
	dir, digest := writeWF(t)
	t.Setenv(workflowbackend.EngineEnv, "") // no engine available
	r := &Runner{}                          // no injected engine
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	step := model.PlanStep{Name: "s", Workflow: "wf.yaml", WorkflowDigest: digest}

	if _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step); err == nil {
		t.Fatalf("expected a clear error when no workflow engine is configured")
	}
}

func TestRunWorkflowStepInjectsCredentials(t *testing.T) {
	dir, digest := writeWF(t)
	eng := &fakeWFEngine{res: workflowbackend.Result{Status: workflowbackend.StatusSuccess}}
	r := &Runner{WorkflowEngine: eng}
	ec := executor.ExecContext{
		Context:      context.Background(),
		WorkspaceDir: dir,
		// The job's resolved orun-secrets values (name→value).
		SecretEnv: map[string]string{"GITHUB_TOKEN": "ghp_s3cr3t"},
	}
	step := model.PlanStep{Name: "open-pr", Workflow: "wf.yaml", WorkflowDigest: digest}

	out, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step)
	if err != nil {
		t.Fatalf("runWorkflowStep: %v", err)
	}
	// The resolved secret reaches the engine in-memory via the request.
	if eng.gotReq.Connections["GITHUB_TOKEN"] != "ghp_s3cr3t" {
		t.Fatalf("credential not injected into the engine request: %#v", eng.gotReq.Connections)
	}
	// It must never appear in the sealed step output (the summary carries no
	// credentials; the runner's redactor is the second line of defense).
	if strings.Contains(out, "ghp_s3cr3t") {
		t.Fatalf("credential leaked into the sealed workflow output: %q", out)
	}
}

func TestCredentialsFromSecretEnv(t *testing.T) {
	if credentialsFromSecretEnv(nil) != nil {
		t.Fatalf("empty secret env should yield nil credentials")
	}
	got := credentialsFromSecretEnv(map[string]string{"A": "1", "B": "2"})
	if got["A"] != "1" || got["B"] != "2" || len(got) != 2 {
		t.Fatalf("unexpected conversion: %#v", got)
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
