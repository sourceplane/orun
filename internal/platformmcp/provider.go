package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sourceplane/orun/internal/mcpserve"
	"github.com/sourceplane/orun/internal/remotestate"
)

// PlatformAPI is the seam onto the Orun Cloud public API; *remotestate.Client
// implements it (client-not-service: every call carries the caller's own
// bearer, so RBAC, rate limits, audit, and metering apply unchanged).
type PlatformAPI interface {
	GetAuthProfile(ctx context.Context) (*remotestate.PlatformPage, error)
	ListOrganizations(ctx context.Context) (*remotestate.PlatformPage, error)
	ListProjects(ctx context.Context, org, project string) (*remotestate.PlatformPage, error)
	ListProjectEnvironments(ctx context.Context, org, project string) (*remotestate.PlatformPage, error)
	ListCatalogEntities(ctx context.Context, org string, opts remotestate.CatalogEntitiesQuery) (*remotestate.PlatformPage, error)
	ListCatalogDocs(ctx context.Context, org string, opts remotestate.CatalogDocsQuery) (*remotestate.PlatformPage, error)
	ReadCatalogDoc(ctx context.Context, org, digest string) ([]byte, error)
	ListOrgRuns(ctx context.Context, org string, opts remotestate.OrgRunsQuery) (*remotestate.PlatformPage, error)
	ListProjectRuns(ctx context.Context, org, project string, opts remotestate.ProjectRunsQuery) (*remotestate.PlatformPage, error)
	GetPlatformRun(ctx context.Context, org, project, runID string) (*remotestate.PlatformPage, error)
	ListPlatformRunJobs(ctx context.Context, org, project, runID string) (*remotestate.PlatformPage, error)
	ReadPlatformJobLogs(ctx context.Context, org, project, runID, jobID string, fromSeq int) (*remotestate.PlatformPage, error)
	ListAuditEntries(ctx context.Context, org string, opts remotestate.AuditQuery) (*remotestate.PlatformPage, error)
	ListPlatformEvents(ctx context.Context, org string, opts remotestate.PlatformEventsQuery) (*remotestate.PlatformPage, error)
	GetPlatformEvent(ctx context.Context, org, eventID string) (*remotestate.PlatformPage, error)
	ListSecurityEvents(ctx context.Context, opts remotestate.PageQuery) (*remotestate.PlatformPage, error)
	GetEffectiveAccess(ctx context.Context, org, subjectID, project string) (*remotestate.PlatformPage, error)
	ListOrgMembers(ctx context.Context, org string) (*remotestate.PlatformPage, error)
	ListOrgTeams(ctx context.Context, org string) (*remotestate.PlatformPage, error)
	GetUsageSummary(ctx context.Context, org string, opts remotestate.UsageQuery) (*remotestate.PlatformPage, error)
	CheckQuota(ctx context.Context, org string, opts remotestate.QuotaQuery) (*remotestate.PlatformPage, error)
	GetBillingSummary(ctx context.Context, org string) (*remotestate.PlatformPage, error)
	ListEntitlements(ctx context.Context, org string) (*remotestate.PlatformPage, error)
	GetConfigSettings(ctx context.Context, scope remotestate.ConfigScope) (*remotestate.PlatformPage, error)
	ListFeatureFlags(ctx context.Context, scope remotestate.ConfigScope) (*remotestate.PlatformPage, error)
	ListSecretsMetadata(ctx context.Context, scope remotestate.ConfigScope) (*remotestate.PlatformPage, error)
	ListWebhookEndpoints(ctx context.Context, org string, opts remotestate.PageQuery) (*remotestate.PlatformPage, error)
	ListWebhookDeliveries(ctx context.Context, org, endpoint string, opts remotestate.PageQuery) (*remotestate.PlatformPage, error)

	// Writes (UM2): each carries the per-attempt Idempotency-Key.
	CreateProject(ctx context.Context, org string, body interface{}, idemKey string) (*remotestate.PlatformPage, error)
	CreateProjectEnvironment(ctx context.Context, org, project string, body interface{}, idemKey string) (*remotestate.PlatformPage, error)
	CreateFeatureFlag(ctx context.Context, scope remotestate.ConfigScope, body interface{}, idemKey string) (*remotestate.PlatformPage, error)
	UpdateFeatureFlag(ctx context.Context, scope remotestate.ConfigScope, flagID string, body interface{}, idemKey string) (*remotestate.PlatformPage, error)
	CreateWebhookEndpoint(ctx context.Context, org, project string, body interface{}, idemKey string) (*remotestate.PlatformPage, error)
	CreateWebhookSubscription(ctx context.Context, org string, body interface{}, idemKey string) (*remotestate.PlatformPage, error)
	ReplayWebhookDelivery(ctx context.Context, org, attemptID, idemKey string) (*remotestate.PlatformPage, error)
	CreateInvitation(ctx context.Context, org string, body interface{}, idemKey string) (*remotestate.PlatformPage, error)
}

