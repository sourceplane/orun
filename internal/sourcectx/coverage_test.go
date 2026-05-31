package sourcectx_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sourceplane/orun/internal/sourcectx"
)

// ------------------------------------------------------------------
// Defaults — Clock.Now and FixedClock.
// ------------------------------------------------------------------

func TestDefaultClock_Now(t *testing.T) {
	clk := sourcectx.DefaultClock()
	a := clk.Now()
	b := clk.Now()
	if !b.After(a) && !b.Equal(a) {
		t.Errorf("DefaultClock.Now() not monotonic-ish: %v then %v", a, b)
	}
	if a.Location().String() != "UTC" {
		t.Errorf("DefaultClock.Now() location = %q; want UTC", a.Location())
	}
}

func TestFixedClock_ReturnsConstant(t *testing.T) {
	want := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	clk := sourcectx.FixedClock{T: want}
	if got := clk.Now(); !got.Equal(want) {
		t.Errorf("FixedClock.Now() = %v; want %v", got, want)
	}
}

// ------------------------------------------------------------------
// Default filesystem — Walk + Stat + ReadFile against an actual tmpdir.
// ------------------------------------------------------------------

func TestDefaultFilesystem_WalkStatRead(t *testing.T) {
	root := t.TempDir()
	if err := writeFileWithDir(filepath.Join(root, "intent.yaml"), []byte("namespace: x\n")); err != nil {
		t.Fatal(err)
	}
	if err := writeFileWithDir(filepath.Join(root, "a", "component.yaml"), []byte("metadata: { name: a }\n")); err != nil {
		t.Fatal(err)
	}
	// Pruned dir — must be skipped during Walk.
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileWithDir(filepath.Join(root, "node_modules", "x", "noise.yaml"), []byte("x\n")); err != nil {
		t.Fatal(err)
	}
	// Pruned dir — .git should also be skipped.
	if err := os.MkdirAll(filepath.Join(root, ".git", "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileWithDir(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/heads/main\n")); err != nil {
		t.Fatal(err)
	}

	fsAdapter := sourcectx.DefaultFilesystem(root)
	visited := map[string]bool{}
	if err := fsAdapter.Walk(root, func(rel string, d fs.DirEntry) error {
		visited[rel] = true
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !visited["intent.yaml"] || !visited["a/component.yaml"] {
		t.Errorf("Walk missed expected files: %v", keysOf(visited))
	}
	for v := range visited {
		if strings.HasPrefix(v, "node_modules") || strings.HasPrefix(v, ".git") {
			t.Errorf("Walk did not prune %q", v)
		}
	}

	// Stat / ReadFile via relative path.
	info, err := fsAdapter.Stat("intent.yaml")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.IsDir() {
		t.Error("Stat returned dir for intent.yaml")
	}
	body, err := fsAdapter.ReadFile("intent.yaml")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(body), "namespace") {
		t.Errorf("ReadFile body = %q", string(body))
	}

	// ReadFile via absolute path.
	abs, err := fsAdapter.ReadFile(filepath.Join(root, "a", "component.yaml"))
	if err != nil {
		t.Fatalf("ReadFile abs: %v", err)
	}
	if !strings.Contains(string(abs), "name: a") {
		t.Errorf("abs ReadFile body = %q", string(abs))
	}

	// ReadFile of a missing file produces an fs.ErrNotExist-shaped error
	// — used by populateDirty's race-skip branch.
	if _, err := fsAdapter.ReadFile("does-not-exist.yaml"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ReadFile missing: errors.Is(err, fs.ErrNotExist) = false; err=%v", err)
	}
}

// keysOf is a tiny test helper for diagnostic output.
func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ------------------------------------------------------------------
// CIEventNoMatchError — Error() rendering covers all branches.
// ------------------------------------------------------------------

func TestCIEventNoMatchError_ErrorRendering(t *testing.T) {
	cases := []struct {
		err  *sourcectx.CIEventNoMatchError
		want []string
	}{
		{&sourcectx.CIEventNoMatchError{Reason: "no-git-repo", Provider: "github", Event: "pull_request", Action: "synchronize"},
			[]string{"reason=no-git-repo", "provider=github", "event=pull_request", "action=synchronize"}},
		{&sourcectx.CIEventNoMatchError{Reason: "no-head-revision"},
			[]string{"reason=no-head-revision"}},
		{&sourcectx.CIEventNoMatchError{},
			[]string{"sourcectx: CI event no-match"}},
	}
	for _, tc := range cases {
		s := tc.err.Error()
		for _, sub := range tc.want {
			if !strings.Contains(s, sub) {
				t.Errorf("Error() = %q; missing substring %q", s, sub)
			}
		}
		if !errors.Is(tc.err, sourcectx.ErrCIEventNoMatch) {
			t.Errorf("errors.Is(typed, ErrCIEventNoMatch) = false")
		}
	}
}

// ------------------------------------------------------------------
// Resolver: missing WorkspacePath.
// ------------------------------------------------------------------

func TestResolveSourceSnapshot_MissingWorkspacePath(t *testing.T) {
	_, err := sourcectx.ResolveSourceSnapshot(context.Background(), sourcectx.ResolveOptions{})
	if err == nil || !strings.Contains(err.Error(), "WorkspacePath") {
		t.Errorf("expected WorkspacePath error, got %v", err)
	}
}

// ------------------------------------------------------------------
// Resolver: dirty probe gracefully skips files removed between probe and
// read (covers the isNotExist branch in populateDirty).
// ------------------------------------------------------------------

func TestResolveSourceSnapshot_DirtyProbeSkipsMissingFile(t *testing.T) {
	memfs := memFS{m: fstest.MapFS{
		"intent.yaml": &fstest.MapFile{Data: []byte("namespace: x\n")},
	}}
	git := &fakeGit{
		hasRepo: true,
		head:    "abc12345def0abcdef01",
		tree:    "9aa7710abcdef01",
		branch:  "feature/x",
		// Diff claims a component.yaml that the FS does not have — must
		// not error out the resolver.
		diff: []string{"intent.yaml", "missing/component.yaml"},
	}
	state, err := sourcectx.ResolveSourceSnapshot(context.Background(), sourcectx.ResolveOptions{
		WorkspacePath: t.TempDir(),
		Git:           git,
		FS:            memfs,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !state.Dirty {
		t.Error("expected Dirty=true (intent.yaml is dirty)")
	}
}

// ------------------------------------------------------------------
// Resolver: empty diff (clean tree) → Dirty=false, no DirtyHash.
// ------------------------------------------------------------------

func TestResolveSourceSnapshot_CleanTree(t *testing.T) {
	git := &fakeGit{
		hasRepo: true,
		head:    "abc12345def0abcdef01",
		tree:    "9aa7710abcdef01",
		branch:  "main",
		ref:     "refs/heads/main",
	}
	state, err := sourcectx.ResolveSourceSnapshot(context.Background(), sourcectx.ResolveOptions{
		WorkspacePath: t.TempDir(),
		Git:           git,
		FS:            memFS{m: fstest.MapFS{}},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if state.Dirty || state.DirtyHash != "" {
		t.Errorf("expected clean tree; got Dirty=%v DirtyHash=%q", state.Dirty, state.DirtyHash)
	}
	if state.Scope() != "branch-main" {
		t.Errorf("Scope() = %q; want branch-main", state.Scope())
	}
}

// ------------------------------------------------------------------
// Resolver: only non-relevant diff entries → effectively clean.
// ------------------------------------------------------------------

func TestResolveSourceSnapshot_OnlyNonRelevantDirty(t *testing.T) {
	git := &fakeGit{
		hasRepo: true,
		head:    "abc12345def0abcdef01",
		tree:    "9aa7710abcdef01",
		branch:  "main",
		diff:    []string{"notes.txt", "vendor/foo.go"},
	}
	state, err := sourcectx.ResolveSourceSnapshot(context.Background(), sourcectx.ResolveOptions{
		WorkspacePath: t.TempDir(),
		Git:           git,
		FS:            memFS{m: fstest.MapFS{}},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if state.Dirty {
		t.Errorf("expected Dirty=false when only non-relevant files differ; got Dirty=true DirtyHash=%q", state.DirtyHash)
	}
}

// ------------------------------------------------------------------
// Resolver: HasRepo error bubbles up.
// ------------------------------------------------------------------

type erroringGit struct{ *fakeGit }

func (e *erroringGit) HasRepo(ctx context.Context, _ string) (bool, error) {
	return false, errors.New("probe boom")
}

func TestResolveSourceSnapshot_HasRepoError(t *testing.T) {
	_, err := sourcectx.ResolveSourceSnapshot(context.Background(), sourcectx.ResolveOptions{
		WorkspacePath: t.TempDir(),
		Git:           &erroringGit{fakeGit: &fakeGit{}},
		FS:            memFS{m: fstest.MapFS{}},
	})
	if err == nil || !strings.Contains(err.Error(), "HasRepo") {
		t.Errorf("expected HasRepo wrapped error, got %v", err)
	}
}
