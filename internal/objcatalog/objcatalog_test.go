package objcatalog

import (
	"context"
	"testing"

	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

type fixture struct {
	store *objectstore.LocalStore
	refs  *refstore.LocalRefStore
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Clock: clock.Fixed{}})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	return fixture{store: store, refs: refs}
}

func sampleManifests() []nodes.ComponentManifest {
	return []nodes.ComponentManifest{
		{
			Kind: nodes.KindComponentManifest,
			Identity: nodes.ComponentIdentity{
				ComponentKey: "sourceplane/orun/api-edge",
				Name:         "api-edge",
				Namespace:    "sourceplane",
				Repo:         "orun",
				Path:         "apps/api-edge/component.yaml",
			},
			Type:      "cloudflare-worker",
			Ownership: map[string]any{"owner": "team/platform-edge", "source": "authored"},
			Lifecycle: map[string]any{"stage": "production", "maturity": nil},
			Spec: map[string]any{
				"type":   "cloudflare-worker",
				"domain": "edge",
				"environments": map[string]any{
					"production": map[string]any{"active": true, "profile": "worker.release"},
					"staging":    map[string]any{"active": false, "profile": "worker.pull_request"},
				},
				"dependencies": map[string]any{
					"components": []any{
						map[string]any{"key": "sourceplane/orun/shared", "name": "shared"},
						map[string]any{"key": "sourceplane/orun/identity", "name": "identity"},
					},
				},
			},
			Relations: []nodes.EntityRelation{
				{Type: "dependsOn", To: "sourceplane/orun/shared", ToKind: "Component"},
				{Type: "dependsOn", To: "sourceplane/orun/identity", ToKind: "Component"},
				{Type: "partOf", To: "edge", ToKind: "Domain"},
			},
		},
		{
			Kind: nodes.KindComponentManifest,
			Identity: nodes.ComponentIdentity{
				ComponentKey: "sourceplane/orun/shared",
				Name:         "shared",
				Namespace:    "sourceplane",
				Repo:         "orun",
				Path:         "libs/shared/component.yaml",
			},
			Spec: map[string]any{"type": "library"},
		},
	}
}

func sampleGraphs() []nodes.CatalogGraph {
	return []nodes.CatalogGraph{
		{
			Kind:     nodes.KindCatalogGraph,
			EdgeKind: "dependencies",
			Nodes: []nodes.GraphNode{
				{Key: "sourceplane/orun/api-edge", Kind: "component", Name: "api-edge"},
				{Key: "sourceplane/orun/shared", Kind: "component", Name: "shared"},
			},
			Edges: []nodes.GraphEdge{
				{From: "sourceplane/orun/api-edge", To: "sourceplane/orun/shared", Type: "depends_on"},
			},
		},
	}
}

// seedCatalog writes a catalog tree (no impact/) and points catalogs/current at
// it. Returns the catalog root id.
func seedCatalog(t *testing.T, f fixture) objectstore.ObjectID {
	t.Helper()
	ctx := context.Background()
	cat := nodes.CatalogSnapshot{
		Kind:            nodes.KindCatalogSnapshot,
		HumanKey:        "cat-sample",
		SourceID:        "sha256:" + repeat("a", 64),
		ResolverVersion: 1,
	}
	root, err := nodes.AssembleCatalog(ctx, f.store, cat, sampleManifests(), sampleGraphs(), nodes.ImpactOwnership{}, nil)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if err := f.refs.Update(ctx, refCatalogCurrent, "", string(root)); err != nil {
		t.Fatalf("ref update: %v", err)
	}
	return root
}

func repeat(s string, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = s[0]
	}
	return string(out)
}

