package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

// The live-path seal, projection ordering, and plan-hash behavior now live in
// internal/objrun (where the session glue moved). This file keeps only the
// cmd-side adapter's input guard.
func TestBeginObjectModelRunGuards(t *testing.T) {
	orunDir := filepath.Join(t.TempDir(), ".orun")
	plan := &model.Plan{Metadata: model.PlanMetadata{Name: "demo"}}
	if s := beginObjectModelRun(orunDir, nil, "exec-1"); s != nil {
		t.Fatalf("begin returned a session for a nil plan")
	}
	if s := beginObjectModelRun(orunDir, plan, ""); s != nil {
		t.Fatalf("begin returned a session for an empty execID")
	}
	// No object-model root is created when the guard rejects the input.
	if _, err := os.Stat(objectModelRoot(orunDir)); !os.IsNotExist(err) {
		t.Fatalf("object-model root created despite guard rejection")
	}
}
