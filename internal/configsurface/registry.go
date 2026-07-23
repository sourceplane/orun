package configsurface

// Integration Registry read (orun-integrations-cli ICL0, pairing the orun-cloud
// saas-integration-registry epic, IR0/IR7). The registry serves per-provider
// IntegrationDescriptors — the manifest projection the CLI renders its dynamic
// `orun integrations` verb trees from. The CLI carries no catalog: everything
// provider-shaped on this surface is server truth, cached for presentation only.
//
// The value-free invariant of this package extends here structurally: no type
// on this surface has a value/credential field, and every method is metadata
// in, metadata out.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// IntegrationDescriptor is one provider's registry projection (orun-cloud
// saas-integration-registry design §9). The wire spells the provider id as
// `id`; Provider is that id (with a tolerant fallback to a legacy `provider`
// spelling). Unknown fields are ignored — descriptors evolve additively.
type IntegrationDescriptor struct {
	Provider        string                     `json:"id"`
	DisplayName     string                     `json:"displayName"`
	Category        string                     `json:"category"`
	Tagline         string                     `json:"tagline,omitempty"`
	Connect         []IntegrationConnectMethod `json:"connect,omitempty"`
	MultiConnection bool                       `json:"multiConnection,omitempty"`
	Capabilities    []string                   `json:"capabilities"`
	// Connected is the org's live connection count for the provider, when the
	// registry projects it (entitled orgs only; absent renders as unknown).
	Connected   int               `json:"connectedCount,omitempty"`
	Space       *IntegrationSpace `json:"space,omitempty"`
	CLI         *CliNamespace     `json:"cli,omitempty"`
	Entitlement string            `json:"entitlement,omitempty"`
	Version     int               `json:"version,omitempty"`
	Status      string            `json:"status"` // live | dormant | roadmap
	Entitled    *bool             `json:"entitled,omitempty"`
}

// UnmarshalJSON decodes a descriptor, sourcing Provider from the wire `id`
// with a fallback to the legacy `provider` spelling.
func (d *IntegrationDescriptor) UnmarshalJSON(data []byte) error {
	type wire IntegrationDescriptor // no methods: avoids recursion
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	if w.Provider == "" {
		var alt struct {
			Provider string `json:"provider"`
		}
		if err := json.Unmarshal(data, &alt); err == nil {
			w.Provider = alt.Provider
		}
	}
	*d = IntegrationDescriptor(w)
	return nil
}

// Live reports whether the provider serves a working surface today. An absent
// status means live (additive-evolution rule); dormant/roadmap providers list
// with their status and render no verb tree.
func (d IntegrationDescriptor) Live() bool {
	return d.Status == "" || d.Status == "live"
}

// ConnectRecipe returns the first connect method's token recipe, if any — the
// air-gapped prep material the `recipe` extension prints.
func (d IntegrationDescriptor) ConnectRecipe() *ConnectRecipe {
	for i := range d.Connect {
		if d.Connect[i].Recipe != nil {
			return d.Connect[i].Recipe
		}
	}
	return nil
}

// IntegrationConnectMethod is one way a provider connects (oauth, token, …).
type IntegrationConnectMethod struct {
	Kind   string         `json:"kind"`
	Live   bool           `json:"live"`
	Recipe *ConnectRecipe `json:"recipe,omitempty"`
}

// ConnectRecipe is the human token-authoring recipe for a manual connect:
// what to create, why each permission is needed, and where to do it. Pure
// documentation — never a credential.
type ConnectRecipe struct {
	Intro string              `json:"intro"`
	Items []ConnectRecipeItem `json:"items"`
	Links []ConnectRecipeLink `json:"links"`
}

// ConnectRecipeItem is one permission/scope line of a connect recipe.
type ConnectRecipeItem struct {
	Name string `json:"name"`
	Why  string `json:"why"`
}

// ConnectRecipeLink is one reference link of a connect recipe.
type ConnectRecipeLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// IntegrationSpace is the console-space projection of a manifest (tabs,
// modules, authoring surface). The CLI carries it through the cache untouched;
// it renders nothing from it today.
type IntegrationSpace struct {
	Tabs      []string `json:"tabs,omitempty"`
	Modules   []string `json:"modules,omitempty"`
	Authoring string   `json:"authoring,omitempty"`
}

