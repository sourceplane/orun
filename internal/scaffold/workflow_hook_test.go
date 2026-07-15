package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/workflowbackend"
)

type fakeHookEngine struct {
	res    workflowbackend.Result
	err    error
	calls  int
	gotReq workflowbackend.Request
}

func (f *fakeHookEngine) Digest() string { return "sha256:fake" }
func (f *fakeHookEngine) Invoke(_ context.Context, req workflowbackend.Request) (workflowbackend.Result, error) {
	f.calls++
	f.gotReq = req
	return f.res, f.err
}

const hookBP = `apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: svc
modules:
  - name: worker
    mode: template
    files:
      "README.md": "# hi\n"
hooks:
  postInstantiate:
    - id: open-pr
      workflow: hook.yaml
      with: { branch: scaffold/x }
`

func writeHookWorkflow(t *testing.T) (baseDir, digest string) {
	t.Helper()
	baseDir = t.TempDir()
	body := []byte("apiVersion: torkflow/v1\nkind: Workflow\n")
	if err := os.WriteFile(filepath.Join(baseDir, "hook.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	return baseDir, workflowbackend.DigestBytes(body)
}

func TestHookMutualExclusionValidation(t *testing.T) {
	both := `apiVersion: orun.dev/v1
kind: Blueprint
metadata: { name: svc }
modules:
  - { name: m, mode: template, files: { "R.md": "x" } }
hooks:
  postInstantiate:
    - id: bad
      run: ["echo", "hi"]
      workflow: hook.yaml
`
	if _, err := ParseBlueprint([]byte(both)); err == nil {
		t.Fatalf("expected validation error for a hook with both run and workflow")
	}

	neither := `apiVersion: orun.dev/v1
kind: Blueprint
metadata: { name: svc }
modules:
  - { name: m, mode: template, files: { "R.md": "x" } }
hooks:
  postInstantiate:
    - { id: empty }
`
	if _, err := ParseBlueprint([]byte(neither)); err == nil {
		t.Fatalf("expected validation error for a hook with neither run nor workflow")
	}
}

func TestWorkflowHookRunsAndIsPinned(t *testing.T) {
	baseDir, digest := writeHookWorkflow(t)
	eng := &fakeHookEngine{res: workflowbackend.Result{Status: workflowbackend.StatusSuccess}}

	res, err := Run(context.Background(), Options{
		Blueprint:      []byte(hookBP),
		OutDir:         t.TempDir(),
		Store:          objectstore.NewMemStore(objectstore.AlgoSHA256),
		SourceBaseDir:  baseDir,
		RunHooks:       true,
		WorkflowEngine: eng,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The hook ran through the engine with its declared inputs.
	if eng.calls != 1 || eng.gotReq.With["branch"] != "scaffold/x" {
		t.Fatalf("workflow hook not invoked with inputs: calls=%d req=%#v", eng.calls, eng.gotReq)
	}
	if len(res.HooksRun) != 1 || res.HooksRun[0] != "open-pr" {
		t.Fatalf("expected open-pr to run, got %v", res.HooksRun)
	}
	// Provenance pins the hook by reference + digest — never an outcome.
	if len(res.Provenance.Hooks) != 1 {
		t.Fatalf("expected 1 pinned hook, got %d", len(res.Provenance.Hooks))
	}
	ph := res.Provenance.Hooks[0]
	if ph.ID != "open-pr" || ph.Workflow != "hook.yaml" || ph.Digest != digest {
		t.Fatalf("hook not pinned faithfully: %#v (want digest %s)", ph, digest)
	}
}

func TestWorkflowHookPinnedEvenWithoutRunHooks(t *testing.T) {
	baseDir, digest := writeHookWorkflow(t)
	res, err := Run(context.Background(), Options{
		Blueprint:     []byte(hookBP),
		OutDir:        t.TempDir(),
		Store:         objectstore.NewMemStore(objectstore.AlgoSHA256),
		SourceBaseDir: baseDir,
		RunHooks:      false, // hooks not executed …
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.HooksRun) != 0 {
		t.Fatalf("hooks should not run when RunHooks is off: %v", res.HooksRun)
	}
	// … but the lineage is still pinned.
	if len(res.Provenance.Hooks) != 1 || res.Provenance.Hooks[0].Digest != digest {
		t.Fatalf("hook digest must be pinned even without --run-hooks: %#v", res.Provenance.Hooks)
	}
}

func TestWorkflowHookFailureLeavesTreeInPlace(t *testing.T) {
	baseDir, _ := writeHookWorkflow(t)
	out := t.TempDir()
	eng := &fakeHookEngine{res: workflowbackend.Result{Status: workflowbackend.StatusFailed, Error: "provider 4xx"}}

	_, err := Run(context.Background(), Options{
		Blueprint:      []byte(hookBP),
		OutDir:         out,
		Store:          objectstore.NewMemStore(objectstore.AlgoSHA256),
		SourceBaseDir:  baseDir,
		RunHooks:       true,
		WorkflowEngine: eng,
	})
	if err == nil || !strings.Contains(err.Error(), "hook") {
		t.Fatalf("expected a hook failure error, got %v", err)
	}
	// The gated tree was written before the hook ran — it stays in place.
	if _, statErr := os.Stat(filepath.Join(out, "README.md")); statErr != nil {
		t.Fatalf("scaffold tree should be materialized despite the hook failure: %v", statErr)
	}
}

func TestWorkflowHookMissingFileIsError(t *testing.T) {
	// No hook.yaml on disk: the reference cannot be pinned → fail-closed.
	_, err := Run(context.Background(), Options{
		Blueprint:     []byte(hookBP),
		OutDir:        t.TempDir(),
		Store:         objectstore.NewMemStore(objectstore.AlgoSHA256),
		SourceBaseDir: t.TempDir(), // empty dir, no hook.yaml
		RunHooks:      false,
	})
	if err == nil {
		t.Fatalf("expected an error for an unresolvable hook workflow reference")
	}
}
