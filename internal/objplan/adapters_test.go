package objplan

import (
	"os"
	"path/filepath"
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
	cat, manifests, graphs, ownership, _ := BuildCatalogNodes(sampleView(), 2, nil)
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
	c2, m2, g2, o2, _ := BuildCatalogNodes(nil, 1, nil)
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
	_, _, graphs, _, _ := BuildCatalogNodes(view, 1, nil)
	want := []string{"dependencies", "systems", "apis", "resources", "owners", "graph5"}
	for i, g := range graphs {
		if g.EdgeKind != want[i] {
			t.Fatalf("graph[%d] edgeKind = %q, want %q", i, g.EdgeKind, want[i])
		}
	}
}

func TestMapEntity_CarriesChangeWatches(t *testing.T) {
	t.Parallel()
	cm := &catalogmodel.ComponentManifest{
		Identity: catalogmodel.ComponentIdentity{ComponentKey: "ns/repo/api", Name: "api", Namespace: "ns", Repo: "repo"},
		Spec:     catalogmodel.ComponentSpec{Type: "worker", Change: &catalogmodel.ComponentChange{Watches: []string{"env", "groups"}}},
	}
	m := mapEntity(cm, 2, nil)
	// The node manifest spec carries change.watches verbatim — the path the
	// affected engine reads (spec.change.watches) for intent-impact watch mode.
	change, ok := m.Spec["change"].(map[string]any)
	if !ok {
		t.Fatalf("node spec missing change block: %v", m.Spec)
	}
	watches, ok := change["watches"].([]any)
	if !ok || len(watches) != 2 || watches[0] != "env" || watches[1] != "groups" {
		t.Fatalf("change.watches = %v", change["watches"])
	}
}

