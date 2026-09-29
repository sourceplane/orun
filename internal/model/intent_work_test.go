package model

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// saas-work-gitops WG-1: a repository with no `work:` section keeps today's
// layout, a declared one is normalized, and a bad one is refused at load.

func TestWorkLayoutDefaults(t *testing.T) {
	t.Parallel()
	var nilIntent *Intent
	if got := nilIntent.WorkLayout(); got != DefaultWorkLayout() || got.Declared() {
		t.Fatalf("nil intent: %+v", got)
	}
	if got := (&Intent{}).WorkLayout(); got.Epics != "specs/epics" || got.Tasks != "tasks" || got.Sync != "off" || got.Declared() {
		t.Fatalf("no section: %+v", got)
	}
}

func TestWorkLayoutDeclared(t *testing.T) {
	t.Parallel()
	var in Intent
	if err := yaml.Unmarshal([]byte("work:\n  epics: ./work/epics/\n  tasks: work\\tasks\n  sync: On-Merge\n"), &in); err != nil {
		t.Fatal(err)
	}
	got := in.WorkLayout()
	if got.Epics != "work/epics" || got.Tasks != "work/tasks" || got.Sync != WorkSyncOnMerge || !got.Declared() {
		t.Fatalf("declared: %+v", got)
	}
	if err := in.ValidateWork(); err != nil {
		t.Fatalf("valid section refused: %v", err)
	}
	// A partial section keeps the other defaults.
	partial := Intent{Work: &IntentWork{Tasks: "work/tasks"}}
	if got := partial.WorkLayout(); got.Epics != "specs/epics" || got.Tasks != "work/tasks" || got.Sync != "off" {
		t.Fatalf("partial: %+v", got)
	}
}

func TestValidateWorkRefusals(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		w    IntentWork
		want string
	}{
		{"absolute", IntentWork{Tasks: "/etc/tasks"}, "work.tasks"},
		{"escape", IntentWork{Epics: "../elsewhere"}, "work.epics"},
		{"dot-only", IntentWork{Epics: "."}, "work.epics"},
		{"sync", IntentWork{Sync: "always"}, "work.sync"},
	} {
		w := c.w
		err := (&Intent{Work: &w}).ValidateWork()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want mention of %s", c.name, err, c.want)
		}
	}
}
