// Package configsurface is the CLI client for the platform's org/project/
// environment-scoped config surface (…/config/secrets — specs/orun-secrets/
// data-model.md §8). It is deliberately separate from internal/remotestate:
// that client is state-plane-scoped (…/projects/{p}/state/…), while this
// surface hangs config routes off each rung of the tenancy chain.
//
// Invariant: no request or response on this surface ever carries a secret
// value back to the caller, and no error produced here ever embeds the value
// being written. Request bodies are marshalled and sent, never echoed.
package configsurface

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/remotestate"
)

const (
	defaultTimeout   = 30 * time.Second
	maxRetryAttempts = 3
	retryBaseDelay   = 500 * time.Millisecond

	// ImportChunkSize is the server's per-request cap on dotenv import items.
	ImportChunkSize = 100
)

// ScopeKind names a rung of the settings/secrets chain.
type ScopeKind string

const (
	ScopeWorkspace   ScopeKind = "workspace"
	ScopeProject     ScopeKind = "project"
	ScopeEnvironment ScopeKind = "environment"
)

// Scope identifies the chain rung a secrets call targets. Org is required
// always; Project for project/environment scope; EnvID (the public env_… id,
// never the slug) for environment scope.
type Scope struct {
	Kind    ScopeKind
	Org     string
	Project string
	EnvID   string
}

// secretsPath builds the scoped …/config/secrets base path for this rung.
func (s Scope) secretsPath() (string, error) {
	if strings.TrimSpace(s.Org) == "" {
		return "", fmt.Errorf("configsurface: scope is missing the organization")
	}
	base := "/v1/organizations/" + urlSegment(s.Org)
	switch s.Kind {
	case ScopeWorkspace:
		return base + "/config/secrets", nil
	case ScopeProject, ScopeEnvironment:
		if strings.TrimSpace(s.Project) == "" {
			return "", fmt.Errorf("configsurface: %s scope is missing the project", s.Kind)
		}
		base += "/projects/" + urlSegment(s.Project)
		if s.Kind == ScopeProject {
			return base + "/config/secrets", nil
		}
		if strings.TrimSpace(s.EnvID) == "" {
			return "", fmt.Errorf("configsurface: environment scope is missing the environment id")
		}
		return base + "/environments/" + urlSegment(s.EnvID) + "/config/secrets", nil
	default:
		return "", fmt.Errorf("configsurface: unknown scope kind %q", string(s.Kind))
	}
}

// secretPoliciesPath builds the scoped …/config/secret-policies base path. The
// SecretPolicy routes are org- or project-scoped only (data-model.md §7d): a
// document's tenancy scope is where it is pushed, so environment scope is not a
// valid target here.
func (s Scope) secretPoliciesPath() (string, error) {
	if strings.TrimSpace(s.Org) == "" {
		return "", fmt.Errorf("configsurface: scope is missing the organization")
	}
	base := "/v1/organizations/" + urlSegment(s.Org)
	switch s.Kind {
	case ScopeWorkspace:
		return base + "/config/secret-policies", nil
	case ScopeProject:
		if strings.TrimSpace(s.Project) == "" {
			return "", fmt.Errorf("configsurface: project scope is missing the project")
		}
		return base + "/projects/" + urlSegment(s.Project) + "/config/secret-policies", nil
	default:
		return "", fmt.Errorf("configsurface: secret-policies scope must be workspace or project, got %q", string(s.Kind))
	}
}

// APIError is the decoded platform error envelope
// ({ error: { code, message, requestId } }).
type APIError struct {
	Message   string
	Code      string
	RequestID string
	Status    int
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = fmt.Sprintf("server returned status %d", e.Status)
	}
	if e.Code != "" {
		msg = fmt.Sprintf("%s (code: %s)", msg, e.Code)
	}
	if e.RequestID != "" {
		msg = fmt.Sprintf("%s [requestId: %s]", msg, e.RequestID)
	}
	return msg
}

// IsLocked reports whether the write was rejected because the key is served
// by a non-overridable higher rung (409 — SD-12′) or otherwise conflicts.
func (e *APIError) IsLocked() bool { return e.Status == http.StatusConflict }

