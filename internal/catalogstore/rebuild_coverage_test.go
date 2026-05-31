package catalogstore_test

import (
	"context"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/statestore"
)

// rebuild_coverage_test.go holds verifier-attached (Task 0035) focused
// coverage top-ups for rebuild.go. Each test drives a previously
// unexercised but reachable branch through the public RebuildIndexes
// API: corrupt source.json skip (listAllSources), corrupt catalog.json
// skip + shared-catalogKey dedup (collectAllCatalogs), and the
// CreatedAt-tie sort comparators in the component-merge ordering.

const testSrcKey2 = "src-branch-main-cabcdef-tabcdef1"

// TestRebuildIndexes_SkipsCorruptSourceDoc — a malformed
// sources/<key>/source.json is skipped by listAllSources; the
// well-formed source still rebuilds. Covers rebuild.go:155.
func TestRebuildIndexes_SkipsCorruptSourceDoc(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	src := makeSource()
	cat := makeCatalog()
	mf := makeManifest("aaa")
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, src, cat, []catalogmodel.ComponentManifest{mf}, catalogstore.CatalogGraphs{}, catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("seed cat: %v", err)
	}

	// Inject a corrupt sibling source.json under a different source key.
	corruptSrcPath, _ := catalogstore.SourceDocPath(testSrcKey2)
	if _, err := spy.Write(ctx, corruptSrcPath, []byte("{not json"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("seed corrupt source: %v", err)
	}

	if err := st.RebuildIndexes(ctx); err != nil {
		t.Fatalf("RebuildIndexes (corrupt source sibling): %v", err)
	}
	// The good source index is present; corrupt one produced nothing.
	goodIdx, _ := catalogstore.SourceGlobalIndexPath(src.SourceSnapshotKey)
	if _, ok := spy.objects[goodIdx]; !ok {
		t.Errorf("good source index missing at %s", goodIdx)
	}
	badIdx, _ := catalogstore.SourceGlobalIndexPath(testSrcKey2)
	if _, ok := spy.objects[badIdx]; ok {
		t.Errorf("corrupt source must not yield an index at %s", badIdx)
	}
}

// TestRebuildIndexes_SkipsCorruptCatalogDoc — a malformed
// sources/<src>/catalogs/<cat>/catalog.json is skipped by
// collectAllCatalogs; the well-formed catalog still rebuilds. Covers
// rebuild.go:191.
func TestRebuildIndexes_SkipsCorruptCatalogDoc(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	src := makeSource()
	cat := makeCatalog()
	mf := makeManifest("aaa")
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, src, cat, []catalogmodel.ComponentManifest{mf}, catalogstore.CatalogGraphs{}, catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("seed cat: %v", err)
	}

	// Inject a corrupt sibling catalog.json under a different catalog key.
	corruptCatPath, _ := catalogstore.CatalogDocPath(src.SourceSnapshotKey, "cat-badbadba")
	if _, err := spy.Write(ctx, corruptCatPath, []byte("{not json"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("seed corrupt catalog: %v", err)
	}

	if err := st.RebuildIndexes(ctx); err != nil {
		t.Fatalf("RebuildIndexes (corrupt catalog sibling): %v", err)
	}
	goodIdx, _ := catalogstore.CatalogGlobalIndexPath(cat.CatalogSnapshotKey)
	if _, ok := spy.objects[goodIdx]; !ok {
		t.Errorf("good catalog index missing at %s", goodIdx)
	}
	badIdx, _ := catalogstore.CatalogGlobalIndexPath("cat-badbadba")
	if _, ok := spy.objects[badIdx]; ok {
		t.Errorf("corrupt catalog must not yield an index at %s", badIdx)
	}
}

