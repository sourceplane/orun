package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/cliauth"
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
// design §8 precedence: --org/--project flags > ORUN_ORG/ORUN_PROJECT env >
// the cached RepoLink's org/project. Empty fields are filled from the next
// source; the remotestate client defaults any still-empty field to the OSS
// _local scope.
func resolveScope(flagOrg, flagProject, linkOrg, linkProject string) remotestate.Scope {
	scope := remotestate.Scope{
		OrgID:     strings.TrimSpace(flagOrg),
		ProjectID: strings.TrimSpace(flagProject),
	}
	if scope.OrgID == "" {
		scope.OrgID = strings.TrimSpace(os.Getenv(orgEnvVar))
	}
	if scope.ProjectID == "" {
		scope.ProjectID = strings.TrimSpace(os.Getenv(projectEnvVar))
	}
	if scope.OrgID == "" {
		scope.OrgID = strings.TrimSpace(linkOrg)
	}
	if scope.ProjectID == "" {
		scope.ProjectID = strings.TrimSpace(linkProject)
	}
	return scope
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

func persistRepoLink(backendURL string, repo *repoContext, resp *cliauth.LinkRepoFromSessionResponse) error {
	if repo == nil || resp == nil || strings.TrimSpace(resp.NamespaceID) == "" {
		return nil
	}
	repoFullName := resp.RepoFullName
	if repoFullName == "" && repo != nil {
		repoFullName = repo.RepoFullName
	}
	return cliauth.UpsertRepoLink(cliauth.RepoLink{
		BackendURL:    backendURL,
		GitRemote:     repo.GitRemote,
		RepoFullName:  repoFullName,
		NamespaceID:   resp.NamespaceID,
		NamespaceKind: resp.NamespaceKind,
		RepoID:        resp.RepoID,
		LinkedAt:      timeNowRFC3339(),
	})
}

func timeNowRFC3339() string {
	return nowFunc().Format("2006-01-02T15:04:05Z07:00")
}

var nowFunc = func() time.Time { return time.Now().UTC() }

// autoResolveNamespace calls the backend session repo link endpoint to resolve
// repoFullName to a local namespace. Returns the full link response so the caller
// can persist namespace kind, repo ID, and other metadata.
//
// Must only be called outside GitHub Actions and when ORUN_TOKEN is not set.
func autoResolveNamespace(ctx context.Context, backendURL, repoFullName string) (*cliauth.LinkRepoFromSessionResponse, error) {
	tokenSrc := &remotestate.SessionTokenSource{
		BackendURL: backendURL,
		Version:    version,
	}
	accessToken, err := tokenSrc.Token(ctx)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if termIsInteractive() {
				return nil, fmt.Errorf("no local Orun login; run `orun auth login` to authenticate and auto-resolve the repo namespace")
			}
			return nil, fmt.Errorf("no local Orun login; run `orun auth login --device` or pre-link the namespace with `orun cloud link`")
		}
		return nil, fmt.Errorf("auth for namespace resolution: %w", err)
	}
	client := cliauth.NewBackendClient(backendURL, version)
	linked, err := client.LinkRepoFromSession(ctx, accessToken, repoFullName)
	if err != nil {
		return nil, translateLinkError(err, repoFullName)
	}
	return linked, nil
}

// translateLinkError converts backend API errors from the repo link endpoint
// into user-friendly messages.
func translateLinkError(err error, repoFullName string) error {
	var apiErr *cliauth.APIError
	if !errors.As(err, &apiErr) {
		return fmt.Errorf("resolve namespace for %s: %w", repoFullName, err)
	}
	switch apiErr.Code {
	case "NOT_FOUND":
		return fmt.Errorf("repo %s is not known to your Orun session; run `orun auth login` again to refresh namespace access", repoFullName)
	case "FORBIDDEN":
		return fmt.Errorf("repo %s is not authorized in your Orun session; re-authenticate with `orun auth login` or verify GitHub admin access", repoFullName)
	case "UNAUTHORIZED":
		return fmt.Errorf("Orun session token invalid or expired; run `orun auth login`")
	case "HTTP_404":
		return fmt.Errorf("backend does not support session repo linking; ensure the backend is updated to Task 0012.2")
	default:
		return fmt.Errorf("resolve namespace for %s: %w", repoFullName, err)
	}
}
