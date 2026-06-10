package catalogread

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/affected"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// fixture seeds an object-model catalog (api depends_on shared) with fingerprints
// matching a temp workspace's clean state, and returns the Reader + workspace.
func fixture(t *testing.T) (*Reader, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()      // object store root
	ws := t.TempDir()        // workspace root
	writeFile(t, ws, "intent.yaml", "catalog: {}\n")
	writeFile(t, ws, "apps/api/component.yaml", "name: api\n")
	writeFile(t, ws, "libs/shared/component.yaml", "name: shared\n")

	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Clock: clock.Fixed{}})
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
			Type: "worker", Spec: map[string]any{"type": "worker", "domain": "edge"},
			Relations: []nodes.EntityRelation{
				{Type: "dependsOn", To: "ns/repo/shared", ToKind: "Component"},
				{Type: "partOf", To: "edge", ToKind: "Domain"},
			}},
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
	cat := nodes.CatalogSnapshot{Kind: nodes.KindCatalogSnapshot, SourceID: "sha256:" + rep("a", 64), ResolverVersion: 1}
	id, err := nodes.AssembleCatalog(ctx, store, cat, manifests, graphs, ownership,
		[]nodes.ComponentFingerprint{fp("ns/repo/api", "apps/api"), fp("ns/repo/shared", "libs/shared")})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if err := refs.Update(ctx, catalogCurrentRef, "", string(id)); err != nil {
		t.Fatalf("ref: %v", err)
	}
	return New(store, refs, ws), ws
}

func TestCatalogView_NoOverlay(t *testing.T) {
	r, _ := fixture(t)
	v, err := r.CatalogView(context.Background(), false)
	if err != nil {
		t.Fatalf("CatalogView: %v", err)
	}
	if v.Overlay {
		t.Errorf("overlay should be off")
	}
	if len(v.Components) != 2 {
		t.Fatalf("components = %d", len(v.Components))
	}
	if v.Components[0].Key != "ns/repo/api" || v.Components[0].Domain != "edge" {
		t.Errorf("api row = %+v", v.Components[0])
	}
}

func TestCatalogView_OverlayMarksEditedComponent(t *testing.T) {
	r, ws := fixture(t)
	// Edit shared's input → its fingerprint subtree changes.
	writeFile(t, ws, "libs/shared/component.yaml", "name: shared-edited\n")

	v, err := r.CatalogView(context.Background(), true)
	if err != nil {
		t.Fatalf("CatalogView: %v", err)
	}
	if !v.Overlay {
		t.Fatalf("overlay should be on")
	}
	byKey := map[string]bool{}
	for _, c := range v.Components {
		if c.DirectlyChanged {
			byKey[c.Key] = true
		}
	}
	if !byKey["ns/repo/shared"] {
		t.Errorf("edited component shared not marked changed: %+v", v.Components)
	}
	// api depends_on shared → api is affected (dependent).
	for _, c := range v.Components {
		if c.Key == "ns/repo/api" && !c.Dependent {
			t.Errorf("api should be a dependent: %+v", c)
		}
	}
}

func TestCatalogView_CleanWorkspaceNoChanges(t *testing.T) {
	r, _ := fixture(t)
	v, err := r.CatalogView(context.Background(), true)
	if err != nil {
		t.Fatalf("CatalogView: %v", err)
	}
	for _, c := range v.Components {
		if c.Changed() {
			t.Errorf("clean workspace: %s should not be changed", c.Key)
		}
	}
}

func TestComponentView(t *testing.T) {
	r, _ := fixture(t)
	cv, ok, err := r.ComponentView(context.Background(), "ns/repo/api")
	if err != nil || !ok {
		t.Fatalf("ComponentView: ok=%v err=%v", ok, err)
	}
	if cv.Name != "api" || cv.Type != "worker" || cv.Domain != "edge" {
		t.Errorf("component view = %+v", cv)
	}
	// Unknown key → not found, no error.
	if _, ok, err := r.ComponentView(context.Background(), "ns/repo/nope"); ok || err != nil {
		t.Errorf("unknown component: ok=%v err=%v", ok, err)
	}
}

func TestCatalogView_MissingCatalogErrors(t *testing.T) {
	root := t.TempDir()
	store, _ := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	refs, _ := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Clock: clock.Fixed{}})
	if _, err := New(store, refs, root).CatalogView(context.Background(), false); err == nil {
		t.Fatal("expected error when no catalog present")
	}
}

func TestWithPolicy(t *testing.T) {
	r, _ := fixture(t)
	if r.WithPolicy(affected.IntentImpactAll).policy != affected.IntentImpactAll {
		t.Errorf("WithPolicy did not apply")
	}
}

// --- helpers ---

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rep(s string, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = s[0]
	}
	return string(b)
}
