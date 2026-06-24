package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/ui"
)

// runConnect authenticates the CLI and then auto-links the current repo. It is
// the engine behind `orun auth login`. Authentication is the primary job: once
// logged in, a linking problem is surfaced as a non-fatal hint, never a failed
// login.
//
// Note: there is deliberately no top-level `orun login` verb — that name is the
// OCI registry login (`orun login <registry>`, see command_publish.go). The
// unified cloud onboarding lives under `orun auth login`.
func runConnect(backendFlag string, device bool, orgSlug string, noLink bool) error {
	backendURL, err := requireBackendURL(loadIntentForCloudConfig(), backendFlag)
	if err != nil {
		return err
	}
	ctx := context.Background()
	var creds *cliauth.Credentials
	if device {
		creds, err = cliauth.DeviceLogin(ctx, backendURL, version, os.Stdout)
	} else {
		creds, err = cliauth.BrowserLogin(ctx, backendURL, version, os.Stdout, nil)
	}
	if err != nil {
		return err
	}

	color := ui.ColorEnabledForWriter(os.Stdout)
	fmt.Printf("%s logged in as %s\n", ui.Green(color, "✓"), valueOrUnknown(creds.DisplayUser()))
	fmt.Printf("  backend: %s\n", backendURL)
	if len(creds.Orgs) > 0 {
		fmt.Printf("  orgs: %s\n", formatOrgs(creds.Orgs))
	}
	if exp := creds.AccessExpiryTime(); !exp.IsZero() {
		fmt.Printf("  access token expires: %s\n", exp.Format(time.RFC3339))
	}

	if noLink {
		return nil
	}

	repo, link, created, linkErr := autoLinkRepo(ctx, backendURL, orgSlug, "")
	if repo == nil || (repo != nil && strings.TrimSpace(repo.GitRemote) == "") || isNoGitRemoteErr(linkErr) {
		// Not inside a git repo with a remote: nothing to link, and that's fine.
		fmt.Printf("  (no git remote here — run `orun auth login` inside a repo to link it)\n")
		return nil
	}
	if linkErr != nil {
		// Auth succeeded; linking is best-effort here. Surface the actionable
		// reason and exit 0 — `orun auth login --org <slug>` (or `orun cloud
		// link`) can complete it.
		fmt.Fprintf(os.Stderr, "%s logged in, but couldn't link this repo automatically: %v\n", ui.Yellow(color, "○"), linkErr)
		return nil
	}
	if link != nil {
		verb := "already linked"
		if created {
			verb = "linked"
		}
		fmt.Printf("%s %s this repo → %s\n", ui.Green(color, "✓"), verb, repoLinkLabel(link))
	}
	return nil
}

// autoLinkRepo ensures the current repo is linked to an org/project on
// backendURL, creating the link if needed. It is the self-healing core shared
// by `orun login`, `orun auth login`, and `orun run`:
//   - no git remote        → (repo, nil, false, nil): nothing to link
//   - already linked        → (repo, cachedLink, false, nil)
//   - OSS single-tenant     → link to the fixed _local/_local scope
//   - otherwise             → resolve the remote and link, auto-picking the org
//     when unambiguous (1 candidate), prompting once when interactive, or
//     erroring (naming --org) when ambiguous and non-interactive
//
// The created project is always named after the repo (empty projectSlug).
// Interactivity is taken from termIsInteractive() inside resolveOrCreateLinkFor.
func autoLinkRepo(ctx context.Context, backendURL, orgSlug, projectSlug string) (*repoContext, *cliauth.RepoLink, bool, error) {
	repo, err := resolveRepoContext(backendURL)
	if err != nil {
		return nil, nil, false, err
	}
	if repo == nil || strings.TrimSpace(repo.GitRemote) == "" {
		return repo, nil, false, nil
	}

	// Already linked: resolveRepoContext fills these from the cached RepoLink.
	if strings.TrimSpace(repo.OrgID) != "" || strings.TrimSpace(repo.ProjectID) != "" || strings.TrimSpace(repo.NamespaceID) != "" {
		existing, _ := cliauth.FindRepoLink(backendURL, repo.GitRemote, repo.RepoFullName)
		return repo, existing, false, nil
	}

	// OSS single-tenant backend: fixed _local/_local scope, no link API call
	// ("one contract, two servers").
	if isOSSBackend(backendURL) {
		if err := persistLocalLink(backendURL, repo); err != nil {
			return repo, nil, false, err
		}
		existing, _ := cliauth.FindRepoLink(backendURL, repo.GitRemote, repo.RepoFullName)
		return repo, existing, true, nil
	}

	token, err := cloudSessionToken(ctx, backendURL)
	if err != nil {
		return repo, nil, false, err
	}
	client := cliauth.NewBackendClient(backendURL, version)

	// In a non-interactive context the resolve flow must not block on a prompt;
	// resolveOrCreateLinkFor already errors (naming the override) when the org is
	// ambiguous and termIsInteractive() is false.
	link, err := resolveOrCreateLinkFor(ctx, client, token, repo.GitRemote, orgSlug, projectSlug)
	if err != nil {
		return repo, nil, false, err
	}
	if err := persistWorkspaceLink(backendURL, repo, link); err != nil {
		return repo, nil, false, err
	}
	existing, _ := cliauth.FindRepoLink(backendURL, repo.GitRemote, repo.RepoFullName)
	return repo, existing, true, nil
}

// isNoGitRemoteErr reports whether err is the "couldn't detect a git remote"
// failure from resolveRepoContext (e.g. running outside a git repo). Such a
// failure is not an error for login — there is simply nothing to link.
func isNoGitRemoteErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "detect git remote")
}

// repoLinkLabel renders a cached link as "org/repo", preferring slugs and
// falling back to IDs or the repo full name.
func repoLinkLabel(link *cliauth.RepoLink) string {
	if link == nil {
		return "(unknown)"
	}
	org := strings.TrimSpace(link.OrgSlug)
	if org == "" {
		org = strings.TrimSpace(link.OrgID)
	}
	repo := strings.TrimSpace(link.ProjectSlug)
	if repo == "" {
		repo = strings.TrimSpace(link.ProjectID)
	}
	if org != "" && repo != "" {
		return org + "/" + repo
	}
	if s := strings.TrimSpace(link.RepoFullName); s != "" {
		return s
	}
	return valueOrUnknown(repo)
}
