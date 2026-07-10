package remotestate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// Public-API read methods (orun-mcp UM1) — the CLI's seam onto the Orun Cloud
// public API, the routes behind the platform MCP tool plane. Same discipline
// as work.go: doJSON-style transport (bearer, retry, Retry-After, platform
// error decode) with thin methods — params in, decoded JSON out. Payloads
// stay json.RawMessage: the MCP tools re-emit them verbatim; only what a tool
// inspects (cursors, entity refs) is typed.
//
// Route map, PINNED against the TS SDK (orun-cloud packages/sdk/src @
// f8dcc15 — the plane these tools mirror), not derived. org is the tool's
// `workspace` argument, passed per call — these routes are NOT bound to the
// client scope. There is NO single-catalog-entity route (SC0 never shipped):
// catalog_get_entity emulates over the entity list, client-side exact match.
//
//	GET /v1/auth/profile                                              → GetAuthProfile
//	GET /v1/organizations                                             → ListOrganizations
//	GET /v1/organizations/{org}/projects[?project=]                   → ListProjects
//	GET /v1/organizations/{org}/projects/{prj}/environments           → ListProjectEnvironments
//	GET /v1/organizations/{org}/catalog/entities[?facets…]            → ListCatalogEntities
//	GET /v1/organizations/{org}/catalog/docs[?filters…]               → ListCatalogDocs
//	GET /v1/organizations/{org}/catalog/doc?digest=sha256:…           → ReadCatalogDoc (raw markdown body)
//	GET /v1/organizations/{org}/state/runs[?filters…]                 → ListOrgRuns
//	GET /v1/organizations/{org}/projects/{prj}/state/runs[?filters…]  → ListProjectRuns
//	GET …/projects/{prj}/state/runs/{runId}                           → GetPlatformRun
//	GET …/projects/{prj}/state/runs/{runId}/jobs                      → ListPlatformRunJobs
//	GET …/projects/{prj}/state/runs/{runId}/logs/{jobId}[?fromSeq=]   → ReadPlatformJobLogs
//	GET /v1/organizations/{org}/audit[?filters…]                      → ListAuditEntries
//	GET /v1/organizations/{org}/events[?filters…]                     → ListPlatformEvents
//	GET /v1/organizations/{org}/events/{eventId}                      → GetPlatformEvent
//	GET /v1/auth/security-events[?cursor&limit]                       → ListSecurityEvents (actor-scoped)
//	GET /v1/organizations/{org}/effective-access[?subjectId&project]  → GetEffectiveAccess
//	GET /v1/organizations/{org}/members                               → ListOrgMembers
//	GET /v1/organizations/{org}/teams                                 → ListOrgTeams
//	GET /v1/organizations/{org}/usage/summary?metric=…                → GetUsageSummary
//	GET /v1/organizations/{org}/quotas/check?metric=…                 → CheckQuota
//	GET /v1/organizations/{org}/billing/summary                       → GetBillingSummary
//	GET /v1/organizations/{org}/billing/entitlements                  → ListEntitlements
//	GET <config scope>/config/settings                                → GetConfigSettings
//	GET <config scope>/config/feature-flags                           → ListFeatureFlags
//	GET <config scope>/config/secrets                                 → ListSecretsMetadata (metadata only)
//	GET /v1/organizations/{org}/webhooks/endpoints[?cursor&limit]     → ListWebhookEndpoints
//	GET …/webhooks/endpoints/{endpoint}/delivery-attempts[?…]         → ListWebhookDeliveries
//
// <config scope> is the tenancy-chain rung (configsurface convention):
// /v1/organizations/{org}[/projects/{prj}[/environments/{env}]].

// PlatformPage is one public-API response: the data payload plus the meta
// object (cursor etc.) passed through verbatim. Non-paginated reads simply
// have no Meta.
type PlatformPage struct {
	Data json.RawMessage `json:"data"`
	Meta json.RawMessage `json:"meta,omitempty"`
}

// Cursor extracts the continuation cursor from Meta ("" when absent) — the
// one meta field the MCP tools inspect.
func (p *PlatformPage) Cursor() string {
	if p == nil || len(p.Meta) == 0 {
		return ""
	}
	var m struct {
		Cursor string `json:"cursor"`
	}
	if json.Unmarshal(p.Meta, &m) != nil {
		return ""
	}
	return m.Cursor
}

