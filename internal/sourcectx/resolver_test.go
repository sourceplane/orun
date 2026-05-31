package sourcectx_test

import (
	"context"
	"errors"
	"io/fs"
	"math/rand"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/sourcectx"
)

// ------------------------------------------------------------------
// Fake adapters used across the resolver test matrix.
// ------------------------------------------------------------------

// fakeGit is a programmable Git adapter. Each method returns the field
// value, with the option of forcing HasRepo=false to exercise the
// local-nogit path. fakeGit also stores a fake working-tree diff list so
// the resolver's dirty probe can be exercised without `git` on PATH.
type fakeGit struct {
	hasRepo bool
	head    string
	tree    string
	branch  string
	ref     string
	tag     string
	remote  string
	diff    []string
}

func (f *fakeGit) HasRepo(ctx context.Context, _ string) (bool, error) { return f.hasRepo, nil }
func (f *fakeGit) HeadRevision(ctx context.Context, _ string) (string, error) {
	return f.head, nil
}
func (f *fakeGit) TreeHash(ctx context.Context, _ string) (string, error) { return f.tree, nil }
func (f *fakeGit) Branch(ctx context.Context, _ string) (string, error)   { return f.branch, nil }
func (f *fakeGit) Ref(ctx context.Context, _ string) (string, error)      { return f.ref, nil }
func (f *fakeGit) Tag(ctx context.Context, _ string) (string, error)      { return f.tag, nil }
func (f *fakeGit) RemoteURL(ctx context.Context, _ string) (string, error) {
	return f.remote, nil
}
func (f *fakeGit) DiffTreePaths(ctx context.Context, _, _ string) ([]string, error) {
	out := append([]string{}, f.diff...)
	sort.Strings(out)
	return out, nil
}

// memFS satisfies sourcectx.Filesystem from an in-memory fstest.MapFS, so
// the resolver test matrix doesn't touch disk.
type memFS struct{ m fstest.MapFS }

func (mf memFS) Walk(_ string, fn func(rel string, d fs.DirEntry) error) error {
	return fs.WalkDir(mf.m, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return fn("", d)
		}
		return fn(p, d)
	})
}

func (mf memFS) Stat(p string) (fs.FileInfo, error) { return fs.Stat(mf.m, p) }
func (mf memFS) ReadFile(p string) ([]byte, error)  { return fs.ReadFile(mf.m, p) }

// ------------------------------------------------------------------
// Fixture matrix — every spec scope produces the right Scope() string and
// the BuildSourceSnapshotKey output matches the documented shape.
// ------------------------------------------------------------------

