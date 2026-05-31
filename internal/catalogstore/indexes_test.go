package catalogstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/statestore"
)

func makeComponentGlobalIndex(name string) *catalogmodel.ComponentGlobalIndex {
	return &catalogmodel.ComponentGlobalIndex{
		APIVersion:   "orun.io/v1alpha1",
		Kind:         "ComponentGlobalIndex",
		ComponentKey: "sourceplane/orun/" + name,
		Name:         name,
		Repo:         "sourceplane/orun",
		Latest: catalogmodel.ComponentIndexLocation{
			SourceSnapshotKey:  testSrcKey,
			CatalogSnapshotKey: testCatKey,
		},
		Main: catalogmodel.ComponentIndexLocation{
			SourceSnapshotKey:  testSrcKey,
			CatalogSnapshotKey: testCatKey,
		},
	}
}

// TestWriteGlobalIndexes_NoOpWhenEmpty.
func TestWriteGlobalIndexes_NoOpWhenEmpty(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	if err := st.WriteGlobalIndexes(context.Background(), catalogstore.GlobalIndexUpdate{}); err != nil {
		t.Fatalf("empty update must be no-op success, got %v", err)
	}
	if len(spy.trace) != 0 {
		t.Errorf("no writes; got %v", spy.trace)
	}
}

// TestWriteGlobalIndexes_HappyPath_WritesSourceCatalogAndComponents.
func TestWriteGlobalIndexes_HappyPath_WritesSourceCatalogAndComponents(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)

	src := makeSource()
	cat := makeCatalog()
	comps := []*catalogmodel.ComponentGlobalIndex{
		makeComponentGlobalIndex("aaa"),
		makeComponentGlobalIndex("bbb"),
	}

	if err := st.WriteGlobalIndexes(context.Background(), catalogstore.GlobalIndexUpdate{
		Source:     &src,
		Catalog:    &cat,
		Components: comps,
	}); err != nil {
		t.Fatalf("WriteGlobalIndexes: %v", err)
	}

	// Verify source/catalog global indexes via plain Write.
	srcPath, _ := catalogstore.SourceGlobalIndexPath(testSrcKey)
	catPath, _ := catalogstore.CatalogGlobalIndexPath(testCatKey)
	if _, ok := spy.objects[srcPath]; !ok {
		t.Errorf("missing source global index at %s", srcPath)
	}
	if _, ok := spy.objects[catPath]; !ok {
		t.Errorf("missing catalog global index at %s", catPath)
	}
	for _, c := range comps {
		p, _ := catalogstore.ComponentGlobalIndexPath(c.ComponentKey)
		if _, ok := spy.objects[p]; !ok {
			t.Errorf("missing component global index at %s", p)
		}
	}

	// First-time component writes go through CreateIfAbsent, NOT plain
	// Write — this is what gives us the CAS-on-conflict guarantee.
	for _, c := range comps {
		p, _ := catalogstore.ComponentGlobalIndexPath(c.ComponentKey)
		want := "create:" + p
		found := false
		for _, e := range spy.trace {
			if e == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing CreateIfAbsent for %s in trace: %v", p, spy.trace)
		}
	}
}

// TestWriteGlobalIndexes_DeterministicComponentOrder — components are
// processed by sorted ComponentKey ascending.
func TestWriteGlobalIndexes_DeterministicComponentOrder(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	// Provide in reverse alpha order; expect ascending ordering in trace.
	comps := []*catalogmodel.ComponentGlobalIndex{
		makeComponentGlobalIndex("zzz"),
		makeComponentGlobalIndex("mmm"),
		makeComponentGlobalIndex("aaa"),
	}
	if err := st.WriteGlobalIndexes(context.Background(), catalogstore.GlobalIndexUpdate{
		Components: comps,
	}); err != nil {
		t.Fatalf("%v", err)
	}
	// Extract component-create order from trace.
	var got []string
	for _, e := range spy.trace {
		if strings.HasPrefix(e, "create:indexes/components/") {
			got = append(got, e)
		}
	}
	if len(got) != 3 {
		t.Fatalf("want 3 component creates, got %v", got)
	}
	wantOrder := []string{"aaa", "mmm", "zzz"}
	for i, suf := range wantOrder {
		if !strings.Contains(got[i], suf) {
			t.Errorf("creates[%d]=%q want suffix %s", i, got[i], suf)
		}
	}
}

