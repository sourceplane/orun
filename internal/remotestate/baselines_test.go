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

// ── Publish (orun-bootstrap-engine BE-O7b) ─────────────────────────────────

func TestPublishBaselineAsksTheWorkspaceScopedDoor(t *testing.T) {
	var (
		method string
		path   string
		body   map[string]any
	)
	c := baselineClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"baseline":     map[string]any{"id": "zephyr", "tag": "baseline-v2", "sourceRepo": "acme/zephyr"},
			"publishedTag": "baseline-v2",
		})
	})
	res, err := c.PublishBaseline(context.Background(), "ws_1", "zephyr", "baseline-v2")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if method != http.MethodPost {
		t.Errorf("publish used %s", method)
	}
	if path != "/v1/organizations/ws_1/baselines/zephyr/publish" {
		t.Errorf("wrong door: %q", path)
	}
	// The TAG is the request. A body that omitted it would have the door
	// refuse every publish for a missing field, which reads as a platform
	// fault rather than a client one.
	if body["tag"] != "baseline-v2" {
		t.Errorf("the tag did not reach the door: %+v", body)
	}
	if res.PublishedTag != "baseline-v2" || res.Baseline.ID != "zephyr" {
		t.Errorf("unexpected result: %+v", res)
	}
}

// The door's refusal names the file, the line and what to do about it. A
// client that summarised it would throw away the only part worth reading.
func TestPublishBaselineCarriesTheDoorsRefusalVerbatim(t *testing.T) {
	const said = "zephyr cannot be published at main: main is not a tag in acme/zephyr — cut a tag and publish that."
	c := baselineClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "validation_failed", "message": said},
		})
	})
	_, err := c.PublishBaseline(context.Background(), "ws_1", "zephyr", "main")
	if err == nil {
		t.Fatal("a refused publish reported success")
	}
	if !strings.Contains(err.Error(), "cut a tag") {
		t.Errorf("the refusal was lost: %v", err)
	}
}

// NOT RETRYABLE. Publishing is a write, and a blind retry of one that timed
// out after the registry moved would be a second publish of a tag the caller
// has already been told about — or, worse, a retry of a REFUSED one racing an
// operator who is mid-fix.
func TestPublishBaselineDoesNotRetry(t *testing.T) {
	var calls int
	c := baselineClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "internal_error", "message": "boom"},
		})
	})
	if _, err := c.PublishBaseline(context.Background(), "ws_1", "zephyr", "baseline-v2"); err == nil {
		t.Fatal("expected an error")
	}
	if calls != 1 {
		t.Errorf("publish was attempted %d times; a write must not be retried blindly", calls)
	}
}

func TestRegisterBaselineAsksTheAccountScopedDoor(t *testing.T) {
	var (
		method string
		path   string
		body   map[string]any
	)
	c := baselineClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"baseline": map[string]any{"id": "zephyr", "visibility": "private", "sourceRepo": "acme/zephyr", "tag": "baseline-v1"},
		})
	})
	brief := "flows/agent/BASELINE-TASK.md"
	umbrella := "flows/phases/00-all/workflow.yaml"
	res, err := c.RegisterBaseline(context.Background(), "ws_1", RegisterBaselineRequest{
		ID: "zephyr", Name: "Zephyr", SourceRepo: "acme/zephyr", Tag: "baseline-v1",
		ExpectedMinutes: 30, BriefPath: &brief, UmbrellaPath: &umbrella,
		ManifestPath: "blueprints/coolify.yaml",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if method != http.MethodPost || path != "/v1/organizations/ws_1/baselines" {
		t.Errorf("wrong door: %s %s", method, path)
	}
	// The contract path had no field at all until orun-cloud BE-K4c, so a row
	// registered from here could only ever hold the column's default.
	if body["manifestPath"] != "blueprints/coolify.yaml" {
		t.Errorf("the contract path did not reach the door: %+v", body)
	}
	if body["briefPath"] != brief || body["umbrellaPath"] != umbrella {
		t.Errorf("the shell layer did not reach the door: %+v", body)
	}
	if res.Baseline.ID != "zephyr" {
		t.Errorf("unexpected row: %+v", res.Baseline)
	}
}

// A BLUEPRINT-DRIVEN baseline sends NEITHER shell path. Sending two empty
// strings would be a different statement to a door that distinguishes "not
// declared" from "declared empty" — and omitting the keys is what makes the
// row the shape this epic made canonical.
func TestRegisterBaselineOmitsTheShellLayerWhenThereIsNone(t *testing.T) {
	var body map[string]any
	c := baselineClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"baseline": map[string]any{"id": "zephyr"}})
	})
	if _, err := c.RegisterBaseline(context.Background(), "ws_1", RegisterBaselineRequest{
		ID: "zephyr", Name: "Zephyr", SourceRepo: "acme/zephyr", Tag: "baseline-v1", ExpectedMinutes: 30,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, ok := body["briefPath"]; ok {
		t.Errorf("sent a briefPath nobody declared: %+v", body)
	}
	if _, ok := body["umbrellaPath"]; ok {
		t.Errorf("sent an umbrellaPath nobody declared: %+v", body)
	}
}

// The door answers a taken id with a 409 CARRYING the existing row — the
// allocator posture. A blind retry would report that as a failure of the thing
// that in fact succeeded.
func TestRegisterBaselineDoesNotRetry(t *testing.T) {
	var calls int
	c := baselineClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "internal_error", "message": "boom"},
		})
	})
	if _, err := c.RegisterBaseline(context.Background(), "ws_1", RegisterBaselineRequest{ID: "zephyr"}); err == nil {
		t.Fatal("expected an error")
	}
	if calls != 1 {
		t.Errorf("register was attempted %d times; a write must not be retried blindly", calls)
	}
}
