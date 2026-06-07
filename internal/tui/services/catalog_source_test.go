package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
	}}

	got := catalogComponentSummaries(comps)
	if len(got) != 1 {
		t.Fatalf("want 1 summary, got %d", len(got))
	}
	c := got[0]
	if c.Name != "api" || c.Type != "worker" || c.Domain != "edge" {
		t.Errorf("scalar mapping wrong: %+v", c)
	}
	// Envs sorted by name; Profile = first non-empty in name order (dev=small).
	if len(c.Envs) != 2 || c.Envs[0] != "dev" || c.Envs[1] != "prod" {
		t.Errorf("Envs = %v, want [dev prod]", c.Envs)
	}
	if c.Profile != "small" {
		t.Errorf("Profile = %q, want small", c.Profile)
	}
	if len(c.DependsOn) != 1 || c.DependsOn[0] != "ns/repo/shared" {
		t.Errorf("DependsOn = %v", c.DependsOn)
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
