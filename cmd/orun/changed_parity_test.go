package main

// changed_parity_test.go is the CS5/CS8 selection gate (test-plan.md §2,
// PG-1/PG-2). It originally compared the internal/affected engine against the
// legacy collectChangedComponents → ResolveComponentSet path; once CS8 locked
// parity, the legacy selector was retired (CS5 PR2) and the captured selections
// became the goldens here. It proves the engine selects the expected component
// set across include modes, multi-change, intent-impact watch/all/none, and
// nested component dirs.

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/sourceplane/orun/internal/affected"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/git"
	"github.com/sourceplane/orun/internal/nodes"
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
// The object catalog ingests these inline components, so the engine sees the
// full component set the goldens are expressed over.
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

// engineSelection resolves the workspace into the object catalog and runs the
// affected engine, returning the Selection as component *names* (mapped from the
// engine's component keys via the catalog).
func engineSelection(t *testing.T, root string, files []string, impact string) map[string]bool {
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
	res, err := affected.NewDetector(&ocView, affected.IntentImpact(impact)).
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

	cases := []struct {
		name   string
		files  []string
		impact string // intentImpact mode; "" ⇒ watch
		want   []string
	}{
		// --- component-input changes (impact irrelevant; default watch) ---
		// shared has no deps → selection is just shared (api is a *dependent*,
		// in the blast radius but not the job set).
		{"shared-input", []string{"libs/shared/main.go"}, "", []string{"shared"}},
		// api depends_on shared with include:always → shared is pulled in.
		{"api-input", []string{"apps/api/main.go"}, "", []string{"api", "shared"}},
		// web depends_on api with include:if-selected → api is NOT pulled.
		{"web-input", []string{"apps/web/main.go"}, "", []string{"web"}},
		// A deeply-nested file under a component dir maps to its owner by
		// longest-prefix on both paths (PG-2 nested dirs).
		{"web-nested", []string{"apps/web/src/components/card.tsx"}, "", []string{"web"}},
		// Two components changed at once.
		{"web+shared", []string{"apps/web/main.go", "libs/shared/main.go"}, "", []string{"shared", "web"}},
		{"no-change", []string{"README.md"}, "", nil},

		// --- intent.yaml changes (the previously-uncovered path) ---
		// In --files mode the diff is undiffable ⇒ both treat it as a global
		// intent change. Under watch, no component watches a (nil) section ⇒ none.
		{"intent-watch", []string{"intent.yaml"}, "watch", nil},
		// Under intent-impact=all, a global intent change selects everything.
		{"intent-all", []string{"intent.yaml"}, "all", []string{"api", "shared", "web"}},
		// Under intent-impact=none, a global intent change selects nothing extra.
		{"intent-none", []string{"intent.yaml"}, "none", nil},
		// Intent change + a component input, impact=all ⇒ still everything.
		{"intent-all+api", []string{"intent.yaml", "apps/api/main.go"}, "all", []string{"api", "shared", "web"}},
		// Intent change + a component input, impact=none ⇒ just the input's closure.
		{"intent-none+api", []string{"intent.yaml", "apps/api/main.go"}, "none", []string{"api", "shared"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			impact := tc.impact
			if impact == "" {
				impact = "watch"
			}
			engine := engineSelection(t, root, tc.files, impact)
			if !equalStringSets(engine, sliceToSet(tc.want)) {
				t.Fatalf("engine selection = %v, want %v", sortedSet(engine), tc.want)
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
