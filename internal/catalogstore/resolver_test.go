package catalogstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/statestore"
)

// resolver_test.go covers the C4 PR-3 Resolver surface declared in
// store.go and implemented in resolver.go. Each test seeds the spyStore
// directly via the writer surface (or canonical PrettyEncode against a
// path helper) so the tests pin the read path without depending on
// internal canonical encoding details.

// ----- ResolveSource / ResolveCurrentSource --------------------------

func TestResolveCurrentSource_HappyPath_ViaCurrentRef(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	// Seed source body + current ref via the writer surface.
	src := makeSource()
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	if err := st.WriteRefs(ctx, catalogstore.RefUpdate{
		Source: ptrSourceRef(catalogmodel.RefNameCurrent),
	}); err != nil {
		t.Fatalf("seed ref: %v", err)
	}

	got, err := st.ResolveCurrentSource(ctx)
	if err != nil {
		t.Fatalf("ResolveCurrentSource: %v", err)
	}
	if got.SourceSnapshotKey != src.SourceSnapshotKey {
		t.Errorf("key=%q want %q", got.SourceSnapshotKey, src.SourceSnapshotKey)
	}
}

func TestResolveSource_BySnapshotKey(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	src := makeSource()
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := st.ResolveSource(ctx, catalogstore.RefSelector{Snapshot: src.SourceSnapshotKey})
	if err != nil {
		t.Fatalf("ResolveSource: %v", err)
	}
	if got.SourceSnapshotKey != src.SourceSnapshotKey {
		t.Errorf("key=%q want %q", got.SourceSnapshotKey, src.SourceSnapshotKey)
	}
}

func TestResolveSource_ByMainRef(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	src := makeSource()
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.WriteRefs(ctx, catalogstore.RefUpdate{
		Source: ptrSourceRef(catalogmodel.RefNameMain),
	}); err != nil {
		t.Fatalf("seed ref: %v", err)
	}
	got, err := st.ResolveSource(ctx, catalogstore.RefSelector{Kind: catalogmodel.RefNameMain})
	if err != nil {
		t.Fatalf("ResolveSource(main): %v", err)
	}
	if got.SourceSnapshotKey != src.SourceSnapshotKey {
		t.Errorf("key=%q want %q", got.SourceSnapshotKey, src.SourceSnapshotKey)
	}
}

func TestResolveSource_FallbackPicksMostRecent(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	older := makeSource()
	older.SourceSnapshotKey = "src-branch-main-c1111111-t1111111"
	older.HeadRevision = "1111111"
	older.TreeHash = "1111111"
	older.CreatedAt = "2026-05-30T00:00:00Z"

	newer := makeSource()
	newer.SourceSnapshotKey = "src-branch-main-c2222222-t2222222"
	newer.HeadRevision = "2222222"
	newer.TreeHash = "2222222"
	newer.CreatedAt = "2026-06-01T00:00:00Z"

	if err := st.WriteSourceSnapshot(ctx, older); err != nil {
		t.Fatalf("seed older: %v", err)
	}
	if err := st.WriteSourceSnapshot(ctx, newer); err != nil {
		t.Fatalf("seed newer: %v", err)
	}
	// No refs/sources/current.json — fallback walk MUST run.

	got, err := st.ResolveCurrentSource(ctx)
	if err != nil {
		t.Fatalf("ResolveCurrentSource: %v", err)
	}
	if got.SourceSnapshotKey != newer.SourceSnapshotKey {
		t.Errorf("fallback picked %q, want most-recent %q", got.SourceSnapshotKey, newer.SourceSnapshotKey)
	}
}

func TestResolveCurrentSource_EmptyStoreReturnsNotFound(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	_, err := st.ResolveCurrentSource(context.Background())
	if err == nil {
		t.Fatalf("expected NotFound error")
	}
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("err must wrap statestore.ErrNotFound: %v", err)
	}
}

func TestResolveSource_NonCurrentRefMissingReturnsNotFound(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	// No fallback for "main" — typed-ref miss surfaces directly.
	_, err := st.ResolveSource(context.Background(), catalogstore.RefSelector{Kind: catalogmodel.RefNameMain})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("err must wrap statestore.ErrNotFound: %v", err)
	}
}

