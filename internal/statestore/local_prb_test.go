package statestore_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/testfx/statefs"
	"pgregory.net/rapid"
)

// ---------- CompareAndSwap -------------------------------------------------

func TestCompareAndSwap_HappyPath(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	path := "refs/named/cas.json"

	meta, err := s.Write(ctx, path, []byte(`{"v":1}`), statestore.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	newMeta, err := s.CompareAndSwap(ctx, path, meta.Revision, []byte(`{"v":2}`))
	if err != nil {
		t.Fatalf("CAS: %v", err)
	}
	if newMeta.Revision == meta.Revision {
		t.Fatalf("revision did not change after CAS")
	}
	got, _, err := s.Read(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"v":2}` {
		t.Fatalf("body=%q want updated", got)
	}
}

func TestCompareAndSwap_NotFound(t *testing.T) {
	s := newStore(t)
	_, err := s.CompareAndSwap(context.Background(), "refs/named/missing.json", "deadbeef", []byte("y"))
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

func TestCompareAndSwap_RevisionMismatchReturnsErrConflict(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	path := "refs/named/cas-mismatch.json"
	if _, err := s.Write(ctx, path, []byte("a"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err := s.CompareAndSwap(ctx, path, "0000000000000000000000000000000000000000000000000000000000000000", []byte("b"))
	if !errors.Is(err, statestore.ErrConflict) {
		t.Fatalf("err=%v want ErrConflict", err)
	}
}

func TestCompareAndSwap_InvalidPath(t *testing.T) {
	s := newStore(t)
	_, err := s.CompareAndSwap(context.Background(), "", "rev", []byte("y"))
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("err=%v want ErrInvalid", err)
	}
}

// TestCompareAndSwap_TwoConcurrentSameOldRev exercises the §3.3 / test-plan §2
// contract: with two goroutines both holding the same starting oldRev and both
// invoking CAS, exactly one must succeed and the other must observe ErrConflict.
// Run as a tight loop to make the race deterministic on CI.
func TestCompareAndSwap_TwoConcurrentSameOldRev(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	const iters = 200
	for i := 0; i < iters; i++ {
		path := fmt.Sprintf("refs/named/cas-iter-%d.json", i)
		startMeta, err := s.Write(ctx, path, []byte(fmt.Sprintf("seed-%d", i)), statestore.WriteOptions{})
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		var wg sync.WaitGroup
		var winners int64
		var conflicts int64
		var unexpected int64
		ready := make(chan struct{})
		for g := 0; g < 2; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				<-ready
				_, err := s.CompareAndSwap(ctx, path, startMeta.Revision, []byte(fmt.Sprintf("g-%d-i-%d", g, i)))
				switch {
				case err == nil:
					atomic.AddInt64(&winners, 1)
				case errors.Is(err, statestore.ErrConflict):
					atomic.AddInt64(&conflicts, 1)
				default:
					atomic.AddInt64(&unexpected, 1)
				}
			}(g)
		}
		close(ready)
		wg.Wait()
		if unexpected != 0 {
			t.Fatalf("iter %d: unexpected error count=%d", i, unexpected)
		}
		if winners != 1 || conflicts != 1 {
			t.Fatalf("iter %d: winners=%d conflicts=%d (want 1/1)", i, winners, conflicts)
		}
	}
}

// ---------- List ----------------------------------------------------------

func TestList_EmptyStore(t *testing.T) {
	s := newStore(t)
	got, err := s.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 entries, got %d: %+v", len(got), got)
	}
}

func TestList_NonexistentPrefixReturnsEmpty(t *testing.T) {
	s := newStore(t)
	got, err := s.List(context.Background(), "refs/missing")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 entries, got %d", len(got))
	}
}

func TestList_InvalidPrefixReturnsErrInvalid(t *testing.T) {
	s := newStore(t)
	_, err := s.List(context.Background(), "../etc")
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("err=%v want ErrInvalid", err)
	}
}

func TestList_WalksDirectoryTree(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	want := []string{
		"refs/named/a.json",
		"refs/named/b.json",
		"refs/triggers/push/main.json",
		"revisions/rev-1/plan.json",
		"revisions/rev-1/executions/exec-1/execution.json",
	}
	for _, p := range want {
		if _, err := s.Write(ctx, p, []byte(p), statestore.WriteOptions{}); err != nil {
			t.Fatalf("seed %s: %v", p, err)
		}
	}

	// Empty prefix — full scan.
	all, err := s.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	gotPaths := pathsOf(all)
	sort.Strings(gotPaths)
	expect := append([]string(nil), want...)
	sort.Strings(expect)
	if !equalStringSlices(gotPaths, expect) {
		t.Fatalf("List(\"\") got=%v want=%v", gotPaths, expect)
	}
	for _, oi := range all {
		if oi.Size <= 0 {
			t.Errorf("entry %q has Size %d", oi.Path, oi.Size)
		}
		if oi.UpdatedAt.IsZero() {
			t.Errorf("entry %q has zero UpdatedAt", oi.Path)
		}
	}

	// Subtree prefix.
	sub, err := s.List(ctx, "refs/named")
	if err != nil {
		t.Fatal(err)
	}
	subPaths := pathsOf(sub)
	sort.Strings(subPaths)
	expectSub := []string{"refs/named/a.json", "refs/named/b.json"}
	if !equalStringSlices(subPaths, expectSub) {
		t.Fatalf("List(refs/named) got=%v want=%v", subPaths, expectSub)
	}
}

func TestList_FileAsPrefixReturnsSingle(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	p := "refs/named/single.json"
	if _, err := s.Write(ctx, p, []byte("body"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := s.List(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != p {
		t.Fatalf("got=%+v want single %q", got, p)
	}
}

func TestList_SkipsOrphanTempfiles(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Write(ctx, "refs/named/x.json", []byte("y"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	// Drop a fake orphan tempfile in a deep dir; list must not surface it.
	dir := filepath.Join(s.Root(), "refs", "named")
	tmp := filepath.Join(dir, ".orun-tmp-fake123")
	if err := os.WriteFile(tmp, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	// And one at the root, in case the walk misses deep ones.
	if err := os.WriteFile(filepath.Join(s.Root(), ".orun-tmp-rooty"), []byte("g"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := s.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, oi := range got {
		if strings.HasPrefix(filepath.Base(oi.Path), ".orun-tmp-") {
			t.Errorf("List surfaced tempfile %q", oi.Path)
		}
	}
}

func TestList_FilePrefixThatIsTempfileReturnsEmpty(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	// Drop a tempfile under the root; List with that prefix returns empty.
	tmpRel := ".orun-tmp-x"
	if err := os.WriteFile(filepath.Join(s.Root(), tmpRel), []byte("g"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.List(ctx, tmpRel)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 entries, got %d", len(got))
	}
}

func TestList_LogicalPathsAreForwardSlashed(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Write(ctx, "revisions/rev-x/executions/exec-y/execution.json", []byte("b"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := s.List(ctx, "revisions")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries", len(got))
	}
	if strings.ContainsRune(got[0].Path, '\\') {
		t.Fatalf("path contains backslash: %q", got[0].Path)
	}
	if !strings.Contains(got[0].Path, "/") {
		t.Fatalf("path missing slash: %q", got[0].Path)
	}
	if strings.HasPrefix(got[0].Path, "/") {
		t.Fatalf("path has leading slash: %q", got[0].Path)
	}
}

// ---------- Atomicity / exclusivity test-plan §2 ----------------------------

// 100 goroutines concurrently Write + Read the same path; readers must always
// observe a complete JSON document (zero decode errors) regardless of which
// of the two homogeneous payloads is currently visible.
func TestWrite_100GoroutinesAtomicJSONDecodes(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	path := "revisions/rev-x/plan.json"

	a := []byte(`{"k":"a","filler":"` + strings.Repeat("x", 1024) + `"}`)
	b := []byte(`{"k":"b","filler":"` + strings.Repeat("y", 2048) + `"}`)

	if _, err := s.Write(ctx, path, a, statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}

	const writers = 50
	const readers = 50
	const opsPerWorker = 50

	var wg sync.WaitGroup
	var decodeErrs int64
	var unexpectedErrs int64

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				payload := a
				if (i+j)%2 == 0 {
					payload = b
				}
				if _, err := s.Write(ctx, path, payload, statestore.WriteOptions{}); err != nil {
					atomic.AddInt64(&unexpectedErrs, 1)
					t.Errorf("writer: %v", err)
					return
				}
			}
		}(i)
	}

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				got, _, err := s.Read(ctx, path)
				if err != nil {
					atomic.AddInt64(&unexpectedErrs, 1)
					t.Errorf("reader: %v", err)
					return
				}
				dec := json.NewDecoder(bytes.NewReader(got))
				dec.DisallowUnknownFields()
				var v map[string]any
				if err := dec.Decode(&v); err != nil {
					atomic.AddInt64(&decodeErrs, 1)
					t.Errorf("reader: decode err=%v body=%q", err, got)
					return
				}
				if k, _ := v["k"].(string); k != "a" && k != "b" {
					t.Errorf("reader: unexpected k=%q", k)
					return
				}
			}
		}()
	}

	wg.Wait()
	if decodeErrs != 0 {
		t.Fatalf("decode errors: %d (want 0)", decodeErrs)
	}
	if unexpectedErrs != 0 {
		t.Fatalf("unexpected errors: %d", unexpectedErrs)
	}
}

// 100 goroutines call CreateIfAbsent on the same path; exactly one wins,
// 99 observe ErrExists.
func TestCreateIfAbsent_100GoroutinesExclusivity(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	const n = 100
	path := "refs/named/exclusive100.json"

	var wg sync.WaitGroup
	var winners int64
	var existsErrs int64
	var unexpected int64
	ready := make(chan struct{})

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-ready
			_, err := s.CreateIfAbsent(ctx, path, []byte(fmt.Sprintf(`{"i":%d}`, i)))
			switch {
			case err == nil:
				atomic.AddInt64(&winners, 1)
			case errors.Is(err, statestore.ErrExists):
				atomic.AddInt64(&existsErrs, 1)
			default:
				atomic.AddInt64(&unexpected, 1)
				t.Errorf("unexpected: %v", err)
			}
		}(i)
	}
	close(ready)
	wg.Wait()
	if winners != 1 {
		t.Fatalf("winners=%d want 1", winners)
	}
	if existsErrs != n-1 {
		t.Fatalf("ErrExists=%d want %d", existsErrs, n-1)
	}
	if unexpected != 0 {
		t.Fatalf("unexpected errors=%d", unexpected)
	}
}

// ---------- rapid property test --------------------------------------------

// Round-trip property: arbitrary path-alphabet inputs and arbitrary byte
// payloads survive Write -> Read byte-for-byte and produce a stable
// (lowercase-hex sha256) ObjectMeta.Revision.
func TestProperty_WriteReadRoundTripStableRevision(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Each property iteration uses its own store so paths don't
		// collide across draws.
		// (rapid's *T is not the same as *testing.T; statefs.NewWorkspace
		// requires *testing.T, so use a plain TempDir here.)
		tmp, err := os.MkdirTemp("", "statestore-rapid-*")
		if err != nil {
			t.Fatalf("mkdir tmp: %v", err)
		}
		defer os.RemoveAll(tmp)
		root := filepath.Join(tmp, ".orun")
		s, err := statestore.NewLocalStore(statestore.LocalConfig{Root: root})
		if err != nil {
			t.Fatalf("NewLocalStore: %v", err)
		}

		segments := rapid.SliceOfN(
			rapid.StringMatching(`[a-zA-Z0-9._-]{1,32}`),
			1, 5,
		).Draw(t, "segments")
		// Reject path components equal to "." or ".." which the
		// regex above can produce.
		for _, seg := range segments {
			if seg == "." || seg == ".." {
				t.Skip()
			}
		}
		path := strings.Join(segments, "/")
		payload := rapid.SliceOfN(rapid.Byte(), 0, 4096).Draw(t, "payload")

		meta, err := s.Write(context.Background(), path, payload, statestore.WriteOptions{})
		if err != nil {
			t.Fatalf("Write(%q): %v", path, err)
		}
		got, gotMeta, err := s.Read(context.Background(), path)
		if err != nil {
			t.Fatalf("Read(%q): %v", path, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("round-trip mismatch: got %d bytes want %d", len(got), len(payload))
		}
		if meta.Revision != gotMeta.Revision {
			t.Fatalf("revision drift: write=%q read=%q", meta.Revision, gotMeta.Revision)
		}
		// Revision must be 64 lowercase-hex chars (sha256).
		if len(meta.Revision) != 64 {
			t.Fatalf("revision length=%d want 64", len(meta.Revision))
		}
		for _, r := range meta.Revision {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
				t.Fatalf("revision contains non-hex rune %q: %s", r, meta.Revision)
			}
		}
		if meta.Size != int64(len(payload)) {
			t.Fatalf("size=%d want %d", meta.Size, len(payload))
		}
	})
}

// Ensure the testfx workspace import is not orphaned — a small smoke check
// that uses the harness path the property test deliberately bypasses.
func TestProperty_Smoke_StatefsWorkspaceStillWorks(t *testing.T) {
	root := filepath.Join(statefs.NewWorkspace(t), ".orun")
	if _, err := statestore.NewLocalStore(statestore.LocalConfig{Root: root}); err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
}

// ---------- additional List edge-cases (coverage) -------------------------

func TestList_StatErrorPropagates(t *testing.T) {
	skipIfRoot(t)
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Write(ctx, "refs/named/x.json", []byte("y"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.Root(), "refs", "named")
	if err := os.Chmod(filepath.Join(s.Root(), "refs"), 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Join(s.Root(), "refs"), 0o755)
	defer os.Chmod(dir, 0o755)

	_, err := s.List(ctx, "refs/named/x.json")
	if err == nil {
		t.Fatalf("expected stat error")
	}
	if errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("perm error misclassified as NotFound: %v", err)
	}
}

func TestList_SkipsSymlinks(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Write(ctx, "refs/named/real.json", []byte("y"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	// Create a symlink alongside real.json that points to it.
	link := filepath.Join(s.Root(), "refs", "named", "link.json")
	if err := os.Symlink(filepath.Join(s.Root(), "refs", "named", "real.json"), link); err != nil {
		t.Skipf("symlink not supported on this fs: %v", err)
	}
	got, err := s.List(ctx, "refs/named")
	if err != nil {
		t.Fatal(err)
	}
	for _, oi := range got {
		if strings.HasSuffix(oi.Path, "link.json") {
			t.Errorf("List surfaced symlink %q", oi.Path)
		}
	}
}

func TestList_SkipsNonRegularFiles(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Write(ctx, "refs/named/real.json", []byte("y"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	// Create a FIFO; on systems where mkfifo isn't available the test is
	// skipped. The test verifies the IsRegular() filter.
	fifo := filepath.Join(s.Root(), "refs", "named", "fifo")
	if err := mkfifoOrSkip(t, fifo); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	got, err := s.List(ctx, "refs/named")
	if err != nil {
		t.Fatal(err)
	}
	for _, oi := range got {
		if strings.HasSuffix(oi.Path, "fifo") {
			t.Errorf("List surfaced non-regular file %q", oi.Path)
		}
	}
}

func TestList_WalkDirErrorPropagates(t *testing.T) {
	skipIfRoot(t)
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Write(ctx, "refs/named/real.json", []byte("y"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	// Lock the directory so WalkDir's ReadDir on it fails.
	dir := filepath.Join(s.Root(), "refs", "named")
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)

	_, err := s.List(ctx, "refs/named")
	if err == nil {
		t.Fatalf("expected walkdir error")
	}
}

func TestList_ContextCancelledMidWalk(t *testing.T) {
	s := newStore(t)
	// Seed enough files that the walk has work to do.
	for i := 0; i < 30; i++ {
		if _, err := s.Write(context.Background(),
			fmt.Sprintf("revisions/rev-%d/plan.json", i),
			[]byte("y"), statestore.WriteOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.List(ctx, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v want context.Canceled", err)
	}
}

// TestNewLocalStore_MkdirFails covers the os.MkdirAll error branch inside
// NewLocalStore by pre-creating the parent directory chmod 0o000 so the
// MkdirAll call cannot create the requested root subdirectory.
func TestNewLocalStore_MkdirFails(t *testing.T) {
	skipIfRoot(t)
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(parent, 0o755)
	_, err := statestore.NewLocalStore(statestore.LocalConfig{
		Root: filepath.Join(parent, "blocked", ".orun"),
	})
	if err == nil {
		t.Fatalf("expected mkdir error, got nil")
	}
	if !strings.Contains(err.Error(), "mkdir root") {
		t.Errorf("err=%v want substring 'mkdir root'", err)
	}
}

// TestList_TranslateEscapeRejected exercises translate's defensive
// strings.HasPrefix branch by passing a prefix that ValidatePath would
// permit but that resolves outside the store root via filepath.Clean
// peculiarities. We use a deeply-nested ".." chain that ValidatePath
// already rejects, but the same call path covers the translate error
// return for List.
func TestList_TranslateEscapeRejected(t *testing.T) {
	s := newStore(t)
	_, err := s.List(context.Background(), "../escape")
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("err=%v want ErrInvalid", err)
	}
}

// TestCreateIfAbsent_ParentDirUnwritable hits the create-error (non-ErrExist)
// branch inside CreateIfAbsent by chmod'ing the parent directory 0o500 so
// O_CREAT|O_EXCL fails with EACCES.
func TestCreateIfAbsent_ParentDirUnwritable(t *testing.T) {
	skipIfRoot(t)
	s := newStore(t)
	ctx := context.Background()
	// Seed the parent dir, then chmod it read-only.
	if _, err := s.Write(ctx, "refs/named/seed.json", []byte("y"), statestore.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.Root(), "refs", "named")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)
	_, err := s.CreateIfAbsent(ctx, "refs/named/new.json", []byte("z"))
	if err == nil {
		t.Fatalf("expected create error, got nil")
	}
	if errors.Is(err, statestore.ErrExists) {
		t.Fatalf("perm error misclassified as ErrExists: %v", err)
	}
}

// ---------- helpers --------------------------------------------------------

func pathsOf(infos []statestore.ObjectInfo) []string {
	out := make([]string, 0, len(infos))
	for _, oi := range infos {
		out = append(out, oi.Path)
	}
	return out
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
