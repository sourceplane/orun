package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/mcpserve"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/workmcp"
)

// fakeAPI records every seam call as "Method org=… …" and serves one canned
// page (workmcp's fakeAPI convention).
type fakeAPI struct {
	calls []string
	page  *remotestate.PlatformPage
	// pages, when set, is consumed one page per call (multi-page flows).
	pages []*remotestate.PlatformPage
	doc   []byte
	err   error
}

func page(data, meta string) *remotestate.PlatformPage {
	p := &remotestate.PlatformPage{Data: json.RawMessage(data)}
	if meta != "" {
		p.Meta = json.RawMessage(meta)
	}
	return p
}

func (f *fakeAPI) rec(format string, args ...interface{}) (*remotestate.PlatformPage, error) {
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
	if f.err != nil {
		return nil, f.err
	}
	if len(f.pages) > 0 {
		p := f.pages[0]
		f.pages = f.pages[1:]
		return p, nil
	}
	return f.page, nil
}

func (f *fakeAPI) GetAuthProfile(context.Context) (*remotestate.PlatformPage, error) {
	return f.rec("GetAuthProfile")
}
func (f *fakeAPI) ListOrganizations(context.Context) (*remotestate.PlatformPage, error) {
	return f.rec("ListOrganizations")
}
func (f *fakeAPI) ListProjects(_ context.Context, org, project string) (*remotestate.PlatformPage, error) {
	return f.rec("ListProjects org=%s project=%s", org, project)
}
func (f *fakeAPI) ListProjectEnvironments(_ context.Context, org, project string) (*remotestate.PlatformPage, error) {
	return f.rec("ListProjectEnvironments org=%s project=%s", org, project)
}
func (f *fakeAPI) ListCatalogEntities(_ context.Context, org string, q remotestate.CatalogEntitiesQuery) (*remotestate.PlatformPage, error) {
	return f.rec("ListCatalogEntities org=%s kind=%s q=%s cursor=%s limit=%d", org, q.Kind, q.Q, q.Cursor, q.Limit)
}
func (f *fakeAPI) ListCatalogDocs(_ context.Context, org string, q remotestate.CatalogDocsQuery) (*remotestate.PlatformPage, error) {
	return f.rec("ListCatalogDocs org=%s role=%s", org, q.Role)
}
func (f *fakeAPI) ReadCatalogDoc(_ context.Context, org, digest string) ([]byte, error) {
	f.calls = append(f.calls, fmt.Sprintf("ReadCatalogDoc org=%s digest=%s", org, digest))
	return f.doc, f.err
}
func (f *fakeAPI) ListOrgRuns(_ context.Context, org string, q remotestate.OrgRunsQuery) (*remotestate.PlatformPage, error) {
	return f.rec("ListOrgRuns org=%s status=%s branch=%s", org, q.Status, q.Branch)
}
func (f *fakeAPI) ListProjectRuns(_ context.Context, org, project string, q remotestate.ProjectRunsQuery) (*remotestate.PlatformPage, error) {
	return f.rec("ListProjectRuns org=%s project=%s status=%s", org, project, q.Status)
}
func (f *fakeAPI) GetPlatformRun(_ context.Context, org, project, runID string) (*remotestate.PlatformPage, error) {
	return f.rec("GetPlatformRun org=%s project=%s run=%s", org, project, runID)
}
func (f *fakeAPI) ListPlatformRunJobs(_ context.Context, org, project, runID string) (*remotestate.PlatformPage, error) {
	return f.rec("ListPlatformRunJobs org=%s project=%s run=%s", org, project, runID)
}
func (f *fakeAPI) ReadPlatformJobLogs(_ context.Context, org, project, runID, jobID string, fromSeq int) (*remotestate.PlatformPage, error) {
	return f.rec("ReadPlatformJobLogs org=%s project=%s run=%s job=%s fromSeq=%d", org, project, runID, jobID, fromSeq)
}
func (f *fakeAPI) ListAuditEntries(_ context.Context, org string, q remotestate.AuditQuery) (*remotestate.PlatformPage, error) {
	return f.rec("ListAuditEntries org=%s actor=%s from=%s", org, q.ActorID, q.From)
}
func (f *fakeAPI) ListPlatformEvents(_ context.Context, org string, q remotestate.PlatformEventsQuery) (*remotestate.PlatformPage, error) {
	return f.rec("ListPlatformEvents org=%s type=%s", org, q.Type)
}
func (f *fakeAPI) GetPlatformEvent(_ context.Context, org, eventID string) (*remotestate.PlatformPage, error) {
	return f.rec("GetPlatformEvent org=%s event=%s", org, eventID)
}
func (f *fakeAPI) ListSecurityEvents(_ context.Context, q remotestate.PageQuery) (*remotestate.PlatformPage, error) {
	return f.rec("ListSecurityEvents cursor=%s limit=%d", q.Cursor, q.Limit)
}
func (f *fakeAPI) GetEffectiveAccess(_ context.Context, org, subjectID, project string) (*remotestate.PlatformPage, error) {
	return f.rec("GetEffectiveAccess org=%s subject=%s project=%s", org, subjectID, project)
}
func (f *fakeAPI) ListOrgMembers(_ context.Context, org string) (*remotestate.PlatformPage, error) {
	return f.rec("ListOrgMembers org=%s", org)
}
func (f *fakeAPI) ListOrgTeams(_ context.Context, org string) (*remotestate.PlatformPage, error) {
	return f.rec("ListOrgTeams org=%s", org)
}
func (f *fakeAPI) GetUsageSummary(_ context.Context, org string, q remotestate.UsageQuery) (*remotestate.PlatformPage, error) {
	return f.rec("GetUsageSummary org=%s metric=%s bucket=%s", org, q.Metric, q.BucketType)
}
func (f *fakeAPI) CheckQuota(_ context.Context, org string, q remotestate.QuotaQuery) (*remotestate.PlatformPage, error) {
	return f.rec("CheckQuota org=%s metric=%s", org, q.Metric)
}
func (f *fakeAPI) GetBillingSummary(_ context.Context, org string) (*remotestate.PlatformPage, error) {
	return f.rec("GetBillingSummary org=%s", org)
}
func (f *fakeAPI) ListEntitlements(_ context.Context, org string) (*remotestate.PlatformPage, error) {
	return f.rec("ListEntitlements org=%s", org)
}
func (f *fakeAPI) GetConfigSettings(_ context.Context, s remotestate.ConfigScope) (*remotestate.PlatformPage, error) {
	return f.rec("GetConfigSettings org=%s project=%s env=%s", s.Org, s.Project, s.Environment)
}
func (f *fakeAPI) ListFeatureFlags(_ context.Context, s remotestate.ConfigScope) (*remotestate.PlatformPage, error) {
	return f.rec("ListFeatureFlags org=%s project=%s env=%s", s.Org, s.Project, s.Environment)
}
func (f *fakeAPI) ListSecretsMetadata(_ context.Context, s remotestate.ConfigScope) (*remotestate.PlatformPage, error) {
	return f.rec("ListSecretsMetadata org=%s project=%s env=%s", s.Org, s.Project, s.Environment)
}
func (f *fakeAPI) ListWebhookEndpoints(_ context.Context, org string, q remotestate.PageQuery) (*remotestate.PlatformPage, error) {
	return f.rec("ListWebhookEndpoints org=%s cursor=%s", org, q.Cursor)
}
func (f *fakeAPI) ListWebhookDeliveries(_ context.Context, org, endpoint string, q remotestate.PageQuery) (*remotestate.PlatformPage, error) {
	return f.rec("ListWebhookDeliveries org=%s endpoint=%s cursor=%s", org, endpoint, q.Cursor)
}

