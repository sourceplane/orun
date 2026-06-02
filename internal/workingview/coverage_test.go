package workingview

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

func mustID(s string) objectstore.ObjectID { return objectstore.ObjectID(s) }

// firstObjectFile returns the path of one stored object file under root/objects.
func firstObjectFile(t *testing.T, root string) string {
	t.Helper()
	var found string
	_ = filepath.WalkDir(filepath.Join(root, "objects"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !strings.HasPrefix(d.Name(), "tmp-") && found == "" {
			found = p
		}
		return nil
	})
	if found == "" {
		t.Fatal("no object file found")
	}
	return found
}

func TestFsckCorruptInsideClosure(t *testing.T) {
	t.Parallel()
	store, refs, root := rig(t)
	ctx := context.Background()
	revID := writeRevision(t, store) // tree + revision.json + plan.json
	if err := refs.Update(ctx, "revisions/latest", "", string(revID)); err != nil {
		t.Fatalf("ref: %v", err)
	}
	// Corrupt one object that participates in the ref's closure. The integrity
	// pass reports it; the ref walk hits ErrCorrupt and must not double-report.
	if err := os.WriteFile(firstObjectFile(t, root), []byte("garbage"), 0o644); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	problems, err := Fsck(ctx, store, refs)
	if err != nil {
		t.Fatalf("Fsck: %v", err)
	}
	corrupt := 0
	for _, p := range problems {
		if p.Kind == "corrupt" {
			corrupt++
		}
	}
	if corrupt != 1 {
		t.Fatalf("expected exactly one corrupt problem, got %v", problems)
	}
}

func TestMaterializeErrors(t *testing.T) {
	t.Parallel()
	store, _, root := rig(t)
	ctx := context.Background()
	// Absent root id → Get error.
	absent := "sha256:" + strings.Repeat("0", 64)
	if err := Materialize(ctx, store, mustID(absent), filepath.Join(root, "x")); err == nil {
		t.Fatalf("Materialize(absent) should error")
	}
	// Tree with a dangling child → recursion surfaces the missing object.
	fake := mustID("sha256:" + strings.Repeat("c", 64))
	tree, err := store.PutTree(ctx, []objectstore.TreeEntry{{Name: "child", Kind: objectstore.KindBlob, ID: fake}})
	if err != nil {
		t.Fatalf("PutTree: %v", err)
	}
	if err := Materialize(ctx, store, tree, filepath.Join(root, "danglingdir")); err == nil {
		t.Fatalf("Materialize(dangling tree) should error")
	}
}