// TestRebuildIndexes_DedupsSharedCatalogKey — two sources whose
// catalog.json bodies share the same CatalogSnapshotKey are deduped by
// collectAllCatalogs (the second occurrence hits the `seen` guard).
// Covers rebuild.go:194.
func TestRebuildIndexes_DedupsSharedCatalogKey(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	src1 := makeSource() // testSrcKey
	src2 := makeSource()
	src2.SourceSnapshotKey = testSrcKey2

	// Both catalogs carry the SAME CatalogSnapshotKey but are linked to
	// their respective sources (WriteCatalogSnapshot enforces linkage).
	cat1 := makeCatalog()
	cat1.SourceSnapshotKey = src1.SourceSnapshotKey
	cat2 := makeCatalog()
	cat2.SourceSnapshotKey = src2.SourceSnapshotKey
	// cat1 and cat2 share CatalogSnapshotKey == testCatKey by default.

	if err := st.WriteSourceSnapshot(ctx, src1); err != nil {
		t.Fatalf("seed src1: %v", err)
	}
	if err := st.WriteSourceSnapshot(ctx, src2); err != nil {
		t.Fatalf("seed src2: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, src1, cat1, nil, catalogstore.CatalogGraphs{}, catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("seed cat1: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, src2, cat2, nil, catalogstore.CatalogGraphs{}, catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("seed cat2: %v", err)
	}

	if err := st.RebuildIndexes(ctx); err != nil {
		t.Fatalf("RebuildIndexes (shared catalog key): %v", err)
	}
	// Exactly one catalog global index for the shared key.
	idx, _ := catalogstore.CatalogGlobalIndexPath(testCatKey)
	if _, ok := spy.objects[idx]; !ok {
		t.Errorf("shared catalog index missing at %s", idx)
	}
}

// TestRebuildIndexes_SortTieBreakSameCreatedAtDiffSource — two sources
// with identical CreatedAt force the merge-order comparator past the
// CreatedAt arm into the SourceSnapshotKey tiebreak. Covers
// rebuild.go:102-104.
func TestRebuildIndexes_SortTieBreakSameCreatedAtDiffSource(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	const sameTime = "2026-05-31T00:00:00Z"

	src1 := makeSource() // testSrcKey, CreatedAt sameTime by default
	src1.CreatedAt = sameTime
	cat1 := makeCatalog()
	cat1.SourceSnapshotKey = src1.SourceSnapshotKey
	mf1 := makeManifest("aaa")
	mf1.Source.SourceSnapshotKey = src1.SourceSnapshotKey
	mf1.Source.CatalogSnapshotKey = cat1.CatalogSnapshotKey

	src2 := makeSource()
	src2.SourceSnapshotKey = testSrcKey2
	src2.CreatedAt = sameTime
	cat2 := makeCatalog()
	cat2.SourceSnapshotKey = src2.SourceSnapshotKey
	cat2.CatalogSnapshotKey = "cat-bbbbbbbb"
	mf2 := makeManifest("aaa") // same componentKey -> merged CGI
	mf2.Source.SourceSnapshotKey = src2.SourceSnapshotKey
	mf2.Source.CatalogSnapshotKey = cat2.CatalogSnapshotKey

	if err := st.WriteSourceSnapshot(ctx, src1); err != nil {
		t.Fatalf("seed src1: %v", err)
	}
	if err := st.WriteSourceSnapshot(ctx, src2); err != nil {
		t.Fatalf("seed src2: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, src1, cat1, []catalogmodel.ComponentManifest{mf1}, catalogstore.CatalogGraphs{}, catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("seed cat1: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, src2, cat2, []catalogmodel.ComponentManifest{mf2}, catalogstore.CatalogGraphs{}, catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("seed cat2: %v", err)
	}

	if err := st.RebuildIndexes(ctx); err != nil {
		t.Fatalf("RebuildIndexes (same CreatedAt, diff source): %v", err)
	}
	cgi, err := st.ResolveComponentLatest(ctx, mf1.Identity.ComponentKey)
	if err != nil {
		t.Fatalf("ResolveComponentLatest: %v", err)
	}
	// Higher srcKey (testSrcKey2 > testSrcKey) sorts last -> freshest.
	if cgi.Latest.SourceSnapshotKey != testSrcKey2 {
		t.Errorf("Latest.SourceSnapshotKey=%q want %q (srcKey tiebreak)", cgi.Latest.SourceSnapshotKey, testSrcKey2)
	}
}

// TestRebuildIndexes_SortTieBreakSameSourceDiffCatalog — one source with
// two catalogs (identical CreatedAt and SourceSnapshotKey) forces the
// comparator past the CreatedAt and SourceSnapshotKey arms into the
// CatalogSnapshotKey tiebreak. Covers rebuild.go:105-107.
func TestRebuildIndexes_SortTieBreakSameSourceDiffCatalog(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	src := makeSource()
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed src: %v", err)
	}

	catA := makeCatalog()
	catA.CatalogSnapshotKey = "cat-aaaaaaaa"
	mfA := makeManifest("aaa")
	mfA.Source.CatalogSnapshotKey = catA.CatalogSnapshotKey

	catB := makeCatalog()
	catB.CatalogSnapshotKey = "cat-bbbbbbbb"
	mfB := makeManifest("aaa") // same componentKey -> merged CGI
	mfB.Source.CatalogSnapshotKey = catB.CatalogSnapshotKey

	if err := st.WriteCatalogSnapshot(ctx, src, catA, []catalogmodel.ComponentManifest{mfA}, catalogstore.CatalogGraphs{}, catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("seed catA: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, src, catB, []catalogmodel.ComponentManifest{mfB}, catalogstore.CatalogGraphs{}, catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("seed catB: %v", err)
	}

	if err := st.RebuildIndexes(ctx); err != nil {
		t.Fatalf("RebuildIndexes (same source, diff catalog): %v", err)
	}
	cgi, err := st.ResolveComponentLatest(ctx, mfA.Identity.ComponentKey)
	if err != nil {
		t.Fatalf("ResolveComponentLatest: %v", err)
	}
	// Higher catKey (cat-bbbbbbbb > cat-aaaaaaaa) sorts last -> freshest.
	if cgi.Latest.CatalogSnapshotKey != "cat-bbbbbbbb" {
		t.Errorf("Latest.CatalogSnapshotKey=%q want cat-bbbbbbbb (catKey tiebreak)", cgi.Latest.CatalogSnapshotKey)
	}
}

// writeRaw seeds a JSON body at an arbitrary path, bypassing the Writer
// validation, so the rebuild walk decodes a structurally-valid document
// whose embedded key fails the per-write Validate*Key guard.
func writeRaw(t *testing.T, spy *spyStore, path string, v any) {
	t.Helper()
	body, err := catalogmodel.PrettyEncode(v)
	if err != nil {
		t.Fatalf("encode raw %s: %v", path, err)
	}
	if _, err := spy.Write(context.Background(), path, body, statestore.WriteOptions{}); err != nil {
		t.Fatalf("write raw %s: %v", path, err)
	}
}

// TestRebuildIndexes_InvalidSourceKeySurfaces — a source.json whose
// embedded SourceSnapshotKey is invalid decodes cleanly during the walk
// but trips ValidateSourceKey inside rebuildSourceGlobalIndex, surfacing
// a wrapped error. Covers rebuild.go:245-246.
func TestRebuildIndexes_InvalidSourceKeySurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)

	// Path key is well-formed (so listAllSources enumerates it), but the
	// decoded body carries an invalid SourceSnapshotKey.
	src := makeSource()
	src.SourceSnapshotKey = "NOT A VALID KEY"
	writeRaw(t, spy, "sources/"+testSrcKey+"/source.json", src)

	if err := st.RebuildIndexes(context.Background()); err == nil {
		t.Fatalf("expected error from invalid source key, got nil")
	}
}

