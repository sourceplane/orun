package actions

import (
	"context"
	"fmt"
	"time"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/provenance"
)

// orun.pr/land@v1 — open the task's PR through the pen and land it.
//
// This is the whole of what a baseline's phase does between applying its slice
// and watching the convergence, and it is the action that retires the largest
// piece of shell: branch through the grammar, push, body carrying the manifest,
// wait for checks, merge, return to a pulled base.
//
// It calls the SAME `provenance.Pen` the `orun pr open` command calls, so the
// command and the action cannot diverge — there is one implementation of the
// pen's gesture and two ways to ask for it.
func init() {
	register(Spec{
		ID:      "orun.pr/land@v1",
		Summary: "Open the task's PR through the provenance pen and merge it once checks settle",
		Params: []Param{
			{Name: "task", Type: ParamString, Required: true,
				Description: "the task key this landing closes — a PR opens FOR a task"},
			{Name: "title", Type: ParamString,
				Description: "PR title (default: the task key)"},
			{Name: "branchSlug", Type: ParamString,
				Description: "slug half of orun/<task>-<slug>, verbatim; [a-z0-9-]"},
			{Name: "base", Type: ParamString, Default: "main",
				Description: "base branch, and the branch to return to"},
			{Name: "body", Type: ParamString,
				Description: "prose half of the PR body; the manifest block is appended"},
			{Name: "epic", Type: ParamString,
				Description: "the epic this task belongs to, recorded in the manifest"},
			{Name: "draft", Type: ParamBool, Default: false,
				Description: "open as a draft (it is then not merged)"},
			{Name: "wait", Type: ParamBool, Default: true,
				Description: "wait for checks before merging; false when a later convergence is the real gate"},
			{Name: "checkTimeoutSeconds", Type: ParamInt, Default: 1800,
				Description: "how long to wait for checks to settle"},
			{Name: "mergeMethod", Type: ParamString, Default: "squash",
				Description: "squash | merge | rebase"},
		},
		Outputs: []string{"branch", "number", "url", "merged", "mergeSha"},
	}, runPRLand)
}

func runPRLand(ctx context.Context, in Input) (Result, error) {
	pen := &provenance.Pen{Workdir: in.Dir, Token: cliauth.GitHubTokenFromEnv}

	opened, err := pen.Open(ctx, provenance.OpenRequest{
		TaskKey:    StringParam(in, "task"),
		Title:      StringParam(in, "title"),
		Base:       StringParam(in, "base"),
		BranchSlug: StringParam(in, "branchSlug"),
		Draft:      BoolParam(in, "draft"),
		Prose:      StringParam(in, "body"),
		Manifest: provenance.Manifest{
			Version: provenance.ManifestVersion,
			Epic:    StringParam(in, "epic"),
		},
	})
	if err != nil {
		return Result{}, err
	}

	out := map[string]string{
		"branch":   opened.Branch,
		"number":   fmt.Sprint(opened.Number),
		"url":      opened.URL,
		"merged":   "false",
		"mergeSha": "",
	}
	// No credential: the pen prepared everything and printed the compare URL.
	// That is honest for a human at a terminal and useless to an unattended
	// bootstrap, which cannot click it — so say so rather than reporting a
	// landing that did not happen.
	if !opened.Opened {
		return Result{Outputs: out}, fmt.Errorf(
			"orun.pr/land@v1: branch %s is pushed but no PR was opened (no GitHub credential ambient) — open it at %s",
			opened.Branch, opened.CompareURL)
	}
	// A draft is deliberately not merged: opening one says "not yet".
	if BoolParam(in, "draft") {
		return Result{Outputs: out}, nil
	}

	var timeout time.Duration
	if BoolParam(in, "wait") {
		timeout = time.Duration(IntParam(in, "checkTimeoutSeconds")) * time.Second
	}
	landed, err := pen.Land(ctx, provenance.LandRequest{
		Number:       opened.Number,
		Base:         StringParam(in, "base"),
		CheckTimeout: timeout,
		MergeMethod:  StringParam(in, "mergeMethod"),
	})
	if landed != nil && landed.Merged {
		out["merged"] = "true"
		out["mergeSha"] = landed.MergeSHA
	}
	if err != nil {
		return Result{Outputs: out}, err
	}
	return Result{Outputs: out}, nil
}
