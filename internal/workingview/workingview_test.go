package workingview

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

func rig(t *testing.T) (*objectstore.LocalStore, *refstore.LocalRefStore, string) {
	t.Helper()
	root := t.TempDir()
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Clock: clock.Fixed{}})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	return store, refs, root
}

// writeRevision assembles a revision tree and returns its id.
func writeRevision(t *testing.T, store objectstore.ObjectStore) objectstore.ObjectID {
	t.Helper()
	id, err := nodes.AssembleRevision(context.Background(), store,
		nodes.PlanRevision{Scope: nodes.RevisionScope{Mode: "full"}, JobCount: 2}, []byte(`{"plan":"A"}`))
	if err != nil {
		t.Fatalf("AssembleRevision: %v", err)
	}
	return id
}

func TestFsckHealthy(t *testing.T) {
	t.Parallel()
	store, refs, _ := rig(t)
	ctx := context.Background()
	revID := writeRevision(t, store)
	if err := refs.Update(ctx, "revisions/latest", "", string(revID)); err != nil {
		t.Fatalf("ref: %v", err)
	}
	problems, err := Fsck(ctx, store, refs)
	if err != nil {
		t.Fatalf("Fsck: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("healthy store has problems: %v", problems)
	}
}

func TestFsckRefMissing(t *testing.T) {
	t.Parallel()
	store, refs, _ := rig(t)
	ctx := context.Background()
	absent := "sha256:" + strings.Repeat("0", 64)
	if err := refs.Update(ctx, "revisions/latest", "", absent); err != nil {
		t.Fatalf("ref: %v", err)
	}
	problems, err := Fsck(ctx, store, refs)
	if err != nil {
		t.Fatalf("Fsck: %v", err)
	}
	if len(problems) != 1 || problems[0].Kind != "ref-missing" {
		t.Fatalf("problems = %v", problems)
	}
	_ = problems[0].String()
}

func TestFsckDangling(t *testing.T) {
	t.Parallel()
	store, refs, _ := rig(t)
	ctx := context.Background()
	fake := objectstore.ObjectID("sha256:" + strings.Repeat("b", 64))
	tree, err := store.PutTree(ctx, []objectstore.TreeEntry{{Name: "child", Kind: objectstore.KindBlob, ID: fake}})
	if err != nil {
		t.Fatalf("PutTree: %v", err)
	}
	if err := refs.Update(ctx, "x", "", string(tree)); err != nil {
		t.Fatalf("ref: %v", err)
	}
	problems, err := Fsck(ctx, store, refs)
	if err != nil {
		t.Fatalf("Fsck: %v", err)
	}
	if len(problems) != 1 || problems[0].Kind != "dangling" {
		t.Fatalf("problems = %v", problems)
	}
}

func TestFsckCorrupt(t *testing.T) {
	t.Parallel()
	store, refs, root := rig(t)
	ctx := context.Background()
	if _, err := store.PutBlob(ctx, []byte("real content")); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	// Overwrite the single object file with non-zstd garbage.
	objDir := filepath.Join(root, "objects")
	var objFile string
	_ = filepath.WalkDir(objDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !strings.HasPrefix(d.Name(), "tmp-") {
			objFile = p
		}
		return nil
	})
	if objFile == "" {
		t.Fatal("no object file found")
	}
	if err := os.WriteFile(objFile, []byte("garbage"), 0o644); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	problems, err := Fsck(ctx, store, refs)
	if err != nil {
		t.Fatalf("Fsck: %v", err)
	}
	if len(problems) != 1 || problems[0].Kind != "corrupt" {
		t.Fatalf("problems = %v", problems)
	}
}

func TestResolveRef(t *testing.T) {
	t.Parallel()
	store, refs, _ := rig(t)
	ctx := context.Background()
	revID := writeRevision(t, store)
	if err := refs.Update(ctx, "revisions/latest", "", string(revID)); err != nil {
		t.Fatalf("ref: %v", err)
	}
	// Literal id passes through.
	if got, err := ResolveRef(ctx, refs, string(revID)); err != nil || got != revID {
		t.Fatalf("ResolveRef(id) = %s,%v", got, err)
	}
	// Ref name resolves.
	if got, err := ResolveRef(ctx, refs, "revisions/latest"); err != nil || got != revID {
		t.Fatalf("ResolveRef(name) = %s,%v", got, err)
	}
	// Unknown name is ErrNotFound.
	if _, err := ResolveRef(ctx, refs, "nope/missing"); !errors.Is(err, refstore.ErrNotFound) {
		t.Fatalf("ResolveRef(unknown) = %v", err)
	}
}

func TestCatObjectAndLsTree(t *testing.T) {
	t.Parallel()
	store, _, _ := rig(t)
	ctx := context.Background()
	revID := writeRevision(t, store)
	// Tree cat returns the canonical tree body; ls-tree lists entries.
	entries, err := LsTree(ctx, store, revID)
	if err != nil || len(entries) != 2 {
		t.Fatalf("LsTree = %v, %v", entries, err)
	}
	// Cat the revision.json blob → pretty JSON.
	var revBlob objectstore.ObjectID
	for _, e := range entries {
		if e.Name == "revision.json" {
			revBlob = e.ID
		}
	}
	out, err := CatObject(ctx, store, revBlob)
	if err != nil {
		t.Fatalf("CatObject: %v", err)
	}
	if !strings.Contains(string(out), "\n  \"kind\": \"PlanRevision\"") {
		t.Fatalf("CatObject not pretty-printed: %s", out)
	}
	// Cat a tree returns its raw body without error.
	if _, err := CatObject(ctx, store, revID); err != nil {
		t.Fatalf("CatObject(tree): %v", err)
	}
	// Cat a non-JSON blob returns raw bytes.
	rawID, _ := store.PutBlob(ctx, []byte("not json at all"))
	if out, _ := CatObject(ctx, store, rawID); string(out) != "not json at all" {
		t.Fatalf("CatObject(raw) = %q", out)
	}
}

func TestMaterialize(t *testing.T) {
	t.Parallel()
	store, _, root := rig(t)
	ctx := context.Background()
	revID := writeRevision(t, store)
	dest := filepath.Join(root, "current", "revision")
	if err := Materialize(ctx, store, revID, dest); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	revBytes, err := os.ReadFile(filepath.Join(dest, "revision.json"))
	if err != nil {
		t.Fatalf("read materialized revision.json: %v", err)
	}
	if !strings.Contains(string(revBytes), "\n  \"planHash\"") {
		t.Fatalf("materialized revision.json not pretty: %s", revBytes)
	}
	if _, err := os.Stat(filepath.Join(dest, "plan.json")); err != nil {
		t.Fatalf("plan.json not materialized: %v", err)
	}
	// Materializing a single blob writes a file.
	rawID, _ := store.PutBlob(ctx, []byte("hello"))
	blobDest := filepath.Join(root, "current", "blob.txt")
	if err := Materialize(ctx, store, rawID, blobDest); err != nil {
		t.Fatalf("Materialize(blob): %v", err)
	}
	if b, _ := os.ReadFile(blobDest); string(b) != "hello" {
		t.Fatalf("materialized blob = %q", b)
	}
}
