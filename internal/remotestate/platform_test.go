package remotestate_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

// TestPlatformReadPaths pins the public-API route shapes (orun-mcp UM1): the
// org comes from the call, never the client's bound scope, and the success
// envelope's data AND meta both survive the decode.
func TestPlatformReadPaths(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"items":[1]},"meta":{"cursor":"next|1"}}`))
	}))
	defer srv.Close()
	// Scope is deliberately bound to another org: platform reads must ignore it.
	c := remotestate.NewClientWithScope(srv.URL, "test", remotestate.NewStaticTokenSource("tok"), remotestate.Scope{OrgID: "org_other"})
	ctx := context.Background()

	calls := []struct {
		name string
		do   func() (*remotestate.PlatformPage, error)
		want string
	}{
		{"GetAuthProfile", func() (*remotestate.PlatformPage, error) { return c.GetAuthProfile(ctx) }, "/v1/auth/profile"},
		{"ListOrganizations", func() (*remotestate.PlatformPage, error) { return c.ListOrganizations(ctx) }, "/v1/organizations"},
		{"ListProjects", func() (*remotestate.PlatformPage, error) { return c.ListProjects(ctx, "ws_1", "prj_a") },
			"/v1/organizations/ws_1/projects?project=prj_a"},
		{"ListProjectEnvironments", func() (*remotestate.PlatformPage, error) { return c.ListProjectEnvironments(ctx, "ws_1", "prj_a") },
			"/v1/organizations/ws_1/projects/prj_a/environments"},
		{"ListCatalogEntities", func() (*remotestate.PlatformPage, error) {
			return c.ListCatalogEntities(ctx, "ws_1", remotestate.CatalogEntitiesQuery{Kind: "Component", Q: "api", Cursor: "c0", Limit: 5})
		}, "/v1/organizations/ws_1/catalog/entities?cursor=c0&kind=Component&limit=5&q=api"},
		{"ListCatalogDocs", func() (*remotestate.PlatformPage, error) {
			return c.ListCatalogDocs(ctx, "ws_1", remotestate.CatalogDocsQuery{Role: "runbook"})
		}, "/v1/organizations/ws_1/catalog/docs?role=runbook"},
		{"ListOrgRuns", func() (*remotestate.PlatformPage, error) {
			return c.ListOrgRuns(ctx, "ws_1", remotestate.OrgRunsQuery{Status: "failed", Branch: "main", Source: "ci"})
		}, "/v1/organizations/ws_1/state/runs?branch=main&source=ci&status=failed"},
		{"ListProjectRuns", func() (*remotestate.PlatformPage, error) {
			return c.ListProjectRuns(ctx, "ws_1", "prj_a", remotestate.ProjectRunsQuery{Cursor: "c1"})
		}, "/v1/organizations/ws_1/projects/prj_a/state/runs?cursor=c1"},
		{"GetPlatformRun", func() (*remotestate.PlatformPage, error) { return c.GetPlatformRun(ctx, "ws_1", "prj_a", "r1") },
			"/v1/organizations/ws_1/projects/prj_a/state/runs/r1"},
		{"ListPlatformRunJobs", func() (*remotestate.PlatformPage, error) { return c.ListPlatformRunJobs(ctx, "ws_1", "prj_a", "r1") },
			"/v1/organizations/ws_1/projects/prj_a/state/runs/r1/jobs"},
		{"ReadPlatformJobLogs", func() (*remotestate.PlatformPage, error) {
			return c.ReadPlatformJobLogs(ctx, "ws_1", "prj_a", "r1", "j1", 7)
		}, "/v1/organizations/ws_1/projects/prj_a/state/runs/r1/logs/j1?fromSeq=7"},
		{"ListAuditEntries", func() (*remotestate.PlatformPage, error) {
			return c.ListAuditEntries(ctx, "ws_1", remotestate.AuditQuery{ActorID: "usr_9", From: "2026-01-01T00:00:00Z"})
		}, "/v1/organizations/ws_1/audit?actorId=usr_9&from=2026-01-01T00%3A00%3A00Z"},
		{"ListPlatformEvents", func() (*remotestate.PlatformPage, error) {
			return c.ListPlatformEvents(ctx, "ws_1", remotestate.PlatformEventsQuery{Type: "run.*"})
		}, "/v1/organizations/ws_1/events?type=run.%2A"},
		{"GetPlatformEvent", func() (*remotestate.PlatformPage, error) { return c.GetPlatformEvent(ctx, "ws_1", "evt_1") },
			"/v1/organizations/ws_1/events/evt_1"},
		{"ListSecurityEvents", func() (*remotestate.PlatformPage, error) {
			return c.ListSecurityEvents(ctx, remotestate.PageQuery{Limit: 10})
		}, "/v1/auth/security-events?limit=10"},
		{"GetEffectiveAccess", func() (*remotestate.PlatformPage, error) { return c.GetEffectiveAccess(ctx, "ws_1", "usr_2", "prj_a") },
			"/v1/organizations/ws_1/effective-access?project=prj_a&subjectId=usr_2"},
		{"ListOrgMembers", func() (*remotestate.PlatformPage, error) { return c.ListOrgMembers(ctx, "ws_1") },
			"/v1/organizations/ws_1/members"},
		{"ListOrgTeams", func() (*remotestate.PlatformPage, error) { return c.ListOrgTeams(ctx, "ws_1") },
			"/v1/organizations/ws_1/teams"},
		{"GetUsageSummary", func() (*remotestate.PlatformPage, error) {
			return c.GetUsageSummary(ctx, "ws_1", remotestate.UsageQuery{Metric: "runs", BucketType: "day"})
		}, "/v1/organizations/ws_1/usage/summary?bucketType=day&metric=runs"},
		{"CheckQuota", func() (*remotestate.PlatformPage, error) {
			return c.CheckQuota(ctx, "ws_1", remotestate.QuotaQuery{Metric: "runs"})
		}, "/v1/organizations/ws_1/quotas/check?metric=runs"},
		{"GetBillingSummary", func() (*remotestate.PlatformPage, error) { return c.GetBillingSummary(ctx, "ws_1") },
			"/v1/organizations/ws_1/billing/summary"},
		{"ListEntitlements", func() (*remotestate.PlatformPage, error) { return c.ListEntitlements(ctx, "ws_1") },
			"/v1/organizations/ws_1/billing/entitlements"},
		{"GetConfigSettings/org", func() (*remotestate.PlatformPage, error) {
			return c.GetConfigSettings(ctx, remotestate.ConfigScope{Org: "ws_1"})
		}, "/v1/organizations/ws_1/config/settings"},
		{"ListFeatureFlags/env", func() (*remotestate.PlatformPage, error) {
			return c.ListFeatureFlags(ctx, remotestate.ConfigScope{Org: "ws_1", Project: "prj_a", Environment: "env_x"})
		}, "/v1/organizations/ws_1/projects/prj_a/environments/env_x/config/feature-flags"},
		{"ListSecretsMetadata/project", func() (*remotestate.PlatformPage, error) {
			return c.ListSecretsMetadata(ctx, remotestate.ConfigScope{Org: "ws_1", Project: "prj_a"})
		}, "/v1/organizations/ws_1/projects/prj_a/config/secrets"},
		{"ListWebhookEndpoints", func() (*remotestate.PlatformPage, error) {
			return c.ListWebhookEndpoints(ctx, "ws_1", remotestate.PageQuery{})
		}, "/v1/organizations/ws_1/webhooks/endpoints"},
		{"ListWebhookDeliveries", func() (*remotestate.PlatformPage, error) {
			return c.ListWebhookDeliveries(ctx, "ws_1", "whep_1", remotestate.PageQuery{Cursor: "c2"})
		}, "/v1/organizations/ws_1/webhooks/endpoints/whep_1/delivery-attempts?cursor=c2"},
	}
	for _, tc := range calls {
		page, err := tc.do()
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if gotPath != tc.want {
			t.Errorf("%s: path = %s, want %s", tc.name, gotPath, tc.want)
		}
		if gotAuth != "Bearer tok" {
			t.Errorf("%s: auth = %q", tc.name, gotAuth)
		}
		if string(page.Data) != `{"items":[1]}` {
			t.Errorf("%s: data = %s", tc.name, page.Data)
		}
		if page.Cursor() != "next|1" {
			t.Errorf("%s: cursor = %q, want next|1 (meta must survive the decode)", tc.name, page.Cursor())
		}
	}
}

// TestPlatformReadBytesAndErrors: doc reads return the raw body, and the
// platform error envelope decodes with its code preserved.
func TestPlatformReadBytesAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/organizations/ws_1/catalog/doc" &&
			r.URL.Query().Get("digest") == "sha256:abc" {
			_, _ = w.Write([]byte("# Runbook\n\nbody"))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"missing member role","requestId":"req_7"}}`))
	}))
	defer srv.Close()
	c := remotestate.NewClient(srv.URL, "test", remotestate.NewStaticTokenSource("tok"))
	ctx := context.Background()

	body, err := c.ReadCatalogDoc(ctx, "ws_1", "sha256:abc")
	if err != nil {
		t.Fatalf("ReadCatalogDoc: %v", err)
	}
	if string(body) != "# Runbook\n\nbody" {
		t.Fatalf("doc body = %q", body)
	}

	_, err = c.ListAuditEntries(ctx, "ws_1", remotestate.AuditQuery{})
	var apiErr *remotestate.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %v", err)
	}
	if apiErr.Code != "forbidden" || apiErr.Message != "missing member role" || apiErr.RequestID != "req_7" {
		t.Fatalf("decoded error = %+v", apiErr)
	}
}