func TestResolveSource_BranchSelector(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()
	src := makeSource()
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Hand-write a branch ref via the writer's branch path — the
	// writer fans branch refs when Branch is set on the RefUpdate.
	if err := st.WriteRefs(ctx, catalogstore.RefUpdate{
		Source: ptrSourceRef(catalogmodel.RefNameMain),
		Branch: "main",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := st.ResolveSource(ctx, catalogstore.RefSelector{Kind: "branch", Branch: "main"})
	if err != nil {
		t.Fatalf("ResolveSource(branch=main): %v", err)
	}
	if got.SourceSnapshotKey != src.SourceSnapshotKey {
		t.Errorf("key=%q want %q", got.SourceSnapshotKey, src.SourceSnapshotKey)
	}
}

func TestResolveSource_BranchSelectorMissingBranchErrs(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	_, err := st.ResolveSource(context.Background(), catalogstore.RefSelector{Kind: "branch"})
	if err == nil || !errors.Is(err, catalogstore.ErrInvalidPathInput) {
		t.Errorf("want ErrInvalidPathInput, got %v", err)
	}
}

func TestResolveSource_PRSelectorMissingPRErrs(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	_, err := st.ResolveSource(context.Background(), catalogstore.RefSelector{Kind: "pr"})
	if err == nil || !errors.Is(err, catalogstore.ErrInvalidPathInput) {
		t.Errorf("want ErrInvalidPathInput, got %v", err)
	}
}

func TestResolveSource_UnknownSelectorKindErrs(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	_, err := st.ResolveSource(context.Background(), catalogstore.RefSelector{Kind: "bogus"})
	if err == nil || !errors.Is(err, catalogstore.ErrInvalidPathInput) {
		t.Errorf("want ErrInvalidPathInput, got %v", err)
	}
}

func TestResolveSource_BadSnapshotKeyErrs(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	_, err := st.ResolveSource(context.Background(), catalogstore.RefSelector{Snapshot: "BAD"})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestResolveSource_CorruptRefBodyErrs(t *testing.T) {
	spy := newSpyStore()
	refPath, _ := catalogstore.SourceRefPath(catalogmodel.RefNameCurrent)
	spy.objects[refPath] = []byte("{not-json")
	spy.revisions[refPath] = spy.nextRev()
	st := catalogstore.New(spy)
	_, err := st.ResolveCurrentSource(context.Background())
	if err == nil {
		t.Fatalf("expected decode error")
	}
}

// ----- ResolveCatalog -------------------------------------------------

func TestResolveCatalog_HappyPath_ViaCurrentRef(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()
	src := makeSource()
	cat := makeCatalog()
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, src, cat, nil, makeAllGraphs(), catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("seed cat: %v", err)
	}
	if err := st.WriteRefs(ctx, catalogstore.RefUpdate{
		Source:  ptrSourceRef(catalogmodel.RefNameCurrent),
		Catalog: ptrCatalogRef(catalogmodel.RefNameCurrent),
	}); err != nil {
		t.Fatalf("seed refs: %v", err)
	}
	got, err := st.ResolveCatalog(ctx, catalogstore.RefSelector{})
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	if got.CatalogSnapshotKey != cat.CatalogSnapshotKey {
		t.Errorf("key=%q want %q", got.CatalogSnapshotKey, cat.CatalogSnapshotKey)
	}
}

func TestResolveCatalog_FallbackPicksMostRecent(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()
	src := makeSource()
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	// Seed current source ref so ResolveCurrentSource hits the typed
	// pointer (not the source-side fallback).
	if err := st.WriteRefs(ctx, catalogstore.RefUpdate{
		Source: ptrSourceRef(catalogmodel.RefNameCurrent),
	}); err != nil {
		t.Fatalf("seed src ref: %v", err)
	}

	older := makeCatalog()
	older.CatalogSnapshotKey = "cat-11111111"
	older.CatalogHash = "sha256:1111"
	older.CreatedAt = "2026-05-30T00:00:00Z"

	newer := makeCatalog()
	newer.CatalogSnapshotKey = "cat-22222222"
	newer.CatalogHash = "sha256:2222"
	newer.CreatedAt = "2026-06-01T00:00:00Z"

	for _, c := range []catalogmodel.CatalogSnapshot{older, newer} {
		if err := st.WriteCatalogSnapshot(ctx, src, c, nil, makeAllGraphs(), catalogstore.CatalogLocalIndexes{}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// No refs/catalogs/current.json — fallback walk runs.

	got, err := st.ResolveCatalog(ctx, catalogstore.RefSelector{})
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	if got.CatalogSnapshotKey != newer.CatalogSnapshotKey {
		t.Errorf("picked %q, want most-recent %q", got.CatalogSnapshotKey, newer.CatalogSnapshotKey)
	}
}

func TestResolveCatalog_FallbackNoSourceReturnsCatalogNotFound(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	_, err := st.ResolveCatalog(context.Background(), catalogstore.RefSelector{})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, catalogstore.ErrCatalogNotFound) {
		t.Errorf("err not ErrCatalogNotFound chain: %v", err)
	}
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("err must wrap statestore.ErrNotFound: %v", err)
	}
}

func TestResolveCatalog_FallbackSourceButNoCatalogReturnsCatalogNotFound(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()
	src := makeSource()
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Source falls back via List walk; no catalogs exist.
	_, err := st.ResolveCatalog(ctx, catalogstore.RefSelector{})
	if !errors.Is(err, catalogstore.ErrCatalogNotFound) {
		t.Errorf("err not ErrCatalogNotFound chain: %v", err)
	}
}

func TestResolveCatalog_NonCurrentMissingReturnsCatalogNotFound(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	_, err := st.ResolveCatalog(context.Background(), catalogstore.RefSelector{Kind: catalogmodel.RefNameMain})
	if !errors.Is(err, catalogstore.ErrCatalogNotFound) {
		t.Errorf("want ErrCatalogNotFound, got %v", err)
	}
}

func TestResolveCatalog_UnknownKindErrs(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	_, err := st.ResolveCatalog(context.Background(), catalogstore.RefSelector{Kind: "bogus"})
	if !errors.Is(err, catalogstore.ErrInvalidPathInput) {
		t.Errorf("want ErrInvalidPathInput, got %v", err)
	}
}

func TestResolveCatalog_CorruptRefBodyErrs(t *testing.T) {
	spy := newSpyStore()
	refPath, _ := catalogstore.CatalogRefPath(catalogmodel.RefNameCurrent)
	spy.objects[refPath] = []byte("not-json")
	spy.revisions[refPath] = spy.nextRev()
	st := catalogstore.New(spy)
	_, err := st.ResolveCatalog(context.Background(), catalogstore.RefSelector{})
	if err == nil {
		t.Fatalf("expected decode error")
	}
}

// ----- ResolveComponent ----------------------------------------------

func TestResolveComponent_HappyPath(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()
	src := makeSource()
	cat := makeCatalog()
	manifests := []catalogmodel.ComponentManifest{makeManifest("aaa")}
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, src, cat, manifests, makeAllGraphs(), catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("seed cat: %v", err)
	}
	if err := st.WriteRefs(ctx, catalogstore.RefUpdate{
		Source:  ptrSourceRef(catalogmodel.RefNameCurrent),
		Catalog: ptrCatalogRef(catalogmodel.RefNameCurrent),
	}); err != nil {
		t.Fatalf("seed refs: %v", err)
	}
	got, err := st.ResolveComponent(ctx, catalogstore.RefSelector{}, "aaa")
	if err != nil {
		t.Fatalf("ResolveComponent: %v", err)
	}
	if got.Identity.Name != "aaa" {
		t.Errorf("name=%q want aaa", got.Identity.Name)
	}
}

func TestResolveComponent_MissingManifestReturnsComponentNotFound(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()
	src := makeSource()
	cat := makeCatalog()
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, src, cat, nil, makeAllGraphs(), catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("seed cat: %v", err)
	}
	if err := st.WriteRefs(ctx, catalogstore.RefUpdate{
		Source:  ptrSourceRef(catalogmodel.RefNameCurrent),
		Catalog: ptrCatalogRef(catalogmodel.RefNameCurrent),
	}); err != nil {
		t.Fatalf("seed refs: %v", err)
	}
	_, err := st.ResolveComponent(ctx, catalogstore.RefSelector{}, "ghost")
	if !errors.Is(err, catalogstore.ErrComponentNotFound) {
		t.Errorf("want ErrComponentNotFound chain, got %v", err)
	}
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("err must wrap statestore.ErrNotFound: %v", err)
	}
}

