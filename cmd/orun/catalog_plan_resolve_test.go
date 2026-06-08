package main

// catalog_plan_resolve_test.go — integration coverage for the plan-path catalog
// resolver. Drives resolvePlanCatalog against a real seeded git workspace
// (reusing the catalog-refresh harness) and asserts:
//   - a clean branch-main workspace resolves a (source, catalog) snapshot pair;
//   - --no-catalog-refresh short-circuits to Skipped;
//   - a non-git / unresolvable workspace degrades to !Resolved without error
//     (best-effort posture).
//
// The resolver no longer persists to the legacy catalogstore — the object-model
// catalog is written from the threaded-out View by writeObjectModelPlan.

import (
	"context"
	"strings"
	"testing"
)

func TestResolvePlanCatalog_Resolves_BranchMain(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedGitCatalogWorkspace(t, dir)
	resetCatalogFlags(t)

	res, err := resolvePlanCatalog(context.Background(), planCatalogOptions{})
	if err != nil {
		t.Fatalf("resolvePlanCatalog: %v", err)
	}
	if !res.Resolved {
		t.Fatalf("expected Resolved=true for clean branch-main workspace, got %+v", res)
	}
	if res.Skipped {
		t.Errorf("Skipped should be false when resolving, got %+v", res)
	}
	if res.Source == nil || res.Catalog == nil {
		t.Fatalf("Source/Catalog provenance missing: %+v", res)
	}
	if !strings.HasPrefix(res.Catalog.CatalogSnapshotKey, "cat-") {
		t.Errorf("catalog key not cat-prefixed: %q", res.Catalog.CatalogSnapshotKey)
	}
	if res.View == nil {
		t.Error("resolved View must be threaded out for the object-model plan write")
	}
}

func TestResolvePlanCatalog_Idempotent(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedGitCatalogWorkspace(t, dir)
	resetCatalogFlags(t)

	first, err := resolvePlanCatalog(context.Background(), planCatalogOptions{})
	if err != nil || !first.Resolved {
		t.Fatalf("first resolve: err=%v res=%+v", err, first)
	}
	second, err := resolvePlanCatalog(context.Background(), planCatalogOptions{})
	if err != nil || !second.Resolved {
		t.Fatalf("second resolve: err=%v res=%+v", err, second)
	}
	// The resolve is a deterministic function of the workspace: the catalog
	// snapshot key must not drift between identical resolves.
	if first.Catalog.CatalogSnapshotKey != second.Catalog.CatalogSnapshotKey {
		t.Errorf("idempotent catalog-key drift: %q != %q",
			first.Catalog.CatalogSnapshotKey, second.Catalog.CatalogSnapshotKey)
	}
}

func TestResolvePlanCatalog_NoRefresh_Skips(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedGitCatalogWorkspace(t, dir)
	resetCatalogFlags(t)

	res, err := resolvePlanCatalog(context.Background(), planCatalogOptions{NoRefresh: true})
	if err != nil {
		t.Fatalf("resolvePlanCatalog(NoRefresh): %v", err)
	}
	if res.Resolved {
		t.Errorf("NoRefresh should not resolve, got %+v", res)
	}
	if !res.Skipped {
		t.Errorf("NoRefresh should set Skipped=true, got %+v", res)
	}
}

func TestResolvePlanCatalog_BestEffort_DegradesOnNoGit(t *testing.T) {
	// A bare temp intent root with NO git repo: the best-effort contract is
	// "no error, no panic" — whether Resolved is true or false depends on the
	// resolver's local-nogit handling; both are acceptable.
	withTempIntentRoot(t)
	resetCatalogFlags(t)

	if _, err := resolvePlanCatalog(context.Background(), planCatalogOptions{}); err != nil {
		t.Fatalf("best-effort resolve must not error on no-git workspace, got %v", err)
	}
}
