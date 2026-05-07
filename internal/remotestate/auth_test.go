package remotestate_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

func TestOIDCTokenSource_MissingEnv(t *testing.T) {
	src := remotestate.NewOIDCTokenSource("orun")
	_, err := src.Token(context.Background())
	if err == nil {
		t.Fatal("expected error when OIDC env vars are not set")
	}
	if !strings.Contains(err.Error(), "ACTIONS_ID_TOKEN_REQUEST_URL") {
		t.Errorf("error message should mention ACTIONS_ID_TOKEN_REQUEST_URL, got: %v", err)
	}
}

func TestOIDCTokenSource_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "no auth", 401)
			return
		}
		if r.URL.Query().Get("audience") != "orun" {
			http.Error(w, "wrong audience", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"value": "test-oidc-token"})
	}))
	defer srv.Close()

	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", srv.URL+"/token")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "req-token")

	src := remotestate.NewOIDCTokenSource("orun")
	token, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "test-oidc-token" {
		t.Errorf("expected test-oidc-token, got %q", token)
	}
}

func TestStaticTokenSource(t *testing.T) {
	src := remotestate.NewStaticTokenSource("my-bearer-token")
	token, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "my-bearer-token" {
		t.Errorf("expected my-bearer-token, got %q", token)
	}
}

func TestStaticTokenSource_Empty(t *testing.T) {
	src := remotestate.NewStaticTokenSource("")
	_, err := src.Token(context.Background())
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestResolveTokenSource_OIDCAvailable(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "https://example.com/token")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "req-token")

	src, _, _, err := remotestate.ResolveTokenSource(context.Background(), remotestate.ResolveOptions{BackendURL: "https://api.example.com", Version: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src == nil {
		t.Fatal("expected non-nil source")
	}
}

func TestResolveTokenSource_FallbackToken(t *testing.T) {
	os.Unsetenv("GITHUB_ACTIONS")
	os.Unsetenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	os.Unsetenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	t.Setenv("ORUN_TOKEN", "my-token")

	src, _, _, err := remotestate.ResolveTokenSource(context.Background(), remotestate.ResolveOptions{BackendURL: "https://api.example.com", Version: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	token, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "my-token" {
		t.Errorf("expected my-token, got %q", token)
	}
}

func TestResolveTokenSource_NoToken(t *testing.T) {
	os.Unsetenv("GITHUB_ACTIONS")
	os.Unsetenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	os.Unsetenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	os.Unsetenv("ORUN_TOKEN")

	_, _, _, err := remotestate.ResolveTokenSource(context.Background(), remotestate.ResolveOptions{BackendURL: "https://api.example.com", Version: "test", Interactive: false})
	if err == nil {
		t.Fatal("expected error when no token is available")
	}
}

// TestOIDCPathDoesNotRequireLocalSession verifies that in a GitHub Actions OIDC
// context, ResolveTokenSource succeeds without a local CLI session. This ensures
// the GHA path does not invoke any local session link logic.
func TestOIDCPathDoesNotRequireLocalSession(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "https://example.com/token")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "req-token")
	// No local session; HOME points to empty temp dir.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	src, nsID, _, err := remotestate.ResolveTokenSource(context.Background(), remotestate.ResolveOptions{
		BackendURL: "https://api.example.com",
		Version:    "test",
	})
	if err != nil {
		t.Fatalf("unexpected error in OIDC path: %v", err)
	}
	if src == nil {
		t.Fatal("expected non-nil TokenSource in OIDC path")
	}
	// OIDC path does not resolve namespace ID from local session; it is empty
	// until the backend extracts it from JWT claims.
	_ = nsID
}

// TestOrunTokenPathNamespacePassThrough verifies that when ORUN_TOKEN is set,
// ResolveAuth uses the token as-is and passes through any pre-supplied NamespaceID.
// The caller is responsible for providing a namespace ID (from cache or config)
// since ORUN_TOKEN cannot call the session link endpoint.
func TestOrunTokenPathNamespacePassThrough(t *testing.T) {
	os.Unsetenv("GITHUB_ACTIONS")
	os.Unsetenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	os.Unsetenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	t.Setenv("ORUN_TOKEN", "static-machine-token")

	auth, err := remotestate.ResolveAuth(context.Background(), remotestate.ResolveOptions{
		BackendURL:  "https://api.example.com",
		Version:     "test",
		NamespaceID: "pre-cached-ns",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth.ResolvedMode != "static" {
		t.Errorf("ResolvedMode = %q, want static", auth.ResolvedMode)
	}
	if auth.NamespaceID != "pre-cached-ns" {
		t.Errorf("NamespaceID = %q, want pre-cached-ns", auth.NamespaceID)
	}
	token, err := auth.TokenSource.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error: %v", err)
	}
	if token != "static-machine-token" {
		t.Errorf("token = %q, want static-machine-token", token)
	}
}