// Provider is the platform-plane mcpserve.ToolProvider. DefaultWorkspace,
// when set (the serve-time resolved scope), fills an absent `workspace`
// argument and makes it optional on the advertised schemas; an explicit
// argument always wins. ReadOnly filters the 6 write tools out of the
// advertised roster (and blocks their execution): 19 tools instead of 25.
type Provider struct {
	API              PlatformAPI
	DefaultWorkspace string
	ReadOnly         bool

	// ent is the lazy per-workspace feature.mcp_server gate (design §3).
	ent entitlementGate
}

// maxToolBytes caps one tool result's text (parity with the TS plane's
// 64 KiB output discipline).
const maxToolBytes = 64 * 1024

// maxEntityLookupPages bounds catalog_get_entity's list-emulation walk.
const maxEntityLookupPages = 5

// Tools implements mcpserve.ToolProvider: the manifest's tools, verbatim
// (writes filtered out under ReadOnly), with `workspace` demoted from
// required when a default is active.
func (p *Provider) Tools() []mcpserve.ToolDef {
	out := make([]mcpserve.ToolDef, 0, len(allTools))
	for i := range allTools {
		t := &allTools[i]
		if p.ReadOnly && !t.readOnly {
			continue
		}
		schema := decodeSchema(t.InputSchema)
		if p.DefaultWorkspace != "" && t.hasWorkspace {
			dropRequired(schema, "workspace")
		}
		out = append(out, mcpserve.ToolDef{
			Name:        t.Name,
			Title:       t.Title,
			Description: t.Description,
			InputSchema: schema,
			Annotations: t.Annotations,
		})
	}
	return out
}

// Call implements mcpserve.ToolProvider: owned=false outside the platform
// roster; every owned failure maps to an isError result with the platform
// error code preserved.
func (p *Provider) Call(ctx context.Context, name string, raw json.RawMessage) (mcpserve.Result, bool) {
	t, ok := toolsByName[name]
	if !ok {
		return nil, false
	}
	if p.ReadOnly && !t.readOnly {
		// Filtered from tools/list AND blocked at execution (design §3).
		return mcpserve.TextResult(fmt.Sprintf("error: %s is unavailable: this server is running --read-only", name), true), true
	}
	a := argmap{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return mcpserve.TextResult(fmt.Sprintf("error: %s: invalid arguments: %v", name, err), true), true
		}
	}
	if t.hasWorkspace && a.str("workspace") == "" {
		if p.DefaultWorkspace != "" {
			a["workspace"] = p.DefaultWorkspace
		} else if t.workspaceRequired {
			return mcpserve.TextResult(fmt.Sprintf("error: %s: workspace is required (call whoami or workspaces_list to discover yours)", name), true), true
		}
	}
	// Entitlement gate: every workspace-carrying call (reads and writes)
	// checks feature.mcp_server lazily, once per workspace per TTL.
	if ws := a.str("workspace"); ws != "" {
		if err := p.ent.check(ctx, p.API, ws); err != nil {
			return mcpserve.TextResult(errText(err), true), true
		}
	}
	var text string
	var err error
	if t.readOnly {
		text, err = p.call(ctx, name, a)
	} else {
		text, err = p.callWrite(ctx, name, a)
	}
	if err != nil {
		return mcpserve.TextResult(errText(err), true), true
	}
	return mcpserve.TextResult(truncate(text), false), true
}

// argmap is the decoded tool-argument object; unconsumed members pass
// through to the wire untouched (the schema is the manifest's business).
type argmap map[string]interface{}

func (a argmap) str(k string) string {
	v, _ := a[k].(string)
	return v
}

func (a argmap) intval(k string) int {
	if v, ok := a[k].(float64); ok {
		return int(v)
	}
	return 0
}