func TestResolveSourceSnapshot_FixtureMatrix(t *testing.T) {
	cases := []struct {
		name      string
		git       *fakeGit
		ci        sourcectx.CIEventInjection
		wantScope string
		// keyRegex is the documented shape from identity-and-keys.md §2,
		// with hex segments captured by [a-f0-9]+ since fixture content
		// dictates them.
		keyRegex string
	}{
		{
			name:      "branch-main",
			git:       &fakeGit{hasRepo: true, head: "def456a1b2c3deadbeef", tree: "5ab21c3aaaaaaaaa", branch: "main", ref: "refs/heads/main"},
			wantScope: catalogmodel.SourceScopeBranchMain,
			keyRegex:  `^src-branch-main-c[a-f0-9]{8}-t[a-f0-9]{7}$`,
		},
		{
			name:      "branch-feature",
			git:       &fakeGit{hasRepo: true, head: "abc12345def0abcdef01", tree: "9aa7710abcdef01", branch: "feature/x-new"},
			wantScope: catalogmodel.SourceScopeBranchFeature,
			keyRegex:  `^src-branch-feature-x-new-c[a-f0-9]{8}-t[a-f0-9]{7}$`,
		},
		{
			name:      "pr-via-ci",
			git:       &fakeGit{hasRepo: true, head: "abc12345def0abcdef01", tree: "9aa7710abcdef01", branch: "feature/x"},
			ci:        sourcectx.CIEventInjection{PRNumber: 139, Provider: "github", Event: "pull_request"},
			wantScope: catalogmodel.SourceScopePR,
			keyRegex:  `^src-pr139-c[a-f0-9]{8}-t[a-f0-9]{7}$`,
		},
		{
			name:      "tag",
			git:       &fakeGit{hasRepo: true, head: "def456a1b2c3deadbeef", tree: "5ab21c3aaaaaaaaa", branch: "main", tag: "v0.18.0"},
			wantScope: catalogmodel.SourceScopeTag,
			keyRegex:  `^src-tag-v0-18-0-c[a-f0-9]{8}-t[a-f0-9]{7}$`,
		},
		{
			name:      "local-nogit",
			git:       &fakeGit{hasRepo: false},
			wantScope: catalogmodel.SourceScopeLocalNoGit,
			keyRegex:  `^src-local-nogit$`,
		},
		{
			name: "ci-event-override",
			git:  &fakeGit{hasRepo: true, head: "abc12345def0abcdef01", tree: "9aa7710abcdef01", branch: "feature/x"},
			ci: sourcectx.CIEventInjection{
				CIEventScope: "ci-pr139",
				Provider:     "github",
				Event:        "pull_request",
			},
			wantScope: catalogmodel.SourceScopeCIEvent,
			keyRegex:  `^src-ci-pr139-c[a-f0-9]{8}-t[a-f0-9]{7}$`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, err := sourcectx.ResolveSourceSnapshot(context.Background(), sourcectx.ResolveOptions{
				WorkspacePath: t.TempDir(),
				Git:           tc.git,
				Clock:         sourcectx.FixedClock{T: time.Unix(0, 0).UTC()},
				FS:            memFS{m: fstest.MapFS{}},
				CIEvent:       tc.ci,
			})
			if err != nil {
				t.Fatalf("ResolveSourceSnapshot: %v", err)
			}
			if got := state.Scope(); got != tc.wantScope {
				t.Errorf("Scope() = %q; want %q", got, tc.wantScope)
			}
			key := sourcectx.BuildSourceSnapshotKey(state)
			if !regexp.MustCompile(tc.keyRegex).MatchString(key) {
				t.Errorf("BuildSourceSnapshotKey = %q; want match %s", key, tc.keyRegex)
			}
			if err := catalogmodel.ValidateSourceSnapshotKey(key); err != nil {
				t.Errorf("ValidateSourceSnapshotKey(%q): %v", key, err)
			}
		})
	}
}

// ------------------------------------------------------------------
// local-dirty fixture — an actual dirty file shows up in DirtyHash and the
// key gains the d-segment.
// ------------------------------------------------------------------

