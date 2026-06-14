package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/cliauth"
)

// captureStdoutErr runs fn with os.Stdout redirected to a pipe and returns both
// what was written and fn's error (unlike the package captureStdout helper,
// which fatals on error). The auth commands print directly to os.Stdout.
func captureStdoutErr(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	outCh := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		outCh <- buf.String()
	}()
	runErr := fn()
	_ = w.Close()
	out := <-outCh
	os.Stdout = orig
	return out, runErr
}

func writeCmdSession(t *testing.T, home string, creds cliauth.Credentials) {
	t.Helper()
	if runtime.GOOS == "darwin" {
		t.Skip("auth output test relies on the file credential store")
	}
	dir := filepath.Join(home, ".orun")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestRunAuthStatus_ShowsUserOrgsExpiryBackend(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Run from the temp dir so git remote resolution does not pick up the orun repo.
	t.Chdir(home)
	t.Setenv(backendURLEnvVar, "")

	exp := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)
	writeCmdSession(t, home, cliauth.Credentials{
		AccessToken:       "tok",
		AccessTokenExpiry: exp,
		User:              cliauth.SessionUser{ID: "u1", Email: "dev@example.com", DisplayName: "Dev User"},
		Orgs: []cliauth.OrgRef{
			{ID: "o1", Slug: "acme", Name: "Acme Inc", Role: "admin"},
			{ID: "o2", Slug: "beta", Name: "Beta", Role: "member"},
		},
		BackendURL: "https://api.example.com",
	})

	authBackendURL = ""
	out, err := captureStdoutErr(t, runAuthStatus)
	if err != nil {
		t.Fatalf("runAuthStatus error: %v", err)
	}
	for _, want := range []string{
		"Dev User",
		"https://api.example.com",
		"Acme Inc",
		"admin",
		"Beta",
		"member",
		exp,
		"valid",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestRunAuthStatus_NotLoggedIn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(home)
	authBackendURL = ""

	_, err := captureStdoutErr(t, runAuthStatus)
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
	if !strings.Contains(err.Error(), "auth login") {
		t.Errorf("error = %v, want a 'auth login' hint", err)
	}
}

func TestRunAuthToken_RefreshesExpiredAndPrints(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("auth token test relies on the file credential store")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/cli/token" {
			http.Error(w, "unexpected path", 404)
			return
		}
		// Assert the OP1 grantType discriminator on the refresh body.
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["grantType"] != "refresh_token" {
			t.Errorf("cli/token grantType = %q, want refresh_token", body["grantType"])
		}
		w.Header().Set("Content-Type", "application/json")
		// Real OP1 success envelope: { data, meta }.
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"accessToken":  "fresh-token",
				"expiresAt":    time.Now().Add(15 * time.Minute).Format(time.RFC3339),
				"refreshToken": "r2",
			},
			"meta": map[string]any{"requestId": "req-test", "cursor": nil},
		})
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(home)
	os.Unsetenv("GITHUB_ACTIONS")
	os.Unsetenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	os.Unsetenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	os.Unsetenv("ORUN_TOKEN")

	writeCmdSession(t, home, cliauth.Credentials{
		AccessToken:       "expired",
		AccessTokenExpiry: time.Now().Add(-time.Minute).Format(time.RFC3339),
		RefreshToken:      "r1",
		BackendURL:        srv.URL,
	})

	authBackendURL = srv.URL
	out, err := captureStdoutErr(t, runAuthToken)
	if err != nil {
		t.Fatalf("runAuthToken error: %v", err)
	}
	if strings.TrimSpace(out) != "fresh-token" {
		t.Errorf("token output = %q, want fresh-token", strings.TrimSpace(out))
	}
}

func TestRunAuthToken_RevokedSingleError(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("auth token test relies on the file credential store")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Real OP1 reuse/family-revoked response: HTTP 401, code "unauthenticated".
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "unauthenticated", "message": "revoked", "details": map[string]any{}, "requestId": "req-9"},
		})
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(home)
	os.Unsetenv("GITHUB_ACTIONS")
	os.Unsetenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	os.Unsetenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	os.Unsetenv("ORUN_TOKEN")

	writeCmdSession(t, home, cliauth.Credentials{
		AccessToken:       "expired",
		AccessTokenExpiry: time.Now().Add(-time.Minute).Format(time.RFC3339),
		RefreshToken:      "r1",
		BackendURL:        srv.URL,
	})

	authBackendURL = srv.URL
	_, err := captureStdoutErr(t, runAuthToken)
	if !errors.Is(err, cliauth.ErrSessionRevoked) {
		t.Fatalf("error = %v, want ErrSessionRevoked", err)
	}
	if !strings.Contains(err.Error(), "auth login") {
		t.Errorf("error = %v, want a 'auth login' hint", err)
	}
}
