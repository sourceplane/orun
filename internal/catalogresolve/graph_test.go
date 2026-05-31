package catalogresolve

import (
	"sort"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

func TestBuildGraphs_FiveSiblingsInFixedOrder(t *testing.T) {
	manifests := makeFixtureManifests(t)
	graphs := buildGraphs(manifests, "src-1", "cat-2")

	if got, want := len(graphs), 5; got != want {
		t.Fatalf("graphs = %d, want %d", got, want)
	}
	// All should carry the source/snapshot keys we passed.
	for i, g := range graphs {
		if g.SourceSnapshotKey != "src-1" {
			t.Errorf("graph[%d].SourceSnapshotKey = %q", i, g.SourceSnapshotKey)
		}
		if g.CatalogSnapshotKey != "cat-2" {
			t.Errorf("graph[%d].CatalogSnapshotKey = %q", i, g.CatalogSnapshotKey)
		}
		if g.APIVersion != "orun.io/v1alpha1" || g.Kind != "CatalogGraph" {
			t.Errorf("graph[%d] header = %+v", i, g)
		}
	}
}

func TestBuildGraphs_NodesSortedByKey(t *testing.T) {
	manifests := makeFixtureManifests(t)
	graphs := buildGraphs(manifests, "", "")
	for i, g := range graphs {
		keys := make([]string, len(g.Nodes))
		for j, n := range g.Nodes {
			keys[j] = n.Key
		}
		if !sort.StringsAreSorted(keys) {
			t.Errorf("graph[%d] nodes not sorted by key: %v", i, keys)
		}
	}
}

func TestBuildGraphs_EdgesSortedByFromToTypeOptional(t *testing.T) {
	manifests := makeFixtureManifests(t)
	graphs := buildGraphs(manifests, "", "")
	for i, g := range graphs {
		for k := 1; k < len(g.Edges); k++ {
			a, b := g.Edges[k-1], g.Edges[k]
			less := a.From < b.From ||
				(a.From == b.From && a.To < b.To) ||
				(a.From == b.From && a.To == b.To && a.Type < b.Type) ||
				(a.From == b.From && a.To == b.To && a.Type == b.Type && !a.Optional && b.Optional) ||
				(a.From == b.From && a.To == b.To && a.Type == b.Type && a.Optional == b.Optional)
			if !less {
				t.Errorf("graph[%d] edges out of order at %d: %+v >= %+v", i, k, a, b)
			}
		}
	}
}

func TestBuildGraphs_DependencyEdgeFromManifest(t *testing.T) {
	manifests := makeFixtureManifests(t)
	graphs := buildGraphs(manifests, "", "")
	deps := graphs[0]
	// The fixture has api-edge → identity-worker via Rel="calls".
	found := false
	for _, e := range deps.Edges {
		if e.From == "sourceplane/orun/api-edge" &&
			e.To == "sourceplane/orun/identity-worker" &&
			e.Type == catalogmodel.RelCalls {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("dep graph missing api-edge→identity-worker calls edge: %+v", deps.Edges)
	}
}

func TestBuildGraphs_SystemPartOfEdge(t *testing.T) {
	manifests := makeFixtureManifests(t)
	graphs := buildGraphs(manifests, "", "")
	sys := graphs[1]
	// Both fixture manifests are part-of system "edge".
	count := 0
	for _, e := range sys.Edges {
		if e.To == "edge" && e.Type == "part-of" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("system graph: expected 2 part-of edges to 'edge', got %d", count)
	}
}

func TestBuildGraphs_OwnerOwnsEdge(t *testing.T) {
	manifests := makeFixtureManifests(t)
	graphs := buildGraphs(manifests, "", "")
	own := graphs[4]
	// Owner edge direction: owner → component (type=owns).
	for _, e := range own.Edges {
		if e.Type != "owns" {
			t.Errorf("owner edge not type=owns: %+v", e)
		}
	}
	if len(own.Edges) != 2 {
		t.Errorf("owner graph: want 2 edges, got %d", len(own.Edges))
	}
}

func TestBuildGraphs_APIProvidesAndConsumes(t *testing.T) {
	manifests := makeFixtureManifests(t)
	graphs := buildGraphs(manifests, "", "")
	apis := graphs[2]
	var provides, consumes int
	for _, e := range apis.Edges {
		switch e.Type {
		case "provides":
			provides++
		case "consumes":
			consumes++
		default:
			t.Errorf("unexpected api edge type: %+v", e)
		}
	}
	// Fixture: 2 provides (public-api, identity-api) + 1 consumes (identity-api).
	if provides != 2 || consumes != 1 {
		t.Errorf("apis graph: provides=%d consumes=%d (want 2/1)", provides, consumes)
	}
}

func TestBuildGraphs_ResourceUses(t *testing.T) {
	manifests := makeFixtureManifests(t)
	graphs := buildGraphs(manifests, "", "")
	res := graphs[3]
	for _, e := range res.Edges {
		if e.Type != "uses" {
			t.Errorf("resource edge not type=uses: %+v", e)
		}
	}
	if len(res.Edges) != 2 {
		t.Errorf("resource graph: want 2 edges, got %d", len(res.Edges))
	}
}

func TestBuildGraphs_EmptyManifestsProducesEmptyGraphs(t *testing.T) {
	graphs := buildGraphs(nil, "src", "cat")
	if len(graphs) != 5 {
		t.Fatalf("graphs = %d, want 5", len(graphs))
	}
	for i, g := range graphs {
		if len(g.Nodes) != 0 {
			t.Errorf("graph[%d] expected 0 nodes, got %d", i, len(g.Nodes))
		}
		if len(g.Edges) != 0 {
			t.Errorf("graph[%d] expected 0 edges, got %d", i, len(g.Edges))
		}
	}
}

func TestStampCatalogSnapshotKey_Idempotent(t *testing.T) {
	graphs := buildGraphs(nil, "src", "")
	stampCatalogSnapshotKey(graphs, "src", "cat-deadbeef")
	for _, g := range graphs {
		if g.CatalogSnapshotKey != "cat-deadbeef" {
			t.Errorf("stamp failed: %q", g.CatalogSnapshotKey)
		}
	}
	stampCatalogSnapshotKey(graphs, "src", "cat-deadbeef")
	for _, g := range graphs {
		if g.CatalogSnapshotKey != "cat-deadbeef" {
			t.Errorf("re-stamp not idempotent: %q", g.CatalogSnapshotKey)
		}
	}
}
