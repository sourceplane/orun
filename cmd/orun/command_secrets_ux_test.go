package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// secretsCommandForTest builds the real `secrets` command tree and returns the
// parent, so tests exercise the actual registered subcommands and aliases.
func secretsCommandForTest(t *testing.T) *cobra.Command {
	t.Helper()
	root := &cobra.Command{Use: "orun"}
	registerSecretsCommand(root)
	for _, c := range root.Commands() {
		if c.Name() == "secrets" {
			return c
		}
	}
	t.Fatal("secrets command was not registered")
	return nil
}

// A typo'd subcommand must produce an error (non-zero exit) with a "did you
// mean" suggestion — not the silent generic help cobra prints by default.
func TestUnknownSecretsSubcommandSuggests(t *testing.T) {
	secretsCmd := secretsCommandForTest(t)
	err := unknownSecretsSubcommand(secretsCmd, "revieal")
	if err == nil {
		t.Fatal("a typo'd subcommand must return an error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "did you mean:") || !strings.Contains(msg, "reveal") {
		t.Errorf("expected a 'did you mean: reveal' suggestion, got:\n%s", msg)
	}
	if !strings.Contains(msg, "unknown subcommand \"revieal\"") {
		t.Errorf("expected the unknown subcommand to be named, got:\n%s", msg)
	}
	if !strings.Contains(msg, "available subcommands:") {
		t.Errorf("expected the available subcommands to be listed, got:\n%s", msg)
	}
}

// Executing the tree with a typo must surface a non-nil error, so main() exits
// non-zero instead of printing help and exiting 0.
func TestSecretsTypoExecuteReturnsError(t *testing.T) {
	root := &cobra.Command{Use: "orun", SilenceUsage: true, SilenceErrors: true}
	registerSecretsCommand(root)
	root.SetArgs([]string{"secrets", "revieal", "SOME_KEY"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected `secrets revieal` to return an error, got nil (would exit 0)")
	}
}

// The subcommand-name helper lists canonical names and, as match candidates,
// includes aliases (so `rm` still resolves) while omitting help/completion.
func TestSecretsSubcommandNames(t *testing.T) {
	secretsCmd := secretsCommandForTest(t)
	canonical, candidates := secretsSubcommandNames(secretsCmd)
	for _, want := range []string{"set", "import", "list", "rotate", "revoke", "versions", "reveal"} {
		if !sliceContains(canonical, want) {
			t.Errorf("canonical names missing %q: %v", want, canonical)
		}
	}
	if sliceContains(canonical, "rm") {
		t.Errorf("aliases must not appear in the printable canonical list: %v", canonical)
	}
	if !sliceContains(candidates, "rm") {
		t.Errorf("candidates must include the revoke alias 'rm' for matching: %v", candidates)
	}
	if sliceContains(canonical, "help") || sliceContains(canonical, "completion") {
		t.Errorf("help/completion must be excluded: %v", canonical)
	}
}

func withRevealFlags(breakGlass bool, reason, env string, project, workspace bool, fn func()) {
	sbg, sr, se, sp, sw := secretsBreakGlass, secretsReason, secretsEnvFlag, secretsProjectFlag, secretsWorkspFlag
	defer func() {
		secretsBreakGlass, secretsReason, secretsEnvFlag, secretsProjectFlag, secretsWorkspFlag = sbg, sr, se, sp, sw
	}()
	secretsBreakGlass, secretsReason, secretsEnvFlag, secretsProjectFlag, secretsWorkspFlag = breakGlass, reason, env, project, workspace
	fn()
}

const (
	whyBreakGlass = "acknowledge this is an audited"
	whyReason     = "recorded in the audit log"
	whyScope      = "which rung to reveal from"
)

// When nothing is supplied, the preflight reports all three preconditions at
// once, plus a ready-to-run example — not one failure at a time.
func TestRevealPreflightReportsAllMissing(t *testing.T) {
	withRevealFlags(false, "", "", false, false, func() {
		err := revealPreflight("CLOUDFLARE_TEST_KEY")
		if err == nil {
			t.Fatal("expected an error when every precondition is missing")
		}
		msg := err.Error()
		for _, want := range []string{whyBreakGlass, whyReason, whyScope, "example:", "CLOUDFLARE_TEST_KEY"} {
			if !strings.Contains(msg, want) {
				t.Errorf("composite error missing %q:\n%s", want, msg)
			}
		}
	})
}

// Only the genuinely-missing preconditions are listed; a supplied reason and
// scope must not appear in the missing list.
func TestRevealPreflightListsOnlyMissing(t *testing.T) {
	withRevealFlags(false, "incident #7", "dev", false, false, func() {
		err := revealPreflight("KEY")
		if err == nil {
			t.Fatal("expected an error when --break-glass is missing")
		}
		msg := err.Error()
		if !strings.Contains(msg, whyBreakGlass) {
			t.Errorf("expected the break-glass line, got:\n%s", msg)
		}
		if strings.Contains(msg, whyReason) {
			t.Errorf("reason was supplied; its missing-line must not appear:\n%s", msg)
		}
		if strings.Contains(msg, whyScope) {
			t.Errorf("scope was supplied; its missing-line must not appear:\n%s", msg)
		}
	})
}

// A workspace-scoped reveal satisfies the scope requirement (regression guard
// for the gap where reveal only accepted --env).
func TestRevealPreflightAcceptsWorkspaceScope(t *testing.T) {
	withRevealFlags(true, "incident #7", "", false, true, func() {
		if err := revealPreflight("KEY"); err != nil {
			t.Errorf("workspace scope should satisfy reveal preflight, got: %v", err)
		}
	})
}

// All preconditions present → no error.
func TestRevealPreflightPasses(t *testing.T) {
	withRevealFlags(true, "incident #7", "dev", false, false, func() {
		if err := revealPreflight("KEY"); err != nil {
			t.Errorf("expected no error when all preconditions are met, got: %v", err)
		}
	})
}

func TestAnySecretsScopeSelector(t *testing.T) {
	cases := []struct {
		env             string
		project, worksp bool
		want            bool
	}{
		{"", false, false, false},
		{"dev", false, false, true},
		{"", true, false, true},
		{"", false, true, true},
	}
	for _, tc := range cases {
		withRevealFlags(false, "", tc.env, tc.project, tc.worksp, func() {
			if got := anySecretsScopeSelector(); got != tc.want {
				t.Errorf("anySecretsScopeSelector(env=%q project=%v ws=%v) = %v, want %v", tc.env, tc.project, tc.worksp, got, tc.want)
			}
		})
	}
}

func sliceContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
