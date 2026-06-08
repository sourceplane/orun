package main

// catalog_plan_resolve_pr2_test.go — plan/catalog integration: best-effort/
// strict resolution flags and source/catalog metadata stamping. (The legacy
// --catalog-source/--catalog-snapshot pinning and the revision/catalog-parent
// mirror tests were removed with the catalogstore retirement; `orun plan`
// always resolves the live workspace into the object-model catalog.)

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

// --- Flag behavior tests ---

func TestResolvePlanCatalog_Strict_FailsOnError(t *testing.T) {
	// A bare temp dir (no git, no components): strict mode must not silently
	// succeed when the workspace has no catalog content — it either errors or
	// resolves, never a silent skip.
	withTempIntentRoot(t)
	resetCatalogFlags(t)

	res, err := resolvePlanCatalog(context.Background(), planCatalogOptions{Strict: true})
	if err != nil {
		return // strict surfaced the error — good.
	}
	if !res.Resolved {
		t.Fatalf("strict mode: no error but also not resolved — should have either resolved or errored")
	}
}

func TestResolvePlanCatalog_NoRefresh_SkipsMetadata(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedGitCatalogWorkspace(t, dir)
	resetCatalogFlags(t)

	res, err := resolvePlanCatalog(context.Background(), planCatalogOptions{NoRefresh: true})
	if err != nil {
		t.Fatalf("NoRefresh: %v", err)
	}
	if res.Resolved {
		t.Errorf("NoRefresh should not resolve")
	}
	if !res.Skipped {
		t.Errorf("NoRefresh should set Skipped=true")
	}
	if res.Source != nil {
		t.Errorf("NoRefresh should leave Source nil, got %+v", res.Source)
	}
	if res.Catalog != nil {
		t.Errorf("NoRefresh should leave Catalog nil, got %+v", res.Catalog)
	}
}

func TestResolvePlanCatalog_BestEffort_NoError(t *testing.T) {
	// Non-git workspace, no strict: must not error.
	withTempIntentRoot(t)
	resetCatalogFlags(t)

	res, err := resolvePlanCatalog(context.Background(), planCatalogOptions{})
	if err != nil {
		t.Fatalf("best-effort must not error: %v", err)
	}
	_ = res // either resolved or not; the contract is no error.
}

func TestResolvePlanCatalog_ResolvedView(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedGitCatalogWorkspace(t, dir)
	resetCatalogFlags(t)

	res, err := resolvePlanCatalog(context.Background(), planCatalogOptions{})
	if err != nil || !res.Resolved {
		t.Fatalf("resolve: err=%v res=%+v", err, res)
	}
	if res.Source == nil || res.Catalog == nil {
		t.Fatalf("Source/Catalog provenance missing: %+v", res)
	}
	if res.View == nil {
		t.Error("resolved View must be threaded out for the object-model plan write")
	}
	if !strings.HasPrefix(res.Catalog.CatalogSnapshotKey, "cat-") {
		t.Errorf("catalog key not cat-prefixed: %q", res.Catalog.CatalogSnapshotKey)
	}
}

// --- Metadata stamping tests ---

func TestPlanMetadata_SourceCatalog_Populated(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedGitCatalogWorkspace(t, dir)
	resetCatalogFlags(t)

	res, err := resolvePlanCatalog(context.Background(), planCatalogOptions{})
	if err != nil || !res.Resolved {
		t.Fatalf("resolve: err=%v res=%+v", err, res)
	}

	plan := &model.Plan{Metadata: model.PlanMetadata{Name: "test"}}
	if res.Source != nil {
		plan.Metadata.Source = &model.PlanSourceMeta{
			SnapshotKey:  res.Source.SourceSnapshotKey,
			Ref:          res.Source.Ref,
			HeadRevision: res.Source.HeadRevision,
			TreeHash:     res.Source.TreeHash,
			WorkingTree:  res.Source.WorkingTree,
			DirtyHash:    res.Source.DirtyHash,
		}
	}
	if res.Catalog != nil {
		plan.Metadata.Catalog = &model.PlanCatalogMeta{
			SnapshotKey:       res.Catalog.CatalogSnapshotKey,
			CatalogHash:       res.Catalog.CatalogHash,
			SourceSnapshotKey: res.Catalog.SourceSnapshotKey,
		}
	}

	if plan.Metadata.Source == nil || plan.Metadata.Source.SnapshotKey == "" {
		t.Fatal("source metadata not populated")
	}
	if plan.Metadata.Catalog == nil || plan.Metadata.Catalog.SnapshotKey == "" {
		t.Fatal("catalog metadata not populated")
	}
	if plan.Metadata.Catalog.Skipped {
		t.Error("catalog should not be skipped when resolved")
	}

	// JSON round-trip preserves the additive fields.
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var rt model.Plan
	if err := json.Unmarshal(data, &rt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rt.Metadata.Source == nil || rt.Metadata.Source.SnapshotKey != plan.Metadata.Source.SnapshotKey {
		t.Error("source metadata not preserved through JSON round-trip")
	}
	if rt.Metadata.Catalog == nil || rt.Metadata.Catalog.SnapshotKey != plan.Metadata.Catalog.SnapshotKey {
		t.Error("catalog metadata not preserved through JSON round-trip")
	}
}

func TestPlanMetadata_Skipped_WhenNoRefresh(t *testing.T) {
	plan := &model.Plan{Metadata: model.PlanMetadata{Name: "test"}}
	plan.Metadata.Catalog = &model.PlanCatalogMeta{Skipped: true}

	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"skipped":true`) {
		t.Errorf("expected skipped:true in JSON, got %s", string(data))
	}
	var rt model.Plan
	if err := json.Unmarshal(data, &rt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rt.Metadata.Catalog == nil || !rt.Metadata.Catalog.Skipped {
		t.Error("skipped flag not preserved through JSON round-trip")
	}
}

func TestPlanMetadata_Nil_WhenUnresolved(t *testing.T) {
	plan := &model.Plan{Metadata: model.PlanMetadata{Name: "test"}}

	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), `"source"`) {
		t.Errorf("source should be omitted when nil")
	}
	if strings.Contains(string(data), `"catalog"`) {
		t.Errorf("catalog should be omitted when nil")
	}
}
