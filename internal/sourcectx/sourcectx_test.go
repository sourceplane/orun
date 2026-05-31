package sourcectx_test

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/sourcectx"
)

func TestWorkspaceState_Scope(t *testing.T) {
	cases := []struct {
		name string
		w    sourcectx.WorkspaceState
		want string
	}{
		{"main", sourcectx.WorkspaceState{Branch: "main", HeadRevision: "def456a1b2c3"}, catalogmodel.SourceScopeBranchMain},
		{"feature", sourcectx.WorkspaceState{Branch: "feature/x", HeadRevision: "abc123def456"}, catalogmodel.SourceScopeBranchFeature},
		{"dirty", sourcectx.WorkspaceState{Branch: "feature/x", HeadRevision: "abc123def456", Dirty: true, DirtyHash: "sha256:91aa77b2x"}, catalogmodel.SourceScopeLocalDirty},
		{"pr", sourcectx.WorkspaceState{Branch: "feature/x", HeadRevision: "abc123def456", PRNumber: 139}, catalogmodel.SourceScopePR},
		{"tag", sourcectx.WorkspaceState{Branch: "main", HeadRevision: "def456a1b2c3", Tag: "v0.18.0"}, catalogmodel.SourceScopeTag},
		{"local-nogit", sourcectx.WorkspaceState{}, catalogmodel.SourceScopeLocalNoGit},
		{"ci-event", sourcectx.WorkspaceState{CIEvent: "ci-pr139", HeadRevision: "abc123def456"}, catalogmodel.SourceScopeCIEvent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.w.Scope(); got != tc.want {
				t.Errorf("Scope() = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestBuildSourceSnapshotKey(t *testing.T) {
	cases := []struct {
		name string
		w    sourcectx.WorkspaceState
	}{
		{"main", sourcectx.WorkspaceState{Branch: "main", HeadRevision: "def456a1b2c3", TreeHash: "5ab21c3aaaa"}},
		{"feature", sourcectx.WorkspaceState{Branch: "feature/x-new", HeadRevision: "abc12345def0", TreeHash: "9aa7710abcd"}},
		{"pr", sourcectx.WorkspaceState{Branch: "feature/x", HeadRevision: "abc12345def0", TreeHash: "9aa7710abcd", PRNumber: 139}},
		{"local-nogit", sourcectx.WorkspaceState{Dirty: true, DirtyHash: "sha256:91aa77b2123abc"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := sourcectx.BuildSourceSnapshotKey(tc.w)
			if !strings.HasPrefix(key, "src-") {
				t.Errorf("key %q does not start with src-", key)
			}
			if err := catalogmodel.ValidateSourceSnapshotKey(key); err != nil {
				t.Errorf("ValidateSourceSnapshotKey(%q): %v", key, err)
			}
		})
	}
}

func TestDirtyHash_Stable(t *testing.T) {
	files := []sourcectx.DirtyFile{
		{Path: "z/last.txt", Content: []byte("z")},
		{Path: "a/first.txt", Content: []byte("a")},
		{Path: "m/middle.txt", Content: []byte("m")},
	}
	a := sourcectx.DirtyHash(files)

	// Reorder — output must be identical (we sort internally).
	files2 := []sourcectx.DirtyFile{files[1], files[2], files[0]}
	b := sourcectx.DirtyHash(files2)
	if a != b {
		t.Fatalf("DirtyHash not stable across order:\n a=%s\n b=%s", a, b)
	}

	// Tweak one byte → hash must change.
	files3 := append([]sourcectx.DirtyFile{}, files...)
	files3[0].Content = []byte("Z")
	c := sourcectx.DirtyHash(files3)
	if a == c {
		t.Fatal("DirtyHash unchanged under content mutation")
	}

	if !strings.HasPrefix(a, "sha256:") {
		t.Errorf("DirtyHash missing sha256: prefix: %q", a)
	}
}

func TestCatalogInputHash_Stable(t *testing.T) {
	in := sourcectx.CatalogInputHashInputs{
		TreeHash:        "5ab21c3",
		DirtyHash:       "",
		OrunVersion:     "0.18.0",
		ResolverVersion: 1,
		SchemaVersion:   "orun.io/v1alpha1",
		StackSources:    []string{"ghcr.io/x/y:1.0", "ghcr.io/a/b:2.0"},
		IntentCanonical: []byte(`{"namespace":"sourceplane"}`),
	}
	a := sourcectx.CatalogInputHash(in)

	// Reorder stacks → identical.
	in2 := in
	in2.StackSources = []string{"ghcr.io/a/b:2.0", "ghcr.io/x/y:1.0"}
	b := sourcectx.CatalogInputHash(in2)
	if a != b {
		t.Fatalf("CatalogInputHash sensitive to stack-source order:\n a=%s\n b=%s", a, b)
	}

	// Bump resolverVersion → must differ.
	in3 := in
	in3.ResolverVersion = 2
	c := sourcectx.CatalogInputHash(in3)
	if a == c {
		t.Fatal("CatalogInputHash unchanged under resolverVersion bump")
	}

	if !strings.HasPrefix(a, "sha256:") {
		t.Errorf("CatalogInputHash missing sha256: prefix: %q", a)
	}
}
