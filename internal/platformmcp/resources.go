package platformmcp

// MCP resources (orun-mcp UM6, mirroring the TS plane's resources.ts): the
// two templates — catalog://{workspace}/{entityKey} and
// runs://{workspace}/{project}/{runId} — each read as one markdown document.
// Concrete resources are never enumerated (the loop answers resources/list
// with an empty list): agents discover ids via catalog_search / runs_list
// and attach the specific URI. Reads have no isError channel, so an owned
// failure is returned as an error whose message carries the platform code —
// errText semantics, the TS ResourceReadError posture.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/sourceplane/orun/internal/mcpserve"
	"github.com/sourceplane/orun/internal/remotestate"
)

// resourceMimeType is the TS plane's RESOURCE_MIME_TYPE. The manifest stub
// carries no mimeType; the provider supplies it.
const resourceMimeType = "text/markdown"

// resourceListLimit is the single-page fetch size the TS resource reads use
// (no cursor walk — the exact-ref q filter nearly always hits page one).
const resourceListLimit = 100

// ResourceTemplates implements mcpserve.ResourceProvider: the manifest's
// resources section, verbatim, plus the mime type.
func (p *Provider) ResourceTemplates() []mcpserve.ResourceTemplateDef {
	out := make([]mcpserve.ResourceTemplateDef, 0, len(embedded.Resources))
	for _, r := range embedded.Resources {
		out = append(out, mcpserve.ResourceTemplateDef{
			URITemplate: r.URITemplate,
			Name:        r.Name,
			Title:       r.Title,
			Description: r.Description,
			MimeType:    resourceMimeType,
		})
	}
	return out
}

// ReadResource implements mcpserve.ResourceProvider: owned when the uri
// carries one of the two templates' schemes.
func (p *Provider) ReadResource(ctx context.Context, uri string) ([]mcpserve.ResourceContent, bool, error) {
	var (
		text string
		err  error
	)
	switch {
	case strings.HasPrefix(uri, "catalog://"):
		text, err = p.readCatalogEntity(ctx, strings.TrimPrefix(uri, "catalog://"))
	case strings.HasPrefix(uri, "runs://"):
		text, err = p.readRunSummary(ctx, strings.TrimPrefix(uri, "runs://"))
	default:
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	return []mcpserve.ResourceContent{{URI: uri, MimeType: resourceMimeType, Text: text}}, true, nil
}

// ── entityKey codec ─────────────────────────────────────────
// Entity refs contain `:` and `/` (`component:default/api`), which cannot
// ride one URI path segment verbatim; entityKey is base64url(entityRef) —
// lossless for any ref, URL-safe by construction (the TS codec).

var entityKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// EncodeEntityKey mints the URI-safe form of an entity ref (unpadded
// base64url), exported so callers and tests can build catalog:// URIs.
func EncodeEntityKey(entityRef string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(entityRef))
}

func decodeEntityKey(entityKey string) (string, error) {
	if !entityKeyPattern.MatchString(entityKey) {
		return "", errors.New("validation_failed: `entityKey` must be base64url(entityRef), e.g. base64url(`component:default/api`)")
	}
	b, err := base64.RawURLEncoding.DecodeString(entityKey)
	if err != nil {
		return "", errors.New("validation_failed: `entityKey` is not valid base64url")
	}
	return string(b), nil
}

// ── catalog://{workspace}/{entityKey} ───────────────────────