// CliNamespace is a provider's declarative CLI verb tree (design.md §1).
type CliNamespace struct {
	Verbs []CliVerb `json:"verbs"`
}

// CliVerb is one declared verb: its path under the provider namespace, its
// typed args, and the allowlisted operation it invokes. Descriptors are data,
// never capability — a verb can only select an operation this binary compiles
// in (the invoke allowlist); it can never name a URL, header, or local exec.
type CliVerb struct {
	Path            []string  `json:"path"` // e.g. ["secret","create"]
	Summary         string    `json:"summary"`
	Args            []CliArg  `json:"args"`
	Invoke          CliInvoke `json:"invoke"`
	NeedsConnection bool      `json:"needsConnection,omitempty"`
}

// CliArg is one typed positional or flag of a verb.
type CliArg struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"` // positional | flag
	Type     string   `json:"type"` // string | int | bool | enum | kv
	Enum     []string `json:"enum,omitempty"`
	Required bool     `json:"required,omitempty"`
	Repeat   bool     `json:"repeat,omitempty"`
	Help     string   `json:"help,omitempty"`
}

// CliInvoke maps a verb onto an allowlisted operation of an existing plane.
type CliInvoke struct {
	Plane string            `json:"plane"` // "config" | "integrations"
	Op    string            `json:"op"`    // allowlisted operation id
	Bind  map[string]string `json:"bind"`  // arg name -> request field
}

// IntegrationRegistryResult is the outcome of a conditional registry read.
type IntegrationRegistryResult struct {
	Registry []IntegrationDescriptor
	// ETag is the server's registry version tag (empty when not served).
	ETag string
	// NotModified reports a 304 against the caller's If-None-Match: the cached
	// registry is still current and Registry is empty.
	NotModified bool
}

// GetIntegrationRegistry calls the org-scoped registry read
// (GET /v1/organizations/{org}/integrations/registry): every provider's
// descriptor in one response. Pure metadata.
func (c *Client) GetIntegrationRegistry(ctx context.Context, org string) ([]IntegrationDescriptor, error) {
	res, err := c.GetIntegrationRegistryConditional(ctx, org, "")
	if err != nil {
		return nil, err
	}
	return res.Registry, nil
}

// GetIntegrationRegistryConditional is the ETag-aware registry read: with a
// non-empty ifNoneMatch the server may answer 304, reported as NotModified so
// the caller keeps its cache. GETs retry like every idempotent read here.
func (c *Client) GetIntegrationRegistryConditional(ctx context.Context, org, ifNoneMatch string) (*IntegrationRegistryResult, error) {
	if strings.TrimSpace(org) == "" {
		return nil, fmt.Errorf("configsurface: registry read needs an organization")
	}
	path := "/v1/organizations/" + urlSegment(org) + "/integrations/registry"
	var lastErr error
	for attempt := 0; attempt < maxRetryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryBaseDelay * time.Duration(attempt)):
			}
		}
		res, err := c.getRegistryOnce(ctx, path, ifNoneMatch)
		if err == nil {
			return res, nil
		}
		if !isRetryableErr(err) {
			return nil, fmt.Errorf("get integration registry: %w", err)
		}
		lastErr = err
	}
	return nil, fmt.Errorf("get integration registry: %w", lastErr)
}

// getRegistryOnce performs one conditional GET, decoding the standard
// {data:{registry:[…]}} envelope and capturing the response ETag.
func (c *Client) getRegistryOnce(ctx context.Context, path, ifNoneMatch string) (*IntegrationRegistryResult, error) {
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
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return &IntegrationRegistryResult{NotModified: true, ETag: ifNoneMatch}, nil
	}
	if resp.StatusCode >= 400 {
		return nil, parseErrorEnvelope(resp)
	}
	var payload struct {
		Registry []IntegrationDescriptor `json:"registry"`
	}
	if err := c.decodeBody(resp, &payload); err != nil {
		return nil, err
	}
	return &IntegrationRegistryResult{
		Registry: payload.Registry,
		ETag:     resp.Header.Get("ETag"),
	}, nil
}

// decodeBody reads a 2xx body into out through the platform success envelope.
func (c *Client) decodeBody(resp *http.Response, out interface{}) error {
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if err := decodeSuccessBody(data, out); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}

