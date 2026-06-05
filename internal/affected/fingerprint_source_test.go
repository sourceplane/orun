package affected

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/objcatalog"
)

// fpFixture builds a temp workspace with two component dirs + an intent file,
// and a CatalogView whose stored fingerprints match that clean state.
func fpFixture(t *testing.T) (root string, cat *objcatalog.CatalogView) {
	t.Helper()
	root = t.TempDir()
	mustWrite(t, root, "intent.yaml", "catalog: {}\n")
	mustWrite(t, root, "apps/api/component.yaml", "name: api\n")
	mustWrite(t, root, "apps/api/package.json", `{"name":"api"}`)
	mustWrite(t, root, "libs/shared/component.yaml", "name: shared\n")

	gd := catalogresolve.ComputeGlobalDigest(filepath.Join(root, "intent.yaml"))
	store := func(key, dir string) objcatalog.FingerprintView {
		fp := catalogresolve.FingerprintForDir(root, dir, key, gd)
		return objcatalog.FingerprintView{ComponentKey: key, Dir: dir, Subtree: fp.Subtree, GlobalDigest: gd}
	}
	cat = &objcatalog.CatalogView{
		Components: []objcatalog.CatalogComponentView{
			{ComponentKey: "ns/repo/api", Name: "api", Path: "apps/api/component.yaml"},
			{ComponentKey: "ns/repo/shared", Name: "shared", Path: "libs/shared/component.yaml"},
		},
		Fingerprints: map[string]objcatalog.FingerprintView{
			"ns/repo/api":    store("ns/repo/api", "apps/api"),
			"ns/repo/shared": store("ns/repo/shared", "libs/shared"),
		},
	}
	return root, cat
}

func TestFingerprintSource_NoChange(t *testing.T) {
	root, cat := fpFixture(t)
	files, ic, err := FingerprintChangeSource{Catalog: cat, WorkspaceRoot: root}.ChangedPaths(context.Background())
	if err != nil {
		t.Fatalf("ChangedPaths: %v", err)
	}
	if len(files) != 0 || ic.Changed {
		t.Fatalf("clean workspace reported changes: files=%v intent=%v", files, ic.Changed)
	}
}

func TestFingerprintSource_ContentChange(t *testing.T) {
	root, cat := fpFixture(t)
	// Edit api's package.json — only apps/api should be reported.
	mustWrite(t, root, "apps/api/package.json", `{"name":"api","v":2}`)

	files, ic, err := FingerprintChangeSource{Catalog: cat, WorkspaceRoot: root}.ChangedPaths(context.Background())
	if err != nil {
		t.Fatalf("ChangedPaths: %v", err)
	}
	if ic.Changed {
		t.Errorf("content change should not flag intent")
	}
	if len(files) != 1 || files[0] != "apps/api" {
		t.Fatalf("expected only apps/api changed, got %v", files)
	}

	// Fed through the engine (with an ownership map), apps/api maps to its key.
	cat.Ownership = &objcatalog.OwnershipView{SchemaVersion: 1,
		Components: map[string]string{"apps/api": "ns/repo/api", "libs/shared": "ns/repo/shared"}}
	r := detect(t, cat, IntentImpactWatch, FingerprintChangeSource{Catalog: cat, WorkspaceRoot: root})
	eq(t, r.DirectlyChanged, []string{"ns/repo/api"}, "DirectlyChanged via fingerprint source")
}

func TestFingerprintSource_IntentChange(t *testing.T) {
	root, cat := fpFixture(t)
	// Change the intent file → global digest differs → intent flagged, but no
	// component's own subtree changes (each recomputed against its stored digest).
	mustWrite(t, root, "intent.yaml", "catalog:\n  defaults:\n    x: 1\n")

	files, ic, err := FingerprintChangeSource{Catalog: cat, WorkspaceRoot: root}.ChangedPaths(context.Background())
	if err != nil {
		t.Fatalf("ChangedPaths: %v", err)
	}
	if !ic.Changed {
		t.Errorf("intent change not detected via global digest")
	}
	if len(files) != 0 {
		t.Errorf("intent change must not mark components directly changed: %v", files)
	}
}

func TestFingerprintSource_NewComponentWithoutStored(t *testing.T) {
	root, cat := fpFixture(t)
	// A component with no stored fingerprint is treated as changed.
	delete(cat.Fingerprints, "ns/repo/shared")
	files, _, err := FingerprintChangeSource{Catalog: cat, WorkspaceRoot: root}.ChangedPaths(context.Background())
	if err != nil {
		t.Fatalf("ChangedPaths: %v", err)
	}
	if len(files) != 1 || files[0] != "libs/shared" {
		t.Fatalf("component without stored fingerprint should be changed: %v", files)
	}
}

func TestFingerprintSource_NilCatalog(t *testing.T) {
	files, ic, err := FingerprintChangeSource{}.ChangedPaths(context.Background())
	if err != nil || len(files) != 0 || ic.Changed {
		t.Fatalf("nil catalog should yield empty: %v %v %v", files, ic, err)
	}
}

func TestFingerprintSource_IntentAbs(t *testing.T) {
	s := FingerprintChangeSource{WorkspaceRoot: "/ws"}
	if got := s.intentAbs(); got != filepath.Join("/ws", "intent.yaml") {
		t.Errorf("default intentAbs = %q", got)
	}
	if got := (FingerprintChangeSource{WorkspaceRoot: "/ws", IntentPath: "sub/intent.yaml"}).intentAbs(); got != filepath.Join("/ws", "sub", "intent.yaml") {
		t.Errorf("relative intentAbs = %q", got)
	}
	abs := filepath.Join(os.TempDir(), "intent.yaml")
	if got := (FingerprintChangeSource{WorkspaceRoot: "/ws", IntentPath: abs}).intentAbs(); got != abs {
		t.Errorf("absolute intentAbs = %q", got)
	}
}
