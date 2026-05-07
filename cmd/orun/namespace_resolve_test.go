package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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
				"namespaceId":   "ns-resolved",
				"namespaceSlug": "owner/repo",
				"linkedAt":      "2026-05-07T10:00:00Z",
			})
			return
		}
		// Refresh token endpoint.
		if r.URL.Path == "/v1/auth/cli/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"accessToken":  "fresh-token",
				"expiresAt":    "2099-01-01T00:00:00Z",
				"githubLogin":  "testuser",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Store a session with a non-expired token.
	if err := cliauth.SaveSession(&cliauth.Credentials{
		AccessToken:       "valid-token",
		AccessTokenExpiry: "2099-01-01T00:00:00Z",
		RefreshToken:      "refresh-tok",
		GitHubLogin:       "testuser",
		BackendURL:        srv.URL,
	}); err != nil {
		t.Fatalf("SaveSession() error: %v", err)
	}

	nsID, err := autoResolveNamespace(context.Background(), srv.URL, "owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nsID != "ns-resolved" {
		t.Errorf("namespaceID = %q, want ns-resolved", nsID)
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

	if err := cliauth.SaveSession(&cliauth.Credentials{
		AccessToken:       "valid-token",
		AccessTokenExpiry: "2099-01-01T00:00:00Z",
		RefreshToken:      "refresh-tok",
		GitHubLogin:       "testuser",
		BackendURL:        srv.URL,
	}); err != nil {
		t.Fatalf("SaveSession() error: %v", err)
	}

	_, err := autoResolveNamespace(context.Background(), srv.URL, "owner/repo")
	if err == nil {
		t.Fatal("expected error for NOT_FOUND")
	}
	if !strings.Contains(err.Error(), "orun auth login") {
		t.Errorf("expected re-login hint, got: %v", err)
	}
}
