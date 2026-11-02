package ground

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newRemote builds a bare repo with one commit on `main` and a second branch
// `release`, and returns its path — the stand-in for the bound repository. No
// network, no credentials: GS0's clone path is exercised end to end over a
// local file remote.
func newRemote(t *testing.T) string {
	t.Helper()
	requireGit(t)
	root := t.TempDir()
	work := filepath.Join(root, "seed")
	bare := filepath.Join(root, "remote.git")
	mustGit(t, "", "init", "--quiet", "--initial-branch=main", work)
	mustGit(t, work, "config", "user.email", "test@example.com")
	mustGit(t, work, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, work, "add", ".")
	mustGit(t, work, "commit", "--quiet", "-m", "seed")
	mustGit(t, work, "branch", "release")
	mustGit(t, "", "clone", "--quiet", "--bare", work, bare)
	return bare
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runGit(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return out
}

func TestDetectInertWithoutRemote(t *testing.T) {
	// The whole feature is opt-in on ORUN_REPO_REMOTE: an ungrounded session
	// must see nil and boot exactly as it does today.
	env := map[string]string{"ORUN_REPO_FULL_NAME": "acme/widgets", "ORUN_REPO_REF": "main"}
	if got := Detect(func(k string) string { return env[k] }); got != nil {
		t.Fatalf("Detect without ORUN_REPO_REMOTE = %+v, want nil", got)
	}
}

func TestDetectReadsTrio(t *testing.T) {
	env := map[string]string{
		"ORUN_REPO_REMOTE":    "  https://github.com/acme/widgets.git  ",
		"ORUN_REPO_FULL_NAME": "acme/widgets",
		"ORUN_REPO_REF":       "main",
	}
	got := Detect(func(k string) string { return env[k] })
	if got == nil {
		t.Fatal("Detect returned nil for a grounded session")
	}
	if got.Remote != "https://github.com/acme/widgets.git" {
		t.Errorf("Remote = %q, want the trimmed URL", got.Remote)
	}
	if got.FullName != "acme/widgets" || got.Ref != "main" {
		t.Errorf("FullName/Ref = %q/%q", got.FullName, got.Ref)
	}
}

func TestGroundClonesAtRefAndCreatesBranch(t *testing.T) {
	remote := newRemote(t)
	root := t.TempDir()
	cfg := &Config{Remote: remote, FullName: "acme/widgets", Ref: "release"}

	dir, err := Ground(context.Background(), cfg, Options{Branch: "agent/ORN-1-implementer", WorkdirRoot: root})
	if err != nil {
		t.Fatalf("Ground: %v", err)
	}
	if want := filepath.Join(root, "widgets"); dir != want {
		t.Errorf("workdir = %q, want %q (named from the repo, not the remote path)", dir, want)
	}
	if head := mustGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); head != "agent/ORN-1-implementer" {
		t.Errorf("HEAD = %q, want the task branch checked out", head)
	}
	// The branch must start from the bound ref, not the remote's default.
	if base := mustGit(t, dir, "rev-parse", "release"); base != mustGit(t, dir, "rev-parse", "HEAD") {
		t.Error("task branch did not start from ORUN_REPO_REF")
	}
	// Blobless: the clone records the partial-clone filter promise.
	if got := mustGit(t, dir, "config", "--get", "remote.origin.promisor"); got != "true" {
		t.Errorf("remote.origin.promisor = %q, want true (blobless clone)", got)
	}
}

func TestGroundWithoutBranchStaysOnRef(t *testing.T) {
	// An interactive session carries no task key, so serve passes no branch —
	// grounding still clones, it just leaves HEAD on the ref.
	remote := newRemote(t)
	cfg := &Config{Remote: remote, FullName: "acme/widgets", Ref: "main"}
	dir, err := Ground(context.Background(), cfg, Options{WorkdirRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("Ground: %v", err)
	}
	if head := mustGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); head != "main" {
		t.Errorf("HEAD = %q, want the ref", head)
	}
}

