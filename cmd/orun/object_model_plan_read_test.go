package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// seedObjectRevision writes one revision (plan.json) into .orun/objectmodel
// under cwd and points revisions/latest + by-hash at it.
func seedObjectRevision(t *testing.T, checksum string, plan *model.Plan) {
	t.Helper()
	root := filepath.Join(storeDir(), ".orun", "objectmodel")
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Clock: clock.Fixed{}})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	planBytes, _ := json.Marshal(plan)
	revID, err := nodes.AssembleRevision(context.Background(), store,
		nodes.PlanRevision{Scope: nodes.RevisionScope{Mode: "full"}, JobCount: len(plan.Jobs), LegacyChecksum: checksum}, planBytes)
	if err != nil {
		t.Fatalf("AssembleRevision: %v", err)
	}
	for _, ref := range []string{"revisions/latest", "revisions/by-hash/" + checksum} {
		if err := refs.Update(context.Background(), ref, "", string(revID)); err != nil {
			t.Fatalf("ref %s: %v", ref, err)
		}
	}
}

func TestObjResolvePlanAndList(t *testing.T) {
	t.Chdir(t.TempDir())

	plan := &model.Plan{Jobs: []model.PlanJob{{ID: "a@deploy"}, {ID: "b@deploy"}}}
	plan.Metadata.Name = "demo"
	seedObjectRevision(t, "abc123", plan)

	// Resolve latest.
	got, ok := objResolvePlan("latest")
	if !ok || len(got.Jobs) != 2 || got.Metadata.Name != "demo" {
		t.Fatalf("objResolvePlan latest = %+v, ok=%v", got, ok)
	}
	// Resolve by exact hash and by prefix.
	if _, ok := objResolvePlan("abc123"); !ok {
		t.Fatalf("resolve by exact hash failed")
	}
	if _, ok := objResolvePlan("abc"); !ok {
		t.Fatalf("resolve by hash prefix failed")
	}
	if _, ok := objResolvePlan("nope"); ok {
		t.Fatalf("resolve of unknown ref should fail")
	}

	// List.
	rows, ok := objListPlanRows()
	if !ok || len(rows) != 1 || rows[0].Jobs != 2 || rows[0].Name != "demo" {
		t.Fatalf("objListPlanRows = %+v, ok=%v", rows, ok)
	}
}

func TestObjResolvePlanOffWhenAbsent(t *testing.T) {
	t.Chdir(t.TempDir()) // no object model
	if _, ok := objResolvePlan("latest"); ok {
		t.Fatalf("should not resolve with no object model")
	}
	if _, ok := objListPlanRows(); ok {
		t.Fatalf("should not list with no object model")
	}
}

// TestObjResolvePlanByName: `orun plan --name <name>` publishes the revision
// under revisions/by-name/<name>, so `orun run <name>` resolves it (and the
// name follows the newest plan written with it).
func TestObjResolvePlanByName(t *testing.T) {
	t.Chdir(t.TempDir())
	orunDir, err := filepath.Abs(filepath.Join(storeDir(), ".orun"))
	if err != nil {
		t.Fatal(err)
	}
	trig := triggerctx.TriggerOccurrence{TriggerName: "system.manual", PlanScope: triggerctx.PlanScope{Mode: "full"}}

	planA := &model.Plan{Jobs: []model.PlanJob{{ID: "a@deploy"}}}
	planA.Metadata.Checksum = "sha256-aaa111"
	bytesA, _ := json.Marshal(planA)
	writeObjectModelPlan(orunDir, planA, bytesA, "sha256-aaa111", "rev-a", "qs-named", trig, planCatalogResolution{})

	got, ok := objResolvePlan("qs-named")
	if !ok || got.Metadata.Checksum != "sha256-aaa111" {
		t.Fatalf("resolve by name = %+v, ok=%v", got, ok)
	}

	// An unnamed plan moves latest but leaves the name pointing at plan A.
	planB := &model.Plan{Jobs: []model.PlanJob{{ID: "b@deploy"}, {ID: "c@deploy"}}}
	planB.Metadata.Checksum = "sha256-bbb222"
	bytesB, _ := json.Marshal(planB)
	writeObjectModelPlan(orunDir, planB, bytesB, "sha256-bbb222", "rev-b", "", trig, planCatalogResolution{})
	if got, ok := objResolvePlan("qs-named"); !ok || got.Metadata.Checksum != "sha256-aaa111" {
		t.Fatalf("name moved by unnamed plan: %+v, ok=%v", got, ok)
	}
	if got, ok := objResolvePlan("latest"); !ok || got.Metadata.Checksum != "sha256-bbb222" {
		t.Fatalf("latest = %+v, ok=%v", got, ok)
	}

	// Re-planning with the same name moves it to the newer plan.
	writeObjectModelPlan(orunDir, planB, bytesB, "sha256-bbb222", "rev-b", "qs-named", trig, planCatalogResolution{})
	if got, ok := objResolvePlan("qs-named"); !ok || got.Metadata.Checksum != "sha256-bbb222" {
		t.Fatalf("name not moved to newer plan: %+v, ok=%v", got, ok)
	}

	// An exact name wins over a checksum prefix ("sha256-a" prefixes plan A's
	// checksum but names plan B here).
	writeObjectModelPlan(orunDir, planB, bytesB, "sha256-bbb222", "rev-b", "sha256-a", trig, planCatalogResolution{})
	if got, ok := objResolvePlan("sha256-a"); !ok || got.Metadata.Checksum != "sha256-bbb222" {
		t.Fatalf("name should beat checksum prefix: %+v, ok=%v", got, ok)
	}

	// Unknown names still fall through (so `orun run` treats them as a
	// component filter).
	if _, ok := objResolvePlan("no-such-plan"); ok {
		t.Fatalf("unknown name should not resolve")
	}
}
