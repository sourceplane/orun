package cliauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestListOrgLinks_Paginates(t *testing.T) {
	type cursor struct {
		CreatedAt string `json:"createdAt"`
		ID        string `json:"id"`
	}
	type page struct {
		Links      []WorkspaceLink `json:"links"`
		NextCursor *cursor         `json:"nextCursor"`
	}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/organizations/org_a/cli/links" {
			http.Error(w, "unexpected request", 400)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "missing auth", 401)
			return
		}
		calls++
		if c := r.URL.Query().Get("cursor"); c == "" {
			writeData(w, 200, page{
				Links:      []WorkspaceLink{{OrgSlug: "acme", ProjectSlug: "platform", RemoteURL: "github.com/acme/platform"}},
				NextCursor: &cursor{CreatedAt: "2024-01-01T00:00:00Z", ID: "lnk_1"},
			})
		} else {
			writeData(w, 200, page{
				Links:      []WorkspaceLink{{OrgSlug: "acme", ProjectSlug: "infra", RemoteURL: "github.com/acme/infra"}},
				NextCursor: nil,
			})
		}
	}))
	defer srv.Close()

	c := NewBackendClient(srv.URL, "test")
	links, err := c.ListOrgLinks(context.Background(), "tok", "org_a")
	if err != nil {
		t.Fatalf("ListOrgLinks() error: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 paged calls, got %d", calls)
	}
	if len(links) != 2 || links[0].ProjectSlug != "platform" || links[1].ProjectSlug != "infra" {
		t.Fatalf("links = %+v, want platform+infra across pages", links)
	}
}

func TestListOrgLinks_Denied404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErrDetails(w, 404, "not_found", "Not found", nil)
	}))
	defer srv.Close()
	c := NewBackendClient(srv.URL, "test")
	_, err := c.ListOrgLinks(context.Background(), "tok", "org_a")
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Status != 404 {
		t.Errorf("Status = %d, want 404", apiErr.Status)
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

// ── accounts ─────────────────────────────────────────────────────────────────

// decodeBody reads the JSON request body into m (nil on failure).
func decodeBody(r *http.Request) map[string]interface{} {
	var m map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&m)
	return m
}

func TestCreateOrgInAccount_SendsAccountIDOnlyWhenNamed(t *testing.T) {
	var bodies []map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/organizations" {
			http.Error(w, "unexpected request", 400)
			return
		}
		bodies = append(bodies, decodeBody(r))
		writeData(w, 201, map[string]interface{}{"organization": map[string]interface{}{
			"id": "org_1", "workspaceRef": "ws_NEW", "slug": "acme", "name": "Acme", "accountId": "ws_ACME",
		}})
	}))
	defer srv.Close()

	c := NewBackendClient(srv.URL, "test")
	org, err := c.CreateOrgInAccount(context.Background(), "tok", "Acme", "acme", "ws_ACME")
	if err != nil {
		t.Fatalf("CreateOrgInAccount() error: %v", err)
	}
	if org.AccountID != "ws_ACME" || org.WorkspaceRef != "ws_NEW" || org.Role != "owner" {
		t.Errorf("org = %+v, want accountId/workspaceRef/owner role from the response", org)
	}
	if _, err := c.CreateOrg(context.Background(), "tok", "Acme", ""); err != nil {
		t.Fatalf("CreateOrg() error: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(bodies))
	}
	if got := bodies[0]["accountId"]; got != "ws_ACME" {
		t.Errorf("accountId = %v, want ws_ACME in the create body", got)
	}
	if got := bodies[0]["slug"]; got != "acme" {
		t.Errorf("slug = %v, want acme", got)
	}
	if _, present := bodies[1]["accountId"]; present {
		t.Errorf("accountId must be omitted when no account is named: %v", bodies[1])
	}
	if _, present := bodies[1]["slug"]; present {
		t.Errorf("slug must be omitted when empty: %v", bodies[1])
	}
}

