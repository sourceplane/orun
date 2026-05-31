package catalogresolve

import (
	"context"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

func goldenInputs() ResolverInputs {
	return ResolverInputs{
		OrunVersion:       "0.18.0",
		SchemaVersion:     "orun.io/v1alpha1",
		ResolverVersion:   1,
		StackSources:      []string{"ghcr.io/sourceplane/stack-tectonic:0.12.0"},
		SourceSnapshotKey: "src-deadbeef01",
		CatalogInputHash:  "sha256:cafef00d",
		Repo:              "sourceplane/orun",
		SourceScope:       "branch-main",
		HeadRevision:      "abcdef012345",
		TreeHash:          "fedcba6",
		WorkingTree:       "clean",
		Authoritative:     true,
		Preview:           false,
		CreatedAt:         "2026-05-31T12:00:00Z",
	}
}

func TestBuildCatalog_E2E_HappyPath(t *testing.T) {
	root := fixturePath(t, "resolve_e2e")
	view, _, err := BuildCatalog(context.Background(), Options{WorkspaceRoot: root}, goldenInputs())
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	if view == nil || view.Snapshot == nil {
		t.Fatal("view or snapshot nil")
	}
	if view.Snapshot.CatalogHash == "" || !strings.HasPrefix(view.Snapshot.CatalogHash, "sha256:") {
		t.Errorf("CatalogHash = %q", view.Snapshot.CatalogHash)
	}
	if err := catalogmodel.ValidateCatalogSnapshotKey(view.Snapshot.CatalogSnapshotKey); err != nil {
		t.Errorf("ValidateCatalogSnapshotKey: %v", err)
	}
	if !regexp.MustCompile(`^cat-[a-f0-9]{6,16}$`).MatchString(view.Snapshot.CatalogSnapshotKey) {
		t.Errorf("CatalogSnapshotKey regex mismatch: %q", view.Snapshot.CatalogSnapshotKey)
	}
	// Snapshot id is a fresh ULID-like value.
	if !strings.HasPrefix(view.Snapshot.CatalogSnapshotID, "cat_") {
		t.Errorf("CatalogSnapshotID prefix: %q", view.Snapshot.CatalogSnapshotID)
	}
	if got := view.Snapshot.Summary.Components; got != 2 {
		t.Errorf("Summary.Components = %d, want 2", got)
	}
	if len(view.Graphs) != 5 {
		t.Errorf("Graphs = %d, want 5", len(view.Graphs))
	}
	// Graphs back-filled with the derived snapshot key.
	for i, g := range view.Graphs {
		if g.CatalogSnapshotKey != view.Snapshot.CatalogSnapshotKey {
			t.Errorf("graph[%d] CatalogSnapshotKey not back-filled: %q vs %q", i, g.CatalogSnapshotKey, view.Snapshot.CatalogSnapshotKey)
		}
	}
	// objects.components ordered by key.
	keys := make([]string, len(view.Snapshot.Objects.Components))
	for i, c := range view.Snapshot.Objects.Components {
		keys[i] = c.Key
	}
	want := []string{"sourceplane/resolve_e2e/api-edge", "sourceplane/resolve_e2e/identity-worker"}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("objects.components keys = %v, want %v", keys, want)
	}
	// resolver block populated from inputs.
	if view.Snapshot.Resolver.OrunVersion != "0.18.0" {
		t.Errorf("Resolver.OrunVersion = %q", view.Snapshot.Resolver.OrunVersion)
	}
	if view.Snapshot.Resolver.ResolverVersion != 1 {
		t.Errorf("Resolver.ResolverVersion = %d", view.Snapshot.Resolver.ResolverVersion)
	}
	if !view.Snapshot.Authoritative || view.Snapshot.Preview {
		t.Errorf("Authoritative/Preview = %v/%v", view.Snapshot.Authoritative, view.Snapshot.Preview)
	}
}

// TestBuildCatalog_Deterministic asserts two consecutive BuildCatalog
// calls on the same fixture produce byte-identical canonical-encoded
// (snapshot, graphs).
func TestBuildCatalog_Deterministic(t *testing.T) {
	root := fixturePath(t, "resolve_e2e")
	in := goldenInputs()

	v1, _, err := BuildCatalog(context.Background(), Options{WorkspaceRoot: root}, in)
	if err != nil {
		t.Fatal(err)
	}
	v2, _, err := BuildCatalog(context.Background(), Options{WorkspaceRoot: root}, in)
	if err != nil {
		t.Fatal(err)
	}
	if v1.Snapshot.CatalogHash != v2.Snapshot.CatalogHash {
		t.Errorf("CatalogHash differs: %s vs %s", v1.Snapshot.CatalogHash, v2.Snapshot.CatalogHash)
	}
	if v1.Snapshot.CatalogSnapshotKey != v2.Snapshot.CatalogSnapshotKey {
		t.Errorf("CatalogSnapshotKey differs")
	}
	// CatalogSnapshotID is fresh per-call (ULID) — clear it before encoding compare.
	v1.Snapshot.CatalogSnapshotID = ""
	v2.Snapshot.CatalogSnapshotID = ""
	a, err := catalogmodel.CanonicalEncode(v1.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	b, err := catalogmodel.CanonicalEncode(v2.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Errorf("snapshot canonical encoding differs:\n  a=%s\n  b=%s", a, b)
	}
	for i := range v1.Graphs {
		ga, _ := catalogmodel.CanonicalEncode(v1.Graphs[i])
		gb, _ := catalogmodel.CanonicalEncode(v2.Graphs[i])
		if string(ga) != string(gb) {
			t.Errorf("graph[%d] differs:\n a=%s\n b=%s", i, ga, gb)
		}
	}
}

func TestBuildCatalog_MissingInputs(t *testing.T) {
	root := fixturePath(t, "resolve_e2e")
	_, _, err := BuildCatalog(context.Background(), Options{WorkspaceRoot: root}, ResolverInputs{})
	if err == nil {
		t.Fatal("expected error for empty ResolverInputs")
	}
	if !IsResolverInputsIncomplete(err) {
		t.Errorf("expected ErrResolverInputsIncomplete, got %T: %v", err, err)
	}
}

// TestSummaryCounts_FromSortedDistinct asserts each summary.* counter
// equals sorted-distinct enumeration over the resolved manifest set.
func TestSummaryCounts_FromSortedDistinct(t *testing.T) {
	manifests := makeFixtureManifests(t)
	got := computeSummary(manifests)

	// Fixture: 2 components, 1 system (edge), 2 distinct apis (public-api,
	// identity-api), 2 distinct resources (redis, postgres), 2 owners
	// (team-a, team-b), 2 domains (platform, identity).
	want := catalogmodel.CatalogSummary{
		Components: 2, Systems: 1, APIs: 2, Resources: 2, Owners: 2, Domains: 2,
	}
	if got != want {
		t.Errorf("summary mismatch:\n got %+v\n want %+v", got, want)
	}
}
