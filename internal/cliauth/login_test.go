package cliauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// platformAuthServer is a minimal mock of the platform CLI auth endpoints (OP1)
// used by the login/refresh flows. Every success response is the real
// { data, meta } envelope; every error is the real { error: { code, message,
// requestId } } envelope. It records the last refresh tokens seen so tests can
// assert single-use rotation behavior.
type platformAuthServer struct {
	srv *httptest.Server

	// session is returned by cli/token redeem and device-poll once approved.
	session SessionResponse

	// browserApproveAfter is the number of cli/token (grantType cli_code) polls
	// that return "Not yet approved" (HTTP 400) before the grant is approved and
	// a session is minted.
	browserApproveAfter int
	browserPolls        int32

	// deviceApproveAfter is the number of device/poll calls that return pending
	// (status:"pending") before the flow returns a complete session.
	deviceApproveAfter int
	devicePolls        int32

	// deviceTerminal, when set, forces device/poll to a terminal HTTP error:
	// "denied" → 403 access_denied, "expired" → 410 expired.
	deviceTerminal string

	// refresh rotation state.
	currentRefresh string
	rotateTo       string
	familyRevoked  bool

	// lastTokenBody records the most recent cli/token request body so tests can
	// assert the grantType discriminator and field names.
	lastTokenBody map[string]string
}

