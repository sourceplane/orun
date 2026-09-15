package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/remotestate"
)

// Which repository a platform-run build writes into
// (orun-bootstrap-engine BE-O7b).
//
// BE-O7 deferred `--via-platform` for one reason, in its own words: it "needs a
// repo link id the CLI has no verb to resolve yet". A person standing in a
// repository knows the repository; the platform knows the `repl_…` that names
// it. The resolution is a list and a name match, and it belongs in the binary
// rather than in somebody's clipboard.
//
// # Why this refuses rather than picks
//
// The chosen repository is where an hour of automated commits lands, so the
// one thing this must never do is guess. A workspace can hold two links to one
// repository — GitHub resolves `Acme/Storefront` and `acme/storefront` to the
// same place, and orun-cloud's build lease is keyed lowercased for exactly
// that reason — so an ambiguous match is reported with both ids rather than
// resolved by ordering.

// resolveRepoLink finds the link for `repoFullName` among a workspace's links.
//
// Matching is case-insensitive because GitHub is: a link stored as
// `Acme/Storefront` and a remote that says `acme/storefront` are the same
// repository, and a case-sensitive match would send somebody to link a
// repository that is already linked.
func resolveRepoLink(links []remotestate.RepoLink, repoFullName string) (*remotestate.RepoLink, error) {
	want := strings.ToLower(strings.TrimSpace(repoFullName))
	if want == "" {
		return nil, fmt.Errorf("no repository named")
	}
	var matched []remotestate.RepoLink
	for _, l := range links {
		if strings.ToLower(strings.TrimSpace(l.RepoFullName)) == want {
			matched = append(matched, l)
		}
	}
	switch len(matched) {
	case 0:
		return nil, fmt.Errorf(
			"no active repository link for %s in this workspace.\n"+
				"A platform-run build writes into a repository the workspace has LINKED — link it on the\n"+
				"repository's Git tab, then re-run.%s", repoFullName, nearby(links, want))
	case 1:
		l := matched[0]
		// AGENT ACCESS IS A CEILING, and `off` means this repository is linked
		// for everything EXCEPT this. The door refuses it too — that is the
		// boundary — and saying it here costs no round trip and names the
		// switch to flip.
		if strings.EqualFold(strings.TrimSpace(l.AgentAccess), "off") {
			return nil, fmt.Errorf(
				"agent access is off for %s — a build cannot write to it.\n"+
					"Turn it on on the repository's Git tab, then re-run.", l.RepoFullName)
		}
		return &l, nil
	default:
		ids := make([]string, 0, len(matched))
		for _, l := range matched {
			ids = append(ids, l.ID)
		}
		sort.Strings(ids)
		// NEVER pick. This chooses where an hour of automated commits lands,
		// and "the first one" is not a reason.
		return nil, fmt.Errorf(
			"this workspace has %d active links to %s (%s).\n"+
				"Pass --repo-link with the one you mean: a build writes an entire product into it,\n"+
				"and picking for you is not something this can get wrong quietly.",
			len(matched), repoFullName, strings.Join(ids, ", "))
	}
}

// nearby names a few of the links that DO exist, because "no link for
// acme/storefront" plus silence sends somebody to the console to read a list
// this command already has in hand.
func nearby(links []remotestate.RepoLink, want string) string {
	if len(links) == 0 {
		return "\nThis workspace has no linked repositories at all."
	}
	names := make([]string, 0, len(links))
	for _, l := range links {
		if strings.ToLower(l.RepoFullName) != want {
			names = append(names, l.RepoFullName)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	if len(names) > 5 {
		names = append(names[:5], fmt.Sprintf("and %d more", len(names)-5))
	}
	return "\nLinked here: " + strings.Join(names, ", ")
}

// repoLinkFor turns the repository a caller named — or the one they are
// standing in — into the id the bootstrap door takes.
func repoLinkFor(
	ctx context.Context,
	client *remotestate.Client,
	repoFlag, dir string,
) (*remotestate.RepoLink, error) {
	repo := strings.TrimSpace(repoFlag)
	if repo == "" {
		// The repository this checkout points at. Convenience, not magic: it
		// is printed before anything starts, and `--repo` overrides it.
		remote, err := currentGitRemoteURL(dir)
		if err != nil {
			return nil, fmt.Errorf(
				"--repo is required here: this is not a git checkout with an origin remote, so there is\n" +
					"nothing to infer the target repository from")
		}
		repo = parseGitHubRepoFullName(remote)
		if repo == "" {
			return nil, fmt.Errorf(
				"--repo is required here: origin is %q, which is not a GitHub repository this can name", remote)
		}
	}
	links, err := client.ListRepoLinks(ctx, client.Scope().OrgID)
	if err != nil {
		return nil, fmt.Errorf("reading this workspace's repository links: %w", err)
	}
	return resolveRepoLink(links, repo)
}