// IsNotFound reports a 404 (which, per resource-hiding, also covers
// "not authorized to see it").
func (e *APIError) IsNotFound() bool { return e.Status == http.StatusNotFound }

// SecretMeta is the metadata-only projection of a secret. There is no value
// field — structurally.
type SecretMeta struct {
	ID             string `json:"id"`
	SecretKey      string `json:"secretKey"`
	DisplayName    string `json:"displayName,omitempty"`
	Scope          string `json:"scope,omitempty"`
	ScopeKind      string `json:"scopeKind,omitempty"`
	Version        int    `json:"version,omitempty"`
	Status         string `json:"status,omitempty"`
	RotationPolicy string `json:"rotationPolicy,omitempty"`
	Personal       bool   `json:"personal,omitempty"`
	Overridable    *bool  `json:"overridable,omitempty"`
	ServesFrom     string `json:"servesFrom,omitempty"`
	LastRotatedAt  string `json:"lastRotatedAt,omitempty"`
	LastUsedAt     string `json:"lastUsedAt,omitempty"`
	CreatedAt      string `json:"createdAt,omitempty"`
	// Source distinguishes a stored value ("static") from a brokered pointer
	// ("brokered"), whose value is minted just-in-time from an integration
	// connection (saas-integration-hub IH7).
	Source string `json:"source,omitempty"`
	// BindingStatus / Orphaned are the derived orphan-health projection the
	// config surface stamps onto a brokered row from live connection health
	// (brokered-orphan-safety, Feature 1). Absent on static rows and left unset
	// when the health lookup was unreachable (health unknown, NOT orphaned).
	BindingStatus string `json:"bindingStatus,omitempty"`
	Orphaned      *bool  `json:"orphaned,omitempty"`
	// Rotation is the provider-rotation producer provenance (provider-rotated-
	// secrets RS4): present when the stored value is re-minted from a connected
	// parent on the rotation schedule. Display-only — never params, never a value.
	Rotation *SecretRotationInfo `json:"rotation,omitempty"`
}

// SecretRotationInfo is the display projection of a rotated secret's producer.
type SecretRotationInfo struct {
	Provider      string `json:"provider"`
	ConnectionID  string `json:"connectionId"`
	Template      string `json:"template"`
	GraceSeconds  *int   `json:"graceSeconds,omitempty"`
	DeliverTarget string `json:"deliverTarget,omitempty"`
}

// EffectiveScope returns the serving scope name, tolerating either the
// `scope` or `scopeKind` field spelling in responses.
func (m SecretMeta) EffectiveScope() string {
	if m.Scope != "" {
		return m.Scope
	}
	return m.ScopeKind
}

// Locked reports whether the row is explicitly non-overridable.
func (m SecretMeta) Locked() bool {
	return m.Overridable != nil && !*m.Overridable
}

// Brokered reports whether the row is a brokered pointer (value minted at
// resolve time) rather than a stored value.
func (m SecretMeta) Brokered() bool {
	return m.Source == "brokered"
}

// IsOrphaned reports whether the config surface flagged this brokered row as
// orphaned — its integration connection is no longer active, so it can no
// longer mint at plan/run time. Static rows and rows whose health could not be
// determined (Orphaned unset) report false.
func (m SecretMeta) IsOrphaned() bool {
	return m.Orphaned != nil && *m.Orphaned
}

// ActorName decodes a createdBy field that may be a bare id string or a
// projected actor object ({ id, displayName }).
type ActorName string

