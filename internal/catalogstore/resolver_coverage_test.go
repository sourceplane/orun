package catalogstore_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/statestore"
)

// resolver_coverage_test.go holds verifier-attached (Task 0035) focused
// coverage top-ups for resolver.go: the `pr` selector arms in the
// ref-path mappers, and the fallback-walk skip branches
// (non-matching/nested siblings, corrupt bodies) in
// fallbackMostRecentSource / fallbackMostRecentCatalog.

// TestResolveSource_PRSelectorAbsent exercises the `pr` arm of
// sourceRefPathForSelector (resolver.go:213) — the PR ref path is
// computed and, absent, surfaces a typed not-found.
func TestResolveSource_PRSelectorAbsent(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	_, err := st.ResolveSource(context.Background(), catalogstore.RefSelector{Kind: "pr", PR: "139"})
	if err == nil || !errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("expected typed not-found for absent pr source ref, got %v", err)
	}
}

// TestResolveCatalog_PRSelectorAbsent exercises the `pr` arm of
// catalogRefPathForSelector (resolver.go:235).
func TestResolveCatalog_PRSelectorAbsent(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	_, err := st.ResolveCatalog(context.Background(), catalogstore.RefSelector{Kind: "pr", PR: "139"})
	if err == nil || !errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("expected typed not-found for absent pr catalog ref, got %v", err)
	}
}

// TestResolveCurrentSource_FallbackSkipsNonMatching seeds a corrupt
// source.json plus a nested non-source object so the §4 fallback walk
// exercises the suffix/slash-count/decode skip arms
// (fallbackMostRecentSource resolver.go:308-323) while still resolving
// the one well-formed source.
func TestResolveCurrentSource_FallbackSkipsNonMatching(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	good := makeSource()
	if err := st.WriteSourceSnapshot(ctx, good); err != nil {
		t.Fatalf("seed good source: %v", err)
	}
	// Nested non-source object under sources/ — fails the slash-count and
	// suffix filters in the fallback walk.
	if _, err := spy.Write(ctx, "sources/"+testSrcKey2+"/catalogs/cat-x/catalog.json", []byte("{}"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("seed nested: %v", err)
	}
	// Corrupt sibling source.json — fails the decode skip arm.
	if _, err := spy.Write(ctx, "sources/"+testSrcKey2+"/source.json", []byte("{not json"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("seed corrupt source: %v", err)
	}

	// No refs/sources/current.json present -> fallback walk runs.
	got, err := st.ResolveCurrentSource(ctx)
	if err != nil {
		t.Fatalf("ResolveCurrentSource fallback: %v", err)
	}
	if got.SourceSnapshotKey != good.SourceSnapshotKey {
		t.Errorf("fallback resolved %q, want %q", got.SourceSnapshotKey, good.SourceSnapshotKey)
	}
}

// TestResolveCatalog_FallbackSkipsNonMatching seeds a corrupt
// catalog.json plus a nested non-catalog object so the §4 row-2 fallback
// walk exercises the skip arms in fallbackMostRecentCatalog
// (resolver.go:355-364) while resolving the one well-formed catalog.
func TestResolveCatalog_FallbackSkipsNonMatching(t *testing.T) {
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
	base := "sources/" + src.SourceSnapshotKey + "/catalogs/"
	// Nested non-catalog object — fails slash-count/suffix filters.
	if _, err := spy.Write(ctx, base+"cat-other/components/aaa/manifest.json", []byte("{}"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("seed nested: %v", err)
	}
	// Corrupt sibling catalog.json — fails the decode skip arm.
	if _, err := spy.Write(ctx, base+"cat-corrupt/catalog.json", []byte("{not json"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("seed corrupt catalog: %v", err)
	}

	// No refs/catalogs/current.json -> fallback walk runs (current selector).
	got, err := st.ResolveCatalog(ctx, catalogstore.RefSelector{Kind: "current"})
	if err != nil {
		t.Fatalf("ResolveCatalog fallback: %v", err)
	}
	if got.CatalogSnapshotKey != cat.CatalogSnapshotKey {
		t.Errorf("fallback resolved %q, want %q", got.CatalogSnapshotKey, cat.CatalogSnapshotKey)
	}
}

// TestResolveComponentLatest_ReadErrorSurfaces injects a non-NotFound
// read error on the component global index path so the wrapped-error arm
// (resolver.go:169) is exercised rather than the typed-not-found arm.
func TestResolveComponentLatest_ReadErrorSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	key := "sourceplane/orun/aaa"
	p, _ := catalogstore.ComponentGlobalIndexPath(key)
	boom := errors.New("io-boom")
	spy.readErr[p] = boom

	_, err := st.ResolveComponentLatest(ctx, key)
	if err == nil || !errors.Is(err, boom) {
		t.Errorf("expected wrapped io-boom, got %v", err)
	}
	if errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("non-NotFound read error must not map to ErrNotFound: %v", err)
	}
}

// TestResolveComponent_ReadErrorSurfaces injects a non-NotFound read
// error on the manifest path (after the catalog resolves) so the
// wrapped-error arm (resolver.go:139) is exercised.
func TestResolveComponent_ReadErrorSurfaces(t *testing.T) {
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
	mp, _ := catalogstore.ComponentManifestPath(src.SourceSnapshotKey, cat.CatalogSnapshotKey, "aaa")
	boom := errors.New("mf-boom")
	spy.readErr[mp] = boom

	_, err := st.ResolveComponent(ctx, catalogstore.RefSelector{Kind: "current"}, "aaa")
	if err == nil || !errors.Is(err, boom) {
		t.Errorf("expected wrapped mf-boom, got %v", err)
	}
}

// TestResolveCatalog_ReadErrorSurfaces injects a non-NotFound read error
// on the catalog doc path (reached via an explicit ref) so the
// wrapped-error arm of readCatalogByKeys (resolver.go:279) is exercised.
func TestResolveCatalog_ReadErrorSurfaces(t *testing.T) {
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
	// Seed a current catalog ref pointing at the catalog doc.
	sref := makeSourceRef("current")
	cref := makeCatalogRef("current")
	if err := st.WriteRefs(ctx, catalogstore.RefUpdate{
		Source:  &sref,
		Catalog: &cref,
	}); err != nil {
		t.Fatalf("seed refs: %v", err)
	}
	catPath, _ := catalogstore.CatalogDocPath(src.SourceSnapshotKey, cat.CatalogSnapshotKey)
	boom := errors.New("cat-read-boom")
	spy.readErr[catPath] = boom

	_, err := st.ResolveCatalog(ctx, catalogstore.RefSelector{Kind: "current"})
	if err == nil || !errors.Is(err, boom) {
		t.Errorf("expected wrapped cat-read-boom, got %v", err)
	}
}
