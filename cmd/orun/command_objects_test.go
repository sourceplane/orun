package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

func objectsRig(t *testing.T) (*objectstore.LocalStore, *refstore.LocalRefStore, string, objectstore.ObjectID) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "objectmodel")
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Clock: clock.Fixed{}})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	revID, err := nodes.AssembleRevision(context.Background(), store,
		nodes.PlanRevision{Scope: nodes.RevisionScope{Mode: "full"}, JobCount: 2}, []byte(`{"plan":"A"}`))
	if err != nil {
		t.Fatalf("AssembleRevision: %v", err)
	}
	if err := refs.Update(context.Background(), "revisions/latest", "", string(revID)); err != nil {
		t.Fatalf("ref: %v", err)
	}
	return store, refs, root, revID
}

func TestRunObjectsRevParseCatLsTree(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, _, revID := objectsRig(t)

	var buf bytes.Buffer
	if err := runObjectsRevParse(ctx, refs, "revisions/latest", &buf); err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	if strings.TrimSpace(buf.String()) != string(revID) {
		t.Fatalf("rev-parse = %q, want %s", buf.String(), revID)
	}

	// cat of the tree shows its entries; cat of the revision.json blob shows the
	// pretty-printed record.
	entries, err := store.GetTree(ctx, revID)
	if err != nil {
		t.Fatalf("GetTree: %v", err)
	}
	var revBlob objectstore.ObjectID
	for _, e := range entries {
		if e.Name == "revision.json" {
			revBlob = e.ID
		}
	}
	buf.Reset()
	if err := runObjectsCat(ctx, store, refs, string(revBlob), &buf); err != nil {
		t.Fatalf("cat: %v", err)
	}
	if !strings.Contains(buf.String(), `"kind": "PlanRevision"`) {
		t.Fatalf("cat output = %s", buf.String())
	}

	buf.Reset()
	if err := runObjectsLsTree(ctx, store, refs, "revisions/latest", &buf); err != nil {
		t.Fatalf("ls-tree: %v", err)
	}
	if !strings.Contains(buf.String(), "revision.json") || !strings.Contains(buf.String(), "plan.json") {
		t.Fatalf("ls-tree output = %s", buf.String())
	}
}

func TestRunObjectsFsckHealthyAndCorrupt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, root, _ := objectsRig(t)

	var buf bytes.Buffer
	if err := runObjectsFsck(ctx, store, refs, &buf); err != nil {
		t.Fatalf("fsck healthy: %v", err)
	}
	if !strings.Contains(buf.String(), "healthy") {
		t.Fatalf("fsck output = %s", buf.String())
	}

	// Corrupt an object and expect a non-nil error.
	objFile := ""
	_ = filepath.Walk(filepath.Join(root, "objects"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && !strings.HasPrefix(filepath.Base(p), "tmp-") && objFile == "" {
			objFile = p
		}
		return nil
	})
	if err := os.WriteFile(objFile, []byte("garbage"), 0o644); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	buf.Reset()
	if err := runObjectsFsck(ctx, store, refs, &buf); err == nil {
		t.Fatalf("fsck should fail on corruption; output=%s", buf.String())
	}
}

func TestRunObjectsCheckoutAndErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, root, _ := objectsRig(t)

	var buf bytes.Buffer
	if err := runObjectsCheckout(ctx, store, refs, root, "revisions/latest", &buf); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	dest := filepath.Join(root, "current", "revisions-latest")
	if _, err := os.Stat(filepath.Join(dest, "revision.json")); err != nil {
		t.Fatalf("checkout did not materialize revision.json: %v", err)
	}

	// Unknown ref surfaces an error on each command.
	if err := runObjectsCat(ctx, store, refs, "nope/x", &buf); err == nil {
		t.Fatalf("cat(unknown) should error")
	}
	if err := runObjectsCheckout(ctx, store, refs, root, "nope/x", &buf); err == nil {
		t.Fatalf("checkout(unknown) should error")
	}
}

func TestSanitizeCheckoutName(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"revisions/latest": "revisions-latest", "a.b_c-1": "a.b_c-1", "": "object"} {
		if got := sanitizeCheckoutName(in); got != want {
			t.Fatalf("sanitizeCheckoutName(%q) = %q, want %q", in, got, want)
		}
	}
}
