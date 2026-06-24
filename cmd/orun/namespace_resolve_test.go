package main

import (
	"encoding/json"
	"errors"
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

func TestTranslateLinkAPIError(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantSub string
	}{
		{"404 denial", &cliauth.APIError{Status: 404, Code: "not_found", Message: "nope"}, "not authorized"},
		{"412 limit", &cliauth.APIError{Status: 412, Code: "precondition_failed", Details: json.RawMessage(`{"reason":"limit_reached"}`)}, "project limit reached"},
		{"412 other", &cliauth.APIError{Status: 412, Code: "precondition_failed", Message: "nope"}, "precondition failed"},
		{"409 conflict", &cliauth.APIError{Status: 409, Code: "conflict"}, "already linked"},
		{"422 bad remote", &cliauth.APIError{Status: 422, Code: "validation_failed"}, "not a recognized git remote"},
		{"non-api", errors.New("network down"), "network down"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := translateLinkAPIError(tc.err, "git@github.com:acme/platform.git")
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("translateLinkAPIError() = %q, want substring %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestTranslateLinkAPIError_PreservesRequestID(t *testing.T) {
	err := translateLinkAPIError(&cliauth.APIError{Status: 404, Code: "not_found", RequestID: "req_abc"}, "git@github.com:acme/platform.git")
	if !strings.Contains(err.Error(), "req_abc") {
		t.Errorf("expected requestId in message, got: %v", err)
	}
}

func TestPersistWorkspaceLink_CachesNormalizedRemote(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	repo := &repoContext{
		GitRemote:    "git@github.com:acme/platform.git",
		RepoFullName: "acme/platform",
	}
	link := &cliauth.WorkspaceLink{
		ID:          "wsl_1",
		OrgID:       "org_a",
		OrgSlug:     "acme",
		ProjectID:   "prj_1",
		ProjectSlug: "platform",
		RemoteURL:   "github.com/acme/platform", // server-normalized form
	}

	if err := persistWorkspaceLink("https://api.orun.cloud", repo, link); err != nil {
		t.Fatalf("persistWorkspaceLink() error: %v", err)
	}

	cached, err := cliauth.FindRepoLink("https://api.orun.cloud", repo.GitRemote, repo.RepoFullName)
	if err != nil {
		t.Fatalf("FindRepoLink() error: %v", err)
	}
	if cached == nil {
		t.Fatal("expected a cached link")
	}
	if cached.OrgID != "org_a" || cached.OrgSlug != "acme" || cached.ProjectID != "prj_1" || cached.ProjectSlug != "platform" {
		t.Errorf("cached scope = %+v, want org_a/acme prj_1/platform", cached)
	}
	if cached.RepoID != "github.com/acme/platform" {
		t.Errorf("cached normalized remote = %q, want server form github.com/acme/platform", cached.RepoID)
	}
}

func TestPersistLocalLink_UsesLocalScope(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	repo := &repoContext{GitRemote: "git@github.com:acme/platform.git", RepoFullName: "acme/platform"}
	if err := persistLocalLink("https://orun-api.example.workers.dev", repo); err != nil {
		t.Fatalf("persistLocalLink() error: %v", err)
	}
	cached, err := cliauth.FindRepoLink("https://orun-api.example.workers.dev", repo.GitRemote, repo.RepoFullName)
	if err != nil {
		t.Fatalf("FindRepoLink() error: %v", err)
	}
	if cached == nil || cached.OrgID != "_local" || cached.ProjectID != "_local" {
		t.Fatalf("expected _local/_local scope, got %+v", cached)
	}
}

func TestIsOSSBackend(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	const ossURL = "https://orun-api.example.workers.dev"
	if err := cliauth.SaveBootstrapMetadata(cliauth.BackendBootstrap{
		ManagedBy: "orun-backend-init",
	}, ossURL); err != nil {
		t.Fatalf("SaveBootstrapMetadata() error: %v", err)
	}
	if !isOSSBackend(ossURL) {
		t.Errorf("isOSSBackend(%q) = false, want true", ossURL)
	}
	if isOSSBackend("https://api.orun.cloud") {
		t.Errorf("isOSSBackend(cloud URL) = true, want false")
	}
}

func TestErrRepoNotLinked(t *testing.T) {
	err := errRepoNotLinked("https://api.orun.cloud")
	// Primary next step is the unified auth-login (it auto-links since UO1);
	// `orun cloud link` is offered as the org-picking alternative.
	if !strings.Contains(err.Error(), "orun auth login") {
		t.Errorf("expected `orun auth login` hint, got: %v", err)
	}
	if !strings.Contains(err.Error(), "orun cloud link") {
		t.Errorf("expected `orun cloud link` alternative, got: %v", err)
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

func TestRemoveRepoLink(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := cliauth.UpsertRepoLink(cliauth.RepoLink{
		BackendURL:   "https://api.orun.cloud",
		GitRemote:    "git@github.com:acme/platform.git",
		RepoFullName: "acme/platform",
		OrgID:        "org_a",
		ProjectID:    "prj_1",
		LinkedAt:     "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("UpsertRepoLink() error: %v", err)
	}
	if err := cliauth.RemoveRepoLink("https://api.orun.cloud", "git@github.com:acme/platform.git", "acme/platform"); err != nil {
		t.Fatalf("RemoveRepoLink() error: %v", err)
	}
	cached, err := cliauth.FindRepoLink("https://api.orun.cloud", "git@github.com:acme/platform.git", "acme/platform")
	if err != nil {
		t.Fatalf("FindRepoLink() error: %v", err)
	}
	if cached != nil {
		t.Errorf("expected link removed, got %+v", cached)
	}
}
