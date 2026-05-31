package catalogstore_test

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/statestore"
)

// rebuild_test.go covers catalog-store.md §8 — RebuildIndexes. The
// load-bearing assertion is T-STORE-3: scrub every index file, call
// RebuildIndexes, then byte-compare the rebuilt index files against the
// originals captured before the scrub.

// TestRebuildIndexes_ByteIdenticalAfterScrub is T-STORE-3. We seed a
// catalog tree the Writer way (WriteSourceSnapshot, WriteCatalogSnapshot,
// WriteGlobalIndexes), capture every `indexes/*.json` body, scrub them,
// then call RebuildIndexes and assert each rebuilt body equals the
// original byte-for-byte.
func TestRebuildIndexes_ByteIdenticalAfterScrub(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	src := makeSource()
	cat := makeCatalog()
	mfA := makeManifest("aaa")
	mfB := makeManifest("bbb")
	manifests := []catalogmodel.ComponentManifest{mfA, mfB}

	// Seed the source/catalog/manifest tree.
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("WriteSourceSnapshot: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, src, cat, manifests, catalogstore.CatalogGraphs{}, catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("WriteCatalogSnapshot: %v", err)
	}

	// Seed the global indexes the same way the Writer does — the rebuild
	// must produce byte-identical bodies.
	cgiA := makeRebuildCGI(mfA, src, cat.CatalogSnapshotKey)
	cgiB := makeRebuildCGI(mfB, src, cat.CatalogSnapshotKey)
	if err := st.WriteGlobalIndexes(ctx, catalogstore.GlobalIndexUpdate{
		Source:     &src,
		Catalog:    &cat,
		Components: []*catalogmodel.ComponentGlobalIndex{cgiA, cgiB},
	}); err != nil {
		t.Fatalf("WriteGlobalIndexes: %v", err)
	}

	// Capture original bodies.
	originals := snapshotIndexBodies(spy)
	if len(originals) == 0 {
		t.Fatalf("expected at least one index body in spy; got 0")
	}

	// Scrub.
	for p := range originals {
		spy.deletePath(p)
	}
	if got := snapshotIndexBodies(spy); len(got) != 0 {
		t.Fatalf("scrub leaked: %d remaining", len(got))
	}

	// Rebuild.
	if err := st.RebuildIndexes(ctx); err != nil {
		t.Fatalf("RebuildIndexes: %v", err)
	}

	// Assert byte-identical.
	rebuilt := snapshotIndexBodies(spy)
	if len(rebuilt) != len(originals) {
		t.Fatalf("rebuilt count %d != original count %d (originals=%v rebuilt=%v)",
			len(rebuilt), len(originals), keysOf(originals), keysOf(rebuilt))
	}
	for p, orig := range originals {
		got, ok := rebuilt[p]
		if !ok {
			t.Errorf("rebuilt missing path %s", p)
			continue
		}
		if !bytes.Equal(got, orig) {
			t.Errorf("path %s: rebuilt body diverges from original\n--- orig ---\n%s\n--- rebuilt ---\n%s", p, orig, got)
		}
	}

	// Also sanity-check every kind of index file appeared.
	wantKinds := map[string]bool{
		"indexes/sources/":    false,
		"indexes/catalogs/":   false,
		"indexes/components/": false,
	}
	for p := range rebuilt {
		for prefix := range wantKinds {
			if strings.HasPrefix(p, prefix) {
				wantKinds[prefix] = true
			}
		}
	}
	for prefix, seen := range wantKinds {
		if !seen {
			t.Errorf("T-STORE-3: no rebuilt body under %s — rebuild missed an index kind", prefix)
		}
	}
}

// TestRebuildIndexes_IdempotentSecondRebuild — calling RebuildIndexes
// twice produces byte-identical bodies on the second pass too.
func TestRebuildIndexes_IdempotentSecondRebuild(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	src := makeSource()
	cat := makeCatalog()
	mf := makeManifest("aaa")
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("WriteSourceSnapshot: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, src, cat, []catalogmodel.ComponentManifest{mf}, catalogstore.CatalogGraphs{}, catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("WriteCatalogSnapshot: %v", err)
	}

	if err := st.RebuildIndexes(ctx); err != nil {
		t.Fatalf("first RebuildIndexes: %v", err)
	}
	first := snapshotIndexBodies(spy)
	if err := st.RebuildIndexes(ctx); err != nil {
		t.Fatalf("second RebuildIndexes: %v", err)
	}
	second := snapshotIndexBodies(spy)

	if len(first) != len(second) {
		t.Fatalf("count drift: first=%d second=%d", len(first), len(second))
	}
	for p, b := range first {
		if !bytes.Equal(b, second[p]) {
			t.Errorf("path %s: second rebuild diverges from first", p)
		}
	}
}