// ConfigScope is the discriminated tenancy-chain rung a config read targets:
// organization (Project empty), project, or project+environment.
type ConfigScope struct {
	Org         string
	Project     string
	Environment string
}

func (s ConfigScope) path(suffix string) string {
	p := "/v1/organizations/" + urlSegment(s.Org)
	if s.Project != "" {
		p += "/projects/" + urlSegment(s.Project)
		if s.Environment != "" {
			p += "/environments/" + urlSegment(s.Environment)
		}
	}
	return p + suffix
}

// ── list query options (empty fields are omitted from the wire) ──────────────

// PageQuery is the bare cursor/limit pagination pair.
type PageQuery struct {
	Cursor string
	Limit  int
}

// CatalogEntitiesQuery filters the org catalog entity list.
type CatalogEntitiesQuery struct {
	Kind        string
	Owner       string
	Project     string
	Environment string
	Q           string
	Cursor      string
	Limit       int
}

// CatalogDocsQuery filters the catalog doc index.
type CatalogDocsQuery struct {
	EntityRef   string
	Role        string
	Project     string
	Environment string
	Q           string
	Cursor      string
	Limit       int
}

// OrgRunsQuery filters the org-wide run list.
type OrgRunsQuery struct {
	Status      string
	Environment string
	Branch      string
	Source      string
	Cursor      string
	Limit       int
}

// ProjectRunsQuery filters one project's run history.
type ProjectRunsQuery struct {
	Status      string
	Environment string
	Cursor      string
	Limit       int
}

// AuditQuery filters the audit-log search.
type AuditQuery struct {
	From        string
	To          string
	ActorID     string
	ActorType   string
	SubjectID   string
	SubjectKind string
	EventType   string
	Category    string
	Cursor      string
	Limit       int
}

// PlatformEventsQuery filters the typed event stream.
type PlatformEventsQuery struct {
	Type        string
	Source      string
	Project     string
	Environment string
	From        string
	To          string
	Cursor      string
	Limit       int
}

// UsageQuery scopes a usage summary.
type UsageQuery struct {
	Metric      string
	Project     string
	Environment string
	StartTime   string
	EndTime     string
	BucketType  string
}

// QuotaQuery scopes a quota check.
type QuotaQuery struct {
	Metric      string
	Project     string
	Environment string
	ResourceID  string
}

// ── methods ──────────────────────────────────────────────────────────────────

// GetAuthProfile calls GET /v1/auth/profile — the authenticated actor.
func (c *Client) GetAuthProfile(ctx context.Context) (*PlatformPage, error) {
	return c.platformGet(ctx, "/v1/auth/profile")
}

// ListOrganizations calls GET /v1/organizations — the workspaces the caller
// is a member of.
func (c *Client) ListOrganizations(ctx context.Context) (*PlatformPage, error) {
	return c.platformGet(ctx, "/v1/organizations")
}

// ListProjects calls GET /v1/organizations/{org}/projects, optionally
// narrowed to one project (id or slug).
func (c *Client) ListProjects(ctx context.Context, org, project string) (*PlatformPage, error) {
	q := url.Values{}
	setQ(q, "project", project)
	return c.platformGet(ctx, orgPath(org, "/projects")+encodeQ(q))
}

// ListProjectEnvironments calls GET …/projects/{prj}/environments.
func (c *Client) ListProjectEnvironments(ctx context.Context, org, project string) (*PlatformPage, error) {
	return c.platformGet(ctx, orgPath(org, "/projects/"+urlSegment(project)+"/environments"))
}

// ListCatalogEntities calls GET …/catalog/entities with the search facets.
func (c *Client) ListCatalogEntities(ctx context.Context, org string, opts CatalogEntitiesQuery) (*PlatformPage, error) {
	q := url.Values{}
	setQ(q, "kind", opts.Kind)
	setQ(q, "owner", opts.Owner)
	setQ(q, "project", opts.Project)
	setQ(q, "environment", opts.Environment)
	setQ(q, "q", opts.Q)
	setQ(q, "cursor", opts.Cursor)
	setLimit(q, opts.Limit)
	return c.platformGet(ctx, orgPath(org, "/catalog/entities")+encodeQ(q))
}