func (p *Provider) readCatalogEntity(ctx context.Context, rest string) (string, error) {
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", errors.New("validation_failed: uri must match catalog://{workspace}/{entityKey}")
	}
	ws := parts[0]
	ref, err := decodeEntityKey(parts[1])
	if err != nil {
		return "", err
	}
	// Same emulation path as catalog_get_entity (SC0 unshipped): exact-filter
	// the entity list — one page of 100, like the TS resource.
	entPage, err := p.API.ListCatalogEntities(ctx, ws, remotestate.CatalogEntitiesQuery{Q: ref, Limit: resourceListLimit})
	if err != nil {
		return "", errors.New(errText(err))
	}
	raw, found := findEntity(entPage.Data, ref)
	if !found {
		return "", fmt.Errorf("not_found: no catalog entity with ref %q in workspace %s", ref, ws)
	}
	var entity catalogEntityRow
	if err := json.Unmarshal(raw, &entity); err != nil {
		return "", errors.New("internal_error: undecodable catalog entity: " + err.Error())
	}
	docsPage, err := p.API.ListCatalogDocs(ctx, ws, remotestate.CatalogDocsQuery{EntityRef: ref, Limit: resourceListLimit})
	if err != nil {
		return "", errors.New(errText(err))
	}
	var docs []catalogDocRow
	_ = json.Unmarshal(listUnder(docsPage.Data, "docs", "items"), &docs)
	overview, hasOverview := "", false
	for _, d := range docs {
		if d.Role == "overview" {
			body, err := p.API.ReadCatalogDoc(ctx, ws, d.Digest)
			if err != nil {
				return "", errors.New(errText(err))
			}
			overview, hasOverview = string(body), true
			break
		}
	}
	return entityMarkdown(entity, docs, overview, hasOverview), nil
}

// catalogEntityRow / catalogDocRow are the fields the markdown rendering
// reads (contracts/state.ts OrgCatalogEntity / CatalogDoc; nulls decode to
// zero values and render as the TS fallbacks).
type catalogEntityRow struct {
	EntityRef   string `json:"entityRef"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Owner       string `json:"owner"`
	Lifecycle   string `json:"lifecycle"`
	Description string `json:"description"`
	System      string `json:"system"`
	Relations   []struct {
		Type      string `json:"type"`
		TargetRef string `json:"targetRef"`
	} `json:"relations"`
	SourceProjectID   string `json:"sourceProjectId"`
	SourceEnvironment string `json:"sourceEnvironment"`
	SourceCommit      string `json:"sourceCommit"`
	HeadDigest        string `json:"headDigest"`
}

type catalogDocRow struct {
	DocKey string `json:"docKey"`
	Title  string `json:"title"`
	Role   string `json:"role"`
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

func entityMarkdown(e catalogEntityRow, docs []catalogDocRow, overview string, hasOverview bool) string {
	lines := []string{
		"# " + e.EntityRef + " — " + e.Name,
		"",
		"- **Kind:** " + e.Kind,
		"- **Owner:** " + orDash(e.Owner),
		"- **Lifecycle:** " + orDash(e.Lifecycle),
	}
	if e.Description != "" {
		lines = append(lines, "- **Description:** "+e.Description)
	}
	if e.System != "" {
		lines = append(lines, "- **System:** "+e.System)
	}
	lines = append(lines, "", "## Relations")
	if len(e.Relations) == 0 {
		lines = append(lines, "None declared.")
	} else {
		for _, rel := range e.Relations {
			lines = append(lines, "- "+rel.Type+" → `"+rel.TargetRef+"`")
		}
	}
	env, commit := e.SourceEnvironment, e.SourceCommit
	if env == "" {
		env = "project-wide"
	}
	if commit == "" {
		commit = "unknown"
	}
	lines = append(lines,
		"",
		"## Provenance",
		"- Project `"+e.SourceProjectID+"` · environment "+env+" · commit "+commit,
		"- Snapshot `"+e.HeadDigest+"`",
		"",
		"## Docs",
	)
	if len(docs) == 0 {
		lines = append(lines, "No catalog docs.")
	} else {
		lines = append(lines, "| Key | Title | Role | Path | Digest |", "|---|---|---|---|---|")
		for _, d := range docs {
			lines = append(lines, "| "+d.DocKey+" | "+d.Title+" | "+d.Role+" | `"+d.Path+"` | `"+d.Digest+"` |")
		}
	}
	if hasOverview {
		// Same byte cap as catalog_read_doc — an oversized doc gets the
		// explicit truncation marker, never a silent cut.
		lines = append(lines, "", "## Overview", "", truncate(overview))
	}
	return strings.Join(lines, "\n")
}

// ── runs://{workspace}/{project}/{runId} ────────────────────

func (p *Provider) readRunSummary(ctx context.Context, rest string) (string, error) {
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", errors.New("validation_failed: uri must match runs://{workspace}/{project}/{runId}")
	}
	ws, project, runID := parts[0], parts[1], parts[2]
	runPage, err := p.API.GetPlatformRun(ctx, ws, project, runID)
	if err != nil {
		return "", errors.New(errText(err))
	}
	jobsPage, err := p.API.ListPlatformRunJobs(ctx, ws, project, runID)
	if err != nil {
		return "", errors.New(errText(err))
	}
	var run runRow
	if err := json.Unmarshal(unwrapKey(runPage.Data, "run"), &run); err != nil {
		return "", errors.New("internal_error: undecodable run: " + err.Error())
	}
	var jobs []jobRow
	_ = json.Unmarshal(listUnder(jobsPage.Data, "jobs", "items"), &jobs)
	return runMarkdown(run, jobs), nil
}

// runRow / jobRow are the fields the markdown rendering reads
// (contracts/state.ts Run / RunJob).
type runRow struct {
	RunID       string `json:"runId"`
	ProjectID   string `json:"projectId"`
	Environment string `json:"environment"`
	Status      string `json:"status"`
	Source      string `json:"source"`
	Git         struct {
		Ref    string `json:"ref"`
		Commit string `json:"commit"`
		Dirty  bool   `json:"dirty"`
	} `json:"git"`
	CreatedBy struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
	} `json:"createdBy"`
	CreatedAt  string `json:"createdAt"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt"`
	JobCounts  struct {
		Queued    int `json:"queued"`
		Running   int `json:"running"`
		Succeeded int `json:"succeeded"`
		Failed    int `json:"failed"`
	} `json:"jobCounts"`
}

