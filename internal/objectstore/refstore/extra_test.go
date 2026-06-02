package refstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestListSkipsNonRefFiles(t *testing.T) {
	t.Parallel()
	rs := newStore(t)
	ctx := context.Background()
	if err := rs.Update(ctx, "real", "", id(1)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Litter the refs dir with files List must ignore.
	refsDir := filepath.Join(rs.root, "refs")
	for _, name := range []string{"real.json.lock", "tmp-stray", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(refsDir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("litter %s: %v", name, err)
		}
	}
	got, err := rs.List(ctx, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0] != "real" {
		t.Fatalf("List = %v, want [real]", got)
	}
}

func TestNewLocalRefStoreMkdirFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Block refs/ with a regular file.
	if err := os.WriteFile(filepath.Join(root, "refs"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := NewLocalRefStore(LocalConfig{Root: root}); err == nil {
		t.Fatalf("NewLocalRefStore succeeded despite blocked refs/ path")
	}
}

func TestWriteAtomicErrorBranches(t *testing.T) {
	t.Parallel()
	rs := newStore(t)
	// Rename failure: destination path is a non-empty directory.
	dest := rs.refPath("rf")
	if err := os.MkdirAll(filepath.Join(dest, "child"), 0o755); err != nil {
		t.Fatalf("mkdir dest dir: %v", err)
	}
	if err := rs.writeAtomic("rf", Ref{Kind: "Ref", Target: id(1)}); err == nil {
		t.Fatalf("writeAtomic over non-empty dir succeeded, want rename error")
	}
	// Mkdir failure: the parent dir is blocked by a regular file.
	if err := os.WriteFile(filepath.Join(rs.root, "refs", "blk"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	if err := rs.writeAtomic("blk/x", Ref{Kind: "Ref", Target: id(1)}); err == nil {
		t.Fatalf("writeAtomic with blocked parent succeeded, want mkdir error")
	}
}

func TestDeleteRemoveError(t *testing.T) {
	t.Parallel()
	rs := newStore(t)
	// A non-empty directory at the ref path makes os.Remove fail (not NotExist).
	d := rs.refPath("nd")
	if err := os.MkdirAll(filepath.Join(d, "child"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := rs.Delete(context.Background(), "nd"); err == nil {
		t.Fatalf("Delete of non-empty dir path succeeded, want error")
	}
}

func TestReadAndUpdateOnDirectoryPath(t *testing.T) {
	t.Parallel()
	rs := newStore(t)
	ctx := context.Background()
	// Make the ref's file path a directory so ReadFile fails with a
	// non-NotExist error, exercising read's error branch and Update's default.
	if err := os.MkdirAll(rs.refPath("weird"), 0o755); err != nil {
		t.Fatalf("mkdir ref-as-dir: %v", err)
	}
	if _, err := rs.Read(ctx, "weird"); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("Read(dir) = %v, want a non-NotFound error", err)
	}
	if err := rs.Update(ctx, "weird", "", id(1)); err == nil {
		t.Fatalf("Update(dir) succeeded, want error")
	}
}
