// Package integrationscli renders the registry-served integration verb trees
// as cobra commands (orun-integrations-cli, specs/orun-integrations-cli/).
//
// The binary carries no provider catalog: provider namespaces, verb paths,
// args, and help all derive from the org's cached Integration Registry read
// (internal/configsurface.GetIntegrationRegistry). This package owns the
// per-org presentation cache, the descriptor → cobra renderer, the compiled-in
// invoke allowlist (the security boundary — descriptors select operations,
// they never define them), and the native-extension seam.
//
// Invariants inherited from configsurface: no request or response on any path
// here ever carries a secret value, and no error embeds one.
package integrationscli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/configsurface"
)

// CacheSoftTTL is the soft freshness window for a cached registry: an older
// cache still renders help and completion (offline help), but invocations
// print a one-line staleness note pointing at `orun integrations sync`.
// Execution is always server-validated regardless of cache age.
const CacheSoftTTL = 24 * time.Hour

// CachedRegistry is one org's cached registry read plus its freshness state.
type CachedRegistry struct {
	Org       string                                `json:"org"`
	FetchedAt time.Time                             `json:"fetchedAt"`
	Registry  []configsurface.IntegrationDescriptor `json:"registry"`
	// ETag is the server's registry version tag, kept in the sidecar file and
	// sent as If-None-Match on the next sync.
	ETag string `json:"-"`
}

// Stale reports whether the cache is past the soft TTL at now.
func (c *CachedRegistry) Stale(now time.Time) bool {
	return now.Sub(c.FetchedAt) > CacheSoftTTL
}

// Descriptor returns the cached descriptor for provider, or nil.
func (c *CachedRegistry) Descriptor(provider string) *configsurface.IntegrationDescriptor {
	if c == nil {
		return nil
	}
	for i := range c.Registry {
		if c.Registry[i].Provider == provider {
			return &c.Registry[i]
		}
	}
	return nil
}

// CachePath returns the registry cache file for org under the .orun directory:
// <orunDir>/integrations/registry-<org>.json (+ a .etag sidecar beside it).
func CachePath(orunDir, org string) string {
	return filepath.Join(orunDir, "integrations", "registry-"+safeFileSegment(org)+".json")
}

func etagPath(orunDir, org string) string {
	return filepath.Join(orunDir, "integrations", "registry-"+safeFileSegment(org)+".etag")
}

// safeFileSegment maps an org slug/id onto a filesystem-safe file segment.
func safeFileSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// LoadCachedRegistry reads the per-org cache. Any miss — absent file, corrupt
// JSON, mismatched shape — returns nil: the cache is presentation state only
// and is never allowed to fail a command (corrupt cache = cache miss).
func LoadCachedRegistry(orunDir, org string) *CachedRegistry {
	if strings.TrimSpace(org) == "" {
		return nil
	}
	data, err := os.ReadFile(CachePath(orunDir, org))
	if err != nil {
		return nil
	}
	var cache CachedRegistry
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil
	}
	if len(cache.Registry) == 0 || cache.FetchedAt.IsZero() {
		return nil
	}
	cache.Org = org
	if etag, err := os.ReadFile(etagPath(orunDir, org)); err == nil {
		cache.ETag = strings.TrimSpace(string(etag))
	}
	return &cache
}

// SaveCachedRegistry writes the per-org cache and its etag sidecar, creating
// <orunDir>/integrations/ as needed. The write is atomic (temp + rename) so a
// crashed sync can only ever leave the previous cache or the new one — a torn
// file would otherwise read as corrupt and drop offline help.
func SaveCachedRegistry(orunDir, org string, registry []configsurface.IntegrationDescriptor, etag string, now time.Time) error {
	if err := os.MkdirAll(filepath.Join(orunDir, "integrations"), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(CachedRegistry{Org: org, FetchedAt: now.UTC(), Registry: registry}, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(CachePath(orunDir, org), append(data, '\n')); err != nil {
		return err
	}
	return writeFileAtomic(etagPath(orunDir, org), []byte(etag+"\n"))
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
