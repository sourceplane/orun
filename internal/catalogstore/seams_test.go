package catalogstore_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogstore"
)

// ----- ParseRefSelector ----------------------------------------------

func TestParseRefSelector_Forms(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		snapshot string
		want     catalogstore.RefSelector
	}{
		{"empty defaults to current", "", "", catalogstore.RefSelector{Kind: "current"}},
		{"current", "current", "", catalogstore.RefSelector{Kind: "current"}},
		{"main", "main", "", catalogstore.RefSelector{Kind: "main"}},
		{"latest", "latest", "", catalogstore.RefSelector{Kind: "latest"}},
		{"branch", "branches/feature-x", "", catalogstore.RefSelector{Kind: "branch", Branch: "feature-x"}},
		{"pr canonical", "prs/139", "", catalogstore.RefSelector{Kind: "pr", PR: "139"}},
		{"pr alias", "pr-139", "", catalogstore.RefSelector{Kind: "pr", PR: "139"}},
		{"cat pin via source", "cat-deadbeef", "", catalogstore.RefSelector{Snapshot: "cat-deadbeef"}},
		{"snapshot pin wins over source", "main", "cat-deadbeef", catalogstore.RefSelector{Snapshot: "cat-deadbeef"}},
		{"whitespace trimmed source", "  main  ", "", catalogstore.RefSelector{Kind: "main"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := catalogstore.ParseRefSelector(tt.source, tt.snapshot)
			if err != nil {
				t.Fatalf("ParseRefSelector(%q, %q) unexpected err: %v", tt.source, tt.snapshot, err)
			}
			if got != tt.want {
				t.Errorf("ParseRefSelector(%q, %q) = %+v, want %+v", tt.source, tt.snapshot, got, tt.want)
			}
		})
	}
}

func TestParseRefSelector_Malformed(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		snapshot string
	}{
		{"unknown keyword", "bogus", ""},
		{"empty branch segment", "branches/", ""},
		{"nested branch segment", "branches/a/b", ""},
		{"empty pr segment", "prs/", ""},
		{"nested pr segment", "prs/1/2", ""},
		{"empty pr alias", "pr-", ""},
		{"snapshot pin without cat prefix", "", "deadbeef"},
		{"whitespace-only snapshot", "", "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := catalogstore.ParseRefSelector(tt.source, tt.snapshot)
			if err == nil {
				t.Fatalf("ParseRefSelector(%q, %q) expected error, got nil", tt.source, tt.snapshot)
			}
			var ie *catalogstore.ErrInvalidSelector
			if !errors.As(err, &ie) {
				t.Errorf("expected *ErrInvalidSelector, got %T: %v", err, err)
			}
		})
	}
}

// ----- AssembleBundle ------------------------------------------------

func makeSeamManifest(name, key string) *catalogmodel.ComponentManifest {
	return &catalogmodel.ComponentManifest{
		APIVersion: catalogmodel.APIVersionV1Alpha1,
		Kind:       catalogmodel.KindComponentManifest,
		Identity: catalogmodel.ComponentIdentity{
			ComponentID:  "comp_" + name,
			ComponentKey: key,
			Name:         name,
			Namespace:    "sourceplane",
			Repo:         "orun",
		},
		Source: catalogmodel.ComponentSource{
			SourceSnapshotKey:  testSrcKey,
			CatalogSnapshotKey: testCatKey,
			ManifestHash:       "sha256:" + name,
		},
	}
}

func TestAssembleBundle_RequiresSnapshot(t *testing.T) {
	_, err := catalogstore.AssembleBundle(catalogstore.BundleInputs{
		Source:    makeSource(),
		UpdatedAt: "2026-05-31T00:00:00Z",
	})
	if err == nil {
		t.Fatal("expected error for nil Snapshot, got nil")
	}
}

func TestAssembleBundle_RequiresUpdatedAt(t *testing.T) {
	cat := makeCatalog()
	_, err := catalogstore.AssembleBundle(catalogstore.BundleInputs{
		Source:   makeSource(),
		Snapshot: &cat,
	})
	if err == nil {
		t.Fatal("expected error for empty UpdatedAt, got nil")
	}
}