// UnmarshalJSON accepts either a JSON string or an actor object.
func (a *ActorName) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*a = ActorName(s)
		return nil
	}
	var obj struct {
		DisplayName string `json:"displayName"`
		ID          string `json:"id"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	if obj.DisplayName != "" {
		*a = ActorName(obj.DisplayName)
	} else {
		*a = ActorName(obj.ID)
	}
	return nil
}

// SecretVersion is one row of a secret's version-metadata history.
type SecretVersion struct {
	Version   int       `json:"version"`
	Status    string    `json:"status"`
	CreatedAt string    `json:"createdAt"`
	CreatedBy ActorName `json:"createdBy"`
}

// CreateSecretRequest is the body for POST …/config/secrets. Value is
// write-only: it is sent and never surfaced again by this package. Exactly one
// of Value, Binding, or Rotation is set: a brokered create (IH7) and a rotated
// create (provider-rotated-secrets RS1) carry NO value — the value is minted
// from the connected parent (at resolve time for brokered; once + on schedule
// for rotated).
type CreateSecretRequest struct {
	SecretKey      string `json:"secretKey"`
	Value          string `json:"value,omitempty"`
	DisplayName    string `json:"displayName,omitempty"`
	RotationPolicy string `json:"rotationPolicy,omitempty"`
	Personal       bool   `json:"personal,omitempty"`
	Overridable    *bool  `json:"overridable,omitempty"`
	Binding        *SecretBrokerBinding   `json:"binding,omitempty"`
	Rotation       *SecretRotationBinding `json:"rotation,omitempty"`
}

// SecretBrokerBinding is the brokered-pointer half of a brokered create (IH7,
// contracts CreateBrokeredSecretRequest): the value is minted just-in-time
// from the connection at resolve, never stored. Never carries credential
// material.
type SecretBrokerBinding struct {
	// ConnectionID is the integration connection public id (int_<32 hex>).
	ConnectionID string `json:"connectionId"`
	// Template is the broker scope template (e.g. "workers-deploy").
	Template string `json:"template"`
	// Params are the template params, validated server-side against the
	// template's declared param names.
	Params map[string]string `json:"params,omitempty"`
}

// SecretRotationBinding is the provider-rotation producer half of a rotated
// create (RS1): which connected parent mints the value, under which broker
// scope template. Never carries credential material.
type SecretRotationBinding struct {
	// ConnectionID is the integration connection public id (int_<32 hex>).
	ConnectionID string `json:"connectionId"`
	// Template is the broker scope template (e.g. "workers-deploy").
	Template string `json:"template"`
	// Params are the template params, validated server-side against the
	// template's declared param names.
	Params map[string]string `json:"params,omitempty"`
	// GraceSeconds is the overlap the prior token stays valid after a rotation.
	// Nil = the server default (24h).
	GraceSeconds *int `json:"graceSeconds,omitempty"`
	// DeliverTarget is the materialize target re-delivered on rotation, for a
	// long-lived consumer that HOLDS the value. Empty = per-run consumers only.
	DeliverTarget string `json:"deliverTarget,omitempty"`
}

// ── Secrets capabilities (saas-secrets-platform SP0c/SP-A1, read by SP5) ──────

// ScopeTemplate is one named, versioned credential scope a provider can mint
// against (contracts IntegrationScopeTemplate). Safe descriptor — never a
// credential.
type ScopeTemplate struct {
	ID            string   `json:"id"`
	Provider      string   `json:"provider"`
	Version       int      `json:"version"`
	DisplayName   string   `json:"displayName"`
	Description   string   `json:"description"`
	Params        []string `json:"params"`
	MaxTTLSeconds int      `json:"maxTtlSeconds"`
	CustodyKind   string   `json:"custodyKind,omitempty"`
	// Origin is where the template is authored: "declared" (adapter code
	// catalog) or "custom" (org-curated, SP4). Empty means declared.
	Origin       string `json:"origin,omitempty"`
	BaseTemplate string `json:"baseTemplate,omitempty"`
	// Status soft-retires a template from create surfaces while existing
	// bindings keep resolving (SP-A6). Empty means active.
	Status string `json:"status,omitempty"`
}

// Active reports whether the template may back a NEW create: a retired
// template stays resolvable for bound secrets but disappears from authoring
// (SP-A6). Absent status means active.
func (t ScopeTemplate) Active() bool { return t.Status != "retired" }

// SecretsCapability is one provider's DESCRIBE declaration (contracts
// ProviderSecretsCapability): its template catalog, the secret modes its mint
// can back, and the materialize targets a rotated value can be delivered into.
// The CLI derives all authoring validation from this — it carries no catalog
// of its own.
type SecretsCapability struct {
	Provider        string          `json:"provider"`
	ScopeTemplates  []ScopeTemplate `json:"scopeTemplates"`
	SupportedModes  []string        `json:"supportedModes"`
	DeliveryTargets []string        `json:"deliveryTargets"`
	Authoring       string          `json:"authoring,omitempty"`
}

// ListSecretsCapabilities calls the org-scoped bulk capability read
// (GET /v1/organizations/{org}/integrations/secrets-capabilities, SP-A1):
// every capability-declaring provider in one response. Pure metadata.
func (c *Client) ListSecretsCapabilities(ctx context.Context, org string) ([]SecretsCapability, error) {
	if strings.TrimSpace(org) == "" {
		return nil, fmt.Errorf("configsurface: capability listing needs an organization")
	}
	path := "/v1/organizations/" + urlSegment(org) + "/integrations/secrets-capabilities"
	var payload struct {
		Capabilities []SecretsCapability `json:"capabilities"`
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &payload, true); err != nil {
		return nil, fmt.Errorf("list secrets capabilities: %w", err)
	}
	return payload.Capabilities, nil
}

// ImportSecret is one item of a dotenv bulk import.
type ImportSecret struct {
	SecretKey   string `json:"secretKey"`
	Value       string `json:"value"`
	DisplayName string `json:"displayName,omitempty"`
}

// ImportResult is the per-key outcome of an import.
type ImportResult struct {
	SecretKey string `json:"secretKey"`
	Status    string `json:"status"` // created | conflict | …
}

// Environment is one row of the project's environment list, used for
// slug → env_<id> resolution.
type Environment struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name,omitempty"`
}

