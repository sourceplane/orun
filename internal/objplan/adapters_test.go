package objplan

import (
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/sourcectx"
)

func TestBuildSourceNodeScopes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		ws        sourcectx.WorkspaceState
		wantScope string
		wantWT    string
	}{
		{"main-clean", sourcectx.WorkspaceState{Repo: "ns/r", HeadRevision: "abc", TreeHash: "t", Branch: "main"}, nodes.ScopeMain, "clean"},
		{"feature", sourcectx.WorkspaceState{HeadRevision: "abc", Branch: "feat"}, nodes.ScopeBranch, "clean"},
		{"dirty", sourcectx.WorkspaceState{HeadRevision: "abc", Branch: "feat", Dirty: true, DirtyHash: "sha256:d"}, nodes.ScopeBranch, "dirty"},
		{"pr", sourcectx.WorkspaceState{HeadRevision: "abc", Branch: "feat", PRNumber: 139}, nodes.ScopePR, "clean"},
		{"nogit", sourcectx.WorkspaceState{}, nodes.ScopeLocalNoGit, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := BuildSourceNode(c.ws, "hk")
			if src.Scope != c.wantScope {
				t.Fatalf("scope = %q, want %q", src.Scope, c.wantScope)
			}
			if src.WorkingTree != c.wantWT {
				t.Fatalf("workingTree = %q, want %q", src.WorkingTree, c.wantWT)
			}
			if src.Kind != nodes.KindSourceSnapshot || src.HumanKey != "hk" {
				t.Fatalf("bad source node: %+v", src)
			}
			if err := src.Validate(); err != nil {
				t.Fatalf("source node invalid: %v", err)
			}
		})
	}
	if BuildSourceNode(sourcectx.WorkspaceState{PRNumber: 139, HeadRevision: "x"}, "").PR != "139" {
		t.Fatalf("PR not stringified")
	}
}

func TestItoa(t *testing.T) {
	t.Parallel()
	for in, want := range map[int]string{0: "0", 7: "7", 139: "139", -5: "-5"} {
		if got := itoa(in); got != want {
			t.Fatalf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestSourceAndCatalogRefs(t *testing.T) {
	t.Parallel()
	main := nodes.SourceSnapshot{Scope: nodes.ScopeMain}
	if got := SourceRefs(main); len(got) != 2 || got[1] != "refs/sources/main" {
		t.Fatalf("main source refs = %v", got)
	}
	if got := CatalogRefs(main); got[1] != "refs/catalogs/main" {
		t.Fatalf("main catalog refs = %v", got)
	}
	branch := nodes.SourceSnapshot{Scope: nodes.ScopeBranch, Branch: "feat/x"}
	if got := SourceRefs(branch); got[1] != "refs/sources/branches/feat-x" {
		t.Fatalf("branch source refs = %v", got)
	}
	pr := nodes.SourceSnapshot{Scope: nodes.ScopePR, PR: "139"}
	if got := CatalogRefs(pr); got[1] != "refs/catalogs/prs/139" {
		t.Fatalf("pr catalog refs = %v", got)
	}
	// nogit / branch-without-name => only the current pointer.
	if got := SourceRefs(nodes.SourceSnapshot{Scope: nodes.ScopeLocalNoGit}); len(got) != 1 {
		t.Fatalf("nogit refs = %v", got)
	}
	if got := SourceRefs(nodes.SourceSnapshot{Scope: nodes.ScopeBranch}); len(got) != 1 {
		t.Fatalf("branch-no-name refs = %v", got)
	}
	if got := TriggerRefs("system.manual"); got[0] != "refs/triggers/system.manual/latest" {
		t.Fatalf("trigger refs = %v", got)
	}
	if RevisionRefs()[0] != "refs/revisions/latest" {
		t.Fatalf("revision refs")
	}
}

func TestSanitizeRefSeg(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"main": "main", "feat/x": "feat-x", "***": "x", "": "x", "-a-": "a"} {
		if got := sanitizeRefSeg(in); got != want {
			t.Fatalf("sanitizeRefSeg(%q) = %q, want %q", in, got, want)
		}
	}
}

func sampleView() *catalogresolve.CatalogView {
	return &catalogresolve.CatalogView{
		ResolvedCatalog: &catalogresolve.ResolvedCatalog{
			Manifests: []*catalogmodel.ComponentManifest{
				{Identity: catalogmodel.ComponentIdentity{ComponentKey: "ns/repo/api", Name: "api", Namespace: "ns", Repo: "repo"}},
				nil, // adapter must skip nils
			},
		},
		Snapshot: &catalogmodel.CatalogSnapshot{CatalogSnapshotKey: "cat-x"},
		Graphs: []*catalogmodel.CatalogGraph{
			{Nodes: []catalogmodel.GraphNode{{Key: "ns/repo/api", Kind: "Component", Name: "api"}},
				Edges: []catalogmodel.GraphEdge{{From: "ns/repo/api", To: "ns/repo/db", Type: "depends-on"}}},
			nil,
		},
	}
}

func TestBuildCatalogNodes(t *testing.T) {
	t.Parallel()
	cat, manifests, graphs := BuildCatalogNodes(sampleView(), 2)
	if cat.Kind != nodes.KindCatalogSnapshot || cat.ResolverVersion != 2 || cat.HumanKey != "cat-x" {
		t.Fatalf("catalog = %+v", cat)
	}
	if len(manifests) != 1 || manifests[0].Identity.ComponentKey != "ns/repo/api" {
		t.Fatalf("manifests = %+v", manifests)
	}
	if err := manifests[0].Validate(); err != nil {
		t.Fatalf("mapped manifest invalid: %v", err)
	}
	if len(graphs) != 1 || graphs[0].EdgeKind != "dependencies" {
		t.Fatalf("graphs = %+v", graphs)
	}
	if len(graphs[0].Edges) != 1 || graphs[0].Edges[0].Type != "depends-on" {
		t.Fatalf("graph edges = %+v", graphs[0].Edges)
	}
	// nil view is tolerated.
	c2, m2, g2 := BuildCatalogNodes(nil, 1)
	if c2.Kind != nodes.KindCatalogSnapshot || m2 != nil || g2 != nil {
		t.Fatalf("nil view = %+v %v %v", c2, m2, g2)
	}
}

func TestBuildCatalogNodesManyGraphsPositional(t *testing.T) {
	t.Parallel()
	view := &catalogresolve.CatalogView{
		ResolvedCatalog: &catalogresolve.ResolvedCatalog{},
		Snapshot:        &catalogmodel.CatalogSnapshot{},
		Graphs: []*catalogmodel.CatalogGraph{
			{}, {}, {}, {}, {}, {}, // 6 graphs: 5 named + 1 overflow
		},
	}
	_, _, graphs := BuildCatalogNodes(view, 1)
	want := []string{"dependencies", "systems", "apis", "resources", "owners", "graph5"}
	for i, g := range graphs {
		if g.EdgeKind != want[i] {
			t.Fatalf("graph[%d] edgeKind = %q, want %q", i, g.EdgeKind, want[i])
		}
	}
}
