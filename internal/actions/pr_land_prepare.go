package actions

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// What the pen needs before it can land anything (orun.pr/land@v1).
//
// The pen's gesture is branch, push, open, merge. It commits nothing: `orun pr
// open` is for a person who has already committed. The action is called by a
// bootstrap whose phase has just WRITTEN files, and until this existed nothing
// between the write and the pen turned them into a commit — so the first real
// landing, into a repository `git init` had just created, died on its first
// question:
//
//	✕ hook "land" (orun.pr/land@v1): git rev-parse --abbrev-ref HEAD:
//	  exit status 128: fatal: ambiguous argument 'HEAD': unknown revision
//
// The pen's own tests answered that question with a fake git that always said
// "main", which is how an action that had never landed a byte shipped as the
// one that lands every phase. The shell it replaced — cirrus's land-pr.sh and
// its phase-01 repo step — did three things first, each learnt live, and this
// does the same three:
//
//   - stage everything and commit it on a SCRATCH branch, so local main never
//     runs ahead of origin (a squash merge would then refuse the fast-forward
//     pull that returns the product to a clean base);
//   - on a repository with no history, give the base branch a first commit and
//     push it, because GitHub cannot open a pull request into a branch that
//     does not exist — the phase's work then lands as PR #1, bound to its task;
//   - land nothing, successfully, when there is nothing to land, so re-running
//     a phase that already landed is a no-op rather than an empty PR.

// landing is what prepareLanding found and made.
type landing struct {
	// nothing: the tree holds nothing the base does not already have.
	nothing bool
	// scratch is the local branch the commit was made on. The pen checks its
	// grammar branch out from it, after which it is deleted.
	scratch string
	// seeded: the base branch was born on the remote by this call.
	seeded bool
}

// gitDir runs git in one directory.
type gitDir string