// Client speaks the config surface. It reuses the remotestate TokenSource so
// auth resolution (session refresh, OIDC exchange) stays in one place.
type Client struct {
	baseURL    string
	tokenSrc   remotestate.TokenSource
	userAgent  string
	httpClient *http.Client
	// envCache caches environment slug→id per (org, project) for the lifetime
	// of one CLI invocation.
	envCache map[string]map[string]string
}

// NewClient creates a config-surface client for baseURL.
func NewClient(baseURL, version string, tokenSrc remotestate.TokenSource) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		tokenSrc:   tokenSrc,
		userAgent:  "orun-cli/" + version,
		httpClient: &http.Client{Timeout: defaultTimeout},
		envCache:   map[string]map[string]string{},
	}
}

// ListSecrets calls GET <scope>/config/secrets (metadata only). With chain
// true (environment scope) the ?chain=true variant adds servesFrom and
// overridable per key. The second return is the raw items array for --json.
func (c *Client) ListSecrets(ctx context.Context, scope Scope, chain bool) ([]SecretMeta, json.RawMessage, error) {
	path, err := scope.secretsPath()
	if err != nil {
		return nil, nil, err
	}
	if chain {
		path += "?chain=true"
	}
	var body json.RawMessage
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &body, true); err != nil {
		return nil, nil, fmt.Errorf("list secrets: %w", err)
	}
	items, raw, err := decodeItems(body, "secrets")
	if err != nil {
		return nil, nil, fmt.Errorf("list secrets: decoding response: %w", err)
	}
	var out []SecretMeta
	if err := json.Unmarshal(items, &out); err != nil {
		return nil, nil, fmt.Errorf("list secrets: decoding response: %w", err)
	}
	return out, raw, nil
}

// CreateSecret calls POST <scope>/config/secrets. The response is metadata
// only; a locked-key write surfaces as a 409 *APIError (IsLocked).
func (c *Client) CreateSecret(ctx context.Context, scope Scope, req CreateSecretRequest) (*SecretMeta, error) {
	path, err := scope.secretsPath()
	if err != nil {
		return nil, err
	}
	var meta secretEnvelope
	if err := c.doJSON(ctx, http.MethodPost, path, req, &meta, false); err != nil {
		return nil, fmt.Errorf("set secret %s: %w", req.SecretKey, err)
	}
	return meta.unwrap(), nil
}

// RotateSecret calls POST <scope>/config/secrets/{id}/rotate, appending a
// version and bumping the head.
func (c *Client) RotateSecret(ctx context.Context, scope Scope, id, value string) (*SecretMeta, error) {
	path, err := scope.secretsPath()
	if err != nil {
		return nil, err
	}
	// Value is omitted when empty: an empty-body rotate is the metadata-only
	// bump for a static head and the re-mint for a provider-rotated head (the
	// server dispatches on the head, RS3) — sending `"value":""` would 422.
	body := struct {
		Value string `json:"value,omitempty"`
	}{Value: value}
	var meta secretEnvelope
	if err := c.doJSON(ctx, http.MethodPost, path+"/"+urlSegment(id)+"/rotate", body, &meta, false); err != nil {
		return nil, fmt.Errorf("rotate secret: %w", err)
	}
	return meta.unwrap(), nil
}

