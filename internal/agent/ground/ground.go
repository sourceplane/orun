// Package ground turns a session's repository binding into a real checkout —
// the runtime half of grounded sessions (specs/orun-grounded-sessions
// design §1, GS0). `orun agent serve` reads the ORUN_REPO_* trio, clones
// bloblessly at the bound ref under $HOME/work/<repo-name>, creates or
// reuses the task-keyed branch, and hands the workdir to the driver brief.
//
// Every failure out of Ground is a *StageError whose message carries the
// distinct terminal marker (`repo_clone_failed: <stage>: <cause>`) the cloud
// failure path keys on — a grounded session must never silently degrade to
// an ungrounded one, so the caller treats any error here as terminal.
//
// Must-nots (design §4): no global git config mutation; no credential
// material in URLs, git config writes, or errors; no new event kinds —
// grounding progress rides plain log lines on the writer the caller
// provides.
package ground

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Config is the session's repository binding, read from create-time sandbox
// env (the cloud epic's env contract). ORUN_REPO_REMOTE alone decides
// groundedness; FullName names the workdir and Ref pins the clone.
type Config struct {
	Remote   string // ORUN_REPO_REMOTE — the https clone URL (never carries a credential)
	FullName string // ORUN_REPO_FULL_NAME — the owner/repo full name
	Ref      string // ORUN_REPO_REF — the branch or tag to clone at
}

// Detect reads the ORUN_REPO_* trio through getenv (os.Getenv in serve;
// injected for tests). Nil when ORUN_REPO_REMOTE is unset: the session is
// not grounded and the entire feature is inert — today's behavior,
// byte-identical.
func Detect(getenv func(string) string) *Config {
	remote := strings.TrimSpace(getenv("ORUN_REPO_REMOTE"))
	if remote == "" {
		return nil
	}
	return &Config{
		Remote:   remote,
		FullName: strings.TrimSpace(getenv("ORUN_REPO_FULL_NAME")),
		Ref:      strings.TrimSpace(getenv("ORUN_REPO_REF")),
	}
}

// Grounding stages, in boot order. Every failure out of Ground names exactly
// one of these, so the cloud sweep can say what broke without parsing git
// output.
const (
	StagePrepare  = "prepare"  // workdir derivation + resume-safety validation
	StageClone    = "clone"    // fresh blobless clone at the ref
	StageFetch    = "fetch"    // resume: refresh origin in a prior clone
	StageCheckout = "checkout" // resume: check out the existing task branch
	StageBranch   = "branch"   // create the task branch from the ref
)

// StageError tags a grounding failure with the stage that failed. Its
// message is the distinct terminal marker of design §1 step 7:
// `repo_clone_failed: <stage>: <cause>`.
type StageError struct {
	Stage string
	Err   error
}

func (e *StageError) Error() string {
	return fmt.Sprintf("repo_clone_failed: %s: %v", e.Stage, e.Err)
}

func (e *StageError) Unwrap() error { return e.Err }

func stageErr(stage string, err error) *StageError { return &StageError{Stage: stage, Err: err} }

// Options configures one grounding run.
type Options struct {
	// Branch is the task-keyed branch to create-or-reuse from the ref —
	// agent/<task>-<type>, the exact string serve already computes. Empty
	// skips the branch step (an interactive session stays on the ref).
	Branch string
	// WorkdirRoot overrides the checkout parent (default $HOME/work). Tests
	// point it at a temp dir.
	WorkdirRoot string
	// CredentialHelper, when set, rides the clone command itself as
	// repo-local config (--config credential.helper=…) — no global git state
	// and no window where a fetch could prompt (design §1 step 3). GS0
	// leaves it empty (a local or public remote needs no auth); GS1 wires
	// `!orun git-credential` here.
	CredentialHelper string
	// Log receives progress lines (serve's stderr). Nil = silent.
	Log io.Writer
}

