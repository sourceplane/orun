package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objplan"
	"github.com/sourceplane/orun/internal/sourcectx"
)

func TestCatalogComponentSummaries_Mapping(t *testing.T) {
	comps := []objcatalog.CatalogComponentView{{
		ComponentKey: "ns/repo/api",
		Name:         "api",
		Type:         "worker",
		Domain:       "edge",
		Path:         "apps/api/component.yaml",
		Environments: map[string]objcatalog.EnvView{
			"prod": {Profile: "", Active: true},
			"dev":  {Profile: "small", Active: true},
		},
		DependsOn: []string{"ns/repo/shared"},
		Spec: map[string]any{
			"change": map[string]any{"watches": []any{"environments", "secrets"}},
		},
	}}

	got := catalogComponentSummaries(comps)
	if len(got) != 1 {
		t.Fatalf("want 1 summary, got %d", len(got))
	}
	c := got[0]
	if c.Name != "api" || c.Type != "worker" || c.Domain != "edge" {
		t.Errorf("scalar mapping wrong: %+v", c)
	}
	// Path is reduced to the component directory (matches the intent-path summary).
	if c.Path != "apps/api" {
		t.Errorf("Path = %q, want apps/api", c.Path)
	}
	// Envs sorted by name; Profile = first non-empty in name order (dev=small).
	if len(c.Envs) != 2 || c.Envs[0] != "dev" || c.Envs[1] != "prod" {
		t.Errorf("Envs = %v, want [dev prod]", c.Envs)
	}
	if c.Profile != "small" {
		t.Errorf("Profile = %q, want small", c.Profile)
	}
	// DependsOn is mapped from the dependency key back to the component name; the
	// referenced "shared" component is not in this single-component input, so the
	// fallback derives the name from the key's last segment.
	if len(c.DependsOn) != 1 || c.DependsOn[0] != "shared" {
		t.Errorf("DependsOn = %v, want [shared]", c.DependsOn)
	}
	if len(c.Watches) != 2 || c.Watches[0] != "environments" || c.Watches[1] != "secrets" {
		t.Errorf("Watches = %v, want [environments secrets]", c.Watches)
	}
}

func TestSpecWatches(t *testing.T) {
	if got := specWatches(nil); got != nil {
		t.Errorf("nil spec → %v, want nil", got)
	}
	if got := specWatches(map[string]any{"change": map[string]any{}}); got != nil {
		t.Errorf("change without watches → %v, want nil", got)
	}
	got := specWatches(map[string]any{
		"change": map[string]any{"watches": []any{"environments", 7, "secrets"}},
	})
	// Non-string entries are dropped.
	if len(got) != 2 || got[0] != "environments" || got[1] != "secrets" {
		t.Errorf("watches = %v, want [environments secrets]", got)
	}
}

func TestApplyChangeOverlay(t *testing.T) {
	comps := []ComponentSummary{{Name: "api"}, {Name: "web"}, {Name: "shared"}}

	// Nil overlay leaves the list untouched.
	applyChangeOverlay(comps, nil)
	for _, c := range comps {
		if c.Changed || c.ChangeKind != "" {
			t.Fatalf("nil overlay must not annotate: %+v", c)
		}
	}

	applyChangeOverlay(comps, map[string]string{"api": "changed", "web": "affected"})
	if !comps[0].Changed || comps[0].ChangeKind != "changed" {
		t.Errorf("api = %+v, want changed", comps[0])
	}
	if !comps[1].Changed || comps[1].ChangeKind != "affected" {
		t.Errorf("web = %+v, want affected", comps[1])
	}
	if comps[2].Changed || comps[2].ChangeKind != "" {
		t.Errorf("shared = %+v, want unaffected", comps[2])
	}
}

func TestCatalogChangeOverlay_NoObjectModel(t *testing.T) {
	s := NewLiveOrunService(LiveServiceConfig{}) // no ObjectModelRoot
	if got := s.catalogChangeOverlay(context.Background(), t.TempDir()); got != nil {
		t.Fatalf("with no object model the overlay must be nil, got %v", got)
	}
}