func callTool(t *testing.T, p *Provider, name, args string) (string, bool) {
	t.Helper()
	result, owned := p.Call(context.Background(), name, json.RawMessage(args))
	if !owned {
		t.Fatalf("%s not owned by the platform provider", name)
	}
	content := result["content"].([]map[string]interface{})
	text := content[0]["text"].(string)
	isErr, _ := result["isError"].(bool)
	return text, isErr
}

// TestPerToolHappyPath drives every read tool over the fake seam: the right
// method with the right args, and the summary + compact-JSON output shape.
func TestPerToolHappyPath(t *testing.T) {
	cases := []struct {
		tool, args  string
		wantCalls   []string
		summaryFrag string
	}{
		{"whoami", `{}`, []string{"GetAuthProfile", "ListOrganizations"}, "authenticated actor"},
		{"workspaces_list", `{}`, []string{"ListOrganizations"}, "workspaces"},
		{"projects_list", `{"workspace":"ws_1"}`, []string{"ListProjects org=ws_1 project="}, "projects in ws_1"},
		{"projects_list", `{"workspace":"ws_1","project":"prj_a"}`,
			[]string{"ListProjects org=ws_1 project=prj_a", "ListProjectEnvironments org=ws_1 project=prj_a"}, "environments"},
		{"catalog_search", `{"workspace":"ws_1","kind":"Component","q":"api","cursor":"c0","limit":5}`,
			[]string{"ListCatalogEntities org=ws_1 kind=Component q=api cursor=c0 limit=5"}, "catalog entities in ws_1"},
		{"catalog_read_doc", `{"workspace":"ws_1","role":"runbook"}`, []string{"ListCatalogDocs org=ws_1 role=runbook"}, "catalog docs"},
		{"runs_list", `{"workspace":"ws_1","branch":"main"}`, []string{"ListOrgRuns org=ws_1 status= branch=main"}, "runs in ws_1"},
		{"runs_list", `{"workspace":"ws_1","project":"prj_a","status":"failed"}`,
			[]string{"ListProjectRuns org=ws_1 project=prj_a status=failed"}, "runs for project prj_a"},
		{"runs_get", `{"workspace":"ws_1","project":"prj_a","runId":"r1"}`,
			[]string{"GetPlatformRun org=ws_1 project=prj_a run=r1", "ListPlatformRunJobs org=ws_1 project=prj_a run=r1"}, "run r1"},
		{"runs_read_logs", `{"workspace":"ws_1","project":"prj_a","runId":"r1","jobId":"j1","fromSeq":7}`,
			[]string{"ReadPlatformJobLogs org=ws_1 project=prj_a run=r1 job=j1 fromSeq=7"}, "fromSeq"},
		{"audit_search", `{"workspace":"ws_1","actorId":"usr_9","from":"2026-01-01T00:00:00Z"}`,
			[]string{"ListAuditEntries org=ws_1 actor=usr_9 from=2026-01-01T00:00:00Z"}, "audit entries"},
		{"events_search", `{"workspace":"ws_1","type":"run.*"}`, []string{"ListPlatformEvents org=ws_1 type=run.*"}, "events in ws_1"},
		{"events_search", `{"workspace":"ws_1","eventId":"evt_1"}`, []string{"GetPlatformEvent org=ws_1 event=evt_1"}, "event evt_1"},
		{"security_events_list", `{"limit":10}`, []string{"ListSecurityEvents cursor= limit=10"}, "security events"},
		{"access_explain", `{"workspace":"ws_1","subjectId":"usr_2"}`,
			[]string{"GetEffectiveAccess org=ws_1 subject=usr_2 project=", "ListOrgMembers org=ws_1", "ListOrgTeams org=ws_1"}, "effective access"},
		{"usage_summary", `{"workspace":"ws_1","metric":"runs","bucketType":"day"}`,
			[]string{"GetUsageSummary org=ws_1 metric=runs bucket=day"}, "usage summary for runs"},
		{"quota_check", `{"workspace":"ws_1","metric":"runs"}`, []string{"CheckQuota org=ws_1 metric=runs"}, "quota check for runs"},
		{"billing_summary", `{"workspace":"ws_1"}`, []string{"GetBillingSummary org=ws_1", "ListEntitlements org=ws_1"}, "billing posture"},
		{"config_read", `{"workspace":"ws_1","project":"prj_a","environment":"env_x"}`,
			[]string{"GetConfigSettings org=ws_1 project=prj_a env=env_x", "ListFeatureFlags org=ws_1 project=prj_a env=env_x"}, "environment scope"},
		{"secrets_list", `{"workspace":"ws_1"}`, []string{"ListSecretsMetadata org=ws_1 project= env="}, "organization scope"},
		{"webhook_deliveries_list", `{"workspace":"ws_1"}`, []string{"ListWebhookEndpoints org=ws_1 cursor="}, "webhook endpoints"},
		{"webhook_deliveries_list", `{"workspace":"ws_1","endpoint":"whep_1"}`,
			[]string{"ListWebhookDeliveries org=ws_1 endpoint=whep_1 cursor="}, "deliveries for webhook endpoint whep_1"},
	}
	for _, tc := range cases {
		api := &fakeAPI{page: page(`{"items":[{"id":1}]}`, "")}
		p := &Provider{API: api}
		text, isErr := callTool(t, p, tc.tool, tc.args)
		if isErr {
			t.Errorf("%s %s errored: %s", tc.tool, tc.args, text)
			continue
		}
		if len(api.calls) != len(tc.wantCalls) {
			t.Errorf("%s: calls = %v, want %v", tc.tool, api.calls, tc.wantCalls)
			continue
		}
		for i, want := range tc.wantCalls {
			if api.calls[i] != want {
				t.Errorf("%s: call %d = %q, want %q", tc.tool, i, api.calls[i], want)
			}
		}
		lines := strings.SplitN(text, "\n", 2)
		if len(lines) != 2 {
			t.Errorf("%s: output is not summary\\nJSON: %q", tc.tool, text)
			continue
		}
		if !strings.Contains(lines[0], tc.summaryFrag) {
			t.Errorf("%s: summary %q lacks %q", tc.tool, lines[0], tc.summaryFrag)
		}
		var payload interface{}
		if err := json.Unmarshal([]byte(lines[1]), &payload); err != nil {
			t.Errorf("%s: payload is not JSON: %v", tc.tool, err)
		}
		if !strings.Contains(lines[1], `{"id":1}`) {
			t.Errorf("%s: payload does not pass the data through: %s", tc.tool, lines[1])
		}
	}
}

