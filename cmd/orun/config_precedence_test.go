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
	// flag > env > intent > link (specs/oidc-ci-tenancy §4.1).
	t.Setenv(orgEnvVar, "env-org")
	t.Setenv(projectEnvVar, "env-project")

	// Flags win over env, intent, and link.
	s := resolveScope("flag-org", "flag-project", "intent-org", "intent-project", "link-org", "link-project")
	if s.OrgID != "flag-org" || s.ProjectID != "flag-project" {
		t.Errorf("flag precedence: got %+v", s)
	}
	// Env wins over intent + link when flags empty.
	s = resolveScope("", "", "intent-org", "intent-project", "link-org", "link-project")
	if s.OrgID != "env-org" || s.ProjectID != "env-project" {
		t.Errorf("env precedence: got %+v", s)
	}
	// Intent wins over link when flags + env empty.
	t.Setenv(orgEnvVar, "")
	t.Setenv(projectEnvVar, "")
	s = resolveScope("", "", "intent-org", "intent-project", "link-org", "link-project")
	if s.OrgID != "intent-org" || s.ProjectID != "intent-project" {
		t.Errorf("intent precedence: got %+v", s)
	}
	// Link is the floor when flags + env + intent empty.
	s = resolveScope("", "", "", "", "link-org", "link-project")
	if s.OrgID != "link-org" || s.ProjectID != "link-project" {
		t.Errorf("link precedence: got %+v", s)
	}
	// All empty leaves the scope empty (client defaults to _local).
	s = resolveScope("", "", "", "", "", "")
	if s.OrgID != "" || s.ProjectID != "" {
		t.Errorf("expected empty scope, got %+v", s)
	}
}

func TestIntentScopeAndStrictMode(t *testing.T) {
	// No intent: enforcement off.
	if org, proj, req := intentScope(nil); org != "" || proj != "" || req {
		t.Errorf("nil intent: got %q/%q/%v", org, proj, req)
	}
	// Declaring an org implies strict mode (decision D2).
	in := &model.Intent{}
	in.Execution.State.Org = "org_abc"
	if org, _, req := intentScope(in); org != "org_abc" || !req {
		t.Errorf("declared org should imply requireOrg: got %q/%v", org, req)
	}
	// requireOrg without a declared org is honored.
	in2 := &model.Intent{}
	in2.Execution.State.RequireOrg = true
	if _, _, req := intentScope(in2); !req {
		t.Errorf("explicit requireOrg should be honored")
	}

	// Strict fail-fast only fires when enforcement is on AND no org resolved.
	if err := enforceRequireOrg(true, ""); err == nil {
		t.Errorf("expected fail-fast when requireOrg and no org")
	}
	if err := enforceRequireOrg(true, "org_abc"); err != nil {
		t.Errorf("resolved org should pass strict mode: %v", err)
	}
	if err := enforceRequireOrg(false, ""); err != nil {
		t.Errorf("enforcement off should never fail: %v", err)
	}
}

// TestWorkspaceAliasResolution covers the saas-workspaces A4 aliasing: the
// Workspace spelling leads, the legacy org spelling is still honored, and when
// both are present Workspace wins — for the env layer and the intent layer.
func TestWorkspaceAliasResolution(t *testing.T) {
	// ORUN_WORKSPACE is honored at the env layer.
	t.Setenv(workspaceEnvVar, "ws-env")
	t.Setenv(orgEnvVar, "")
	if s := resolveScope("", "", "", "", "", ""); s.OrgID != "ws-env" {
		t.Errorf("ORUN_WORKSPACE should resolve: got %q", s.OrgID)
	}
	// Legacy ORUN_ORG still works when ORUN_WORKSPACE is unset.
	t.Setenv(workspaceEnvVar, "")
	t.Setenv(orgEnvVar, "org-env")
	if s := resolveScope("", "", "", "", "", ""); s.OrgID != "org-env" {
		t.Errorf("legacy ORUN_ORG should resolve: got %q", s.OrgID)
	}
	// Both set: Workspace wins (A4: read either, prefer workspace).
	t.Setenv(workspaceEnvVar, "ws-env")
	t.Setenv(orgEnvVar, "org-env")
	if s := resolveScope("", "", "", "", "", ""); s.OrgID != "ws-env" {
		t.Errorf("ORUN_WORKSPACE should win over ORUN_ORG: got %q", s.OrgID)
	}
	t.Setenv(workspaceEnvVar, "")
	t.Setenv(orgEnvVar, "")

	// Intent: execution.state.workspace is honored and implies strict mode.
	inWS := &model.Intent{}
	inWS.Execution.State.Workspace = "ws_intent"
	if org, _, req := intentScope(inWS); org != "ws_intent" || !req {
		t.Errorf("execution.state.workspace should resolve + imply strict: got %q/%v", org, req)
	}
	// Intent: legacy execution.state.org still works.
	inOrg := &model.Intent{}
	inOrg.Execution.State.Org = "org_intent"
	if org, _, _ := intentScope(inOrg); org != "org_intent" {
		t.Errorf("legacy execution.state.org should resolve: got %q", org)
	}
	// Intent: both spellings present — workspace wins.
	inBoth := &model.Intent{}
	inBoth.Execution.State.Workspace = "ws_intent"
	inBoth.Execution.State.Org = "org_intent"
	if org, _, _ := intentScope(inBoth); org != "ws_intent" {
		t.Errorf("execution.state.workspace should win over .org: got %q", org)
	}
}
