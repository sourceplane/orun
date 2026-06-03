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
	t.Setenv("ORUN_OBJECT_RUNNER", "1")
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
	t.Setenv("ORUN_OBJECT_RUNNER", "1")
	t.Chdir(t.TempDir()) // no object model
	if _, ok := objResolvePlan("latest"); ok {
		t.Fatalf("should not resolve with no object model")
	}
	if _, ok := objListPlanRows(); ok {
		t.Fatalf("should not list with no object model")
	}
}
