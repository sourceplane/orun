package cliauth

import "time"

// OrgRef identifies an organization the actor belongs to, with their role. It
// is the org/project-spine replacement for the retiring "namespace" model:
// orgs[].id serves what the CLI previously called allowedNamespaceIds.
type OrgRef struct {
	ID   string `json:"id" yaml:"id"`
	Slug string `json:"slug,omitempty" yaml:"slug,omitempty"`
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	Role string `json:"role,omitempty" yaml:"role,omitempty"`
}

// Credentials are the locally stored Orun CLI session secrets.
//
// Org model (OC0): Orgs is the org/project-spine field. AllowedNamespaceIDs is
// retained read-only for back-compat — the embedded OSS `orun backend` worker
// still returns it and is not migrated until OC6. LoadSession migrates an old
// credentials file (only allowedNamespaceIds) to Orgs once on read.
type Credentials struct {
	AccessToken         string   `json:"accessToken,omitempty"`
	AccessTokenExpiry   string   `json:"accessTokenExpiry,omitempty"`
	RefreshToken        string   `json:"refreshToken,omitempty"`
	RefreshTokenExpiry  string   `json:"refreshTokenExpiry,omitempty"`
	GitHubLogin         string   `json:"githubLogin,omitempty"`
	Orgs                []OrgRef `json:"orgs,omitempty"`
	AllowedNamespaceIDs []string `json:"allowedNamespaceIds,omitempty"`
	BackendURL          string   `json:"backendUrl,omitempty"`
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

// Config is the non-secret CLI config stored in ~/.orun/config.yaml.
type Config struct {
	// Cloud is the preferred backend/catalog config block (design §8).
	Cloud CloudConfig `yaml:"cloud,omitempty"`
	// Backend is the deprecated alias for cloud.url (one release, design §8).
	Backend          BackendConfig     `yaml:"backend,omitempty"`
	BackendBootstrap *BackendBootstrap `yaml:"backendBootstrap,omitempty"`
	Repos            []RepoLink        `yaml:"repos,omitempty"`
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
