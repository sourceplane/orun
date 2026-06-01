package catalogdiff_test

import (
	"encoding/json"
	"testing"

	"github.com/sourceplane/orun/internal/catalogdiff"
	"github.com/sourceplane/orun/internal/catalogmodel"
)

// diff_test.go covers the catalogdiff engine: added/removed/changed component
// detection, the set-vs-list field comparison rules (cli-surface.md §6),
// graph node/edge add/remove, single-component filtering, and the
// determinism contract (repeated runs are byte-identical).

func manifest(key, name string) catalogmodel.ComponentManifest {
	return catalogmodel.ComponentManifest{
		Identity: catalogmodel.ComponentIdentity{ComponentKey: key, Name: name},
	}
}

func TestDiff_AddedRemoved(t *testing.T) {
	base := catalogdiff.Snapshot{Components: []catalogmodel.ComponentManifest{
		manifest("ns/repo/a", "a"),
		manifest("ns/repo/b", "b"),
	}}
	head := catalogdiff.Snapshot{Components: []catalogmodel.ComponentManifest{
		manifest("ns/repo/a", "a"),
		manifest("ns/repo/c", "c"),
	}}

	res := catalogdiff.Diff(base, head)
	if len(res.Added) != 1 || res.Added[0].ComponentKey != "ns/repo/c" {
		t.Errorf("Added = %+v, want [ns/repo/c]", res.Added)
	}
	if len(res.Removed) != 1 || res.Removed[0].ComponentKey != "ns/repo/b" {
		t.Errorf("Removed = %+v, want [ns/repo/b]", res.Removed)
	}
	if len(res.Changed) != 0 {
		t.Errorf("Changed = %+v, want none", res.Changed)
	}
}

func TestDiff_ScalarFieldChange(t *testing.T) {
	bm := manifest("ns/repo/a", "a")
	bm.Spec.Type = "service"
	bm.Metadata.Owner = "team/x"
	hm := manifest("ns/repo/a", "a")
	hm.Spec.Type = "worker"
	hm.Metadata.Owner = "team/x"

	res := catalogdiff.Diff(
		catalogdiff.Snapshot{Components: []catalogmodel.ComponentManifest{bm}},
		catalogdiff.Snapshot{Components: []catalogmodel.ComponentManifest{hm}},
	)
	if len(res.Changed) != 1 {
		t.Fatalf("Changed = %+v, want 1", res.Changed)
	}
	fields := res.Changed[0].Fields
	if len(fields) != 1 {
		t.Fatalf("fields = %+v, want 1 (spec.type only)", fields)
	}
	if fields[0].Path != "spec.type" || fields[0].Base != "service" || fields[0].Head != "worker" {
		t.Errorf("field = %+v, want spec.type service→worker", fields[0])
	}
}

// TestDiff_TagsOrderInsensitive proves the set-shaped `tags` field does NOT
// register a change on reorder (§6).
func TestDiff_TagsOrderInsensitive(t *testing.T) {
	bm := manifest("ns/repo/a", "a")
	bm.Metadata.Tags = []string{"x", "y", "z"}
	hm := manifest("ns/repo/a", "a")
	hm.Metadata.Tags = []string{"z", "y", "x"}

	res := catalogdiff.Diff(
		catalogdiff.Snapshot{Components: []catalogmodel.ComponentManifest{bm}},
		catalogdiff.Snapshot{Components: []catalogmodel.ComponentManifest{hm}},
	)
	if len(res.Changed) != 0 {
		t.Errorf("tag reorder must not be a change, got %+v", res.Changed)
	}

	// A genuine membership change IS a diff.
	hm.Metadata.Tags = []string{"x", "y", "w"}
	res = catalogdiff.Diff(
		catalogdiff.Snapshot{Components: []catalogmodel.ComponentManifest{bm}},
		catalogdiff.Snapshot{Components: []catalogmodel.ComponentManifest{hm}},
	)
	if len(res.Changed) != 1 || res.Changed[0].Fields[0].Path != "metadata.tags" {
		t.Errorf("tag membership change expected, got %+v", res.Changed)
	}
}