func TestOrgPayload_ReadsNestedAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeData(w, 200, map[string]interface{}{"organizations": []map[string]interface{}{
			{"id": "org_1", "workspaceRef": "ws_1", "slug": "one", "account": map[string]string{"id": "ws_ACME", "slug": "acme"}},
			{"id": "org_2", "workspaceRef": "ws_2", "slug": "two", "accountId": "ws_LAB"},
			{"id": "org_3", "workspaceRef": "ws_3", "slug": "three"},
		}})
	}))
	defer srv.Close()
	orgs, err := NewBackendClient(srv.URL, "test").ListMyOrgs(context.Background(), "tok")
	if err != nil {
		t.Fatalf("ListMyOrgs() error: %v", err)
	}
	if len(orgs) != 3 {
		t.Fatalf("orgs = %d, want 3", len(orgs))
	}
	if orgs[0].AccountID != "ws_ACME" || orgs[0].AccountSlug != "acme" {
		t.Errorf("nested account not read: %+v", orgs[0])
	}
	if orgs[1].AccountID != "ws_LAB" || orgs[1].AccountSlug != "" {
		t.Errorf("flat accountId not read: %+v", orgs[1])
	}
	if orgs[2].AccountID != "" {
		t.Errorf("no account must stay empty: %+v", orgs[2])
	}
}

func TestListMyAccounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/accounts" {
			http.Error(w, "unexpected request", 400)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "missing auth", 401)
			return
		}
		writeData(w, 200, map[string]interface{}{"accounts": []map[string]interface{}{
			{"id": "ws_ACME", "slug": "acme", "name": "Acme Inc", "role": "owner"},
			{"id": "ws_LAB", "slug": "lab", "isOwner": true},
			{"id": "ws_GUEST", "slug": "guest", "role": "member"},
			{"slug": "no-id"},
		}})
	}))
	defer srv.Close()
	accounts, err := NewBackendClient(srv.URL, "test").ListMyAccounts(context.Background(), "tok")
	if err != nil {
		t.Fatalf("ListMyAccounts() error: %v", err)
	}
	if len(accounts) != 3 {
		t.Fatalf("accounts = %+v, want 3 (the id-less row dropped)", accounts)
	}
	if accounts[0].Role != "owner" || accounts[0].Label() != "acme" {
		t.Errorf("accounts[0] = %+v", accounts[0])
	}
	if accounts[1].Role != "owner" {
		t.Errorf("isOwner must read as the owner role: %+v", accounts[1])
	}
	if !accounts[0].Matches("ACME") || !accounts[0].Matches("ws_acme") || accounts[0].Matches("lab") {
		t.Errorf("Matches should accept slug or id case-insensitively")
	}
}

func TestNeedsAccountChoice(t *testing.T) {
	msg := "You own more than one account. Name the account this workspace belongs to (accountId)."
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"platform refusal", &APIError{Status: 412, Code: "precondition_failed", Message: msg}, true},
		{"wrapped", fmt.Errorf("create workspace: %w", &APIError{Status: 412, Code: "precondition_failed", Message: msg}), true},
		{"412 for another reason", &APIError{Status: 412, Code: "precondition_failed", Message: "limit"}, false},
		{"409 slug taken", &APIError{Status: 409, Code: "conflict", Message: msg}, false},
		{"plain error", errors.New(msg), false},
	}
	for _, tc := range cases {
		if got := NeedsAccountChoice(tc.err); got != tc.want {
			t.Errorf("%s: NeedsAccountChoice = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAccountsFromErrorDetails(t *testing.T) {
	err := &APIError{Status: 412, Details: json.RawMessage(`{"accounts":[{"id":"ws_A","slug":"a"}],"accountIds":["ws_B",""]}`)}
	got := AccountsFromErrorDetails(err)
	if len(got) != 2 || got[0].ID != "ws_A" || got[0].Slug != "a" || got[1].ID != "ws_B" {
		t.Errorf("AccountsFromErrorDetails = %+v", got)
	}
	if got := AccountsFromErrorDetails(&APIError{Status: 412}); len(got) != 0 {
		t.Errorf("no details must yield nothing: %+v", got)
	}
	if got := AccountsFromErrorDetails(errors.New("x")); len(got) != 0 {
		t.Errorf("non-API error must yield nothing: %+v", got)
	}
}
