package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

var (
	cloudCmd        = &cobra.Command{Use: "cloud", Short: "Manage Orun Cloud workspace linkage"}
	cloudBackendURL string
	cloudLinkOrg    string
	cloudLinkProj   string

	// cloudBrowserOpener is the browser opener used by `cloud open`; overridable
	// in tests.
	cloudBrowserOpener = cliauth.OpenBrowser
)

func registerCloudCommand(root *cobra.Command) {
	root.AddCommand(cloudCmd)
	cloudCmd.PersistentFlags().StringVar(&cloudBackendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")

	linkCmd := &cobra.Command{
		Use:   "link",
		Short: "Link the current repo to an Orun Cloud org/project",
		Long: `Link the current repo to an Orun Cloud org/project.

Detects the git remote, resolves it against the platform, and caches the
org/project link in ~/.orun/config.yaml. With no candidate links it presents an
org picker (creating the project on demand); with several it presents an
org/project picker. The non-interactive form skips all prompts:

  orun cloud link --org acme --project platform

The Orun CLI session from 'orun auth login' is used for authorization; no
GitHub PAT or OAuth token is required.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudLink()
		},
	}
	linkCmd.Flags().StringVar(&cloudLinkOrg, "org", "", "Org slug to link to (non-interactive)")
	linkCmd.Flags().StringVar(&cloudLinkProj, "project", "", "Project slug to link to (non-interactive; created on demand)")

	unlinkCmd := &cobra.Command{
		Use:   "unlink",
		Short: "Drop the local org/project link for the current repo",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudUnlink()
		},
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show the active org/project link for the current repo",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudStatus()
		},
	}

	openCmd := &cobra.Command{
		Use:   "open",
		Short: "Open the project's Orun Cloud console page in the browser",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudOpen()
		},
	}

	cloudCmd.AddCommand(linkCmd, unlinkCmd, statusCmd, openCmd)
}

// cloudSessionToken loads the CLI session and returns a fresh access token,
// failing fast and actionably when the user is not logged in (degradation
// table row 2: "run `orun auth login`").
func cloudSessionToken(ctx context.Context, backendURL string) (string, error) {
	creds, err := cliauth.LoadSession()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errNotLoggedIn()
		}
		return "", err
	}
	if creds == nil {
		return "", errNotLoggedIn()
	}
	if strings.TrimSpace(creds.BackendURL) != "" && !sameBackendURL(creds.BackendURL, backendURL) {
		return "", fmt.Errorf("stored Orun login targets %s; run `orun auth login --backend-url %s`", creds.BackendURL, backendURL)
	}
	tokenSrc := &remotestate.SessionTokenSource{BackendURL: backendURL, Version: version}
	token, err := tokenSrc.Token(ctx)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, cliauth.ErrSessionRevoked) {
			return "", errNotLoggedIn()
		}
		return "", err
	}
	return token, nil
}

func errNotLoggedIn() error {
	return fmt.Errorf("not logged in to Orun Cloud; run `orun auth login`")
}

func runCloudLink() error {
	backendURL, err := requireBackendURL(nil, cloudBackendURL)
	if err != nil {
		return err
	}
	repo, err := resolveRepoContext(backendURL)
	if err != nil {
		return err
	}
	if repo == nil || strings.TrimSpace(repo.GitRemote) == "" {
		return fmt.Errorf("could not detect a git remote for this workspace; remote-state requires a git remote")
	}

	color := ui.ColorEnabledForWriter(os.Stdout)

	// OSS single-tenant backend: it does not serve /cli/links. Short-circuit to
	// the fixed _local/_local scope (design §1 "one contract, two servers").
	if isOSSBackend(backendURL) {
		if err := persistLocalLink(backendURL, repo); err != nil {
			return err
		}
		fmt.Printf("%s linked %s → %s/%s (local backend)\n", ui.Green(color, "✓"), repo.GitRemote, localScopeSegment, localScopeSegment)
		fmt.Printf("  backend: %s\n", backendURL)
		return nil
	}

	ctx := context.Background()
	token, err := cloudSessionToken(ctx, backendURL)
	if err != nil {
		return err
	}
	client := cliauth.NewBackendClient(backendURL, version)

	link, err := resolveOrCreateLink(ctx, client, token, repo.GitRemote)
	if err != nil {
		return err
	}

	if err := persistWorkspaceLink(backendURL, repo, link); err != nil {
		return err
	}
	fmt.Printf("%s linked %s → %s/%s\n", ui.Green(color, "✓"), valueOrUnknown(link.RemoteURL), valueOrUnknown(link.OrgSlug), valueOrUnknown(link.ProjectSlug))
	fmt.Printf("  backend: %s\n", backendURL)
	return nil
}

// resolveOrCreateLink runs the OC2 scope-resolution flow against the platform:
// resolve the remote, then 0 candidates → org picker + create, 1 → use it,
// >1 → org/project picker. The non-interactive --org/--project form resolves or
// creates directly with no prompts.
func resolveOrCreateLink(ctx context.Context, client *cliauth.BackendClient, token, remoteURL string) (*cliauth.WorkspaceLink, error) {
	resolved, err := client.ResolveLinks(ctx, token, remoteURL)
	if err != nil {
		return nil, translateLinkAPIError(err, remoteURL)
	}

	// Non-interactive form: --org [--project] resolves/creates directly.
	if strings.TrimSpace(cloudLinkOrg) != "" {
		if link := matchExistingLink(resolved.Links, cloudLinkOrg, cloudLinkProj); link != nil {
			return link, nil
		}
		orgID := orgIDForSlug(resolved, cloudLinkOrg)
		if orgID == "" {
			// Fall back to the session orgs (resolve only returns candidates the
			// actor may link for that remote; --org may name a broader org).
			orgID = sessionOrgIDForSlug(cloudLinkOrg)
		}
		if orgID == "" {
			orgID = cloudLinkOrg // let the server resolve a slug it accepts
		}
		link, err := client.CreateLink(ctx, token, orgID, remoteURL, cloudLinkProj)
		if err != nil {
			return nil, translateLinkAPIError(err, remoteURL)
		}
		return link, nil
	}

	// Existing active links for this remote.
	switch len(resolved.Links) {
	case 1:
		return &resolved.Links[0], nil
	case 0:
		// No existing link: pick an org from the candidates (or session orgs)
		// and create one.
		return pickOrgAndCreate(ctx, client, token, remoteURL, resolved)
	default:
		// Multiple existing links: pick which org/project to use.
		idx, err := pickFromLinks("This repo has multiple linked org/projects. Choose one:", resolved.Links)
		if err != nil {
			return nil, err
		}
		return &resolved.Links[idx], nil
	}
}

// matchExistingLink returns an existing resolved link matching orgSlug (and
// projectSlug, when given), or nil.
func matchExistingLink(links []cliauth.WorkspaceLink, orgSlug, projectSlug string) *cliauth.WorkspaceLink {
	orgSlug = strings.TrimSpace(orgSlug)
	projectSlug = strings.TrimSpace(projectSlug)
	for i := range links {
		if !strings.EqualFold(links[i].OrgSlug, orgSlug) {
			continue
		}
		if projectSlug == "" || strings.EqualFold(links[i].ProjectSlug, projectSlug) {
			return &links[i]
		}
	}
	return nil
}

// orgIDForSlug returns the org ID for an org slug from the resolve candidates
// (or links), or "".
func orgIDForSlug(resolved *cliauth.ResolveLinksResponse, orgSlug string) string {
	orgSlug = strings.TrimSpace(orgSlug)
	for _, l := range append(append([]cliauth.WorkspaceLink{}, resolved.Candidates...), resolved.Links...) {
		if strings.EqualFold(l.OrgSlug, orgSlug) && strings.TrimSpace(l.OrgID) != "" {
			return l.OrgID
		}
	}
	return ""
}

// sessionOrgIDForSlug returns the org ID for an org slug from the cached
// session orgs, or "".
func sessionOrgIDForSlug(orgSlug string) string {
	orgSlug = strings.TrimSpace(orgSlug)
	creds, err := cliauth.LoadSession()
	if err != nil || creds == nil {
		return ""
	}
	for _, o := range creds.Orgs {
		if strings.EqualFold(o.Slug, orgSlug) && strings.TrimSpace(o.ID) != "" {
			return o.ID
		}
	}
	return ""
}

// pickOrgAndCreate presents the actor's candidate orgs (falling back to the
// session orgs) and creates a link in the chosen one.
func pickOrgAndCreate(ctx context.Context, client *cliauth.BackendClient, token, remoteURL string, resolved *cliauth.ResolveLinksResponse) (*cliauth.WorkspaceLink, error) {
	orgs := candidateOrgs(resolved)
	if len(orgs) == 0 {
		// Resolve returned no candidate orgs (the actor can still create a link
		// in an org they belong to per the session payload).
		orgs = sessionOrgs()
	}
	if len(orgs) == 0 {
		return nil, fmt.Errorf("no orgs available to link %s; run `orun auth login` again to refresh org access", remoteURL)
	}
	if !termIsInteractive() {
		return nil, fmt.Errorf("repo %s is not linked and no candidate is selected; re-run with `orun cloud link --org <slug> [--project <slug>]`", remoteURL)
	}
	var idx int
	var err error
	if len(orgs) == 1 {
		idx = 0
	} else {
		idx, err = pickFromOrgs("Choose an org to link this repo to:", orgs)
		if err != nil {
			return nil, err
		}
	}
	link, err := client.CreateLink(ctx, token, orgs[idx].ID, remoteURL, "")
	if err != nil {
		return nil, translateLinkAPIError(err, remoteURL)
	}
	return link, nil
}

// orgChoice is an org option in the picker.
type orgChoice struct {
	ID    string
	Slug  string
	Label string
}

// candidateOrgs returns the unique orgs from the resolve candidates.
func candidateOrgs(resolved *cliauth.ResolveLinksResponse) []orgChoice {
	seen := map[string]bool{}
	out := []orgChoice{}
	for _, c := range resolved.Candidates {
		key := c.OrgID + "|" + c.OrgSlug
		if c.OrgID == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, orgChoice{ID: c.OrgID, Slug: c.OrgSlug, Label: orgChoiceLabel(c.OrgSlug, c.OrgID)})
	}
	return out
}

// sessionOrgs returns the orgs from the cached session.
func sessionOrgs() []orgChoice {
	creds, err := cliauth.LoadSession()
	if err != nil || creds == nil {
		return nil
	}
	out := make([]orgChoice, 0, len(creds.Orgs))
	for _, o := range creds.Orgs {
		if strings.TrimSpace(o.ID) == "" {
			continue
		}
		out = append(out, orgChoice{ID: o.ID, Slug: o.Slug, Label: orgChoiceLabel(o.Slug, o.ID)})
	}
	return out
}

func orgChoiceLabel(slug, id string) string {
	if strings.TrimSpace(slug) != "" {
		return slug
	}
	return id
}

// pickFromOrgs prompts the user to select an org by number.
func pickFromOrgs(prompt string, orgs []orgChoice) (int, error) {
	labels := make([]string, len(orgs))
	for i, o := range orgs {
		labels[i] = o.Label
	}
	return promptChoice(prompt, labels)
}

// pickFromLinks prompts the user to select from existing org/project links.
func pickFromLinks(prompt string, links []cliauth.WorkspaceLink) (int, error) {
	labels := make([]string, len(links))
	for i, l := range links {
		labels[i] = fmt.Sprintf("%s/%s", valueOrUnknown(l.OrgSlug), valueOrUnknown(l.ProjectSlug))
	}
	return promptChoice(prompt, labels)
}

// promptChoice prints a numbered menu and reads a 1-based selection from stdin.
func promptChoice(prompt string, labels []string) (int, error) {
	if !termIsInteractive() {
		return 0, fmt.Errorf("multiple choices available but not running interactively; re-run with `orun cloud link --org <slug> [--project <slug>]`")
	}
	fmt.Fprintln(os.Stderr, prompt)
	for i, l := range labels {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, l)
	}
	fmt.Fprint(os.Stderr, "Selection [1]: ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > len(labels) {
		return 0, fmt.Errorf("invalid selection %q", line)
	}
	return n - 1, nil
}

func runCloudUnlink() error {
	backendURL, err := requireBackendURL(nil, cloudBackendURL)
	if err != nil {
		return err
	}
	repo, err := resolveRepoContext(backendURL)
	if err != nil {
		return err
	}
	if repo == nil || strings.TrimSpace(repo.GitRemote) == "" {
		return fmt.Errorf("could not detect a git remote for this workspace")
	}
	link, err := cliauth.FindRepoLink(backendURL, repo.GitRemote, repo.RepoFullName)
	if err != nil {
		return err
	}
	if link == nil {
		return fmt.Errorf("no local link for this repo on %s; nothing to unlink", backendURL)
	}
	if err := cliauth.RemoveRepoLink(backendURL, repo.GitRemote, repo.RepoFullName); err != nil {
		return err
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	fmt.Printf("%s unlinked %s (the server-side link is untouched)\n", ui.Green(color, "✓"), valueOrUnknown(repo.RepoFullName))
	return nil
}

func runCloudStatus() error {
	backendURL, _ := requireBackendURL(nil, cloudBackendURL)
	repo, err := resolveRepoContext(backendURL)
	if err != nil {
		return err
	}
	if repo == nil || strings.TrimSpace(repo.GitRemote) == "" {
		return fmt.Errorf("could not detect a git remote for this workspace")
	}
	link, err := cliauth.FindRepoLink(backendURL, repo.GitRemote, repo.RepoFullName)
	if err != nil {
		return err
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	fmt.Printf("Remote:      %s\n", valueOrUnknown(repo.GitRemote))
	fmt.Printf("Backend URL: %s\n", valueOrUnknown(backendURL))
	if link == nil || (strings.TrimSpace(link.OrgID) == "" && strings.TrimSpace(link.ProjectID) == "") {
		fmt.Printf("Link:        %s — run `orun cloud link`\n", ui.Yellow(color, "not linked"))
		return nil
	}
	fmt.Printf("Link:        %s\n", ui.Green(color, "linked"))
	fmt.Printf("Org:         %s\n", valueOrUnknown(linkOrgLabel(link)))
	fmt.Printf("Project:     %s\n", valueOrUnknown(linkProjectLabel(link)))
	if normalized := strings.TrimSpace(link.RepoID); normalized != "" && link.OrgID != localScopeSegment {
		fmt.Printf("Normalized:  %s\n", normalized)
	}
	return nil
}

func runCloudOpen() error {
	backendURL, err := requireBackendURL(nil, cloudBackendURL)
	if err != nil {
		return err
	}
	repo, err := resolveRepoContext(backendURL)
	if err != nil {
		return err
	}
	if repo == nil || strings.TrimSpace(repo.GitRemote) == "" {
		return fmt.Errorf("could not detect a git remote for this workspace")
	}
	link, err := cliauth.FindRepoLink(backendURL, repo.GitRemote, repo.RepoFullName)
	if err != nil {
		return err
	}
	if link == nil || strings.TrimSpace(link.OrgSlug) == "" || strings.TrimSpace(link.ProjectSlug) == "" {
		return fmt.Errorf("repo is not linked to an Orun Cloud org/project; run `orun cloud link` first")
	}
	consoleURL, err := consoleProjectURL(backendURL, link.OrgSlug, link.ProjectSlug)
	if err != nil {
		return err
	}
	fmt.Printf("Opening %s\n", consoleURL)
	if err := cloudBrowserOpener(consoleURL); err != nil {
		fmt.Fprintf(os.Stderr, "could not open a browser: %v\nVisit: %s\n", err, consoleURL)
	}
	return nil
}

// consoleProjectURL derives the console page for an org/project from the backend
// host: an `api.` host maps to `app.`; otherwise the host is used as-is. The
// path is /{orgSlug}/{projectSlug}.
func consoleProjectURL(backendURL, orgSlug, projectSlug string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(backendURL))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("cannot derive a console URL from backend %q", backendURL)
	}
	host := u.Host
	if strings.HasPrefix(host, "api.") {
		host = "app." + strings.TrimPrefix(host, "api.")
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/%s/%s", scheme, host,
		url.PathEscape(orgSlug), url.PathEscape(projectSlug)), nil
}

func linkOrgLabel(link *cliauth.RepoLink) string {
	if s := strings.TrimSpace(link.OrgSlug); s != "" {
		return s
	}
	return link.OrgID
}

func linkProjectLabel(link *cliauth.RepoLink) string {
	if s := strings.TrimSpace(link.ProjectSlug); s != "" {
		return s
	}
	return link.ProjectID
}