// DeleteSecret calls DELETE <scope>/config/secrets/{id} (revoke/tombstone).
func (c *Client) DeleteSecret(ctx context.Context, scope Scope, id string) error {
	path, err := scope.secretsPath()
	if err != nil {
		return err
	}
	if err := c.doJSON(ctx, http.MethodDelete, path+"/"+urlSegment(id), nil, nil, false); err != nil {
		return fmt.Errorf("revoke secret: %w", err)
	}
	return nil
}

// ListVersions calls GET <scope>/config/secrets/{id}/versions.
func (c *Client) ListVersions(ctx context.Context, scope Scope, id string) ([]SecretVersion, error) {
	path, err := scope.secretsPath()
	if err != nil {
		return nil, err
	}
	var body json.RawMessage
	if err := c.doJSON(ctx, http.MethodGet, path+"/"+urlSegment(id)+"/versions", nil, &body, true); err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	items, _, err := decodeItems(body, "versions")
	if err != nil {
		return nil, fmt.Errorf("list versions: decoding response: %w", err)
	}
	var out []SecretVersion
	if err := json.Unmarshal(items, &out); err != nil {
		return nil, fmt.Errorf("list versions: decoding response: %w", err)
	}
	return out, nil
}

// RevealSecret calls POST <scope>/config/secrets/{id}/reveal — the single
// audited, elevated break-glass path that returns a plaintext value (SD-3).
// The reason is mandatory and recorded server-side; the returned value is
// printed to the caller and never persisted. This is the ONLY method on this
// client that returns a secret value.
func (c *Client) RevealSecret(ctx context.Context, scope Scope, id, reason string) (*RevealedSecret, error) {
	path, err := scope.secretsPath()
	if err != nil {
		return nil, err
	}
	body := struct {
		Reason string `json:"reason"`
	}{Reason: reason}
	// doJSON unwraps the platform {data:…} envelope, so decode the inner payload.
	var payload struct {
		Secret RevealedSecret `json:"secret"`
	}
	if err := c.doJSON(ctx, http.MethodPost, path+"/"+urlSegment(id)+"/reveal", body, &payload, false); err != nil {
		return nil, fmt.Errorf("reveal secret: %w", err)
	}
	return &payload.Secret, nil
}

// RevealedSecret is the break-glass response: the plaintext value plus which
// version served it. Held only transiently by the CLI to print, never stored.
type RevealedSecret struct {
	Value   string `json:"value"`
	Version int    `json:"version"`
}

// ImportSecrets calls POST <scope>/config/secrets/import, chunking the items
// into batches of ≤ ImportChunkSize. On a chunk error the results gathered so
// far are returned alongside the error so the caller can render a partial
// summary. When the server response carries no per-key results, the sent keys
// are reported as "created".
func (c *Client) ImportSecrets(ctx context.Context, scope Scope, items []ImportSecret) ([]ImportResult, error) {
	path, err := scope.secretsPath()
	if err != nil {
		return nil, err
	}
	var results []ImportResult
	for start := 0; start < len(items); start += ImportChunkSize {
		end := start + ImportChunkSize
		if end > len(items) {
			end = len(items)
		}
		chunk := items[start:end]
		body := struct {
			Secrets []ImportSecret `json:"secrets"`
		}{Secrets: chunk}
		var resp struct {
			Results []ImportResult `json:"results"`
			Secrets []ImportResult `json:"secrets"`
			Items   []ImportResult `json:"items"`
		}
		if err := c.doJSON(ctx, http.MethodPost, path+"/import", body, &resp, false); err != nil {
			return results, fmt.Errorf("import secrets: %w", err)
		}
		chunkResults := resp.Results
		if len(chunkResults) == 0 {
			chunkResults = resp.Secrets
		}
		if len(chunkResults) == 0 {
			chunkResults = resp.Items
		}
		if len(chunkResults) == 0 {
			for _, item := range chunk {
				chunkResults = append(chunkResults, ImportResult{SecretKey: item.SecretKey, Status: "created"})
			}
		}
		results = append(results, chunkResults...)
	}
	return results, nil
}

