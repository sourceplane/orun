package objectstore

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests exercise the atomic-write error branches (temp / fsync / rename)
// via the package fault-injection seams. They are non-parallel: they mutate
// package-level seam vars and restore them, so the Go scheduler runs them
// before any t.Parallel() test in this package starts.

func faultStore(t *testing.T) *LocalStore {
	t.Helper()
	s, err := NewLocalStore(LocalConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	return s
}

// assertNoTempFiles fails if any tmp-* file leaked under root (the cleanup
// defer must remove the temp on a mid-write failure).
func assertNoTempFiles(t *testing.T, root string) {
	t.Helper()
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasPrefix(d.Name(), "tmp-") {
			t.Errorf("leftover temp file after failed write: %s", p)
		}
		return nil
	})
}

func TestWrite_TempCreateFailure(t *testing.T) {
	s := faultStore(t)
	orig := osCreateTemp
	osCreateTemp = func(string, string) (*os.File, error) { return nil, errors.New("inject: no temp") }
	defer func() { osCreateTemp = orig }()

	_, err := s.PutBlob(context.Background(), []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "temp file") {
		t.Fatalf("PutBlob err = %v, want a 'temp file' failure", err)
	}
}

func TestWrite_FsyncFailure_CleansTemp(t *testing.T) {
	s := faultStore(t)
	orig := fsyncFile
	fsyncFile = func(*os.File) error { return errors.New("inject: fsync") }
	defer func() { fsyncFile = orig }()

	_, err := s.PutTree(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "fsync") {
		t.Fatalf("PutTree err = %v, want an 'fsync' failure", err)
	}
	assertNoTempFiles(t, s.Root())
}

func TestWrite_RenameFailure_CleansTemp(t *testing.T) {
	s := faultStore(t)
	orig := osRename
	osRename = func(string, string) error { return errors.New("inject: rename") }
	defer func() { osRename = orig }()

	_, err := s.PutBlob(context.Background(), []byte("payload"))
	if err == nil || !strings.Contains(err.Error(), "rename") {
		t.Fatalf("PutBlob err = %v, want a 'rename' failure", err)
	}
	assertNoTempFiles(t, s.Root())
	// The object must NOT be present after a failed rename.
	if _, _, gerr := s.Get(context.Background(), mustBlobID(t, s, []byte("payload"))); gerr == nil {
		t.Error("object should be absent after a failed rename")
	}
}

// mustBlobID computes the id a payload would get without writing it.
func mustBlobID(t *testing.T, s *LocalStore, data []byte) ObjectID {
	t.Helper()
	_, id, err := computeBlobID(s.algo, data)
	if err != nil {
		t.Fatalf("computeBlobID: %v", err)
	}
	return id
}
