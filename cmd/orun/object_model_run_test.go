package main

import (
	"io/fs"
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
