package services

// catalog_parity_test.go is the CS8 catalog parity guard (test-plan.md §5,
// CG-1/CG-2): it proves the cockpit's graph-path component summaries
// (freshCatalogComponents, served from the object-model catalog) equal the
// intent-path summaries (componentSummaries, served from the live intent
// loader) field-for-field over a multi-component workspace. This is the standing
// guard for the lossy-graph "dropped Path" class (S-6): if a resolved catalog
// ever drops a component identity field the cockpit renders, the two paths
// diverge and this test fails.

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/loader"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/normalize"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objplan"
	"github.com/sourceplane/orun/internal/sourcectx"
)

// cgParityIntent is a multi-component workspace exercising the fields CG-1/CG-2
// require: nested component dirs (apps/web/frontend), a real input file per dir
// (the inference candidate), dependency edges (api→shared, web→api), a
// multi-env subscription with profiles, distinct domains/types, and a
// change.watches list.
const cgParityIntent = `apiVersion: orun.io/v1alpha1
kind: Intent
metadata:
  name: cgparity
catalog:
  namespace: ns
environments:
  dev:
    selectors:
      components: [shared, api, web]
  prod:
    selectors:
      components: [shared, api, web]
components:
  - name: shared
    type: library
    domain: platform
    path: libs/shared
    subscribe:
      environments:
        - {name: dev, profile: fast}
        - {name: prod, profile: fast}
  - name: api
    type: worker
    domain: edge
    path: apps/api
    dependsOn:
      - component: shared
        include: always
    subscribe:
      environments:
        - {name: dev, profile: small}
        - {name: prod, profile: small}
    change:
      watches: [environments]
  - name: web
    type: worker
    domain: edge
    path: apps/web/frontend
    dependsOn:
      - component: api
        include: if-selected
    subscribe:
      environments:
        - {name: dev, profile: small}
        - {name: prod, profile: small}
`

func TestCatalogParity_GraphPathEqualsIntentPath(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir() // workspace root
	om := t.TempDir() // ObjectModelRoot; store lives at om/objectmodel

	write := func(rel, body string) {
		p := filepath.Join(ws, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("intent.yaml", cgParityIntent)
	// Real input files so each (nested) component dir resolves on disk.
	write("libs/shared/main.go", "package shared\n")
	write("apps/api/main.go", "package api\n")
	write("apps/web/frontend/main.go", "package web\n")

	// --- graph path: resolve → object-model catalog → freshCatalogComponents ---
	graph := buildFreshCatalogAndRead(t, ctx, ws, om)

	// --- intent path: live loader → componentSummaries ---
	intent, _, err := loader.LoadResolvedIntent(filepath.Join(ws, "intent.yaml"))
	if err != nil {
		t.Fatalf("load intent: %v", err)
	}
	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	intentSummaries := componentSummaries(intent, normalized)

	// Both paths must surface the same components.
	if len(graph) == 0 {
		t.Fatal("graph path returned no components")
	}
	gi := indexByName(graph)
	ii := indexByName(intentSummaries)
	if len(gi) != len(ii) {
		t.Fatalf("component count: graph=%d intent=%d", len(gi), len(ii))
	}

	for name, g := range gi {
		in, ok := ii[name]
		if !ok {
			t.Fatalf("graph component %q absent on the intent path", name)
		}
		assertComponentParity(t, name, g, in)
	}
}

// assertComponentParity compares the two summaries field-for-field. Scalars must
// match exactly (this is where a dropped Path/Domain/Profile would surface);
// the list fields are compared order-independently since each path sorts
// differently but must carry the same set.
func assertComponentParity(t *testing.T, name string, graph, intent ComponentSummary) {
	t.Helper()
	if graph.Type != intent.Type {
		t.Errorf("%s: Type graph=%q intent=%q", name, graph.Type, intent.Type)
	}
	if graph.Domain != intent.Domain {
		t.Errorf("%s: Domain graph=%q intent=%q", name, graph.Domain, intent.Domain)
	}
	if graph.Path != intent.Path {
		t.Errorf("%s: Path graph=%q intent=%q (the dropped-Path guard)", name, graph.Path, intent.Path)
	}
	if graph.Profile != intent.Profile {
		t.Errorf("%s: Profile graph=%q intent=%q", name, graph.Profile, intent.Profile)
	}
	if !equalSorted(graph.Envs, intent.Envs) {
		t.Errorf("%s: Envs graph=%v intent=%v", name, sortedCopy(graph.Envs), sortedCopy(intent.Envs))
	}
	if !equalSorted(graph.DependsOn, intent.DependsOn) {
		t.Errorf("%s: DependsOn graph=%v intent=%v", name, sortedCopy(graph.DependsOn), sortedCopy(intent.DependsOn))
	}
	if !equalSorted(graph.Watches, intent.Watches) {
		t.Errorf("%s: Watches graph=%v intent=%v", name, sortedCopy(graph.Watches), sortedCopy(intent.Watches))
	}
}

// buildFreshCatalogAndRead resolves the workspace into an object-model catalog
// whose SourceID matches the workspace's current source (so the freshness gate
// serves it) and returns the component summaries the cockpit reads.
func buildFreshCatalogAndRead(t *testing.T, ctx context.Context, ws, om string) []ComponentSummary {
	t.Helper()
	assembleFreshTestCatalog(t, ctx, ws, om)
	s := NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: om})
	comps, ok := s.freshCatalogComponents(ctx, ws)
	if !ok {
		t.Fatal("freshly-resolved catalog must be served by the gate (ok=false)")
	}
	return comps
}

