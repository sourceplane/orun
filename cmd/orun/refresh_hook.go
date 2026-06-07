package main

// refresh_hook.go is the universal refresh hook (§0, cli-surface.md;
// design.md §3.4 trigger 1; D-8). A root pre-run hook runs the freshness gate
// around any catalog-using command: on a clean/unchanged tree it is cheap, on a
// miss it resolves and repoints catalogs/current as a side effect of using
// orun. It is invisible, best-effort (errors are swallowed), time-bounded, and
// debounced to at most one resolve per autoRefreshTTL per workspace.
//
// It is a no-op for commands that resolve authoritatively (plan, catalog
// refresh), for commands that do not read the object catalog, and when
// ORUN_NO_AUTO_REFRESH is set. The cockpit is excluded too: it keeps itself
// fresh on its own interval ticker (design.md §3.4 trigger 2).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

const (
	// autoRefreshEnvVar disables the universal refresh hook when truthy — an
	// escape hatch for scripted/perf-sensitive callers (cli-surface.md §0).
	autoRefreshEnvVar = "ORUN_NO_AUTO_REFRESH"
	// verboseEnvVar logs the auto-refresh outcome to stderr when truthy. The
	// hook is otherwise silent (no output, no flag).
	verboseEnvVar = "ORUN_VERBOSE"
	// autoRefreshTTL bounds the hook to at most one resolve per window per
	// workspace (D-8). The freshness gate keeps the clean case free; the TTL
	// bounds the dirty-edit case to one resolve per window (S-9).
	autoRefreshTTL = 1 * time.Second
	// autoRefreshTimeout caps the best-effort refresh so a slow resolve never
	// blocks the command's primary work.
	autoRefreshTimeout = 10 * time.Second
	// autoRefreshMarkerName is the debounce record under the object-model cache.
	autoRefreshMarkerName = "auto-refresh.json"
)

// autoRefreshMarker is the persisted debounce record.
type autoRefreshMarker struct {
	SourceKey   string `json:"sourceKey"`
	RefreshedAt string `json:"refreshedAt"`
}

// maybeAutoRefresh is the universal refresh hook. It is called from the root
// PersistentPreRunE after intent discovery (so storeDir() is settled). It never
// returns an error: a refresh failure leaves the catalog as-is for the command
// to handle, which keeps `orun <cmd>` from ever hard-failing on a stale store.
func maybeAutoRefresh(cmd *cobra.Command) {
	if !autoRefreshEnabled(cmd) {
		return
	}

	root, ok := autoRefreshRoot()
	if !ok {
		return
	}
	markerPath := filepath.Join(root, "cache", autoRefreshMarkerName)
	if autoRefreshDebounced(markerPath, time.Now()) {
		return
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), autoRefreshTimeout)
	defer cancel()

	rc, err := refreshObjectCatalog(ctx)
	if err != nil {
		autoRefreshLog(cmd, "orun: auto-refresh skipped: %v", err)
		return
	}
	writeAutoRefreshMarker(markerPath, autoRefreshMarker{
		SourceKey:   rc.sourceKey,
		RefreshedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	autoRefreshLog(cmd, "orun: auto-refreshed catalog %s (source %s)", rc.result.CatalogID, rc.sourceKey)
}

// autoRefreshEnabled reports whether the hook should run for cmd. It excludes
// the escape-hatch env, commands that resolve authoritatively (plan / catalog
// refresh), and commands that do not read the object catalog (incl. the bare
// TUI, which refreshes on its own ticker).
func autoRefreshEnabled(cmd *cobra.Command) bool {
	if envTruthy(autoRefreshEnvVar) {
		return false
	}
	if cmd == nil {
		return false
	}
	if commandResolvesAuthoritatively(cmd) {
		return false
	}
	return commandUsesObjectCatalog(cmd)
}

// commandResolvesAuthoritatively reports whether cmd already populates the
// object catalog through its own full resolve, making the hook redundant.
func commandResolvesAuthoritatively(cmd *cobra.Command) bool {
	if cmd.Name() == "plan" {
		return true
	}
	if cmd.Name() == "refresh" {
		if p := cmd.Parent(); p != nil && p.Name() == "catalog" {
			return true
		}
	}
	return false
}

// commandUsesObjectCatalog reports whether cmd reads the object-model catalog:
// the intent-using commands plus the catalog read subcommands.
func commandUsesObjectCatalog(cmd *cobra.Command) bool {
	if commandUsesIntent(cmd) {
		return true
	}
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "catalog" {
			return true
		}
	}
	return false
}

// autoRefreshRoot returns the object-model root for the debounce marker without
// opening the stores, keeping the debounced path cheap.
func autoRefreshRoot() (string, bool) {
	abs, err := filepath.Abs(filepath.Join(storeDir(), ".orun"))
	if err != nil {
		return "", false
	}
	return objectModelRoot(abs), true
}

// autoRefreshDebounced reports whether the last refresh was within autoRefreshTTL
// of now. A missing/unparsable marker is never debounced (refresh proceeds).
func autoRefreshDebounced(markerPath string, now time.Time) bool {
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return false
	}
	var m autoRefreshMarker
	if json.Unmarshal(data, &m) != nil {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, m.RefreshedAt)
	if err != nil {
		return false
	}
	return now.Sub(t) >= 0 && now.Sub(t) < autoRefreshTTL
}

// writeAutoRefreshMarker records the debounce marker best-effort.
func writeAutoRefreshMarker(markerPath string, m autoRefreshMarker) {
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	_ = os.WriteFile(markerPath, data, 0o644)
}

// autoRefreshVerbose reports whether to log the refresh outcome: the
// ORUN_VERBOSE env, or a --verbose flag set on the running command.
func autoRefreshVerbose(cmd *cobra.Command) bool {
	if envTruthy(verboseEnvVar) {
		return true
	}
	if cmd != nil {
		if f := cmd.Flags().Lookup("verbose"); f != nil && f.Changed {
			return true
		}
	}
	return false
}

func autoRefreshLog(cmd *cobra.Command, format string, args ...any) {
	if autoRefreshVerbose(cmd) {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

// envTruthy reports whether the named env var is set to a truthy value.
func envTruthy(key string) bool {
	switch os.Getenv(key) {
	case "", "0", "false", "no":
		return false
	default:
		return true
	}
}