func TestLoad_CurrentRef(t *testing.T) {
	f := newFixture(t)
	root := seedCatalog(t, f)
	ctx := context.Background()

	view, err := New(f.store, f.refs).Load(ctx, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if view.ObjectID != root {
		t.Errorf("ObjectID = %q, want %q", view.ObjectID, root)
	}
	if view.HumanKey != "cat-sample" {
		t.Errorf("HumanKey = %q", view.HumanKey)
	}
	if view.SourceID == "" {
		t.Errorf("SourceID empty")
	}
	if len(view.Components) != 2 {
		t.Fatalf("components = %d, want 2", len(view.Components))
	}
	// Sorted by component key: api-edge < shared.
	api := view.Components[0]
	if api.ComponentKey != "sourceplane/orun/api-edge" {
		t.Fatalf("first component = %q", api.ComponentKey)
	}
	if api.Path != "apps/api-edge/component.yaml" {
		t.Errorf("Path = %q (the CS1 path fix must round-trip)", api.Path)
	}
	if api.Type != "cloudflare-worker" {
		t.Errorf("Type = %q", api.Type)
	}
	if api.Domain != "edge" {
		t.Errorf("Domain = %q", api.Domain)
	}
	if got := api.Environments["production"]; got.Profile != "worker.release" || !got.Active {
		t.Errorf("production env = %+v", got)
	}
	if got := api.Environments["staging"]; got.Active {
		t.Errorf("staging should be inactive: %+v", got)
	}
	// DependsOn is sorted.
	if len(api.DependsOn) != 2 || api.DependsOn[0] != "sourceplane/orun/identity" || api.DependsOn[1] != "sourceplane/orun/shared" {
		t.Errorf("DependsOn = %v", api.DependsOn)
	}
	// Owner is projected from the ownership block (no longer in metadata).
	if api.Owner != "team/platform-edge" || api.OwnerSource != "authored" {
		t.Errorf("owner projection = %q/%q", api.Owner, api.OwnerSource)
	}

	// SC3: the Domain entity derived from the partOf relation is enumerated,
	// and catalog.json carries countsByKind.
	var sawDomain bool
	for _, e := range view.Entities {
		if e.Kind == "Domain" && e.Name == "edge" {
			sawDomain = true
		}
	}
	if !sawDomain {
		t.Errorf("derived Domain entity not enumerated: %+v", view.Entities)
	}
	if view.CountsByKind["Component"] != 2 || view.CountsByKind["Domain"] != 1 {
		t.Errorf("countsByKind = %v", view.CountsByKind)
	}
	if api.Stage != "production" {
		t.Errorf("Stage = %q", api.Stage)
	}

	// shared has type from spec only (manifest.Type empty) and no deps/envs.
	shared := view.Components[1]
	if shared.Type != "library" {
		t.Errorf("shared.Type = %q (should fall back to spec.type)", shared.Type)
	}
	if shared.DependsOn != nil || shared.Environments != nil || shared.Domain != "" {
		t.Errorf("shared should have empty deps/envs/domain: %+v", shared)
	}

	// Graph slice keyed by edgeKind.
	dep, ok := view.Graph["dependencies"]
	if !ok {
		t.Fatalf("dependencies graph missing")
	}
	if len(dep.Nodes) != 2 || len(dep.Edges) != 1 {
		t.Errorf("graph slice = %+v", dep)
	}
	if dep.Edges[0].Type != "depends_on" {
		t.Errorf("edge type = %q", dep.Edges[0].Type)
	}

	// AssembleCatalog always writes impact/ownership.json (CS3); with an empty
	// ImpactOwnership the map is present but carries no component entries.
	if view.Ownership == nil {
		t.Fatalf("Ownership nil, want the always-written ownership map")
	}
	if view.Ownership.SchemaVersion != 1 {
		t.Errorf("ownership schemaVersion = %d, want 1", view.Ownership.SchemaVersion)
	}
	if len(view.Ownership.Components) != 0 {
		t.Errorf("empty ownership should carry no components: %v", view.Ownership.Components)
	}
}

func TestLoad_GraphEdgeOptional(t *testing.T) {
	// The object-model catalog graph must carry dependency-edge optionality
	// (the resolver knows it; the lossy mapping used to drop it). A faithful
	// `orun catalog tree` re-point depends on this round-tripping.
	f := newFixture(t)
	ctx := context.Background()
	cat := nodes.CatalogSnapshot{
		Kind:            nodes.KindCatalogSnapshot,
		HumanKey:        "cat-optional",
		SourceID:        "sha256:" + repeat("b", 64),
		ResolverVersion: 1,
	}
	graphs := []nodes.CatalogGraph{{
		Kind:     nodes.KindCatalogGraph,
		EdgeKind: "dependencies",
		Nodes: []nodes.GraphNode{
			{Key: "ns/repo/a", Kind: "component", Name: "a"},
			{Key: "ns/repo/b", Kind: "component", Name: "b"},
			{Key: "ns/repo/c", Kind: "component", Name: "c"},
		},
		Edges: []nodes.GraphEdge{
			{From: "ns/repo/a", To: "ns/repo/b", Type: "depends_on", Optional: true},
			{From: "ns/repo/a", To: "ns/repo/c", Type: "depends_on"},
		},
	}}
	root, err := nodes.AssembleCatalog(ctx, f.store, cat, sampleManifests(), graphs, nodes.ImpactOwnership{}, nil)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if err := f.refs.Update(ctx, refCatalogCurrent, "", string(root)); err != nil {
		t.Fatalf("ref update: %v", err)
	}

	view, err := New(f.store, f.refs).Load(ctx, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	dep, ok := view.Graph["dependencies"]
	if !ok {
		t.Fatalf("dependencies graph missing")
	}
	got := map[string]bool{}
	for _, e := range dep.Edges {
		got[e.To] = e.Optional
	}
	if !got["ns/repo/b"] {
		t.Errorf("a→b should be optional, edges = %+v", dep.Edges)
	}
	if got["ns/repo/c"] {
		t.Errorf("a→c should be required, edges = %+v", dep.Edges)
	}
}

func TestLoad_NoImpactSubtree(t *testing.T) {
	// A pre-CS3 catalog (no impact/ subtree at all) still loads, with Ownership
	// left nil — the reader's forward/backward compatibility contract.
	f := newFixture(t)
	ctx := context.Background()
	root := seedCatalogWithExtraSubtree(t, f, dirImpact, "") // "" drops impact/

	view, err := New(f.store, f.refs).Load(ctx, string(root))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if view.Ownership != nil {
		t.Errorf("Ownership should be nil when impact/ is wholly absent: %+v", view.Ownership)
	}
}

func TestLoad_ByObjectID(t *testing.T) {
	f := newFixture(t)
	root := seedCatalog(t, f)
	ctx := context.Background()

	view, err := New(f.store, f.refs).Load(ctx, string(root))
	if err != nil {
		t.Fatalf("load by id: %v", err)
	}
	if view.ObjectID != root || len(view.Components) != 2 {
		t.Errorf("by-id load mismatch: %q / %d comps", view.ObjectID, len(view.Components))
	}
}

func TestLoad_WithImpactOwnership(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	root := seedCatalogWithImpact(t, f, `{
		"kind": "ImpactOwnership",
		"schemaVersion": 1,
		"components": {"apps/api-edge": "sourceplane/orun/api-edge", "libs/shared": "sourceplane/orun/shared"},
		"globalPaths": ["intent.yaml"],
		"globalBlocks": ["catalog.defaults"],
		"structuralFilenames": ["component.yaml"],
		"ignoreDirs": [".git", "node_modules"]
	}`)

	view, err := New(f.store, f.refs).Load(ctx, string(root))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if view.Ownership == nil {
		t.Fatalf("Ownership nil, want decoded map")
	}
	o := view.Ownership
	if o.SchemaVersion != 1 {
		t.Errorf("schemaVersion = %d", o.SchemaVersion)
	}
	if o.Components["apps/api-edge"] != "sourceplane/orun/api-edge" {
		t.Errorf("ownership components = %v", o.Components)
	}
	if len(o.GlobalPaths) != 1 || o.GlobalPaths[0] != "intent.yaml" {
		t.Errorf("globalPaths = %v", o.GlobalPaths)
	}
	if len(o.IgnoreDirs) != 2 {
		t.Errorf("ignoreDirs = %v", o.IgnoreDirs)
	}
}

func TestLoad_ReadsEntities_MultiKindAndSkips(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sysBlob, _ := f.store.PutBlob(ctx, []byte(`{"apiVersion":"orun.io/v1","kind":"System","identity":{"entityKey":"ns/repo/identity","kind":"System","name":"identity"}}`))
	grpBlob, _ := f.store.PutBlob(ctx, []byte(`{"apiVersion":"orun.io/v1","kind":"Group","identity":{"entityKey":"@org/team","kind":"Group","name":"team"}}`))
	// A kind subtree with a valid blob plus a stray non-blob entry (skipped).
	strayTree, _ := f.store.PutTree(ctx, nil)
	sysTree, _ := f.store.PutTree(ctx, []objectstore.TreeEntry{
		{Name: "identity.json", Kind: objectstore.KindBlob, ID: sysBlob},
		{Name: "stray", Kind: objectstore.KindTree, ID: strayTree},
	})
	grpTree, _ := f.store.PutTree(ctx, []objectstore.TreeEntry{{Name: "team.json", Kind: objectstore.KindBlob, ID: grpBlob}})
	// A top-level non-tree entry (skipped) sits beside the kind subtrees.
	entities, _ := f.store.PutTree(ctx, []objectstore.TreeEntry{
		{Name: "System", Kind: objectstore.KindTree, ID: sysTree},
		{Name: "Group", Kind: objectstore.KindTree, ID: grpTree},
		{Name: "note.txt", Kind: objectstore.KindBlob, ID: sysBlob},
	})
	root := seedCatalogWithExtraSubtree(t, f, dirEntities, entities)

	view, err := New(f.store, f.refs).Load(ctx, string(root))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(view.Entities) != 2 {
		t.Fatalf("entities = %+v", view.Entities)
	}
	// Sorted by (kind, key): Group before System.
	if view.Entities[0].Kind != "Group" || view.Entities[0].Name != "team" {
		t.Errorf("first entity = %+v", view.Entities[0])
	}
	if view.Entities[1].Kind != "System" || view.Entities[1].EntityKey != "ns/repo/identity" {
		t.Errorf("second entity = %+v", view.Entities[1])
	}
}

func TestLoad_CorruptEntityBlob(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	bad, _ := f.store.PutBlob(ctx, []byte(`{not json`))
	kindTree, _ := f.store.PutTree(ctx, []objectstore.TreeEntry{{Name: "x.json", Kind: objectstore.KindBlob, ID: bad}})
	entities, _ := f.store.PutTree(ctx, []objectstore.TreeEntry{{Name: "System", Kind: objectstore.KindTree, ID: kindTree}})
	root := seedCatalogWithExtraSubtree(t, f, dirEntities, entities)
	if _, err := New(f.store, f.refs).Load(ctx, string(root)); err == nil {
		t.Fatal("corrupt entity blob should fail load")
	}
}

func TestLoad_ReadsFingerprints(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	own, _ := f.store.PutBlob(ctx, []byte(`{"kind":"ImpactOwnership","schemaVersion":1}`))
	fpBlob, _ := f.store.PutBlob(ctx, []byte(`{"kind":"ComponentFingerprint","schemaVersion":1,"componentKey":"sourceplane/orun/api-edge","dir":"apps/api-edge","subtree":"sha256:abc","globalDigest":"sha256:gd","files":{"apps/api-edge/component.yaml":"sha256:f"}}`))
	fpTree, _ := f.store.PutTree(ctx, []objectstore.TreeEntry{{Name: "api-edge.json", Kind: objectstore.KindBlob, ID: fpBlob}})
	impact, _ := f.store.PutTree(ctx, []objectstore.TreeEntry{
		{Name: fileOwnership, Kind: objectstore.KindBlob, ID: own},
		{Name: dirFingerprints, Kind: objectstore.KindTree, ID: fpTree},
	})
	root := seedCatalogWithExtraSubtree(t, f, dirImpact, impact)

	view, err := New(f.store, f.refs).Load(ctx, string(root))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	fp, ok := view.Fingerprints["sourceplane/orun/api-edge"]
	if !ok {
		t.Fatalf("fingerprint not read; have %v", view.Fingerprints)
	}
	if fp.Subtree != "sha256:abc" || fp.Dir != "apps/api-edge" || fp.GlobalDigest != "sha256:gd" {
		t.Errorf("fingerprint = %+v", fp)
	}
	if fp.Files["apps/api-edge/component.yaml"] != "sha256:f" {
		t.Errorf("fingerprint files = %v", fp.Files)
	}
}

func TestLoad_ImpactWithoutOwnership(t *testing.T) {
	// An impact/ subtree that holds only fingerprints/ (no ownership.json) must
	// still yield Ownership == nil, not an error.
	f := newFixture(t)
	ctx := context.Background()
	root := seedCatalogImpactFingerprintsOnly(t, f)

	view, err := New(f.store, f.refs).Load(ctx, string(root))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if view.Ownership != nil {
		t.Errorf("Ownership should be nil when ownership.json absent: %+v", view.Ownership)
	}
}

func TestLoad_MissingRef(t *testing.T) {
	f := newFixture(t)
	_, err := New(f.store, f.refs).Load(context.Background(), "catalogs/nope")
	if err == nil {
		t.Fatalf("expected error for missing ref")
	}
}

func TestLoad_NotACatalogTree(t *testing.T) {
	// A tree with no catalog.json is invalid.
	f := newFixture(t)
	ctx := context.Background()
	blob, err := f.store.PutBlob(ctx, []byte(`{"kind":"x"}`))
	if err != nil {
		t.Fatalf("blob: %v", err)
	}
	root, err := f.store.PutTree(ctx, []objectstore.TreeEntry{
		{Name: "other.json", Kind: objectstore.KindBlob, ID: blob},
	})
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	if _, err := New(f.store, f.refs).Load(ctx, string(root)); err == nil {
		t.Fatalf("expected invalid for tree without catalog.json")
	}
}

func TestIsObjectID(t *testing.T) {
	cases := map[string]bool{
		"sha256:" + repeat("a", 64): true,
		"catalogs/current":          false,
		"":                          false,
		"sha256:":                   false,
		"sha256:XYZ":                false, // non-lowerhex
		"plainname":                 false,
	}
	for in, want := range cases {
		if got := isObjectID(in); got != want {
			t.Errorf("isObjectID(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestComponentView_SpecEdgeCases(t *testing.T) {
	// environments with a non-map body is handled without panicking; the typed
	// relations project into DependsOn (Component edges only) and System/Domain
	// (first partOf membership of each kind).
	m := nodes.ComponentManifest{
		Kind: nodes.KindComponentManifest,
		Identity: nodes.ComponentIdentity{
			ComponentKey: "sourceplane/orun/odd",
			Name:         "odd",
			Namespace:    "sourceplane",
			Repo:         "orun",
		},
		Spec: map[string]any{
			"system": "ident",
			"domain": "plat",
			"environments": map[string]any{
				"prod":   map[string]any{"active": true, "profile": "p"},
				"broken": "not-a-map",
			},
			"dependencies": map[string]any{
				"components": []any{
					map[string]any{"key": "sourceplane/orun/a"},
					map[string]any{"name": "no-key"},
					"not-a-map",
				},
			},
		},
		Relations: []nodes.EntityRelation{
			{Type: "dependsOn", To: "sourceplane/orun/a", ToKind: "Component"},
			{Type: "partOf", To: "ident", ToKind: "System"},
		},
	}
	v := componentView(m)
	if v.Type != "" {
		t.Errorf("Type = %q, want empty (no spec.type, no manifest.Type)", v.Type)
	}
	if got := v.Environments["broken"]; got.Profile != "" || got.Active {
		t.Errorf("broken env should decode to zero EnvView: %+v", got)
	}
	if got := v.Environments["prod"]; !got.Active || got.Profile != "p" {
		t.Errorf("prod env = %+v", got)
	}
	// DependsOn keeps only keyed dependency entries (from the lossless spec block).
	if len(v.DependsOn) != 1 || v.DependsOn[0] != "sourceplane/orun/a" {
		t.Errorf("DependsOn should keep only keyed entries: %v", v.DependsOn)
	}
	if v.System != "ident" || v.Domain != "plat" {
		t.Errorf("System/Domain projection = %q/%q", v.System, v.Domain)
	}
	// Typed relations are carried additively for the portal/graph.
	if len(v.Relations) != 2 {
		t.Errorf("Relations carried = %d, want 2", len(v.Relations))
	}
}

func TestStringAndBoolField_NilMap(t *testing.T) {
	if stringField(nil, "x") != "" {
		t.Errorf("stringField(nil) not empty")
	}
	if boolField(nil, "x") {
		t.Errorf("boolField(nil) not false")
	}
	if stringField(map[string]any{"x": 1}, "x") != "" {
		t.Errorf("stringField non-string not empty")
	}
}

func TestLoad_CorruptManifestBlob(t *testing.T) {
	// A components/<name>.json that is not valid JSON propagates a decode error
	// rather than being silently dropped.
	f := newFixture(t)
	ctx := context.Background()
	catBlob, err := f.store.PutBlob(ctx, []byte(`{"kind":"CatalogSnapshot","sourceId":"sha256:`+repeat("a", 64)+`","resolverVersion":1,"componentCount":0,"components":[]}`))
	if err != nil {
		t.Fatalf("cat blob: %v", err)
	}
	badBlob, err := f.store.PutBlob(ctx, []byte(`{ not json`))
	if err != nil {
		t.Fatalf("bad blob: %v", err)
	}
	// A KindTree child inside components/ must be skipped, not decoded.
	innerTree, err := f.store.PutTree(ctx, nil)
	if err != nil {
		t.Fatalf("inner tree: %v", err)
	}
	compTree, err := f.store.PutTree(ctx, []objectstore.TreeEntry{
		{Name: "bad.json", Kind: objectstore.KindBlob, ID: badBlob},
		{Name: "nested", Kind: objectstore.KindTree, ID: innerTree},
	})
	if err != nil {
		t.Fatalf("comp tree: %v", err)
	}
	root, err := f.store.PutTree(ctx, []objectstore.TreeEntry{
		{Name: fileCatalog, Kind: objectstore.KindBlob, ID: catBlob},
		{Name: dirComponents, Kind: objectstore.KindTree, ID: compTree},
	})
	if err != nil {
		t.Fatalf("root tree: %v", err)
	}
	if _, err := New(f.store, f.refs).Load(ctx, string(root)); err == nil {
		t.Fatalf("expected decode error from corrupt manifest blob")
	}
}

func catalogBlob(t *testing.T, f fixture) objectstore.ObjectID {
	t.Helper()
	id, err := f.store.PutBlob(context.Background(),
		[]byte(`{"kind":"CatalogSnapshot","sourceId":"sha256:`+repeat("a", 64)+`","resolverVersion":1,"componentCount":0,"components":[]}`))
	if err != nil {
		t.Fatalf("cat blob: %v", err)
	}
	return id
}

func TestLoad_CorruptGraphBlob(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	bad, _ := f.store.PutBlob(ctx, []byte(`{ not json`))
	inner, _ := f.store.PutTree(ctx, nil) // a KindTree child must be skipped
	graphTree, _ := f.store.PutTree(ctx, []objectstore.TreeEntry{
		{Name: "nested", Kind: objectstore.KindTree, ID: inner},
		{Name: "dependencies.json", Kind: objectstore.KindBlob, ID: bad},
	})
	root, _ := f.store.PutTree(ctx, []objectstore.TreeEntry{
		{Name: fileCatalog, Kind: objectstore.KindBlob, ID: catalogBlob(t, f)},
		{Name: dirGraph, Kind: objectstore.KindTree, ID: graphTree},
	})
	if _, err := New(f.store, f.refs).Load(ctx, string(root)); err == nil {
		t.Fatalf("expected decode error from corrupt graph blob")
	}
}

func TestLoad_CorruptOwnershipBlob(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	bad, _ := f.store.PutBlob(ctx, []byte(`{ not json`))
	inner, _ := f.store.PutTree(ctx, nil) // a KindTree child must be skipped
	impactTree, _ := f.store.PutTree(ctx, []objectstore.TreeEntry{
		{Name: dirFingerprints, Kind: objectstore.KindTree, ID: inner},
		{Name: fileOwnership, Kind: objectstore.KindBlob, ID: bad},
	})
	root, _ := f.store.PutTree(ctx, []objectstore.TreeEntry{
		{Name: fileCatalog, Kind: objectstore.KindBlob, ID: catalogBlob(t, f)},
		{Name: dirImpact, Kind: objectstore.KindTree, ID: impactTree},
	})
	if _, err := New(f.store, f.refs).Load(ctx, string(root)); err == nil {
		t.Fatalf("expected decode error from corrupt ownership blob")
	}
}

// --- manual tree builders for impact/ cases ---
//
// AssembleCatalog now always writes an impact/ownership.json (CS3), so these
// helpers REPLACE that auto-written impact/ subtree to exercise specific shapes
// (custom ownership, fingerprints-only, or none at all).

func seedCatalogWithImpact(t *testing.T, f fixture, ownershipJSON string) objectstore.ObjectID {
	t.Helper()
	ctx := context.Background()
	ownBlob, err := f.store.PutBlob(ctx, []byte(ownershipJSON))
	if err != nil {
		t.Fatalf("ownership blob: %v", err)
	}
	impactTree, err := f.store.PutTree(ctx, []objectstore.TreeEntry{
		{Name: fileOwnership, Kind: objectstore.KindBlob, ID: ownBlob},
	})
	if err != nil {
		t.Fatalf("impact tree: %v", err)
	}
	return seedCatalogWithExtraSubtree(t, f, dirImpact, impactTree)
}

func seedCatalogImpactFingerprintsOnly(t *testing.T, f fixture) objectstore.ObjectID {
	t.Helper()
	ctx := context.Background()
	fpBlob, err := f.store.PutBlob(ctx, []byte(`{"kind":"ComponentFingerprint"}`))
	if err != nil {
		t.Fatalf("fp blob: %v", err)
	}
	fpTree, err := f.store.PutTree(ctx, []objectstore.TreeEntry{
		{Name: "api-edge.json", Kind: objectstore.KindBlob, ID: fpBlob},
	})
	if err != nil {
		t.Fatalf("fp tree: %v", err)
	}
	impactTree, err := f.store.PutTree(ctx, []objectstore.TreeEntry{
		{Name: dirFingerprints, Kind: objectstore.KindTree, ID: fpTree},
	})
	if err != nil {
		t.Fatalf("impact tree: %v", err)
	}
	return seedCatalogWithExtraSubtree(t, f, dirImpact, impactTree)
}

// seedCatalogWithExtraSubtree rebuilds the AssembleCatalog tree and replaces the
// named subtree (impact/) in the catalog root with sub (a nil sub drops it
// entirely, modelling a pre-CS3 catalog), then republishes the ref.
func seedCatalogWithExtraSubtree(t *testing.T, f fixture, name string, sub objectstore.ObjectID) objectstore.ObjectID {
	t.Helper()
	ctx := context.Background()
	base := seedCatalog(t, f)
	entries, err := f.store.GetTree(ctx, base)
	if err != nil {
		t.Fatalf("get base tree: %v", err)
	}
	kept := entries[:0]
	for _, e := range entries {
		if e.Name == name {
			continue // drop the auto-written entry; re-add below if sub is set
		}
		kept = append(kept, e)
	}
	if sub != "" {
		kept = append(kept, objectstore.TreeEntry{Name: name, Kind: objectstore.KindTree, ID: sub})
	}
	root, err := f.store.PutTree(ctx, kept)
	if err != nil {
		t.Fatalf("graft tree: %v", err)
	}
	if err := f.refs.Update(ctx, refCatalogCurrent, string(base), string(root)); err != nil {
		t.Fatalf("ref update: %v", err)
	}
	return root
}
