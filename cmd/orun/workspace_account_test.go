package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const needsAccountMsg = "You own more than one account. Name the account this workspace belongs to (accountId)."

// workspaceCreateServer is a fake platform for `workspace create`: it records
// every POST /v1/organizations body, answers GET /v1/accounts with accounts
// (404 when nil), and refuses the create with the platform's "name the
// account" 412 unless the body names one.
type workspaceCreateServer struct {
	*httptest.Server
	accounts []map[string]interface{}
	bodies   []map[string]interface{}
}

func newWorkspaceCreateServer(t *testing.T, accounts []map[string]interface{}) *workspaceCreateServer {
	t.Helper()
	s := &workspaceCreateServer{accounts: accounts}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts":
			if s.accounts == nil {
				linkErr(w, 404, "not_found", "Route not found: /v1/accounts", nil)
				return
			}
			linkData(w, 200, map[string]interface{}{"accounts": s.accounts})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations":
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.bodies = append(s.bodies, body)
			accountID, _ := body["accountId"].(string)
			if accountID == "" {
				linkErr(w, 412, "precondition_failed", needsAccountMsg, nil)
				return
			}
			linkData(w, 201, map[string]interface{}{"organization": map[string]interface{}{
				"id": "org_new", "workspaceRef": "ws_NEW", "slug": body["slug"], "name": body["name"], "accountId": accountID,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	// Headless auth against the fake: ORUN_TOKEN is the bearer, the env var
	// the backend URL, and a scratch HOME keeps the real config out of it.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ORUN_CREDENTIAL_STORE", "file")
	t.Setenv("ORUN_TOKEN", "tok")
	t.Setenv("ORUN_TOKEN_FILE", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv(backendURLEnvVar, s.URL)
	return s
}

func twoAccounts() []map[string]interface{} {
	return []map[string]interface{}{
		{"id": "ws_ACME", "slug": "acme", "name": "Acme Inc", "role": "owner"},
		{"id": "ws_LAB", "slug": "lab", "name": "The Lab", "role": "owner"},
		{"id": "ws_GUEST", "slug": "guest", "name": "Someone else's", "role": "member"},
	}
}

func TestWorkspaceCreate_AccountSlugIsSentAsAccountID(t *testing.T) {
	srv := newWorkspaceCreateServer(t, twoAccounts())
	out, err := captureStdoutErr(t, func() error {
		return runCloudWorkspaceCreate(context.Background(), workspaceCreateOpts{
			Name: "Acme Platform", Slug: "acme-platform", Account: "ACME", CommandPath: "orun workspace create",
		})
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(srv.bodies) != 1 {
		t.Fatalf("create requests = %d, want 1", len(srv.bodies))
	}
	body := srv.bodies[0]
	if body["accountId"] != "ws_ACME" {
		t.Errorf("accountId = %v, want ws_ACME (the slug resolved against /v1/accounts)", body["accountId"])
	}
	if body["name"] != "Acme Platform" || body["slug"] != "acme-platform" {
		t.Errorf("body = %v, want name and slug", body)
	}
	for _, want := range []string{"workspace created", "id:   ws_NEW", "slug: acme-platform", "account: acme (ws_ACME)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestWorkspaceCreate_AccountIDIsSentAsTyped(t *testing.T) {
	srv := newWorkspaceCreateServer(t, twoAccounts())
	_, err := captureStdoutErr(t, func() error {
		return runCloudWorkspaceCreate(context.Background(), workspaceCreateOpts{Name: "Lab", Account: "ws_LAB"})
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(srv.bodies) != 1 || srv.bodies[0]["accountId"] != "ws_LAB" {
		t.Errorf("bodies = %v, want one create with accountId ws_LAB", srv.bodies)
	}
	if _, present := srv.bodies[0]["slug"]; present {
		t.Errorf("slug must be omitted when not given: %v", srv.bodies[0])
	}
}

func TestWorkspaceCreate_AccountPassesThroughWhenBackendCannotList(t *testing.T) {
	srv := newWorkspaceCreateServer(t, nil) // no /v1/accounts route
	_, err := captureStdoutErr(t, func() error {
		return runCloudWorkspaceCreate(context.Background(), workspaceCreateOpts{Name: "Lab", Account: "ws_LAB"})
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(srv.bodies) != 1 || srv.bodies[0]["accountId"] != "ws_LAB" {
		t.Errorf("bodies = %v, want the typed account sent through", srv.bodies)
	}
}

func TestWorkspaceCreate_UnknownAccountFailsBeforeTheRequest(t *testing.T) {
	srv := newWorkspaceCreateServer(t, twoAccounts())
	_, err := captureStdoutErr(t, func() error {
		return runCloudWorkspaceCreate(context.Background(), workspaceCreateOpts{Name: "X", Account: "acme-inc"})
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	for _, want := range []string{`no account "acme-inc" on this session`, "did you mean: --account acme", "accounts: acme, lab, guest"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q:\n%s", want, msg)
		}
	}
	if len(srv.bodies) != 0 {
		t.Errorf("no create request should be sent for an unknown account: %v", srv.bodies)
	}
}

func TestWorkspaceCreate_NeedsAccountHintListsOwnedAccountsAndRerun(t *testing.T) {
	srv := newWorkspaceCreateServer(t, twoAccounts())
	_, err := captureStdoutErr(t, func() error {
		return runCloudWorkspaceCreate(context.Background(), workspaceCreateOpts{
			Name: "Acme Platform", Slug: "acme-platform", CommandPath: "orun cloud workspace create",
		})
	})
	if err == nil {
		t.Fatal("expected the platform's refusal to surface")
	}
	msg := err.Error()
	if len(srv.bodies) != 1 {
		t.Fatalf("create requests = %d, want 1", len(srv.bodies))
	}
	if _, present := srv.bodies[0]["accountId"]; present {
		t.Errorf("no accountId should be sent without --account: %v", srv.bodies[0])
	}
	for _, want := range []string{
		needsAccountMsg,
		"(precondition_failed)",
		"--account",
		"ws_ACME", "Acme Inc",
		"ws_LAB", "The Lab",
		"orun cloud workspace create 'Acme Platform' --slug acme-platform --account acme",
		"orun cloud workspace create 'Acme Platform' --slug acme-platform --account lab",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("hint missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "ws_GUEST") || strings.Contains(msg, "--account guest") {
		t.Errorf("an account the login does not own must not be offered:\n%s", msg)
	}
}

func TestWorkspaceCreate_NeedsAccountHintWithoutAListing(t *testing.T) {
	newWorkspaceCreateServer(t, nil) // the platform refuses, and cannot list accounts
	_, err := captureStdoutErr(t, func() error {
		return runCloudWorkspaceCreate(context.Background(), workspaceCreateOpts{Name: "Lab", CommandPath: "orun workspace create"})
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	for _, want := range []string{needsAccountMsg, "orun workspace create Lab --account <ws_…|slug>", "console's account settings"} {
		if !strings.Contains(msg, want) {
			t.Errorf("hint missing %q:\n%s", want, msg)
		}
	}
}

func TestWorkspaceCreate_NeedsAccountHintFromErrorDetails(t *testing.T) {
	// The platform may name the choices in the refusal itself; then no
	// listing call is needed (and none is possible here: no /v1/accounts).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/organizations" {
			linkErr(w, 412, "precondition_failed", needsAccountMsg, map[string]interface{}{
				"accounts": []map[string]string{{"id": "ws_ONE", "slug": "one"}, {"id": "ws_TWO", "slug": "two"}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ORUN_CREDENTIAL_STORE", "file")
	t.Setenv("ORUN_TOKEN", "tok")
	t.Setenv("ORUN_TOKEN_FILE", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv(backendURLEnvVar, srv.URL)
	_, err := captureStdoutErr(t, func() error {
		return runCloudWorkspaceCreate(context.Background(), workspaceCreateOpts{Name: "X", CommandPath: "orun workspace create"})
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"orun workspace create X --account one", "orun workspace create X --account two"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("hint missing %q:\n%s", want, err.Error())
		}
	}
}

func TestWorkspaceCreate_OtherErrorsAreNotTurnedIntoTheHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/organizations" {
			linkErr(w, 409, "conflict", "slug already taken", nil)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ORUN_CREDENTIAL_STORE", "file")
	t.Setenv("ORUN_TOKEN", "tok")
	t.Setenv("ORUN_TOKEN_FILE", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv(backendURLEnvVar, srv.URL)
	_, err := captureStdoutErr(t, func() error {
		return runCloudWorkspaceCreate(context.Background(), workspaceCreateOpts{Name: "X"})
	})
	if err == nil || !strings.Contains(err.Error(), "create workspace: slug already taken (conflict)") {
		t.Errorf("error = %v, want the plain create error", err)
	}
	if err != nil && strings.Contains(err.Error(), "--account") {
		t.Errorf("a 409 must not get the account hint: %v", err)
	}
}

func TestWorkspaceCreateCommandLine(t *testing.T) {
	cases := []struct {
		opts    workspaceCreateOpts
		account string
		want    string
	}{
		{workspaceCreateOpts{Name: "Acme Platform", Slug: "acme", CommandPath: "orun workspace create"}, "acme-inc",
			"orun workspace create 'Acme Platform' --slug acme --account acme-inc"},
		{workspaceCreateOpts{Name: "lab", CommandPath: "orun cloud workspace create"}, "ws_LAB",
			"orun cloud workspace create lab --account ws_LAB"},
		{workspaceCreateOpts{Name: "it's"}, "<ws_…|slug>",
			`orun workspace create 'it'\''s' --account <ws_…|slug>`},
	}
	for _, tc := range cases {
		if got := workspaceCreateCommandLine(tc.opts, tc.account); got != tc.want {
			t.Errorf("workspaceCreateCommandLine(%+v, %q)\n got %s\nwant %s", tc.opts, tc.account, got, tc.want)
		}
	}
}

func TestRenderWorkspaceList_AccountColumnOnlyWhenKnown(t *testing.T) {
	orgs := []orgChoice{
		{ID: "org_1", WorkspaceRef: "ws_OGPIC", Slug: "ogpic", Role: "owner"},
		{ID: "org_2", WorkspaceRef: "ws_HALO", Slug: "halo", Role: "owner"},
	}
	plain := renderWorkspaceList(orgs, "ws_HALO")
	if strings.Contains(plain, "ACCOUNT") {
		t.Errorf("no account known: the column must not appear:\n%s", plain)
	}
	if !strings.Contains(plain, "*  halo") {
		t.Errorf("current workspace not marked:\n%s", plain)
	}
	orgs[0].AccountID, orgs[0].AccountSlug = "ws_ACME", "acme"
	orgs[1].AccountID = "ws_LAB"
	withAccounts := renderWorkspaceList(orgs, "")
	lines := strings.Split(strings.TrimSpace(withAccounts), "\n")
	if len(lines) != 3 || !strings.HasSuffix(lines[0], "ACCOUNT") {
		t.Fatalf("want a header ending in ACCOUNT and two rows:\n%s", withAccounts)
	}
	if !strings.HasSuffix(lines[1], "acme") || !strings.HasSuffix(lines[2], "ws_LAB") {
		t.Errorf("rows should end with the account slug, else its id:\n%s", withAccounts)
	}
}