type jobRow struct {
	JobID     string `json:"jobId"`
	Status    string `json:"status"`
	Attempt   int    `json:"attempt"`
	Component string `json:"component"`
	ErrorText string `json:"errorText"`
}

func runMarkdown(run runRow, jobs []jobRow) string {
	dirty := ""
	if run.Git.Dirty {
		dirty = " (dirty)"
	}
	creator := run.CreatedBy.DisplayName
	if creator == "" {
		creator = run.CreatedBy.ID
	}
	c := run.JobCounts
	lines := []string{
		"# Run " + run.RunID + " — " + run.Status,
		"",
		"- **Project:** `" + run.ProjectID + "` · **Environment:** " + orDash(run.Environment),
		"- **Source:** " + run.Source + " · `" + run.Git.Ref + "` @ `" + run.Git.Commit + "`" + dirty,
		"- **Created:** " + run.CreatedAt + " by " + creator,
		"- **Started:** " + orDash(run.StartedAt) + " · **Finished:** " + orDash(run.FinishedAt),
		fmt.Sprintf("- **Job counts:** %d queued · %d running · %d succeeded · %d failed", c.Queued, c.Running, c.Succeeded, c.Failed),
		"",
		"## Jobs",
	}
	if len(jobs) == 0 {
		lines = append(lines, "No jobs recorded.")
	} else {
		lines = append(lines, "| Job | Status | Attempt | Component | Error |", "|---|---|---|---|---|")
		for _, j := range jobs {
			lines = append(lines, fmt.Sprintf("| %s | %s | %d | %s | %s |", j.JobID, j.Status, j.Attempt, orDash(j.Component), orDash(j.ErrorText)))
		}
	}
	return strings.Join(lines, "\n")
}

// orDash is the TS renderings' `?? "—"` fallback for nullable strings.
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// unwrapKey returns data[key] when data is an object carrying it (the wire's
// {data: {run: {…}}} envelope), else data itself.
func unwrapKey(data json.RawMessage, key string) json.RawMessage {
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) == nil {
		if raw, ok := obj[key]; ok {
			return raw
		}
	}
	return data
}

// listUnder returns a page data's item array: the data itself when it is one,
// else the first of the given keys present ({docs: […]} / {jobs: […]}).
func listUnder(data json.RawMessage, keys ...string) json.RawMessage {
	var probe []json.RawMessage
	if json.Unmarshal(data, &probe) == nil {
		return data
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) == nil {
		for _, k := range keys {
			if raw, ok := obj[k]; ok {
				return raw
			}
		}
	}
	return nil
}