// ListCatalogDocs calls GET …/catalog/docs (the browse index).
func (c *Client) ListCatalogDocs(ctx context.Context, org string, opts CatalogDocsQuery) (*PlatformPage, error) {
	q := url.Values{}
	setQ(q, "entityRef", opts.EntityRef)
	setQ(q, "role", opts.Role)
	setQ(q, "project", opts.Project)
	setQ(q, "environment", opts.Environment)
	setQ(q, "q", opts.Q)
	setQ(q, "cursor", opts.Cursor)
	setLimit(q, opts.Limit)
	return c.platformGet(ctx, orgPath(org, "/catalog/docs")+encodeQ(q))
}

// ReadCatalogDoc calls GET …/catalog/doc?digest=sha256:… (singular; the
// digest rides as a query param because of the colon) and returns the raw
// markdown body — not a JSON envelope, so no {data,meta} unwrap.
func (c *Client) ReadCatalogDoc(ctx context.Context, org, digest string) ([]byte, error) {
	q := url.Values{}
	setQ(q, "digest", digest)
	return c.platformGetBytes(ctx, orgPath(org, "/catalog/doc")+encodeQ(q))
}

// ListOrgRuns calls GET …/state/runs — the org-wide run list.
func (c *Client) ListOrgRuns(ctx context.Context, org string, opts OrgRunsQuery) (*PlatformPage, error) {
	q := url.Values{}
	setQ(q, "status", opts.Status)
	setQ(q, "environment", opts.Environment)
	setQ(q, "branch", opts.Branch)
	setQ(q, "source", opts.Source)
	setQ(q, "cursor", opts.Cursor)
	setLimit(q, opts.Limit)
	return c.platformGet(ctx, orgPath(org, "/state/runs")+encodeQ(q))
}

// ListProjectRuns calls GET …/projects/{prj}/state/runs — one project's history.
func (c *Client) ListProjectRuns(ctx context.Context, org, project string, opts ProjectRunsQuery) (*PlatformPage, error) {
	q := url.Values{}
	setQ(q, "status", opts.Status)
	setQ(q, "environment", opts.Environment)
	setQ(q, "cursor", opts.Cursor)
	setLimit(q, opts.Limit)
	return c.platformGet(ctx, orgPath(org, "/projects/"+urlSegment(project)+"/state/runs")+encodeQ(q))
}

// GetPlatformRun calls GET …/projects/{prj}/state/runs/{runId}.
func (c *Client) GetPlatformRun(ctx context.Context, org, project, runID string) (*PlatformPage, error) {
	return c.platformGet(ctx, orgPath(org, "/projects/"+urlSegment(project)+"/state/runs/"+urlSegment(runID)))
}

// ListPlatformRunJobs calls GET …/state/runs/{runId}/jobs — the plan-DAG statuses.
func (c *Client) ListPlatformRunJobs(ctx context.Context, org, project, runID string) (*PlatformPage, error) {
	return c.platformGet(ctx, orgPath(org, "/projects/"+urlSegment(project)+"/state/runs/"+urlSegment(runID)+"/jobs"))
}

// ReadPlatformJobLogs calls GET …/state/runs/{runId}/logs/{jobId}?fromSeq= —
// the assembled log read with the live-tail cursor (logs/{jobId}, matching
// the state contract's log route shape).
func (c *Client) ReadPlatformJobLogs(ctx context.Context, org, project, runID, jobID string, fromSeq int) (*PlatformPage, error) {
	path := orgPath(org, "/projects/"+urlSegment(project)+"/state/runs/"+urlSegment(runID)+"/logs/"+urlSegment(jobID))
	if fromSeq > 0 {
		path += "?fromSeq=" + strconv.Itoa(fromSeq)
	}
	return c.platformGet(ctx, path)
}