// ListEnvironments calls GET /v1/organizations/{org}/projects/{prj}/environments.
func (c *Client) ListEnvironments(ctx context.Context, org, project string) ([]Environment, error) {
	if strings.TrimSpace(org) == "" || strings.TrimSpace(project) == "" {
		return nil, fmt.Errorf("configsurface: environment listing needs an organization and project")
	}
	path := "/v1/organizations/" + urlSegment(org) + "/projects/" + urlSegment(project) + "/environments"
	var body json.RawMessage
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &body, true); err != nil {
		return nil, fmt.Errorf("list environments: %w", err)
	}
	items, _, err := decodeItems(body, "environments")
	if err != nil {
		return nil, fmt.Errorf("list environments: decoding response: %w", err)
	}
	var out []Environment
	if err := json.Unmarshal(items, &out); err != nil {
		return nil, fmt.Errorf("list environments: decoding response: %w", err)
	}
	return out, nil
}

// ResolveEnvironmentID resolves an environment slug to its public env_… id
// via the environments list, caching per (org, project) for this invocation.
// A value that already looks like a public id (env_…) passes through.
func (c *Client) ResolveEnvironmentID(ctx context.Context, org, project, env string) (string, error) {
	env = strings.TrimSpace(env)
	if env == "" {
		return "", fmt.Errorf("configsurface: empty environment")
	}
	if strings.HasPrefix(env, "env_") {
		return env, nil
	}
	cacheKey := org + "/" + project
	if byslug, ok := c.envCache[cacheKey]; ok {
		if id, ok := byslug[strings.ToLower(env)]; ok {
			return id, nil
		}
		return "", c.unknownEnvError(env, byslug)
	}
	envs, err := c.ListEnvironments(ctx, org, project)
	if err != nil {
		return "", err
	}
	byslug := make(map[string]string, len(envs))
	for _, e := range envs {
		if e.Slug != "" && e.ID != "" {
			byslug[strings.ToLower(e.Slug)] = e.ID
		}
	}
	c.envCache[cacheKey] = byslug
	if id, ok := byslug[strings.ToLower(env)]; ok {
		return id, nil
	}
	return "", c.unknownEnvError(env, byslug)
}

