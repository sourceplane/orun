package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/model"
)

type repoContext struct {
	GitRemote    string
	RepoFullName string
	NamespaceID  string
}

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
		return strings.TrimSpace(cfg.Backend.URL)
	}
	return ""
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
		ctx.NamespaceID = strings.TrimSpace(link.NamespaceID)
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

func persistRepoLink(backendURL string, repo *repoContext, namespaceID string) error {
	if repo == nil || strings.TrimSpace(namespaceID) == "" {
		return nil
	}
	return cliauth.UpsertRepoLink(cliauth.RepoLink{
		BackendURL:   backendURL,
		GitRemote:    repo.GitRemote,
		RepoFullName: repo.RepoFullName,
		NamespaceID:  namespaceID,
		LinkedAt:     timeNowRFC3339(),
	})
}

func timeNowRFC3339() string {
	return nowFunc().Format("2006-01-02T15:04:05Z07:00")
}

var nowFunc = func() time.Time { return time.Now().UTC() }
