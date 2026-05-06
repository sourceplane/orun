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

func TestParseGitHubRepoFullName(t *testing.T) {
	cases := map[string]string{
		"git@github.com:sourceplane/orun.git":    "sourceplane/orun",
		"ssh://git@github.com/sourceplane/orun":  "sourceplane/orun",
		"https://github.com/sourceplane/orun.git": "sourceplane/orun",
	}
	for input, want := range cases {
		if got := parseGitHubRepoFullName(input); got != want {
			t.Fatalf("parseGitHubRepoFullName(%q) = %q, want %q", input, got, want)
		}
	}
}
