package runner

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
)

// cacheWritingExecutor behaves like a tool that writes a cache beside the
// staged trees (turbo under .orun/runs/<exec>/cache), which a job's own
// cleanup never removes.
type cacheWritingExecutor struct{}

func (*cacheWritingExecutor) Name() string                       { return "cache-writing" }
func (*cacheWritingExecutor) Prepare(executor.ExecContext) error { return nil }
func (*cacheWritingExecutor) Cleanup(executor.ExecContext) error { return nil }
func (*cacheWritingExecutor) RunStep(ctx executor.ExecContext, _ model.PlanJob, _ model.PlanStep) (string, error) {
	// ctx.WorkDir is <src>/.orun/runs/<exec>/<job>/work in a staged run.
	cache := filepath.Join(filepath.Dir(filepath.Dir(ctx.WorkDir)), "cache", "turbo")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return "", err
	}
	return "ok", os.WriteFile(filepath.Join(cache, "entry"), []byte("x"), 0o644)
}

func plainJob(id string) model.PlanJob {
	return model.PlanJob{ID: id, Name: id, Component: id, Environment: "stage", Steps: []model.PlanStep{
		{ID: "build", Name: "build", Run: "build " + id},
	}}
}

func newStagedRunner(t *testing.T, execID string) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(root, false, io.Discard, io.Discard, false, "", false, false,
		&cacheWritingExecutor{}, executor.RuntimeContext{}, execID, 2, nil, "")
	r.Isolation = IsolationWorkspace
	return r, root
}

func TestRun_RemovesTheRunDirectoryAfterAWholePlanRun(t *testing.T) {
	r, root := newStagedRunner(t, "exec_rm")
	if err := r.Run(&model.Plan{Jobs: []model.PlanJob{plainJob("a"), plainJob("b")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".orun", "runs", "exec_rm")); !os.IsNotExist(err) {
		t.Fatalf(".orun/runs/exec_rm left behind after the run (stat err = %v)", err)
	}
}

func TestRun_KeepsTheRunDirectoryForAJobRunAndWithKeepWorkspaces(t *testing.T) {
	r, root := newStagedRunner(t, "exec_job")
	r.JobID = "a"
	if err := r.Run(&model.Plan{Jobs: []model.PlanJob{plainJob("a"), plainJob("b")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".orun", "runs", "exec_job", "cache", "turbo", "entry")); err != nil {
		t.Fatalf("a --job run shares the exec directory with other processes and leaves it: %v", err)
	}

	r2, root2 := newStagedRunner(t, "exec_keep")
	r2.KeepWorkspaces = true
	if err := r2.Run(&model.Plan{Jobs: []model.PlanJob{plainJob("a")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root2, ".orun", "runs", "exec_keep")); err != nil {
		t.Fatalf("--keep-workspaces keeps the run directory: %v", err)
	}
}
