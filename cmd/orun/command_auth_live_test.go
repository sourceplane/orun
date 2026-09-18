package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/cliauth"
)

// The live check in `orun auth status` (2026-09-18). Status used to print the
// stored login and stop, so a login the server no longer honoured read as
// healthy until the next real command failed.

// liveSession writes a file-store session. The file store is forced explicitly
// (and test binaries never reach the keychain since #669), so these run on
// macOS too — unlike the older helpers that skip there.
func liveSession(t *testing.T, backend string, accessValid bool) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORUN_CREDENTIAL_STORE", "file")
	t.Setenv(backendURLEnvVar, "")
	t.Chdir(home)
	exp := time.Now().Add(-time.Minute)
	if accessValid {
		exp = time.Now().Add(10 * time.Minute)
	}
	creds := cliauth.Credentials{
		AccessToken:       "access-1",
		AccessTokenExpiry: exp.UTC().Format(time.RFC3339),
		RefreshToken:      "refresh-1",
		User:              cliauth.SessionUser{ID: "u1", Email: "dev@example.com", DisplayName: "Dev"},
		BackendURL:        backend,
	}
	dir := filepath.Join(home, ".orun")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(creds)
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	authBackendURL = ""
	authOffline = false
	t.Cleanup(func() { authOffline = false })
}

func TestRunAuthStatus_ValidAccessTokenIsUsableWithoutCallingTheBackend(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "should not be called", http.StatusTeapot)
	}))
	defer srv.Close()
	liveSession(t, srv.URL, true)

	out, err := captureStdoutErr(t, runAuthStatus)
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Session: ") || !strings.Contains(out, "usable") {
		t.Errorf("want a usable session line\n%s", out)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Errorf("backend called %d times for a still-valid access token", calls)
	}
}

func TestRunAuthStatus_ExpiredAccessTokenIsRefreshedAndReportedActive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"accessToken":  "access-2",
				"expiresAt":    time.Now().Add(15 * time.Minute).Format(time.RFC3339),
				"refreshToken": "refresh-2",
			},
			"meta": map[string]any{"requestId": "r", "cursor": nil},
		})
	}))
	defer srv.Close()
	liveSession(t, srv.URL, false)

	out, err := captureStdoutErr(t, runAuthStatus)
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "active (refreshed just now)") {
		t.Errorf("want an active, refreshed session\n%s", out)
	}
}

func TestRunAuthStatus_ARefusedRefreshIsReportedAndFailsTheCommand(t *testing.T) {
	// The exact failure behind the 2026-09-18 logouts: the stored login looks
	// fine, but the backend will not honour its refresh token.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "unauthenticated", "message": "Invalid refresh token", "details": map[string]any{}, "requestId": "r"},
		})
	}))
	defer srv.Close()
	liveSession(t, srv.URL, false)

	out, err := captureStdoutErr(t, runAuthStatus)
	if !errors.Is(err, cliauth.ErrSessionRevoked) {
		t.Fatalf("err = %v, want ErrSessionRevoked (non-zero exit)\n%s", err, out)
	}
	if !strings.Contains(out, "no longer valid") || !strings.Contains(out, "orun auth login") {
		t.Errorf("want an actionable 'no longer valid' line\n%s", out)
	}
}

func TestRunAuthStatus_AnUnreachableBackendIsNotAVerdict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing listens here any more
	liveSession(t, url, false)

	out, err := captureStdoutErr(t, runAuthStatus)
	if err != nil {
		t.Fatalf("an outage must not fail status as if logged out: %v\n%s", err, out)
	}
	if !strings.Contains(out, "could not verify") {
		t.Errorf("want 'could not verify'\n%s", out)
	}
	if strings.Contains(out, "no longer valid") {
		t.Errorf("an outage was reported as a dead login\n%s", out)
	}
}

func TestRunAuthStatus_OfflineSkipsTheCheck(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer srv.Close()
	liveSession(t, srv.URL, false)
	authOffline = true

	out, err := captureStdoutErr(t, runAuthStatus)
	if err != nil {
		t.Fatalf("status --offline: %v", err)
	}
	if strings.Contains(out, "Session: ") || atomic.LoadInt32(&calls) != 0 {
		t.Errorf("--offline must not check the backend (calls=%d)\n%s", calls, out)
	}
}