// TestCatalogChangeOverlay_TracksEdits seeds a real object-model catalog
// (api depends_on shared) with fingerprints matching a clean workspace, then
// proves the cockpit overlay path reports nothing on a clean tree and surfaces
// the edited component (+ its dependent) once an input changes — the CS6
// "overlay tracks edits" guarantee, exercised through the service wiring.
func TestCatalogChangeOverlay_TracksEdits(t *testing.T) {
	ctx := context.Background()
	om := t.TempDir() // ObjectModelRoot; store lives at om/objectmodel
	ws := t.TempDir() // workspace root
	root := filepath.Join(om, "objectmodel")

	writeWS := func(rel, body string) {
		p := filepath.Join(ws, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeWS("intent.yaml", "catalog: {}\n")
	writeWS("apps/api/component.yaml", "name: api\n")
	writeWS("libs/shared/component.yaml", "name: shared\n")

	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Clock: clock.Fixed{}, Writer: "test"})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}

	gd := catalogresolve.ComputeGlobalDigest(filepath.Join(ws, "intent.yaml"))
	fp := func(key, dir string) nodes.ComponentFingerprint {
		f := catalogresolve.FingerprintForDir(ws, dir, key, gd)
		return nodes.ComponentFingerprint{ComponentKey: key, Dir: dir, Subtree: f.Subtree, GlobalDigest: gd}
	}
	manifests := []nodes.ComponentManifest{
		{Kind: nodes.KindComponentManifest, Identity: nodes.ComponentIdentity{
			ComponentKey: "ns/repo/api", Name: "api", Namespace: "ns", Repo: "repo", Path: "apps/api/component.yaml"},
			Type: "worker", Spec: map[string]any{"type": "worker"}},
		{Kind: nodes.KindComponentManifest, Identity: nodes.ComponentIdentity{
			ComponentKey: "ns/repo/shared", Name: "shared", Namespace: "ns", Repo: "repo", Path: "libs/shared/component.yaml"}},
	}
	graphs := []nodes.CatalogGraph{{Kind: nodes.KindCatalogGraph, EdgeKind: "dependencies",
		Edges: []nodes.GraphEdge{{From: "ns/repo/api", To: "ns/repo/shared", Type: "depends_on"}}}}
	ownership := nodes.ImpactOwnership{Kind: nodes.KindImpactOwnership, SchemaVersion: 1,
		Components:          map[string]string{"apps/api": "ns/repo/api", "libs/shared": "ns/repo/shared"},
		GlobalPaths:         []string{"intent.yaml"},
		StructuralFilenames: []string{"component.yaml"},
		IgnoreDirs:          []string{".git"}}
	cat := nodes.CatalogSnapshot{Kind: nodes.KindCatalogSnapshot, SourceID: "sha256:" + repeat64('a'), ResolverVersion: 1}
	id, err := nodes.AssembleCatalog(ctx, store, cat, manifests, graphs, ownership,
		[]nodes.ComponentFingerprint{fp("ns/repo/api", "apps/api"), fp("ns/repo/shared", "libs/shared")})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if err := refs.Update(ctx, "catalogs/current", "", string(id)); err != nil {
		t.Fatalf("ref: %v", err)
	}

	s := NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: om})

	// Clean tree → no changes.
	if got := s.catalogChangeOverlay(ctx, ws); got != nil {
		t.Fatalf("clean workspace overlay = %v, want nil", got)
	}

	// Edit shared's input → shared is directly changed, api (its dependent) is affected.
	writeWS("libs/shared/component.yaml", "name: shared-edited\n")
	got := s.catalogChangeOverlay(ctx, ws)
	if got["shared"] != "changed" {
		t.Errorf("shared = %q, want changed (overlay = %v)", got["shared"], got)
	}
	if got["api"] != "affected" {
		t.Errorf("api = %q, want affected (overlay = %v)", got["api"], got)
	}
}

func TestFreshCatalogComponents_NoObjectModel(t *testing.T) {
	s := NewLiveOrunService(LiveServiceConfig{}) // no ObjectModelRoot
	if _, ok := s.freshCatalogComponents(context.Background(), t.TempDir()); ok {
		t.Fatal("with no object model the gate must miss (ok=false)")
	}
}

// TestFreshCatalogComponents_FreshVsStale builds an object-model catalog whose
// SourceID matches the workspace's current source (fresh) and proves the gate
// serves it; then it points the catalog at a different source id (stale) and
// proves the gate misses so the caller falls back to the live loader.
func TestFreshCatalogComponents_FreshVsStale(t *testing.T) {
	ctx := context.Background()
	om := t.TempDir() // ObjectModelRoot; store lives at om/objectmodel
	ws := t.TempDir() // workspace root
	if err := os.WriteFile(filepath.Join(ws, "intent.yaml"), []byte("catalog: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(om, "objectmodel")
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Clock: clock.Fixed{}, Writer: "test"})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}

	// The catalog's fresh source id = what catalogFresh recomputes for ws.
	wsState, err := sourcectx.ResolveSourceSnapshot(ctx, sourcectx.ResolveOptions{WorkspacePath: ws})
	if err != nil {
		t.Fatalf("resolve source: %v", err)
	}
	src := objplan.BuildSourceNode(wsState, sourcectx.BuildSourceSnapshotKey(wsState))
	srcID, err := nodes.SourceID(store.Algo(), src)
	if err != nil {
		t.Fatalf("source id: %v", err)
	}

	prev := ""
	writeCatalog := func(sourceID string) {
		manifests := []nodes.ComponentManifest{{
			Kind: nodes.KindComponentManifest,
			Identity: nodes.ComponentIdentity{
				ComponentKey: "ns/repo/api", Name: "api", Namespace: "ns", Repo: "repo",
				Path: "apps/api/component.yaml"},
			Type: "worker",
			Spec: map[string]any{"type": "worker", "domain": "edge"},
		}}
		cat := nodes.CatalogSnapshot{Kind: nodes.KindCatalogSnapshot, SourceID: sourceID, ResolverVersion: 1}
		id, err := nodes.AssembleCatalog(ctx, store, cat, manifests, nil, nodes.ImpactOwnership{}, nil)
		if err != nil {
			t.Fatalf("assemble: %v", err)
		}
		if err := refs.Update(ctx, "catalogs/current", prev, string(id)); err != nil {
			t.Fatalf("ref: %v", err)
		}
		prev = string(id)
	}

	s := NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: om})

	// Fresh: the catalog's source id matches the workspace → served from state.
	writeCatalog(string(srcID))
	comps, ok := s.freshCatalogComponents(ctx, ws)
	if !ok {
		t.Fatal("a catalog matching the current source must be served (ok=true)")
	}
	if len(comps) != 1 || comps[0].Name != "api" {
		t.Fatalf("fresh components = %+v", comps)
	}

	// Stale: a different source id → the gate misses (fall back to live loader).
	writeCatalog("sha256:" + repeat64('b'))
	if _, ok := s.freshCatalogComponents(ctx, ws); ok {
		t.Fatal("a catalog resolved against a different source must miss (ok=false)")
	}
}

func repeat64(c byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