// TestDiff_ApiSetsOrderInsensitive covers providesApis / consumesApis.
func TestDiff_ApiSetsOrderInsensitive(t *testing.T) {
	bm := manifest("ns/repo/a", "a")
	bm.Spec.Dependencies.APIs.Provides = []string{"p1", "p2"}
	bm.Spec.Dependencies.APIs.Consumes = []string{"c1", "c2"}
	hm := manifest("ns/repo/a", "a")
	hm.Spec.Dependencies.APIs.Provides = []string{"p2", "p1"} // reorder only
	hm.Spec.Dependencies.APIs.Consumes = []string{"c2", "c1"} // reorder only

	res := catalogdiff.Diff(
		catalogdiff.Snapshot{Components: []catalogmodel.ComponentManifest{bm}},
		catalogdiff.Snapshot{Components: []catalogmodel.ComponentManifest{hm}},
	)
	if len(res.Changed) != 0 {
		t.Errorf("api set reorder must not be a change, got %+v", res.Changed)
	}
}

// TestDiff_DependsOnOrderSensitive proves `dependsOn` IS order-sensitive (§6):
// a reorder of the same dependency set registers a change.
func TestDiff_DependsOnOrderSensitive(t *testing.T) {
	bm := manifest("ns/repo/a", "a")
	bm.Spec.Dependencies.Components = []catalogmodel.ComponentDependency{
		{Name: "b", Relationship: "calls"},
		{Name: "c", Relationship: "calls"},
	}
	hm := manifest("ns/repo/a", "a")
	hm.Spec.Dependencies.Components = []catalogmodel.ComponentDependency{
		{Name: "c", Relationship: "calls"},
		{Name: "b", Relationship: "calls"},
	}

	res := catalogdiff.Diff(
		catalogdiff.Snapshot{Components: []catalogmodel.ComponentManifest{bm}},
		catalogdiff.Snapshot{Components: []catalogmodel.ComponentManifest{hm}},
	)
	if len(res.Changed) != 1 || res.Changed[0].Fields[0].Path != "spec.dependsOn" {
		t.Errorf("dependsOn reorder must be a change, got %+v", res.Changed)
	}
}

func TestDiff_GraphNodesAndEdges(t *testing.T) {
	base := catalogdiff.Snapshot{
		Components: []catalogmodel.ComponentManifest{manifest("ns/repo/a", "a")},
		Graphs: map[string]catalogmodel.CatalogGraph{
			"dependencies": {
				Nodes: []catalogmodel.GraphNode{{Key: "ns/repo/a", Name: "a"}, {Key: "ns/repo/b", Name: "b"}},
				Edges: []catalogmodel.GraphEdge{{From: "ns/repo/a", To: "ns/repo/b", Type: "calls"}},
			},
		},
	}
	head := catalogdiff.Snapshot{
		Components: []catalogmodel.ComponentManifest{manifest("ns/repo/a", "a")},
		Graphs: map[string]catalogmodel.CatalogGraph{
			"dependencies": {
				Nodes: []catalogmodel.GraphNode{{Key: "ns/repo/a", Name: "a"}, {Key: "ns/repo/c", Name: "c"}},
				Edges: []catalogmodel.GraphEdge{{From: "ns/repo/a", To: "ns/repo/c", Type: "calls"}},
			},
		},
	}

	res := catalogdiff.Diff(base, head)
	var nodeAdded, nodeRemoved, edgeAdded, edgeRemoved int
	for _, g := range res.GraphChanges {
		if g.Graph != "dependencies" {
			t.Errorf("unexpected graph kind %q", g.Graph)
		}
		switch g.Change {
		case "node-added":
			nodeAdded++
		case "node-removed":
			nodeRemoved++
		case "edge-added":
			edgeAdded++
		case "edge-removed":
			edgeRemoved++
		}
	}
	if nodeAdded != 1 || nodeRemoved != 1 || edgeAdded != 1 || edgeRemoved != 1 {
		t.Errorf("graph changes = %+v (want 1 of each)", res.GraphChanges)
	}
}

