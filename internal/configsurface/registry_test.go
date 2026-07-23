package configsurface

// orun-integrations-cli ICL0: the Integration Registry read and the ICL2 wire
// methods. The fixture bodies pin the IR0 contract shape (orun-cloud
// saas-integration-registry design §9) — the server side may land later;
// these fixtures ARE the contract. Everything on this surface is metadata:
// no fixture and no assertion ever involves a secret value.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// registryFixture is the recorded IR0-contract registry body for a
// representative org: three live providers, one live serving explicit CLI
// verbs, and a dormant one.
const registryFixture = `{
  "data": {
    "registry": [
      {
        "id": "cloudflare",
        "displayName": "Cloudflare",
        "category": "infrastructure",
        "tagline": "Workers, Pages, DNS",
        "connect": [
          {
            "kind": "token",
            "live": true,
            "recipe": {
              "intro": "Create an API token scoped for Orun:",
              "items": [
                {"name": "Account.Workers Scripts:Edit", "why": "deploy Workers"},
                {"name": "Zone.DNS:Edit", "why": "manage DNS records"}
              ],
              "links": [
                {"label": "Create token", "url": "https://dash.example.test/profile/api-tokens"}
              ]
            }
          }
        ],
        "multiConnection": true,
        "capabilities": ["secrets", "credential-broker"],
        "connectedCount": 2,
        "space": {"tabs": ["overview", "connections"], "modules": ["secrets"], "authoring": "custom"},
        "cli": {
          "verbs": [
            {
              "path": ["connections", "list"],
              "summary": "List Cloudflare connections (served)",
              "args": [],
              "invoke": {"plane": "integrations", "op": "integrations.listConnections", "bind": {}}
            },
            {
              "path": ["dns", "audit"],
              "summary": "Audit DNS posture for a zone",
              "args": [
                {"name": "zone", "kind": "flag", "type": "string", "required": true, "help": "Zone id"}
              ],
              "invoke": {"plane": "integrations", "op": "integrations.dnsAudit.v2", "bind": {"zone": "zoneId"}}
            }
          ]
        },
        "entitlement": "pro",
        "version": 3,
        "status": "live",
        "entitled": true
      },
      {
        "id": "github",
        "displayName": "GitHub",
        "category": "scm",
        "capabilities": ["credential-broker"],
        "connectedCount": 1,
        "status": "live"
      },
      {
        "id": "supabase",
        "displayName": "Supabase",
        "category": "database",
        "capabilities": ["secrets", "provision"],
        "status": "live"
      },
      {
        "id": "aws",
        "displayName": "Amazon Web Services",
        "category": "infrastructure",
        "capabilities": [],
        "status": "dormant"
      }
    ]
  }
}`

func TestGetIntegrationRegistryDecodesFixture(t *testing.T) {
	var gotPath, gotAuth string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("ETag", `"v7"`)
		_, _ = io.WriteString(w, registryFixture)
	})
	registry, err := client.GetIntegrationRegistry(context.Background(), "org_1")
	if err != nil {
		t.Fatalf("GetIntegrationRegistry: %v", err)
	}
	if gotPath != "/v1/organizations/org_1/integrations/registry" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer tok_test" {
		t.Errorf("auth = %q", gotAuth)
	}
	if len(registry) != 4 {
		t.Fatalf("len(registry) = %d", len(registry))
	}
	cf := registry[0]
	if cf.Provider != "cloudflare" {
		t.Errorf("Provider must come from the wire `id`, got %q", cf.Provider)
	}
	if cf.DisplayName != "Cloudflare" || cf.Category != "infrastructure" || cf.Tagline != "Workers, Pages, DNS" {
		t.Errorf("descriptor identity = %+v", cf)
	}
	if !cf.MultiConnection || cf.Connected != 2 || cf.Entitlement != "pro" || cf.Version != 3 {
		t.Errorf("descriptor projection = %+v", cf)
	}
	if cf.Entitled == nil || !*cf.Entitled {
		t.Errorf("entitled = %v", cf.Entitled)
	}
	if !cf.Live() {
		t.Error("status live must report Live()")
	}
	if len(cf.Capabilities) != 2 || cf.Capabilities[0] != "secrets" {
		t.Errorf("capabilities = %v", cf.Capabilities)
	}
	if cf.Space == nil || cf.Space.Authoring != "custom" || len(cf.Space.Tabs) != 2 {
		t.Errorf("space = %+v", cf.Space)
	}
	recipe := cf.ConnectRecipe()
	if recipe == nil || recipe.Intro == "" || len(recipe.Items) != 2 || len(recipe.Links) != 1 {
		t.Fatalf("connect recipe = %+v", recipe)
	}
	if recipe.Items[0].Name != "Account.Workers Scripts:Edit" || recipe.Items[0].Why != "deploy Workers" {
		t.Errorf("recipe item = %+v", recipe.Items[0])
	}
	if cf.CLI == nil || len(cf.CLI.Verbs) != 2 {
		t.Fatalf("cli namespace = %+v", cf.CLI)
	}
	served := cf.CLI.Verbs[0]
	if strings.Join(served.Path, " ") != "connections list" || served.Invoke.Op != "integrations.listConnections" {
		t.Errorf("served verb = %+v", served)
	}
	audit := cf.CLI.Verbs[1]
	if audit.Invoke.Plane != "integrations" || audit.Invoke.Bind["zone"] != "zoneId" {
		t.Errorf("audit invoke = %+v", audit.Invoke)
	}
	if len(audit.Args) != 1 || audit.Args[0].Kind != "flag" || !audit.Args[0].Required {
		t.Errorf("audit args = %+v", audit.Args)
	}
	aws := registry[3]
	if aws.Provider != "aws" || aws.Live() || aws.Status != "dormant" {
		t.Errorf("dormant descriptor = %+v", aws)
	}
	// github serves no cli block; supabase carries provision.
	if registry[1].CLI != nil {
		t.Errorf("github cli = %+v", registry[1].CLI)
	}
	if got := registry[2].Capabilities; len(got) != 2 || got[1] != "provision" {
		t.Errorf("supabase capabilities = %v", got)
	}
}