// TestCatalogGetEntityEmulation: no single-entity route exists (SC0 never
// shipped) — the tool lists with q=entityRef and matches the exact ref
// client-side, following continuation cursors, like the TS tool.
func TestCatalogGetEntityEmulation(t *testing.T) {
	// Exact match wins over a same-page near-miss.
	api := &fakeAPI{page: page(`{"entities":[{"entityRef":"component:default/api-gateway"},{"entityRef":"component:default/api","owner":"team-a"}]}`, "")}
	p := &Provider{API: api}
	text, isErr := callTool(t, p, "catalog_get_entity", `{"workspace":"ws_1","entityRef":"component:default/api"}`)
	if isErr {
		t.Fatalf("catalog_get_entity errored: %s", text)
	}
	if api.calls[0] != "ListCatalogEntities org=ws_1 kind= q=component:default/api cursor= limit=0" {
		t.Fatalf("emulation call: %v", api.calls)
	}
	if !strings.Contains(text, `"owner":"team-a"`) || strings.Contains(text, "api-gateway") {
		t.Fatalf("exact-ref filtering failed: %s", text)
	}

	// The walk follows meta.cursor to a later page.
	api = &fakeAPI{pages: []*remotestate.PlatformPage{
		page(`{"entities":[{"entityRef":"component:default/api-gateway"}]}`, `{"cursor":"c1"}`),
		page(`{"entities":[{"entityRef":"component:default/api"}]}`, ""),
	}}
	p = &Provider{API: api}
	if _, isErr := callTool(t, p, "catalog_get_entity", `{"workspace":"ws_1","entityRef":"component:default/api"}`); isErr {
		t.Fatal("cursor-follow lookup failed")
	}
	if len(api.calls) != 2 || !strings.Contains(api.calls[1], "cursor=c1") {
		t.Fatalf("cursor not followed: %v", api.calls)
	}

	// No match anywhere → an isError verdict.
	p = &Provider{API: &fakeAPI{page: page(`{"entities":[]}`, "")}}
	text, isErr = callTool(t, p, "catalog_get_entity", `{"workspace":"ws_1","entityRef":"component:default/nope"}`)
	if !isErr || !strings.Contains(text, "no catalog entity with ref component:default/nope") {
		t.Fatalf("not-found shape: %q (isError=%v)", text, isErr)
	}
}

