package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/runner"
	"github.com/sourceplane/orun/internal/runworktree"
	"github.com/sourceplane/orun/internal/state"
)

func TestBeginObjectModelRunDisabledByFlag(t *testing.T) {
	t.Setenv("ORUN_OBJECT_RUNNER", "0")
	orunDir := filepath.Join(t.TempDir(), ".orun")
	_, plan := legacyStoreWithExecution(t, "exec-off")
	if s := beginObjectModelRun(orunDir, plan, "exec-off"); s != nil {
		t.Fatalf("begin returned a session with flag explicitly disabled")
	}
	if _, err := os.Stat(objectModelRoot(orunDir)); !os.IsNotExist(err) {
		t.Fatalf("object-model root created with flag explicitly disabled")
	}
}

func TestObjectModelRunnerLivePathSeals(t *testing.T) {
	t.Setenv("ORUN_OBJECT_RUNNER", "1")
	// begin re-resolves the workspace against cwd; isolate it.
	t.Chdir(t.TempDir())

	execID := "exec-live-1"
	_, plan := legacyStoreWithExecution(t, execID)
	orunDir := filepath.Join(t.TempDir(), ".orun")

	sess := beginObjectModelRun(orunDir, plan, execID)
	if sess == nil {
		t.Fatalf("begin returned nil session")
	}
	root := objectModelRoot(orunDir)

	// A live working tree + in-flight handle exist mid-run.
	if _, err := os.Stat(filepath.Join(root, "run")); err != nil {
		t.Fatalf("working tree dir missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "refs", "executions", "live")); err != nil {
		t.Fatalf("live ref missing: %v", err)
	}

	// Install the hooks on a runner and drive a step log through them. With a
	// bare runner (no live state) AfterStateUpdate is a safe no-op, so drive the
	// job/step state into the working tree directly to simulate progress.
	r := &runner.Runner{}
	installObjectRunnerHooks(r, sess)
	if r.Hooks == nil || r.Hooks.AfterStateUpdate == nil || r.Hooks.AfterStepLog == nil {
		t.Fatalf("hooks not installed")
	}
	r.Hooks.AfterStateUpdate() // no-op (no live state) — must not panic
	if err := sess.wt.Project([]runworktree.ProjectedJob{
		{JobID: "api@deploy", Status: nodes.StatusSucceeded, Steps: []runworktree.ProjectedStep{{StepID: "build", Status: nodes.StatusSucceeded}}},
	}); err != nil {
		t.Fatalf("project: %v", err)
	}
	r.Hooks.AfterStepLog("api@deploy", "build", "build output\n")

	finishObjectModelRun(r, sess, nil)

	// Sealed: objects + executions/latest written; working tree + live handle gone.
	if n := countObjectFiles(t, filepath.Join(root, "objects")); n == 0 {
		t.Fatalf("no objects written")
	}
	if _, err := os.Stat(filepath.Join(root, "refs", "executions", "latest.json")); err != nil {
		t.Fatalf("executions/latest not published: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "run")); len(entries) != 0 {
		t.Fatalf("working tree survived seal: %d entries", len(entries))
	}
	if _, err := os.Stat(filepath.Join(root, "refs", "executions", "live", "exec-live-1.json")); !os.IsNotExist(err) {
		t.Fatalf("live handle survived seal")
	}
}

func TestProjectFromExecStateSorted(t *testing.T) {
	st := &state.ExecState{Jobs: map[string]*state.JobState{
		"b@deploy": {Status: "running", Steps: map[string]string{"z": "running", "a": "success"}},
		"a@deploy": {Status: "success", Steps: map[string]string{"build": "success"}},
	}}
	out := projectFromExecState(st)
	if len(out) != 2 || out[0].JobID != "a@deploy" || out[1].JobID != "b@deploy" {
		t.Fatalf("jobs not sorted: %+v", out)
	}
	// Steps within a job are sorted too.
	if out[1].Steps[0].StepID != "a" || out[1].Steps[1].StepID != "z" {
		t.Fatalf("steps not sorted: %+v", out[1].Steps)
	}
	// Status mapping folded onto the node vocabulary.
	if out[0].Status != "succeeded" || out[1].Status != "running" {
		t.Fatalf("status mapping wrong: %+v", out)
	}
}