// TestRebuildIndexes_NoSources — empty store: rebuild succeeds and writes
// nothing.
func TestRebuildIndexes_NoSources(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	if err := st.RebuildIndexes(context.Background()); err != nil {
		t.Fatalf("RebuildIndexes on empty store: %v", err)
	}
	if got := snapshotIndexBodies(spy); len(got) != 0 {
		t.Errorf("empty store rebuild wrote %d index bodies", len(got))
	}
}

// TestRebuildIndexes_MultiSourceUnionsPreviews — a feature-branch source
// alongside a main source produces a CGI with main pointer from the main
// source and a previews entry from the feature branch.
func TestRebuildIndexes_MultiSourceUnionsPreviews(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	mainSrc := makeSource()
	mainSrc.SourceSnapshotKey = "src-branch-main-cabcdef-tabcdef0"
	mainSrc.SourceScope = catalogmodel.SourceScopeBranchMain
	mainSrc.CreatedAt = "2026-05-30T00:00:00Z"
	mainCat := makeCatalog()
	mainCat.SourceSnapshotKey = mainSrc.SourceSnapshotKey
	mainCat.CatalogSnapshotKey = "cat-aaaaaaaa"
	mainMf := makeManifest("aaa")
	mainMf.Source.SourceSnapshotKey = mainSrc.SourceSnapshotKey
	mainMf.Source.CatalogSnapshotKey = mainCat.CatalogSnapshotKey

	prSrc := makeSource()
	prSrc.SourceSnapshotKey = "src-pr-pr-139-cabcdef-tabcdef1"
	prSrc.SourceScope = catalogmodel.SourceScopePR
	prSrc.Branch = "feat/x"
	prSrc.CreatedAt = "2026-05-31T00:00:00Z"
	prCat := makeCatalog()
	prCat.SourceSnapshotKey = prSrc.SourceSnapshotKey
	prCat.CatalogSnapshotKey = "cat-bbbbbbbb"
	prCat.Authoritative = false
	prMf := makeManifest("aaa")
	prMf.Source.SourceSnapshotKey = prSrc.SourceSnapshotKey
	prMf.Source.CatalogSnapshotKey = prCat.CatalogSnapshotKey

	if err := st.WriteSourceSnapshot(ctx, mainSrc); err != nil {
		t.Fatalf("write main src: %v", err)
	}
	if err := st.WriteSourceSnapshot(ctx, prSrc); err != nil {
		t.Fatalf("write pr src: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, mainSrc, mainCat, []catalogmodel.ComponentManifest{mainMf}, catalogstore.CatalogGraphs{}, catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("write main catalog: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, prSrc, prCat, []catalogmodel.ComponentManifest{prMf}, catalogstore.CatalogGraphs{}, catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("write pr catalog: %v", err)
	}

	if err := st.RebuildIndexes(ctx); err != nil {
		t.Fatalf("RebuildIndexes: %v", err)
	}

	cgi, err := st.ResolveComponentLatest(ctx, mainMf.Identity.ComponentKey)
	if err != nil {
		t.Fatalf("ResolveComponentLatest: %v", err)
	}
	// PR source is newest → its Latest pointer wins under merge ordering
	// (sort by CreatedAt asc, last writer freshest).
	if cgi.Latest.SourceSnapshotKey != prSrc.SourceSnapshotKey {
		t.Errorf("Latest.SourceSnapshotKey=%q want pr=%q", cgi.Latest.SourceSnapshotKey, prSrc.SourceSnapshotKey)
	}
	// Main pointer was set by the branch-main source and the PR shard
	// does not overwrite it (its Main has empty SourceSnapshotKey, so
	// the merge keeps the existing main).
	if cgi.Main.SourceSnapshotKey != mainSrc.SourceSnapshotKey {
		t.Errorf("Main.SourceSnapshotKey=%q want main=%q", cgi.Main.SourceSnapshotKey, mainSrc.SourceSnapshotKey)
	}
	// PR appeared in Previews.
	foundPreview := false
	for _, p := range cgi.Previews {
		if p.SourceSnapshotKey == prSrc.SourceSnapshotKey && p.SourceScope == catalogmodel.SourceScopePR {
			foundPreview = true
		}
	}
	if !foundPreview {
		t.Errorf("expected pr source in Previews; got %v", cgi.Previews)
	}
}

// TestRebuildIndexes_SourceWriteErrorSurfaces — Write failure on the
// indexes/sources/* path is surfaced verbatim.
func TestRebuildIndexes_SourceWriteErrorSurfaces(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	src := makeSource()
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srcIdxPath, _ := catalogstore.SourceGlobalIndexPath(src.SourceSnapshotKey)
	boom := errors.New("boom")
	spy.writeErr[srcIdxPath] = boom

	err := st.RebuildIndexes(ctx)
	if err == nil || !errors.Is(err, boom) {
		t.Errorf("expected wrapped boom, got %v", err)
	}
}

// TestRebuildIndexes_CatalogWriteErrorSurfaces — Write failure on the
// indexes/catalogs/* path.
func TestRebuildIndexes_CatalogWriteErrorSurfaces(t *testing.T) {
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
	catIdxPath, _ := catalogstore.CatalogGlobalIndexPath(cat.CatalogSnapshotKey)
	boom := errors.New("cat-boom")
	spy.writeErr[catIdxPath] = boom

	err := st.RebuildIndexes(ctx)
	if err == nil || !errors.Is(err, boom) {
		t.Errorf("expected wrapped cat-boom, got %v", err)
	}
}

// TestRebuildIndexes_ComponentWriteErrorSurfaces — Write failure on the
// indexes/components/* path.
func TestRebuildIndexes_ComponentWriteErrorSurfaces(t *testing.T) {
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
	cgiPath, _ := catalogstore.ComponentGlobalIndexPath(mf.Identity.ComponentKey)
	boom := errors.New("cgi-boom")
	spy.writeErr[cgiPath] = boom

	err := st.RebuildIndexes(ctx)
	if err == nil || !errors.Is(err, boom) {
		t.Errorf("expected wrapped cgi-boom, got %v", err)
	}
}

// TestRebuildIndexes_SkipsCorruptManifest — a manifest body that fails to
// JSON-decode is skipped, not fatal.
func TestRebuildIndexes_SkipsCorruptManifest(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	src := makeSource()
	cat := makeCatalog()
	mfGood := makeManifest("aaa")
	if err := st.WriteSourceSnapshot(ctx, src); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	if err := st.WriteCatalogSnapshot(ctx, src, cat, []catalogmodel.ComponentManifest{mfGood}, catalogstore.CatalogGraphs{}, catalogstore.CatalogLocalIndexes{}); err != nil {
		t.Fatalf("seed cat: %v", err)
	}
	// Inject a corrupt sibling manifest.
	corruptPath, _ := catalogstore.ComponentManifestPath(testSrcKey, testCatKey, "bbb")
	if _, err := spy.Write(ctx, corruptPath, []byte("{this is not json"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("seed corrupt: %v", err)
	}

	if err := st.RebuildIndexes(ctx); err != nil {
		t.Fatalf("RebuildIndexes (corrupt sibling): %v", err)
	}
	// Good CGI present.
	goodPath, _ := catalogstore.ComponentGlobalIndexPath(mfGood.Identity.ComponentKey)
	if _, ok := spy.objects[goodPath]; !ok {
		t.Errorf("good CGI missing at %s", goodPath)
	}
}

// ----- helpers --------------------------------------------------------

// snapshotIndexBodies returns a copy of every `indexes/*.json` body in
// the spy.
func snapshotIndexBodies(s *spyStore) map[string][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string][]byte{}
	for p, b := range s.objects {
		if !strings.HasPrefix(p, "indexes/") {
			continue
		}
		out[p] = append([]byte(nil), b...)
	}
	return out
}

// deletePath removes one path from the spy. Used for the scrub step.
func (s *spyStore) deletePath(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, p)
	delete(s.revisions, p)
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// makeRebuildCGI is the test-side mirror of
// rebuild.go:buildComponentGlobalIndexShard for branch-main scope. The
// rebuild test seeds the originals via this helper so the byte-identical
// post-condition can be asserted without depending on the rebuild
// implementation's internal helper.
func makeRebuildCGI(m catalogmodel.ComponentManifest, src catalogmodel.SourceSnapshot, catKey string) *catalogmodel.ComponentGlobalIndex {
	loc := catalogmodel.ComponentIndexLocation{
		SourceSnapshotKey:  src.SourceSnapshotKey,
		CatalogSnapshotKey: catKey,
	}
	mp, _ := catalogstore.ComponentManifestPath(src.SourceSnapshotKey, catKey, m.Identity.Name)
	main := loc
	main.ManifestPath = mp
	cgi := &catalogmodel.ComponentGlobalIndex{
		APIVersion:   m.APIVersion,
		Kind:         catalogmodel.KindComponentGlobal,
		ComponentKey: m.Identity.ComponentKey,
		Name:         m.Identity.Name,
		Repo:         src.Repo,
		Latest:       loc,
		Main:         main,
		Previews:     []catalogmodel.ComponentIndexPreview{},
	}
	return cgi
}