// TestRebuildIndexes_InvalidCatalogKeySurfaces — a catalog.json whose
// embedded CatalogSnapshotKey is invalid trips ValidateCatalogKey inside
// rebuildCatalogGlobalIndex. Covers rebuild.go:265-266.
func TestRebuildIndexes_InvalidCatalogKeySurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	src := makeSource()
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	// Well-formed path under the source's catalogs subtree, but the
	// decoded body carries an invalid CatalogSnapshotKey.
	cat := makeCatalog()
	cat.CatalogSnapshotKey = "NOT A VALID KEY"
	writeRaw(t, spy, "sources/"+src.SourceSnapshotKey+"/catalogs/"+testCatKey+"/catalog.json", cat)

	if err := st.RebuildIndexes(ctx); err == nil {
		t.Fatalf("expected error from invalid catalog key, got nil")
	}
}

// TestRebuildIndexes_InvalidComponentKeySurfaces — a manifest.json whose
// embedded ComponentKey is invalid trips ValidateComponentKey inside
// writeComponentGlobalIndexPlain. Covers rebuild.go:287-288.
func TestRebuildIndexes_InvalidComponentKeySurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	src := makeSource()
	cat := makeCatalog()
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, src, cat, nil, catalogstore.CatalogGraphs{}, catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("seed cat: %v", err)
	}
	// Well-formed manifest path, but the decoded body carries an invalid
	// ComponentKey that survives the merge and fails at write time.
	mf := makeManifest("aaa")
	mf.Identity.ComponentKey = "NOT A VALID KEY"
	writeRaw(t, spy, "sources/"+src.SourceSnapshotKey+"/catalogs/"+cat.CatalogSnapshotKey+"/components/aaa/manifest.json", mf)

	if err := st.RebuildIndexes(ctx); err == nil {
		t.Fatalf("expected error from invalid component key, got nil")
	}
}
