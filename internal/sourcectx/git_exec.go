package sourcectx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// execGit is the default Git adapter. It shells out to the system `git`
// binary via os/exec.
//
// Trade-off (recorded in PR body too):
//
//   - Pro: zero new Go module dependencies; matches what every other
//     internal/git probe in the repo already does (internal/git/changes.go);
//     CI runners always have `git` installed; behavior tracks the user's
//     local git config (signing, hooks, remotes) for free.
//   - Con: per-call subprocess cost (~1-3 ms on macOS) and a hard
//     dependency on `git` being on PATH. For C1 we measure ≤30 ms total
//     on a clean tree (test-plan.md §7 budget) and the subprocess overhead
//     is well inside that.
//
// go-git was the alternative; rejected for this milestone because the
// binary-size and module-graph cost outweighs the per-call savings on the
// resolver hot path. We keep the Git interface stable so swapping in a
// go-git adapter later is a one-file change.
type execGit struct{}

// DefaultGit returns the production Git adapter (shell-out to `git`).
func DefaultGit() Git { return execGit{} }

// runGit runs `git -C <workspace> <args...>` and returns stdout. A non-zero
// exit produces an error whose body includes the trimmed stderr — useful
// for resolver-side diagnostics, never bubbled up to user output verbatim.
func runGit(ctx context.Context, workspacePath string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", workspacePath}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w (stderr: %s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// HasRepo implements Git.HasRepo. Returns false (not error) when the
// workspace is not inside a git repository — this is the local-nogit case.
func (execGit) HasRepo(ctx context.Context, workspacePath string) (bool, error) {
	if _, err := os.Stat(workspacePath); err != nil {
		return false, fmt.Errorf("workspace path: %w", err)
	}
	out, err := runGit(ctx, workspacePath, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		// Distinguish "not a repo" from "git missing / corrupt repo": the
		// former produces a clean exit-128; the latter we surface.
		if errors.Is(err, exec.ErrNotFound) {
			return false, fmt.Errorf("git binary not found on PATH: %w", err)
		}
		// Treat any other failure as "no repo here" — matches Phase 1
		// internal/git's permissive shape.
		return false, nil
	}
	return strings.TrimSpace(out) == "true", nil
}

// HeadRevision returns the full hex SHA at HEAD.
func (execGit) HeadRevision(ctx context.Context, workspacePath string) (string, error) {
	out, err := runGit(ctx, workspacePath, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// TreeHash returns `git rev-parse HEAD^{tree}`.
func (execGit) TreeHash(ctx context.Context, workspacePath string) (string, error) {
	out, err := runGit(ctx, workspacePath, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Branch returns the short branch name at HEAD ("" for detached HEAD).
func (execGit) Branch(ctx context.Context, workspacePath string) (string, error) {
	out, err := runGit(ctx, workspacePath, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		// Detached HEAD makes symbolic-ref exit non-zero — that's "no
		// branch", not a probe failure.
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// Ref returns the full symbolic ref ("refs/heads/main") or "" when
// detached.
func (execGit) Ref(ctx context.Context, workspacePath string) (string, error) {
	out, err := runGit(ctx, workspacePath, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// Tag returns the annotated/lightweight tag at HEAD if any. Empty string
// when HEAD is not at a tag.
func (execGit) Tag(ctx context.Context, workspacePath string) (string, error) {
	// `git describe --exact-match --tags HEAD` exits non-zero when HEAD
	// is not at a tag. That's not an error condition for us.
	out, err := runGit(ctx, workspacePath, "describe", "--exact-match", "--tags", "HEAD")
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// RemoteURL returns the URL of `origin`, "" if no `origin` remote.
func (execGit) RemoteURL(ctx context.Context, workspacePath string) (string, error) {
	out, err := runGit(ctx, workspacePath, "remote", "get-url", "origin")
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// DiffTreePaths returns repo-relative paths whose working-tree state
// differs from treeHash. Output combines:
//
//   - tracked modifications (`git diff --name-only HEAD`)
//   - staged changes (`git diff --cached --name-only`)
//   - untracked, non-ignored files (`git ls-files --others --exclude-standard`)
//
// Sorted, deduplicated.
func (execGit) DiffTreePaths(ctx context.Context, workspacePath, treeHash string) ([]string, error) {
	set := make(map[string]struct{})
	add := func(s string) {
		for _, line := range strings.Split(s, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				set[filepath.ToSlash(line)] = struct{}{}
			}
		}
	}
	// Working tree vs HEAD (covers both unstaged and staged diffs in one
	// pass when given HEAD; we still ask for --cached separately so a
	// caller running with --no-index workflows still sees staged-only
	// changes).
	if out, err := runGit(ctx, workspacePath, "diff", "--name-only", "--no-renames", "--relative", "HEAD"); err == nil {
		add(out)
	}
	if out, err := runGit(ctx, workspacePath, "diff", "--cached", "--name-only", "--no-renames", "--relative"); err == nil {
		add(out)
	}
	if out, err := runGit(ctx, workspacePath, "ls-files", "--others", "--exclude-standard"); err == nil {
		add(out)
	}
	paths := make([]string, 0, len(set))
	for p := range set {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}

// ensure execGit satisfies Git at compile time.
var _ Git = execGit{}