func TestResolveSourceSnapshot_LocalDirty(t *testing.T) {
	fs := memFS{m: fstest.MapFS{
		"intent.yaml":         &fstest.MapFile{Data: []byte("namespace: x\n")},
		"a/component.yaml":    &fstest.MapFile{Data: []byte("metadata: { name: a }\n")},
		"notes.txt":           &fstest.MapFile{Data: []byte("not catalog relevant\n")},
		"a/vendored/foo.go":   &fstest.MapFile{Data: []byte("package vendored\n")},
	}}
	git := &fakeGit{
		hasRepo: true,
		head:    "abc12345def0abcdef01",
		tree:    "9aa7710abcdef01",
		branch:  "feature/x",
		// git considers all four files dirty, but only the first two are
		// catalog-relevant. notes.txt and the vendored .go file MUST NOT
		// reach DirtyHash (T-IDK-4).
		diff: []string{"intent.yaml", "a/component.yaml", "notes.txt", "a/vendored/foo.go"},
	}

	state, err := sourcectx.ResolveSourceSnapshot(context.Background(), sourcectx.ResolveOptions{
		WorkspacePath: t.TempDir(),
		Git:           git,
		FS:            fs,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !state.Dirty {
		t.Fatalf("expected Dirty=true")
	}
	if state.Scope() != catalogmodel.SourceScopeLocalDirty {
		t.Errorf("Scope() = %q; want local-dirty", state.Scope())
	}
	key := sourcectx.BuildSourceSnapshotKey(state)
	if !strings.Contains(key, "-d") {
		t.Errorf("expected key to carry d-segment, got %q", key)
	}
}

// ------------------------------------------------------------------
// T-IDK-3 — SourceSnapshotKey is byte-identical across 1 000 random
// orderings of the dirty-input list.
// ------------------------------------------------------------------

func TestResolveSourceSnapshot_KeyStableUnderDirtyOrdering(t *testing.T) {
	fs := memFS{m: fstest.MapFS{
		"intent.yaml":              &fstest.MapFile{Data: []byte("namespace: x\n")},
		"a/component.yaml":         &fstest.MapFile{Data: []byte("metadata: { name: a }\n")},
		"b/component.yaml":         &fstest.MapFile{Data: []byte("metadata: { name: b }\n")},
		"c/component.yaml":         &fstest.MapFile{Data: []byte("metadata: { name: c }\n")},
		"composition.yaml":         &fstest.MapFile{Data: []byte("kind: Composition\n")},
		"stack.yaml":               &fstest.MapFile{Data: []byte("kind: Stack\n")},
		"notes.txt":                &fstest.MapFile{Data: []byte("ignored\n")},
		"vendor/foo.go":            &fstest.MapFile{Data: []byte("package vendor\n")},
		"deep/nested/component.yaml": &fstest.MapFile{Data: []byte("metadata: { name: deep }\n")},
	}}

	dirty := []string{
		"intent.yaml", "a/component.yaml", "b/component.yaml",
		"c/component.yaml", "composition.yaml", "stack.yaml",
		"deep/nested/component.yaml",
		// non-relevant noise interleaved into the diff list.
		"notes.txt", "vendor/foo.go",
	}

	resolve := func(order []string) string {
		git := &fakeGit{
			hasRepo: true,
			head:    "abc12345def0abcdef01",
			tree:    "9aa7710abcdef01",
			branch:  "feature/x",
			diff:    append([]string{}, order...),
		}
		state, err := sourcectx.ResolveSourceSnapshot(context.Background(), sourcectx.ResolveOptions{
			WorkspacePath: t.TempDir(),
			Git:           git,
			FS:            fs,
		})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		return sourcectx.BuildSourceSnapshotKey(state)
	}

	want := resolve(dirty)
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 1000; i++ {
		shuffled := append([]string{}, dirty...)
		rng.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		if got := resolve(shuffled); got != want {
			t.Fatalf("iteration %d: key drift\n want %q\n got  %q", i, want, got)
		}
	}
}

// ------------------------------------------------------------------
// T-IDK-4 — adding a non-catalog-relevant file to the dirty input set
// MUST NOT change dirtyHash.
// ------------------------------------------------------------------

func TestResolveSourceSnapshot_DirtyHashIgnoresNonCatalogFiles(t *testing.T) {
	fs := memFS{m: fstest.MapFS{
		"intent.yaml":      &fstest.MapFile{Data: []byte("namespace: x\n")},
		"a/component.yaml": &fstest.MapFile{Data: []byte("metadata: { name: a }\n")},
		"notes.txt":        &fstest.MapFile{Data: []byte("ignored on purpose\n")},
		"vendor/foo.go":    &fstest.MapFile{Data: []byte("package vendor\n")},
		".github/workflows/ci.yml": &fstest.MapFile{Data: []byte("name: ci\n")},
	}}

	gitRelevantOnly := &fakeGit{
		hasRepo: true,
		head:    "abc12345def0abcdef01",
		tree:    "9aa7710abcdef01",
		branch:  "feature/x",
		diff:    []string{"intent.yaml", "a/component.yaml"},
	}
	gitWithNoise := &fakeGit{
		hasRepo: true,
		head:    "abc12345def0abcdef01",
		tree:    "9aa7710abcdef01",
		branch:  "feature/x",
		diff:    []string{"intent.yaml", "a/component.yaml", "notes.txt", "vendor/foo.go", ".github/workflows/ci.yml"},
	}

	a, err := sourcectx.ResolveSourceSnapshot(context.Background(), sourcectx.ResolveOptions{
		WorkspacePath: t.TempDir(), Git: gitRelevantOnly, FS: fs,
	})
	if err != nil {
		t.Fatalf("resolve a: %v", err)
	}
	b, err := sourcectx.ResolveSourceSnapshot(context.Background(), sourcectx.ResolveOptions{
		WorkspacePath: t.TempDir(), Git: gitWithNoise, FS: fs,
	})
	if err != nil {
		t.Fatalf("resolve b: %v", err)
	}
	if a.DirtyHash != b.DirtyHash {
		t.Fatalf("DirtyHash drifted under non-catalog noise:\n a=%s\n b=%s", a.DirtyHash, b.DirtyHash)
	}
	if !a.Dirty || !b.Dirty {
		t.Fatalf("expected both states Dirty=true; got a=%v b=%v", a.Dirty, b.Dirty)
	}
	if sourcectx.BuildSourceSnapshotKey(a) != sourcectx.BuildSourceSnapshotKey(b) {
		t.Fatalf("key drifted under non-catalog noise:\n a=%s\n b=%s",
			sourcectx.BuildSourceSnapshotKey(a),
			sourcectx.BuildSourceSnapshotKey(b))
	}
}

// ------------------------------------------------------------------
// `--from-ci` no-match — typed sentinel + structured reason.
// ------------------------------------------------------------------

func TestResolveSourceSnapshot_CIEventNoMatch(t *testing.T) {
	cases := []struct {
		name       string
		git        *fakeGit
		ci         sourcectx.CIEventInjection
		wantReason string
	}{
		{
			name:       "no-git-repo",
			git:        &fakeGit{hasRepo: false},
			ci:         sourcectx.CIEventInjection{PRNumber: 42, Provider: "github", Event: "pull_request"},
			wantReason: "no-git-repo",
		},
		{
			name:       "no-head-revision",
			git:        &fakeGit{hasRepo: true, head: "", tree: "5ab21c3"},
			ci:         sourcectx.CIEventInjection{PRNumber: 42, Provider: "github", Event: "pull_request"},
			wantReason: "no-head-revision",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sourcectx.ResolveSourceSnapshot(context.Background(), sourcectx.ResolveOptions{
				WorkspacePath: t.TempDir(),
				Git:           tc.git,
				FS:            memFS{m: fstest.MapFS{}},
				CIEvent:       tc.ci,
			})
			if err == nil {
				t.Fatal("expected non-nil error")
			}
			if !errors.Is(err, sourcectx.ErrCIEventNoMatch) {
				t.Fatalf("expected errors.Is(..., ErrCIEventNoMatch); got %v", err)
			}
			var typed *sourcectx.CIEventNoMatchError
			if !errors.As(err, &typed) {
				t.Fatalf("expected *CIEventNoMatchError; got %T", err)
			}
			if typed.Reason != tc.wantReason {
				t.Errorf("Reason = %q; want %q", typed.Reason, tc.wantReason)
			}
			if typed.Provider != tc.ci.Provider {
				t.Errorf("Provider = %q; want %q", typed.Provider, tc.ci.Provider)
			}
		})
	}
}

// ------------------------------------------------------------------
// CatalogInputHash — resolver-version block changes the hash; non-relevant
// inputs do not.
// ------------------------------------------------------------------

func TestCatalogInputHash_ResolverInputsAffectHash(t *testing.T) {
	base := sourcectx.CatalogInputHashInputs{
		TreeHash:        "5ab21c3",
		DirtyHash:       "",
		OrunVersion:     "0.18.0",
		ResolverVersion: 1,
		SchemaVersion:   "orun.io/v1alpha1",
		StackSources:    []string{"ghcr.io/x/y:1.0"},
		IntentCanonical: []byte(`{"namespace":"sourceplane"}`),
	}
	h := sourcectx.CatalogInputHash(base)
	bumped := base
	bumped.OrunVersion = "0.18.1"
	if got := sourcectx.CatalogInputHash(bumped); got == h {
		t.Errorf("CatalogInputHash insensitive to OrunVersion bump")
	}
}

// ------------------------------------------------------------------
// WithDefaults — nil adapters get filled in.
// ------------------------------------------------------------------

func TestWithDefaults_FillsAdapters(t *testing.T) {
	opts := sourcectx.WithDefaults(sourcectx.ResolveOptions{WorkspacePath: t.TempDir()})
	if opts.Git == nil || opts.Clock == nil || opts.FS == nil {
		t.Fatalf("WithDefaults left a nil adapter: %+v", opts)
	}
}

// ------------------------------------------------------------------
// CatalogRelevant — total + matches §7.
// ------------------------------------------------------------------

func TestCatalogRelevant(t *testing.T) {
	flags := sourcectx.InferenceFlags{Readme: true, PackageJSON: true, Dockerfile: true, Helm: true, Terraform: true}
	always := []string{"intent.yaml", "a/component.yaml", "stack.yaml", "composition.yaml"}
	for _, p := range always {
		if !sourcectx.CatalogRelevant(p, sourcectx.InferenceFlags{}) {
			t.Errorf("CatalogRelevant(%q, zero flags) = false; want true", p)
		}
	}
	gated := []string{"package.json", "Dockerfile", "Chart.yaml", "main.tf", "README.md"}
	for _, p := range gated {
		if sourcectx.CatalogRelevant(p, sourcectx.InferenceFlags{}) {
			t.Errorf("CatalogRelevant(%q, zero flags) = true; expected gated off", p)
		}
		if !sourcectx.CatalogRelevant(p, flags) {
			t.Errorf("CatalogRelevant(%q, all flags) = false; want true", p)
		}
	}
	excluded := []string{"notes.txt", "vendor/foo.go", ".github/workflows/ci.yml", "docs/index.md"}
	for _, p := range excluded {
		if sourcectx.CatalogRelevant(p, flags) {
			t.Errorf("CatalogRelevant(%q, all flags) = true; want excluded", p)
		}
	}
	if sourcectx.CatalogRelevant("", flags) {
		t.Error("CatalogRelevant on empty path should be false")
	}
}

// ------------------------------------------------------------------
// Repo derivation from remote URL — covers SSH, HTTPS, and bare paths.
// ------------------------------------------------------------------

func TestResolveSourceSnapshot_RepoDerivation(t *testing.T) {
	cases := []struct {
		remote   string
		wantRepo string
	}{
		{"git@github.com:sourceplane/orun.git", "sourceplane/orun"},
		{"https://github.com/sourceplane/orun.git", "sourceplane/orun"},
		{"https://gitlab.com/group/sub/repo.git", "sub/repo"},
		{"", ""},
	}
	for _, tc := range cases {
		git := &fakeGit{hasRepo: true, head: "abc12345def0abcdef01", tree: "9aa7710abcdef01", branch: "main", remote: tc.remote}
		state, err := sourcectx.ResolveSourceSnapshot(context.Background(), sourcectx.ResolveOptions{
			WorkspacePath: t.TempDir(),
			Git:           git,
			FS:            memFS{m: fstest.MapFS{}},
		})
		if err != nil {
			t.Fatalf("resolve %q: %v", tc.remote, err)
		}
		if state.Repo != tc.wantRepo {
			t.Errorf("Repo for remote %q = %q; want %q", tc.remote, state.Repo, tc.wantRepo)
		}
	}
}

// ------------------------------------------------------------------
// Default Git adapter — exercise against a real on-disk repo built in
// t.TempDir(). We chose the shell-out route (see git_exec.go header), so
// we DO NOT skip on missing git: tests gate on git being present in the
// dev/CI image, matching every other internal/git test in the repo.
// ------------------------------------------------------------------

func TestDefaultGitAdapter_RealRepo(t *testing.T) {
	dir := t.TempDir()
	mustRun := func(args ...string) {
		t.Helper()
		out, err := runCmd(dir, args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	mustRun("git", "init", "-q", "-b", "main")
	mustRun("git", "config", "user.email", "test@example.com")
	mustRun("git", "config", "user.name", "Test")
	if err := writeFileTo(dir, "intent.yaml", "namespace: x\n"); err != nil {
		t.Fatal(err)
	}
	mustRun("git", "add", ".")
	mustRun("git", "commit", "-q", "-m", "init")

	git := sourcectx.DefaultGit()
	ctx := context.Background()
	has, err := git.HasRepo(ctx, dir)
	if err != nil {
		t.Fatalf("HasRepo: %v", err)
	}
	if !has {
		t.Fatal("HasRepo = false on a fresh repo")
	}
	head, err := git.HeadRevision(ctx, dir)
	if err != nil || len(head) < 12 {
		t.Fatalf("HeadRevision = %q err=%v", head, err)
	}
	tree, err := git.TreeHash(ctx, dir)
	if err != nil || len(tree) < 7 {
		t.Fatalf("TreeHash = %q err=%v", tree, err)
	}
	br, err := git.Branch(ctx, dir)
	if err != nil || br != "main" {
		t.Fatalf("Branch = %q err=%v", br, err)
	}
	ref, err := git.Ref(ctx, dir)
	if err != nil || ref != "refs/heads/main" {
		t.Fatalf("Ref = %q err=%v", ref, err)
	}

	// Tag at HEAD.
	mustRun("git", "tag", "v0.0.1")
	tag, err := git.Tag(ctx, dir)
	if err != nil || tag != "v0.0.1" {
		t.Fatalf("Tag = %q err=%v", tag, err)
	}

	// Dirty modification → DiffTreePaths picks it up.
	if err := writeFileTo(dir, "intent.yaml", "namespace: x\n# changed\n"); err != nil {
		t.Fatal(err)
	}
	paths, err := git.DiffTreePaths(ctx, dir, tree)
	if err != nil {
		t.Fatalf("DiffTreePaths: %v", err)
	}
	found := false
	for _, p := range paths {
		if p == "intent.yaml" {
			found = true
		}
	}
	if !found {
		t.Fatalf("DiffTreePaths did not surface dirty intent.yaml: %v", paths)
	}

	// HasRepo on a non-repo path returns false (no error).
	noRepo := t.TempDir()
	hr, err := git.HasRepo(ctx, noRepo)
	if err != nil {
		t.Fatalf("HasRepo on tmp non-repo: %v", err)
	}
	if hr {
		t.Fatal("HasRepo on bare tmpdir returned true")
	}

	// End-to-end: ResolveSourceSnapshot on the real repo produces a
	// well-shaped key.
	state, err := sourcectx.ResolveSourceSnapshot(ctx, sourcectx.ResolveOptions{
		WorkspacePath: dir,
	})
	if err != nil {
		t.Fatalf("ResolveSourceSnapshot on real repo: %v", err)
	}
	key := sourcectx.BuildSourceSnapshotKey(state)
	if err := catalogmodel.ValidateSourceSnapshotKey(key); err != nil {
		t.Fatalf("ValidateSourceSnapshotKey(%q): %v", key, err)
	}
}

// runCmd is a tiny exec helper (test-local) so we don't drag os/exec into
// the production package surface.
func runCmd(dir string, args ...string) (string, error) {
	cmd := execCommand(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func writeFileTo(dir, name, content string) error {
	return writeFileWithDir(filepath.Join(dir, name), []byte(content))
}
