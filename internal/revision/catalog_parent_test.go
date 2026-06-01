package revision

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/statestore"
)

// C6 — catalog-parent mirror. These tests assert the additive Phase 2 layout
// is written under sources/<srcKey>/catalogs/<catKey>/revisions/<revKey>/ and
// is byte-identical to the Phase 1 global layout, while the global layout is
// itself unaffected by the presence/absence of CatalogParent.

const (
	testSrcKey = "src-branch-main-cdef456a-t5ab21c3"
	testCatKey = "cat-c8e91d2a"
)

func writerCfgWithParent(store statestore.StateStore, now time.Time) Config {
	cfg := newWriterCfg(store, now)
	cfg.CatalogParent = CatalogParentRef{SourceKey: testSrcKey, CatalogKey: testCatKey}
	return cfg
}

// TestWriteRevision_CatalogParent_ByteIdentical verifies the three body
// files mirrored under the catalog parent match the Phase 1 globals exactly.
func TestWriteRevision_CatalogParent_ByteIdentical(t *testing.T) {
	store := newTestStore(t)
	trig := newTestTrigger(t)
	cfg := writerCfgWithParent(store, time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC))
	plan := []byte(`{"apiVersion":"orun.io/v1alpha1","kind":"Plan","jobs":[]}`)
	planHash := "feedface00112233445566778899aabbccddeeff00112233"

	rev, err := WriteRevision(context.Background(), cfg, trig, plan, planHash)
	if err != nil {
		t.Fatalf("WriteRevision: %v", err)
	}
	if err := WriteManifest(context.Background(), cfg, rev, trig); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	type pair struct {
		name   string
		global string
		parent func() (string, error)
	}
	parents := []pair{
		{"plan.json", statestore.PlanPath(rev.RevisionKey), func() (string, error) {
			return catalogstore.CatalogRevisionPlanPath(testSrcKey, testCatKey, rev.RevisionKey)
		}},
		{"trigger.json", statestore.TriggerPath(rev.RevisionKey), func() (string, error) {
			return catalogstore.CatalogRevisionTriggerPath(testSrcKey, testCatKey, rev.RevisionKey)
		}},
		{"revision.json", statestore.RevisionDocPath(rev.RevisionKey), func() (string, error) {
			return catalogstore.CatalogRevisionDocPath(testSrcKey, testCatKey, rev.RevisionKey)
		}},
		{"manifest.json", statestore.ManifestPath(rev.RevisionKey), func() (string, error) {
			return catalogstore.CatalogRevisionManifestPath(testSrcKey, testCatKey, rev.RevisionKey)
		}},
	}
	for _, p := range parents {
		globalRaw, _, err := store.Read(context.Background(), p.global)
		if err != nil {
			t.Fatalf("read global %s: %v", p.name, err)
		}
		parentPath, err := p.parent()
		if err != nil {
			t.Fatalf("parent path %s: %v", p.name, err)
		}
		parentRaw, _, err := store.Read(context.Background(), parentPath)
		if err != nil {
			t.Fatalf("read catalog-parent %s at %q: %v", p.name, parentPath, err)
		}
		if string(globalRaw) != string(parentRaw) {
			t.Errorf("%s diverged between layouts\n global=%s\n parent=%s", p.name, globalRaw, parentRaw)
		}
	}
}

// TestWriteRevision_CatalogParent_Inactive verifies that with no
// CatalogParent (the Phase 1 / --no-catalog-refresh case) NO files appear
// under sources/ and the global layout is still written.
func TestWriteRevision_CatalogParent_Inactive(t *testing.T) {
	store := newTestStore(t)
	trig := newTestTrigger(t)
	cfg := newWriterCfg(store, time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC)) // no CatalogParent
	plan := []byte(`{"apiVersion":"orun.io/v1alpha1","kind":"Plan","jobs":[]}`)
	planHash := "feedface00112233445566778899aabbccddeeff00112233"

	rev, err := WriteRevision(context.Background(), cfg, trig, plan, planHash)
	if err != nil {
		t.Fatalf("WriteRevision: %v", err)
	}
	if err := WriteManifest(context.Background(), cfg, rev, trig); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	// Global layout present.
	if _, _, err := store.Read(context.Background(), statestore.PlanPath(rev.RevisionKey)); err != nil {
		t.Fatalf("global plan.json missing: %v", err)
	}
	// Catalog parent absent.
	parentPlan, err := catalogstore.CatalogRevisionPlanPath(testSrcKey, testCatKey, rev.RevisionKey)
	if err != nil {
		t.Fatalf("parent path: %v", err)
	}
	if _, _, err := store.Read(context.Background(), parentPlan); !errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("catalog-parent plan.json unexpectedly present when CatalogParent inactive: err=%v", err)
	}
}

// TestWriteRevision_CatalogParent_PartialKeysSkip verifies that a half-filled
// CatalogParent (only one key set) is treated as inactive — no parent writes,
// no error. Guards the Active() invariant.
func TestWriteRevision_CatalogParent_PartialKeysSkip(t *testing.T) {
	cases := []CatalogParentRef{
		{SourceKey: testSrcKey},  // catalog key empty
		{CatalogKey: testCatKey}, // source key empty
		{},                       // both empty
	}
	for i, parent := range cases {
		store := newTestStore(t)
		trig := newTestTrigger(t)
		cfg := newWriterCfg(store, time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC))
		cfg.CatalogParent = parent
		plan := []byte(`{"apiVersion":"orun.io/v1alpha1","kind":"Plan"}`)
		planHash := "feedface00112233445566778899aabbccddeeff00112233"
		rev, err := WriteRevision(context.Background(), cfg, trig, plan, planHash)
		if err != nil {
			t.Fatalf("case %d WriteRevision: %v", i, err)
		}
		// Use a fully-valid pair only to construct the would-be path; with a
		// partial parent nothing should have been written under sources/.
		parentPlan, perr := catalogstore.CatalogRevisionPlanPath(testSrcKey, testCatKey, rev.RevisionKey)
		if perr != nil {
			t.Fatalf("case %d parent path: %v", i, perr)
		}
		if _, _, err := store.Read(context.Background(), parentPlan); !errors.Is(err, statestore.ErrNotFound) {
			t.Errorf("case %d: parent written for partial CatalogParent %+v: err=%v", i, parent, err)
		}
	}
}

// TestCatalogParentRef_Active is a direct unit check on the gating predicate.
func TestCatalogParentRef_Active(t *testing.T) {
	if !(CatalogParentRef{SourceKey: "s", CatalogKey: "c"}).Active() {
		t.Error("both keys set should be active")
	}
	for _, c := range []CatalogParentRef{{}, {SourceKey: "s"}, {CatalogKey: "c"}} {
		if c.Active() {
			t.Errorf("%+v should be inactive", c)
		}
	}
}
