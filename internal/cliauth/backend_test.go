package cliauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// writeErrDetails emits the platform's { error: { code, message, details,
// requestId } } envelope with an optional structured details object. (The plain
// writeData/writeError helpers live in login_test.go.)
func writeErrDetails(w http.ResponseWriter, status int, code, message string, details map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	errObj := map[string]interface{}{"code": code, "message": message, "requestId": "req_test"}
	if details != nil {
		errObj["details"] = details
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": errObj})
}

func TestResolveLinks_Candidates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/cli/links/resolve" {
			http.Error(w, "unexpected request", 400)
			return
		}
		if got := r.URL.Query().Get("remoteUrl"); got != "git@github.com:acme/platform.git" {
			t.Errorf("remoteUrl = %q, want raw remote", got)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "missing auth", 401)
			return
		}
		writeData(w, 200, ResolveLinksResponse{
			Candidates: []WorkspaceLink{
				{OrgID: "org_a", OrgSlug: "acme"},
				{OrgID: "org_b", OrgSlug: "globex"},
			},
			Links: []WorkspaceLink{},
		})
	}))
	defer srv.Close()

	c := NewBackendClient(srv.URL, "test")
	resp, err := c.ResolveLinks(context.Background(), "tok", "git@github.com:acme/platform.git")
	if err != nil {
		t.Fatalf("ResolveLinks() error: %v", err)
	}
	if len(resp.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(resp.Candidates))
	}
	if resp.Candidates[0].OrgSlug != "acme" {
		t.Errorf("candidate[0].OrgSlug = %q, want acme", resp.Candidates[0].OrgSlug)
	}
}

func TestResolveLinks_ExistingLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeData(w, 200, ResolveLinksResponse{
			Candidates: []WorkspaceLink{},
			Links: []WorkspaceLink{
				{ID: "wsl_1", OrgID: "org_a", OrgSlug: "acme", ProjectID: "prj_1", ProjectSlug: "platform", RemoteURL: "github.com/acme/platform"},
			},
		})
	}))
	defer srv.Close()

	c := NewBackendClient(srv.URL, "test")
	resp, err := c.ResolveLinks(context.Background(), "tok", "git@github.com:acme/platform.git")
	if err != nil {
		t.Fatalf("ResolveLinks() error: %v", err)
	}
	if len(resp.Links) != 1 || resp.Links[0].RemoteURL != "github.com/acme/platform" {
		t.Fatalf("links = %+v, want one normalized link", resp.Links)
	}
}

func TestResolveLinks_BadRemote422(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErrDetails(w, 422, "validation_failed", "remote not recognized", nil)
	}))
	defer srv.Close()

	c := NewBackendClient(srv.URL, "test")
	_, err := c.ResolveLinks(context.Background(), "tok", "not-a-remote")
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Status != 422 {
		t.Errorf("Status = %d, want 422", apiErr.Status)
	}
}

func TestCreateLink_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/organizations/org_a/cli/links" {
			http.Error(w, "unexpected request: "+r.URL.Path, 400)
			return
		}
		var body struct {
			RemoteURL   string `json:"remoteUrl"`
			ProjectSlug string `json:"projectSlug"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.RemoteURL != "git@github.com:acme/platform.git" {
			t.Errorf("remoteUrl = %q, want raw remote", body.RemoteURL)
		}
		if body.ProjectSlug != "platform" {
			t.Errorf("projectSlug = %q, want platform", body.ProjectSlug)
		}
		writeData(w, 201, map[string]interface{}{
			"link": WorkspaceLink{
				ID:          "wsl_1",
				OrgID:       "org_a",
				OrgSlug:     "acme",
				ProjectID:   "prj_1",
				ProjectSlug: "platform",
				RemoteURL:   "github.com/acme/platform",
				CreatedBy:   &LinkActor{ID: "usr_1", Kind: "user"},
				CreatedAt:   "2026-06-14T10:00:00Z",
			},
		})
	}))
	defer srv.Close()

	c := NewBackendClient(srv.URL, "test")
	link, err := c.CreateLink(context.Background(), "tok", "org_a", "git@github.com:acme/platform.git", "platform")
	if err != nil {
		t.Fatalf("CreateLink() error: %v", err)
	}
	if link.RemoteURL != "github.com/acme/platform" {
		t.Errorf("RemoteURL = %q, want server normalized form", link.RemoteURL)
	}
	if link.ProjectSlug != "platform" || link.OrgSlug != "acme" {
		t.Errorf("link slugs = %q/%q, want acme/platform", link.OrgSlug, link.ProjectSlug)
	}
}

func TestCreateLink_Errors(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		code       string
		details    map[string]interface{}
		wantReason string
	}{
		{"bad remote", 422, "validation_failed", nil, ""},
		{"denied", 404, "not_found", nil, ""},
		{"already linked", 409, "conflict", nil, ""},
		{"entitlement", 412, "precondition_failed", map[string]interface{}{"reason": "limit_reached"}, "limit_reached"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeErrDetails(w, tc.status, tc.code, tc.name, tc.details)
			}))
			defer srv.Close()
			c := NewBackendClient(srv.URL, "test")
			_, err := c.CreateLink(context.Background(), "tok", "org_a", "git@github.com:acme/platform.git", "")
			apiErr, ok := err.(*APIError)
			if !ok {
				t.Fatalf("expected *APIError, got %T: %v", err, err)
			}
			if apiErr.Status != tc.status {
				t.Errorf("Status = %d, want %d", apiErr.Status, tc.status)
			}
			if apiErr.DetailReason() != tc.wantReason {
				t.Errorf("DetailReason() = %q, want %q", apiErr.DetailReason(), tc.wantReason)
			}
		})
	}
}