func TestGetIntegrationRegistryETagRoundTrip(t *testing.T) {
	var gotIfNoneMatch []string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		inm := r.Header.Get("If-None-Match")
		gotIfNoneMatch = append(gotIfNoneMatch, inm)
		if inm == `"v7"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v7"`)
		_, _ = io.WriteString(w, registryFixture)
	})
	// First read: no etag → full body + the server's tag.
	res, err := client.GetIntegrationRegistryConditional(context.Background(), "org_1", "")
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if res.NotModified || res.ETag != `"v7"` || len(res.Registry) != 4 {
		t.Fatalf("first read result = %+v", res)
	}
	// Second read with the cached etag → 304, cache-valid.
	res, err = client.GetIntegrationRegistryConditional(context.Background(), "org_1", `"v7"`)
	if err != nil {
		t.Fatalf("conditional read: %v", err)
	}
	if !res.NotModified || res.ETag != `"v7"` || len(res.Registry) != 0 {
		t.Fatalf("conditional result = %+v", res)
	}
	if len(gotIfNoneMatch) != 2 || gotIfNoneMatch[0] != "" || gotIfNoneMatch[1] != `"v7"` {
		t.Errorf("If-None-Match sequence = %v", gotIfNoneMatch)
	}
}

func TestGetIntegrationRegistryErrors(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "not_found", "message": "Organization not found", "requestId": "req_1"},
		})
	})
	if _, err := client.GetIntegrationRegistry(context.Background(), " "); err == nil ||
		!strings.Contains(err.Error(), "organization") {
		t.Errorf("expected a missing-organization error, got: %v", err)
	}
	_, err := client.GetIntegrationRegistry(context.Background(), "org_x")
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || !apiErr.IsNotFound() {
		t.Fatalf("expected typed not-found APIError, got %T: %v", err, err)
	}
}

// A legacy body spelling the id as `provider` still decodes (additive
// tolerance) — Provider prefers `id` when both are present.
func TestIntegrationDescriptorProviderFallback(t *testing.T) {
	var d IntegrationDescriptor
	if err := json.Unmarshal([]byte(`{"provider":"slack","displayName":"Slack","status":"live"}`), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Provider != "slack" {
		t.Errorf("fallback provider = %q", d.Provider)
	}
	if err := json.Unmarshal([]byte(`{"id":"slack","provider":"legacy","status":"live"}`), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Provider != "slack" {
		t.Errorf("id must win over provider, got %q", d.Provider)
	}
}

// ── ICL2 wire methods ────────────────────────────────────────────────────────

func TestListIntegrationConnections(t *testing.T) {
	var gotPath, gotQuery string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"connections": []map[string]any{
			{"id": "int_" + strings.Repeat("ab", 16), "provider": "cloudflare", "displayName": "prod account",
				"status": "active", "authKind": "token", "createdAt": "2026-07-01T00:00:00Z", "lastUsedAt": "2026-07-20T00:00:00Z"},
			{"id": "int_" + strings.Repeat("cd", 16), "provider": "cloudflare", "status": "error", "health": "broken"},
		}}})
	})
	conns, err := client.ListIntegrationConnections(context.Background(), "org_1", "cloudflare")
	if err != nil {
		t.Fatalf("ListIntegrationConnections: %v", err)
	}
	if gotPath != "/v1/organizations/org_1/integrations" {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery != "provider=cloudflare" {
		t.Errorf("query = %q", gotQuery)
	}
	if len(conns) != 2 || conns[0].DisplayName != "prod account" || conns[1].Health != "broken" {
		t.Errorf("connections = %+v", conns)
	}
	if _, err := client.ListIntegrationConnections(context.Background(), " ", "x"); err == nil {
		t.Error("blank org must fail locally")
	}
}

