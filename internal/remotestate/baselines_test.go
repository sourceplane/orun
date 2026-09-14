package remotestate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func baselineClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClientWithScope(srv.URL, "test", nil, Scope{OrgID: "ws_1"})
}

// The public catalogue needs no session — that is what "public" means, and
// requiring a login to read it would make the signed-out catalogue a lie.
func TestListPublicBaselinesNeedsNoSession(t *testing.T) {
	var sawAuth bool
	c := baselineClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			sawAuth = true
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"baselines": []map[string]any{
			{"id": "cirrus", "tag": "baseline-v5", "tier": "free"},
		}})
	})
	rows, err := c.ListPublicBaselines(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "cirrus" {
		t.Errorf("unexpected rows: %+v", rows)
	}
	if sawAuth {
		t.Error("the public read must not require a credential")
	}
}

func TestListBaselinesUsesTheWorkspaceScopedRoute(t *testing.T) {
	var path string
	c := baselineClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"baselines": []any{}})
	})
	if _, err := c.ListBaselines(context.Background(), "ws_1"); err != nil {
		t.Fatalf("list: %v", err)
	}
	// The account read, not the public one: a signed-in reader should see
	// their own private baseline at the one moment it matters.
	if !strings.Contains(path, "/organizations/ws_1/baselines") {
		t.Errorf("expected the workspace-scoped route, got %q", path)
	}
}

func TestGetBlueprintCarriesReadiness(t *testing.T) {
	c := baselineClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"blueprint": map[string]any{
			"id": "cirrus", "tag": "baseline-v5",
			"readiness": map[string]any{
				"integrationsReady": false,
				"integrations": []map[string]any{
					{"provider": "github", "connected": true},
					{"provider": "cloudflare", "connected": false},
				},
			},
		}})
	})
	view, err := c.GetBlueprint(context.Background(), "ws_1", "cirrus")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.Blueprint.Readiness.IntegrationsReady {
		t.Error("readiness should be false when a provider is unconnected")
	}
	if len(view.Blueprint.Readiness.Integrations) != 2 {
		t.Errorf("expected both providers, got %+v", view.Blueprint.Readiness.Integrations)
	}
}

// A stale pin RESOLVES and is reported. The visitor clicking a link from a blog
// post wants to build the platform, not to litigate a version.
func TestGetBlueprintReportsAStalePinWithoutRefusing(t *testing.T) {
	c := baselineClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"blueprint":      map[string]any{"id": "cirrus", "tag": "baseline-v5"},
			"pinnedTag":      "baseline-v2",
			"pinnedTagStale": true,
		})
	})
	view, err := c.GetBlueprint(context.Background(), "ws_1", "cirrus@baseline-v2")
	if err != nil {
		t.Fatalf("a stale pin must resolve, not refuse: %v", err)
	}
	if !view.PinnedTagStale || view.PinnedTag != "baseline-v2" {
		t.Errorf("the stale pin should be reported, got %+v", view)
	}
	if view.Blueprint.Tag != "baseline-v5" {
		t.Errorf("the build uses what the registry publishes, got %q", view.Blueprint.Tag)
	}
}

func TestBootstrapPostsTheRepoLink(t *testing.T) {
	var body map[string]any
	var method, path string
	c := baselineClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{"sessionId": "ses_1"})
	})
	out, err := c.Bootstrap(context.Background(), "ws_1", "cirrus",
		BootstrapRequest{RepoLinkID: "lnk_1", Inputs: map[string]string{"productname": "Acme"}})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if method != http.MethodPost || !strings.HasSuffix(path, "/bootstrap") {
		t.Errorf("expected a POST to the bootstrap door, got %s %s", method, path)
	}
	if body["repoLinkId"] != "lnk_1" {
		t.Errorf("the repo link must be sent, got %v", body)
	}
	if out.SessionID != "ses_1" {
		t.Errorf("expected the session id back, got %+v", out)
	}
}

func TestRetiredIsReadFromTheRow(t *testing.T) {
	if (Baseline{}).Retired() {
		t.Error("a row with no retiredAt is not retired")
	}
	if !(Baseline{RetiredAt: "2026-01-01T00:00:00Z"}).Retired() {
		t.Error("a row with retiredAt is retired")
	}
}