// ListAuditEntries calls GET …/audit.
func (c *Client) ListAuditEntries(ctx context.Context, org string, opts AuditQuery) (*PlatformPage, error) {
	q := url.Values{}
	setQ(q, "from", opts.From)
	setQ(q, "to", opts.To)
	setQ(q, "actorId", opts.ActorID)
	setQ(q, "actorType", opts.ActorType)
	setQ(q, "subjectId", opts.SubjectID)
	setQ(q, "subjectKind", opts.SubjectKind)
	setQ(q, "eventType", opts.EventType)
	setQ(q, "category", opts.Category)
	setQ(q, "cursor", opts.Cursor)
	setLimit(q, opts.Limit)
	return c.platformGet(ctx, orgPath(org, "/audit")+encodeQ(q))
}

// ListPlatformEvents calls GET …/events — the typed event stream.
func (c *Client) ListPlatformEvents(ctx context.Context, org string, opts PlatformEventsQuery) (*PlatformPage, error) {
	q := url.Values{}
	setQ(q, "type", opts.Type)
	setQ(q, "source", opts.Source)
	setQ(q, "project", opts.Project)
	setQ(q, "environment", opts.Environment)
	setQ(q, "from", opts.From)
	setQ(q, "to", opts.To)
	setQ(q, "cursor", opts.Cursor)
	setLimit(q, opts.Limit)
	return c.platformGet(ctx, orgPath(org, "/events")+encodeQ(q))
}

// GetPlatformEvent calls GET …/events/{eventId}.
func (c *Client) GetPlatformEvent(ctx context.Context, org, eventID string) (*PlatformPage, error) {
	return c.platformGet(ctx, orgPath(org, "/events/"+urlSegment(eventID)))
}

// ListSecurityEvents calls GET /v1/auth/security-events — actor-scoped, no
// org segment.
func (c *Client) ListSecurityEvents(ctx context.Context, opts PageQuery) (*PlatformPage, error) {
	q := url.Values{}
	setQ(q, "cursor", opts.Cursor)
	setLimit(q, opts.Limit)
	return c.platformGet(ctx, "/v1/auth/security-events"+encodeQ(q))
}

// GetEffectiveAccess calls GET …/effective-access — permissions with grant
// provenance; defaults to the caller when subjectID is empty.
func (c *Client) GetEffectiveAccess(ctx context.Context, org, subjectID, project string) (*PlatformPage, error) {
	q := url.Values{}
	setQ(q, "subjectId", subjectID)
	setQ(q, "project", project)
	return c.platformGet(ctx, orgPath(org, "/effective-access")+encodeQ(q))
}

// ListOrgMembers calls GET …/members.
func (c *Client) ListOrgMembers(ctx context.Context, org string) (*PlatformPage, error) {
	return c.platformGet(ctx, orgPath(org, "/members"))
}

// ListOrgTeams calls GET …/teams.
func (c *Client) ListOrgTeams(ctx context.Context, org string) (*PlatformPage, error) {
	return c.platformGet(ctx, orgPath(org, "/teams"))
}

// GetUsageSummary calls GET …/usage/summary?metric=….
func (c *Client) GetUsageSummary(ctx context.Context, org string, opts UsageQuery) (*PlatformPage, error) {
	q := url.Values{}
	setQ(q, "metric", opts.Metric)
	setQ(q, "project", opts.Project)
	setQ(q, "environment", opts.Environment)
	setQ(q, "startTime", opts.StartTime)
	setQ(q, "endTime", opts.EndTime)
	setQ(q, "bucketType", opts.BucketType)
	return c.platformGet(ctx, orgPath(org, "/usage/summary")+encodeQ(q))
}

// CheckQuota calls GET …/quotas/check?metric=….
func (c *Client) CheckQuota(ctx context.Context, org string, opts QuotaQuery) (*PlatformPage, error) {
	q := url.Values{}
	setQ(q, "metric", opts.Metric)
	setQ(q, "project", opts.Project)
	setQ(q, "environment", opts.Environment)
	setQ(q, "resourceId", opts.ResourceID)
	return c.platformGet(ctx, orgPath(org, "/quotas/check")+encodeQ(q))
}

// GetBillingSummary calls GET …/billing/summary.
func (c *Client) GetBillingSummary(ctx context.Context, org string) (*PlatformPage, error) {
	return c.platformGet(ctx, orgPath(org, "/billing/summary"))
}

