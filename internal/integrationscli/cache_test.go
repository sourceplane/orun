package integrationscli

// ICL0: the per-org registry cache — TTL, etag sidecar, and the corrupt-cache-
// is-a-miss rule (the cache is presentation state and may never fail a
// command).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/configsurface"
)

// loadFixtureRegistry decodes the recorded IR0-contract fixture.
func loadFixtureRegistry(t *testing.T) []configsurface.IntegrationDescriptor {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "registry.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var payload struct {
		Registry []configsurface.IntegrationDescriptor `json:"registry"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(payload.Registry) == 0 {
		t.Fatal("fixture registry is empty")
	}
	return payload.Registry
}

func fixtureDescriptor(t *testing.T, provider string) configsurface.IntegrationDescriptor {
	t.Helper()
	for _, d := range loadFixtureRegistry(t) {
		if d.Provider == provider {
			return d
		}
	}
	t.Fatalf("fixture has no provider %q", provider)
	return configsurface.IntegrationDescriptor{}
}

func TestCacheRoundTrip(t *testing.T) {
	orunDir := t.TempDir()
	registry := loadFixtureRegistry(t)
	now := time.Now()
	if err := SaveCachedRegistry(orunDir, "org_1", registry, `"v7"`, now); err != nil {
		t.Fatalf("save: %v", err)
	}
	cache := LoadCachedRegistry(orunDir, "org_1")
	if cache == nil {
		t.Fatal("expected a cache hit")
	}
	if cache.Org != "org_1" || cache.ETag != `"v7"` {
		t.Errorf("cache identity = %+v", cache)
	}
	if len(cache.Registry) != len(registry) {
		t.Fatalf("registry rows = %d, want %d", len(cache.Registry), len(registry))
	}
	// Provider ids survive the round trip (the id JSON tag + fallback decode).
	if cache.Registry[0].Provider != "cloudflare" || cache.Registry[4].Provider != "aws" {
		t.Errorf("providers = %q, %q", cache.Registry[0].Provider, cache.Registry[4].Provider)
	}
	if cache.Descriptor("supabase") == nil || cache.Descriptor("nope") != nil {
		t.Error("Descriptor lookup broken")
	}
	if cache.Stale(now.Add(time.Hour)) {
		t.Error("a 1h-old cache must not be stale")
	}
	if !cache.Stale(now.Add(25 * time.Hour)) {
		t.Error("a 25h-old cache must be stale (24h soft TTL)")
	}
	// A different org is a miss.
	if LoadCachedRegistry(orunDir, "org_2") != nil {
		t.Error("cache must be per-org")
	}
}

func TestCacheCorruptIsMissNeverFatal(t *testing.T) {
	orunDir := t.TempDir()
	path := CachePath(orunDir, "org_1")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, corrupt := range []string{"{not json", `"a string"`, `{"org":"org_1"}`, ""} {
		if err := os.WriteFile(path, []byte(corrupt), 0o644); err != nil {
			t.Fatal(err)
		}
		if cache := LoadCachedRegistry(orunDir, "org_1"); cache != nil {
			t.Errorf("corrupt cache %q must read as a miss, got %+v", corrupt, cache)
		}
	}
	// Recovery: a good save over a corrupt file works.
	if err := SaveCachedRegistry(orunDir, "org_1", loadFixtureRegistry(t), "", time.Now()); err != nil {
		t.Fatalf("save over corrupt: %v", err)
	}
	cache := LoadCachedRegistry(orunDir, "org_1")
	if cache == nil {
		t.Fatal("expected a hit after re-save")
	}
	if cache.ETag != "" {
		t.Errorf("etag = %q, want empty", cache.ETag)
	}
}

func TestCacheMissingEtagSidecar(t *testing.T) {
	orunDir := t.TempDir()
	if err := SaveCachedRegistry(orunDir, "org_1", loadFixtureRegistry(t), `"v1"`, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(orunDir, "integrations", "registry-org_1.etag")); err != nil {
		t.Fatal(err)
	}
	cache := LoadCachedRegistry(orunDir, "org_1")
	if cache == nil || cache.ETag != "" {
		t.Fatalf("missing sidecar must load with empty etag, got %+v", cache)
	}
}

func TestCachePathSanitizesOrg(t *testing.T) {
	got := CachePath("/x/.orun", "acme/../evil org")
	want := filepath.Join("/x/.orun", "integrations", "registry-acme_.._evil_org.json")
	if got != want {
		t.Errorf("CachePath = %q, want %q", got, want)
	}
}

func TestLoadCachedRegistryBlankOrg(t *testing.T) {
	if LoadCachedRegistry(t.TempDir(), " ") != nil {
		t.Error("blank org must be a miss")
	}
}
