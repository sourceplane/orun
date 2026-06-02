package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/state"
)

func countObjectFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func legacyStoreWithExecution(t *testing.T, execID string) (*state.Store, *model.Plan) {
	t.Helper()
	ls := state.NewStore(t.TempDir())
	if err := ls.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	plan := &model.Plan{Metadata: model.PlanMetadata{Name: "demo"}}
	if _, err := ls.CreateExecution(execID, plan); err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	if err := ls.SaveState(execID, &state.ExecState{
		ExecID: execID,
		Jobs: map[string]*state.JobState{
			"api@deploy": {Status: "success", Steps: map[string]string{"build": "success"}},
		},
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	if err := ls.SaveMetadata(execID, &state.ExecMetadata{ExecID: execID, Status: "success"}); err != nil {
		t.Fatalf("SaveMetadata: %v", err)
	}
	return ls, plan
}

func TestSealObjectModelRunDisabledByDefault(t *testing.T) {
	os.Unsetenv("ORUN_OBJECT_RUNNER")
	orunDir := filepath.Join(t.TempDir(), ".orun")
	ls, plan := legacyStoreWithExecution(t, "exec-off")
	sealObjectModelRun(orunDir, plan, ls, "exec-off")
	if _, err := os.Stat(objectModelRoot(orunDir)); !os.IsNotExist(err) {
		t.Fatalf("object-model root created with flag off: %v", err)
	}
}

func TestSealObjectModelRunSeals(t *testing.T) {
	t.Setenv("ORUN_OBJECT_RUNNER", "1")
	// The hook re-resolves the workspace catalog/source against the process cwd;
	// run from an isolated temp dir so it never writes a stray .orun/ into the
	// repository tree (cwd is restored on cleanup).
	t.Chdir(t.TempDir())
	execID := "exec-seal-1"
	ls, plan := legacyStoreWithExecution(t, execID)
	orunDir := filepath.Join(t.TempDir(), ".orun")

	sealObjectModelRun(orunDir, plan, ls, execID)

	root := objectModelRoot(orunDir)
	if n := countObjectFiles(t, filepath.Join(root, "objects")); n == 0 {
		t.Fatalf("no objects written under %s", root)
	}
	for _, ref := range []string{
		filepath.Join(root, "refs", "revisions", "latest.json"),
		filepath.Join(root, "refs", "executions", "latest.json"),
	} {
		if _, err := os.Stat(ref); err != nil {
			t.Fatalf("expected ref %s: %v", ref, err)
		}
	}
}
