package workflowbackend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakeEngine struct {
	res    Result
	err    error
	gotReq Request
	calls  int
}

func (f *fakeEngine) Digest() string { return "sha256:fake" }

func (f *fakeEngine) Invoke(_ context.Context, req Request) (Result, error) {
	f.calls++
	f.gotReq = req
	return f.res, f.err
}

func writeWorkflow(t *testing.T, body string) (dir, path, digest string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, "wf.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, path, DigestBytes([]byte(body))
}

func TestRunStepInvokesWhenDigestMatches(t *testing.T) {
	_, path, digest := writeWorkflow(t, "apiVersion: torkflow/v1\n")
	eng := &fakeEngine{res: Result{Status: StatusSuccess, Outputs: map[string]any{"ok": true}}}

	res, err := RunStep(context.Background(), eng, StepSpec{
		WorkflowPath:   path,
		ExpectedDigest: digest,
		With:           map[string]any{"channel": "ops"},
		Connections:    map[string]any{"token": "s3cr3t"},
		Metadata:       map[string]any{"jobId": "j"},
		RunDir:         "/tmp/run",
	})
	if err != nil {
		t.Fatalf("RunStep: %v", err)
	}
	if !res.Succeeded() {
		t.Fatalf("expected success, got %q", res.Status)
	}
	if eng.calls != 1 {
		t.Fatalf("engine invoked %d times, want 1", eng.calls)
	}
	if eng.gotReq.Workflow != path || eng.gotReq.With["channel"] != "ops" ||
		eng.gotReq.Connections["token"] != "s3cr3t" || eng.gotReq.RunDir != "/tmp/run" {
		t.Fatalf("request not passed through faithfully: %#v", eng.gotReq)
	}
}

func TestRunStepDigestMismatchDoesNotInvoke(t *testing.T) {
	_, path, _ := writeWorkflow(t, "apiVersion: torkflow/v1\n")
	eng := &fakeEngine{res: Result{Status: StatusSuccess}}

	_, err := RunStep(context.Background(), eng, StepSpec{
		WorkflowPath:   path,
		ExpectedDigest: "sha256:stale",
	})
	var dm *DigestMismatchError
	if !errors.As(err, &dm) {
		t.Fatalf("expected DigestMismatchError, got %v", err)
	}
	if eng.calls != 0 {
		t.Fatalf("engine must not run when the digest is stale (ran %d times)", eng.calls)
	}
}

func TestRunStepNoExpectedDigestInvokes(t *testing.T) {
	_, path, _ := writeWorkflow(t, "apiVersion: torkflow/v1\n")
	eng := &fakeEngine{res: Result{Status: StatusFailed, Error: "boom"}}

	res, err := RunStep(context.Background(), eng, StepSpec{WorkflowPath: path})
	if err != nil {
		t.Fatalf("a failed workflow is not an infra error: %v", err)
	}
	if res.Succeeded() || res.Error != "boom" {
		t.Fatalf("expected failed passthrough, got %#v", res)
	}
	if eng.calls != 1 {
		t.Fatalf("engine invoked %d times, want 1", eng.calls)
	}
}