// ListEntitlements calls GET …/billing/entitlements.
func (c *Client) ListEntitlements(ctx context.Context, org string) (*PlatformPage, error) {
	return c.platformGet(ctx, orgPath(org, "/billing/entitlements"))
}

// GetConfigSettings calls GET <scope>/config/settings.
func (c *Client) GetConfigSettings(ctx context.Context, scope ConfigScope) (*PlatformPage, error) {
	return c.platformGet(ctx, scope.path("/config/settings"))
}

// ListFeatureFlags calls GET <scope>/config/feature-flags.
func (c *Client) ListFeatureFlags(ctx context.Context, scope ConfigScope) (*PlatformPage, error) {
	return c.platformGet(ctx, scope.path("/config/feature-flags"))
}

// ListSecretsMetadata calls GET <scope>/config/secrets — metadata only; no
// route on this surface can return a secret value.
func (c *Client) ListSecretsMetadata(ctx context.Context, scope ConfigScope) (*PlatformPage, error) {
	return c.platformGet(ctx, scope.path("/config/secrets"))
}

// ListWebhookEndpoints calls GET …/webhooks/endpoints.
func (c *Client) ListWebhookEndpoints(ctx context.Context, org string, opts PageQuery) (*PlatformPage, error) {
	q := url.Values{}
	setQ(q, "cursor", opts.Cursor)
	setLimit(q, opts.Limit)
	return c.platformGet(ctx, orgPath(org, "/webhooks/endpoints")+encodeQ(q))
}

// ListWebhookDeliveries calls GET …/webhooks/endpoints/{endpoint}/delivery-attempts.
func (c *Client) ListWebhookDeliveries(ctx context.Context, org, endpoint string, opts PageQuery) (*PlatformPage, error) {
	q := url.Values{}
	setQ(q, "cursor", opts.Cursor)
	setLimit(q, opts.Limit)
	return c.platformGet(ctx, orgPath(org, "/webhooks/endpoints/"+urlSegment(endpoint)+"/delivery-attempts")+encodeQ(q))
}

// ── transport helpers ────────────────────────────────────────────────────────

func orgPath(org, suffix string) string {
	return "/v1/organizations/" + urlSegment(org) + suffix
}

func setQ(q url.Values, key, val string) {
	if val != "" {
		q.Set(key, val)
	}
}

func setLimit(q url.Values, limit int) {
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
}

func encodeQ(q url.Values) string {
	if enc := q.Encode(); enc != "" {
		return "?" + enc
	}
	return ""
}

// platformGet performs a retried GET that keeps the platform success envelope
// ({data, meta}) intact — doJSON's decode drops meta, and the MCP tools pass
// cursors through, so this path captures both halves verbatim. A flat (OSS)
// body becomes Data with no Meta.
func (c *Client) platformGet(ctx context.Context, path string) (*PlatformPage, error) {
	var page PlatformPage
	err := c.doRawWithRetry(ctx, maxRetryAttempts, func() error {
		body, err := c.platformGetOnce(ctx, path)
		if err != nil {
			return err
		}
		trimmed := bytes.TrimSpace(body)
		if len(trimmed) == 0 {
			page = PlatformPage{}
			return nil
		}
		var env struct {
			Data json.RawMessage `json:"data"`
			Meta json.RawMessage `json:"meta"`
		}
		if trimmed[0] == '{' && json.Unmarshal(trimmed, &env) == nil && len(env.Data) > 0 {
			page = PlatformPage{Data: env.Data, Meta: env.Meta}
			return nil
		}
		page = PlatformPage{Data: trimmed}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &page, nil
}

// platformGetBytes performs a retried GET returning the raw response body
// (doc bodies are markdown, not JSON).
func (c *Client) platformGetBytes(ctx context.Context, path string) ([]byte, error) {
	var body []byte
	err := c.doRawWithRetry(ctx, maxRetryAttempts, func() error {
		b, err := c.platformGetOnce(ctx, path)
		if err != nil {
			return err
		}
		body = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (c *Client) platformGetOnce(ctx context.Context, path string) ([]byte, error) {
	token, err := c.tokenSrc.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolving auth token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, c.parseError(resp)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	return data, nil
}