// TestCursorPassthrough: a page cursor rides through verbatim as meta.cursor
// in the emitted data and is flagged on the summary line.
func TestCursorPassthrough(t *testing.T) {
	api := &fakeAPI{page: page(`{"runs":[]}`, `{"cursor":"abc|123","total":9}`)}
	p := &Provider{API: api}
	text, isErr := callTool(t, p, "runs_list", `{"workspace":"ws_1"}`)
	if isErr {
		t.Fatalf("runs_list errored: %s", text)
	}
	lines := strings.SplitN(text, "\n", 2)
	if !strings.Contains(lines[0], "pass meta.cursor back as cursor") {
		t.Errorf("summary lacks the continuation hint: %q", lines[0])
	}
	var out struct {
		Meta struct {
			Cursor string `json:"cursor"`
		} `json:"meta"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &out); err != nil || out.Meta.Cursor != "abc|123" {
		t.Fatalf("meta.cursor not passed through: %s (err %v)", lines[1], err)
	}
	// And the cursor arg goes back to the wire verbatim.
	api.calls = nil
	callTool(t, p, "catalog_search", `{"workspace":"ws_1","cursor":"abc|123"}`)
	if api.calls[0] != "ListCatalogEntities org=ws_1 kind= q= cursor=abc|123 limit=0" {
		t.Fatalf("cursor arg not forwarded: %v", api.calls)
	}
}

// TestErrorMapping: a platform error keeps its code, message, and requestId
// on the isError result.
func TestErrorMapping(t *testing.T) {
	api := &fakeAPI{err: &remotestate.APIError{Code: "forbidden", Message: "missing member role", RequestID: "req_42", Status: 403}}
	p := &Provider{API: api}
	text, isErr := callTool(t, p, "audit_search", `{"workspace":"ws_1"}`)
	if !isErr {
		t.Fatal("platform error did not map to isError")
	}
	if text != "forbidden: missing member role (requestId: req_42)" {
		t.Fatalf("error text = %q", text)
	}
	// A non-API error keeps the workmcp "error: …" shape.
	p = &Provider{API: &fakeAPI{err: fmt.Errorf("backend down")}}
	text, isErr = callTool(t, p, "audit_search", `{"workspace":"ws_1"}`)
	if !isErr || text != "error: backend down" {
		t.Fatalf("plain error shape: %q (isError=%v)", text, isErr)
	}
}

// TestTruncationMarker: outputs over 64 KiB are byte-capped with the plane's
// exact marker.
func TestTruncationMarker(t *testing.T) {
	api := &fakeAPI{doc: []byte(strings.Repeat("x", 100*1024))}
	p := &Provider{API: api}
	text, isErr := callTool(t, p, "catalog_read_doc", `{"workspace":"ws_1","digest":"sha256:aa"}`)
	if isErr {
		t.Fatalf("catalog_read_doc errored: %s", text)
	}
	full := len("catalog doc sha256:aa\n") + 100*1024
	more := full - maxToolBytes
	marker := fmt.Sprintf("[truncated — %d more bytes; refine your query or use fromSeq/cursor]", more)
	if !strings.HasSuffix(text, marker) {
		t.Fatalf("missing truncation marker %q; tail: %q", marker, text[len(text)-90:])
	}
	if len(text) != maxToolBytes+len(marker) {
		t.Fatalf("truncated length = %d, want %d", len(text), maxToolBytes+len(marker))
	}
}

// TestWorkspaceDefault: the ambient default fills an absent workspace,
// explicit input wins, and without a default the tool fails actionably.
func TestWorkspaceDefault(t *testing.T) {
	api := &fakeAPI{page: page(`{}`, "")}
	p := &Provider{API: api, DefaultWorkspace: "ws_ambient"}
	if _, isErr := callTool(t, p, "projects_list", `{}`); isErr {
		t.Fatal("default workspace not applied")
	}
	if api.calls[0] != "ListProjects org=ws_ambient project=" {
		t.Fatalf("default not filled: %v", api.calls)
	}
	api.calls = nil
	callTool(t, p, "projects_list", `{"workspace":"ws_explicit"}`)
	if api.calls[0] != "ListProjects org=ws_explicit project=" {
		t.Fatalf("explicit workspace must win: %v", api.calls)
	}

	noDefault := &Provider{API: api}
	text, isErr := callTool(t, noDefault, "projects_list", `{}`)
	if !isErr || !strings.Contains(text, "workspace is required") {
		t.Fatalf("missing-workspace shape: %q (isError=%v)", text, isErr)
	}
}

// TestConfigScopeValidation: environment scope requires project (the
// discriminated scope's one invariant).
func TestConfigScopeValidation(t *testing.T) {
	p := &Provider{API: &fakeAPI{page: page(`{}`, "")}}
	text, isErr := callTool(t, p, "config_read", `{"workspace":"ws_1","environment":"env_x"}`)
	if !isErr || !strings.Contains(text, "environment scope requires project") {
		t.Fatalf("scope validation: %q (isError=%v)", text, isErr)
	}
}

// TestComposedServer: 28 tools (9 work + 19 platform) under one initialize,
// calls routed to the owning provider, and the WP-3/WP-10 forbidden-name
// sweep green over the merged roster.
func TestComposedServer(t *testing.T) {
	platformAPI := &fakeAPI{page: page(`{}`, "")}
	work := &workmcp.Server{API: workFake{}, Workspace: "ws_1"}
	platform := &Provider{API: platformAPI, DefaultWorkspace: "ws_1"}
	srv := &mcpserve.Server{Providers: []mcpserve.ToolProvider{work, platform}, Version: "test"}

	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"whoami","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"work_query","arguments":{}}}`,
	}, "\n") + "\n")
	var out strings.Builder
	if err := srv.Serve(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("responses = %d, want 4", len(lines))
	}

	var toolsResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &toolsResp); err != nil {
		t.Fatal(err)
	}
	if len(toolsResp.Result.Tools) != 28 {
		t.Fatalf("merged roster = %d tools, want 28 (9 work + 19 platform)", len(toolsResp.Result.Tools))
	}
	for _, tool := range toolsResp.Result.Tools {
		for _, frag := range mcpserve.ForbiddenNameFragments {
			if strings.Contains(tool.Name, frag) {
				t.Errorf("forbidden tool on the merged surface: %s", tool.Name)
			}
		}
	}
	if !strings.Contains(lines[2], "authenticated actor") {
		t.Errorf("whoami not routed to the platform provider: %s", lines[2])
	}
	if strings.Contains(lines[3], "isError") {
		t.Errorf("work_query not routed to the work provider: %s", lines[3])
	}
	if len(platformAPI.calls) == 0 {
		t.Error("platform seam never called")
	}
}

