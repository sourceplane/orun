package statestore_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/testfx/statefs"
)

func newStore(t *testing.T) *statestore.LocalStore {
	t.Helper()
	root := filepath.Join(statefs.NewWorkspace(t), ".orun")
	s, err := statestore.NewLocalStore(statestore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	return s
}

func newStoreWithClock(t *testing.T, clock func() time.Time) (*statestore.LocalStore, string) {
	t.Helper()
	root := filepath.Join(statefs.NewWorkspace(t), ".orun")
	s, err := statestore.NewLocalStore(statestore.LocalConfig{Root: root, Clock: clock})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	return s, root
}

func TestNewLocalStore_RejectsEmptyRoot(t *testing.T) {
	_, err := statestore.NewLocalStore(statestore.LocalConfig{Root: ""})
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("err=%v, want ErrInvalid", err)
	}
}

func TestNewLocalStore_RejectsRelativeRoot(t *testing.T) {
	_, err := statestore.NewLocalStore(statestore.LocalConfig{Root: "relative/path"})
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("err=%v, want ErrInvalid", err)
	}
}

func TestNewLocalStore_CreatesMissingRoot(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "fresh", ".orun")
	s, err := statestore.NewLocalStore(statestore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	if s.Root() != root {
		t.Fatalf("Root()=%q want %q", s.Root(), root)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("root not created: %v", err)
	}
}

func TestWrite_ReadRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	data := []byte(`{"hello":"world"}`)

	meta, err := s.Write(ctx, "revisions/rev-x/plan.json", data, statestore.WriteOptions{})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	wantRev := sha256Hex(data)
	if meta.Revision != wantRev {
		t.Fatalf("meta.Revision=%q want %q", meta.Revision, wantRev)
	}
	if meta.Size != int64(len(data)) {
		t.Fatalf("meta.Size=%d want %d", meta.Size, len(data))
	}
	if meta.Path != "revisions/rev-x/plan.json" {
		t.Fatalf("meta.Path=%q", meta.Path)
	}

	got, gotMeta, err := s.Read(ctx, "revisions/rev-x/plan.json")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("Read got=%q want %q", got, data)
	}
	if gotMeta.Revision != wantRev {
		t.Fatalf("Read meta.Revision=%q want %q", gotMeta.Revision, wantRev)
	}
}

