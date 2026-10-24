package cliauth

import (
	"strings"
	"time"
)

// OrgRef identifies an organization the actor belongs to, with their role. It
// is the org/project-spine replacement for the retiring "namespace" model:
// orgs[].id serves what the CLI previously called allowedNamespaceIds.
type OrgRef struct {
	ID string `json:"id" yaml:"id"`
	// WorkspaceRef is the led-with public Workspace ID (`ws_…`, WID2) — the
	// PRIMARY identifier users see and pass around; ID is the internal
	// org_… public id kept for API paths and back-compat.
	WorkspaceRef string `json:"workspaceRef,omitempty" yaml:"workspaceRef,omitempty"`
	Slug         string `json:"slug,omitempty" yaml:"slug,omitempty"`
	Name         string `json:"name,omitempty" yaml:"name,omitempty"`
	Role         string `json:"role,omitempty" yaml:"role,omitempty"`
	// AccountID is the account the workspace belongs to (the billing and
	// ownership boundary above a workspace), when the backend reports it;
	// AccountSlug is its slug when that is known too. Both are empty on
	// sessions and backends that predate the field.
	AccountID   string `json:"accountId,omitempty" yaml:"accountId,omitempty"`
	AccountSlug string `json:"accountSlug,omitempty" yaml:"accountSlug,omitempty"`
}

// AccountRef identifies an account the actor can see (GET /v1/accounts). ID is
// what POST /v1/organizations takes as accountId; Role is the actor's role in
// it ("owner" for the accounts a workspace can be created in).
type AccountRef struct {
	ID   string `json:"id" yaml:"id"`
	Slug string `json:"slug,omitempty" yaml:"slug,omitempty"`
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	Role string `json:"role,omitempty" yaml:"role,omitempty"`
}

// Label is the identifier to show and accept for the account: the slug when
// there is one, else the id.
func (a AccountRef) Label() string {
	if s := strings.TrimSpace(a.Slug); s != "" {
		return s
	}
	return strings.TrimSpace(a.ID)
}

// Matches reports whether a user-typed value names this account — by slug or
// id, case-insensitively.
func (a AccountRef) Matches(target string) bool {
	t := strings.TrimSpace(target)
	if t == "" {
		return false
	}
	for _, candidate := range []string{a.Slug, a.ID} {
		if c := strings.TrimSpace(candidate); c != "" && strings.EqualFold(c, t) {
			return true
		}
	}
	return false
}

// Credentials are the locally stored Orun CLI session secrets.
//
// Org model (OC0): Orgs is the org/project-spine field. AllowedNamespaceIDs is
// retained read-only for back-compat — the embedded OSS `orun backend` worker
// still returns it and is not migrated until OC6. LoadSession migrates an old
// credentials file (only allowedNamespaceIds) to Orgs once on read.
type Credentials struct {
	AccessToken         string      `json:"accessToken,omitempty"`
	AccessTokenExpiry   string      `json:"accessTokenExpiry,omitempty"`
	RefreshToken        string      `json:"refreshToken,omitempty"`
	RefreshTokenExpiry  string      `json:"refreshTokenExpiry,omitempty"`
	User                SessionUser `json:"user,omitempty"`
	GitHubLogin         string      `json:"githubLogin,omitempty"`
	Orgs                []OrgRef    `json:"orgs,omitempty"`
	AllowedNamespaceIDs []string    `json:"allowedNamespaceIds,omitempty"`
	BackendURL          string      `json:"backendUrl,omitempty"`
}

// DisplayUser returns a human label for the session user, preferring display
// name, then email, then the legacy githubLogin field.
func (c *Credentials) DisplayUser() string {
	if c == nil {
		return ""
	}
	if c.User.DisplayName != "" {
		return c.User.DisplayName
	}
	if c.User.Email != "" {
		return c.User.Email
	}
	return c.GitHubLogin
}

// BackendConfig holds the default backend URL. Deprecated in favor of the
// cloud block; honored as an alias for one release (design §8).
type BackendConfig struct {
	URL string `yaml:"url,omitempty"`
}

// CloudConfig is the user-config `cloud` block (design §8): the backend URL
// (Orun Cloud or a self-hosted backend) plus catalog options.
type CloudConfig struct {
	URL     string             `yaml:"url,omitempty"`
	Catalog CloudCatalogConfig `yaml:"catalog,omitempty"`
}

// CloudCatalogConfig holds catalog-related cloud options.
type CloudCatalogConfig struct {
	// AutoPush opts into pushing the catalog snapshot automatically. Off by
	// default — publishing the catalog is a team-visible act (design §5).
	AutoPush bool `yaml:"autopush,omitempty"`
}

