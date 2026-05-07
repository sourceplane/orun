package cliauth

import "time"

// Credentials are the locally stored Orun CLI session secrets.
type Credentials struct {
	AccessToken         string   `json:"accessToken,omitempty"`
	AccessTokenExpiry   string   `json:"accessTokenExpiry,omitempty"`
	RefreshToken        string   `json:"refreshToken,omitempty"`
	RefreshTokenExpiry  string   `json:"refreshTokenExpiry,omitempty"`
	GitHubLogin         string   `json:"githubLogin,omitempty"`
	AllowedNamespaceIDs []string `json:"allowedNamespaceIds,omitempty"`
	BackendURL          string   `json:"backendUrl,omitempty"`
}

// BackendConfig holds the default backend URL.
type BackendConfig struct {
	URL string `yaml:"url,omitempty"`
}

// RepoLink records the current repo-to-namespace mapping used for local
// session-authenticated remote-state runs.
type RepoLink struct {
	BackendURL   string `yaml:"backendUrl,omitempty"`
	GitRemote    string `yaml:"gitRemote,omitempty"`
	RepoFullName string `yaml:"repoFullName,omitempty"`
	NamespaceID  string `yaml:"namespaceId,omitempty"`
	LinkedAt     string `yaml:"linkedAt,omitempty"`
}

// Config is the non-secret CLI config stored in ~/.orun/config.yaml.
type Config struct {
	Backend BackendConfig `yaml:"backend,omitempty"`
	Repos   []RepoLink    `yaml:"repos,omitempty"`
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