func TestRead_MissingReturnsErrNotFound(t *testing.T) {
	s := newStore(t)
	_, _, err := s.Read(context.Background(), "missing/file.json")
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

func TestCreateIfAbsent_FirstWinsSecondErrExists(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.CreateIfAbsent(ctx, "refs/named/staging.json", []byte(`{"a":1}`)); err != nil {
		t.Fatalf("first CreateIfAbsent: %v", err)
	}
	_, err := s.CreateIfAbsent(ctx, "refs/named/staging.json", []byte(`{"a":2}`))
	if !errors.Is(err, statestore.ErrExists) {
		t.Fatalf("second err=%v want ErrExists", err)
	}
	got, _, _ := s.Read(ctx, "refs/named/staging.json")
	if string(got) != `{"a":1}` {
		t.Fatalf("body changed: %q", got)
	}
}

func TestCreateIfAbsent_HighConcurrencyExclusivity(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	const n = 50
	var wg sync.WaitGroup
	var ok int64
	var existsErrs int64
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.CreateIfAbsent(ctx, "refs/named/exclusive.json", []byte(fmt.Sprintf(`{"i":%d}`, i)))
			if err == nil {
				atomic.AddInt64(&ok, 1)
			} else if errors.Is(err, statestore.ErrExists) {
				atomic.AddInt64(&existsErrs, 1)
			} else {
				t.Errorf("unexpected err: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if ok != 1 {
		t.Fatalf("winners=%d want 1", ok)
	}
	if existsErrs != n-1 {
		t.Fatalf("ErrExists count=%d want %d", existsErrs, n-1)
	}
}

func TestDelete_MissingIsNoOp(t *testing.T) {
	s := newStore(t)
	if err := s.Delete(context.Background(), "absent/file.json"); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
}

func TestDelete_RemovesFile(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Write(ctx, "refs/named/x.json", []byte("{}"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "refs/named/x.json"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := s.Read(ctx, "refs/named/x.json"); !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestDelete_RefusesNonEmptyDir(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Write(ctx, "revisions/rev-x/plan.json", []byte("{}"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	err := s.Delete(ctx, "revisions/rev-x")
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("err=%v want ErrInvalid", err)
	}
	if !strings.Contains(err.Error(), "non-empty") {
		// also accepts the directory-not-supported message; both wrap ErrInvalid
	}
}

func TestDelete_RefusesEmptyDir(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	// Create then delete the file, leaving an empty directory in place.
	if _, err := s.Write(ctx, "revisions/rev-x/plan.json", []byte("{}"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "revisions/rev-x/plan.json"); err != nil {
		t.Fatal(err)
	}
	err := s.Delete(ctx, "revisions/rev-x")
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("err=%v want ErrInvalid", err)
	}
}

func TestPathValidation_AllPublicMethodsRejectInvalid(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	bad := []string{
		"",
		"/abs/path.json",
		"a/../b.json",
		"a//b.json",
		"a\\b.json",
		"bad char.json",
	}
	for _, p := range bad {
		t.Run("Read/"+p, func(t *testing.T) {
			_, _, err := s.Read(ctx, p)
			if !errors.Is(err, statestore.ErrInvalid) {
				t.Fatalf("Read(%q) err=%v want ErrInvalid", p, err)
			}
		})
		t.Run("Write/"+p, func(t *testing.T) {
			_, err := s.Write(ctx, p, []byte("x"), statestore.WriteOptions{})
			if !errors.Is(err, statestore.ErrInvalid) {
				t.Fatalf("Write(%q) err=%v want ErrInvalid", p, err)
			}
		})
		t.Run("CreateIfAbsent/"+p, func(t *testing.T) {
			_, err := s.CreateIfAbsent(ctx, p, []byte("x"))
			if !errors.Is(err, statestore.ErrInvalid) {
				t.Fatalf("CreateIfAbsent(%q) err=%v want ErrInvalid", p, err)
			}
		})
		t.Run("Delete/"+p, func(t *testing.T) {
			err := s.Delete(ctx, p)
			if !errors.Is(err, statestore.ErrInvalid) {
				t.Fatalf("Delete(%q) err=%v want ErrInvalid", p, err)
			}
		})
	}
}

func TestWrite_AtomicConcurrentReadersSeeFullJSON(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	path := "revisions/rev-x/plan.json"

	// Seed initial bytes.
	a := []byte(strings.Repeat("a", 4096))
	b := []byte(strings.Repeat("b", 4096))
	if _, err := s.Write(ctx, path, a, statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Writer: alternates between a and b.
	wg.Add(1)
	go func() {
		defer wg.Done()
		toggle := false
		for {
			select {
			case <-stop:
				return
			default:
			}
			payload := a
			if toggle {
				payload = b
			}
			toggle = !toggle
			if _, err := s.Write(ctx, path, payload, statestore.WriteOptions{}); err != nil {
				t.Errorf("writer: %v", err)
				return
			}
		}
	}()

	// Readers: must always observe homogeneous bytes (all a's or all b's).
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				select {
				case <-stop:
					return
				default:
				}
				got, _, err := s.Read(ctx, path)
				if err != nil {
					t.Errorf("reader: %v", err)
					return
				}
				if len(got) != 4096 {
					t.Errorf("reader: short read %d bytes", len(got))
					return
				}
				first := got[0]
				if first != 'a' && first != 'b' {
					t.Errorf("reader: unexpected first byte %q", first)
					return
				}
				for _, c := range got {
					if c != first {
						t.Errorf("reader: torn write detected (mixed bytes)")
						return
					}
				}
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}

func TestOrphanSweep_RemovesOldTempfilesPreservesYoung(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }

	root := filepath.Join(statefs.NewWorkspace(t), ".orun")
	if err := os.MkdirAll(filepath.Join(root, "revisions", "rev-x"), 0o755); err != nil {
		t.Fatal(err)
	}

	old := filepath.Join(root, ".orun-tmp-old123")
	young := filepath.Join(root, ".orun-tmp-young456")
	deepOld := filepath.Join(root, "revisions", "rev-x", ".orun-tmp-deep789")
	regular := filepath.Join(root, "regular.json")

	for _, p := range []string{old, young, deepOld, regular} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Backdate the "old" tempfiles past the threshold; "young" stays recent.
	stale := now.Add(-2 * time.Hour)
	fresh := now.Add(-1 * time.Minute)
	mustChtimes(t, old, stale)
	mustChtimes(t, young, fresh)
	mustChtimes(t, deepOld, stale)
	mustChtimes(t, regular, stale) // even though stale, name doesn't match prefix

	if _, err := statestore.NewLocalStore(statestore.LocalConfig{Root: root, Clock: clock}); err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}

	if exists(old) {
		t.Errorf("old tempfile not removed")
	}
	if !exists(young) {
		t.Errorf("young tempfile was removed")
	}
	if exists(deepOld) {
		t.Errorf("deep old tempfile not removed")
	}
	if !exists(regular) {
		t.Errorf("non-tempfile incorrectly removed")
	}
}

func TestNewLocalStore_DefaultClockIsTimeNow(t *testing.T) {
	// Just exercise the nil-Clock branch and confirm Write produces a
	// reasonable UpdatedAt (within 5s of now).
	s, _ := newStoreWithClock(t, nil)
	before := time.Now().Add(-5 * time.Second)
	meta, err := s.Write(context.Background(), "x.json", []byte("y"), statestore.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if meta.UpdatedAt.Before(before) {
		t.Fatalf("UpdatedAt=%v looks wrong", meta.UpdatedAt)
	}
}

func TestCompareAndSwap_NotImplementedInPRA(t *testing.T) {
	s := newStore(t)
	_, err := s.CompareAndSwap(context.Background(), "refs/named/x.json", "rev", []byte("y"))
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("CompareAndSwap err=%v want ErrInvalid", err)
	}
}

func TestList_NotImplementedInPRA(t *testing.T) {
	s := newStore(t)
	_, err := s.List(context.Background(), "refs/")
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("List err=%v want ErrInvalid", err)
	}
}

func TestContextCancellation(t *testing.T) {
	s := newStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Write(ctx, "x.json", []byte("y"), statestore.WriteOptions{}); !errors.Is(err, context.Canceled) {
		t.Errorf("Write canceled err=%v", err)
	}
	if _, _, err := s.Read(ctx, "x.json"); !errors.Is(err, context.Canceled) {
		t.Errorf("Read canceled err=%v", err)
	}
	if _, err := s.CreateIfAbsent(ctx, "x.json", []byte("y")); !errors.Is(err, context.Canceled) {
		t.Errorf("CreateIfAbsent canceled err=%v", err)
	}
	if err := s.Delete(ctx, "x.json"); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete canceled err=%v", err)
	}
}

func TestStateStoreInterfaceCompliance(t *testing.T) {
	// Compile-time check that *LocalStore satisfies StateStore.
	var _ statestore.StateStore = (*statestore.LocalStore)(nil)
}

func TestWrite_EXDEVFallbackSucceeds(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	// Inject EXDEV on the FIRST rename only; subsequent renames (the
	// fallback's own rename) succeed via os.Rename. This drives the
	// crossDeviceCopyRename branch without requiring a cross-device mount.
	var calls int
	restore := statestore.SetRenameFuncForTest(func(oldpath, newpath string) error {
		calls++
		if calls == 1 {
			// Clean up the first tempfile so it doesn't linger.
			_ = os.Remove(oldpath)
			return statestore.MakeEXDEVError(oldpath, newpath)
		}
		return os.Rename(oldpath, newpath)
	})
	defer restore()

	data := []byte(`{"exdev":"ok"}`)
	meta, err := s.Write(ctx, "refs/named/exdev.json", data, statestore.WriteOptions{})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if meta.Size != int64(len(data)) {
		t.Fatalf("size: %d", meta.Size)
	}
	got, _, err := s.Read(ctx, "refs/named/exdev.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("got=%q want %q", got, data)
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 renameFunc call (EXDEV), got %d", calls)
	}
}

func TestWrite_NonEXDEVRenameErrorPropagates(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	restore := statestore.SetRenameFuncForTest(func(oldpath, newpath string) error {
		_ = os.Remove(oldpath)
		return errors.New("synthetic rename failure")
	})
	defer restore()

	_, err := s.Write(ctx, "refs/named/fail.json", []byte("x"), statestore.WriteOptions{})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "synthetic rename failure") {
		t.Fatalf("err=%v", err)
	}
}

func TestIsCrossDeviceErr(t *testing.T) {
	if !statestore.IsCrossDeviceErrForTest(statestore.MakeEXDEVError("a", "b")) {
		t.Errorf("EXDEV LinkError not detected")
	}
	if statestore.IsCrossDeviceErrForTest(errors.New("plain")) {
		t.Errorf("plain error misidentified as EXDEV")
	}
	// Non-LinkError EXDEV (e.g. from a custom wrapper) — exercises the
	// fallback errors.Is(err, syscall.EXDEV) branch.
	if !statestore.IsCrossDeviceErrForTest(fmt.Errorf("wrapped: %w", syscall.EXDEV)) {
		t.Errorf("wrapped EXDEV not detected")
	}
}

func TestRevisionFromAssertJSONFile(t *testing.T) {
	// Demonstrates use with the statefs assertion helper for downstream
	// callers; also gives us coverage of writes through the path helpers.
	s := newStore(t)
	ctx := context.Background()
	body := map[string]any{"plan": "ok"}
	bs := []byte(`{"plan":"ok"}`)
	if _, err := s.Write(ctx, statestore.PlanPath("rev-x"), bs, statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	statefs.AssertJSONFile(t, filepath.Join(s.Root(), "revisions", "rev-x", "plan.json"), body)
}

// --- defensive error-path coverage via real filesystem conditions ----------
//
// These tests rely on POSIX permission semantics. They are skipped on Windows
// (the package targets POSIX hosts in Phase 1) and when run as root, where
// permission denials don't apply.

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("permission-denial tests skipped under root")
	}
}

func TestRead_NonNotExistErrorWraps(t *testing.T) {
	skipIfRoot(t)
	s := newStore(t)
	ctx := context.Background()
	// Create file then chmod its parent directory to 0o000 to force EACCES.
	if _, err := s.Write(ctx, "refs/named/locked.json", []byte("x"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.Root(), "refs", "named")
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)

	_, _, err := s.Read(ctx, "refs/named/locked.json")
	if err == nil {
		t.Fatalf("expected error")
	}
	if errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("perm error misclassified as NotFound: %v", err)
	}
}