func TestGetAndRevokeIntegrationConnection(t *testing.T) {
	connID := "int_" + strings.Repeat("ab", 16)
	var gotMethod, gotPath string
	var gotBody []byte
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"connection": map[string]any{
			"id": connID, "provider": "cloudflare", "status": "active",
		}}})
	})
	conn, err := client.GetIntegrationConnection(context.Background(), "org_1", connID)
	if err != nil {
		t.Fatalf("GetIntegrationConnection: %v", err)
	}
	if gotPath != "/v1/organizations/org_1/integrations/"+connID || conn.ID != connID {
		t.Errorf("path = %q, conn = %+v", gotPath, conn)
	}
	if err := client.RevokeIntegrationConnection(context.Background(), "org_1", connID); err != nil {
		t.Fatalf("RevokeIntegrationConnection: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/v1/organizations/org_1/integrations/"+connID {
		t.Errorf("revoke = %s %s", gotMethod, gotPath)
	}
	// Value-free structurally: a revoke sends no body at all.
	if len(gotBody) != 0 {
		t.Errorf("revoke body must be empty, got %q", gotBody)
	}
}

func TestListProviderScopeTemplates(t *testing.T) {
	var gotPath string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"templates": []map[string]any{
			{"id": "workers-deploy", "provider": "cloudflare", "version": 1, "displayName": "Workers deploy",
				"params": []string{}, "maxTtlSeconds": 3600},
		}}})
	})
	templates, err := client.ListProviderScopeTemplates(context.Background(), "org_1", "cloudflare")
	if err != nil {
		t.Fatalf("ListProviderScopeTemplates: %v", err)
	}
	if gotPath != "/v1/organizations/org_1/integrations/providers/cloudflare/scope-templates" {
		t.Errorf("path = %q", gotPath)
	}
	if len(templates) != 1 || templates[0].ID != "workers-deploy" || templates[0].MaxTTLSeconds != 3600 {
		t.Errorf("templates = %+v", templates)
	}
}

func TestListAndRevokeMintedCredentials(t *testing.T) {
	connID := "int_" + strings.Repeat("ab", 16)
	var gotMethod, gotPath string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"credentials": []map[string]any{
			{"id": "crd_1", "connectionId": connID, "template": "workers-deploy", "status": "active",
				"mintedAt": "2026-07-22T00:00:00Z", "expiresAt": "2026-07-23T00:00:00Z"},
		}}})
	})
	creds, err := client.ListMintedCredentials(context.Background(), "org_1", connID)
	if err != nil {
		t.Fatalf("ListMintedCredentials: %v", err)
	}
	if gotPath != "/v1/organizations/org_1/integrations/"+connID+"/credentials" {
		t.Errorf("path = %q", gotPath)
	}
	if len(creds) != 1 || creds[0].Template != "workers-deploy" {
		t.Errorf("credentials = %+v", creds)
	}
	if err := client.RevokeMintedCredential(context.Background(), "org_1", connID, "crd_1"); err != nil {
		t.Fatalf("RevokeMintedCredential: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/v1/organizations/org_1/integrations/"+connID+"/credentials/crd_1" {
		t.Errorf("revoke = %s %s", gotMethod, gotPath)
	}
}

func TestListProviderSandboxes(t *testing.T) {
	var gotPath string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"sandboxes": []map[string]any{
			{"id": "sbx_1", "name": "preview-42", "status": "running", "createdAt": "2026-07-21T00:00:00Z"},
		}}})
	})
	boxes, err := client.ListProviderSandboxes(context.Background(), "org_1", "daytona")
	if err != nil {
		t.Fatalf("ListProviderSandboxes: %v", err)
	}
	if gotPath != "/v1/organizations/org_1/integrations/providers/daytona/sandboxes" {
		t.Errorf("path = %q", gotPath)
	}
	if len(boxes) != 1 || boxes[0].Name != "preview-42" || boxes[0].Status != "running" {
		t.Errorf("sandboxes = %+v", boxes)
	}
}