// TestMapEntity_Envelope asserts the SC1 reshape: the flat manifest splits into
// metadata/ownership/lifecycle, dependencies/system/domain/owner promote to
// typed relations, and provided/consumed APIs become contracts.
func TestMapEntity_Envelope(t *testing.T) {
	t.Parallel()
	cm := &catalogmodel.ComponentManifest{
		Identity: catalogmodel.ComponentIdentity{ComponentKey: "ns/repo/api", Name: "api", Namespace: "ns", Repo: "repo", SourceFile: "apps/api/component.yaml"},
		Metadata: catalogmodel.ComponentMetadata{
			Title:       "API",
			Description: "the api",
			Owner:       "platform",
			Maintainers: []string{"alice"},
			Contacts:    map[string]string{"slack": "#api", "email": "api@x"},
			Labels:      map[string]string{"team": "platform"},
			Tags:        []string{"edge"},
		},
		Spec: catalogmodel.ComponentSpec{
			Type:      "worker",
			Lifecycle: "production",
			Tier:      "tier-1",
			System:    "identity",
			Domain:    "platform",
			Dependencies: catalogmodel.ComponentDependencies{
				Components: []catalogmodel.ComponentDependency{{Key: "ns/repo/db", Name: "db", Optional: true, Include: "always"}},
				APIs:       catalogmodel.APIDependencies{Provides: []string{"ns/repo/api-spec"}, Consumes: []string{"ns/repo/auth"}},
				Resources:  catalogmodel.ResourceDependencies{Uses: []string{"ns/repo/cache"}},
			},
		},
		Runtime:    catalogmodel.ComponentRuntime{Inferred: catalogmodel.ComponentInferred{Languages: []string{"go"}}},
		Resolution: catalogmodel.ComponentResolution{InheritedFrom: map[string]string{"spec.lifecycle": "intent.yaml"}},
	}
	m := mapEntity(cm, 2, nil)

	if m.APIVersion != catalogmodel.APIVersionV1 || m.Type != "worker" {
		t.Fatalf("apiVersion/type = %q/%q", m.APIVersion, m.Type)
	}
	// metadata no longer carries owner/lifecycle.
	if _, ok := m.Metadata["owner"]; ok {
		t.Errorf("owner must not remain in metadata: %v", m.Metadata)
	}
	if m.Metadata["title"] != "API" {
		t.Errorf("metadata.title = %v", m.Metadata["title"])
	}
	// ownership block, with source.
	if m.Ownership["owner"] != "platform" || m.Ownership["source"] != catalogmodel.OwnershipSourceAuthored {
		t.Errorf("ownership = %v", m.Ownership)
	}
	contacts, _ := m.Ownership["contacts"].([]any)
	if len(contacts) != 2 { // sorted by type: email, slack
		t.Errorf("contacts = %v", m.Ownership["contacts"])
	} else if c0 := contacts[0].(map[string]any); c0["type"] != "email" {
		t.Errorf("contacts not sorted by type: %v", contacts)
	}
	// lifecycle block, maturity explicit null.
	if m.Lifecycle["stage"] != "production" || m.Lifecycle["tier"] != "tier-1" {
		t.Errorf("lifecycle = %v", m.Lifecycle)
	}
	if mat, ok := m.Lifecycle["maturity"]; !ok || mat != nil {
		t.Errorf("lifecycle.maturity must be present and null: %v (ok=%v)", mat, ok)
	}
	// relations: ownedBy(Group), partOf(System), partOf(Domain), dependsOn x2.
	wantRel := map[string]string{} // type|to -> toKind
	for _, r := range m.Relations {
		wantRel[r.Type+"|"+r.To] = r.ToKind
	}
	if wantRel["ownedBy|platform"] != "Group" || wantRel["partOf|identity"] != "System" ||
		wantRel["partOf|platform"] != "Domain" || wantRel["dependsOn|ns/repo/db"] != "Component" ||
		wantRel["dependsOn|ns/repo/cache"] != "Resource" {
		t.Errorf("relations = %+v", m.Relations)
	}
	// relations sorted by (type,to): dependsOn before ownedBy before partOf.
	if m.Relations[0].Type != "dependsOn" {
		t.Errorf("relations not sorted: %+v", m.Relations)
	}
	// the component dep carries optional/include through.
	for _, r := range m.Relations {
		if r.To == "ns/repo/db" && (!r.Optional || r.Include != "always") {
			t.Errorf("dep edge lost optional/include: %+v", r)
		}
	}
	// contracts from APIs.
	provides, _ := m.Contracts["provides"].([]any)
	consumes, _ := m.Contracts["consumes"].([]any)
	if len(provides) != 1 || len(consumes) != 1 {
		t.Errorf("contracts = %v", m.Contracts)
	}
	// spec stays lossless in SC1 (dependencies/system/domain remain) — SC2
	// promotes them fully to relations.json.
	if _, ok := m.Spec["dependencies"]; !ok {
		t.Errorf("spec should retain dependencies (lossless) in SC1: %v", m.Spec)
	}
	if m.Spec["system"] != "identity" || m.Spec["domain"] != "platform" {
		t.Errorf("spec should retain system/domain: %v", m.Spec)
	}
	// provenance resolver stamp.
	res, _ := m.Provenance["resolver"].(map[string]any)
	if res == nil || res["resolverVersion"] != 2 {
		t.Errorf("provenance.resolver = %v", m.Provenance["resolver"])
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("envelope invalid: %v", err)
	}
}