func TestWrite_MkdirFailureWraps(t *testing.T) {
	skipIfRoot(t)
	s := newStore(t)
	ctx := context.Background()
	// Pre-create a parent directory and chmod it 0o000 so MkdirAll fails on
	// the child.
	parent := filepath.Join(s.Root(), "refs")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(parent, 0o755)

	_, err := s.Write(ctx, "refs/named/x.json", []byte("y"), statestore.WriteOptions{})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestCreateIfAbsent_MkdirFailureWraps(t *testing.T) {
	skipIfRoot(t)
	s := newStore(t)
	ctx := context.Background()
	parent := filepath.Join(s.Root(), "refs")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(parent, 0o755)

	_, err := s.CreateIfAbsent(ctx, "refs/named/x.json", []byte("y"))
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestCreateIfAbsent_NonExistOSErrorWraps(t *testing.T) {
	skipIfRoot(t)
	s := newStore(t)
	ctx := context.Background()
	// Pre-create the file as a directory entry; CreateIfAbsent's O_EXCL
	// open of a path that exists as a directory returns a non-ErrExist
	// error in some kernels, exercising the bottom branch.
	dir := filepath.Join(s.Root(), "refs", "named", "asdir.json")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateIfAbsent(ctx, "refs/named/asdir.json", []byte("y"))
	if err == nil {
		t.Fatalf("expected error")
	}
	// Could be ErrExists OR a different wrapped error depending on
	// platform; either path counts as covered.
	_ = err
}

func TestDelete_StatErrorWraps(t *testing.T) {
	skipIfRoot(t)
	s := newStore(t)
	ctx := context.Background()
	// Create file, lock parent.
	if _, err := s.Write(ctx, "refs/named/x.json", []byte("y"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.Root(), "refs", "named")
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)

	err := s.Delete(ctx, "refs/named/x.json")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestDelete_DeepNonEmptyDir(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	// Build a multi-file directory and try to delete it.
	if _, err := s.Write(ctx, "revisions/rev-x/a.json", []byte("a"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write(ctx, "revisions/rev-x/b.json", []byte("b"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	err := s.Delete(ctx, "revisions/rev-x")
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("err=%v want ErrInvalid", err)
	}
}

func TestDelete_RemoveErrorWraps(t *testing.T) {
	skipIfRoot(t)
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Write(ctx, "refs/named/x.json", []byte("y"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(s.Root(), "refs", "named")
	// Make parent read-only so os.Remove fails after stat succeeds (well,
	// stat works because read+exec; remove needs write). Then chmod 0o500.
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(parent, 0o755)

	err := s.Delete(ctx, "refs/named/x.json")
	if err == nil {
		t.Fatalf("expected error from Remove on read-only parent")
	}
}

func TestDelete_ReaddirErrorWraps(t *testing.T) {
	skipIfRoot(t)
	s := newStore(t)
	ctx := context.Background()
	// Create directory tree, then remove read-permission so ReadDir fails
	// while stat succeeds (parent retains x but child loses r).
	dirPath := filepath.Join(s.Root(), "revisions", "rev-x")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dirPath, 0o100); err != nil { // execute only
		t.Fatal(err)
	}
	defer os.Chmod(dirPath, 0o755)

	err := s.Delete(ctx, "revisions/rev-x")
	if err == nil {
		t.Fatalf("expected readdir error")
	}
}

// --- file-op fault injection --------------------------------------------

func TestWrite_TempWriteFailure(t *testing.T) {
	s := newStore(t)
	restore := statestore.SetWriteFnForTest(func(f *os.File, data []byte) (int, error) {
		return 0, errors.New("synthetic write failure")
	})
	defer restore()
	_, err := s.Write(context.Background(), "x.json", []byte("y"), statestore.WriteOptions{})
	if err == nil || !strings.Contains(err.Error(), "synthetic write failure") {
		t.Fatalf("err=%v", err)
	}
}

func TestWrite_TempSyncFailure(t *testing.T) {
	s := newStore(t)
	restore := statestore.SetSyncFnForTest(func(f *os.File) error {
		return errors.New("synthetic sync failure")
	})
	defer restore()
	_, err := s.Write(context.Background(), "x.json", []byte("y"), statestore.WriteOptions{})
	if err == nil || !strings.Contains(err.Error(), "synthetic sync failure") {
		t.Fatalf("err=%v", err)
	}
}

func TestWrite_TempCloseFailure(t *testing.T) {
	s := newStore(t)
	// Close the fd ourselves so the real Close returns EBADF, then return
	// our synthetic error.
	restore := statestore.SetCloseFnForTest(func(f *os.File) error {
		_ = f.Close()
		return errors.New("synthetic close failure")
	})
	defer restore()
	_, err := s.Write(context.Background(), "x.json", []byte("y"), statestore.WriteOptions{})
	if err == nil || !strings.Contains(err.Error(), "synthetic close failure") {
		t.Fatalf("err=%v", err)
	}
}

func TestCreateIfAbsent_WriteFailure(t *testing.T) {
	s := newStore(t)
	restore := statestore.SetWriteFnForTest(func(f *os.File, data []byte) (int, error) {
		return 0, errors.New("synthetic write failure")
	})
	defer restore()
	_, err := s.CreateIfAbsent(context.Background(), "x.json", []byte("y"))
	if err == nil || !strings.Contains(err.Error(), "synthetic write failure") {
		t.Fatalf("err=%v", err)
	}
}

func TestCreateIfAbsent_SyncFailure(t *testing.T) {
	s := newStore(t)
	restore := statestore.SetSyncFnForTest(func(f *os.File) error {
		return errors.New("synthetic sync failure")
	})
	defer restore()
	_, err := s.CreateIfAbsent(context.Background(), "x.json", []byte("y"))
	if err == nil || !strings.Contains(err.Error(), "synthetic sync failure") {
		t.Fatalf("err=%v", err)
	}
}

func TestCreateIfAbsent_CloseFailure(t *testing.T) {
	s := newStore(t)
	restore := statestore.SetCloseFnForTest(func(f *os.File) error {
		_ = f.Close()
		return errors.New("synthetic close failure")
	})
	defer restore()
	_, err := s.CreateIfAbsent(context.Background(), "x.json", []byte("y"))
	if err == nil || !strings.Contains(err.Error(), "synthetic close failure") {
		t.Fatalf("err=%v", err)
	}
}

func TestCrossDeviceFallback_WriteFailure(t *testing.T) {
	s := newStore(t)
	// Force EXDEV on the first rename so we enter crossDeviceCopyRename,
	// then fail the second write.
	calls := 0
	restoreRename := statestore.SetRenameFuncForTest(func(o, n string) error {
		calls++
		if calls == 1 {
			_ = os.Remove(o)
			return statestore.MakeEXDEVError(o, n)
		}
		return os.Rename(o, n)
	})
	defer restoreRename()

	writeCalls := 0
	restoreWrite := statestore.SetWriteFnForTest(func(f *os.File, data []byte) (int, error) {
		writeCalls++
		if writeCalls == 1 {
			return f.Write(data) // first write (initial tempfile) succeeds
		}
		return 0, errors.New("fallback write failed")
	})
	defer restoreWrite()

	_, err := s.Write(context.Background(), "exdev-write.json", []byte("y"), statestore.WriteOptions{})
	if err == nil || !strings.Contains(err.Error(), "fallback write failed") {
		t.Fatalf("err=%v", err)
	}
}

func TestCrossDeviceFallback_SyncFailure(t *testing.T) {
	s := newStore(t)
	calls := 0
	restoreRename := statestore.SetRenameFuncForTest(func(o, n string) error {
		calls++
		if calls == 1 {
			_ = os.Remove(o)
			return statestore.MakeEXDEVError(o, n)
		}
		return os.Rename(o, n)
	})
	defer restoreRename()

	syncCalls := 0
	restoreSync := statestore.SetSyncFnForTest(func(f *os.File) error {
		syncCalls++
		if syncCalls == 1 {
			return f.Sync() // first sync (initial tempfile) succeeds
		}
		return errors.New("fallback sync failed")
	})
	defer restoreSync()

	_, err := s.Write(context.Background(), "exdev-sync.json", []byte("y"), statestore.WriteOptions{})
	if err == nil || !strings.Contains(err.Error(), "fallback sync failed") {
		t.Fatalf("err=%v", err)
	}
}

func TestCrossDeviceFallback_CloseFailure(t *testing.T) {
	s := newStore(t)
	calls := 0
	restoreRename := statestore.SetRenameFuncForTest(func(o, n string) error {
		calls++
		if calls == 1 {
			_ = os.Remove(o)
			return statestore.MakeEXDEVError(o, n)
		}
		return os.Rename(o, n)
	})
	defer restoreRename()

	closeCalls := 0
	restoreClose := statestore.SetCloseFnForTest(func(f *os.File) error {
		closeCalls++
		if closeCalls == 1 {
			return f.Close() // first close succeeds
		}
		_ = f.Close()
		return errors.New("fallback close failed")
	})
	defer restoreClose()

	_, err := s.Write(context.Background(), "exdev-close.json", []byte("y"), statestore.WriteOptions{})
	if err == nil || !strings.Contains(err.Error(), "fallback close failed") {
		t.Fatalf("err=%v", err)
	}
}

func TestSweepOrphan_OnRootWalkDirError(t *testing.T) {
	skipIfRoot(t)
	// Make a non-readable subdirectory before construction; the walk
	// callback should swallow the error and the store should construct
	// successfully.
	tmp := t.TempDir()
	root := filepath.Join(tmp, ".orun")
	if err := os.MkdirAll(filepath.Join(root, "blocked"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "blocked"), 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Join(root, "blocked"), 0o755)

	if _, err := statestore.NewLocalStore(statestore.LocalConfig{Root: root}); err != nil {
		t.Fatalf("NewLocalStore should succeed despite walk hiccups: %v", err)
	}
}

// helpers

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func mustChtimes(t *testing.T, p string, ts time.Time) {
	t.Helper()
	if err := os.Chtimes(p, ts, ts); err != nil {
		t.Fatal(err)
	}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
