package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/loader"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/remotestate"
)

type repoContext struct {
	GitRemote    string
	RepoFullName string
	NamespaceID  string
	OrgID        string
	OrgSlug      string
	ProjectID    string
	ProjectSlug  string
}

// backendURLDeprecationWarned guards the one-line deprecation notice so it is
// emitted at most once per process.
var backendURLDeprecationWarned bool

// resolveBackendURLWithConfig resolves the backend URL with the precedence in
// design §8: flag > env > repo intent > user config. The user-config layer
// prefers cloud.url and falls back to the deprecated backend.url alias (with a
// one-line warning).
func resolveBackendURLWithConfig(intent *model.Intent, explicit string) string {
	if u := strings.TrimSpace(explicit); u != "" {
		return u
	}
	if u := strings.TrimSpace(os.Getenv(backendURLEnvVar)); u != "" {
		return u
	}
	if intent != nil && strings.TrimSpace(intent.Execution.State.BackendURL) != "" {
		return strings.TrimSpace(intent.Execution.State.BackendURL)
	}
	cfg, err := cliauth.LoadConfig()
	if err == nil && cfg != nil {
		if cfg.UsesDeprecatedBackendURL() {
			warnBackendURLDeprecated()
		}
		return cfg.ResolvedBackendURL()
	}
	return ""
}

// warnBackendURLDeprecated prints the one-line deprecation notice for the
// backend.url config alias (design §8), at most once.
func warnBackendURLDeprecated() {
	if backendURLDeprecationWarned {
		return
	}
	backendURLDeprecationWarned = true
	fmt.Fprintln(os.Stderr, "warning: ~/.orun/config.yaml `backend.url` is deprecated; rename it to `cloud.url`")
}

// resolveScope resolves the org/project scope for a remote-state call with the
// precedence in specs/oidc-ci-tenancy §4.1: --org/--project flags >
// ORUN_ORG/ORUN_PROJECT env > intent execution.state.org/project > the cached
// RepoLink's org/project. Empty fields are filled from the next source; the
// remotestate client defaults any still-empty field to the OSS _local scope.
func resolveScope(flagOrg, flagProject, intentOrg, intentProject, linkOrg, linkProject string) remotestate.Scope {
	scope := remotestate.Scope{
		OrgID:     strings.TrimSpace(flagOrg),
		ProjectID: strings.TrimSpace(flagProject),
	}
	if scope.OrgID == "" {
		// ORUN_WORKSPACE is the leading spelling; ORUN_ORG is the retained alias
		// (read either, prefer workspace — saas-workspaces A4).
		scope.OrgID = preferWorkspace(os.Getenv(workspaceEnvVar), os.Getenv(orgEnvVar))
	}
	if scope.ProjectID == "" {
		scope.ProjectID = strings.TrimSpace(os.Getenv(projectEnvVar))
	}
	if scope.OrgID == "" {
		scope.OrgID = strings.TrimSpace(intentOrg)
	}
	if scope.ProjectID == "" {
		scope.ProjectID = strings.TrimSpace(intentProject)
	}
	if scope.OrgID == "" {
		scope.OrgID = strings.TrimSpace(linkOrg)
	}
	if scope.ProjectID == "" {
		scope.ProjectID = strings.TrimSpace(linkProject)
	}
	return scope
}

// intentScope extracts the declared org/project and the strict-mode flag from a
// loaded intent's execution.state. requireOrg is implied true whenever org is
// declared (specs/oidc-ci-tenancy §4.1, decision D2): declaring the tenancy IS
// the request to enforce it.
func intentScope(intent *model.Intent) (org, project string, requireOrg bool) {
	if intent == nil {
		return "", "", false
	}
	s := intent.Execution.State
	// execution.state.workspace is the leading spelling; execution.state.org is
	// the retained alias (read either, prefer workspace — saas-workspaces A4).
	org = preferWorkspace(s.Workspace, s.Org)
	project = strings.TrimSpace(s.Project)
	requireOrg = s.RequireOrg || org != ""
	return org, project, requireOrg
}