// TestMapEntity_CatalogHubBlocks asserts SC6: integrations/links/docs/extensions
// carry from the resolved manifest into the envelope (extensions preserved).
func TestMapEntity_CatalogHubBlocks(t *testing.T) {
	t.Parallel()
	cm := &catalogmodel.ComponentManifest{
		Identity:     catalogmodel.ComponentIdentity{ComponentKey: "ns/repo/api", Name: "api", Namespace: "ns", Repo: "repo"},
		Spec:         catalogmodel.ComponentSpec{Type: "worker"},
		Integrations: map[string]any{"datadog": map[string]any{"service": "api"}},
		Links:        []catalogmodel.ComponentLink{{Title: "Dash", URL: "https://x", Icon: "dashboard"}},
		Docs:         &catalogmodel.ComponentDocs{TechDocs: "docs/", Runbooks: []string{"docs/run.md"}, ADRs: []string{"docs/adr-1.md"}},
		Extensions:   map[string]any{"x-acme": map[string]any{"tier": "gold"}},
	}
	m := mapEntity(cm, 6, nil)
	if m.Integrations["datadog"] == nil {
		t.Errorf("integrations not carried: %v", m.Integrations)
	}
	if m.Extensions["x-acme"] == nil {
		t.Errorf("extensions not preserved: %v", m.Extensions)
	}
	if m.Docs["techdocs"] != "docs/" {
		t.Errorf("docs = %v", m.Docs)
	}
	if runbooks, _ := m.Docs["runbooks"].([]any); len(runbooks) != 1 {
		t.Errorf("docs.runbooks = %v", m.Docs["runbooks"])
	}
	if adrs, _ := m.Docs["adrs"].([]any); len(adrs) != 1 {
		t.Errorf("docs.adrs = %v", m.Docs["adrs"])
	}
	if len(m.Links) != 1 || m.Links[0]["title"] != "Dash" || m.Links[0]["icon"] != "dashboard" {
		t.Errorf("links = %v", m.Links)
	}
	// nil docs/links/integrations → nil blocks (no panic).
	bare := mapEntity(&catalogmodel.ComponentManifest{
		Identity: catalogmodel.ComponentIdentity{ComponentKey: "ns/repo/b", Name: "b"},
		Spec:     catalogmodel.ComponentSpec{Type: "lib"},
	}, 6, nil)
	if bare.Docs != nil || bare.Links != nil || bare.Integrations != nil {
		t.Errorf("bare blocks should be nil: docs=%v links=%v int=%v", bare.Docs, bare.Links, bare.Integrations)
	}
}

// TestMapEntity_EnvironmentRelations asserts SC4: a component's environment
// bindings emit deployedTo edges to derived Environment entities (sorted).
func TestMapEntity_EnvironmentRelations(t *testing.T) {
	t.Parallel()
	cm := &catalogmodel.ComponentManifest{
		Identity: catalogmodel.ComponentIdentity{ComponentKey: "ns/repo/api", Name: "api", Namespace: "ns", Repo: "repo"},
		Spec: catalogmodel.ComponentSpec{
			Type: "worker",
			Environments: map[string]catalogmodel.ComponentEnvironment{
				"production": {Profile: "release", Active: true},
				"staging":    {Profile: "pr", Active: false},
			},
		},
	}
	m := mapEntity(cm, 5, nil)
	var envs []string
	for _, r := range m.Relations {
		if r.Type == "deployedTo" && r.ToKind == "Environment" {
			envs = append(envs, r.To)
		}
	}
	if len(envs) != 2 || envs[0] != "production" || envs[1] != "staging" {
		t.Fatalf("deployedTo env edges = %v", envs)
	}
}

