package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/remotestate"
)

// `orun baseline new --via-platform` — ask the platform to build
// (orun-bootstrap-engine BE-O7b, over orun-cloud's bootstrap door).
//
// # What this is, and what it very much is not
//
// It is a REQUEST. Every decision stays on the server, where it already lives:
// the human-admin requirement, the paid gate, readiness, the repo grounding,
// the one-build-per-repository lease, and the time-boxed admin grant the build
// holds and loses when it ends. A CLI that re-derived any of them would be a
// second answer to a question the platform already answers, and the two would
// drift the first time either moved.
//
// What it is not is a small command. It writes an entire product into somebody
// else's repository over about an hour, creating branches, merging pull
// requests and applying terraform against a real cloud account. That is why
// the repository is resolved and PRINTED before anything starts, why an
// ambiguous match refuses rather than picks, and why the session URL is the
// last thing on stdout: the next thing the operator does is watch it.

type platformBuildOpts struct {
	backendURL string
	workspace  string
	repo       string
	repoLink   string
	profileID  string
	sets       []string
	valuesFile string
}

func runBaselineViaPlatform(cmd *cobra.Command, address string, o platformBuildOpts) error {
	ctx := cmd.Context()
	client, err := cloudClient(ctx, o.backendURL, o.workspace)
	if err != nil {
		return err
	}
	org := client.Scope().OrgID

	// The baseline first, because a typo in the id should cost nothing and
	// should certainly not reach the repository-resolution step.
	view, err := client.GetBlueprint(ctx, org, address)
	if err != nil {
		return fmt.Errorf("orun baseline new: %w", err)
	}
	b := view.Blueprint
	if view.PinnedTagStale {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"you asked for %s; the registry now publishes %s, and that is what this builds\n",
			view.PinnedTag, b.Tag)
	}

	// READINESS IS A GATE, and the same one `--local` applies. A bootstrap
	// that starts without its providers does not fail at the door; it fails
	// thirty minutes in, having created a repo and half a product, and the
	// operator reads a Cloudflare error instead of "you never connected
	// Cloudflare". The door checks this too — that is the boundary — and
	// checking here means the answer arrives before anything is created.
	if !b.Readiness.IntegrationsReady {
		var missing []string
		for _, p := range b.Readiness.Integrations {
			if !p.Connected {
				missing = append(missing, p.Provider)
			}
		}
		return exitErr(1, "orun baseline new: %s cannot be built by this workspace yet — %s %s not connected.\n"+
			"Connect in the console, then re-run. Nothing has been started.",
			b.ID, strings.Join(missing, ", "), pluralize(len(missing), "is", "are"))
	}

	// WHERE IT LANDS. `--repo-link` is the explicit form and wins; otherwise
	// the repository is named or inferred and resolved to exactly one link.
	linkID := strings.TrimSpace(o.repoLink)
	target := ""
	if linkID == "" {
		link, err := repoLinkFor(ctx, client, o.repo, ".")
		if err != nil {
			return exitErr(1, "orun baseline new: %v", err)
		}
		linkID, target = link.ID, link.RepoFullName
	}

	inputs, err := platformInputs(o.valuesFile, o.sets)
	if err != nil {
		return err
	}

	// PRINTED BEFORE IT STARTS. An hour of automated commits into the wrong
	// repository is not recoverable by pressing ctrl-c afterwards.
	if target != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "building %s@%s into %s (%s)\n", b.ID, b.Tag, target, linkID)
	} else {
		fmt.Fprintf(cmd.ErrOrStderr(), "building %s@%s into %s\n", b.ID, b.Tag, linkID)
	}

	req := remotestate.BootstrapRequest{RepoLinkID: linkID}
	if len(inputs) > 0 {
		req.Inputs = inputs
	}
	if p := strings.TrimSpace(o.profileID); p != "" {
		req.ProfileID = p
	}
	res, err := client.Bootstrap(ctx, org, address, req)
	if err != nil {
		// Verbatim. The door refuses a build for reasons that name a plan, a
		// missing input, or the person already building into this repository,
		// and every one of those is more useful than "bootstrap failed".
		return fmt.Errorf("orun baseline new: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s\n", res.SessionID)
	if strings.TrimSpace(res.SessionURL) != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n", res.SessionURL)
	}
	return nil
}

// platformInputs reads the same `--values` file and `--set` overrides
// `--local` reads, through the SAME function, so the two shapes cannot
// disagree about what an input file means.
func platformInputs(valuesFile string, sets []string) (map[string]string, error) {
	prevFile, prevSet := scaffoldValuesFile, scaffoldSet
	scaffoldValuesFile, scaffoldSet = valuesFile, sets
	defer func() { scaffoldValuesFile, scaffoldSet = prevFile, prevSet }()
	return collectScaffoldInputs()
}
