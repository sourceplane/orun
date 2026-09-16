package main

import (
	"testing"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/model"
)

func TestResolveBackendURLWithConfigPrefersExplicitSources(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := cliauth.SaveConfig(&cliauth.Config{Backend: cliauth.BackendConfig{URL: "https://config.example.com"}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	t.Setenv(backendURLEnvVar, "https://env.example.com")
	intent := &model.Intent{Execution: model.IntentExecution{State: model.IntentExecutionState{BackendURL: "https://intent.example.com"}}}

	if got := resolveBackendURLWithConfig(intent, "https://flag.example.com"); got != "https://flag.example.com" {
		t.Fatalf("resolveBackendURLWithConfig(flag) = %q", got)
	}
	if got := resolveBackendURLWithConfig(intent, ""); got != "https://env.example.com" {
		t.Fatalf("resolveBackendURLWithConfig(env) = %q", got)
	}
	t.Setenv(backendURLEnvVar, "")
	if got := resolveBackendURLWithConfig(intent, ""); got != "https://intent.example.com" {
		t.Fatalf("resolveBackendURLWithConfig(intent) = %q", got)
	}
	if got := resolveBackendURLWithConfig(nil, ""); got != "https://config.example.com" {
		t.Fatalf("resolveBackendURLWithConfig(config) = %q", got)
	}
}

// THE LAST RUNG. With nothing naming a backend — no flag, no env, no intent,
// no config file — the CLI dials the production API rather than refusing.
// Asserted separately from the precedence test above so the two claims stay
// distinct: that one proves every explicit source wins; this one proves there
// is somewhere to fall to, and that it is the right somewhere.
func TestResolveBackendURLFallsToTheProductionAPI(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp) // no ~/.orun/config.yaml
	t.Setenv(backendURLEnvVar, "")

	got := resolveBackendURLWithConfig(nil, "")
	if got != defaultCloudURL {
		t.Fatalf("resolveBackendURLWithConfig(nothing) = %q, want the default %q", got, defaultCloudURL)
	}
	if got == "" {
		t.Fatal("the chain has no last rung")
	}
	// And requireBackendURL, which used to refuse here, now answers.
	if u, err := requireBackendURL(nil, ""); err != nil || u != defaultCloudURL {
		t.Fatalf("requireBackendURL(nothing) = %q, %v", u, err)
	}
}

// A config file that exists but names nothing must not shadow the default:
// `ResolvedBackendURL` returns "" for it, and returning that "" verbatim was
// the old behaviour.
func TestResolveBackendURLIgnoresAnEmptyConfigFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv(backendURLEnvVar, "")
	if err := cliauth.SaveConfig(&cliauth.Config{}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if got := resolveBackendURLWithConfig(nil, ""); got != defaultCloudURL {
		t.Fatalf("an empty config file shadowed the default: %q", got)
	}
}

func TestParseGitHubRepoFullName(t *testing.T) {
	cases := map[string]string{
		"git@github.com:sourceplane/orun.git":     "sourceplane/orun",
		"ssh://git@github.com/sourceplane/orun":   "sourceplane/orun",
		"https://github.com/sourceplane/orun.git": "sourceplane/orun",
	}
	for input, want := range cases {
		if got := parseGitHubRepoFullName(input); got != want {
			t.Fatalf("parseGitHubRepoFullName(%q) = %q, want %q", input, got, want)
		}
	}
}
