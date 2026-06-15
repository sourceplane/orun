package remotestate_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/remotestate"
)

// writeSession writes a credentials.json under $HOME/.orun for the file-backed
// credential store. On non-darwin platforms the keychain is unavailable, so the
// file store is used directly.
func writeSession(t *testing.T, home string, creds cliauth.Credentials) {
	t.Helper()
	if runtime.GOOS == "darwin" {
		t.Skip("test relies on the file credential store; skip where the keychain takes precedence")
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

func TestSessionTokenSource_UsesValidAccessToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSession(t, home, cliauth.Credentials{
		AccessToken:       "still-valid",
		AccessTokenExpiry: time.Now().Add(10 * time.Minute).Format(time.RFC3339),
		RefreshToken:      "r1",
		BackendURL:        "https://api.example.com",
	})

	src := &remotestate.SessionTokenSource{BackendURL: "https://api.example.com", Version: "test"}
	tok, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token error: %v", err)
	}
	if tok != "still-valid" {
		t.Errorf("token = %q, want still-valid", tok)
	}
}

func TestSessionTokenSource_RefreshesExpiredAndRotates(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/cli/token" {
			http.Error(w, "unexpected path", 404)
			return
		}
		atomic.AddInt32(&calls, 1)
		// Assert the platform grantType discriminator + refreshToken body (OP1).
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["grantType"] != "refresh_token" {
			t.Errorf("cli/token grantType = %q, want refresh_token", body["grantType"])
		}
		if body["refreshToken"] != "r1" {
			t.Errorf("cli/token refreshToken = %q, want r1", body["refreshToken"])
		}
		w.Header().Set("Content-Type", "application/json")
		// Real OP1 success envelope: { data, meta }. The rotation response may
		// omit user/orgs (only mints a new access + refresh pair).
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"accessToken":  "fresh-access",
				"expiresAt":    time.Now().Add(15 * time.Minute).Format(time.RFC3339),
				"refreshToken": "r2",
			},
			"meta": map[string]any{"requestId": "req-test", "cursor": nil},
		})
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSession(t, home, cliauth.Credentials{
		AccessToken:       "expired",
		AccessTokenExpiry: time.Now().Add(-time.Minute).Format(time.RFC3339),
		RefreshToken:      "r1",
		BackendURL:        srv.URL,
	})

	src := &remotestate.SessionTokenSource{BackendURL: srv.URL, Version: "test"}
	tok, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token error: %v", err)
	}
	if tok != "fresh-access" {
		t.Errorf("token = %q, want fresh-access", tok)
	}
	// The rotated refresh token must be persisted.
	loaded, err := cliauth.LoadSession()
	if err != nil || loaded == nil {
		t.Fatalf("LoadSession: %v / %v", loaded, err)
	}
	if loaded.RefreshToken != "r2" {
		t.Errorf("persisted refresh = %q, want r2 (rotated)", loaded.RefreshToken)
	}
}

// TestSessionTokenSource_RefreshesWithinSkewWindow proves the proactive skew:
// a token that is still technically valid but expires inside the skew window is
// refreshed ahead of time, so a command never starts work with a token about to
// expire mid-flight.
func TestSessionTokenSource_RefreshesWithinSkewWindow(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"accessToken":  "fresh-access",
				"expiresAt":    time.Now().Add(15 * time.Minute).Format(time.RFC3339),
				"refreshToken": "r2",
			},
			"meta": map[string]any{"requestId": "req-test", "cursor": nil},
		})
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	// Access token expires in 20s — still valid, but inside the 60s skew window.
	writeSession(t, home, cliauth.Credentials{
		AccessToken:       "about-to-expire",
		AccessTokenExpiry: time.Now().Add(20 * time.Second).Format(time.RFC3339),
		RefreshToken:      "r1",
		BackendURL:        srv.URL,
	})

	src := &remotestate.SessionTokenSource{BackendURL: srv.URL, Version: "test"}
	tok, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token error: %v", err)
	}
	if tok != "fresh-access" {
		t.Errorf("token = %q, want fresh-access (proactive refresh within skew)", tok)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server called %d times, want 1 (refreshed ahead of expiry)", got)
	}
}