// Ground performs design §1 steps 2–5: prepare the workdir under
// <root>/<repo-name>, clone bloblessly at the ref — or, when the dir is
// already a clone of the same remote, fetch and reuse it (the resume path,
// cloud design §10) — then create-or-reuse the task branch. Returns the
// checkout path. Every failure is a stage-tagged *StageError the caller must
// treat as terminal.
func Ground(ctx context.Context, cfg *Config, opts Options) (string, error) {
	if cfg == nil || cfg.Remote == "" {
		return "", stageErr(StagePrepare, fmt.Errorf("no repository binding (ORUN_REPO_REMOTE unset)"))
	}
	root := opts.WorkdirRoot
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", stageErr(StagePrepare, fmt.Errorf("resolve home dir: %w", err))
		}
		root = filepath.Join(home, "work")
	}
	name := repoName(cfg)
	if name == "" {
		return "", stageErr(StagePrepare, fmt.Errorf("cannot derive a repo name from ORUN_REPO_FULL_NAME=%q or the remote URL", cfg.FullName))
	}
	dir := filepath.Join(root, name)

	resume, err := classifyWorkdir(ctx, dir, cfg.Remote)
	if err != nil {
		return "", err // already stage-tagged (prepare)
	}
	if resume {
		logf(opts.Log, "orun agent serve: grounding — %s is a prior clone of the bound remote, fetching origin\n", dir)
		if _, err := runGit(ctx, dir, "fetch", "origin"); err != nil {
			return "", stageErr(StageFetch, err)
		}
	} else {
		if err := os.MkdirAll(root, 0o755); err != nil {
			return "", stageErr(StagePrepare, fmt.Errorf("create workdir root: %w", err))
		}
		// Blobless partial clone (full history, blobs on demand): agents want
		// `git log`/`blame`, and blob:none keeps boot fast on big repos.
		args := []string{"clone", "--filter=blob:none"}
		if opts.CredentialHelper != "" {
			args = append(args, "--config", "credential.helper="+opts.CredentialHelper)
		}
		if cfg.Ref != "" {
			args = append(args, "--branch", cfg.Ref)
		}
		args = append(args, cfg.Remote, dir)
		logf(opts.Log, "orun agent serve: grounding — cloning %s (blobless) at %s into %s\n",
			redactCredentials(cfg.Remote), refOrDefault(cfg.Ref), dir)
		if _, err := runGit(ctx, "", args...); err != nil {
			// A zero-commit repository has no refs, so `--branch <ref>` cannot
			// match — and a fresh product repo is the blueprint bootstrap's
			// normal starting point ("an empty repo is ideal"). Confirm the
			// remote really is refless before rerouting; any other clone
			// failure keeps its original error.
			if cfg.Ref == "" || !remoteIsEmpty(ctx, cfg.Remote, opts.CredentialHelper) {
				return "", stageErr(StageClone, err)
			}
			logf(opts.Log, "orun agent serve: grounding — %s is an empty repository, cloning refless and starting %s unborn\n",
				redactCredentials(cfg.Remote), cfg.Ref)
			// git removes the clone target on failure; clear defensively in
			// case a partial dir survived, then clone without the ref pin.
			_ = os.RemoveAll(dir)
			args = []string{"clone", "--filter=blob:none"}
			if opts.CredentialHelper != "" {
				args = append(args, "--config", "credential.helper="+opts.CredentialHelper)
			}
			args = append(args, cfg.Remote, dir)
			if _, err := runGit(ctx, "", args...); err != nil {
				return "", stageErr(StageClone, err)
			}
			// Aim the unborn HEAD at the bound ref so the very first commit is
			// born on the branch the platform expects to see pushed.
			if _, err := runGit(ctx, dir, "symbolic-ref", "HEAD", "refs/heads/"+cfg.Ref); err != nil {
				return "", stageErr(StageClone, err)
			}
		}
	}

	if opts.Branch != "" {
		if branchExists(ctx, dir, opts.Branch) {
			// Reuse, never clobber: a resumed session's task branch may carry
			// commits the agent already made — check it out as-is, no reset
			// back to the ref.
			if _, err := runGit(ctx, dir, "checkout", opts.Branch); err != nil {
				return "", stageErr(StageCheckout, err)
			}
			logf(opts.Log, "orun agent serve: grounding — task branch %s exists, reused\n", opts.Branch)
		} else if remoteBranchExists(ctx, dir, opts.Branch) {
			// saas-session-continuity CT3: a branch a previous session pushed
			// but this box has never seen. A fresh clone holds it only as
			// origin/<branch>; re-creating it from the ref would start the
			// task over from the default branch (the first orun.work app
			// rebuilt a milestone that way). Check it out, tracking origin.
			if _, err := runGit(ctx, dir, "checkout", "-b", opts.Branch, "--track", "origin/"+opts.Branch); err != nil {
				return "", stageErr(StageCheckout, err)
			}
			logf(opts.Log, "orun agent serve: grounding — task branch %s found on origin, checked out\n", opts.Branch)
		} else {
			args := []string{"checkout", "-b", opts.Branch}
			// The start point must resolve locally; on an empty remote the
			// bound ref is an unborn HEAD, and the task branch starts unborn
			// from it. (A bad ref on a real repo already failed the clone.)
			if cfg.Ref != "" && refResolves(ctx, dir, cfg.Ref) {
				args = append(args, cfg.Ref)
			}
			if _, err := runGit(ctx, dir, args...); err != nil {
				return "", stageErr(StageBranch, err)
			}
			logf(opts.Log, "orun agent serve: grounding — task branch %s created from %s\n",
				opts.Branch, refOrDefault(cfg.Ref))
		}
	}
	return dir, nil
}