// preferWorkspace returns the first non-blank of the Workspace-spelled and the
// legacy org-spelled value, trimmed, preferring the Workspace spelling when both
// are set (saas-workspaces A4: read either, prefer workspace). The two are
// aliases for one underlying tenancy value, so a sane config sets at most one.
func preferWorkspace(workspace, org string) string {
	if w := strings.TrimSpace(workspace); w != "" {
		return w
	}
	return strings.TrimSpace(org)
}

// errOrgRequired is the strict-mode fail-fast for a non-interactive remote op
// that resolved no org while org enforcement is on (intent declared an org or
// set requireOrg: true). It points at the committed knob rather than silently
// exchanging an empty claim into an ambiguous scope (specs/oidc-ci-tenancy
// §4.1).
func errOrgRequired() error {
	return fmt.Errorf("no workspace resolved but tenancy enforcement is on; declare `execution.state.workspace` in intent.yaml (or pass --workspace / set ORUN_WORKSPACE)")
}

// enforceRequireOrg applies strict mode: when requireOrg is on and no org
// resolved, return the actionable fail-fast error. It is a no-op when an org
// resolved or enforcement is off, so the OIDC/exchange path (which resolves the
// org server-side from a declared, non-empty scope) is never blocked.
func enforceRequireOrg(requireOrg bool, resolvedOrg string) error {
	if requireOrg && strings.TrimSpace(resolvedOrg) == "" {
		return errOrgRequired()
	}
	return nil
}

// loadIntentForCloudConfig reads execution.state (backend URL / mode) from the
// discovered intent.yaml for the auth and cloud command groups, which do not
// otherwise compile the intent. It is a raw best-effort parse
// (loader.LoadIntent, not a full component resolve) so that a repo whose
// components don't fully resolve can still contribute its declared backendUrl;
// execution.state is read identically either way. Any read/parse error yields
// nil and backend-URL resolution falls through to the flag/env/user-config
// layers (preserving prior behavior outside a repo).
func loadIntentForCloudConfig() *model.Intent {
	path := strings.TrimSpace(intentFile)
	if path == "" {
		return nil
	}
	intent, err := loader.LoadIntent(path)
	if err != nil {
		return nil
	}
	return intent
}

func requireBackendURL(intent *model.Intent, explicit string) (string, error) {
	backendURL := resolveBackendURLWithConfig(intent, explicit)
	if backendURL == "" {
		return "", fmt.Errorf("missing backend URL; pass --backend-url, set ORUN_BACKEND_URL, configure intent.yaml execution.state.backendUrl, or set ~/.orun/config.yaml backend.url")
	}
	return backendURL, nil
}

func resolveRepoContext(backendURL string) (*repoContext, error) {
	remoteURL, err := currentGitRemoteURL(storeDir())
	if err != nil {
		return nil, err
	}
	repoFullName := parseGitHubRepoFullName(remoteURL)
	ctx := &repoContext{
		GitRemote:    remoteURL,
		RepoFullName: repoFullName,
	}
	if repoFullName == "" {
		return ctx, nil
	}
	link, err := cliauth.FindRepoLink(backendURL, remoteURL, repoFullName)
	if err != nil {
		return nil, err
	}
	if link != nil {
		nsID := strings.TrimSpace(link.NamespaceID)
		// Invalidate canonical (non-local) namespace IDs cached from old backend versions.
		// CLI sessions must use a local:user:... namespace; anything else forces re-link.
		if nsID != "" && !strings.HasPrefix(nsID, "local:") {
			nsID = ""
		}
		ctx.NamespaceID = nsID
		// Org/project spine (additive; empty for legacy links).
		ctx.OrgID = strings.TrimSpace(link.OrgID)
		ctx.OrgSlug = strings.TrimSpace(link.OrgSlug)
		ctx.ProjectID = strings.TrimSpace(link.ProjectID)
		ctx.ProjectSlug = strings.TrimSpace(link.ProjectSlug)
	}
	return ctx, nil
}