// workFake is a minimal workmcp.WorkAPI for the composition test.
type workFake struct{}

func (workFake) GetWorkSummary(context.Context) (*remotestate.WorkSummary, error) {
	return &remotestate.WorkSummary{}, nil
}
func (workFake) GetWorkTimeline(_ context.Context, key string) (*remotestate.WorkTimeline, error) {
	return &remotestate.WorkTimeline{Key: key}, nil
}
func (workFake) GetWorkDoc(_ context.Context, specKey, _ string) (*remotestate.WorkDoc, error) {
	return &remotestate.WorkDoc{SpecKey: specKey}, nil
}
func (workFake) CreateWorkTask(context.Context, remotestate.CreateWorkTaskRequest) (*remotestate.WorkMutationResponse, error) {
	return &remotestate.WorkMutationResponse{}, nil
}
func (workFake) CommentWork(context.Context, string, string) (*remotestate.WorkMutationResponse, error) {
	return &remotestate.WorkMutationResponse{}, nil
}
func (workFake) AssignWork(context.Context, string, string, bool) (*remotestate.WorkMutationResponse, error) {
	return &remotestate.WorkMutationResponse{}, nil
}
func (workFake) EditWorkContract(context.Context, string, remotestate.WorkContract) (*remotestate.WorkMutationResponse, error) {
	return &remotestate.WorkMutationResponse{}, nil
}