func newPlatformAuthServer(t *testing.T) *platformAuthServer {
	t.Helper()
	p := &platformAuthServer{}
	mux := http.NewServeMux()

	mux.HandleFunc(cliStartPath, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Host        string `json:"host"`
			RedirectURI string `json:"redirectUri"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		// The platform accepts NO redirectUri — the CLI must not send one.
		if body.RedirectURI != "" {
			t.Errorf("cli/start body included redirectUri %q; OP1 accepts only host", body.RedirectURI)
		}
		writeData(w, 201, CLIStartResponse{
			AuthorizeURL: p.srv.URL + "/authorize?code=cli-code-123",
			CLICode:      "cli-code-123",
			ExpiresAt:    time.Now().Add(5 * time.Minute).Format(time.RFC3339),
		})
	})

	mux.HandleFunc(cliTokenPath, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		p.lastTokenBody = body
		switch body["grantType"] {
		case grantTypeRefreshToken:
			rt := body["refreshToken"]
			if rt == "" {
				writeError(w, 400, "validation_failed", "refreshToken required")
				return
			}
			if p.familyRevoked || (p.currentRefresh != "" && rt != p.currentRefresh) {
				// Reuse of a spent refresh token (or explicit revoke) ⇒ the
				// platform revokes the family and answers 401 unauthenticated.
				writeError(w, 401, "unauthenticated", "Refresh token reuse detected; session revoked")
				return
			}
			resp := p.session
			resp.RefreshToken = p.rotateTo
			p.currentRefresh = p.rotateTo
			writeData(w, 200, resp)
		case grantTypeCLICode:
			if body["cliCode"] == "" {
				writeError(w, 422, "validation_failed", "cliCode required")
				return
			}
			n := atomic.AddInt32(&p.browserPolls, 1)
			if int(n) <= p.browserApproveAfter {
				// Pending: platform returns HTTP 400 "Not yet approved".
				writeError(w, 400, "validation_failed", "Not yet approved")
				return
			}
			writeData(w, 200, p.session)
		default:
			writeError(w, 422, "validation_failed", "Must be 'cli_code' or 'refresh_token'")
		}
	})

	mux.HandleFunc(cliDeviceStartPath, func(w http.ResponseWriter, r *http.Request) {
		writeData(w, 201, DeviceStartResponse{
			DeviceCode:      "device-code-abc",
			UserCode:        "WXYZ-1234",
			VerificationURL: p.srv.URL + "/device",
			Interval:        1, // keep the test fast
			ExpiresAt:       time.Now().Add(5 * time.Minute).Format(time.RFC3339),
		})
	})

	mux.HandleFunc(cliDevicePollPath, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&p.devicePolls, 1)
		switch p.deviceTerminal {
		case "denied":
			writeError(w, 403, "access_denied", "Device authorization was denied")
			return
		case "expired":
			writeError(w, 410, "expired", "Device code expired")
			return
		}
		if int(n) <= p.deviceApproveAfter {
			// Pending: status under data, error mirrors RFC-8628.
			writeData(w, 200, map[string]any{"status": "pending", "error": "authorization_pending"})
			return
		}
		// Complete: the session is NESTED under data.session.
		writeData(w, 200, map[string]any{"status": "complete", "session": p.session})
	})

	mux.HandleFunc(cliRevokePath, func(w http.ResponseWriter, r *http.Request) {
		writeData(w, 200, map[string]any{"success": true})
	})

	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

// writeData emits the platform's { data, meta } success envelope (OP1).
func writeData(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": payload,
		"meta": map[string]any{"requestId": "req-test", "cursor": nil},
	})
}

// writeError emits the platform's { error: { code, message, requestId } }
// envelope (OP1) at the given HTTP status.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"code": code, "message": message, "details": map[string]any{}, "requestId": "req-test"},
	})
}

// fileStoreHome points credential storage at a temp HOME with the keychain
// disabled so tests neither prompt nor touch the real home directory.
func fileStoreHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	userHomeDir = func() (string, error) { return tmp, nil }
	lookPath = func(string) (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() {
		userHomeDir = os.UserHomeDir
		lookPath = exec.LookPath
	})
	return tmp
}

func sampleSession() SessionResponse {
	return SessionResponse{
		AccessToken:      "access-tok",
		ExpiresAt:        time.Now().Add(15 * time.Minute).Format(time.RFC3339),
		RefreshToken:     "refresh-1",
		RefreshExpiresAt: time.Now().Add(720 * time.Hour).Format(time.RFC3339),
		User:             SessionUser{ID: "u1", Email: "dev@example.com", DisplayName: "Dev User"},
		Orgs:             []OrgRef{{ID: "org-1", Slug: "acme", Name: "Acme", Role: "admin"}},
	}
}

func TestBrowserLogin_StartAndPollToRedeem(t *testing.T) {
	fileStoreHome(t)
	p := newPlatformAuthServer(t)
	p.session = sampleSession()
	// Two cli/token polls return "Not yet approved" (HTTP 400), then a session.
	p.browserApproveAfter = 2

	var opened atomic.Bool
	opener := func(authorizeURL string) error {
		// The platform owns approval; the CLI only opens the page and polls.
		if !strings.Contains(authorizeURL, "/authorize?code=") {
			t.Errorf("authorize URL = %q, want authorize page", authorizeURL)
		}
		if strings.Contains(authorizeURL, "redirect=") {
			t.Errorf("authorize URL %q must not carry a loopback redirect", authorizeURL)
		}
		opened.Store(true)
		return nil
	}

	creds, err := BrowserLogin(context.Background(), p.srv.URL, "test", nil, opener)
	if err != nil {
		t.Fatalf("BrowserLogin error: %v", err)
	}
	if !opened.Load() {
		t.Error("browser was not opened")
	}
	// The redeem must use the grantType "cli_code" discriminator + cliCode.
	if p.lastTokenBody["grantType"] != grantTypeCLICode {
		t.Errorf("cli/token grantType = %q, want %q", p.lastTokenBody["grantType"], grantTypeCLICode)
	}
	if p.lastTokenBody["cliCode"] != "cli-code-123" {
		t.Errorf("cli/token cliCode = %q, want cli-code-123", p.lastTokenBody["cliCode"])
	}
	if _, ok := p.lastTokenBody["grant"]; ok {
		t.Errorf("cli/token body must not include a 'grant' field: %+v", p.lastTokenBody)
	}
	if got := atomic.LoadInt32(&p.browserPolls); got < 3 {
		t.Errorf("expected at least 3 cli/token polls, got %d", got)
	}
	if creds.AccessToken != "access-tok" {
		t.Errorf("AccessToken = %q, want access-tok", creds.AccessToken)
	}
	if creds.RefreshToken != "refresh-1" {
		t.Errorf("RefreshToken = %q, want refresh-1", creds.RefreshToken)
	}
	if creds.DisplayUser() != "Dev User" {
		t.Errorf("DisplayUser = %q, want Dev User", creds.DisplayUser())
	}
	if len(creds.Orgs) != 1 || creds.Orgs[0].Role != "admin" {
		t.Errorf("Orgs = %+v, want one admin org", creds.Orgs)
	}
	// Session should have been persisted.
	loaded, err := LoadSession()
	if err != nil || loaded == nil {
		t.Fatalf("LoadSession after login: %v / %v", loaded, err)
	}
	if loaded.AccessToken != "access-tok" {
		t.Errorf("persisted AccessToken = %q", loaded.AccessToken)
	}
}

func TestBrowserLogin_TerminalRedeemError(t *testing.T) {
	fileStoreHome(t)
	p := newPlatformAuthServer(t)
	p.session = sampleSession()

	// cli/start hands back a code, but cli/token reports the grant not_found
	// (terminal, HTTP 401) instead of a pending state. BrowserLogin must fail
	// rather than poll forever.
	mux := http.NewServeMux()
	mux.HandleFunc(cliStartPath, func(w http.ResponseWriter, r *http.Request) {
		writeData(w, 201, CLIStartResponse{
			AuthorizeURL: "https://console.example/authorize?code=x",
			CLICode:      "cli-code-123",
			ExpiresAt:    time.Now().Add(5 * time.Minute).Format(time.RFC3339),
		})
	})
	mux.HandleFunc(cliTokenPath, func(w http.ResponseWriter, r *http.Request) {
		writeError(w, 401, "unauthenticated", "Unknown or used code")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := BrowserLogin(context.Background(), srv.URL, "test", nil, func(string) error { return nil })
	if err == nil {
		t.Fatal("expected a terminal error when the grant is not found")
	}
	if !strings.Contains(err.Error(), "redeem Orun login") {
		t.Errorf("error = %v, want a redeem failure", err)
	}
}

func TestDeviceLogin_StartAndPoll(t *testing.T) {
	fileStoreHome(t)
	p := newPlatformAuthServer(t)
	p.session = sampleSession()
	p.deviceApproveAfter = 2 // two pending polls, then a session

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	creds, err := DeviceLogin(ctx, p.srv.URL, "test", nil)
	if err != nil {
		t.Fatalf("DeviceLogin error: %v", err)
	}
	if creds.AccessToken != "access-tok" {
		t.Errorf("AccessToken = %q, want access-tok", creds.AccessToken)
	}
	if atomic.LoadInt32(&p.devicePolls) < 3 {
		t.Errorf("expected at least 3 polls, got %d", p.devicePolls)
	}
	// Sanity: it should have slept between polls (default interval).
	if time.Since(start) < time.Second {
		t.Logf("device login completed quickly (%s); poll backoff may not have applied", time.Since(start))
	}
}

func TestRefreshSession_RotatesRefreshToken(t *testing.T) {
	fileStoreHome(t)
	p := newPlatformAuthServer(t)
	p.session = sampleSession()
	p.session.AccessToken = "access-2"
	p.currentRefresh = "refresh-1"
	p.rotateTo = "refresh-2"

	creds := &Credentials{
		AccessToken:  "old-access",
		RefreshToken: "refresh-1",
		BackendURL:   p.srv.URL,
	}
	if err := SaveSession(creds); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	updated, err := RefreshSession(context.Background(), p.srv.URL, "test", creds)
	if err != nil {
		t.Fatalf("RefreshSession error: %v", err)
	}
	// Refresh must use the grantType "refresh_token" discriminator + refreshToken.
	if p.lastTokenBody["grantType"] != grantTypeRefreshToken {
		t.Errorf("cli/token grantType = %q, want %q", p.lastTokenBody["grantType"], grantTypeRefreshToken)
	}
	if p.lastTokenBody["refreshToken"] == "" {
		t.Errorf("cli/token body missing refreshToken: %+v", p.lastTokenBody)
	}
	if updated.AccessToken != "access-2" {
		t.Errorf("AccessToken = %q, want access-2", updated.AccessToken)
	}
	if updated.RefreshToken != "refresh-2" {
		t.Errorf("rotated RefreshToken = %q, want refresh-2 (single-use)", updated.RefreshToken)
	}
	// Persisted creds must carry the new refresh token, not the spent one.
	loaded, _ := LoadSession()
	if loaded == nil || loaded.RefreshToken != "refresh-2" {
		t.Fatalf("persisted RefreshToken = %v, want refresh-2", loaded)
	}

	// Reusing the spent refresh token must trip family_revoked, clear the
	// session, and return ErrSessionRevoked.
	stale := &Credentials{RefreshToken: "refresh-1", BackendURL: p.srv.URL}
	_, err = RefreshSession(context.Background(), p.srv.URL, "test", stale)
	if !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("reuse error = %v, want ErrSessionRevoked", err)
	}
	if loaded, _ := LoadSession(); loaded != nil {
		t.Errorf("session should be cleared after family_revoked, got %+v", loaded)
	}
}

func TestRefreshSession_FamilyRevokedClearsSession(t *testing.T) {
	fileStoreHome(t)
	p := newPlatformAuthServer(t)
	p.session = sampleSession()
	p.familyRevoked = true

	creds := &Credentials{AccessToken: "a", RefreshToken: "refresh-1", BackendURL: p.srv.URL}
	if err := SaveSession(creds); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	_, err := RefreshSession(context.Background(), p.srv.URL, "test", creds)
	if !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("error = %v, want ErrSessionRevoked", err)
	}
	if loaded, _ := LoadSession(); loaded != nil {
		t.Errorf("expected cleared session, got %+v", loaded)
	}
}

func TestLogout_RevokeEndpoint(t *testing.T) {
	var revoked atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc(cliRevokePath, func(w http.ResponseWriter, r *http.Request) {
		revoked.Store(true)
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["refreshToken"] != "rt" {
			writeError(w, 422, "validation_failed", "missing token")
			return
		}
		writeData(w, 200, map[string]any{"success": true})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewBackendClient(srv.URL, "test")
	if err := c.Logout(context.Background(), "rt"); err != nil {
		t.Fatalf("Logout error: %v", err)
	}
	if !revoked.Load() {
		t.Error("expected revoke endpoint to be called")
	}
}

func TestDeviceLogin_DeniedTerminal(t *testing.T) {
	fileStoreHome(t)
	p := newPlatformAuthServer(t)
	p.session = sampleSession()
	p.deviceTerminal = "denied"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := DeviceLogin(ctx, p.srv.URL, "test", nil)
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("DeviceLogin error = %v, want denied", err)
	}
}

func TestDeviceLogin_ExpiredTerminal(t *testing.T) {
	fileStoreHome(t)
	p := newPlatformAuthServer(t)
	p.session = sampleSession()
	p.deviceTerminal = "expired"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := DeviceLogin(ctx, p.srv.URL, "test", nil)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("DeviceLogin error = %v, want expired", err)
	}
}