func TestResolveComponent_BadName(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	_, err := st.ResolveComponent(context.Background(), catalogstore.RefSelector{}, "BAD")
	if err == nil || !errors.Is(err, catalogstore.ErrInvalidPathInput) {
		t.Errorf("want ErrInvalidPathInput, got %v", err)
	}
}

func TestResolveComponent_CatalogMissingPropagates(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	_, err := st.ResolveComponent(context.Background(), catalogstore.RefSelector{}, "a")
	if !errors.Is(err, catalogstore.ErrCatalogNotFound) {
		t.Errorf("want ErrCatalogNotFound, got %v", err)
	}
}

// ----- ResolveComponentLatest ----------------------------------------

func TestResolveComponentLatest_HappyPath(t *testing.T) {
	spy := newSpyStore()
	ctx := context.Background()
	componentKey := "sourceplane/orun/aaa"
	idx := catalogmodel.ComponentGlobalIndex{
		APIVersion:   catalogmodel.APIVersionV1Alpha1,
		Kind:         catalogmodel.KindComponentGlobal,
		ComponentKey: componentKey,
		Name:         "aaa",
		Repo:         "sourceplane/orun",
		Latest: catalogmodel.ComponentIndexLocation{
			SourceSnapshotKey:  testSrcKey,
			CatalogSnapshotKey: testCatKey,
		},
		Main: catalogmodel.ComponentIndexLocation{
			SourceSnapshotKey:  testSrcKey,
			CatalogSnapshotKey: testCatKey,
			ManifestPath:       "sources/" + testSrcKey + "/catalogs/" + testCatKey + "/components/aaa/manifest.json",
		},
		Previews: []catalogmodel.ComponentIndexPreview{},
	}
	body, err := catalogmodel.PrettyEncode(idx)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	indexPath, err := catalogstore.ComponentGlobalIndexPath(componentKey)
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	spy.objects[indexPath] = body
	spy.revisions[indexPath] = spy.nextRev()

	st := catalogstore.New(spy)
	got, err := st.ResolveComponentLatest(ctx, componentKey)
	if err != nil {
		t.Fatalf("ResolveComponentLatest: %v", err)
	}
	if got.ComponentKey != componentKey {
		t.Errorf("componentKey=%q want %q", got.ComponentKey, componentKey)
	}
	if got.Main.ManifestPath == "" {
		t.Errorf("Main.ManifestPath should be populated")
	}
	if got.Latest.SourceSnapshotKey != testSrcKey {
		t.Errorf("Latest.SourceSnapshotKey=%q want %q", got.Latest.SourceSnapshotKey, testSrcKey)
	}
}