// TestSessionTokenSource_ConcurrentRefreshRedeemsOnce is the core regression
// for "the token expires seconds after login". Many concurrent callers hit an
// expired access token at once; the rotating, single-use refresh token must be
// redeemed EXACTLY once (otherwise the losers replay a spent token, trip
// reuse-detection, and the platform revokes the whole family). All callers must
// receive the one freshly minted access token.
func TestSessionTokenSource_ConcurrentRefreshRedeemsOnce(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		// A second redemption means a sibling replayed the spent token — that is
		// exactly the bug under test. Fail loudly by returning the reuse error so
		// the assertion below catches it deterministically.
		if n > 1 {
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "unauthenticated", "message": "Refresh token reuse detected; session revoked", "details": map[string]any{}, "requestId": "req-reuse"},
			})
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["refreshToken"] != "r1" {
			t.Errorf("redeemed refreshToken = %q, want r1", body["refreshToken"])
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"accessToken":  "fresh-access",
				"expiresAt":    time.Now().Add(15 * time.Minute).Format(time.RFC3339),
				"refreshToken": "r2",
			},
			"meta": map[string]any{"requestId": "req-test", "cursor": nil},
		})
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSession(t, home, cliauth.Credentials{
		AccessToken:       "expired",
		AccessTokenExpiry: time.Now().Add(-time.Minute).Format(time.RFC3339),
		RefreshToken:      "r1",
		BackendURL:        srv.URL,
	})

	src := &remotestate.SessionTokenSource{BackendURL: srv.URL, Version: "test"}

	const n = 12
	var wg sync.WaitGroup
	start := make(chan struct{})
	toks := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // release all goroutines together to maximize the race
			toks[i], errs[i] = src.Token(context.Background())
		}(i)
	}
	close(start)
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: Token error: %v", i, errs[i])
		}
		if toks[i] != "fresh-access" {
			t.Errorf("goroutine %d: token = %q, want fresh-access", i, toks[i])
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("refresh token redeemed %d times, want exactly 1 (concurrent refresh must coalesce)", got)
	}
	loaded, err := cliauth.LoadSession()
	if err != nil || loaded == nil || loaded.RefreshToken != "r2" {
		t.Fatalf("persisted refresh = %v (err %v), want r2", loaded, err)
	}
}

func TestSessionTokenSource_FamilyRevokedSingleError(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		// Real OP1 reuse/family-revoked response: HTTP 401, code "unauthenticated".
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "unauthenticated", "message": "Refresh token reuse detected; session revoked", "details": map[string]any{}, "requestId": "req-9"},
		})
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSession(t, home, cliauth.Credentials{
		AccessToken:       "expired",
		AccessTokenExpiry: time.Now().Add(-time.Minute).Format(time.RFC3339),
		RefreshToken:      "r1",
		BackendURL:        srv.URL,
	})

	src := &remotestate.SessionTokenSource{BackendURL: srv.URL, Version: "test"}

	// First call: refresh fails with family_revoked → ErrSessionRevoked, session cleared.
	_, err := src.Token(context.Background())
	if !errors.Is(err, cliauth.ErrSessionRevoked) {
		t.Fatalf("first Token error = %v, want ErrSessionRevoked", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server called %d times on first Token, want 1", got)
	}

	// Session must be cleared so a subsequent call does NOT hit the server again
	// (no per-call spam): it should fail locally with the same single error.
	_, err = src.Token(context.Background())
	if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, cliauth.ErrSessionRevoked) {
		t.Fatalf("second Token error = %v, want ErrNotExist or ErrSessionRevoked", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server called %d times total; expected no second refresh attempt after revoke", got)
	}
}