// classifyWorkdir applies the resume-safety rule (cloud design §10): absent
// or empty ⇒ fresh clone (false); a git repo whose origin matches the bound
// remote ⇒ resume (true); anything else non-empty is refused — grounding
// must never adopt a directory it does not own.
func classifyWorkdir(ctx context.Context, dir, remote string) (resume bool, err error) {
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return false, nil
		}
		return false, stageErr(StagePrepare, fmt.Errorf("inspect workdir %s: %w", dir, rerr))
	}
	if len(entries) == 0 {
		return false, nil
	}
	if _, serr := os.Stat(filepath.Join(dir, ".git")); serr != nil {
		return false, stageErr(StagePrepare, fmt.Errorf("workdir %s is non-empty but not a git repository — refusing to reuse it", dir))
	}
	origin, gerr := runGit(ctx, dir, "remote", "get-url", "origin")
	if gerr != nil {
		return false, stageErr(StagePrepare, fmt.Errorf("workdir %s has no origin remote — refusing to reuse it", dir))
	}
	if origin != remote {
		return false, stageErr(StagePrepare, fmt.Errorf("workdir %s tracks a different remote (%s) than the bound repository — refusing to reuse it",
			dir, redactCredentials(origin)))
	}
	return true, nil
}

// branchExists reports whether the local task branch is already present (a
// prior boot of this session created it).
func branchExists(ctx context.Context, dir, branch string) bool {
	_, err := runGit(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// remoteBranchExists reports whether origin has the branch (as fetched into
// this clone).
func remoteBranchExists(ctx context.Context, dir, branch string) bool {
	_, err := runGit(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch)
	return err == nil
}

// refResolves reports whether the ref names a commit in the local clone.
func refResolves(ctx context.Context, dir, ref string) bool {
	_, err := runGit(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err == nil
}

// remoteIsEmpty reports whether the remote has no refs at all — a freshly
// created repository. The listing rides the same credential helper as the
// clone; any listing failure reads as NOT empty, so the clone's own error
// stays the one reported.
func remoteIsEmpty(ctx context.Context, remote, credentialHelper string) bool {
	var args []string
	if credentialHelper != "" {
		args = append(args, "-c", "credential.helper="+credentialHelper)
	}
	args = append(args, "ls-remote", "--heads", "--tags", remote)
	out, err := runGit(ctx, "", args...)
	return err == nil && strings.TrimSpace(out) == ""
}

// runGit runs `git -C <dir> <args…>` (plain `git <args…>` when dir is empty —
// the clone creates its own directory) and returns trimmed stdout. A failure
// carries git's trimmed stderr, credential-redacted; stage tagging is the
// caller's job.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	cmd := exec.CommandContext(ctx, "git", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := redactCredentials(strings.TrimSpace(stderr.String())); msg != "" {
			return "", fmt.Errorf("git %s: %w (stderr: %s)", args[0], err, msg)
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// repoName derives the checkout directory name: the last path segment of the
// full name, falling back to the remote URL's last segment when
// ORUN_REPO_FULL_NAME did not arrive. ".git" suffixes are trimmed.
func repoName(cfg *Config) string {
	if n := lastSegment(cfg.FullName); n != "" {
		return n
	}
	return lastSegment(cfg.Remote)
}

func lastSegment(s string) string {
	s = strings.Trim(strings.TrimSpace(s), "/")
	if i := strings.LastIndexAny(s, "/:"); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSuffix(s, ".git")
}

func refOrDefault(ref string) string {
	if ref == "" {
		return "the remote default branch"
	}
	return ref
}

func logf(w io.Writer, format string, args ...any) {
	if w != nil {
		fmt.Fprintf(w, format, args...)
	}
}

// credURL matches userinfo in an http(s) URL (https://user:pass@host/…) —
// nothing GS0 ever writes into a URL, but git may echo one back in stderr
// and the invariant is absolute: no credential material in errors.
var credURL = regexp.MustCompile(`(https?://)[^/@\s]+@`)

func redactCredentials(s string) string { return credURL.ReplaceAllString(s, "${1}") }
