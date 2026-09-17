package actions

import (
	"strings"
	"testing"
)

// ORUN_WORKSPACE IS THE LEADING SPELLING and every CLI surface prefers it, but
// the actions read only the ORUN_ORG alias — so a hook inside a bootstrap that
// exported the leading name was told "set ORUN_ORG", naming the one spelling
// the operator had not used. (Live: `orun baseline new` phase 03's `providers`
// probe.)
func TestWorkspaceFromEnvPrefersTheLeadingSpelling(t *testing.T) {
	t.Setenv("ORUN_WORKSPACE", "ws_leading")
	t.Setenv("ORUN_ORG", "ws_alias")
	if got := workspaceFromEnv(); got != "ws_leading" {
		t.Fatalf("workspaceFromEnv() = %q, want the ORUN_WORKSPACE value", got)
	}
}

// The alias still answers on its own: a sandbox or CI that sets only ORUN_ORG
// must keep working.
func TestWorkspaceFromEnvStillReadsTheAlias(t *testing.T) {
	t.Setenv("ORUN_WORKSPACE", "")
	t.Setenv("ORUN_ORG", "ws_alias")
	if got := workspaceFromEnv(); got != "ws_alias" {
		t.Fatalf("workspaceFromEnv() = %q, want the ORUN_ORG value", got)
	}
}

// Whitespace is not a workspace — an exported-but-empty var must not shadow
// the alias, or beat the `org` parameter into a blank scope.
func TestWorkspaceFromEnvIgnoresBlanks(t *testing.T) {
	t.Setenv("ORUN_WORKSPACE", "   ")
	t.Setenv("ORUN_ORG", "ws_alias")
	if got := workspaceFromEnv(); got != "ws_alias" {
		t.Fatalf("a blank ORUN_WORKSPACE shadowed the alias: %q", got)
	}
	t.Setenv("ORUN_ORG", "")
	if got := workspaceFromEnv(); got != "" {
		t.Fatalf("workspaceFromEnv() = %q, want empty", got)
	}
}

// The message has to name both spellings and the parameter; the old one named
// only the alias.
func TestNoWorkspaceErrorNamesBothSpellings(t *testing.T) {
	msg := errNoWorkspace().Error()
	for _, want := range []string{"`org`", "ORUN_WORKSPACE", "ORUN_ORG"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("errNoWorkspace() = %q, missing %q", msg, want)
		}
	}
}
