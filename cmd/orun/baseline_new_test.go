package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// The build document is the one path in a baseline manifest this binary
// EXECUTES from, so it is the last one to trust for looking plausible.

func TestBuildDocumentReadsTheDeclaredPath(t *testing.T) {
	got, err := buildDocument([]byte(`
apiVersion: orun.io/v1
kind: Blueprint
spec:
  bootstrap:
    blueprint: repo-blueprint.yaml
    expectedMinutes: 60
`), "blueprint.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "repo-blueprint.yaml" {
		t.Fatalf("got %q, want repo-blueprint.yaml", got)
	}
}

func TestBuildDocumentRefusals(t *testing.T) {
	cases := []struct {
		name     string
		yaml     string
		wantWord string
	}{{
		name:     "absent — a shell-layer baseline, or one that has not declared it",
		yaml:     "spec:\n  bootstrap:\n    expectedMinutes: 60\n",
		wantWord: "declares no spec.bootstrap.blueprint",
	}, {
		name:     "empty is not absent, and is a half-finished edit either way",
		yaml:     "spec:\n  bootstrap:\n    blueprint: \"\"\n",
		wantWord: "declares no spec.bootstrap.blueprint",
	}, {
		name:     "whitespace only",
		yaml:     "spec:\n  bootstrap:\n    blueprint: \"   \"\n",
		wantWord: "declares no spec.bootstrap.blueprint",
	}, {
		name:     "absolute",
		yaml:     "spec:\n  bootstrap:\n    blueprint: /etc/passwd\n",
		wantWord: "must be repo-relative",
	}, {
		name:     "escaping",
		yaml:     "spec:\n  bootstrap:\n    blueprint: ../../etc/passwd\n",
		wantWord: "must not escape the repo",
	}, {
		// The case a literal prefix check passes: it does not START with `..`,
		// it RESOLVES to it. Cleaning before the check is what catches it.
		name:     "escaping after cleaning",
		yaml:     "spec:\n  bootstrap:\n    blueprint: a/b/../../../etc/passwd\n",
		wantWord: "must not escape the repo",
	}, {
		name:     "not YAML at all",
		yaml:     "\tthis is not: [valid",
		wantWord: "reading blueprint.yaml",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildDocument([]byte(tc.yaml), "blueprint.yaml")
			if err == nil {
				t.Fatalf("accepted %q, returning %q — it must be refused", tc.yaml, got)
			}
			if !strings.Contains(err.Error(), tc.wantWord) {
				t.Fatalf("error %q does not say %q", err, tc.wantWord)
			}
		})
	}
}

// A relative path that stays inside is fine, including a nested one.
func TestBuildDocumentAllowsANestedPath(t *testing.T) {
	got, err := buildDocument([]byte("spec:\n  bootstrap:\n    blueprint: build/repo-blueprint.yaml\n"), "blueprint.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != filepath.Join("build", "repo-blueprint.yaml") {
		t.Fatalf("got %q", got)
	}
}

func TestReadBuildDocumentJoinsCheckoutToDocument(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "blueprint.yaml"),
		"spec:\n  bootstrap:\n    blueprint: repo-blueprint.yaml\n")
	write(t, filepath.Join(dir, "repo-blueprint.yaml"), "kind: Blueprint\nphases: []\n")

	got, err := readBuildDocument(dir, "blueprint.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != filepath.Join(dir, "repo-blueprint.yaml") {
		t.Fatalf("got %q", got)
	}
}

// The registry row must NAME the manifest. Guessing the convention is how a
// two-manifest baseline (stratus / stratus-coolify share a tree) gets built
// from the wrong contract, which has happened on the platform side already.
func TestReadBuildDocumentRefusesAnUnnamedManifest(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "blueprint.yaml"),
		"spec:\n  bootstrap:\n    blueprint: repo-blueprint.yaml\n")
	write(t, filepath.Join(dir, "repo-blueprint.yaml"), "kind: Blueprint\n")

	// The file it WOULD have guessed is right there, so a convention-based
	// implementation passes this test and this one must not.
	if _, err := readBuildDocument(dir, "  "); err == nil {
		t.Fatal("an empty manifestPath was accepted — it guessed the convention")
	} else if !strings.Contains(err.Error(), "names no manifestPath") {
		t.Fatalf("error %q does not explain the refusal", err)
	}
}