func currentGitRemoteURL(dir string) (string, error) {
	root := dir
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	if !filepath.IsAbs(root) {
		if cwd, err := os.Getwd(); err == nil {
			root = filepath.Join(cwd, root)
		}
	}
	cmd := exec.Command("git", "-C", root, "config", "--get", "remote.origin.url")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("detect git remote.origin.url: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func parseGitHubRepoFullName(remoteURL string) string {
	trimmed := strings.TrimSpace(remoteURL)
	trimmed = strings.TrimSuffix(trimmed, ".git")
	trimmed = strings.TrimRight(trimmed, "/")
	switch {
	case strings.HasPrefix(trimmed, "git@github.com:"):
		return strings.TrimPrefix(trimmed, "git@github.com:")
	case strings.HasPrefix(trimmed, "ssh://git@github.com/"):
		return strings.TrimPrefix(trimmed, "ssh://git@github.com/")
	case strings.HasPrefix(trimmed, "https://github.com/"):
		return strings.TrimPrefix(trimmed, "https://github.com/")
	case strings.HasPrefix(trimmed, "http://github.com/"):
		return strings.TrimPrefix(trimmed, "http://github.com/")
	default:
		return ""
	}
}

// errRepoNotLinked is the fail-fast error for an unlinked repo on a
// --remote-state entry point (design §7 row 3). Since `orun auth login` now
// authenticates and auto-links in one step (UO1), it is the next command we
// point at; `orun cloud link` remains available to choose a specific org.
func errRepoNotLinked(backendURL string) error {
	cmd := "orun auth login"
	if strings.TrimSpace(backendURL) != "" {
		cmd = fmt.Sprintf("orun auth login --backend-url %s", backendURL)
	}
	return fmt.Errorf("this repo isn't connected to Orun Cloud yet; run `%s` to link it (or `orun cloud link --org <slug>` to pick the org)", cmd)
}

// errBackendUnreachable is the fail-fast error when the Orun Cloud backend
// cannot be reached (or is failing) at run start (design §7 row 4). It points at
// the --local escape hatch; there is never a silent fallback to local state.
func errBackendUnreachable(backendURL string, cause error) error {
	target := strings.TrimSpace(backendURL)
	if target == "" {
		target = "the configured backend"
	}
	return fmt.Errorf("Orun Cloud backend unreachable at %s: %w\n"+
		"hint: re-run with `--local` to use local filesystem state for this run, "+
		"or retry once connectivity returns (orun never silently falls back to local)", target, cause)
}

// isNoLoginErr reports whether an auth-resolution error means there is no usable
// local session (so the caller should surface the single "run `orun auth login`"
// message per design §7 row 2).
func isNoLoginErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, cliauth.ErrSessionRevoked) {
		return true
	}
	return strings.Contains(err.Error(), "no local Orun login")
}

func timeNowRFC3339() string {
	return nowFunc().Format("2006-01-02T15:04:05Z07:00")
}

var nowFunc = func() time.Time { return time.Now().UTC() }

// persistWorkspaceLink caches a platform WorkspaceLink (org/project IDs+slugs +
// the server's normalized remoteUrl) as a RepoLink in ~/.orun/config.yaml.
func persistWorkspaceLink(backendURL string, repo *repoContext, link *cliauth.WorkspaceLink) error {
	if repo == nil || link == nil {
		return nil
	}
	return cliauth.UpsertRepoLink(cliauth.RepoLink{
		BackendURL:   backendURL,
		GitRemote:    repo.GitRemote,
		RepoFullName: repo.RepoFullName,
		OrgID:        link.OrgID,
		OrgSlug:      link.OrgSlug,
		ProjectID:    link.ProjectID,
		ProjectSlug:  link.ProjectSlug,
		// Cache the SERVER's canonical normalized remote (lowercase
		// host/owner/repo, no scheme/.git); never the client's own form.
		RepoID:   strings.TrimSpace(link.RemoteURL),
		LinkedAt: timeNowRFC3339(),
	})
}

