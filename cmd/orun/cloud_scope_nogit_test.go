package main

import (
	"context"
	"strings"
	"testing"
)

// `orun baseline new --local --workspace ws_… --out <dir>` names its workspace
// outright and builds into --out, so the checkout you happen to be standing in
// is irrelevant — but cloudClient treated the cached repo link as a
// prerequisite and died outside a git repo with `detect git remote.origin.url:
// exit status 1`. The same command one directory over worked. The link is a
// FALLBACK for the workspace; a fallback that cannot be consulted is not an
// error when something else already answered.
func TestCloudClientWorksOutsideAGitRepo(t *testing.T) {
	t.Chdir(t.TempDir()) // no .git anywhere above
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ORUN_TOKEN", "sk_test_not_a_real_key")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv(backendURLEnvVar, "")

	client, err := cloudClient(context.Background(), "https://cloud.example.com", "ws_named")
	if err != nil {
		t.Fatalf("cloudClient outside a git repo = %v, want a client", err)
	}
	if client == nil {
		t.Fatal("cloudClient returned no client and no error")
	}
	if got := client.Scope().OrgID; got != "ws_named" {
		t.Fatalf("scope OrgID = %q, want the workspace the caller named", got)
	}
}

// With nothing naming a workspace AND no link to consult, the refusal must say
// both things: what to pass, and that the link was never read — otherwise
// "link the repo" reads as advice that was already tried.
func TestCloudClientSaysWhyTheLinkWasNotConsulted(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ORUN_TOKEN", "sk_test_not_a_real_key")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv(backendURLEnvVar, "")
	t.Setenv(workspaceEnvVar, "")
	t.Setenv(orgEnvVar, "")

	_, err := cloudClient(context.Background(), "https://cloud.example.com", "")
	if err == nil {
		t.Fatal("cloudClient = nil error with no workspace named anywhere")
	}
	for _, want := range []string{"no workspace resolved", "--workspace", workspaceEnvVar, "not consulted"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, missing %q", err.Error(), want)
		}
	}
}
