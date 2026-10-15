package cachehome

import (
	"path/filepath"
	"testing"
)

func TestToolCacheFromPrefersTheOverrideThenTheHostsThenTheDefault(t *testing.T) {
	t.Parallel()
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	exists := func(existing ...string) func(string) bool {
		return func(path string) bool {
			for _, e := range existing {
				if e == path {
					return true
				}
			}
			return false
		}
	}

	if got := ToolCacheFrom(env(map[string]string{"ORUN_TOOL_CACHE": "/opt/orun-tools", "RUNNER_TOOL_CACHE": "/opt/hostedtoolcache"}), exists("/opt/hostedtoolcache")); got != "/opt/orun-tools" {
		t.Fatalf("ORUN_TOOL_CACHE wins: %s", got)
	}
	if got := ToolCacheFrom(env(map[string]string{"RUNNER_TOOL_CACHE": "/opt/hostedtoolcache"}), exists("/opt/hostedtoolcache")); got != "/opt/hostedtoolcache" {
		t.Fatalf("an existing host tool cache is inherited: %s", got)
	}
	want := filepath.Join(Root(), "tool-cache")
	if got := ToolCacheFrom(env(map[string]string{"RUNNER_TOOL_CACHE": "/nowhere"}), exists()); got != want {
		t.Fatalf("a host tool cache that does not exist is not used: %s, want %s", got, want)
	}
	if got := ToolCacheFrom(env(nil), exists()); got != want {
		t.Fatalf("default: %s, want %s", got, want)
	}
}