func TestAssembleBundle_HappyPath(t *testing.T) {
	src := makeSource()
	cat := makeCatalog()
	manifests := []*catalogmodel.ComponentManifest{
		makeSeamManifest("bbb", "sourceplane/orun/bbb"),
		makeSeamManifest("aaa", "sourceplane/orun/aaa"),
	}
	g := &catalogmodel.CatalogGraph{Kind: catalogmodel.KindCatalogGraph}
	graphs := []*catalogmodel.CatalogGraph{g, g, g, g, g}

	b, err := catalogstore.AssembleBundle(catalogstore.BundleInputs{
		Source:    src,
		Snapshot:  &cat,
		Manifests: manifests,
		Graphs:    graphs,
		UpdatedAt: "2026-05-31T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("AssembleBundle: %v", err)
	}

	// Graphs mapped positionally into all five named slots.
	if b.Graphs.Dependencies == nil || b.Graphs.Systems == nil ||
		b.Graphs.APIs == nil || b.Graphs.Resources == nil || b.Graphs.Owners == nil {
		t.Errorf("expected all five graph slots populated, got %+v", b.Graphs)
	}

	// Manifests deref'd preserving input order.
	if len(b.Manifests) != 2 {
		t.Fatalf("expected 2 manifests, got %d", len(b.Manifests))
	}

	// Local indexes: one component-axis entry per manifest, empty executions.
	if len(b.LocalIndexes.Components) != 2 {
		t.Fatalf("expected 2 component local indexes, got %d", len(b.LocalIndexes.Components))
	}
	cei, ok := b.LocalIndexes.Components["aaa"].(catalogmodel.ComponentExecutionIndex)
	if !ok {
		t.Fatalf("component local index for aaa is %T, want ComponentExecutionIndex", b.LocalIndexes.Components["aaa"])
	}
	if cei.Executions == nil || len(cei.Executions) != 0 {
		t.Errorf("expected empty (non-nil) executions, got %v", cei.Executions)
	}
	if cei.ComponentKey != "sourceplane/orun/aaa" {
		t.Errorf("componentKey = %q, want sourceplane/orun/aaa", cei.ComponentKey)
	}

	// Global indexes: source + catalog pointers, component shards sorted by key.
	if b.GlobalIndexes.Source == nil || b.GlobalIndexes.Catalog == nil {
		t.Fatal("expected non-nil source + catalog global index pointers")
	}
	if len(b.GlobalIndexes.Components) != 2 {
		t.Fatalf("expected 2 component global shards, got %d", len(b.GlobalIndexes.Components))
	}
	if b.GlobalIndexes.Components[0].ComponentKey != "sourceplane/orun/aaa" ||
		b.GlobalIndexes.Components[1].ComponentKey != "sourceplane/orun/bbb" {
		t.Errorf("component shards not sorted by key: %q, %q",
			b.GlobalIndexes.Components[0].ComponentKey, b.GlobalIndexes.Components[1].ComponentKey)
	}
	// branch-main source scope routes to Main pointer.
	if b.GlobalIndexes.Components[0].Main.SourceSnapshotKey != testSrcKey {
		t.Errorf("expected Main pointer set for branch-main scope, got %+v", b.GlobalIndexes.Components[0].Main)
	}

	// Refs: source + catalog ref pair with current name and carried authoritative.
	if b.Refs.Source == nil || b.Refs.Catalog == nil {
		t.Fatal("expected non-nil source + catalog refs")
	}
	if b.Refs.Source.Name != catalogmodel.RefNameCurrent ||
		b.Refs.Catalog.Name != catalogmodel.RefNameCurrent {
		t.Errorf("expected current ref name on both refs")
	}
	if !b.Refs.Source.Authoritative || !b.Refs.Catalog.Authoritative {
		t.Errorf("expected authoritative carried from snapshot")
	}
	if b.Refs.Catalog.CatalogSnapshotKey != testCatKey {
		t.Errorf("catalog ref key = %q, want %q", b.Refs.Catalog.CatalogSnapshotKey, testCatKey)
	}
	if b.Refs.Source.UpdatedAt != "2026-05-31T00:00:00Z" {
		t.Errorf("updatedAt not stamped on source ref: %q", b.Refs.Source.UpdatedAt)
	}
}

func TestAssembleBundle_Deterministic(t *testing.T) {
	src := makeSource()
	cat := makeCatalog()
	manifests := []*catalogmodel.ComponentManifest{
		makeSeamManifest("bbb", "sourceplane/orun/bbb"),
		makeSeamManifest("aaa", "sourceplane/orun/aaa"),
	}
	in := catalogstore.BundleInputs{
		Source:    src,
		Snapshot:  &cat,
		Manifests: manifests,
		UpdatedAt: "2026-05-31T00:00:00Z",
	}
	b1, err := catalogstore.AssembleBundle(in)
	if err != nil {
		t.Fatalf("AssembleBundle 1: %v", err)
	}
	b2, err := catalogstore.AssembleBundle(in)
	if err != nil {
		t.Fatalf("AssembleBundle 2: %v", err)
	}
	for i := range b1.GlobalIndexes.Components {
		if b1.GlobalIndexes.Components[i].ComponentKey != b2.GlobalIndexes.Components[i].ComponentKey {
			t.Errorf("non-deterministic component order at %d", i)
		}
	}
}

func TestAssembleBundle_BranchAndPRScopeCarried(t *testing.T) {
	src := makeSource()
	cat := makeCatalog()
	b, err := catalogstore.AssembleBundle(catalogstore.BundleInputs{
		Source:    src,
		Snapshot:  &cat,
		Branch:    "feature-x",
		PR:        "139",
		UpdatedAt: "2026-05-31T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("AssembleBundle: %v", err)
	}
	if b.Refs.Branch != "feature-x" {
		t.Errorf("branch scope = %q, want feature-x", b.Refs.Branch)
	}
	if b.Refs.PR != "139" {
		t.Errorf("pr scope = %q, want 139", b.Refs.PR)
	}
}

// ----- ListRefs ------------------------------------------------------

func TestListRefs_Empty(t *testing.T) {
	spy := newSpyStore()
	got, err := catalogstore.ListRefs(context.Background(), spy)
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty listing, got %d entries", len(got))
	}
}

func TestListRefs_JoinsSourceAndCatalog(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	src := makeSourceRef(catalogmodel.RefNameCurrent)
	cat := makeCatalogRef(catalogmodel.RefNameCurrent)
	if err := st.WriteRefs(ctx, catalogstore.RefUpdate{
		Source: &src, Catalog: &cat, Branch: "feature-x", PR: "139",
	}); err != nil {
		t.Fatalf("WriteRefs: %v", err)
	}

	got, err := catalogstore.ListRefs(ctx, spy)
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}

	byName := map[string]catalogstore.RefListing{}
	for _, r := range got {
		byName[r.Name] = r
	}

	// current / main / latest plus branches/feature-x and prs/139, both sides.
	for _, want := range []string{"current", "main", "latest", "branches/feature-x", "prs/139"} {
		r, ok := byName[want]
		if !ok {
			t.Errorf("missing ref %q in listing", want)
			continue
		}
		if r.SourceSnapshotKey != testSrcKey {
			t.Errorf("ref %q source key = %q, want %q", want, r.SourceSnapshotKey, testSrcKey)
		}
		if r.CatalogSnapshotKey != testCatKey {
			t.Errorf("ref %q catalog key = %q, want %q", want, r.CatalogSnapshotKey, testCatKey)
		}
		if !r.Authoritative {
			t.Errorf("ref %q expected authoritative", want)
		}
	}
}

func TestListRefs_SortedByName(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()
	src := makeSourceRef(catalogmodel.RefNameCurrent)
	cat := makeCatalogRef(catalogmodel.RefNameCurrent)
	if err := st.WriteRefs(ctx, catalogstore.RefUpdate{Source: &src, Catalog: &cat}); err != nil {
		t.Fatalf("WriteRefs: %v", err)
	}
	got, err := catalogstore.ListRefs(ctx, spy)
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Name > got[i].Name {
			t.Errorf("listing not sorted: %q before %q", got[i-1].Name, got[i].Name)
		}
	}
}
