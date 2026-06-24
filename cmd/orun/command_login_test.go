package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sourceplane/orun/internal/cliauth"
)

// TestResolveOrCreateLinkFor_AutoNamesProjectAfterRepo verifies the auto-link
// path used by `orun auth login`: with an explicit org but no project slug, the
// CreateLink call sends an empty projectSlug so the server names the project
// after the repo ("a project is a repo").
func TestResolveOrCreateLinkFor_AutoNamesProjectAfterRepo(t *testing.T) {
	var sentProjectSlug string
	var createCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/cli/links/resolve":
			// No existing links/candidates for this remote.
			linkData(w, 200, cliauth.ResolveLinksResponse{})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations/org_a/cli/links":
			createCalled = true
			var body struct {
				RemoteURL   string `json:"remoteUrl"`
				ProjectSlug string `json:"projectSlug"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			sentProjectSlug = body.ProjectSlug
			linkData(w, 201, map[string]interface{}{"link": cliauth.WorkspaceLink{
				ID: "wsl_9", OrgID: "org_a", OrgSlug: "acme",
				ProjectID: "prj_9", ProjectSlug: "platform",
				RemoteURL: "github.com/acme/platform",
			}})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cliauth.NewBackendClient(srv.URL, "test")
	// orgSlug resolves to org_a via the org id we feed; here we pass the org id
	// directly as the slug, which the helper forwards to the server unchanged.
	link, err := resolveOrCreateLinkFor(context.Background(), client, "tok", remoteForTest, "org_a", "")
	if err != nil {
		t.Fatalf("resolveOrCreateLinkFor() error: %v", err)
	}
	if !createCalled {
		t.Fatal("expected a CreateLink call")
	}
	if sentProjectSlug != "" {
		t.Errorf("projectSlug sent = %q, want empty (server derives repo name)", sentProjectSlug)
	}
	if link.OrgSlug != "acme" || link.ProjectSlug != "platform" {
		t.Errorf("link = %+v, want acme/platform", link)
	}
}

func TestRepoLinkLabel(t *testing.T) {
	cases := []struct {
		name string
		link *cliauth.RepoLink
		want string
	}{
		{"slugs", &cliauth.RepoLink{OrgSlug: "acme", ProjectSlug: "platform"}, "acme/platform"},
		{"ids fallback", &cliauth.RepoLink{OrgID: "org_a", ProjectID: "prj_1"}, "org_a/prj_1"},
		{"local scope", &cliauth.RepoLink{OrgID: "_local", ProjectID: "_local"}, "_local/_local"},
		{"repo full name", &cliauth.RepoLink{RepoFullName: "acme/platform"}, "acme/platform"},
		{"nil", nil, "(unknown)"},
	}
	for _, tc := range cases {
		if got := repoLinkLabel(tc.link); got != tc.want {
			t.Errorf("%s: repoLinkLabel() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestIsNoGitRemoteErr(t *testing.T) {
	if !isNoGitRemoteErr(errString("detect git remote.origin.url: exit status 128")) {
		t.Error("expected detect-git-remote error to be classified as no-remote")
	}
	if isNoGitRemoteErr(errString("some other failure")) {
		t.Error("unrelated error misclassified as no-remote")
	}
	if isNoGitRemoteErr(nil) {
		t.Error("nil error misclassified as no-remote")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