func TestResolveComponentLatest_MissingIndexReturnsComponentNotFound(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	_, err := st.ResolveComponentLatest(context.Background(), "sourceplane/orun/aaa")
	if !errors.Is(err, catalogstore.ErrComponentNotFound) {
		t.Errorf("want ErrComponentNotFound, got %v", err)
	}
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("err must wrap statestore.ErrNotFound: %v", err)
	}
}

func TestResolveComponentLatest_BadComponentKeyErrs(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	_, err := st.ResolveComponentLatest(context.Background(), "BAD")
	if err == nil || !errors.Is(err, catalogstore.ErrInvalidPathInput) {
		t.Errorf("want ErrInvalidPathInput, got %v", err)
	}
}

func TestResolveComponentLatest_CorruptIndexErrs(t *testing.T) {
	spy := newSpyStore()
	indexPath, _ := catalogstore.ComponentGlobalIndexPath("sourceplane/orun/aaa")
	spy.objects[indexPath] = []byte("{not-json")
	spy.revisions[indexPath] = spy.nextRev()
	st := catalogstore.New(spy)
	_, err := st.ResolveComponentLatest(context.Background(), "sourceplane/orun/aaa")
	if err == nil {
		t.Fatalf("expected decode error")
	}
}

// ----- fallback walk: read errors are skipped ------------------------

func TestResolveCurrentSource_FallbackSkipsCorruptBodies(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	// Seed one good source.
	good := makeSource()
	if err := st.WriteSourceSnapshot(ctx, good); err != nil {
		t.Fatalf("seed good: %v", err)
	}
	// Plant a corrupt source.json under a different srcKey directly.
	corruptKey := "src-branch-main-cffffff-tffffff0"
	corruptPath := "sources/" + corruptKey + "/source.json"
	spy.objects[corruptPath] = []byte("{not-json")
	spy.revisions[corruptPath] = spy.nextRev()

	got, err := st.ResolveCurrentSource(ctx)
	if err != nil {
		t.Fatalf("ResolveCurrentSource: %v", err)
	}
	if got.SourceSnapshotKey != good.SourceSnapshotKey {
		t.Errorf("expected to skip corrupt body and pick %q, got %q", good.SourceSnapshotKey, got.SourceSnapshotKey)
	}
}

