package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/triggerctx"
)

func countFiles(t *testing.T, dir string) int {
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

func TestWriteObjectModelPlanDisabledByDefault(t *testing.T) {
	os.Unsetenv("ORUN_OBJECT_MODEL")
	orunDir := filepath.Join(t.TempDir(), ".orun")
	writeObjectModelPlan(orunDir, &model.Plan{}, []byte(`{}`), "", "", triggerctx.TriggerOccurrence{}, planCatalogResolution{})
	if _, err := os.Stat(objectModelRoot(orunDir)); !os.IsNotExist(err) {
		t.Fatalf("object-model root created with flag off: %v", err)
	}
}

func TestWriteObjectModelPlanWritesGraph(t *testing.T) {
	t.Setenv("ORUN_OBJECT_MODEL", "1")
	orunDir := filepath.Join(t.TempDir(), ".orun")
	trig := triggerctx.TriggerOccurrence{
		TriggerName: "system.manual",
		TriggerKey:  "system.manual:full",
		PlanScope:   triggerctx.PlanScope{Mode: "full"},
		CreatedAt:   time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC),
	}
	// No catalog view → degenerate (source + revision + trigger spine).
	writeObjectModelPlan(orunDir, &model.Plan{}, []byte(`{"plan":"A"}`), "sha256-abc", "rev-x", trig, planCatalogResolution{})

	root := objectModelRoot(orunDir)
	if n := countFiles(t, filepath.Join(root, "objects")); n == 0 {
		t.Fatalf("no objects written under %s", root)
	}
	// The revision and trigger refs must be published.
	for _, ref := range []string{
		filepath.Join(root, "refs", "revisions", "latest.json"),
		filepath.Join(root, "refs", "triggers", "system.manual", "latest.json"),
		filepath.Join(root, "refs", "sources", "current.json"),
	} {
		if _, err := os.Stat(ref); err != nil {
			t.Fatalf("expected ref %s: %v", ref, err)
		}
	}
	// A second run dedups the revision (same plan) — still succeeds and adds a
	// fresh trigger event without erroring.
	writeObjectModelPlan(orunDir, &model.Plan{}, []byte(`{"plan":"A"}`), "sha256-abc", "rev-x", trig, planCatalogResolution{})
}

func TestObjectModelTriggerMapping(t *testing.T) {
	t.Parallel()
	got := objectModelTrigger(triggerctx.TriggerOccurrence{})
	if got.Kind != "TriggerOccurrence" || got.TriggerName != "system.manual" || got.Source.Flavor != "system" {
		t.Fatalf("defaults not applied: %+v", got)
	}
	if got.Scope.Mode != "full" {
		t.Fatalf("scope mode = %q, want full", got.Scope.Mode)
	}
	full := objectModelTrigger(triggerctx.TriggerOccurrence{TriggerName: "github-push", TriggerType: "declared", Mode: "changed", PlanScope: triggerctx.PlanScope{Mode: "changed"}})
	if full.TriggerName != "github-push" || full.Source.Flavor != "declared" || full.Scope.Mode != "changed" {
		t.Fatalf("explicit fields lost: %+v", full)
	}
}

func TestShortID(t *testing.T) {
	t.Parallel()
	if shortID("") != "-" {
		t.Fatalf("empty id should render as -")
	}
	long := objectstore.ObjectID("sha256:" + "abcdef0123456789")
	if got := shortID(long); got != "sha256:abcdef0…" {
		t.Fatalf("shortID = %q", got)
	}
	if got := shortID("short"); got != "short" {
		t.Fatalf("shortID(short) = %q", got)
	}
}