// errText renders a tool failure: platform errors keep their code —
// `<code>: <message> (requestId: …)` — so the agent can reason about
// forbidden/not_found/rate_limited verdicts; anything else is `error: …`.
func errText(err error) string {
	var apiErr *remotestate.APIError
	if errors.As(err, &apiErr) && apiErr.Code != "" {
		s := apiErr.Code + ": " + apiErr.Message
		if apiErr.RequestID != "" {
			s += " (requestId: " + apiErr.RequestID + ")"
		}
		return s
	}
	return "error: " + err.Error()
}

// truncate byte-caps a tool result, appending the plane's exact marker.
func truncate(text string) string {
	if len(text) <= maxToolBytes {
		return text
	}
	more := len(text) - maxToolBytes
	return text[:maxToolBytes] + fmt.Sprintf("[truncated — %d more bytes; refine your query or use fromSeq/cursor]", more)
}

// emit renders the plane's output discipline: one summary line, then the
// compact JSON payload.
func emit(summary string, payload interface{}) (string, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return summary + "\n" + string(b), nil
}

// emitPage renders a single-page passthrough ({data, meta} verbatim —
// cursors ride in meta.cursor), noting continuation on the summary line.
func emitPage(summary string, page *remotestate.PlatformPage) (string, error) {
	if page.Cursor() != "" {
		summary += " (more available — pass meta.cursor back as cursor)"
	}
	return emit(summary, page)
}

func requireStr(a argmap, tool string, keys ...string) error {
	for _, k := range keys {
		if a.str(k) == "" {
			return fmt.Errorf("%s: %s is required", tool, k)
		}
	}
	return nil
}

