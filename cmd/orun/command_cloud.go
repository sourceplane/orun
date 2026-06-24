package main

import (
	"bufio"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
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
		Short: "Link this repo to an Orun Cloud org (advanced; auth login auto-links)",
		Long: `Link the current repo to an Orun Cloud org.

Most users never need this: 'orun auth login' already connects and links this
repo automatically — the repo is the project, named after the git repo. Use
'orun cloud link' to choose a specific org when you belong to several, to
re-link, or to override the name.

Detects the git remote, resolves it against the platform, and caches the link
in ~/.orun/config.yaml. The non-interactive form skips all prompts:

  orun cloud link --org acme

The Orun CLI session from 'orun auth login' is used for authorization; no
GitHub PAT or OAuth token is required.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudLink()
		},
	}
	linkCmd.Flags().StringVar(&cloudLinkOrg, "org", "", "Org slug to link this repo under (non-interactive)")
	linkCmd.Flags().StringVar(&cloudLinkProj, "project", "", "Repo name to link under (advanced; defaults to the git repo name)")

	unlinkCmd := &cobra.Command{
		Use:   "unlink",
		Short: "Drop the local Orun Cloud link for the current repo",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudUnlink()
		},
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show the active Orun Cloud link for the current repo",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudStatus()
		},
	}

	openCmd := &cobra.Command{
		Use:   "open",
		Short: "Open this repo's Orun Cloud console page in the browser",
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
	backendURL, err := requireBackendURL(loadIntentForCloudConfig(), cloudBackendURL)
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

// resolveOrCreateLink is the `orun cloud link` entrypoint: it runs the
// scope-resolution flow using the --org/--project flags. The auto-link path
// (`orun login`/`orun run`) calls resolveOrCreateLinkFor directly with explicit
// org/project args.
func resolveOrCreateLink(ctx context.Context, client *cliauth.BackendClient, token, remoteURL string) (*cliauth.WorkspaceLink, error) {
	return resolveOrCreateLinkFor(ctx, client, token, remoteURL, cloudLinkOrg, cloudLinkProj)
}

// resolveOrCreateLinkFor runs the OC2 scope-resolution flow against the
// platform: resolve the remote, then 0 candidates → org picker + create, 1 →
// use it, >1 → org/project picker. A non-empty orgSlug (and optional
// projectSlug) resolves or creates directly with no prompts. The created
// project is always named after the repo (empty projectSlug ⇒ the server
// derives it from the remote — "a project is a repo").
func resolveOrCreateLinkFor(ctx context.Context, client *cliauth.BackendClient, token, remoteURL, orgSlug, projectSlug string) (*cliauth.WorkspaceLink, error) {
	resolved, err := client.ResolveLinks(ctx, token, remoteURL)
	if err != nil {
		return nil, translateLinkAPIError(err, remoteURL)
	}

	// Non-interactive form: orgSlug [+ projectSlug] resolves/creates directly.
	if strings.TrimSpace(orgSlug) != "" {
		if link := matchExistingLink(resolved.Links, orgSlug, projectSlug); link != nil {
			return link, nil
		}
		orgID := orgIDForSlug(resolved, orgSlug)
		if orgID == "" {
			// Fall back to the session orgs (resolve only returns candidates the
			// actor may link for that remote; orgSlug may name a broader org).
			orgID = sessionOrgIDForSlug(orgSlug)
		}
		if orgID == "" {
			orgID = orgSlug // let the server resolve a slug it accepts
		}
		link, err := client.CreateLink(ctx, token, orgID, remoteURL, projectSlug)
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
		idx, err := pickFromLinks("This repo is linked in multiple orgs. Choose one:", resolved.Links)
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
		// UO2: a brand-new user belongs to no org. Rather than dead-ending,
		// materialize a personal org so their first login lands a working
		// org/repo, then link under it (no prompt — there's exactly one org).
		org, err := materializePersonalOrg(ctx, client, token)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(os.Stderr, "%s created your personal org %s\n", ui.Green(ui.ColorEnabledForWriter(os.Stderr), "✓"), valueOrUnknown(org.Label))
		orgs = []orgChoice{org}
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

// materializePersonalOrg creates a personal org for the actor when they belong
// to none (UO2). On a slug collision (e.g. another user shares the handle) it
// retries once with a short random suffix.
func materializePersonalOrg(ctx context.Context, client *cliauth.BackendClient, token string) (orgChoice, error) {
	name, slug := personalOrgIdentity()
	org, err := client.CreateOrg(ctx, token, name, slug)
	if err != nil {
		var apiErr *cliauth.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 409 {
			org, err = client.CreateOrg(ctx, token, name, slug+"-"+shortToken())
		}
		if err != nil {
			return orgChoice{}, fmt.Errorf("could not create a personal org to link this repo: %w", err)
		}
	}
	return orgChoice{ID: org.ID, Slug: org.Slug, Label: orgChoiceLabel(org.Slug, org.ID)}, nil
}

// personalOrgIdentity derives a display name and slug for the actor's personal
// org from the cached session: GitHub login, else email local-part, else
// display name, else "my".
func personalOrgIdentity() (name, slug string) {
	handle := "my"
	if creds, err := cliauth.LoadSession(); err == nil && creds != nil {
		switch {
		case strings.TrimSpace(creds.GitHubLogin) != "":
			handle = strings.TrimSpace(creds.GitHubLogin)
		case strings.TrimSpace(creds.User.Email) != "":
			handle = strings.SplitN(strings.TrimSpace(creds.User.Email), "@", 2)[0]
		case strings.TrimSpace(creds.User.DisplayName) != "":
			handle = strings.TrimSpace(creds.User.DisplayName)
		}
	}
	return handle, slugifyOrg(handle)
}

// slugifyOrg renders s as a valid org slug (lowercase alnum collapsed by single
// hyphens, 2..63 chars), falling back to a random slug when nothing usable
// remains. Mirrors the server's slug rules so the first create attempt is
// accepted without a round-trip.
func slugifyOrg(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) < 2 {
		slug = "org-" + shortToken()
	}
	if len(slug) > 63 {
		slug = strings.Trim(slug[:63], "-")
	}
	return slug
}

// shortToken returns a short random hex token for slug disambiguation.
func shortToken() string {
	buf := make([]byte, 3)
	if _, err := cryptorand.Read(buf); err != nil {
		return "x1y2"
	}
	return hex.EncodeToString(buf)
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
	backendURL, err := requireBackendURL(loadIntentForCloudConfig(), cloudBackendURL)
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
	backendURL, _ := requireBackendURL(loadIntentForCloudConfig(), cloudBackendURL)
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
		fmt.Printf("Link:        %s — run `orun auth login`\n", ui.Yellow(color, "not connected"))
		return nil
	}
	fmt.Printf("Link:        %s\n", ui.Green(color, "linked"))
	fmt.Printf("Org:         %s\n", valueOrUnknown(linkOrgLabel(link)))
	fmt.Printf("Repo:        %s\n", valueOrUnknown(linkProjectLabel(link)))
	if normalized := strings.TrimSpace(link.RepoID); normalized != "" && link.OrgID != localScopeSegment {
		fmt.Printf("Normalized:  %s\n", normalized)
	}
	return nil
}

func runCloudOpen() error {
	backendURL, err := requireBackendURL(loadIntentForCloudConfig(), cloudBackendURL)
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
		return fmt.Errorf("this repo isn't connected to Orun Cloud yet; run `orun auth login` first")
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