func TestDiff_FilterComponent(t *testing.T) {
	bm := manifest("ns/repo/a", "a")
	bm.Spec.Type = "service"
	hm := manifest("ns/repo/a", "a")
	hm.Spec.Type = "worker"
	base := catalogdiff.Snapshot{Components: []catalogmodel.ComponentManifest{bm, manifest("ns/repo/b", "b")}}
	head := catalogdiff.Snapshot{Components: []catalogmodel.ComponentManifest{hm}}

	res := catalogdiff.Diff(base, head)
	// Full diff: a changed, b removed.
	if len(res.Changed) != 1 || len(res.Removed) != 1 {
		t.Fatalf("unexpected full diff: %+v", res)
	}

	// Filter to "a" (bare name): only the change remains.
	fa := res.FilterComponent("a")
	if len(fa.Changed) != 1 || len(fa.Removed) != 0 {
		t.Errorf("filter a = %+v, want only changed", fa)
	}
	// Filter to full key works too.
	fb := res.FilterComponent("ns/repo/b")
	if len(fb.Removed) != 1 || len(fb.Changed) != 0 {
		t.Errorf("filter ns/repo/b = %+v, want only removed", fb)
	}
}

func TestDiff_Empty(t *testing.T) {
	m := manifest("ns/repo/a", "a")
	snap := catalogdiff.Snapshot{Components: []catalogmodel.ComponentManifest{m}}
	res := catalogdiff.Diff(snap, snap)
	if !res.IsEmpty() {
		t.Errorf("identical snapshots must diff empty, got %+v", res)
	}
	// Slices are non-nil so JSON marshals as [] not null.
	if res.Changed == nil || res.Added == nil || res.Removed == nil || res.GraphChanges == nil {
		t.Error("empty result slices must be non-nil")
	}
}

// TestDiff_Deterministic proves repeated diffs of the same inputs are
// byte-identical when marshaled — the determinism contract.
func TestDiff_Deterministic(t *testing.T) {
	base := catalogdiff.Snapshot{
		Components: []catalogmodel.ComponentManifest{
			manifest("ns/repo/z", "z"), manifest("ns/repo/a", "a"), manifest("ns/repo/m", "m"),
		},
		Graphs: map[string]catalogmodel.CatalogGraph{
			"dependencies": {Edges: []catalogmodel.GraphEdge{
				{From: "ns/repo/z", To: "ns/repo/a", Type: "calls"},
				{From: "ns/repo/a", To: "ns/repo/m", Type: "depends-on"},
			}},
		},
	}
	head := catalogdiff.Snapshot{
		Components: []catalogmodel.ComponentManifest{
			manifest("ns/repo/a", "a"), manifest("ns/repo/n", "n"),
		},
		Graphs: map[string]catalogmodel.CatalogGraph{
			"dependencies": {Edges: []catalogmodel.GraphEdge{
				{From: "ns/repo/a", To: "ns/repo/n", Type: "calls"},
			}},
		},
	}

	var first string
	for i := 0; i < 50; i++ {
		res := catalogdiff.Diff(base, head)
		b, err := json.Marshal(res)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if i == 0 {
			first = string(b)
			continue
		}
		if string(b) != first {
			t.Fatalf("diff non-deterministic at run %d:\n got %s\nwant %s", i, b, first)
		}
	}
}

// TestDiff_AbsentGraphEqualsEmpty proves an absent graph kind on one side and
// an empty graph on the other produce no spurious changes.
func TestDiff_AbsentGraphEqualsEmpty(t *testing.T) {
	base := catalogdiff.Snapshot{Graphs: map[string]catalogmodel.CatalogGraph{}}
	head := catalogdiff.Snapshot{Graphs: map[string]catalogmodel.CatalogGraph{
		"dependencies": {},
	}}
	if res := catalogdiff.Diff(base, head); len(res.GraphChanges) != 0 {
		t.Errorf("absent vs empty graph must not diff, got %+v", res.GraphChanges)
	}
}
