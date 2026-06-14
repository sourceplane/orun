package main

import (
	"testing"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/model"
)

// TestResolveBackendURLPrecedence covers the full design §8 chain:
// flag > env > repo intent > user config, where user config prefers cloud.url.
func TestResolveBackendURLPrecedence(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := cliauth.SaveConfig(&cliauth.Config{
		Cloud: cliauth.CloudConfig{URL: "https://cloud.example.com"},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	intent := &model.Intent{Execution: model.IntentExecution{State: model.IntentExecutionState{BackendURL: "https://intent.example.com"}}}

	// Flag wins.
	t.Setenv(backendURLEnvVar, "https://env.example.com")
	if got := resolveBackendURLWithConfig(intent, "https://flag.example.com"); got != "https://flag.example.com" {
		t.Errorf("flag precedence: got %q", got)
	}
	// Env beats intent + config.
	if got := resolveBackendURLWithConfig(intent, ""); got != "https://env.example.com" {
		t.Errorf("env precedence: got %q", got)
	}
	// Intent beats config.
	t.Setenv(backendURLEnvVar, "")
	if got := resolveBackendURLWithConfig(intent, ""); got != "https://intent.example.com" {
		t.Errorf("intent precedence: got %q", got)
	}
	// Config cloud.url is the floor.
	if got := resolveBackendURLWithConfig(nil, ""); got != "https://cloud.example.com" {
		t.Errorf("config cloud.url: got %q", got)
	}
}

func TestResolveBackendURL_CloudPreferredOverDeprecatedBackend(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := cliauth.SaveConfig(&cliauth.Config{
		Cloud:   cliauth.CloudConfig{URL: "https://cloud.example.com"},
		Backend: cliauth.BackendConfig{URL: "https://legacy.example.com"},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if got := resolveBackendURLWithConfig(nil, ""); got != "https://cloud.example.com" {
		t.Errorf("cloud.url should win over backend.url, got %q", got)
	}
}

func TestResolveBackendURL_DeprecatedBackendAliasHonored(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// Only the deprecated backend.url is set.
	if err := cliauth.SaveConfig(&cliauth.Config{
		Backend: cliauth.BackendConfig{URL: "https://legacy.example.com"},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	backendURLDeprecationWarned = false
	if got := resolveBackendURLWithConfig(nil, ""); got != "https://legacy.example.com" {
		t.Errorf("backend.url alias should be honored, got %q", got)
	}
}

func TestResolveScopePrecedence(t *testing.T) {
	// flag > env > link.
	t.Setenv(orgEnvVar, "env-org")
	t.Setenv(projectEnvVar, "env-project")

	// Flags win over env and link.
	s := resolveScope("flag-org", "flag-project", "link-org", "link-project")
	if s.OrgID != "flag-org" || s.ProjectID != "flag-project" {
		t.Errorf("flag precedence: got %+v", s)
	}
	// Env wins over link when flags empty.
	s = resolveScope("", "", "link-org", "link-project")
	if s.OrgID != "env-org" || s.ProjectID != "env-project" {
		t.Errorf("env precedence: got %+v", s)
	}
	// Link is the floor when flags + env empty.
	t.Setenv(orgEnvVar, "")
	t.Setenv(projectEnvVar, "")
	s = resolveScope("", "", "link-org", "link-project")
	if s.OrgID != "link-org" || s.ProjectID != "link-project" {
		t.Errorf("link precedence: got %+v", s)
	}
	// All empty leaves the scope empty (client defaults to _local).
	s = resolveScope("", "", "", "")
	if s.OrgID != "" || s.ProjectID != "" {
		t.Errorf("expected empty scope, got %+v", s)
	}
}
