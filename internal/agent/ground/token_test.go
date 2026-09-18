package ground

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// doorStub serves the GS4 wire shape so the runtime leg is testable before the
// door itself exists (the AL fixture discipline).
func doorStub(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(srv.Close)
	return srv
}

func okDoor(t *testing.T, token string, ttl time.Duration, calls *int32) *httptest.Server {
	t.Helper()
	return doorStub(t, func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			atomic.AddInt32(calls, 1)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer session-bearer" {
			t.Errorf("Authorization = %q, want the session bearer", got)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"token":     token,
				"expiresAt": time.Now().Add(ttl).UTC().Format(time.RFC3339),
			},
		})
	})
}

func envFor(base string) SessionEnv {
	return SessionEnv{
		CloudAPI:  base,
		OrgID:     "org_1",
		SessionID: "as_1",
		Token:     "session-bearer",
		FullName:  "acme/storefront",
	}
}

func TestMintRepoTokenHitsTheSessionDoor(t *testing.T) {
	var calls int32
	var gotPath string
	srv := doorStub(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"token":     "ghs_minted",
				"expiresAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			},
		})
	})
	tok, err := MintRepoToken(context.Background(), srv.Client(), envFor(srv.URL))
	if err != nil {
		t.Fatalf("MintRepoToken: %v", err)
	}
	if tok.Token != "ghs_minted" {
		t.Errorf("token = %q", tok.Token)
	}
	if want := "/v1/organizations/org_1/agents/sessions/as_1/repo-token"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if calls != 1 {
		t.Errorf("door called %d times, want 1", calls)
	}
}

func TestMintRepoTokenRefusalIsTerminal(t *testing.T) {
	// 403 = the session went terminal, its lease lapsed, or a human lowered
	// the tier. Retrying only repeats it.
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls int32
			srv := doorStub(t, func(w http.ResponseWriter, _ *http.Request) {
				atomic.AddInt32(&calls, 1)
				w.WriteHeader(status)
			})
			_, err := MintRepoToken(context.Background(), srv.Client(), envFor(srv.URL))
			if !errors.Is(err, ErrRefused) {
				t.Fatalf("err = %v, want ErrRefused", err)
			}
			if calls != 1 {
				t.Errorf("door called %d times on a refusal, want exactly 1 (no retry)", calls)
			}
		})
	}
}

func TestMintRepoTokenRetriesServerErrors(t *testing.T) {
	var calls int32
	srv := doorStub(t, func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"token":     "ghs_eventually",
				"expiresAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			},
		})
	})
	tok, err := MintRepoToken(context.Background(), srv.Client(), envFor(srv.URL))
	if err != nil {
		t.Fatalf("MintRepoToken: %v", err)
	}
	if tok.Token != "ghs_eventually" || calls != 3 {
		t.Errorf("token=%q calls=%d, want a retried success", tok.Token, calls)
	}
}

func TestTokenCacheRoundTripAndExpiry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orun", "repo-token.json")
	now := time.Now()

	if _, ok := ReadCachedToken(path, now); ok {
		t.Fatal("empty cache reported a token")
	}
	fresh := RepoToken{Token: "ghs_a", ExpiresAt: now.Add(time.Hour)}
	if err := WriteCachedToken(path, fresh); err != nil {
		t.Fatalf("WriteCachedToken: %v", err)
	}
	got, ok := ReadCachedToken(path, now)
	if !ok || got.Token != "ghs_a" {
		t.Fatalf("cache round-trip failed: %+v ok=%v", got, ok)
	}

	// 0600: the one secret at rest in a grounded sandbox.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("cache mode = %o, want 600", perm)
	}

	// A token inside the skew window is spent — expiring mid-clone is worse
	// than minting a minute early.
	if _, ok := ReadCachedToken(path, now.Add(time.Hour-time.Minute)); ok {
		t.Error("a token expiring within the skew window was served as valid")
	}
	if _, ok := ReadCachedToken(path, now.Add(2*time.Hour)); ok {
		t.Error("an expired token was served")
	}

	if err := ClearCachedToken(path); err != nil {
		t.Fatalf("ClearCachedToken: %v", err)
	}
	if _, ok := ReadCachedToken(path, now); ok {
		t.Error("cache survived erase")
	}
	// Erasing an absent cache is not an error (git calls it freely).
	if err := ClearCachedToken(path); err != nil {
		t.Errorf("second erase: %v", err)
	}
}

