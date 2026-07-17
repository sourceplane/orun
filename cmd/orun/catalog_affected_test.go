package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// seedAffectedCatalog writes an object-model catalog (api depends_on shared, with
// an include:always edge) under the workspace's .orun/objectmodel and points
// catalogs/current at it.
func seedAffectedCatalog(t *testing.T, workspace string) {
	t.Helper()
	root := objectModelRoot(filepath.Join(workspace, ".orun"))
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Clock: clock.Fixed{}})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	manifests := []nodes.ComponentManifest{
		{Kind: nodes.KindComponentManifest, Identity: nodes.ComponentIdentity{
			ComponentKey: "ns/repo/api", Name: "api", Namespace: "ns", Repo: "repo", Path: "apps/api/component.yaml"}},
		{Kind: nodes.KindComponentManifest, Identity: nodes.ComponentIdentity{
			ComponentKey: "ns/repo/shared", Name: "shared", Namespace: "ns", Repo: "repo", Path: "libs/shared/component.yaml"}},
	}
	graphs := []nodes.CatalogGraph{{Kind: nodes.KindCatalogGraph, EdgeKind: "dependencies",
		Edges: []nodes.GraphEdge{{From: "ns/repo/api", To: "ns/repo/shared", Type: "depends_on", Include: "always"}}}}
	ownership := nodes.ImpactOwnership{
		Kind: nodes.KindImpactOwnership, SchemaVersion: 1,
		Components:          map[string]string{"apps/api": "ns/repo/api", "libs/shared": "ns/repo/shared"},
		GlobalPaths:         []string{"intent.yaml"},
		StructuralFilenames: []string{"component.yaml"},
		IgnoreDirs:          []string{".git"},
	}
	cat := nodes.CatalogSnapshot{Kind: nodes.KindCatalogSnapshot, SourceID: "sha256:" + repeatRune("a", 64), ResolverVersion: 1}
	id, err := nodes.AssembleCatalog(context.Background(), store, cat, manifests, graphs, ownership, nil)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if err := refs.Update(context.Background(), "catalogs/current", "", string(id)); err != nil {
		t.Fatalf("ref update: %v", err)
	}
}

func repeatRune(s string, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = s[0]
	}
	return string(b)
}

func runAffectedCmd(t *testing.T) catalogAffectedData {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	out := captureStdout(t, func() error { return runCatalogAffected(cmd, nil) })
	var env struct {
		Kind string              `json:"kind"`
		Data catalogAffectedData `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out)
	}
	if env.Kind != kindCatalogAffectedResult {
		t.Fatalf("kind = %q", env.Kind)
	}
	return env.Data
}

func TestCatalogAffected_ComponentFileChange(t *testing.T) {
	dir := withTempIntentRoot(t)
	resetCatalogFlags(t)
	seedAffectedCatalog(t, dir)

	prev := struct {
		base, head, impact string
		files              []string
		jsonOut            bool
	}{baseBranch, headRef, intentImpact, changedFiles, catalogAffectedJSON}
	t.Cleanup(func() {
		baseBranch, headRef, intentImpact, changedFiles, catalogAffectedJSON = prev.base, prev.head, prev.impact, prev.files, prev.jsonOut
	})
	baseBranch, headRef = "", ""
	intentImpact = "watch"
	changedFiles = []string{"libs/shared/main.go"} // bypasses git; shared changed
	catalogAffectedJSON = true

	d := runAffectedCmd(t)
	if len(d.DirectlyChanged) != 1 || d.DirectlyChanged[0] != "ns/repo/shared" {
		t.Errorf("directlyChanged = %v, want [ns/repo/shared]", d.DirectlyChanged)
	}
	// api depends_on shared → api is a dependent → affected.
	if len(d.Dependents) != 1 || d.Dependents[0] != "ns/repo/api" {
		t.Errorf("dependents = %v, want [ns/repo/api]", d.Dependents)
	}
	if d.Confidence != "high" || d.NeedsFullResolve {
		t.Errorf("confidence/needsFull = %s / %v", d.Confidence, d.NeedsFullResolve)
	}
	if d.CatalogID == "" {
		t.Errorf("catalogId empty")
	}
}

func TestCatalogAffected_StructuralChangeLowersConfidence(t *testing.T) {
	dir := withTempIntentRoot(t)
	resetCatalogFlags(t)
	seedAffectedCatalog(t, dir)

	prev := changedFiles
	prevJSON := catalogAffectedJSON
	t.Cleanup(func() { changedFiles = prev; catalogAffectedJSON = prevJSON })
	changedFiles = []string{"apps/api/component.yaml"} // structural
	catalogAffectedJSON = true
	intentImpact = "watch"
	baseBranch, headRef = "", ""

	d := runAffectedCmd(t)
	if d.Confidence != "low" || !d.NeedsFullResolve {
		t.Errorf("structural change must lower confidence: %s / %v", d.Confidence, d.NeedsFullResolve)
	}
	// api selects shared via the include:always edge.
	foundShared := false
	for _, c := range d.Selection {
		if c == "ns/repo/shared" {
			foundShared = true
		}
	}
	if !foundShared {
		t.Errorf("selection should pull include:always dep shared: %v", d.Selection)
	}
}

func TestCatalogAffected_NoCatalogExits6(t *testing.T) {
	withTempIntentRoot(t) // no catalog seeded
	resetCatalogFlags(t)
	changedFiles = nil
	catalogAffectedJSON = true
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	err := runCatalogAffected(cmd, nil)
	if err == nil {
		t.Fatal("expected error when no catalog present")
	}
	var ec interface{ ExitCode() int }
	if !asExit(err, &ec) || ec.ExitCode() != 6 {
		t.Fatalf("expected exit 6, got %v", err)
	}
}
