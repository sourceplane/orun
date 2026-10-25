package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/provenance"
)

// THE CHECK A BOOTSTRAP LANDING WAITS FOR, RUN WHERE THE TREE IS
// (orun.pr/land@v1 `checks: local`).
//
// A bootstrap's pull request is checked by its product's CI `plan` job: the
// intent validates, every component and composition resolves, and the plan
// for the pull request compiles. A GitHub runner took ~25 s to say so — the
// queue, a checkout, installing orun, the plan — about a tree that sits in the
// box that just wrote it, where the same commands take under a second. Twice
// per todo build, once per landing.
//
// So `checks: local` runs them here, on the commit the landing is about to
// push, and merges without waiting for CI:
//
//   - `orun validate` — the intent and every component manifest;
//   - `orun plan` as the pull request plans it, from a pull_request event
//     made for this commit, so the trigger binding the product's CI would
//     match is the one planned, whatever the intent names it;
//   - `orun plan` as the merge's push to the base plans it — the convergence
//     the bootstrap watches next. The PR's CI never compiled that one.
//
// The commands are this binary run as a child, not the pipelines in process:
// they write to the process's stdout, and a build's progress stream — which a
// background hook may be writing to at the same moment — must not carry them.
//
// The PR's own CI still runs, and nothing waits for it. A base branch that
// REQUIRES a check refuses the early merge; the landing then waits for CI as
// `checks: ci` does (provenance LandRequest.RequiredCheckTimeout).

// landingEvent is the commit a landing is about to push, as the product's CI
// will be told about it.
type landingEvent struct {
	repo    string // owner/name, as GitHub names it in the event
	base    string // the branch the PR merges into
	baseSHA string
	headRef string // the PR's branch
	headSHA string
}

// localCheck checks a landing in the box and says what it checked; orunBinary
// is the orun it runs. Variables, so tests can stand in for either.
var (
	localCheck = checkInBox
	orunBinary = os.Executable
)

// localCheckTimeout bounds the whole check. The first plan of a build may pull
// the product's compositions from their registry; everything after is local.
const localCheckTimeout = 5 * time.Minute

func checkInBox(ctx context.Context, dir string, ev landingEvent) (string, error) {
	intent := ""
	for _, name := range []string{"intent.yaml", "intent.yml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			intent = name
			break
		}
	}
	if intent == "" {
		// No intent, no orun CI: there is nothing for a check to plan, here or
		// on a runner.
		return "no intent to plan", nil
	}
	exe, err := orunBinary()
	if err != nil {
		return "", fmt.Errorf("checking the landing here: finding orun: %w", err)
	}
	tmp, err := os.MkdirTemp("", "orun-land-check-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	ctx, cancel := context.WithTimeout(ctx, localCheckTimeout)
	defer cancel()

	run := func(what, eventName string, args ...string) error {
		cmd := exec.CommandContext(ctx, exe, args...)
		cmd.Dir = dir
		cmd.Env = checkEnv(os.Environ(), eventName)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("the landing's own check failed — %s: %v\n%s", what, err, lastLines(string(out), 25))
		}
		return nil
	}
	if err := run("orun validate", "", "validate", "--intent", intent); err != nil {
		return "", err
	}

	events := []struct {
		name, label string
		payload     map[string]any
	}{
		{"pull_request", "the pull request", map[string]any{
			"action": "opened",
			"pull_request": map[string]any{
				"base": map[string]any{"ref": ev.base, "sha": ev.baseSHA},
				"head": map[string]any{"ref": ev.headRef, "sha": ev.headSHA},
			},
			"repository": map[string]any{"full_name": ev.repo},
		}},
		{"push", "the merge to " + ev.base, map[string]any{
			"ref":        "refs/heads/" + ev.base,
			"before":     ev.baseSHA,
			"after":      ev.headSHA,
			"repository": map[string]any{"full_name": ev.repo},
		}},
	}
	var said []string
	for _, e := range events {
		eventFile := filepath.Join(tmp, e.name+".json")
		planFile := filepath.Join(tmp, e.name+"-plan.json")
		raw, _ := json.Marshal(e.payload)
		if err := os.WriteFile(eventFile, raw, 0o600); err != nil {
			return "", err
		}
		if err := run("orun plan for "+e.label, e.name,
			"plan", "--from-ci", "github", "--event-file", eventFile,
			"--intent", intent, "--changed", "--output", planFile); err != nil {
			return "", err
		}
		said = append(said, fmt.Sprintf("%s plans %s", e.label, jobsIn(planFile)))
	}
	return "validated; " + strings.Join(said, ", "), nil
}

// checkEnv is the environment the check's commands run in: the build's own,
// told which GitHub event it is reading, and with every other GITHUB_ value
// removed — a check run inside some other workflow must not read that
// workflow's event, outputs or run as its own.
func checkEnv(env []string, eventName string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "GITHUB_") && k != "GITHUB_TOKEN" {
			continue
		}
		out = append(out, kv)
	}
	if eventName != "" {
		out = append(out, "GITHUB_EVENT_NAME="+eventName)
	}
	return out
}

// jobsIn says how many jobs a written plan holds ("10 jobs"), or "a plan"
// when the file cannot be read — the plan compiled either way.
func jobsIn(planFile string) string {
	raw, err := os.ReadFile(planFile)
	if err != nil {
		return "a plan"
	}
	var plan struct {
		Jobs []json.RawMessage `json:"jobs"`
	}
	if json.Unmarshal(raw, &plan) != nil {
		return "a plan"
	}
	if len(plan.Jobs) == 1 {
		return "1 job"
	}
	return fmt.Sprintf("%d jobs", len(plan.Jobs))
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// landingEventFor reads the commit a landing is about to push. The PR's
// branch is the one the pen will name it (provenance.Pen.Open).
func landingEventFor(ctx context.Context, g gitDir, base, task, slug, title string) (landingEvent, error) {
	ev := landingEvent{base: base}
	var err error
	if ev.headSHA, err = g.run(ctx, "rev-parse", "HEAD"); err != nil {
		return ev, err
	}
	if ev.baseSHA, err = g.run(ctx, "rev-parse", "--verify", "-q", "refs/heads/"+base); err != nil {
		if ev.baseSHA, err = g.run(ctx, "rev-parse", "--verify", "-q", "refs/remotes/origin/"+base); err != nil {
			return ev, fmt.Errorf("checking the landing here: no %s to compare it with: %w", base, err)
		}
	}
	current, _ := g.run(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	switch {
	case provenance.TaskKeyOfBranch(current) == task:
		ev.headRef = current
	case slug != "":
		ev.headRef = "orun/" + task + "-" + slug
	default:
		ev.headRef = provenance.BranchName(task, title)
	}
	if remote, err := g.run(ctx, "remote", "get-url", "origin"); err == nil {
		ev.repo = repoOfRemote(remote)
	}
	return ev, nil
}

// repoOfRemote is owner/name for a github.com remote, "" for anything else.
func repoOfRemote(remote string) string {
	s := strings.TrimSuffix(strings.TrimSpace(remote), ".git")
	for _, prefix := range []string{"git@github.com:", "https://github.com/", "ssh://git@github.com/"} {
		if strings.HasPrefix(s, prefix) {
			return strings.TrimPrefix(s, prefix)
		}
	}
	return ""
}
