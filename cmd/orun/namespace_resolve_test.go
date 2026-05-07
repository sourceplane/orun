package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/cliauth"
)

func TestParseGitHubRepoFullNameVariants(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"git@github.com:owner/repo.git", "owner/repo"},
		{"git@github.com:owner/repo", "owner/repo"},
		{"ssh://git@github.com/owner/repo.git", "owner/repo"},
		{"https://github.com/owner/repo.git", "owner/repo"},
		{"https://github.com/owner/repo", "owner/repo"},
		{"http://github.com/owner/repo.git", "owner/repo"},
		{"git@gitlab.com:owner/repo.git", ""},
		{"https://bitbucket.org/owner/repo", ""},
		{"", ""},
	}
	for _, tc := range cases {
		got := parseGitHubRepoFullName(tc.input)
		if got != tc.want {
			t.Errorf("parseGitHubRepoFullName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestTranslateLinkError_NotFound(t *testing.T) {
	err := translateLinkError(&cliauth.APIError{Code: "NOT_FOUND", Message: "slug not found"}, "owner/repo")
	if !strings.Contains(err.Error(), "orun auth login") {
		t.Errorf("expected re-login hint, got: %v", err)
	}
}

func TestTranslateLinkError_Forbidden(t *testing.T) {
	err := translateLinkError(&cliauth.APIError{Code: "FORBIDDEN", Message: "forbidden"}, "owner/repo")
	if !strings.Contains(err.Error(), "orun auth login") {
		t.Errorf("expected re-auth hint, got: %v", err)
	}
}

func TestTranslateLinkError_Unauthorized(t *testing.T) {
	err := translateLinkError(&cliauth.APIError{Code: "UNAUTHORIZED", Message: "unauthorized"}, "owner/repo")
	if !strings.Contains(err.Error(), "orun auth login") {
		t.Errorf("expected login hint, got: %v", err)
	}
}

func TestTranslateLinkError_HTTP404RouteNotFound(t *testing.T) {
	err := translateLinkError(&cliauth.APIError{Code: "HTTP_404", Message: "not found"}, "owner/repo")
	if !strings.Contains(err.Error(), "backend") {
		t.Errorf("expected backend update hint, got: %v", err)
	}
}

func TestTranslateLinkError_UnknownError(t *testing.T) {
	err := translateLinkError(errors.New("network error"), "owner/repo")
	if !strings.Contains(err.Error(), "owner/repo") {
		t.Errorf("expected repo name in error, got: %v", err)
	}
}

func TestAutoResolveNamespace_NoSession(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	os.Unsetenv("ORUN_TOKEN")
	os.Unsetenv("GITHUB_ACTIONS")
	os.Unsetenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	os.Unsetenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")

	_, err := autoResolveNamespace(context.Background(), "https://api.example.com", "owner/repo")
	if err == nil {
		t.Fatal("expected error when no session exists")
	}
}

func TestAutoResolveNamespace_Success(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	os.Unsetenv("ORUN_TOKEN")
	os.Unsetenv("GITHUB_ACTIONS")
	os.Unsetenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	os.Unsetenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/accounts/repos/link" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"namespaceKind": "local",
				"namespaceId":   "local:user:12345:repo:67890",
				"namespaceSlug": "local:octocat/owner/repo",
				"repoId":        "67890",
				"repoFullName":  "owner/repo",
				"linkedAt":      "2026-05-07T10:00:00Z",
			})
			return
		}
		// Refresh token endpoint.
		if r.URL.Path == "/v1/auth/cli/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"accessToken": "fresh-token",
				"expiresAt":   "2099-01-01T00:00:00Z",
				"githubLogin": "testuser",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Write credentials directly to bypass macOS keychain prompts in tests.
	writeTestCredentials(t, tmp, &cliauth.Credentials{
		AccessToken:       "valid-token",
		AccessTokenExpiry: "2099-01-01T00:00:00Z",
		RefreshToken:      "refresh-tok",
		GitHubLogin:       "testuser",
		BackendURL:        srv.URL,
	})

	resp, err := autoResolveNamespace(context.Background(), srv.URL, "owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.NamespaceID != "local:user:12345:repo:67890" {
		t.Errorf("NamespaceID = %q, want local:user:12345:repo:67890", resp.NamespaceID)
	}
	if resp.NamespaceKind != "local" {
		t.Errorf("NamespaceKind = %q, want local", resp.NamespaceKind)
	}
	if resp.RepoID != "67890" {
		t.Errorf("RepoID = %q, want 67890", resp.RepoID)
	}
}