// ----- guard: PrettyEncode round-trips through json.Unmarshal --------

// TestPrettyEncodeRoundTripsForResolver pins the assumption that the
// canonical encoded form decodes via plain json.Unmarshal. If a future
// PrettyEncode change breaks this, every Resolver decode breaks too —
// surface it here before downstream tests start failing.
func TestPrettyEncodeRoundTripsForResolver(t *testing.T) {
	src := makeSource()
	body, err := catalogmodel.PrettyEncode(src)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var got catalogmodel.SourceSnapshot
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.SourceSnapshotKey != src.SourceSnapshotKey {
		t.Errorf("round-trip key=%q want %q", got.SourceSnapshotKey, src.SourceSnapshotKey)
	}
}

// ----- selector path mapping -----------------------------------------

func TestResolveCatalog_BranchSelectorMissingBranchErrs(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	_, err := st.ResolveCatalog(context.Background(), catalogstore.RefSelector{Kind: "branch"})
	if !errors.Is(err, catalogstore.ErrInvalidPathInput) {
		t.Errorf("want ErrInvalidPathInput, got %v", err)
	}
}

func TestResolveCatalog_BranchSelectorEmptyAfterSanitizeErrs(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	// Branch sanitizes to empty when every char is invalid (e.g. "/").
	_, err := st.ResolveCatalog(context.Background(), catalogstore.RefSelector{Kind: "branch", Branch: "///"})
	if !errors.Is(err, catalogstore.ErrInvalidPathInput) {
		t.Errorf("want ErrInvalidPathInput, got %v", err)
	}
}

