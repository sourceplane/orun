package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The live-path seal, projection ordering, and plan-hash behavior now live in
// internal/objrun (where the session glue moved). This file keeps only the
// cmd-side flag gate, which is the cmd adapter's own responsibility.
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