// A manifest that names a document the tag does not carry is the failure a
// tagged release makes permanent, so it is named rather than surfaced as a
// bare open() error from somewhere deeper.
func TestReadBuildDocumentRefusesADocumentNotInTheTree(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "blueprint.yaml"),
		"spec:\n  bootstrap:\n    blueprint: repo-blueprint.yaml\n")

	_, err := readBuildDocument(dir, "blueprint.yaml")
	if err == nil {
		t.Fatal("a missing build document was accepted")
	}
	for _, want := range []string{"repo-blueprint.yaml", "not in the tree at this tag"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not say %q", err, want)
		}
	}
}

func TestReadBuildDocumentRefusesAMissingManifest(t *testing.T) {
	_, err := readBuildDocument(t.TempDir(), "blueprint.yaml")
	if err == nil {
		t.Fatal("a missing manifest was accepted")
	}
	if !strings.Contains(err.Error(), "at the pinned tag") {
		t.Fatalf("error %q does not name the tag as the thing that lacks it", err)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// ── the fetch seam, end to end without a network ────────────────────────────
//
// Everything this verb does after resolving a row: fetch the source at the
// pinned tag, find the manifest the ROW names, read the build document the
// MANIFEST names, and hand it to the placement pipeline. The seam stands in
// for the clone; the rest is the real code.

func TestFetchSeamLetsTheJoinBeTestedWithoutANetwork(t *testing.T) {
	baseline := t.TempDir()
	write(t, filepath.Join(baseline, "blueprint.yaml"),
		"spec:\n  bootstrap:\n    blueprint: repo-blueprint.yaml\n")
	write(t, filepath.Join(baseline, "repo-blueprint.yaml"), "kind: Blueprint\nphases: []\n")

	restore := fetchBaselineSource
	t.Cleanup(func() { fetchBaselineSource = restore })
	var gotRepo, gotRef string
	fetchBaselineSource = func(repo, ref, _ string) (string, error) {
		gotRepo, gotRef = repo, ref
		return baseline, nil
	}

	checkout, err := fetchBaselineSource("sourceplane/cirrus", "baseline-v6", t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotRepo != "sourceplane/cirrus" || gotRef != "baseline-v6" {
		t.Fatalf("fetched %s@%s", gotRepo, gotRef)
	}
	doc, err := readBuildDocument(checkout, "blueprint.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(doc) != "repo-blueprint.yaml" {
		t.Fatalf("resolved %q", doc)
	}
}

// The two-manifest case, which is why the row carries a path at all. One tree,
// two rows, two contracts: reading the row's own manifestPath must give each
// its own build document, and a convention gives both the same one.
func TestTheRowsManifestDecidesTheBuild(t *testing.T) {
	tree := t.TempDir()
	write(t, filepath.Join(tree, "blueprint.yaml"),
		"spec:\n  bootstrap:\n    blueprint: repo-blueprint.yaml\n")
	write(t, filepath.Join(tree, "blueprint-coolify.yaml"),
		"spec:\n  bootstrap:\n    blueprint: repo-blueprint-coolify.yaml\n")
	write(t, filepath.Join(tree, "repo-blueprint.yaml"), "kind: Blueprint\n")
	write(t, filepath.Join(tree, "repo-blueprint-coolify.yaml"), "kind: Blueprint\n")

	azure, err := readBuildDocument(tree, "blueprint.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	coolify, err := readBuildDocument(tree, "blueprint-coolify.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if azure == coolify {
		t.Fatal("both rows resolved to the same build document — the manifestPath was ignored")
	}
	if filepath.Base(coolify) != "repo-blueprint-coolify.yaml" {
		t.Fatalf("the coolify row built %q", filepath.Base(coolify))
	}
}

// ── TWO SHAPES, AND NEITHER IS THE DEFAULT (BE-O7b) ────────────────────────
//
// `--local` writes into a directory here. `--via-platform` writes an entire
// product into somebody's linked repository over about an hour, against a real
// cloud account. A flag that silently picked between those would surprise
// somebody at the worst possible moment, so exactly one must be named — and
// naming both is a question, not a preference.

func runBaselineNew(t *testing.T, args ...string) error {
	t.Helper()
	root := &cobra.Command{Use: "orun", SilenceUsage: true, SilenceErrors: true}
	registerBaselineCommand(root)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs(append([]string{"baseline", "new"}, args...))
	return root.Execute()
}

func TestBaselineNewRefusesNeitherShape(t *testing.T) {
	err := runBaselineNew(t, "cirrus")
	if err == nil {
		t.Fatal("ran a build without being told where")
	}
	for _, want := range []string{"--local", "--via-platform"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not offer %q: %v", want, err)
		}
	}
}

func TestBaselineNewRefusesBothShapes(t *testing.T) {
	err := runBaselineNew(t, "cirrus", "--local", "--via-platform", "--out", t.TempDir())
	if err == nil {
		t.Fatal("accepted both --local and --via-platform")
	}
	if !strings.Contains(err.Error(), "two different builds") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// `--out` is where a LOCAL build places the product. A platform build has no
// local output at all, so requiring it there would be asking for a directory
// nothing writes to.
func TestBaselineNewStillRequiresOutForALocalBuild(t *testing.T) {
	err := runBaselineNew(t, "cirrus", "--local")
	if err == nil || !strings.Contains(err.Error(), "--out") {
		t.Fatalf("a local build without --out: %v", err)
	}
}

// ── THE REGISTRY'S `owner/name` IS NOT A HOST ──────────────────────────────
//
// Reported from a real `--local` build, and it broke every one of them:
//
//	✕ clone sourceplane/cirrus@baseline-v5: Get
//	  "https://sourceplane/cirrus/info/refs?service=git-upload-pack":
//	  dial tcp: lookup sourceplane: no such host
//
// `fetchGit` takes a bare HOST/path and prefixes `https://` when there is no
// scheme. A registry row's `sourceRepo` has no host, because GitHub is the
// registry's standing assumption — every platform read of a baseline goes to
// `raw.githubusercontent.com/<sourceRepo>/<tag>/…`.

func TestBaselineCloneURLQualifiesTheRegistrysOwnerName(t *testing.T) {
	// The reported case, exactly.
	if got := baselineCloneURL("sourceplane/cirrus"); got != "github.com/sourceplane/cirrus" {
		t.Fatalf("got %q, want github.com/sourceplane/cirrus", got)
	}
}

// `fetchGit`'s own contract must keep working: a blueprint author writing
// `repo: gitlab.com/acme/x` is following it, and qualifying that would send
// their clone to the wrong forge.
func TestBaselineCloneURLLeavesAnythingDialableAlone(t *testing.T) {
	for _, repo := range []string{
		"github.com/sourceplane/cirrus",
		"gitlab.com/acme/product",
		"https://github.com/sourceplane/cirrus",
		"git@github.com:sourceplane/cirrus.git",
		"ssh://git@example.com/acme/x",
		// An scp-style remote whose host has no dot — a LAN git server. Two
		// segments and no dot in the first, so only the scheme/scp guard
		// keeps it from being rewritten to `github.com/git@gitserver:team/x`.
		// Found by mutation: without this case the guard is dead code.
		"git@gitserver:team/x",
		// Three segments is already host/path.
		"example.com/group/sub/repo",
	} {
		if got := baselineCloneURL(repo); got != repo {
			t.Errorf("rewrote %q to %q", repo, got)
		}
	}
}

func TestBaselineCloneURLHandlesNothing(t *testing.T) {
	if got := baselineCloneURL("   "); got != "" {
		t.Errorf("got %q", got)
	}
}

// The SEAM must apply the qualification, not merely the helper. Without this,
// deleting `baselineCloneURL(...)` from the seam leaves every unit test above
// green and every real build broken — which is exactly the shape of the bug
// this fixes.
func TestFetchBaselineSourceDialsTheQualifiedURL(t *testing.T) {
	var dialed string
	restore := gitRef
	t.Cleanup(func() { gitRef = restore })
	gitRef = func(repo, _, _, _ string) (string, error) {
		dialed = repo
		return "/tmp/checkout", nil
	}

	if _, err := fetchBaselineSource("sourceplane/cirrus", "baseline-v7", t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dialed != "github.com/sourceplane/cirrus" {
		t.Fatalf("the seam dialed %q — a host called %q does not exist",
			dialed, strings.Split(dialed, "/")[0])
	}
}