// assembleFreshTestCatalog resolves ws into a catalog at catalogs/current in
// om's object model, stamped with the workspace's current SourceID.
func assembleFreshTestCatalog(t *testing.T, ctx context.Context, ws, om string) {
	t.Helper()
	inputs := catalogresolve.ResolverInputs{
		OrunVersion:       "0.0.0-test",
		SchemaVersion:     "orun.io/v1alpha1",
		ResolverVersion:   1,
		StackSources:      []string{},
		SourceSnapshotKey: "src-cgparity",
		CatalogInputHash:  "sha256:cafef00d",
		Repo:              "repo",
		SourceScope:       "branch-main",
		HeadRevision:      "abc",
		TreeHash:          "def",
		WorkingTree:       "clean",
		CreatedAt:         "2026-06-06T00:00:00Z",
	}
	view, _, err := catalogresolve.BuildCatalog(ctx, catalogresolve.Options{WorkspaceRoot: ws, Repo: "repo"}, inputs)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
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

	// Make the catalog fresh for ws: its SourceID must equal what catalogFresh
	// recomputes from the workspace.
	wsState, err := sourcectx.ResolveSourceSnapshot(ctx, sourcectx.ResolveOptions{WorkspacePath: ws})
	if err != nil {
		t.Fatalf("resolve source: %v", err)
	}
	srcNode := objplan.BuildSourceNode(wsState, sourcectx.BuildSourceSnapshotKey(wsState))
	srcID, err := nodes.SourceID(store.Algo(), srcNode)
	if err != nil {
		t.Fatalf("source id: %v", err)
	}

	cat, manifests, graphs, ownership, fps := objplan.BuildCatalogNodes(view, 1, nil, nil)
	cat.SourceID = string(srcID)
	id, err := nodes.AssembleCatalog(ctx, store, cat, manifests, graphs, ownership, fps)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if err := refs.Update(ctx, "catalogs/current", "", string(id)); err != nil {
		t.Fatalf("ref: %v", err)
	}
}

// --- helpers ---

func indexByName(comps []ComponentSummary) map[string]ComponentSummary {
	m := make(map[string]ComponentSummary, len(comps))
	for _, c := range comps {
		m[c.Name] = c
	}
	return m
}

func equalSorted(a, b []string) bool {
	return reflect.DeepEqual(sortedCopy(a), sortedCopy(b))
}

func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	return out
}