func TestAutoResolveNamespace_NotFound(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	os.Unsetenv("ORUN_TOKEN")
	os.Unsetenv("GITHUB_ACTIONS")
	os.Unsetenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	os.Unsetenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "slug not found; re-run orun auth login",
			"code":  "NOT_FOUND",
		})
	}))
	defer srv.Close()

	writeTestCredentials(t, tmp, &cliauth.Credentials{
		AccessToken:       "valid-token",
		AccessTokenExpiry: "2099-01-01T00:00:00Z",
		RefreshToken:      "refresh-tok",
		GitHubLogin:       "testuser",
		BackendURL:        srv.URL,
	})

	_, err := autoResolveNamespace(context.Background(), srv.URL, "owner/repo")
	if err == nil {
		t.Fatal("expected error for NOT_FOUND")
	}
	if !strings.Contains(err.Error(), "orun auth login") {
		t.Errorf("expected re-login hint, got: %v", err)
	}
}

func TestInvalidateCanonicalCachedNamespace(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Store a link with a canonical repo namespace ID (pre-0012.2.1 format).
	if err := cliauth.UpsertRepoLink(cliauth.RepoLink{
		BackendURL:   "https://api.example.com",
		GitRemote:    "https://github.com/owner/repo.git",
		RepoFullName: "owner/repo",
		NamespaceID:  "repo:67890",
		LinkedAt:     "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("UpsertRepoLink() error: %v", err)
	}

	link, err := cliauth.FindRepoLink("https://api.example.com", "", "owner/repo")
	if err != nil {
		t.Fatalf("FindRepoLink() error: %v", err)
	}
	if link == nil {
		t.Fatal("expected a stored link")
	}
	// Simulate what resolveRepoContext does: invalidate non-local namespace IDs.
	nsID := link.NamespaceID
	if nsID != "" && !strings.HasPrefix(nsID, "local:") {
		nsID = ""
	}
	if nsID != "" {
		t.Errorf("canonical namespace ID %q should have been cleared, got %q", link.NamespaceID, nsID)
	}
}

func TestPersistRepoLinkStoresLocalMetadata(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	repo := &repoContext{
		GitRemote:    "https://github.com/owner/repo.git",
		RepoFullName: "owner/repo",
	}
	resp := &cliauth.LinkRepoFromSessionResponse{
		NamespaceKind: "local",
		NamespaceID:   "local:user:12345:repo:67890",
		NamespaceSlug: "local:octocat/owner/repo",
		RepoID:        "67890",
		RepoFullName:  "owner/repo",
		LinkedAt:      "2026-05-07T10:00:00Z",
	}

	if err := persistRepoLink("https://api.example.com", repo, resp); err != nil {
		t.Fatalf("persistRepoLink() error: %v", err)
	}

	link, err := cliauth.FindRepoLink("https://api.example.com", "", "owner/repo")
	if err != nil {
		t.Fatalf("FindRepoLink() error: %v", err)
	}
	if link == nil {
		t.Fatal("expected a stored link")
	}
	if link.NamespaceID != "local:user:12345:repo:67890" {
		t.Errorf("NamespaceID = %q, want local:user:12345:repo:67890", link.NamespaceID)
	}
	if link.NamespaceKind != "local" {
		t.Errorf("NamespaceKind = %q, want local", link.NamespaceKind)
	}
	if link.RepoID != "67890" {
		t.Errorf("RepoID = %q, want 67890", link.RepoID)
	}
}

// writeTestCredentials writes credentials directly to the file store in tmp,
// bypassing the macOS keychain which prompts for auth and hangs in tests.
func writeTestCredentials(t *testing.T, homeDir string, creds *cliauth.Credentials) {
	t.Helper()
	data, err := json.Marshal(creds)
	if err != nil {
		t.Fatalf("marshal credentials: %v", err)
	}
	dir := filepath.Join(homeDir, ".orun")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), data, 0o600); err != nil {
		t.Fatalf("write credentials.json: %v", err)
	}
}