// ── Integration connections / credentials / sandboxes (ICL2 wire methods) ────
//
// These read/mutate the existing integrations plane. Everything is metadata:
// a connection row, a scope template, a minted-credential row — never a token
// or secret value.

// IntegrationConnection is the metadata projection of one provider connection.
// There is no credential field — structurally.
type IntegrationConnection struct {
	ID          string `json:"id"` // int_… public id
	Provider    string `json:"provider"`
	DisplayName string `json:"displayName,omitempty"`
	Status      string `json:"status,omitempty"` // active | revoked | error | expiring
	// Health is the derived connection-health axis when the server projects it
	// (ok | degraded | broken); absent means derive from Status.
	Health     string `json:"health,omitempty"`
	AuthKind   string `json:"authKind,omitempty"`
	CreatedAt  string `json:"createdAt,omitempty"`
	LastUsedAt string `json:"lastUsedAt,omitempty"`
	ExpiresAt  string `json:"expiresAt,omitempty"`
}

// MintedCredential is one row of a connection's minted-credential ledger
// (credential-broker capability). Metadata only — the minted value never
// re-crosses this surface.
type MintedCredential struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connectionId,omitempty"`
	Template     string `json:"template"`
	Status       string `json:"status,omitempty"` // active | revoked | expired
	SecretKey    string `json:"secretKey,omitempty"`
	MintedAt     string `json:"mintedAt,omitempty"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
	LastUsedAt   string `json:"lastUsedAt,omitempty"`
}

// IntegrationSandbox is one row of a provision-capable provider's sandbox
// listing.
type IntegrationSandbox struct {
	ID           string `json:"id"`
	Name         string `json:"name,omitempty"`
	Status       string `json:"status,omitempty"`
	ConnectionID string `json:"connectionId,omitempty"`
	CreatedAt    string `json:"createdAt,omitempty"`
	LastActiveAt string `json:"lastActiveAt,omitempty"`
}

// integrationsPath builds the org-scoped …/integrations base path.
func integrationsPath(org string) (string, error) {
	if strings.TrimSpace(org) == "" {
		return "", fmt.Errorf("configsurface: integrations call needs an organization")
	}
	return "/v1/organizations/" + urlSegment(org) + "/integrations", nil
}

// ListIntegrationConnections calls GET …/integrations (optionally filtered by
// provider): the org's connection rows, metadata only.
func (c *Client) ListIntegrationConnections(ctx context.Context, org, provider string) ([]IntegrationConnection, error) {
	path, err := integrationsPath(org)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(provider) != "" {
		path += "?provider=" + url.QueryEscape(provider)
	}
	var body json.RawMessage
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &body, true); err != nil {
		return nil, fmt.Errorf("list integration connections: %w", err)
	}
	items, _, err := decodeItems(body, "connections")
	if err != nil {
		return nil, fmt.Errorf("list integration connections: decoding response: %w", err)
	}
	var out []IntegrationConnection
	if err := json.Unmarshal(items, &out); err != nil {
		return nil, fmt.Errorf("list integration connections: decoding response: %w", err)
	}
	return out, nil
}

// GetIntegrationConnection calls GET …/integrations/{connectionId}.
func (c *Client) GetIntegrationConnection(ctx context.Context, org, connectionID string) (*IntegrationConnection, error) {
	path, err := integrationsPath(org)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(connectionID) == "" {
		return nil, fmt.Errorf("configsurface: connection read needs a connection id")
	}
	var env struct {
		IntegrationConnection
		Connection *IntegrationConnection `json:"connection"`
	}
	if err := c.doJSON(ctx, http.MethodGet, path+"/"+urlSegment(connectionID), nil, &env, true); err != nil {
		return nil, fmt.Errorf("get integration connection: %w", err)
	}
	if env.Connection != nil {
		return env.Connection, nil
	}
	conn := env.IntegrationConnection
	return &conn, nil
}

// RevokeIntegrationConnection calls DELETE …/integrations/{connectionId}
// (revoke). Bound secrets go orphaned server-side; nothing secret crosses.
func (c *Client) RevokeIntegrationConnection(ctx context.Context, org, connectionID string) error {
	path, err := integrationsPath(org)
	if err != nil {
		return err
	}
	if strings.TrimSpace(connectionID) == "" {
		return fmt.Errorf("configsurface: connection revoke needs a connection id")
	}
	if err := c.doJSON(ctx, http.MethodDelete, path+"/"+urlSegment(connectionID), nil, nil, false); err != nil {
		return fmt.Errorf("revoke integration connection: %w", err)
	}
	return nil
}

// ListProviderScopeTemplates calls GET …/integrations/providers/{id}/
// scope-templates: one provider's template catalog (the per-provider slice of
// the SP-A1 bulk capability read).
func (c *Client) ListProviderScopeTemplates(ctx context.Context, org, provider string) ([]ScopeTemplate, error) {
	path, err := integrationsPath(org)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(provider) == "" {
		return nil, fmt.Errorf("configsurface: template listing needs a provider")
	}
	var body json.RawMessage
	if err := c.doJSON(ctx, http.MethodGet, path+"/providers/"+urlSegment(provider)+"/scope-templates", nil, &body, true); err != nil {
		return nil, fmt.Errorf("list scope templates: %w", err)
	}
	items, _, err := decodeItems(body, "templates")
	if err != nil {
		return nil, fmt.Errorf("list scope templates: decoding response: %w", err)
	}
	var out []ScopeTemplate
	if err := json.Unmarshal(items, &out); err != nil {
		return nil, fmt.Errorf("list scope templates: decoding response: %w", err)
	}
	return out, nil
}

// ListMintedCredentials calls GET …/integrations/{connectionId}/credentials:
// the connection's minted-credential ledger, metadata only.
func (c *Client) ListMintedCredentials(ctx context.Context, org, connectionID string) ([]MintedCredential, error) {
	path, err := integrationsPath(org)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(connectionID) == "" {
		return nil, fmt.Errorf("configsurface: credential listing needs a connection id")
	}
	var body json.RawMessage
	if err := c.doJSON(ctx, http.MethodGet, path+"/"+urlSegment(connectionID)+"/credentials", nil, &body, true); err != nil {
		return nil, fmt.Errorf("list minted credentials: %w", err)
	}
	items, _, err := decodeItems(body, "credentials")
	if err != nil {
		return nil, fmt.Errorf("list minted credentials: decoding response: %w", err)
	}
	var out []MintedCredential
	if err := json.Unmarshal(items, &out); err != nil {
		return nil, fmt.Errorf("list minted credentials: decoding response: %w", err)
	}
	return out, nil
}

// RevokeMintedCredential calls DELETE …/integrations/{connectionId}/
// credentials/{credentialId}.
func (c *Client) RevokeMintedCredential(ctx context.Context, org, connectionID, credentialID string) error {
	path, err := integrationsPath(org)
	if err != nil {
		return err
	}
	if strings.TrimSpace(connectionID) == "" || strings.TrimSpace(credentialID) == "" {
		return fmt.Errorf("configsurface: credential revoke needs a connection id and credential id")
	}
	if err := c.doJSON(ctx, http.MethodDelete, path+"/"+urlSegment(connectionID)+"/credentials/"+urlSegment(credentialID), nil, nil, false); err != nil {
		return fmt.Errorf("revoke minted credential: %w", err)
	}
	return nil
}

// ListProviderSandboxes calls GET …/integrations/providers/{id}/sandboxes: a
// provision-capable provider's sandbox rows.
func (c *Client) ListProviderSandboxes(ctx context.Context, org, provider string) ([]IntegrationSandbox, error) {
	path, err := integrationsPath(org)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(provider) == "" {
		return nil, fmt.Errorf("configsurface: sandbox listing needs a provider")
	}
	var body json.RawMessage
	if err := c.doJSON(ctx, http.MethodGet, path+"/providers/"+urlSegment(provider)+"/sandboxes", nil, &body, true); err != nil {
		return nil, fmt.Errorf("list sandboxes: %w", err)
	}
	items, _, err := decodeItems(body, "sandboxes")
	if err != nil {
		return nil, fmt.Errorf("list sandboxes: decoding response: %w", err)
	}
	var out []IntegrationSandbox
	if err := json.Unmarshal(items, &out); err != nil {
		return nil, fmt.Errorf("list sandboxes: decoding response: %w", err)
	}
	return out, nil
}
