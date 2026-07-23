package configsurface

// saas-secrets-platform SP5: the org-scoped bulk capability read the CLI's
// integration-namespaced authoring derives its validation/help from (SP-A1/
// SP-A7). Pins the path, auth, envelope unwrapping, and field decoding.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestListSecretsCapabilitiesDecodesEnvelope(t *testing.T) {
	var gotPath, gotAuth string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"capabilities": []map[string]any{
				{
					"provider": "cloudflare",
					"scopeTemplates": []map[string]any{
						{
							"id": "workers-deploy", "provider": "cloudflare", "version": 1,
							"displayName": "Workers deploy", "description": "Deploy Workers",
							"params": []string{}, "maxTtlSeconds": 3600,
						},
						{
							"id": "zone-dns", "provider": "cloudflare", "version": 3,
							"displayName": "Zone DNS", "description": "Edit zone DNS",
							"params": []string{"zoneIds"}, "maxTtlSeconds": 1800,
							"origin": "custom", "baseTemplate": "zone-dns-base", "status": "retired",
						},
					},
					"supportedModes":  []string{"brokered", "rotated"},
					"deliveryTargets": []string{"cloudflare-worker"},
					"authoring":       "custom",
				},
				{
					"provider":        "supabase",
					"scopeTemplates":  []map[string]any{},
					"supportedModes":  []string{"brokered"},
					"deliveryTargets": []string{},
					"authoring":       "declarative",
				},
			},
		}})
	})
	caps, err := client.ListSecretsCapabilities(context.Background(), "org_1")
	if err != nil {
		t.Fatalf("ListSecretsCapabilities: %v", err)
	}
	if gotPath != "/v1/organizations/org_1/integrations/secrets-capabilities" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer tok_test" {
		t.Errorf("auth = %q", gotAuth)
	}
	if len(caps) != 2 {
		t.Fatalf("len(caps) = %d", len(caps))
	}
	cf := caps[0]
	if cf.Provider != "cloudflare" || cf.Authoring != "custom" {
		t.Errorf("cloudflare capability = %+v", cf)
	}
	if len(cf.SupportedModes) != 2 || cf.SupportedModes[1] != "rotated" {
		t.Errorf("supportedModes = %v", cf.SupportedModes)
	}
	if len(cf.DeliveryTargets) != 1 || cf.DeliveryTargets[0] != "cloudflare-worker" {
		t.Errorf("deliveryTargets = %v", cf.DeliveryTargets)
	}
	if len(cf.ScopeTemplates) != 2 {
		t.Fatalf("scopeTemplates = %v", cf.ScopeTemplates)
	}
	wd := cf.ScopeTemplates[0]
	if wd.ID != "workers-deploy" || wd.Version != 1 || wd.MaxTTLSeconds != 3600 || !wd.Active() {
		t.Errorf("workers-deploy template = %+v", wd)
	}
	dns := cf.ScopeTemplates[1]
	if dns.Status != "retired" || dns.Active() || dns.Origin != "custom" || dns.BaseTemplate != "zone-dns-base" {
		t.Errorf("zone-dns template = %+v", dns)
	}
	if len(dns.Params) != 1 || dns.Params[0] != "zoneIds" {
		t.Errorf("zone-dns params = %v", dns.Params)
	}
	if caps[1].Provider != "supabase" || len(caps[1].SupportedModes) != 1 {
		t.Errorf("supabase capability = %+v", caps[1])
	}
}

func TestListSecretsCapabilitiesErrors(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "not_found", "message": "Organization not found", "requestId": "req_9"},
		})
	})
	// Missing org fails locally, before any request.
	if _, err := client.ListSecretsCapabilities(context.Background(), " "); err == nil ||
		!strings.Contains(err.Error(), "organization") {
		t.Errorf("expected a missing-organization error, got: %v", err)
	}
	// A server error surfaces as the typed APIError.
	_, err := client.ListSecretsCapabilities(context.Background(), "org_x")
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	ok := false
	for e := err; e != nil; {
		if a, is := e.(*APIError); is {
			apiErr, ok = a, true
			break
		}
		u, is := e.(interface{ Unwrap() error })
		if !is {
			break
		}
		e = u.Unwrap()
	}
	if !ok || !apiErr.IsNotFound() {
		t.Fatalf("expected typed not-found APIError, got %T: %v", err, err)
	}
}