// persistLocalLink caches the OSS single-tenant _local/_local scope for the
// current repo without calling the platform link API (the OSS backend has no
// /cli/links route — design §1 "one contract, two servers").
func persistLocalLink(backendURL string, repo *repoContext) error {
	if repo == nil {
		return nil
	}
	return cliauth.UpsertRepoLink(cliauth.RepoLink{
		BackendURL:    backendURL,
		GitRemote:     repo.GitRemote,
		RepoFullName:  repo.RepoFullName,
		OrgID:         localScopeSegment,
		ProjectID:     localScopeSegment,
		NamespaceID:   "local:_local",
		NamespaceKind: "local",
		LinkedAt:      timeNowRFC3339(),
	})
}

// localScopeSegment is the OSS single-tenant org/project scope segment; it
// mirrors remotestate's defaultScopeSegment.
const localScopeSegment = "_local"

// isOSSBackend reports whether backendURL is the OSS `orun backend` server this
// machine provisioned via `orun backend init` (its bootstrap metadata is in
// ~/.orun/config.yaml and its URL matches). For that server, `cloud link`
// short-circuits to _local/_local instead of calling /cli/links, which it does
// not serve.
func isOSSBackend(backendURL string) bool {
	cfg, err := cliauth.LoadConfig()
	if err != nil || cfg == nil || cfg.BackendBootstrap == nil {
		return false
	}
	if strings.TrimSpace(cfg.BackendBootstrap.ManagedBy) != "orun-backend-init" {
		return false
	}
	return sameBackendURL(cfg.ResolvedBackendURL(), backendURL)
}

func sameBackendURL(a, b string) bool {
	norm := func(s string) string {
		return strings.ToLower(strings.TrimRight(strings.TrimSpace(s), "/"))
	}
	return norm(a) != "" && norm(a) == norm(b)
}

// translateLinkAPIError converts a platform link/resolve error envelope into an
// actionable message. Per resource-hiding, 404 (policy/membership denial) reads
// as unauthorized-or-not-found; 412 limit_reached is the entitlement message;
// 409 is already-linked; 422 is a bad remote. The platform requestId is
// preserved by the underlying *APIError when present.
func translateLinkAPIError(err error, remoteURL string) error {
	var apiErr *cliauth.APIError
	if !errors.As(err, &apiErr) {
		return fmt.Errorf("link %s: %w", remoteURL, err)
	}
	reqIDSuffix := ""
	if apiErr.RequestID != "" {
		reqIDSuffix = fmt.Sprintf(" [requestId: %s]", apiErr.RequestID)
	}
	switch apiErr.Status {
	case 404:
		return fmt.Errorf("not authorized to link %s, or the org/project does not exist; check your org membership or run `orun auth login` again%s", remoteURL, reqIDSuffix)
	case 412:
		if apiErr.DetailReason() == "limit_reached" {
			return fmt.Errorf("project limit reached for this org; upgrade the plan or pick an existing project, then retry `orun cloud link`%s", reqIDSuffix)
		}
		return fmt.Errorf("link precondition failed for %s: %s%s", remoteURL, apiErr.Message, reqIDSuffix)
	case 409:
		return fmt.Errorf("%s is already linked to an active org/project; run `orun cloud status` to inspect or `orun cloud unlink` first%s", remoteURL, reqIDSuffix)
	case 422:
		return fmt.Errorf("%q is not a recognized git remote; link requires a valid remote URL%s", remoteURL, reqIDSuffix)
	default:
		return fmt.Errorf("link %s: %w", remoteURL, err)
	}
}
