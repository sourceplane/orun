package main

// changed_parity_test.go is the CS5 parity gate: it proves the internal/affected
// engine selects exactly the same component set as the legacy --changed path
// (collectChangedComponents → ResolveComponentSet) over the same workspace and
// the same changed-files input. This must stay green before the live --changed
// path is switched onto the engine (specs/orun-catalog-state/implementation-plan.md
// CS5/CS8).

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/sourceplane/orun/internal/affected"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/expand"
	"github.com/sourceplane/orun/internal/git"
	"github.com/sourceplane/orun/internal/loader"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/normalize"
	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objplan"
)

// parityWorkspace writes an intent with three inline components wired by
// dependency edges of differing include modes:
//
//	api  --depends_on(include: always)-->      shared
//	web  --depends_on(include: if-selected)--> api
//
// Both the legacy normalize path and the object catalog ingest inline
// components, so the two paths see an identical component set.
func parityWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("intent.yaml", `apiVersion: orun.io/v1alpha1
kind: Intent
metadata:
  name: parity
catalog:
  namespace: ns
environments:
  dev:
    selectors:
      components: [api, shared, web]
components:
  - name: shared
    type: library
    path: libs/shared
  - name: api
    type: worker
    path: apps/api
    dependsOn:
      - component: shared
        include: always
  - name: web
    type: worker
    path: apps/web
    dependsOn:
      - component: api
        include: if-selected
`)
	// Real input files so each component dir fingerprints + exists on disk.
	write("libs/shared/main.go", "package shared\n")
	write("apps/api/main.go", "package api\n")
	write("apps/web/main.go", "package web\n")
	return root
}

// legacySelection runs the production --changed selection: collect changed
// components from the changed-files set, then grow by the include:always
// forward closure.
func legacySelection(t *testing.T, root string, files []string) map[string]bool {
	t.Helper()
	// The legacy path works in the git-diff frame (CWD-relative). Tests chdir
	// into the workspace (see TestChangedSelectionParity) so "intent.yaml" and
	// the changed files share that frame.
	intentFile := "intent.yaml"
	intent, _, err := loader.LoadResolvedIntent(intentFile)
	if err != nil {
		t.Fatalf("load intent: %v", err)
	}
	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	instances, err := expand.NewExpander(normalized).Expand()
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	changedSet := make(map[string]struct{}, len(files))
	for _, f := range files {
		changedSet[f] = struct{}{}
	}
	changed := collectChangedComponents(normalized, instances, changedSet, intentFile, git.ChangeOptions{Files: files})
	return expand.NewDependencyResolver(normalized).ResolveComponentSet(changed)
}

// engineSelection resolves the workspace into the object catalog and runs the
// affected engine, returning the Selection as component *names* (mapped from the
// engine's component keys via the catalog).
func engineSelection(t *testing.T, root string, files []string) map[string]bool {
	t.Helper()
	ctx := context.Background()

	inputs := catalogresolve.ResolverInputs{
		OrunVersion:       "0.0.0-test",
		SchemaVersion:     "orun.io/v1alpha1",
		ResolverVersion:   1,
		StackSources:      []string{},
		SourceSnapshotKey: "src-paritytest",
		CatalogInputHash:  "sha256:cafef00d",
		Repo:              "repo",
		SourceScope:       "branch-main",
		HeadRevision:      "abc",
		TreeHash:          "def",
		WorkingTree:       "clean",
		CreatedAt:         "2026-06-06T00:00:00Z",
	}
	view, _, err := catalogresolve.BuildCatalog(ctx, catalogresolve.Options{WorkspaceRoot: root, Repo: "repo"}, inputs)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}

	storeRoot := t.TempDir()
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: storeRoot})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: storeRoot, Clock: clock.Fixed{}})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	cat, manifests, graphs, ownership, fps := objplan.BuildCatalogNodes(view, 1)
	cat.SourceID = "sha256:" + repeat64('a')
	id, err := nodes.AssembleCatalog(ctx, store, cat, manifests, graphs, ownership, fps)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if err := refs.Update(ctx, "catalogs/current", "", string(id)); err != nil {
		t.Fatalf("ref: %v", err)
	}

	ocView, err := objcatalog.New(store, refs).Load(ctx, "catalogs/current")
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	res, err := affected.NewDetector(&ocView, affected.IntentImpactWatch).
		Detect(ctx, affected.GitChangeSource{Options: git.ChangeOptions{Files: files}, IntentPath: "intent.yaml"})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}

	keyToName := map[string]string{}
	for _, c := range ocView.Components {
		keyToName[c.ComponentKey] = c.Name
	}
	out := map[string]bool{}
	for _, k := range res.Selection {
		if name := keyToName[k]; name != "" {
			out[name] = true
		}
	}
	return out
}

func TestChangedSelectionParity(t *testing.T) {
	root := parityWorkspace(t)
	// Run in the workspace frame so the legacy path's CWD-relative file matching
	// and the engine's workspace-relative ownership map share one coordinate
	// system. Sequential (no t.Parallel), restored on cleanup.
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	cases := []struct {
		name  string
		files []string
		want  []string
	}{
		// shared has no deps → selection is just shared (api is a *dependent*,
		// in the blast radius but not the job set).
		{"shared-input", []string{"libs/shared/main.go"}, []string{"shared"}},
		// api depends_on shared with include:always → shared is pulled in.
		{"api-input", []string{"apps/api/main.go"}, []string{"api", "shared"}},
		// web depends_on api with include:if-selected → api is NOT pulled.
		{"web-input", []string{"apps/web/main.go"}, []string{"web"}},
		// Two components changed at once.
		{"web+shared", []string{"apps/web/main.go", "libs/shared/main.go"}, []string{"shared", "web"}},
		{"no-change", []string{"README.md"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			legacy := legacySelection(t, root, tc.files)
			engine := engineSelection(t, root, tc.files)
			if !equalStringSets(legacy, engine) {
				t.Fatalf("parity mismatch\n legacy: %v\n engine: %v", sortedSet(legacy), sortedSet(engine))
			}
			if !equalStringSets(legacy, sliceToSet(tc.want)) {
				t.Fatalf("legacy selection = %v, want %v", sortedSet(legacy), tc.want)
			}
		})
	}
}

// --- helpers ---

func equalStringSets(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func sliceToSet(s []string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, v := range s {
		m[v] = true
	}
	return m
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func repeat64(c byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
