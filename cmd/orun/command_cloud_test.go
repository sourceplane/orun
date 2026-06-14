package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/cliauth"
)

// linkData wraps v in the platform { data, meta } success envelope.
func linkData(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": v, "meta": map[string]interface{}{"requestId": "req_test"}})
}

func linkErr(w http.ResponseWriter, status int, code, message string, details map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	e := map[string]interface{}{"code": code, "message": message, "requestId": "req_xyz"}
	if details != nil {
		e["details"] = details
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": e})
}

// resetLinkFlags clears the package-level non-interactive flags between tests.
func resetLinkFlags(t *testing.T) {
	t.Helper()
	prevOrg, prevProj := cloudLinkOrg, cloudLinkProj
	cloudLinkOrg, cloudLinkProj = "", ""
	t.Cleanup(func() { cloudLinkOrg, cloudLinkProj = prevOrg, prevProj })
}

const remoteForTest = "git@github.com:acme/platform.git"

func TestResolveOrCreateLink_OneCandidateExistingLink(t *testing.T) {
	resetLinkFlags(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/cli/links/resolve" {
			linkData(w, 200, cliauth.ResolveLinksResponse{
				Links: []cliauth.WorkspaceLink{
					{ID: "wsl_1", OrgID: "org_a", OrgSlug: "acme", ProjectID: "prj_1", ProjectSlug: "platform", RemoteURL: "github.com/acme/platform"},
				},
			})
			return
		}
		t.Errorf("unexpected request to %s", r.URL.Path)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := cliauth.NewBackendClient(srv.URL, "test")
	link, err := resolveOrCreateLink(context.Background(), client, "tok", remoteForTest)
	if err != nil {
		t.Fatalf("resolveOrCreateLink() error: %v", err)
	}
	if link.OrgSlug != "acme" || link.ProjectSlug != "platform" || link.RemoteURL != "github.com/acme/platform" {
		t.Errorf("link = %+v, want acme/platform with normalized remote", link)
	}
}

func TestResolveOrCreateLink_ZeroCandidatesNonInteractiveOrgFlag(t *testing.T) {
	resetLinkFlags(t)
	cloudLinkOrg = "acme"
	cloudLinkProj = "platform"

	var created bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/cli/links/resolve":
			// Candidate carries the org ID for the slug so create targets org_a.
			linkData(w, 200, cliauth.ResolveLinksResponse{
				Candidates: []cliauth.WorkspaceLink{{OrgID: "org_a", OrgSlug: "acme"}},
			})
		case r.URL.Path == "/v1/organizations/org_a/cli/links" && r.Method == http.MethodPost:
			created = true
			var body struct {
				RemoteURL   string `json:"remoteUrl"`
				ProjectSlug string `json:"projectSlug"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.RemoteURL != remoteForTest {
				t.Errorf("create remoteUrl = %q, want raw remote", body.RemoteURL)
			}
			if body.ProjectSlug != "platform" {
				t.Errorf("create projectSlug = %q, want platform", body.ProjectSlug)
			}
			linkData(w, 201, map[string]interface{}{
				"link": cliauth.WorkspaceLink{OrgID: "org_a", OrgSlug: "acme", ProjectID: "prj_new", ProjectSlug: "platform", RemoteURL: "github.com/acme/platform"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cliauth.NewBackendClient(srv.URL, "test")
	link, err := resolveOrCreateLink(context.Background(), client, "tok", remoteForTest)
	if err != nil {
		t.Fatalf("resolveOrCreateLink() error: %v", err)
	}
	if !created {
		t.Fatal("expected a create call")
	}
	if link.ProjectID != "prj_new" {
		t.Errorf("link.ProjectID = %q, want prj_new", link.ProjectID)
	}
}

func TestResolveOrCreateLink_MultipleLinksNonInteractiveSelectsByFlag(t *testing.T) {
	resetLinkFlags(t)
	cloudLinkOrg = "globex"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/cli/links/resolve" {
			linkData(w, 200, cliauth.ResolveLinksResponse{
				Links: []cliauth.WorkspaceLink{
					{OrgID: "org_a", OrgSlug: "acme", ProjectID: "p1", ProjectSlug: "platform", RemoteURL: "github.com/acme/platform"},
					{OrgID: "org_b", OrgSlug: "globex", ProjectID: "p2", ProjectSlug: "platform", RemoteURL: "github.com/acme/platform"},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := cliauth.NewBackendClient(srv.URL, "test")
	link, err := resolveOrCreateLink(context.Background(), client, "tok", remoteForTest)
	if err != nil {
		t.Fatalf("resolveOrCreateLink() error: %v", err)
	}
	if link.OrgSlug != "globex" {
		t.Errorf("link.OrgSlug = %q, want globex (selected by --org)", link.OrgSlug)
	}
}

func TestResolveOrCreateLink_CreateError412Entitlement(t *testing.T) {
	resetLinkFlags(t)
	cloudLinkOrg = "acme"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/cli/links/resolve":
			linkData(w, 200, cliauth.ResolveLinksResponse{
				Candidates: []cliauth.WorkspaceLink{{OrgID: "org_a", OrgSlug: "acme"}},
			})
		case strings.HasSuffix(r.URL.Path, "/cli/links") && r.Method == http.MethodPost:
			linkErr(w, 412, "precondition_failed", "limit", map[string]interface{}{"reason": "limit_reached"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cliauth.NewBackendClient(srv.URL, "test")
	_, err := resolveOrCreateLink(context.Background(), client, "tok", remoteForTest)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "project limit reached") {
		t.Errorf("error = %q, want entitlement message", err.Error())
	}
}

func TestResolveOrCreateLink_ResolveError404Denied(t *testing.T) {
	resetLinkFlags(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		linkErr(w, 404, "not_found", "denied", nil)
	}))
	defer srv.Close()

	client := cliauth.NewBackendClient(srv.URL, "test")
	_, err := resolveOrCreateLink(context.Background(), client, "tok", remoteForTest)
	if err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("error = %v, want not-authorized message", err)
	}
}

func TestResolveOrCreateLink_ResolveError422BadRemote(t *testing.T) {
	resetLinkFlags(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		linkErr(w, 422, "validation_failed", "bad remote", nil)
	}))
	defer srv.Close()

	client := cliauth.NewBackendClient(srv.URL, "test")
	_, err := resolveOrCreateLink(context.Background(), client, "tok", "not-a-remote")
	if err == nil || !strings.Contains(err.Error(), "not a recognized git remote") {
		t.Fatalf("error = %v, want bad-remote message", err)
	}
}

func TestConsoleProjectURL(t *testing.T) {
	cases := []struct {
		backend string
		want    string
	}{
		{"https://api.orun.cloud", "https://app.orun.cloud/acme/platform"},
		{"https://orun-api.example.workers.dev", "https://orun-api.example.workers.dev/acme/platform"},
	}
	for _, tc := range cases {
		got, err := consoleProjectURL(tc.backend, "acme", "platform")
		if err != nil {
			t.Fatalf("consoleProjectURL(%q) error: %v", tc.backend, err)
		}
		if got != tc.want {
			t.Errorf("consoleProjectURL(%q) = %q, want %q", tc.backend, got, tc.want)
		}
	}
}

func TestCloudSessionToken_NotLoggedIn(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("ORUN_TOKEN", "")

	_, err := cloudSessionToken(context.Background(), "https://api.orun.cloud")
	if err == nil || !strings.Contains(err.Error(), "orun auth login") {
		t.Fatalf("error = %v, want `orun auth login` hint", err)
	}
}