func (c *Client) unknownEnvError(env string, byslug map[string]string) error {
	if len(byslug) == 0 {
		return fmt.Errorf("environment %q not found: the linked project declares no environments", env)
	}
	slugs := make([]string, 0, len(byslug))
	for s := range byslug {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	return fmt.Errorf("environment %q not found; available: %s", env, strings.Join(slugs, ", "))
}

// ── Materialization provenance (SD-13 — data-model.md §7e/§8) ─────────────────

// RecordSyncRequest is the body for POST …/config/secrets/syncs (the SM5
// route). It is value-free: the secret key, the version delivered, the target
// adapter id, the provisioned entity ref, and the deploy run id. No value is
// ever sent on this surface — the value was written directly to the target.
type RecordSyncRequest struct {
	SecretKey string `json:"secretKey"`
	Version   int    `json:"version,omitempty"`
	Target    string `json:"target"`
	EntityRef string `json:"entityRef,omitempty"`
	RunID     string `json:"runId,omitempty"`
}

// RecordSync calls POST <scope>/config/secrets/syncs to stamp a materialization
// row (Invariant 10). Best-effort at the call site: the value is already
// written to the target, so a record failure is surfaced but does not undo it.
func (c *Client) RecordSync(ctx context.Context, scope Scope, req RecordSyncRequest) error {
	path, err := scope.secretsPath()
	if err != nil {
		return err
	}
	if err := c.doJSON(ctx, http.MethodPost, path+"/syncs", req, nil, false); err != nil {
		return fmt.Errorf("record secret sync %s: %w", req.SecretKey, err)
	}
	return nil
}

// ── SecretPolicy surface (Layer-2 documents — data-model.md §7d/§8) ───────────

// PutSecretPolicyRequest is the body for PUT …/config/secret-policies. Document
// is the validated SecretPolicy spec ({ "rules": [...] }); the push is
// idempotent by the server's content hash of it.
type PutSecretPolicyRequest struct {
	Name     string          `json:"name"`
	Tier     string          `json:"tier"`
	Source   string          `json:"source"`
	Document json.RawMessage `json:"document"`
}

// SecretPolicyResult is the metadata the server returns after a policy push.
type SecretPolicyResult struct {
	Name         string `json:"name"`
	Tier         string `json:"tier"`
	Source       string `json:"source"`
	Scope        string `json:"scope"`
	DocumentHash string `json:"documentHash"`
	Updated      bool   `json:"updated"`
}

// EvalSubject is the who-axis of an evaluate request (subject facts).
type EvalSubject struct {
	ID    string   `json:"id,omitempty"`
	Kind  string   `json:"kind,omitempty"`
	Teams []string `json:"teams,omitempty"`
}

// EvalComponent is the what-axis of an evaluate request.
type EvalComponent struct {
	Type   string `json:"type,omitempty"`
	Domain string `json:"domain,omitempty"`
	Name   string `json:"name,omitempty"`
}

// EvalTrigger is the where/trigger-axis of an evaluate request.
type EvalTrigger struct {
	Event      string `json:"event,omitempty"`
	Action     string `json:"action,omitempty"`
	Branch     string `json:"branch,omitempty"`
	BaseBranch string `json:"baseBranch,omitempty"`
	Tag        string `json:"tag,omitempty"`
	Declared   bool   `json:"declared,omitempty"`
	Actor      string `json:"actor,omitempty"`
	Repository string `json:"repository,omitempty"`
}

// EvaluateSecretPolicyRequest is the body for POST …/config/secret-policies/
// evaluate — the four-axis facts for a hypothetical resolve. The field shape
// matches the config-worker evaluate handler (flat, not nested under `facts`).
type EvaluateSecretPolicyRequest struct {
	Key        string         `json:"key"`
	Env        string         `json:"env"`
	Platform   string         `json:"platform"`
	Subject    EvalSubject    `json:"subject"`
	Component  *EvalComponent `json:"component,omitempty"`
	Trigger    *EvalTrigger   `json:"trigger,omitempty"`
	ServesFrom string         `json:"servesFrom,omitempty"`
}

// LayerDecision is one layer's outcome in an evaluate response.
type LayerDecision struct {
	Action string `json:"action,omitempty"`
	Allow  bool   `json:"allow"`
	RuleID string `json:"ruleId,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// EvaluateResult is the two-layer decision the evaluate route returns.
type EvaluateResult struct {
	Layer1   LayerDecision `json:"layer1"`
	Layer2   LayerDecision `json:"layer2"`
	Decision struct {
		Allow bool `json:"allow"`
	} `json:"decision"`
}

// PutSecretPolicy calls PUT <scope>/config/secret-policies. The push is
// idempotent by document hash; Updated reports whether the stored document
// actually changed. Layer-1 requires secret.write.
func (c *Client) PutSecretPolicy(ctx context.Context, scope Scope, req PutSecretPolicyRequest) (*SecretPolicyResult, error) {
	path, err := scope.secretPoliciesPath()
	if err != nil {
		return nil, err
	}
	var resp struct {
		Policy *SecretPolicyResult `json:"policy"`
		SecretPolicyResult
	}
	if err := c.doJSON(ctx, http.MethodPut, path, req, &resp, false); err != nil {
		return nil, fmt.Errorf("push secret policy %s: %w", req.Name, err)
	}
	if resp.Policy != nil {
		return resp.Policy, nil
	}
	out := resp.SecretPolicyResult
	return &out, nil
}

// EvaluateSecretPolicy calls POST <scope>/config/secret-policies/evaluate — a
// dry-run reporting both the Layer-1 role decision and the Layer-2 rule
// decision. It never serves a secret value.
func (c *Client) EvaluateSecretPolicy(ctx context.Context, scope Scope, req EvaluateSecretPolicyRequest) (*EvaluateResult, error) {
	path, err := scope.secretPoliciesPath()
	if err != nil {
		return nil, err
	}
	var out EvaluateResult
	if err := c.doJSON(ctx, http.MethodPost, path+"/evaluate", req, &out, false); err != nil {
		return nil, fmt.Errorf("evaluate secret policy: %w", err)
	}
	return &out, nil
}

// ── internal helpers ─────────────────────────────────────────────────────────

// secretEnvelope tolerates both a bare SecretMeta body and one nested under a
// "secret" key.
type secretEnvelope struct {
	SecretMeta
	Secret *SecretMeta `json:"secret"`
}

func (e *secretEnvelope) unwrap() *SecretMeta {
	if e.Secret != nil {
		return e.Secret
	}
	m := e.SecretMeta
	return &m
}

// decodeItems extracts the items array from a (data-unwrapped) list body: a
// bare array, or an object keyed by `key` or "items". Returns the raw array
// (for --json passthrough) alongside itself.
func decodeItems(body json.RawMessage, key string) (json.RawMessage, json.RawMessage, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return json.RawMessage("[]"), json.RawMessage("[]"), nil
	}
	if trimmed[0] == '[' {
		return trimmed, trimmed, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, nil, err
	}
	for _, k := range []string{key, "items"} {
		if raw, ok := obj[k]; ok && len(bytes.TrimSpace(raw)) > 0 && bytes.TrimSpace(raw)[0] == '[' {
			return raw, raw, nil
		}
	}
	return json.RawMessage("[]"), json.RawMessage("[]"), nil
}

// doJSON executes one JSON call against the config surface. Only idempotent
// GETs opt into retries (retryable true); writes are sent once. The error path
// decodes the platform envelope into *APIError and never includes the request
// body.
func (c *Client) doJSON(ctx context.Context, method, path string, body, out interface{}, retryable bool) error {
	attempts := 1
	if retryable {
		attempts = maxRetryAttempts
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryBaseDelay * time.Duration(attempt)):
			}
		}
		err := c.doJSONOnce(ctx, method, path, body, out)
		if err == nil {
			return nil
		}
		if !isRetryableErr(err) {
			return err
		}
		lastErr = err
	}
	return lastErr
}

func (c *Client) doJSONOnce(ctx context.Context, method, path string, body, out interface{}) error {
	token, err := c.tokenSrc.Token(ctx)
	if err != nil {
		return fmt.Errorf("resolving auth token: %w", err)
	}

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			// Deliberately do not include the marshalling input in the error:
			// bodies on this surface carry secret values.
			return fmt.Errorf("encoding request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return parseErrorEnvelope(resp)
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		data, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("reading response: %w", readErr)
		}
		if err := decodeSuccessBody(data, out); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}
	return nil
}

// decodeSuccessBody decodes a 2xx body into out, unwrapping the platform
// success envelope ({ "data": <payload>, "meta": {…} }) when present.
func decodeSuccessBody(data []byte, out interface{}) error {
	if len(data) == 0 || out == nil {
		return nil
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &env) == nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, out)
	}
	return json.Unmarshal(data, out)
}

// parseErrorEnvelope decodes the platform error envelope
// ({ error: { code, message, requestId } }) into an *APIError. Unrecognized
// bodies degrade to a status-only error — the raw body is never echoed, so a
// misbehaving proxy can't reflect request content (values) into CLI output.
func parseErrorEnvelope(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	apiErr := &APIError{Status: resp.StatusCode}
	var nested struct {
		Error *struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"requestId"`
		} `json:"error"`
		RequestID string `json:"requestId"`
	}
	if json.Unmarshal(data, &nested) == nil && nested.Error != nil {
		apiErr.Code = nested.Error.Code
		apiErr.Message = nested.Error.Message
		apiErr.RequestID = nested.Error.RequestID
		if apiErr.RequestID == "" {
			apiErr.RequestID = nested.RequestID
		}
	}
	return apiErr
}

func isRetryableErr(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.Status >= 500 || apiErr.Status == http.StatusTooManyRequests
	}
	// Auth-resolution failures are terminal; network errors are retryable.
	return !strings.Contains(err.Error(), "resolving auth token")
}

// urlSegment escapes the characters that matter in a path segment.
func urlSegment(s string) string {
	s = strings.ReplaceAll(s, "/", "%2F")
	s = strings.ReplaceAll(s, ":", "%3A")
	s = strings.ReplaceAll(s, " ", "%20")
	return s
}