func (g gitDir) run(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"-C", string(g)}, args...)
	out, err := exec.CommandContext(ctx, "git", full...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// ok is a question git answers with its exit status.
func (g gitDir) ok(ctx context.Context, args ...string) bool {
	_, err := g.run(ctx, args...)
	return err == nil
}

// identity supplies a committer only when the environment has none. A fresh
// container has no user.email, and git refuses to invent one; a developer's
// own identity is never overridden.
func (g gitDir) identity(ctx context.Context) []string {
	if email, err := g.run(ctx, "config", "user.email"); err == nil && email != "" {
		return nil
	}
	return []string{"-c", "user.name=orun", "-c", "user.email=orun-bootstrap@users.noreply.github.com"}
}

func prepareLanding(ctx context.Context, g gitDir, base, title, task string) (landing, error) {
	if !g.ok(ctx, "remote", "get-url", "origin") {
		return landing{}, fmt.Errorf("the product has no `origin` remote, so there is nowhere to land — orun.repo/ensure@v1 wires it, and must run before this hook")
	}
	if _, err := g.run(ctx, "add", "-A"); err != nil {
		return landing{}, err
	}
	id := g.identity(ctx)

	var out landing
	if !g.ok(ctx, "rev-parse", "--verify", "-q", "HEAD") {
		seeded, err := seedBase(ctx, g, base, id)
		if err != nil {
			return landing{}, err
		}
		out.seeded = seeded
	}

	if g.ok(ctx, "diff", "--cached", "--quiet") {
		// Nothing staged. Either the phase already landed — the ordinary
		// re-run — or an earlier attempt committed and then failed before its
		// pull request merged, in which case that commit is still the landing.
		current, err := g.run(ctx, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return landing{}, err
		}
		if current == base || landedOn(ctx, g, base) {
			out.nothing = true
		}
		return out, nil
	}

	out.scratch = "orun-landing/" + task
	if _, err := g.run(ctx, "checkout", "-q", "-B", out.scratch); err != nil {
		return landing{}, err
	}
	commit := append(append([]string{}, id...), "commit", "-q", "-m", title, "-m", "Orun-Task: "+task)
	if _, err := g.run(ctx, commit...); err != nil {
		return landing{}, err
	}
	return out, nil
}

// seedBase gives an unborn repository its base branch.
//
// `git init` names the unborn branch after init.defaultBranch — `master` on a
// stock git — so it is renamed to the base first. Then:
//
//   - origin already has the base (the repository was pre-created, or an
//     earlier attempt seeded it): adopt it, leaving the index as staged, so the
//     phase's files land ON it. A pre-created repository that holds files this
//     product does not is refused rather than landed over — that landing would
//     delete them, and nothing about "create the product" asked for that;
//   - origin has nothing: commit the EMPTY tree and push it. Empty rather than
//     a README, because the action cannot know what a product's first file is,
//     and an empty first commit makes the whole product PR #1.
func seedBase(ctx context.Context, g gitDir, base string, id []string) (bool, error) {
	if _, err := g.run(ctx, "symbolic-ref", "HEAD", "refs/heads/"+base); err != nil {
		return false, err
	}
	heads, err := g.run(ctx, "ls-remote", "--heads", "origin", base)
	if err != nil {
		return false, fmt.Errorf("reading origin before the first landing: %w", err)
	}
	if strings.TrimSpace(heads) != "" {
		if _, err := g.run(ctx, "fetch", "-q", "origin", base); err != nil {
			return false, err
		}
		if _, err := g.run(ctx, "update-ref", "refs/heads/"+base, "FETCH_HEAD"); err != nil {
			return false, err
		}
		gone, err := g.run(ctx, "diff", "--cached", "--diff-filter=D", "--name-only")
		if err != nil {
			return false, err
		}
		if gone != "" {
			return false, fmt.Errorf("origin/%s already holds files this product does not (%s) — landing would delete them; a product repository is pre-created EMPTY (no README, no licence)",
				base, strings.Join(strings.Fields(gone), ", "))
		}
		return false, nil
	}
	tree, err := g.run(ctx, "hash-object", "-w", "-t", "tree", "--stdin")
	if err != nil {
		return false, err
	}
	seed, err := g.run(ctx, append(append([]string{}, id...), "commit-tree", tree, "-m", "Initial commit")...)
	if err != nil {
		return false, err
	}
	if _, err := g.run(ctx, "update-ref", "refs/heads/"+base, seed); err != nil {
		return false, err
	}
	if _, err := g.run(ctx, "push", "-q", "-u", "origin", base); err != nil {
		return false, fmt.Errorf("pushing the first commit of %s: %w", base, err)
	}
	return true, nil
}

// landedOn reports whether HEAD is already part of origin's base.
func landedOn(ctx context.Context, g gitDir, base string) bool {
	if !g.ok(ctx, "fetch", "-q", "origin", base) {
		return false
	}
	return g.ok(ctx, "merge-base", "--is-ancestor", "HEAD", "FETCH_HEAD")
}

// wireOrigin points a product's `origin` at the repository orun.repo/ensure@v1
// just found or made.
//
// The docs for that action always said it "wires origin, and pushes"; it did
// neither, so the landing after it had nowhere to push. Only a directory that
// is ITSELF a git repository is touched — a product placed inside some other
// checkout must not have that checkout's remotes rewritten — and an origin
// that already exists is never retargeted.
//
// SSH unless a token is in the environment, which is the rule the shell it
// replaces arrived at live: gh's OAuth token has no `workflow` scope, and
// GitHub refuses a push that adds .github/workflows/ over it ("refusing to
// allow an OAuth App to create or update workflow … without `workflow`
// scope"), so a developer's machine pushes over SSH. A headless run carries a
// token that can (a fine-grained PAT needs only contents:write), and its
// environment supplies the https credential.
func wireOrigin(ctx context.Context, dir, owner, name string) (string, error) {
	if dir == "" {
		return "", nil
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return "", nil
	}
	g := gitDir(dir)
	if url, err := g.run(ctx, "remote", "get-url", "origin"); err == nil {
		return url, nil
	}
	url := originURL(owner, name)
	if _, err := g.run(ctx, "remote", "add", "origin", url); err != nil {
		return "", err
	}
	return url, nil
}

func originURL(owner, name string) string {
	if strings.TrimSpace(os.Getenv("GITHUB_TOKEN")) != "" || strings.TrimSpace(os.Getenv("GH_TOKEN")) != "" {
		return "https://github.com/" + owner + "/" + name + ".git"
	}
	return "git@github.com:" + owner + "/" + name + ".git"
}
