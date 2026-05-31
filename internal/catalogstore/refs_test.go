package catalogstore_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/statestore"
)

// ----- ref fixture helpers -------------------------------------------

func makeSourceRef(name string) catalogmodel.SourceRef {
	return catalogmodel.SourceRef{
		APIVersion:        "orun.io/v1alpha1",
		Kind:              "SourceRef",
		Name:              name,
		SourceScope:       catalogmodel.SourceScopeBranchMain,
		SourceSnapshotKey: testSrcKey,
		HeadRevision:      "abcdef0",
		TreeHash:          "abcdef0",
		WorkingTree:       catalogmodel.WorkingTreeClean,
		Authoritative:     true,
		UpdatedAt:         "2026-05-31T00:00:00Z",
	}
}

func makeCatalogRef(name string) catalogmodel.CatalogRef {
	return catalogmodel.CatalogRef{
		APIVersion:         "orun.io/v1alpha1",
		Kind:               "CatalogRef",
		Name:               name,
		SourceScope:        catalogmodel.SourceScopeBranchMain,
		SourceSnapshotKey:  testSrcKey,
		CatalogSnapshotKey: testCatKey,
		CatalogHash:        "sha256:deadbeef",
		HeadRevision:       "abcdef0",
		TreeHash:           "abcdef0",
		Authoritative:      true,
		UpdatedAt:          "2026-05-31T00:00:00Z",
	}
}

// ----- step D happy path ---------------------------------------------

// TestWriteRefs_HappyPath_OrderAndPaths verifies the canonical write
// order from spec §3.D: current → main (when authoritative) → latest,
// with sources ordered before catalogs at every step.
func TestWriteRefs_HappyPath_OrderAndPaths(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)

	src := makeSourceRef(catalogmodel.RefNameCurrent)
	cat := makeCatalogRef(catalogmodel.RefNameCurrent)

	if err := st.WriteRefs(context.Background(), catalogstore.RefUpdate{
		Source:  &src,
		Catalog: &cat,
	}); err != nil {
		t.Fatalf("WriteRefs: %v", err)
	}

	// Both src and cat are Authoritative=true → main is in the list.
	// Branch/PR are blank → those are skipped. Final D.6 is latest.
	wantPathsInOrder := []string{
		"refs/sources/current.json",
		"refs/catalogs/current.json",
		"refs/sources/main.json",
		"refs/catalogs/main.json",
		"refs/sources/latest.json",
		"refs/catalogs/latest.json",
	}
	// Each ref should have triggered a `create:` (initial CreateIfAbsent
	// succeeds because the spy is empty).
	creates := filterTracePrefix(spy.trace, "create:")
	if len(creates) != len(wantPathsInOrder) {
		t.Fatalf("expected %d creates, got %d: %v", len(wantPathsInOrder), len(creates), creates)
	}
	for i, want := range wantPathsInOrder {
		if creates[i] != "create:"+want {
			t.Errorf("creates[%d]=%q, want %q", i, creates[i], "create:"+want)
		}
	}
	// All bodies must be present.
	for _, p := range wantPathsInOrder {
		if _, ok := spy.objects[p]; !ok {
			t.Errorf("missing object %s", p)
		}
	}
}

// TestWriteRefs_OnlySourceOrCatalog skips the missing side entirely.
func TestWriteRefs_OnlySourceOrCatalog(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSourceRef(catalogmodel.RefNameCurrent)
	if err := st.WriteRefs(context.Background(), catalogstore.RefUpdate{Source: &src}); err != nil {
		t.Fatalf("source-only WriteRefs: %v", err)
	}
	for _, e := range spy.trace {
		if strings.Contains(e, "/refs/catalogs/") {
			t.Errorf("catalog ref written when Catalog==nil: %q", e)
		}
	}
}

// TestWriteRefs_SkipMainWhenNotAuthoritative — Authoritative=false omits
// the main pointer for that side.
func TestWriteRefs_SkipMainWhenNotAuthoritative(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSourceRef(catalogmodel.RefNameCurrent)
	src.Authoritative = false
	cat := makeCatalogRef(catalogmodel.RefNameCurrent)
	cat.Authoritative = false
	if err := st.WriteRefs(context.Background(), catalogstore.RefUpdate{
		Source: &src, Catalog: &cat,
	}); err != nil {
		t.Fatalf("%v", err)
	}
	for _, e := range spy.trace {
		if strings.Contains(e, "main.json") {
			t.Errorf("main ref must be skipped when Authoritative=false: %q", e)
		}
	}
}