func (p *Provider) call(ctx context.Context, name string, a argmap) (string, error) {
	ws := a.str("workspace")
	switch name {
	case "whoami":
		profile, err := p.API.GetAuthProfile(ctx)
		if err != nil {
			return "", err
		}
		orgs, err := p.API.ListOrganizations(ctx)
		if err != nil {
			return "", err
		}
		return emit("authenticated actor and workspace memberships",
			map[string]interface{}{"actor": profile.Data, "workspaces": orgs.Data})

	case "workspaces_list":
		page, err := p.API.ListOrganizations(ctx)
		if err != nil {
			return "", err
		}
		return emitPage("workspaces the caller belongs to", page)

	case "projects_list":
		project := a.str("project")
		page, err := p.API.ListProjects(ctx, ws, project)
		if err != nil {
			return "", err
		}
		if project == "" {
			return emitPage("projects in "+ws, page)
		}
		envs, err := p.API.ListProjectEnvironments(ctx, ws, project)
		if err != nil {
			return "", err
		}
		return emit(fmt.Sprintf("project %s in %s with environments", project, ws),
			map[string]interface{}{"data": page.Data, "environments": envs.Data})

	case "catalog_search":
		page, err := p.API.ListCatalogEntities(ctx, ws, remotestate.CatalogEntitiesQuery{
			Kind: a.str("kind"), Owner: a.str("owner"), Project: a.str("project"),
			Environment: a.str("environment"), Q: a.str("q"),
			Cursor: a.str("cursor"), Limit: a.intval("limit"),
		})
		if err != nil {
			return "", err
		}
		return emitPage("catalog entities in "+ws, page)

	case "catalog_get_entity":
		if err := requireStr(a, name, "entityRef"); err != nil {
			return "", err
		}
		// No single-entity route exists (SC0 never shipped): emulate over the
		// entity list with an exact client-side entityRef match, exactly like
		// the TS tool. q free-text matches entityRef, so page one nearly
		// always carries the hit; a few continuation pages bound the tail.
		ref := a.str("entityRef")
		cursor := ""
		for range [maxEntityLookupPages]struct{}{} {
			page, err := p.API.ListCatalogEntities(ctx, ws, remotestate.CatalogEntitiesQuery{
				Q: ref, Project: a.str("project"), Environment: a.str("environment"), Cursor: cursor,
			})
			if err != nil {
				return "", err
			}
			if entity, ok := findEntity(page.Data, ref); ok {
				return emit("catalog entity "+ref, map[string]interface{}{"entity": entity})
			}
			if cursor = page.Cursor(); cursor == "" {
				break
			}
		}
		return "", fmt.Errorf("%s: no catalog entity with ref %s", name, ref)

	case "catalog_read_doc":
		if digest := a.str("digest"); digest != "" {
			body, err := p.API.ReadCatalogDoc(ctx, ws, digest)
			if err != nil {
				return "", err
			}
			return "catalog doc " + digest + "\n" + string(body), nil
		}
		page, err := p.API.ListCatalogDocs(ctx, ws, remotestate.CatalogDocsQuery{
			EntityRef: a.str("entityRef"), Role: a.str("role"), Project: a.str("project"),
			Environment: a.str("environment"), Q: a.str("q"),
			Cursor: a.str("cursor"), Limit: a.intval("limit"),
		})
		if err != nil {
			return "", err
		}
		return emitPage("catalog docs in "+ws+" (pass a row's digest to read one)", page)

	case "runs_list":
		if project := a.str("project"); project != "" {
			page, err := p.API.ListProjectRuns(ctx, ws, project, remotestate.ProjectRunsQuery{
				Status: a.str("status"), Environment: a.str("environment"),
				Cursor: a.str("cursor"), Limit: a.intval("limit"),
			})
			if err != nil {
				return "", err
			}
			return emitPage(fmt.Sprintf("runs for project %s in %s", project, ws), page)
		}
		page, err := p.API.ListOrgRuns(ctx, ws, remotestate.OrgRunsQuery{
			Status: a.str("status"), Environment: a.str("environment"),
			Branch: a.str("branch"), Source: a.str("source"),
			Cursor: a.str("cursor"), Limit: a.intval("limit"),
		})
		if err != nil {
			return "", err
		}
		return emitPage("runs in "+ws, page)

	case "runs_get":
		if err := requireStr(a, name, "project", "runId"); err != nil {
			return "", err
		}
		run, err := p.API.GetPlatformRun(ctx, ws, a.str("project"), a.str("runId"))
		if err != nil {
			return "", err
		}
		jobs, err := p.API.ListPlatformRunJobs(ctx, ws, a.str("project"), a.str("runId"))
		if err != nil {
			return "", err
		}
		return emit("run "+a.str("runId")+" with its plan-DAG jobs",
			map[string]interface{}{"run": run.Data, "jobs": jobs.Data})

	case "runs_read_logs":
		if err := requireStr(a, name, "project", "runId", "jobId"); err != nil {
			return "", err
		}
		page, err := p.API.ReadPlatformJobLogs(ctx, ws, a.str("project"), a.str("runId"), a.str("jobId"), a.intval("fromSeq"))
		if err != nil {
			return "", err
		}
		return emit(fmt.Sprintf("logs for job %s (pass nextSeq back as fromSeq to resume)", a.str("jobId")), page)

	case "audit_search":
		page, err := p.API.ListAuditEntries(ctx, ws, remotestate.AuditQuery{
			From: a.str("from"), To: a.str("to"),
			ActorID: a.str("actorId"), ActorType: a.str("actorType"),
			SubjectID: a.str("subjectId"), SubjectKind: a.str("subjectKind"),
			EventType: a.str("eventType"), Category: a.str("category"),
			Cursor: a.str("cursor"), Limit: a.intval("limit"),
		})
		if err != nil {
			return "", err
		}
		return emitPage("audit entries in "+ws, page)

	case "events_search":
		if eventID := a.str("eventId"); eventID != "" {
			page, err := p.API.GetPlatformEvent(ctx, ws, eventID)
			if err != nil {
				return "", err
			}
			return emit("event "+eventID, page)
		}
		page, err := p.API.ListPlatformEvents(ctx, ws, remotestate.PlatformEventsQuery{
			Type: a.str("type"), Source: a.str("source"),
			Project: a.str("project"), Environment: a.str("environment"),
			From: a.str("from"), To: a.str("to"),
			Cursor: a.str("cursor"), Limit: a.intval("limit"),
		})
		if err != nil {
			return "", err
		}
		return emitPage("events in "+ws, page)

	case "security_events_list":
		page, err := p.API.ListSecurityEvents(ctx, remotestate.PageQuery{Cursor: a.str("cursor"), Limit: a.intval("limit")})
		if err != nil {
			return "", err
		}
		return emitPage("security events for the calling actor", page)

	case "access_explain":
		access, err := p.API.GetEffectiveAccess(ctx, ws, a.str("subjectId"), a.str("project"))
		if err != nil {
			return "", err
		}
		members, err := p.API.ListOrgMembers(ctx, ws)
		if err != nil {
			return "", err
		}
		teams, err := p.API.ListOrgTeams(ctx, ws)
		if err != nil {
			return "", err
		}
		return emit("effective access in "+ws+" with member and team rosters",
			map[string]interface{}{"access": access.Data, "members": members.Data, "teams": teams.Data})

	case "usage_summary":
		if err := requireStr(a, name, "metric"); err != nil {
			return "", err
		}
		page, err := p.API.GetUsageSummary(ctx, ws, remotestate.UsageQuery{
			Metric: a.str("metric"), Project: a.str("project"), Environment: a.str("environment"),
			StartTime: a.str("startTime"), EndTime: a.str("endTime"), BucketType: a.str("bucketType"),
		})
		if err != nil {
			return "", err
		}
		return emit(fmt.Sprintf("usage summary for %s in %s", a.str("metric"), ws), page)

	case "quota_check":
		if err := requireStr(a, name, "metric"); err != nil {
			return "", err
		}
		page, err := p.API.CheckQuota(ctx, ws, remotestate.QuotaQuery{
			Metric: a.str("metric"), Project: a.str("project"),
			Environment: a.str("environment"), ResourceID: a.str("resourceId"),
		})
		if err != nil {
			return "", err
		}
		return emit(fmt.Sprintf("quota check for %s in %s", a.str("metric"), ws), page)

	case "billing_summary":
		billing, err := p.API.GetBillingSummary(ctx, ws)
		if err != nil {
			return "", err
		}
		entitlements, err := p.API.ListEntitlements(ctx, ws)
		if err != nil {
			return "", err
		}
		return emit("billing posture and entitlements for "+ws,
			map[string]interface{}{"billing": billing.Data, "entitlements": entitlements.Data})

	case "config_read":
		scope, scopeName, err := configScope(a, name, ws)
		if err != nil {
			return "", err
		}
		settings, err := p.API.GetConfigSettings(ctx, scope)
		if err != nil {
			return "", err
		}
		flags, err := p.API.ListFeatureFlags(ctx, scope)
		if err != nil {
			return "", err
		}
		return emit("settings and feature flags at "+scopeName+" scope",
			map[string]interface{}{"settings": settings.Data, "flags": flags.Data})

	case "secrets_list":
		scope, scopeName, err := configScope(a, name, ws)
		if err != nil {
			return "", err
		}
		page, err := p.API.ListSecretsMetadata(ctx, scope)
		if err != nil {
			return "", err
		}
		return emitPage("secret metadata at "+scopeName+" scope (values are write-only platform-wide)", page)

	case "webhook_deliveries_list":
		if endpoint := a.str("endpoint"); endpoint != "" {
			page, err := p.API.ListWebhookDeliveries(ctx, ws, endpoint, remotestate.PageQuery{Cursor: a.str("cursor"), Limit: a.intval("limit")})
			if err != nil {
				return "", err
			}
			return emitPage("deliveries for webhook endpoint "+endpoint, page)
		}
		page, err := p.API.ListWebhookEndpoints(ctx, ws, remotestate.PageQuery{Cursor: a.str("cursor"), Limit: a.intval("limit")})
		if err != nil {
			return "", err
		}
		return emitPage("webhook endpoints in "+ws+" (pass one as endpoint to list its deliveries)", page)

	default:
		return "", fmt.Errorf("unknown tool %s", name)
	}
}

