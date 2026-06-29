package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

// cloudCheckOrg overrides the org for `orun cloud check` (flag highest in the
// flag > env > intent > cached-link precedence).
var cloudCheckOrg string

// registerCloudCheck adds the `orun cloud check` pre-flight to the cloud group.
// It answers "is this repo allow-listed for the resolved org?" before CI runs,
// turning a mysterious CI 404 into a one-command local diagnosis
// (specs/oidc-ci-tenancy §4.2).
func registerCloudCheck(parent *cobra.Command) {
	checkCmd := &cobra.Command{
		Use:   "check",
		Short: "Check whether this repo is allow-listed for the resolved org",
		Long: `Pre-flight the CI credential-free path.

'orun cloud check' resolves the workspace the way a run does (--workspace/--org >
ORUN_WORKSPACE/ORUN_ORG > intent.yaml execution.state.workspace > the cached
link), lists that workspace's allow-list (GET /v1/organizations/{org}/cli/links),
and reports whether THIS repo is on it.

Run it from a dev machine before wiring up CI: a repo that is not allow-listed
will 404 on the OIDC path (resource-hiding), and this turns that into a clear,
actionable message instead of a mysterious CI failure.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudCheck()
		},
	}
	checkCmd.Flags().StringVar(&cloudCheckOrg, "workspace", "", "Workspace slug/id to check against (overrides intent + cached link)")
	checkCmd.Flags().StringVar(&cloudCheckOrg, "org", "", "Alias of --workspace (legacy spelling)")
	parent.AddCommand(checkCmd)
}

func runCloudCheck() error {
	backendURL, err := requireBackendURL(loadIntentForCloudConfig(), cloudBackendURL)
	if err != nil {
		return err
	}
	repo, err := resolveRepoContext(backendURL)
	if err != nil {
		return err
	}
	if repo == nil || strings.TrimSpace(repo.GitRemote) == "" {
		return fmt.Errorf("could not detect a git remote for this workspace; the allow-list is keyed on the repo")
	}

	// The OSS single-tenant backend has no /cli/links route — there is no
	// allow-list to consult; it is always "allowed" for its _local scope.
	if isOSSBackend(backendURL) {
		color := ui.ColorEnabledForWriter(os.Stdout)
		fmt.Printf("%s %s targets a local backend (%s); no allow-list to check\n",
			ui.Green(color, "✓"), valueOrUnknown(repo.RepoFullName), backendURL)
		return nil
	}

	intentOrg, _, _ := intentScope(loadIntentForCloudConfig())
	var linkOrgID string
	if link, lerr := cliauth.FindRepoLink(backendURL, repo.GitRemote, repo.RepoFullName); lerr == nil && link != nil {
		linkOrgID = strings.TrimSpace(link.OrgID)
	}
	org := resolveCheckOrg(cloudCheckOrg, intentOrg, linkOrgID)
	if org == "" {
		return fmt.Errorf("no workspace to check against; pass --workspace, set ORUN_WORKSPACE, or declare `execution.state.workspace` in intent.yaml")
	}

	ctx := context.Background()
	token, err := cloudSessionToken(ctx, backendURL)
	if err != nil {
		return err
	}
	client := cliauth.NewBackendClient(backendURL, version)
	links, err := client.ListOrgLinks(ctx, token, org)
	if err != nil {
		// Resource-hiding: a 404 here means the org is unknown or the actor is
		// not a member — never over-claim "not allow-listed" from it.
		var apiErr *cliauth.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 404 {
			return fmt.Errorf("cannot list the allow-list for org %q: it does not exist or you are not a member; check the org or run `orun auth login` again", org)
		}
		return fmt.Errorf("listing the allow-list for org %q: %w", org, err)
	}

	color := ui.ColorEnabledForWriter(os.Stdout)
	if matched := matchRepoInLinks(repo, links); matched != nil {
		fmt.Printf("%s %s is allow-listed for org %s\n",
			ui.Green(color, "✓"), valueOrUnknown(repo.RepoFullName), valueOrUnknown(orgLabelForLink(matched, org)))
		if slug := strings.TrimSpace(matched.ProjectSlug); slug != "" {
			fmt.Printf("  project: %s\n", slug)
		}
		return nil
	}
	fmt.Printf("%s %s isn't allow-listed for org %s\n",
		ui.Yellow(color, "✗"), valueOrUnknown(repo.RepoFullName), org)
	fmt.Fprintln(os.Stderr, addRepoHint(repo.RepoFullName, org))
	return errRepoNotAllowListed(repo.RepoFullName, org)
}

// resolveCheckOrg resolves the org to check with the flag > env > intent >
// cached-link precedence, mapping a slug to an org id via the cached session
// when possible (the listing path prefers the org_… id).
func resolveCheckOrg(flagOrg, intentOrg, linkOrgID string) string {
	cand := strings.TrimSpace(flagOrg)
	if cand == "" {
		// ORUN_WORKSPACE leads; ORUN_ORG is the retained alias (saas-workspaces A4).
		cand = preferWorkspace(os.Getenv(workspaceEnvVar), os.Getenv(orgEnvVar))
	}
	if cand == "" {
		cand = strings.TrimSpace(intentOrg)
	}
	if cand == "" {
		cand = strings.TrimSpace(linkOrgID)
	}
	if cand != "" && !strings.HasPrefix(cand, "org_") {
		if id := sessionOrgIDForSlug(cand); id != "" {
			return id
		}
	}
	return cand
}

// matchRepoInLinks returns the allow-list entry for this repo, or nil. It
// matches on the server's canonical normalized remote (which ends in
// owner/repo) so a slug rename or scheme/.git difference does not cause a false
// negative.
func matchRepoInLinks(repo *repoContext, links []cliauth.WorkspaceLink) *cliauth.WorkspaceLink {
	full := strings.ToLower(strings.TrimSpace(repo.RepoFullName))
	for i := range links {
		remote := strings.ToLower(strings.TrimSpace(links[i].RemoteURL))
		if remote == "" {
			continue
		}
		if remote == full || strings.HasSuffix(remote, "/"+full) {
			return &links[i]
		}
	}
	return nil
}

// orgLabelForLink prefers the link's org slug for display, falling back to the
// resolved org identifier.
func orgLabelForLink(link *cliauth.WorkspaceLink, fallback string) string {
	if link != nil && strings.TrimSpace(link.OrgSlug) != "" {
		return link.OrgSlug
	}
	return fallback
}

// addRepoHint is the actionable "add your repo" guidance shown on a genuine
// allow-list miss (specs/oidc-ci-tenancy §4.2/§4.4).
func addRepoHint(repoFullName, org string) string {
	return fmt.Sprintf("hint: add %s from the console (Git Repos → add from GitHub) or run `orun cloud link` from a dev machine",
		valueOrUnknown(repoFullName))
}

// errRepoNotAllowListed is the non-zero exit for a confirmed allow-list miss.
func errRepoNotAllowListed(repoFullName, org string) error {
	return fmt.Errorf("%s is not allow-listed for org %s", valueOrUnknown(repoFullName), org)
}

// disambiguateRepoDenial upgrades a generic not-linked/denied error into the
// actionable "add your repo" message ONLY when a session-authorized listing
// confirms the repo is genuinely absent from the org allow-list. It is
// best-effort and resource-hiding-safe: with no usable session (e.g. the CI
// workflow token), a denied/empty listing, or any error, it returns the
// fallback unchanged — never over-claiming "forbidden"/"not allow-listed" from
// a bare 404 (specs/oidc-ci-tenancy §4.2, decision D4).
func disambiguateRepoDenial(ctx context.Context, backendURL string, repo *repoContext, orgID string, fallback error) error {
	if repo == nil || strings.TrimSpace(repo.RepoFullName) == "" || strings.TrimSpace(orgID) == "" {
		return fallback
	}
	token, err := cloudSessionToken(ctx, backendURL)
	if err != nil {
		return fallback // no session (CI/workflow token) — cannot consult the listing.
	}
	client := cliauth.NewBackendClient(backendURL, version)
	links, err := client.ListOrgLinks(ctx, token, orgID)
	if err != nil || len(links) == 0 {
		return fallback // listing denied/empty — degrade to the generic message.
	}
	if matchRepoInLinks(repo, links) != nil {
		return fallback // the repo IS allow-listed; the 404 is something else.
	}
	return fmt.Errorf("%s isn't allow-listed for org %s — add it from the console (Git Repos → add from GitHub) or run `orun cloud link`",
		valueOrUnknown(repo.RepoFullName), orgID)
}
