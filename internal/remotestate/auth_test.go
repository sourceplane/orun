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

func TestOIDCExchangeSource_Success(t *testing.T) {
	var oidcHits, exchangeHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token": // GitHub Actions OIDC endpoint
			oidcHits++
			if r.URL.Query().Get("audience") != "orun-cloud" {
				http.Error(w, "wrong audience", 400)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"value": "gh-oidc-jwt"})
		case "/v1/auth/oidc/exchange": // platform OV3 exchange
			exchangeHits++
			var body struct{ Token, Org string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Token != "gh-oidc-jwt" {
				http.Error(w, "wrong token", 400)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{
				"accessToken": "sk_workflow_token",
				"tokenType":   "Bearer",
				"expiresAt":   "2099-01-01T00:00:00Z",
				"orgId":       body.Org,
				"projectId":   "prj_resolved",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", srv.URL+"/token")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "req-token")

	src := remotestate.NewOIDCExchangeSource("", srv.URL, "org_claimed")

	token, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token != "sk_workflow_token" {
		t.Fatalf("token = %q, want the minted workflow token", token)
	}
	org, project := src.ResolvedScope()
	if org != "org_claimed" || project != "prj_resolved" {
		t.Fatalf("resolved scope = (%q, %q)", org, project)
	}

	// A second call is served from cache — no second OIDC fetch / exchange.
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("second Token: %v", err)
	}
	if oidcHits != 1 || exchangeHits != 1 {
		t.Fatalf("expected exactly one OIDC fetch + exchange, got %d/%d", oidcHits, exchangeHits)
	}
}

func TestOIDCExchangeSource_Denied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_ = json.NewEncoder(w).Encode(map[string]string{"value": "gh-oidc-jwt"})
			return
		}
		w.WriteHeader(403)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{
			"code": "forbidden", "message": "Repository is not linked to an Orun org",
		}})
	}))
	defer srv.Close()

	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", srv.URL+"/token")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "req-token")

	src := remotestate.NewOIDCExchangeSource("", srv.URL, "")
	if _, err := src.Token(context.Background()); err == nil {
		t.Fatal("expected exchange rejection to surface as an error")
	}
}

func TestOIDCSource_NoBackendReturnsRawToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"value": "raw-gh-jwt"})
	}))
	defer srv.Close()
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", srv.URL+"/token")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "req-token")

	// Default audience is the frozen orun-cloud identifier.
	src := remotestate.NewOIDCTokenSource("")
	if got := src.Audience; got != remotestate.DefaultOIDCAudience {
		t.Fatalf("default audience = %q, want %q", got, remotestate.DefaultOIDCAudience)
	}
	token, err := src.Token(context.Background())
	if err != nil || token != "raw-gh-jwt" {
		t.Fatalf("no-backend Token = %q, %v (want raw GitHub token)", token, err)
	}
}
