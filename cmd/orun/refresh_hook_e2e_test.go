package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// TestMaybeAutoRefresh_E2E proves the universal refresh hook populates the
// object-model catalog as a side effect of running a catalog-using command, and
// that a second call within the TTL is debounced (the marker is not rewritten).
func TestMaybeAutoRefresh_E2E(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedGitCatalogWorkspace(t, dir)
	resetCatalogFlags(t)
	t.Setenv(autoRefreshEnvVar, "")

	// A catalog read subcommand triggers the hook.
	catalog := newCmd("catalog", nil)
	list := newCmd("list", catalog)
	list.SetContext(context.Background())

	maybeAutoRefresh(list)

	// The catalog is now readable from the object store.
	root := objectModelRoot(filepath.Join(dir, ".orun"))
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Writer: "test"})
	if err != nil {
		t.Fatalf("open refs: %v", err)
	}
	view, err := objcatalog.New(store, refs).Load(context.Background(), "catalogs/current")
	if err != nil {
		t.Fatalf("the hook should have written catalogs/current: %v", err)
	}
	if len(view.Components) == 0 {
		t.Fatal("expected the refreshed catalog to carry svc-a")
	}

	// The marker exists; a second call within the TTL is debounced (no rewrite).
	markerPath := filepath.Join(root, "cache", autoRefreshMarkerName)
	before, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	maybeAutoRefresh(list)
	after, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker (2): %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("a call within the TTL must be debounced (marker unchanged)")
	}
}

// TestMaybeAutoRefresh_DisabledByEnv proves the escape hatch fully skips the
// hook: no object-model catalog is written.
func TestMaybeAutoRefresh_DisabledByEnv(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedGitCatalogWorkspace(t, dir)
	resetCatalogFlags(t)
	t.Setenv(autoRefreshEnvVar, "1")

	catalog := newCmd("catalog", nil)
	list := newCmd("list", catalog)
	list.SetContext(context.Background())

	maybeAutoRefresh(list)

	if _, err := os.Stat(filepath.Join(dir, ".orun", "objectmodel")); !os.IsNotExist(err) {
		t.Fatalf("the escape hatch must skip the hook; stat err = %v", err)
	}
}
