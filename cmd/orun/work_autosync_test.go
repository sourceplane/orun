package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestWorkAutoSyncEnabled(t *testing.T) {
	if workAutoSyncEnabled(nil) {
		t.Fatal("nil intent enabled")
	}
	off := &model.Intent{}
	if workAutoSyncEnabled(off) {
		t.Fatal("an intent with no work section enabled")
	}
	on := &model.Intent{Work: &model.IntentWork{Epics: "work/epics", Tasks: "work/tasks", Sync: model.WorkSyncOnMerge}}
	if !workAutoSyncEnabled(on) {
		t.Fatal("on-merge not enabled")
	}
	declaredOff := &model.Intent{Work: &model.IntentWork{Epics: "work/epics"}}
	if workAutoSyncEnabled(declaredOff) {
		t.Fatal("a declared tree with sync off enabled")
	}
}

func TestAutosyncWorkMarkerRoundTrip(t *testing.T) {
	root := t.TempDir()
	if got := readAutosyncWorkMarker(root); got != "" {
		t.Fatalf("empty marker read %q", got)
	}
	writeAutosyncWorkMarker(root, "abc123")
	if got := readAutosyncWorkMarker(root); got != "abc123" {
		t.Fatalf("marker = %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "cache", autosyncWorkMarkerName)); err != nil {
		t.Fatal(err)
	}
}

// With no work section the plan-time sync is a no-op that touches nothing
// and never errors — the plan's outcome is unaffected.
func TestMaybeAutoSyncWork_DisabledIsNoOp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "intent.yaml"), []byte("apiVersion: orun.io/v1\nkind: Intent\nmetadata:\n  name: t\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	_ = os.Chdir(dir)
	maybeAutoSyncWork(context.Background())
}