// RepoLink records the current repo-to-scope mapping used for local
// session-authenticated remote-state runs.
//
// Org model (OC0): OrgID/OrgSlug/ProjectID/ProjectSlug are the org/project
// spine added additively. NamespaceID/NamespaceKind are kept for back-compat
// with the embedded OSS backend's single-tenant flow (retired in OC6).
type RepoLink struct {
	BackendURL   string `yaml:"backendUrl,omitempty"`
	GitRemote    string `yaml:"gitRemote,omitempty"`
	RepoFullName string `yaml:"repoFullName,omitempty"`
	// Org/project spine (OC0+).
	OrgID       string `yaml:"orgId,omitempty"`
	OrgSlug     string `yaml:"orgSlug,omitempty"`
	ProjectID   string `yaml:"projectId,omitempty"`
	ProjectSlug string `yaml:"projectSlug,omitempty"`
	// Legacy single-tenant fields (OSS backend; retired in OC6).
	NamespaceID   string `yaml:"namespaceId,omitempty"`
	NamespaceKind string `yaml:"namespaceKind,omitempty"`
	RepoID        string `yaml:"repoId,omitempty"`
	LinkedAt      string `yaml:"linkedAt,omitempty"`
}

// BackendBootstrap holds non-secret metadata written by `orun backend init`.
// Secrets (API tokens, session secrets, GitHub client secrets) are never stored here.
type BackendBootstrap struct {
	// ManagedBy is always "orun-backend-init" to prevent accidental destroy of unrelated resources.
	ManagedBy        string `yaml:"managedBy,omitempty"`
	AccountID        string `yaml:"accountId,omitempty"`
	WorkerName       string `yaml:"workerName,omitempty"`
	D1DatabaseName   string `yaml:"d1DatabaseName,omitempty"`
	D1DatabaseUUID   string `yaml:"d1DatabaseUUID,omitempty"`
	R2BucketName     string `yaml:"r2BucketName,omitempty"`
	CatalogQueueName string `yaml:"catalogQueueName,omitempty"`
	CatalogQueueID   string `yaml:"catalogQueueID,omitempty"`
	CatalogDLQName   string `yaml:"catalogDLQName,omitempty"`
	CatalogDLQID     string `yaml:"catalogDLQID,omitempty"`
	CatalogCron      string `yaml:"catalogCron,omitempty"`
	BackendCommit    string `yaml:"backendCommit,omitempty"`
	InitAt           string `yaml:"initAt,omitempty"`
}

// WorkspaceSelection is the working workspace chosen with `orun workspace use`
// — the machine-wide default for every command that needs a tenancy and was
// given none. It is a DEFAULT, not an override: anything more specific (the
// --workspace flag, ORUN_WORKSPACE, a repo's intent.yaml, a repo link) still
// wins, so selecting a workspace can never silently retarget a linked repo.
type WorkspaceSelection struct {
	// ID is what commands resolve to: the ws_… Workspace ID when the session
	// knows it (WID2), else the org_… public id.
	ID   string `yaml:"id,omitempty"`
	Slug string `yaml:"slug,omitempty"`
	// SetAt is when it was selected (RFC3339), shown by `orun workspace`.
	SetAt string `yaml:"setAt,omitempty"`
}

// Config is the non-secret CLI config stored in ~/.orun/config.yaml.
type Config struct {
	// Cloud is the preferred backend/catalog config block (design §8).
	Cloud CloudConfig `yaml:"cloud,omitempty"`
	// Backend is the deprecated alias for cloud.url (one release, design §8).
	Backend          BackendConfig      `yaml:"backend,omitempty"`
	BackendBootstrap *BackendBootstrap  `yaml:"backendBootstrap,omitempty"`
	Workspace        WorkspaceSelection `yaml:"workspace,omitempty"`
	Repos            []RepoLink         `yaml:"repos,omitempty"`
}

// Label renders the selection for humans: the slug when known (that is what
// people say out loud), else the id.
func (w WorkspaceSelection) Label() string {
	if s := strings.TrimSpace(w.Slug); s != "" {
		return s
	}
	return strings.TrimSpace(w.ID)
}

// AccessExpiryTime parses the stored access-token expiry.
func (c *Credentials) AccessExpiryTime() time.Time {
	if c == nil || c.AccessTokenExpiry == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, c.AccessTokenExpiry)
	return t
}

// RefreshExpiryTime parses the stored refresh-token expiry.
func (c *Credentials) RefreshExpiryTime() time.Time {
	if c == nil || c.RefreshTokenExpiry == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, c.RefreshTokenExpiry)
	return t
}