func TestGroundResumeFetchesInsteadOfCloning(t *testing.T) {
	// The resume path (cloud design §10): the checkout may survive a snapshot,
	// so a second boot over the same directory must reuse it — never re-clone
	// over the agent's working tree.
	remote := newRemote(t)
	root := t.TempDir()
	cfg := &Config{Remote: remote, FullName: "acme/widgets", Ref: "main"}
	branch := "agent/ORN-2-implementer"

	dir, err := Ground(context.Background(), cfg, Options{Branch: branch, WorkdirRoot: root})
	if err != nil {
		t.Fatalf("first Ground: %v", err)
	}
	marker := filepath.Join(dir, "AGENT_SCRATCH")
	if err := os.WriteFile(marker, []byte("in progress\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir2, err := Ground(context.Background(), cfg, Options{Branch: branch, WorkdirRoot: root})
	if err != nil {
		t.Fatalf("resume Ground: %v", err)
	}
	if dir2 != dir {
		t.Fatalf("resume workdir = %q, want %q", dir2, dir)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("resume destroyed the existing working tree (marker file gone) — it re-cloned instead of fetching")
	}
}

func TestGroundResumeReusesBranchWithoutClobbering(t *testing.T) {
	// A resumed session's task branch carries commits the agent already made.
	// Reuse means check out as-is — never reset back to the ref.
	remote := newRemote(t)
	root := t.TempDir()
	cfg := &Config{Remote: remote, FullName: "acme/widgets", Ref: "main"}
	branch := "agent/ORN-3-implementer"

	dir, err := Ground(context.Background(), cfg, Options{Branch: branch, WorkdirRoot: root})
	if err != nil {
		t.Fatalf("first Ground: %v", err)
	}
	mustGit(t, dir, "config", "user.email", "agent@example.com")
	mustGit(t, dir, "config", "user.name", "Agent")
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("agent work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "--quiet", "-m", "agent progress")
	want := mustGit(t, dir, "rev-parse", "HEAD")

	if _, err := Ground(context.Background(), cfg, Options{Branch: branch, WorkdirRoot: root}); err != nil {
		t.Fatalf("resume Ground: %v", err)
	}
	if got := mustGit(t, dir, "rev-parse", "HEAD"); got != want {
		t.Errorf("HEAD = %s, want %s — the agent's commit was clobbered on resume", got, want)
	}
	if head := mustGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); head != branch {
		t.Errorf("HEAD branch = %q, want %q", head, branch)
	}
}

func TestGroundChecksOutABranchThatExistsOnlyOnOrigin(t *testing.T) {
	// saas-session-continuity CT3: a previous session (on another box) pushed
	// the task branch. A fresh clone holds it only as origin/<branch>; the
	// session must start from it, not from the ref.
	remote := newRemote(t)
	branch := "orun/ORN-3-home-page"
	first, err := Ground(context.Background(), &Config{Remote: remote, FullName: "acme/widgets", Ref: "main"}, Options{Branch: branch, WorkdirRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("first Ground: %v", err)
	}
	mustGit(t, first, "config", "user.email", "agent@example.com")
	mustGit(t, first, "config", "user.name", "Agent")
	if err := os.WriteFile(filepath.Join(first, "work.txt"), []byte("pushed work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, first, "add", ".")
	mustGit(t, first, "commit", "--quiet", "-m", "pushed progress")
	mustGit(t, first, "push", "--quiet", "origin", branch)
	want := mustGit(t, first, "rev-parse", "HEAD")

	// A new box: a fresh workdir root, so a fresh clone.
	dir, err := Ground(context.Background(), &Config{Remote: remote, FullName: "acme/widgets", Ref: "main"}, Options{Branch: branch, WorkdirRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("second Ground: %v", err)
	}
	if got := mustGit(t, dir, "rev-parse", "HEAD"); got != want {
		t.Errorf("HEAD = %s, want %s — the pushed branch was re-created from the ref", got, want)
	}
	if up := mustGit(t, dir, "rev-parse", "--abbrev-ref", branch+"@{upstream}"); up != "origin/"+branch {
		t.Errorf("upstream = %q, want origin/%s", up, branch)
	}
}

func TestGroundRefusesForeignWorkdir(t *testing.T) {
	// Grounding must never adopt a directory it does not own.
	remote := newRemote(t)
	other := newRemote(t)
	root := t.TempDir()

	if _, err := Ground(context.Background(), &Config{Remote: other, FullName: "acme/widgets", Ref: "main"},
		Options{WorkdirRoot: root}); err != nil {
		t.Fatalf("seed Ground: %v", err)
	}
	_, err := Ground(context.Background(), &Config{Remote: remote, FullName: "acme/widgets", Ref: "main"},
		Options{WorkdirRoot: root})
	assertStage(t, err, StagePrepare)
}

func TestGroundRefusesNonGitWorkdir(t *testing.T) {
	remote := newRemote(t)
	root := t.TempDir()
	dir := filepath.Join(root, "widgets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("not a repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Ground(context.Background(), &Config{Remote: remote, FullName: "acme/widgets", Ref: "main"},
		Options{WorkdirRoot: root})
	assertStage(t, err, StagePrepare)
}

func TestGroundEmptyWorkdirClonesFresh(t *testing.T) {
	// An empty directory is not a foreign one — it is where the clone goes.
	remote := newRemote(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "widgets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Ground(context.Background(), &Config{Remote: remote, FullName: "acme/widgets", Ref: "main"},
		Options{WorkdirRoot: root}); err != nil {
		t.Fatalf("Ground over an empty dir: %v", err)
	}
}

func TestGroundUnreachableRemoteFailsAtClone(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	cfg := &Config{Remote: filepath.Join(t.TempDir(), "no-such-repo.git"), FullName: "acme/widgets", Ref: "main"}
	_, err := Ground(context.Background(), cfg, Options{WorkdirRoot: root})
	assertStage(t, err, StageClone)
}

func TestGroundMissingRefFailsAtClone(t *testing.T) {
	remote := newRemote(t)
	cfg := &Config{Remote: remote, FullName: "acme/widgets", Ref: "no-such-branch"}
	_, err := Ground(context.Background(), cfg, Options{WorkdirRoot: t.TempDir()})
	assertStage(t, err, StageClone)
}

func TestGroundWithoutBindingIsAPrepareError(t *testing.T) {
	_, err := Ground(context.Background(), nil, Options{WorkdirRoot: t.TempDir()})
	assertStage(t, err, StagePrepare)
}

func TestStageErrorCarriesTerminalMarker(t *testing.T) {
	// The cloud failure path keys on this exact prefix (design §1 step 7).
	err := stageErr(StageClone, errors.New("boom"))
	if got, want := err.Error(), "repo_clone_failed: clone: boom"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(err, err.Err) {
		t.Error("StageError must unwrap to its cause")
	}
}

func TestRedactCredentials(t *testing.T) {
	// GS0 never writes a credential into a URL, but git can echo one back and
	// the invariant is absolute: nothing secret in an error.
	for _, tc := range []struct{ in, want string }{
		{"fatal: https://x-access-token:ghs_secret@github.com/acme/w.git not found",
			"fatal: https://github.com/acme/w.git not found"},
		{"fatal: http://user:pw@host/r.git denied", "fatal: http://host/r.git denied"},
		{"fatal: https://github.com/acme/w.git not found", "fatal: https://github.com/acme/w.git not found"},
	} {
		if got := redactCredentials(tc.in); got != tc.want {
			t.Errorf("redactCredentials(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRepoNameDerivation(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
		want string
	}{
		{"full name", Config{FullName: "acme/widgets", Remote: "https://github.com/acme/widgets.git"}, "widgets"},
		{"full name with .git", Config{FullName: "acme/widgets.git"}, "widgets"},
		{"falls back to remote", Config{Remote: "https://github.com/acme/widgets.git"}, "widgets"},
		{"scp-style remote", Config{Remote: "git@github.com:acme/widgets.git"}, "widgets"},
		{"neither", Config{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := repoName(&tc.cfg); got != tc.want {
				t.Errorf("repoName = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGroundLogsProgress(t *testing.T) {
	remote := newRemote(t)
	var log strings.Builder
	if _, err := Ground(context.Background(), &Config{Remote: remote, FullName: "acme/widgets", Ref: "main"},
		Options{Branch: "agent/ORN-4-implementer", WorkdirRoot: t.TempDir(), Log: &log}); err != nil {
		t.Fatalf("Ground: %v", err)
	}
	for _, want := range []string{"cloning", "task branch"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("progress log missing %q; got:\n%s", want, log.String())
		}
	}
}

func assertStage(t *testing.T, err error, stage string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a %s-stage failure, got nil", stage)
	}
	var se *StageError
	if !errors.As(err, &se) {
		t.Fatalf("error %v is not a *StageError", err)
	}
	if se.Stage != stage {
		t.Errorf("stage = %q, want %q (error: %v)", se.Stage, stage, err)
	}
	if !strings.HasPrefix(err.Error(), "repo_clone_failed: ") {
		t.Errorf("error %q lacks the terminal marker", err.Error())
	}
}

// newEmptyRemote builds a bare repo with no commits at all — the shape of a
// product repo the instant GitHub creates it, and the blueprint bootstrap's
// normal starting point ("an empty repo is ideal").
func newEmptyRemote(t *testing.T) string {
	t.Helper()
	requireGit(t)
	bare := filepath.Join(t.TempDir(), "empty.git")
	mustGit(t, "", "init", "--quiet", "--bare", "--initial-branch=main", bare)
	return bare
}

func TestGroundEmptyRemoteStartsUnbornAtRef(t *testing.T) {
	// A zero-commit remote has no refs for `--branch` to match; grounding must
	// still come up, with HEAD unborn at the bound ref so the first commit is
	// born on the branch the platform expects pushed. (Observed live: the
	// blueprint bootstrap on a fresh repo died at clone and the session hung
	// at "Agent coming online".)
	remote := newEmptyRemote(t)
	cfg := &Config{Remote: remote, FullName: "acme/newborn", Ref: "main"}
	dir, err := Ground(context.Background(), cfg, Options{WorkdirRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("Ground on an empty remote: %v", err)
	}
	if head := mustGit(t, dir, "symbolic-ref", "HEAD"); head != "refs/heads/main" {
		t.Errorf("HEAD = %q, want refs/heads/main (unborn)", head)
	}
	// The workflow's first commit + push must land on main upstream.
	mustGit(t, dir, "config", "user.email", "test@example.com")
	mustGit(t, dir, "config", "user.name", "Test")
	mustGit(t, dir, "commit", "--quiet", "--allow-empty", "-m", "first")
	mustGit(t, dir, "push", "--quiet", "origin", "main")
	if got := mustGit(t, "", "-C", remote, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Errorf("remote HEAD = %q, want main", got)
	}
}

func TestGroundEmptyRemoteCreatesUnbornTaskBranch(t *testing.T) {
	// Task runs carry a branch; on unborn history it starts unborn too rather
	// than failing on a start point that cannot resolve.
	remote := newEmptyRemote(t)
	cfg := &Config{Remote: remote, FullName: "acme/newborn", Ref: "main"}
	dir, err := Ground(context.Background(), cfg, Options{Branch: "agent/ORN-9-implementer", WorkdirRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("Ground: %v", err)
	}
	if head := mustGit(t, dir, "symbolic-ref", "HEAD"); head != "refs/heads/agent/ORN-9-implementer" {
		t.Errorf("HEAD = %q, want the unborn task branch", head)
	}
}