func TestSessionFromEnvPrefersTheTokenFileThenTheBearer(t *testing.T) {
	base := map[string]string{
		"ORUN_CLOUD_API":      "https://api.example",
		"ORUN_ORG_ID":         "org_1",
		"ORUN_SESSION_ID":     "as_1",
		"ORUN_REPO_FULL_NAME": "acme/storefront",
	}
	get := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	// No token file: the bearer.
	withBearer := map[string]string{"ORUN_SESSION_TOKEN": "bearer"}
	for k, v := range base {
		withBearer[k] = v
	}
	s, err := SessionFromEnv(get(withBearer))
	if err != nil || s.Token != "bearer" {
		t.Fatalf("bearer path: token=%q err=%v", s.Token, err)
	}

	// Falling back to the rotation-carrying file (GS2 writes it) means the
	// helper keeps working across a token rotation.
	f := filepath.Join(t.TempDir(), "session-token")
	if err := os.WriteFile(f, []byte("  rotated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withFile := map[string]string{"ORUN_TOKEN_FILE": f}
	for k, v := range base {
		withFile[k] = v
	}
	s, err = SessionFromEnv(get(withFile))
	if err != nil || s.Token != "rotated" {
		t.Fatalf("file path: token=%q err=%v", s.Token, err)
	}

	// BOTH: the file. The environment keeps the BOOT token, which serve
	// rotates away within a quarter-hour; the file carries every rotation.
	// Preferring the environment minted with a dead credential for the rest
	// of an hours-long build.
	both := map[string]string{"ORUN_SESSION_TOKEN": "boot", "ORUN_TOKEN_FILE": f}
	for k, v := range base {
		both[k] = v
	}
	s, err = SessionFromEnv(get(both))
	if err != nil || s.Token != "rotated" {
		t.Fatalf("both set: token=%q err=%v, want the rotated file", s.Token, err)
	}
	// An unreadable file falls back rather than failing.
	both["ORUN_TOKEN_FILE"] = filepath.Join(t.TempDir(), "absent")
	s, err = SessionFromEnv(get(both))
	if err != nil || s.Token != "boot" {
		t.Fatalf("absent file: token=%q err=%v, want the bearer", s.Token, err)
	}

	// No credential at all: not a grounded session, and the error names what
	// is missing rather than failing blank.
	if _, err := SessionFromEnv(get(base)); err == nil {
		t.Fatal("expected an error with no credential")
	} else if want := "ORUN_SESSION_TOKEN"; !contains(err.Error(), want) {
		t.Errorf("error %q does not name %q", err, want)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

func TestCachePathIsRuntimeState(t *testing.T) {
	dir := t.TempDir()
	got := CachePath(func(k string) string {
		if k == "XDG_RUNTIME_DIR" {
			return dir
		}
		return ""
	})
	if want := filepath.Join(dir, "orun", "repo-token.json"); got != want {
		t.Errorf("CachePath = %q, want %q", got, want)
	}
	// Without XDG_RUNTIME_DIR it still lands somewhere writable, never the repo.
	if fallback := CachePath(func(string) string { return "" }); fallback == "" {
		t.Error("CachePath returned empty without XDG_RUNTIME_DIR")
	}
}

func TestSessionTokenFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orun", "session-token")
	if err := WriteSessionToken(path, "bearer-one"); err != nil {
		t.Fatalf("WriteSessionToken: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(b)); got != "bearer-one" {
		t.Errorf("token = %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}

	// Rotation overwrites in place: readers re-read per call, so the NEXT
	// `orun` verb in the sandbox picks the new credential up with no restart.
	if err := WriteSessionToken(path, "bearer-two"); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	b, _ = os.ReadFile(path)
	if got := strings.TrimSpace(string(b)); got != "bearer-two" {
		t.Errorf("after rotation token = %q, want bearer-two", got)
	}
}

func TestWriteSessionTokenRefusesEmpty(t *testing.T) {
	// An empty file would read as "no credential" and send every in-sandbox
	// verb down the not-logged-in path — worse than leaving the old one.
	path := filepath.Join(t.TempDir(), "session-token")
	if err := WriteSessionToken(path, "   "); err == nil {
		t.Fatal("expected an error writing an empty token")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("an empty token created a file")
	}
}

func TestTokenFilePathHonorsExplicitOverride(t *testing.T) {
	got := TokenFilePath(func(k string) string {
		if k == "ORUN_TOKEN_FILE" {
			return "/custom/path"
		}
		return ""
	})
	if got != "/custom/path" {
		t.Errorf("TokenFilePath = %q, want the explicit override", got)
	}
}

func TestTokenFilePathDefaultsToRuntimeState(t *testing.T) {
	dir := t.TempDir()
	got := TokenFilePath(func(k string) string {
		if k == "XDG_RUNTIME_DIR" {
			return dir
		}
		return ""
	})
	if want := filepath.Join(dir, "orun", "session-token"); got != want {
		t.Errorf("TokenFilePath = %q, want %q", got, want)
	}
}

// One resolver for "a GitHub token for this session's repository": the cache
// while it is comfortably valid, a mint when it is not — so a caller can ask
// per request, which is what keeps an hour-long wait authenticated.
func TestRepoTokenFromEnvCachesThenMints(t *testing.T) {
	var calls int32
	door := okDoor(t, "ghs_minted", time.Hour, &calls)
	runtime := t.TempDir()
	env := map[string]string{
		"ORUN_CLOUD_API":      door.URL,
		"ORUN_ORG_ID":         "org_1",
		"ORUN_SESSION_ID":     "as_1",
		"ORUN_SESSION_TOKEN":  "session-bearer",
		"ORUN_REPO_FULL_NAME": "acme/storefront",
		"XDG_RUNTIME_DIR":     runtime,
	}
	get := func(k string) string { return env[k] }
	for i := 0; i < 3; i++ {
		tok, err := RepoTokenFromEnv(context.Background(), get)
		if err != nil || tok != "ghs_minted" {
			t.Fatalf("call %d: %q, %v", i, tok, err)
		}
	}
	if calls != 1 {
		t.Fatalf("minted %d times for three asks within one lifetime, want 1", calls)
	}
	// Near expiry the cache is not trusted, and the next ask mints.
	if err := WriteCachedToken(CachePath(get), RepoToken{Token: "ghs_old", ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if tok, err := RepoTokenFromEnv(context.Background(), get); err != nil || tok != "ghs_minted" || calls != 2 {
		t.Fatalf("an expiring cache must be replaced: %q, %v, mints=%d", tok, err, calls)
	}
}

func TestRepoTokenFromEnvOutsideASessionIsAnError(t *testing.T) {
	if _, err := RepoTokenFromEnv(context.Background(), func(string) string { return "" }); err == nil {
		t.Fatal("no session in the environment must be an error, not an empty token")
	}
}
