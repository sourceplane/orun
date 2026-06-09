package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

// TestEnvScopingE2E_PlanSelectionMetadata exercises the env-scoping "Z" model
// end-to-end: `orun plan` stamps metadata.selection onto the written plan, with
// the mode/allEnvs flags reflecting the selection. It reuses the catalog-run
// workspace seeder (a single env "dev" / component "svc-a").
func TestEnvScopingE2E_PlanSelectionMetadata(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedCatalogRunWorkspace(t, dir)
	resetCatalogFlags(t)
	resetCatalogRunE2EGlobals(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	intentFile = filepath.Join(dir, "intent.yaml")
	intentRoot = dir
	allFlag = true
	outputFormat = "json"

	planSelection := func(t *testing.T) *model.PlanSelection {
		t.Helper()
		planPath := filepath.Join(dir, "plan.json")
		outputFile = planPath
		_ = captureStdout(t, generatePlan)
		raw, err := os.ReadFile(planPath)
		if err != nil {
			t.Fatalf("read plan: %v", err)
		}
		var plan model.Plan
		if err := json.Unmarshal(raw, &plan); err != nil {
			t.Fatalf("decode plan: %v\n%s", err, raw)
		}
		if plan.Metadata.Selection == nil {
			t.Fatalf("plan is missing metadata.selection")
		}
		return plan.Metadata.Selection
	}

	t.Run("full plan (no selection)", func(t *testing.T) {
		environment, allEnvs, planComponents, changedOnly = "", false, nil, false
		sel := planSelection(t)
		if sel.Mode != "full" {
			t.Errorf("mode = %q, want full", sel.Mode)
		}
		if sel.AllEnvs {
			t.Errorf("allEnvs = true, want false")
		}
		if len(sel.PrunedEdges) != 0 {
			t.Errorf("prunedEdges = %v, want empty for a full plan", sel.PrunedEdges)
		}
		if len(sel.Envs) == 0 {
			t.Errorf("expected at least one env in selection, got none")
		}
	})

	t.Run("scoped by --env", func(t *testing.T) {
		environment, allEnvs, planComponents, changedOnly = "dev", false, nil, false
		sel := planSelection(t)
		if sel.Mode != "scoped" {
			t.Errorf("mode = %q, want scoped", sel.Mode)
		}
	})

	t.Run("explicit --all-envs", func(t *testing.T) {
		environment, allEnvs, planComponents, changedOnly = "", true, nil, false
		sel := planSelection(t)
		if !sel.AllEnvs {
			t.Errorf("allEnvs = false, want true")
		}
		if sel.Mode != "full" {
			t.Errorf("mode = %q, want full", sel.Mode)
		}
	})
}