func TestResolveCatalog_PRSelector(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()
	src := makeSource()
	cat := makeCatalog()
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, src, cat, nil, makeAllGraphs(), catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.WriteRefs(ctx, catalogstore.RefUpdate{
		Source:  ptrSourceRef(catalogmodel.RefNameMain),
		Catalog: ptrCatalogRef(catalogmodel.RefNameMain),
		PR:      "139",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := st.ResolveCatalog(ctx, catalogstore.RefSelector{Kind: "pr", PR: "139"})
	if err != nil {
		t.Fatalf("ResolveCatalog(pr=139): %v", err)
	}
	if got.CatalogSnapshotKey != cat.CatalogSnapshotKey {
		t.Errorf("key=%q want %q", got.CatalogSnapshotKey, cat.CatalogSnapshotKey)
	}
}

func TestResolveCatalog_PRSelectorMissingPRErrs(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	_, err := st.ResolveCatalog(context.Background(), catalogstore.RefSelector{Kind: "pr"})
	if !errors.Is(err, catalogstore.ErrInvalidPathInput) {
		t.Errorf("want ErrInvalidPathInput, got %v", err)
	}
}

func TestResolveSource_BranchSelectorEmptyAfterSanitizeErrs(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	_, err := st.ResolveSource(context.Background(), catalogstore.RefSelector{Kind: "branch", Branch: "///"})
	if !errors.Is(err, catalogstore.ErrInvalidPathInput) {
		t.Errorf("want ErrInvalidPathInput, got %v", err)
	}
}

func TestResolveCatalog_NonNotFoundReadErrorPropagates(t *testing.T) {
	spy := newSpyStore()
	refPath, _ := catalogstore.CatalogRefPath(catalogmodel.RefNameCurrent)
	spy.readErr[refPath] = errors.New("disk fault")
	st := catalogstore.New(spy)
	_, err := st.ResolveCatalog(context.Background(), catalogstore.RefSelector{})
	if err == nil {
		t.Fatalf("expected error")
	}
	// Must NOT be classified as the typed not-found surface — generic
	// statestore failures must propagate as-is.
	if errors.Is(err, catalogstore.ErrCatalogNotFound) {
		t.Errorf("disk fault must not classify as ErrCatalogNotFound: %v", err)
	}
}

func TestResolveSource_NonNotFoundReadErrorPropagates(t *testing.T) {
	spy := newSpyStore()
	refPath, _ := catalogstore.SourceRefPath(catalogmodel.RefNameCurrent)
	spy.readErr[refPath] = errors.New("disk fault")
	st := catalogstore.New(spy)
	_, err := st.ResolveCurrentSource(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestResolveCatalog_ReadCatalogReadErrorPropagates(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()
	src := makeSource()
	cat := makeCatalog()
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, src, cat, nil, makeAllGraphs(), catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.WriteRefs(ctx, catalogstore.RefUpdate{
		Source:  ptrSourceRef(catalogmodel.RefNameCurrent),
		Catalog: ptrCatalogRef(catalogmodel.RefNameCurrent),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Inject a read fault on catalog.json.
	catPath, _ := catalogstore.CatalogDocPath(testSrcKey, testCatKey)
	spy.readErr[catPath] = errors.New("disk fault")
	_, err := st.ResolveCatalog(ctx, catalogstore.RefSelector{})
	if err == nil {
		t.Fatalf("expected error")
	}
	if errors.Is(err, catalogstore.ErrCatalogNotFound) {
		t.Errorf("non-NotFound must not classify as ErrCatalogNotFound: %v", err)
	}
}

func TestResolveCatalog_CorruptCatalogBodyErrs(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()
	src := makeSource()
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.WriteRefs(ctx, catalogstore.RefUpdate{
		Source:  ptrSourceRef(catalogmodel.RefNameCurrent),
		Catalog: ptrCatalogRef(catalogmodel.RefNameCurrent),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Plant corrupt catalog body at the path the ref points to.
	catPath, _ := catalogstore.CatalogDocPath(testSrcKey, testCatKey)
	spy.objects[catPath] = []byte("{not-json")
	spy.revisions[catPath] = spy.nextRev()
	_, err := st.ResolveCatalog(ctx, catalogstore.RefSelector{})
	if err == nil {
		t.Fatalf("expected decode error")
	}
}

func TestResolveSource_BySnapshotKeyMissingErrs(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	_, err := st.ResolveSource(context.Background(), catalogstore.RefSelector{Snapshot: testSrcKey})
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("want statestore.ErrNotFound, got %v", err)
	}
}

func TestResolveSource_BadSnapshotBodyErrs(t *testing.T) {
	spy := newSpyStore()
	docPath, _ := catalogstore.SourceDocPath(testSrcKey)
	spy.objects[docPath] = []byte("not-json")
	spy.revisions[docPath] = spy.nextRev()
	st := catalogstore.New(spy)
	_, err := st.ResolveSource(context.Background(), catalogstore.RefSelector{Snapshot: testSrcKey})
	if err == nil {
		t.Fatalf("expected decode error")
	}
}

func TestResolveComponent_NonNotFoundReadErrorPropagates(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()
	src := makeSource()
	cat := makeCatalog()
	manifests := []catalogmodel.ComponentManifest{makeManifest("aaa")}
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, src, cat, manifests, makeAllGraphs(), catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.WriteRefs(ctx, catalogstore.RefUpdate{
		Source:  ptrSourceRef(catalogmodel.RefNameCurrent),
		Catalog: ptrCatalogRef(catalogmodel.RefNameCurrent),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mp, _ := catalogstore.ComponentManifestPath(testSrcKey, testCatKey, "aaa")
	spy.readErr[mp] = errors.New("disk fault")
	_, err := st.ResolveComponent(ctx, catalogstore.RefSelector{}, "aaa")
	if err == nil {
		t.Fatalf("expected error")
	}
	if errors.Is(err, catalogstore.ErrComponentNotFound) {
		t.Errorf("disk fault must not classify as ErrComponentNotFound: %v", err)
	}
}

func TestResolveComponent_CorruptManifestBodyErrs(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()
	src := makeSource()
	cat := makeCatalog()
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, src, cat, nil, makeAllGraphs(), catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.WriteRefs(ctx, catalogstore.RefUpdate{
		Source:  ptrSourceRef(catalogmodel.RefNameCurrent),
		Catalog: ptrCatalogRef(catalogmodel.RefNameCurrent),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mp, _ := catalogstore.ComponentManifestPath(testSrcKey, testCatKey, "ghost")
	spy.objects[mp] = []byte("{not-json")
	spy.revisions[mp] = spy.nextRev()
	_, err := st.ResolveComponent(ctx, catalogstore.RefSelector{}, "ghost")
	if err == nil {
		t.Fatalf("expected decode error")
	}
}

// ptrSourceRef / ptrCatalogRef adapt the value-returning fixtures from
// refs_test.go into *catalogmodel.SourceRef / *catalogmodel.CatalogRef
// pointers for RefUpdate.Source / RefUpdate.Catalog.
func ptrSourceRef(name string) *catalogmodel.SourceRef {
	r := makeSourceRef(name)
	return &r
}

func ptrCatalogRef(name string) *catalogmodel.CatalogRef {
	r := makeCatalogRef(name)
	return &r
}
