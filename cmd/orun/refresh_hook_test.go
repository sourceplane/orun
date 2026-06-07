package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// newCmd builds a command with the given name, optionally parented, so the
// gating predicates (which walk Name()/Parent()) can be exercised.
func newCmd(name string, parent *cobra.Command) *cobra.Command {
	c := &cobra.Command{Use: name}
	if parent != nil {
		parent.AddCommand(c)
	}
	return c
}

func TestAutoRefreshEnabledGating(t *testing.T) {
	catalog := newCmd("catalog", nil)
	catalogRefresh := newCmd("refresh", catalog)
	catalogList := newCmd("list", catalog)

	tests := []struct {
		name string
		cmd  *cobra.Command
		want bool
	}{
		{"plan resolves authoritatively", newCmd("plan", nil), false},
		{"catalog refresh resolves authoritatively", catalogRefresh, false},
		{"catalog list reads the catalog", catalogList, true},
		{"validate uses intent", newCmd("validate", nil), true},
		{"run uses intent", newCmd("run", nil), true},
		{"auth is unrelated", newCmd("auth", nil), false},
		{"bare tui refreshes on its own ticker", newCmd("tui", nil), false},
		{"nil command", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := autoRefreshEnabled(tt.cmd); got != tt.want {
				t.Fatalf("autoRefreshEnabled = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAutoRefreshEnabledDisabledByEnv(t *testing.T) {
	t.Setenv(autoRefreshEnvVar, "1")
	if autoRefreshEnabled(newCmd("validate", nil)) {
		t.Fatal("expected the escape-hatch env to disable the hook")
	}
}

func TestAutoRefreshDebounce(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "cache", autoRefreshMarkerName)
	now := time.Now()

	// No marker yet → never debounced.
	if autoRefreshDebounced(marker, now) {
		t.Fatal("missing marker must not debounce")
	}

	// Fresh marker → debounced within the TTL.
	writeAutoRefreshMarker(marker, autoRefreshMarker{
		SourceKey:   "src-abc",
		RefreshedAt: now.UTC().Format(time.RFC3339Nano),
	})
	if !autoRefreshDebounced(marker, now.Add(autoRefreshTTL/2)) {
		t.Fatal("a fresh marker within the TTL must debounce")
	}

	// Past the TTL → refresh proceeds again.
	if autoRefreshDebounced(marker, now.Add(autoRefreshTTL*2)) {
		t.Fatal("a marker older than the TTL must not debounce")
	}
}

func TestAutoRefreshDebounceUnparsableMarker(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, autoRefreshMarkerName)
	// A garbage marker must fail open (refresh proceeds), not block forever.
	writeAutoRefreshMarker(marker, autoRefreshMarker{RefreshedAt: "not-a-time"})
	if autoRefreshDebounced(marker, time.Now()) {
		t.Fatal("an unparsable marker must not debounce")
	}
}

func TestEnvTruthy(t *testing.T) {
	const key = "ORUN_TEST_TRUTHY"
	for _, v := range []string{"", "0", "false", "no"} {
		t.Setenv(key, v)
		if envTruthy(key) {
			t.Fatalf("envTruthy(%q) = true, want false", v)
		}
	}
	for _, v := range []string{"1", "true", "yes", "anything"} {
		t.Setenv(key, v)
		if !envTruthy(key) {
			t.Fatalf("envTruthy(%q) = false, want true", v)
		}
	}
}

func TestAutoRefreshVerbose(t *testing.T) {
	t.Setenv(verboseEnvVar, "")
	cmd := newCmd("validate", nil)
	cmd.Flags().Bool("verbose", false, "")
	if autoRefreshVerbose(cmd) {
		t.Fatal("verbose must be off by default")
	}

	if err := cmd.Flags().Set("verbose", "true"); err != nil {
		t.Fatal(err)
	}
	if !autoRefreshVerbose(cmd) {
		t.Fatal("a set --verbose flag must enable verbose logging")
	}

	t.Setenv(verboseEnvVar, "1")
	if !autoRefreshVerbose(newCmd("validate", nil)) {
		t.Fatal("ORUN_VERBOSE must enable verbose logging")
	}
}
