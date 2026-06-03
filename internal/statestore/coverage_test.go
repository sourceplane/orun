package statestore_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/statestore"
)

// TestLegacyAndIndexPathHelpers covers the pure path builders used only by the
// executionstate legacy-fallback / revision-index scans.
func TestLegacyAndIndexPathHelpers(t *testing.T) {
	if got := statestore.LegacyExecutionDocPath("exec-1"); got != "executions/exec-1/execution.json" {
		t.Errorf("LegacyExecutionDocPath = %q", got)
	}
	if got := statestore.LegacyExecutionsRoot(); got != "executions" {
		t.Errorf("LegacyExecutionsRoot = %q", got)
	}
	if got := statestore.RevisionIndexDir(); got != "indexes/revisions" {
		t.Errorf("RevisionIndexDir = %q", got)
	}
}

// TestReadErrorPaths covers Read's cancelled-context, invalid-path, and
// non-ErrNotExist (read-a-directory) branches.
func TestReadErrorPaths(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := s.Read(cctx, "a.json"); !errors.Is(err, context.Canceled) {
		t.Errorf("Read(cancelled) err = %v; want context.Canceled", err)
	}

	if _, _, err := s.Read(ctx, ""); !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("Read(empty) err = %v; want ErrInvalid", err)
	}

	// A logical path that resolves to a directory: os.ReadFile returns a
	// non-ErrNotExist error, which Read must surface (not as ErrNotFound).
	if _, err := s.Write(ctx, "dir/inner.json", []byte("x"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, _, err := s.Read(ctx, "dir"); err == nil || errors.Is(err, statestore.ErrNotFound) {
		t.Errorf("Read(dir) err = %v; want a non-ErrNotFound error", err)
	}
}

// TestListBranches covers List's cancelled-context, invalid-prefix,
// absent-prefix (empty), single-file, and orphan-temp skip branches.
func TestListBranches(t *testing.T) {
	s, root := newStoreWithClock(t, time.Now)
	ctx := context.Background()

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.List(cctx, "x"); !errors.Is(err, context.Canceled) {
		t.Errorf("List(cancelled) err = %v; want context.Canceled", err)
	}

	if _, err := s.List(ctx, "/abs"); !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("List(invalid) err = %v; want ErrInvalid", err)
	}

	if got, err := s.List(ctx, "nope"); err != nil || len(got) != 0 {
		t.Errorf("List(absent) = %v, %v; want empty, nil", got, err)
	}

	// Prefix naming a single regular file returns exactly that entry.
	if _, err := s.Write(ctx, "f.json", []byte("hi"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := s.List(ctx, "f.json")
	if err != nil || len(got) != 1 || got[0].Path != "f.json" {
		t.Errorf("List(file) = %v, %v; want single f.json", got, err)
	}

	// Orphan tempfiles are skipped by the directory walk.
	if _, err := s.Write(ctx, "wd/real.json", []byte("r"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "wd", ".orun-tmp-junk"), []byte("j"), 0o644); err != nil {
		t.Fatalf("write orphan: %v", err)
	}
	got, err = s.List(ctx, "wd")
	if err != nil || len(got) != 1 || got[0].Path != "wd/real.json" {
		t.Errorf("List(wd) = %v, %v; want only wd/real.json (orphan skipped)", got, err)
	}
}

// TestDeleteBranches covers Delete's cancelled-context, invalid-path,
// absent (no-op), non-empty-directory, empty-directory, and success branches.
func TestDeleteBranches(t *testing.T) {
	s, root := newStoreWithClock(t, time.Now)
	ctx := context.Background()

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Delete(cctx, "x.json"); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete(cancelled) err = %v; want context.Canceled", err)
	}

	if err := s.Delete(ctx, ""); !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("Delete(empty) err = %v; want ErrInvalid", err)
	}

	if err := s.Delete(ctx, "absent.json"); err != nil {
		t.Errorf("Delete(absent) err = %v; want nil", err)
	}

	// A non-empty directory is refused.
	if _, err := s.Write(ctx, "d/a.json", []byte("x"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := s.Delete(ctx, "d"); !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("Delete(non-empty dir) err = %v; want ErrInvalid", err)
	}

	// An empty directory is also refused (structural, not caller-owned).
	if err := os.MkdirAll(filepath.Join(root, "emptydir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := s.Delete(ctx, "emptydir"); !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("Delete(empty dir) err = %v; want ErrInvalid", err)
	}

	// Deleting a regular file succeeds.
	if err := s.Delete(ctx, "d/a.json"); err != nil {
		t.Errorf("Delete(file) err = %v; want nil", err)
	}
}

// failingWrite/failingSync/failingClose are fault-injection primitives.
var errInjected = errors.New("injected failure")

// TestCreateIfAbsentErrorPaths covers CreateIfAbsent's mkdir, already-exists,
// and write/fsync/close failure branches.
func TestCreateIfAbsentErrorPaths(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	opts := statestore.WriteOptions{}

	// Already-exists: a second create on the same key returns ErrExists.
	if _, err := s.CreateIfAbsent(ctx, "cia/a.json", []byte("1")); err != nil {
		t.Fatalf("CreateIfAbsent: %v", err)
	}
	if _, err := s.CreateIfAbsent(ctx, "cia/a.json", []byte("2")); !errors.Is(err, statestore.ErrExists) {
		t.Errorf("CreateIfAbsent(dup) err = %v; want ErrExists", err)
	}

	// mkdir failure: a regular file blocks creating a child "directory".
	if _, err := s.Write(ctx, "file", []byte("x"), opts); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := s.CreateIfAbsent(ctx, "file/child.json", []byte("x")); err == nil {
		t.Error("CreateIfAbsent(under file) err = nil; want mkdir failure")
	}

	// write / fsync / close failures during the create.
	r := statestore.SetWriteFnForTest(func(*os.File, []byte) (int, error) { return 0, errInjected })
	_, werr := s.CreateIfAbsent(ctx, "cia/w.json", []byte("x"))
	r()
	if !errors.Is(werr, errInjected) {
		t.Errorf("CreateIfAbsent(write fail) err = %v", werr)
	}
	r = statestore.SetSyncFnForTest(func(*os.File) error { return errInjected })
	_, serr := s.CreateIfAbsent(ctx, "cia/s.json", []byte("x"))
	r()
	if !errors.Is(serr, errInjected) {
		t.Errorf("CreateIfAbsent(sync fail) err = %v", serr)
	}
	r = statestore.SetCloseFnForTest(func(*os.File) error { return errInjected })
	_, cerr := s.CreateIfAbsent(ctx, "cia/c.json", []byte("x"))
	r()
	if !errors.Is(cerr, errInjected) {
		t.Errorf("CreateIfAbsent(close fail) err = %v", cerr)
	}
}

// TestWriteAtomicErrorPaths covers writeAtomic's mkdir, write/fsync/close, and
// non-EXDEV rename failure branches via Write.
func TestWriteAtomicErrorPaths(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	opts := statestore.WriteOptions{}

	// mkdir failure (a file blocks the parent directory).
	if _, err := s.Write(ctx, "wf", []byte("x"), opts); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := s.Write(ctx, "wf/child.json", []byte("x"), opts); err == nil {
		t.Error("Write(under file) err = nil; want mkdir failure")
	}

	r := statestore.SetWriteFnForTest(func(*os.File, []byte) (int, error) { return 0, errInjected })
	_, werr := s.Write(ctx, "ok/w.json", []byte("x"), opts)
	r()
	if !errors.Is(werr, errInjected) {
		t.Errorf("Write(write fail) err = %v", werr)
	}
	r = statestore.SetSyncFnForTest(func(*os.File) error { return errInjected })
	_, serr := s.Write(ctx, "ok/s.json", []byte("x"), opts)
	r()
	if !errors.Is(serr, errInjected) {
		t.Errorf("Write(sync fail) err = %v", serr)
	}
	r = statestore.SetCloseFnForTest(func(*os.File) error { return errInjected })
	_, cerr := s.Write(ctx, "ok/c.json", []byte("x"), opts)
	r()
	if !errors.Is(cerr, errInjected) {
		t.Errorf("Write(close fail) err = %v", cerr)
	}

	// Non-EXDEV rename failure surfaces as an error.
	r = statestore.SetRenameFuncForTest(func(_, _ string) error { return errInjected })
	_, rerr := s.Write(ctx, "ok/r.json", []byte("x"), opts)
	r()
	if !errors.Is(rerr, errInjected) {
		t.Errorf("Write(rename fail) err = %v", rerr)
	}
}

// TestWriteCrossDeviceFallback covers the EXDEV path: writeAtomic's rename
// returns EXDEV, so crossDeviceCopyRename re-writes inside the destination dir.
func TestWriteCrossDeviceFallback(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	restore := statestore.SetRenameFuncForTest(func(old, newp string) error {
		return statestore.MakeEXDEVError(old, newp)
	})
	defer restore()

	if _, err := s.Write(ctx, "exdev/a.json", []byte("payload"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("Write(EXDEV) err = %v; want success via cross-device fallback", err)
	}
	got, _, err := s.Read(ctx, "exdev/a.json")
	if err != nil || string(got) != "payload" {
		t.Errorf("Read after EXDEV fallback = %q, %v; want \"payload\"", got, err)
	}
}

// TestListStatNotDir covers List's stat-error branch when a path component is a
// regular file (ENOTDIR, not ErrNotExist).
func TestListStatNotDir(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Write(ctx, "leaf.json", []byte("x"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := s.List(ctx, "leaf.json/under"); err == nil {
		t.Error("List(under file) err = nil; want a stat error")
	}
}

// TestDeleteInjectedErrors covers Delete's readdir-error and remove-error
// branches (and the remove ErrNotExist race) via the readDirFn/removeFn seams.
func TestDeleteInjectedErrors(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	// readdir failure while inspecting a directory.
	if _, err := s.Write(ctx, "rd/a.json", []byte("x"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	r := statestore.SetReadDirFnForTest(func(string) ([]os.DirEntry, error) { return nil, errInjected })
	derr := s.Delete(ctx, "rd")
	r()
	if !errors.Is(derr, errInjected) {
		t.Errorf("Delete(readdir fail) err = %v", derr)
	}

	// remove failure on a regular file surfaces the error.
	if _, err := s.Write(ctx, "rm/a.json", []byte("x"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	r = statestore.SetRemoveFnForTest(func(string) error { return errInjected })
	rerr := s.Delete(ctx, "rm/a.json")
	r()
	if !errors.Is(rerr, errInjected) {
		t.Errorf("Delete(remove fail) err = %v", rerr)
	}

	// remove racing with another deleter (ErrNotExist) is treated as success.
	r = statestore.SetRemoveFnForTest(func(string) error { return os.ErrNotExist })
	nerr := s.Delete(ctx, "rm/a.json")
	r()
	if nerr != nil {
		t.Errorf("Delete(remove ErrNotExist) err = %v; want nil", nerr)
	}
}

// TestNewLocalStoreMkdirFailure covers the root-mkdir failure branch: a root
// whose parent is a regular file cannot be created.
func TestNewLocalStoreMkdirFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(file, "sub")}); err == nil {
		t.Error("NewLocalStore(root under a file) err = nil; want mkdir failure")
	}
}

// TestNewLocalStoreRejectsRelativeRoot covers the non-absolute-root guard.
func TestNewLocalStoreRejectsRelativeRoot(t *testing.T) {
	if _, err := statestore.NewLocalStore(statestore.LocalConfig{Root: "relative/root"}); !errors.Is(err, statestore.ErrInvalid) {
		t.Errorf("NewLocalStore(relative) err = %v; want ErrInvalid", err)
	}
}

// TestListSingleOrphanTempfile covers List's single-file orphan-temp skip: a
// prefix resolving to one .orun-tmp-* file returns no entries.
func TestListSingleOrphanTempfile(t *testing.T) {
	s, root := newStoreWithClock(t, time.Now)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(root, ".orun-tmp-solo"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write orphan: %v", err)
	}
	got, err := s.List(ctx, ".orun-tmp-solo")
	if err != nil {
		t.Fatalf("List(orphan) err = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List(orphan) = %v; want empty (orphan temp skipped)", got)
	}
}

// TestReadStatFailure covers Read's post-read stat-error branch via the statFn
// seam (the file reads fine, but the follow-up stat fails).
func TestReadStatFailure(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Write(ctx, "rsf.json", []byte("x"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	r := statestore.SetStatFnForTest(func(string) (os.FileInfo, error) { return nil, errInjected })
	_, _, err := s.Read(ctx, "rsf.json")
	r()
	if !errors.Is(err, errInjected) {
		t.Errorf("Read(stat fail) err = %v", err)
	}
}
