// Package cachehome names the directories orun's machine-wide caches live
// in, so the GitHub Actions engine, the runner and `orun cache` agree on
// them (specs/orun-cache-efficiency CE3).
package cachehome

import (
	"os"
	"path/filepath"
	"strings"
)

// Root is ~/.orun, or $TMPDIR/orun for a process without a home directory.
func Root() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "orun")
	}
	return filepath.Join(home, ".orun")
}

// Actions is where remote actions are cached, one directory per version.
func Actions() string { return filepath.Join(Root(), "actions") }

// Shared holds the caches every job on the machine shares: the package
// stores, the toolchain caches, the declared caches.
func Shared() string { return filepath.Join(Actions(), "shared") }

// Declared is where the caches composition steps declare (`caches:`) are
// kept between jobs and runs, one directory per name.
func Declared() string { return filepath.Join(Shared(), "caches") }

// ToolCache is the directory setup actions install into (RUNNER_TOOL_CACHE
// for the job): ORUN_TOOL_CACHE when set; else the host runner's
// RUNNER_TOOL_CACHE when it names an existing directory, which on a GitHub
// runner already holds Node, Python and Go; else <root>/tool-cache.
func ToolCache() string { return ToolCacheFrom(os.Getenv, dirExists) }

// ToolCacheFrom is ToolCache over a given environment and existence check.
func ToolCacheFrom(getenv func(string) string, exists func(string) bool) string {
	if v := strings.TrimSpace(getenv("ORUN_TOOL_CACHE")); v != "" {
		return filepath.Clean(v)
	}
	if v := strings.TrimSpace(getenv("RUNNER_TOOL_CACHE")); v != "" && exists(v) {
		return filepath.Clean(v)
	}
	return filepath.Join(Root(), "tool-cache")
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
