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

// The id `orun plan` prints is the short form of metadata.checksum, while
// revisions are indexed under the plan hash with a "sha256-" prefix. Both, and
// a bare hex prefix of the indexed hash, must resolve.
func TestObjResolvePlanByPrintedIDAndBareHashPrefix(t *testing.T) {
	t.Chdir(t.TempDir())

	plan := &model.Plan{Jobs: []model.PlanJob{{ID: "api.staging.ship"}}}
	plan.Metadata.Name = "acme-shop"
	plan.Metadata.Checksum = "sha256-da6acc29f1de0123456789abcdef"
	seedObjectRevision(t, "sha256-9dd54859315dcec3e3e6", plan)

	for _, ref := range []string{"da6acc29f1de", "9dd54859", "sha256-9dd5"} {
		if got, ok := objResolvePlan(ref); !ok || got.Metadata.Name != "acme-shop" {
			t.Errorf("objResolvePlan(%q) = %v, ok=%v", ref, got, ok)
		}
	}
	if _, ok := objResolvePlan("ffff0000"); ok {
		t.Errorf("an unrelated id resolved")
	}
}
