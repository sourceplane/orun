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
	if got := SourceRefs(main); len(got) != 2 || got[1] != "sources/main" {
		t.Fatalf("main source refs = %v", got)
	}
	if got := CatalogRefs(main); got[1] != "catalogs/main" {
		t.Fatalf("main catalog refs = %v", got)
	}
	branch := nodes.SourceSnapshot{Scope: nodes.ScopeBranch, Branch: "feat/x"}
	if got := SourceRefs(branch); got[1] != "sources/branches/feat-x" {
		t.Fatalf("branch source refs = %v", got)
	}
	pr := nodes.SourceSnapshot{Scope: nodes.ScopePR, PR: "139"}
	if got := CatalogRefs(pr); got[1] != "catalogs/prs/139" {
		t.Fatalf("pr catalog refs = %v", got)
	}
	// nogit / branch-without-name => only the current pointer.
	if got := SourceRefs(nodes.SourceSnapshot{Scope: nodes.ScopeLocalNoGit}); len(got) != 1 {
		t.Fatalf("nogit refs = %v", got)
	}
	if got := SourceRefs(nodes.SourceSnapshot{Scope: nodes.ScopeBranch}); len(got) != 1 {
		t.Fatalf("branch-no-name refs = %v", got)
	}
	if got := TriggerRefs("system.manual"); got[0] != "triggers/system.manual/latest" {
		t.Fatalf("trigger refs = %v", got)
	}
	if RevisionRefs("")[0] != "revisions/latest" {
		t.Fatalf("revision refs")
	}
	byHash := RevisionRefs("sha256-abc")
	if len(byHash) != 2 || byHash[1] != "revisions/by-hash/sha256-abc" {
		t.Fatalf("revision by-hash refs = %v", byHash)
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
	cat, manifests, graphs, ownership, _ := BuildCatalogNodes(sampleView(), 2)
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
	// Ownership is derived: the fixed rule lists are populated even when no
	// manifest carries a path (so Components is empty here).
	if ownership.Kind != nodes.KindImpactOwnership || ownership.SchemaVersion != 1 {
		t.Fatalf("ownership = %+v", ownership)
	}
	if len(ownership.GlobalPaths) == 0 || len(ownership.StructuralFilenames) == 0 || len(ownership.IgnoreDirs) == 0 {
		t.Fatalf("ownership rule lists not populated: %+v", ownership)
	}
	if err := ownership.Validate(); err != nil {
		t.Fatalf("derived ownership invalid: %v", err)
	}
	// nil view is tolerated.
	c2, m2, g2, o2, _ := BuildCatalogNodes(nil, 1)
	if c2.Kind != nodes.KindCatalogSnapshot || m2 != nil || g2 != nil {
		t.Fatalf("nil view = %+v %v %v", c2, m2, g2)
	}
	if o2.Kind != nodes.KindImpactOwnership || len(o2.Components) != 0 {
		t.Fatalf("nil view ownership = %+v", o2)
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
	_, _, graphs, _, _ := BuildCatalogNodes(view, 1)
	want := []string{"dependencies", "systems", "apis", "resources", "owners", "graph5"}
	for i, g := range graphs {
		if g.EdgeKind != want[i] {
			t.Fatalf("graph[%d] edgeKind = %q, want %q", i, g.EdgeKind, want[i])
		}
	}
}

func TestBuildOwnershipMapsComponentDirs(t *testing.T) {
	t.Parallel()
	view := &catalogresolve.CatalogView{
		ResolvedCatalog: &catalogresolve.ResolvedCatalog{
			Manifests: []*catalogmodel.ComponentManifest{
				{Identity: catalogmodel.ComponentIdentity{ComponentKey: "ns/repo/api", Name: "api", Path: "apps/api/component.yaml"}},
				{Identity: catalogmodel.ComponentIdentity{ComponentKey: "ns/repo/root", Name: "root", Path: "component.yaml"}},
				{Identity: catalogmodel.ComponentIdentity{ComponentKey: "ns/repo/nopath", Name: "nopath"}}, // empty path: skipped
				nil,
			},
			IntentPath: "intent.yaml",
			Excludes:   []string{"vendor", ".git"}, // unsorted on input
		},
		Snapshot: &catalogmodel.CatalogSnapshot{},
	}
	o := buildOwnership(view)
	if o.Components["apps/api"] != "ns/repo/api" {
		t.Errorf("apps/api → %q", o.Components["apps/api"])
	}
	if o.Components["."] != "ns/repo/root" { // root component (path.Dir("component.yaml") == ".")
		t.Errorf("root component dir = %q", o.Components["."])
	}
	if _, ok := o.Components["nopath"]; ok || len(o.Components) != 2 {
		t.Errorf("pathless manifest must be skipped: %v", o.Components)
	}
	// ignoreDirs mirrors the resolve excludes, sorted.
	if len(o.IgnoreDirs) != 2 || o.IgnoreDirs[0] != ".git" || o.IgnoreDirs[1] != "vendor" {
		t.Errorf("ignoreDirs = %v, want sorted [.git vendor]", o.IgnoreDirs)
	}
	if len(o.GlobalPaths) != 1 || o.GlobalPaths[0] != "intent.yaml" {
		t.Errorf("globalPaths = %v", o.GlobalPaths)
	}
	if err := o.Validate(); err != nil {
		t.Errorf("derived ownership invalid: %v", err)
	}
}

func TestBuildFingerprintsMapsNodeType(t *testing.T) {
	t.Parallel()
	view := &catalogresolve.CatalogView{
		ResolvedCatalog: &catalogresolve.ResolvedCatalog{
			Fingerprints: []catalogresolve.ComponentFingerprint{
				{ComponentKey: "ns/repo/api", Dir: "apps/api", Subtree: "sha256:s",
					Files: map[string]string{"apps/api/component.yaml": "sha256:f"}, GlobalDigest: "sha256:g"},
			},
		},
		Snapshot: &catalogmodel.CatalogSnapshot{},
	}
	fps := buildFingerprints(view)
	if len(fps) != 1 {
		t.Fatalf("got %d fingerprints", len(fps))
	}
	fp := fps[0]
	if fp.Kind != nodes.KindComponentFingerprint || fp.SchemaVersion != 1 {
		t.Errorf("fingerprint defaults = %+v", fp)
	}
	if fp.ComponentKey != "ns/repo/api" || fp.Dir != "apps/api" || fp.Subtree != "sha256:s" {
		t.Errorf("fingerprint fields = %+v", fp)
	}
	if fp.Files["apps/api/component.yaml"] != "sha256:f" || fp.GlobalDigest != "sha256:g" {
		t.Errorf("fingerprint files/global = %+v", fp)
	}
	if err := fp.Validate(); err != nil {
		t.Errorf("mapped fingerprint invalid: %v", err)
	}
	// nil view / nil ResolvedCatalog → no fingerprints, no panic.
	if buildFingerprints(nil) != nil || buildFingerprints(&catalogresolve.CatalogView{}) != nil {
		t.Errorf("nil view should yield nil fingerprints")
	}
}

func TestBuildOwnershipDefaultsExcludesWhenNoIntent(t *testing.T) {
	t.Parallel()
	// A view with no excludes (e.g. nil ResolvedCatalog) falls back to the
	// discovery default exclude set.
	o := buildOwnership(&catalogresolve.CatalogView{})
	if len(o.IgnoreDirs) == 0 {
		t.Fatalf("ignoreDirs should fall back to defaults")
	}
	if o.GlobalPaths[0] != "intent.yaml" {
		t.Errorf("globalPaths default = %v", o.GlobalPaths)
	}
}