// TestMapEntity_UnknownOwner asserts an unowned component gets owner=unknown,
// TestMapEntity_CodeownersOwnership asserts the ownership precedence (S-2):
// authored owner wins; otherwise a CODEOWNERS match supplies owner + source;
// otherwise unknown. The ownedBy relation tracks the resolved owner.
func TestMapEntity_CodeownersOwnership(t *testing.T) {
	t.Parallel()
	resolver := OwnerResolver(func(path string) []string {
		if path == "apps/api/component.yaml" {
			return []string{"@org/api-team", "@org/sre"}
		}
		return nil
	})

	// 1. No authored owner + CODEOWNERS match → source=CODEOWNERS.
	cm := &catalogmodel.ComponentManifest{
		Identity: catalogmodel.ComponentIdentity{ComponentKey: "ns/repo/api", Name: "api", Namespace: "ns", Repo: "repo", SourceFile: "apps/api/component.yaml"},
		Spec:     catalogmodel.ComponentSpec{Type: "worker"},
	}
	m := mapEntity(cm, 3, resolver)
	if m.Ownership["owner"] != "@org/api-team" || m.Ownership["source"] != catalogmodel.OwnershipSourceCODEOWNERS {
		t.Fatalf("codeowners ownership = %v", m.Ownership)
	}
	add, _ := m.Ownership["additionalOwners"].([]any)
	if len(add) != 1 || add[0] != "@org/sre" {
		t.Errorf("additionalOwners = %v", m.Ownership["additionalOwners"])
	}
	var ownedBy bool
	for _, r := range m.Relations {
		if r.Type == "ownedBy" && r.To == "@org/api-team" && r.ToKind == "Group" {
			ownedBy = true
		}
	}
	if !ownedBy {
		t.Errorf("missing ownedBy relation to CODEOWNERS owner: %+v", m.Relations)
	}

	// 2. Authored owner wins over CODEOWNERS.
	cm.Metadata.Owner = "authored-team"
	m = mapEntity(cm, 3, resolver)
	if m.Ownership["owner"] != "authored-team" || m.Ownership["source"] != catalogmodel.OwnershipSourceAuthored {
		t.Fatalf("authored should win: %v", m.Ownership)
	}

	// 3. No authored owner + no CODEOWNERS match → unknown, no ownedBy edge.
	cm2 := &catalogmodel.ComponentManifest{
		Identity: catalogmodel.ComponentIdentity{ComponentKey: "ns/repo/orphan", Name: "orphan", Namespace: "ns", Repo: "repo", SourceFile: "libs/orphan/component.yaml"},
		Spec:     catalogmodel.ComponentSpec{Type: "library"},
	}
	m = mapEntity(cm2, 3, resolver)
	if m.Ownership["source"] != catalogmodel.OwnershipSourceUnknown {
		t.Fatalf("orphan should be unknown: %v", m.Ownership)
	}
	for _, r := range m.Relations {
		if r.Type == "ownedBy" {
			t.Errorf("unowned must have no ownedBy edge: %+v", m.Relations)
		}
	}
}

// TestOwnerResolverForWorkspace reads a CODEOWNERS file from a temp workspace.
func TestOwnerResolverForWorkspace(t *testing.T) {
	t.Parallel()
	if OwnerResolverForWorkspace("") != nil {
		t.Error("empty root should yield nil resolver")
	}
	dir := t.TempDir()
	if OwnerResolverForWorkspace(dir) != nil {
		t.Error("no CODEOWNERS file should yield nil resolver")
	}
	if err := os.MkdirAll(filepath.Join(dir, ".github"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".github", "CODEOWNERS"), []byte("apps/* @team\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := OwnerResolverForWorkspace(dir)
	if r == nil {
		t.Fatal("CODEOWNERS present but resolver nil")
	}
	if owners := r("apps/api"); len(owners) != 1 || owners[0] != "@team" {
		t.Errorf("resolver(apps/api) = %v", owners)
	}
}

// source=unknown and no ownedBy relation.
func TestMapEntity_UnknownOwner(t *testing.T) {
	t.Parallel()
	cm := &catalogmodel.ComponentManifest{
		Identity: catalogmodel.ComponentIdentity{ComponentKey: "ns/repo/x", Name: "x", Namespace: "ns", Repo: "repo"},
		Spec:     catalogmodel.ComponentSpec{Type: "service"},
	}
	m := mapEntity(cm, 2, nil)
	if m.Ownership["owner"] != catalogmodel.OwnershipSourceUnknown || m.Ownership["source"] != catalogmodel.OwnershipSourceUnknown {
		t.Errorf("unowned ownership = %v", m.Ownership)
	}
	for _, r := range m.Relations {
		if r.Type == "ownedBy" {
			t.Errorf("unowned component must have no ownedBy relation: %+v", m.Relations)
		}
	}
	// stage defaults to experimental.
	if m.Lifecycle["stage"] != catalogmodel.LifecycleStageExperimental {
		t.Errorf("default stage = %v", m.Lifecycle["stage"])
	}
}

func TestBuildOwnershipMapsComponentDirs(t *testing.T) {
	t.Parallel()
	view := &catalogresolve.CatalogView{
		ResolvedCatalog: &catalogresolve.ResolvedCatalog{
			Manifests: []*catalogmodel.ComponentManifest{
				{Identity: catalogmodel.ComponentIdentity{ComponentKey: "ns/repo/api", Name: "api", SourceFile: "apps/api/component.yaml"}},
				{Identity: catalogmodel.ComponentIdentity{ComponentKey: "ns/repo/root", Name: "root", SourceFile: "component.yaml"}},
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
