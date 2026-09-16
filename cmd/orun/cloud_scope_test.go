package main

import (
	"context"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/cliauth"
)

// A stored session the platform can no longer turn into a token — access
// expired, nothing left to refresh with — used to get PAST cloudClient,
// because ResolveTokenSource only checks that a session is stored. The first
// request then died with "resolving auth token: file does not exist" (live,
// on `orun baseline new`): the store's own error, naming no remedy. The door
// is where "run `orun auth login`" belongs, and this holds it there.
func TestCloudClientRefusesADeadSessionAtTheDoor(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("ORUN_CREDENTIAL_STORE", "file") // hermetic on a Mac, too
	t.Setenv("ORUN_TOKEN", "")
	t.Setenv("ORUN_TOKEN_FILE", "")
	t.Setenv("GITHUB_ACTIONS", "") // no OIDC short-circuit under CI
	t.Setenv(backendURLEnvVar, "https://cloud.example.com")

	// A session with neither an access token nor a refresh token: stored,
	// and dead. SessionTokenSource reports it as revoked.
	if err := cliauth.SaveSession(&cliauth.Credentials{BackendURL: "https://cloud.example.com"}); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	client, err := cloudClient(context.Background(), "", "ws_dead")
	if err == nil {
		t.Fatalf("cloudClient handed back %v for a session that cannot mint a token", client)
	}
	if !strings.Contains(err.Error(), "orun auth login") {
		t.Fatalf("error = %q, want the `orun auth login` hint", err)
	}
	if strings.Contains(err.Error(), "file does not exist") {
		t.Fatalf("error leaked the raw store failure: %q", err)
	}
}