// TestWriteGlobalIndexes_ComponentMergeOnConflict — pre-existing index
// with a different preview gets merged with the caller's new entry.
func TestWriteGlobalIndexes_ComponentMergeOnConflict(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)

	want := makeComponentGlobalIndex("aaa")
	want.Previews = []catalogmodel.ComponentIndexPreview{
		{
			SourceScope:        "pr-139",
			SourceSnapshotKey:  "src-pr-139-cnewnew-tnewnewx",
			CatalogSnapshotKey: "cat-newcafe",
		},
	}
	p, _ := catalogstore.ComponentGlobalIndexPath(want.ComponentKey)

	// Pre-seed with an index containing a DIFFERENT preview.
	preExisting := *makeComponentGlobalIndex("aaa")
	preExisting.Previews = []catalogmodel.ComponentIndexPreview{
		{
			SourceScope:        "pr-100",
			SourceSnapshotKey:  "src-pr-100-coldold-toldoldx",
			CatalogSnapshotKey: "cat-oldcafe",
		},
	}
	preBody, _ := catalogmodel.PrettyEncode(preExisting)
	spy.preExisting[p] = preBody

	if err := st.WriteGlobalIndexes(context.Background(), catalogstore.GlobalIndexUpdate{
		Components: []*catalogmodel.ComponentGlobalIndex{want},
	}); err != nil {
		t.Fatalf("WriteGlobalIndexes: %v", err)
	}

	// Read back and verify both previews survived.
	body, ok := spy.objects[p]
	if !ok {
		t.Fatalf("merged body missing at %s", p)
	}
	var merged catalogmodel.ComponentGlobalIndex
	if err := json.Unmarshal(body, &merged); err != nil {
		t.Fatalf("decode merged: %v", err)
	}
	if len(merged.Previews) != 2 {
		t.Errorf("expected 2 merged previews, got %d (%+v)", len(merged.Previews), merged.Previews)
	}
	scopes := map[string]bool{}
	for _, p := range merged.Previews {
		scopes[p.SourceScope] = true
	}
	if !scopes["pr-100"] || !scopes["pr-139"] {
		t.Errorf("expected both pr-100 and pr-139 in merged previews, got %v", merged.Previews)
	}
}

// TestWriteGlobalIndexes_RetryExhaustedReturnsErrRefStale.
func TestWriteGlobalIndexes_RetryExhaustedReturnsErrRefStale(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	want := makeComponentGlobalIndex("aaa")
	want.Previews = []catalogmodel.ComponentIndexPreview{
		{SourceScope: "pr-1", SourceSnapshotKey: "src-pr-1-cnewx-tnewxx0", CatalogSnapshotKey: "cat-newxx"},
	}
	p, _ := catalogstore.ComponentGlobalIndexPath(want.ComponentKey)
	// Pre-seed with a divergent body — different preview so the merge
	// produces a body strictly different from what's stored.
	pre := *makeComponentGlobalIndex("aaa")
	pre.Previews = []catalogmodel.ComponentIndexPreview{
		{SourceScope: "pr-99", SourceSnapshotKey: "src-pr-99-coldxx-toldxx0", CatalogSnapshotKey: "cat-oldxx"},
	}
	body, _ := catalogmodel.PrettyEncode(pre)
	spy.preExisting[p] = body
	// Force more conflicts than budget.
	spy.casConflicts[p] = 100

	err := st.WriteGlobalIndexes(context.Background(), catalogstore.GlobalIndexUpdate{
		Components: []*catalogmodel.ComponentGlobalIndex{want},
	})
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

// TestWriteGlobalIndexes_InvalidComponentKey — surface as ErrInvalidPathInput.
func TestWriteGlobalIndexes_InvalidComponentKey(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	bad := makeComponentGlobalIndex("aaa")
	bad.ComponentKey = "BAD KEY"
	err := st.WriteGlobalIndexes(context.Background(), catalogstore.GlobalIndexUpdate{
		Components: []*catalogmodel.ComponentGlobalIndex{bad},
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, catalogstore.ErrInvalidPathInput) {
		t.Errorf("err not ErrInvalidPathInput chain: %v", err)
	}
}

// TestWriteGlobalIndexes_NilComponentSkipped — nil entries are filtered
// silently.
func TestWriteGlobalIndexes_NilComponentSkipped(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	good := makeComponentGlobalIndex("aaa")
	if err := st.WriteGlobalIndexes(context.Background(), catalogstore.GlobalIndexUpdate{
		Components: []*catalogmodel.ComponentGlobalIndex{nil, good, nil},
	}); err != nil {
		t.Fatalf("nil components must not be a hard error, got %v", err)
	}
	p, _ := catalogstore.ComponentGlobalIndexPath(good.ComponentKey)
	if _, ok := spy.objects[p]; !ok {
		t.Errorf("good component should still be written; objects=%v", keys(spy.objects))
	}
}