// findEntity scans a catalog list page's data for the row whose entityRef
// equals ref exactly, returning it verbatim. The items array may be the data
// itself or sit under an "entities"/"items" key.
func findEntity(data json.RawMessage, ref string) (json.RawMessage, bool) {
	var items []json.RawMessage
	if json.Unmarshal(data, &items) != nil {
		var obj map[string]json.RawMessage
		if json.Unmarshal(data, &obj) != nil {
			return nil, false
		}
		for _, key := range []string{"entities", "items"} {
			if raw, ok := obj[key]; ok && json.Unmarshal(raw, &items) == nil {
				break
			}
		}
	}
	for _, item := range items {
		var row struct {
			EntityRef string `json:"entityRef"`
		}
		if json.Unmarshal(item, &row) == nil && row.EntityRef == ref {
			return item, true
		}
	}
	return nil, false
}

// configScope resolves the discriminated config scope from the tool args:
// organization (default), project, or project+environment.
func configScope(a argmap, tool, ws string) (remotestate.ConfigScope, string, error) {
	project, environment := a.str("project"), a.str("environment")
	if environment != "" && project == "" {
		return remotestate.ConfigScope{}, "", fmt.Errorf("%s: environment scope requires project", tool)
	}
	scope := remotestate.ConfigScope{Org: ws, Project: project, Environment: environment}
	switch {
	case environment != "":
		return scope, "environment", nil
	case project != "":
		return scope, "project", nil
	default:
		return scope, "organization", nil
	}
}