// TestWriteRefs_BranchAndPRSelection — Branch/PR scopes are emitted as
// per-side ref files, with branch sanitized via SanitizeBranch.
func TestWriteRefs_BranchAndPRSelection(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSourceRef(catalogmodel.RefNameCurrent)
	cat := makeCatalogRef(catalogmodel.RefNameCurrent)
	if err := st.WriteRefs(context.Background(), catalogstore.RefUpdate{
		Source: &src, Catalog: &cat,
		Branch: "feature/Foo Bar", // requires sanitization
		PR:     "139",
	}); err != nil {
		t.Fatalf("%v", err)
	}
	sanitized := catalogmodel.SanitizeBranch("feature/Foo Bar")
	wantBranchSrc := "refs/sources/branches/" + sanitized + ".json"
	wantBranchCat := "refs/catalogs/branches/" + sanitized + ".json"
	wantPRSrc := "refs/sources/prs/139.json"
	wantPRCat := "refs/catalogs/prs/139.json"
	for _, p := range []string{wantBranchSrc, wantBranchCat, wantPRSrc, wantPRCat} {
		if _, ok := spy.objects[p]; !ok {
			t.Errorf("missing %s; have %v", p, keys(spy.objects))
		}
	}
}

// TestWriteRefs_BranchSanitizesToEmptyReturnsErrInvalidPathInput.
func TestWriteRefs_BranchSanitizesToEmptyReturnsErrInvalidPathInput(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSourceRef(catalogmodel.RefNameCurrent)
	err := st.WriteRefs(context.Background(), catalogstore.RefUpdate{
		Source: &src,
		Branch: "///", // sanitizes to empty
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, catalogstore.ErrInvalidPathInput) {
		t.Errorf("err not ErrInvalidPathInput chain: %v", err)
	}
}

// TestWriteRefs_IdempotentOnByteIdenticalRewrite — second call with
// identical inputs hits ErrExists with byte-identical bodies and is a
// success without entering the CAS loop.
func TestWriteRefs_IdempotentOnByteIdenticalRewrite(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSourceRef(catalogmodel.RefNameCurrent)
	cat := makeCatalogRef(catalogmodel.RefNameCurrent)
	if err := st.WriteRefs(context.Background(), catalogstore.RefUpdate{Source: &src, Catalog: &cat}); err != nil {
		t.Fatalf("first: %v", err)
	}
	traceLen := len(spy.trace)
	if err := st.WriteRefs(context.Background(), catalogstore.RefUpdate{Source: &src, Catalog: &cat}); err != nil {
		t.Errorf("byte-identical re-write should be idempotent, got %v", err)
	}
	// Re-write should produce only create+read pairs (per ref) — no CAS
	// entries because byte-identical bodies short-circuit before the
	// retry loop.
	for _, e := range spy.trace[traceLen:] {
		if strings.HasPrefix(e, "cas:") {
			t.Errorf("idempotent re-write should not enter CAS: %q", e)
		}
	}
}

// TestWriteRefs_RetryThenSuccess — body diverges, but CAS conflicts
// resolve before the budget exhausts.
func TestWriteRefs_RetryThenSuccess(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSourceRef(catalogmodel.RefNameCurrent)

	// Pre-seed the current.json path with a divergent body so the
	// initial CreateIfAbsent hits ErrExists with a non-matching body
	// and we enter the CAS loop.
	currentPath := "refs/sources/current.json"
	spy.preExisting[currentPath] = []byte(`{"different":"body"}`)
	// Force 2 CAS conflicts before letting the third succeed.
	spy.casConflicts[currentPath] = 2

	if err := st.WriteRefs(context.Background(), catalogstore.RefUpdate{Source: &src}); err != nil {
		t.Fatalf("expected eventual success, got %v", err)
	}
	// Verify the trace contains exactly 3 cas: entries on current.json
	// (2 conflicts + 1 success).
	casCount := 0
	for _, e := range spy.trace {
		if strings.HasPrefix(e, "cas:"+currentPath+":") {
			casCount++
		}
	}
	if casCount != 3 {
		t.Errorf("want 3 CAS attempts on %s, got %d", currentPath, casCount)
	}
}

// TestWriteRefs_BudgetExhaustedReturnsErrRefStale — exceeds 16 attempts.
func TestWriteRefs_BudgetExhaustedReturnsErrRefStale(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	src := makeSourceRef(catalogmodel.RefNameCurrent)

	currentPath := "refs/sources/current.json"
	spy.preExisting[currentPath] = []byte(`{"different":"body"}`)
	// Force more conflicts than the budget allows.
	spy.casConflicts[currentPath] = 100

	err := st.WriteRefs(context.Background(), catalogstore.RefUpdate{Source: &src})
	if err == nil {
		t.Fatalf("expected ErrRefStale")
	}
	if !errors.Is(err, catalogstore.ErrRefStale) {
		t.Errorf("err not ErrRefStale chain: %v", err)
	}
	if !errors.Is(err, statestore.ErrConflict) {
		t.Errorf("err must wrap statestore.ErrConflict: %v", err)
	}
}

// TestWriteRefs_NoOpWhenBothNil — empty RefUpdate is a successful no-op.
func TestWriteRefs_NoOpWhenBothNil(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	if err := st.WriteRefs(context.Background(), catalogstore.RefUpdate{}); err != nil {
		t.Fatalf("empty RefUpdate must be no-op success, got %v", err)
	}
	if len(spy.trace) != 0 {
		t.Errorf("no writes; got %v", spy.trace)
	}
}

// ----- helper ---------------------------------------------------------

func filterTracePrefix(trace []string, prefix string) []string {
	out := []string{}
	for _, e := range trace {
		if strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}
