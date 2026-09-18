package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

// applyEnv makes env the process environment for the rest of the test, the
// way the bootstrap child sees it, with nothing ambient to hide behind.
func applyEnv(t *testing.T, env []string) {
	t.Helper()
	for _, k := range []string{"ORUN_TOKEN", "ORUN_TOKEN_FILE", "ORUN_SESSION_TOKEN", "ORUN_WORKSPACE",
		"ORUN_BACKEND_URL", "ORUN_CLOUD_API", "GITHUB_ACTIONS", "ACTIONS_ID_TOKEN_REQUEST_URL", "ACTIONS_ID_TOKEN_REQUEST_TOKEN"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			t.Setenv(k, v)
		}
	}
}

// What a platform sandbox gives serve: the session trio, the boot token, the
// workspace and the build's inputs — and no CLI login.
var sandboxEnv = []string{
	"ORUN_CLOUD_API=https://api.example.test",
	"ORUN_ORG_ID=org_1",
	"ORUN_SESSION_ID=ses_1",
	"ORUN_SESSION_TOKEN=boot-token",
	"ORUN_WORKSPACE=ws_1",
	"ORUN_BASELINE_ID=cirrus@baseline-v9",
}

// The console build's child `orun baseline new` could not authenticate: the
// CLI's token chain reads ORUN_TOKEN, ORUN_TOKEN_FILE or a stored login, and a
// sandbox has none of them — only ORUN_SESSION_TOKEN. So its first platform
// call died before its event sink existed, and the build page said "Nothing
// reported yet" until the sweep failed the session.
func TestTheBootstrapChildResolvesTheSessionCredential(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "session-token")
	if err := os.WriteFile(tokenFile, []byte("rotated-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	getenv := func(k string) string {
		for _, kv := range sandboxEnv {
			if name, v, ok := strings.Cut(kv, "="); ok && name == k {
				return v
			}
		}
		return ""
	}
	child := bootstrapChildEnv(sandboxEnv, getenv, tokenFile, "")

	applyEnv(t, child)
	auth, err := remotestate.ResolveAuth(context.Background(), remotestate.ResolveOptions{
		BackendURL: os.Getenv("ORUN_BACKEND_URL"), Version: "test", RequireLogin: true, Org: "ws_1",
	})
	if err != nil {
		t.Fatalf("the child could not resolve a credential: %v", err)
	}
	if auth.ResolvedMode != "file" {
		t.Errorf("resolved %q, want the rotating session file", auth.ResolvedMode)
	}
	if tok, err := auth.TokenSource.Token(context.Background()); err != nil || tok != "rotated-token" {
		t.Errorf("token = %q, %v; want the session file's current token", tok, err)
	}
	if got := os.Getenv("ORUN_BACKEND_URL"); got != "https://api.example.test" {
		t.Errorf("ORUN_BACKEND_URL = %q; the child must dial the platform serve dials", got)
	}
	if got := os.Getenv("ORUN_BASELINE_ID"); got != "cirrus@baseline-v9" {
		t.Errorf("the build's own inputs were lost from the child env: ORUN_BASELINE_ID=%q", got)
	}
}

// The failure this replaces, pinned: serve's environment alone does not
// authenticate an `orun` child.
func TestServesOwnEnvironmentDoesNotAuthenticateAChild(t *testing.T) {
	applyEnv(t, sandboxEnv)
	_, err := remotestate.ResolveAuth(context.Background(), remotestate.ResolveOptions{
		BackendURL: "https://api.example.test", Version: "test", RequireLogin: true, Org: "ws_1",
	})
	if err == nil || !strings.Contains(err.Error(), "no local Orun login found") {
		t.Fatalf("want the live failure, got %v", err)
	}
}

// The event sink follows the rotating credential rather than the boot token,
// which serve replaces about every 15 minutes of an hour-long build.
func TestTheEventSinkFollowsTheRotatingToken(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "session-token")
	env := map[string]string{"ORUN_SESSION_TOKEN": "boot-token", "ORUN_TOKEN_FILE": tokenFile}
	src := eventSinkTokenSource(func(k string) string { return env[k] })
	for _, want := range []string{"first-rotation", "second-rotation"} {
		if err := os.WriteFile(tokenFile, []byte(want), 0o600); err != nil {
			t.Fatal(err)
		}
		if got, err := src.Token(context.Background()); err != nil || got != want {
			t.Fatalf("token = %q, %v; want %q", got, err, want)
		}
	}
	delete(env, "ORUN_TOKEN_FILE")
	if got, _ := eventSinkTokenSource(func(k string) string { return env[k] }).Token(context.Background()); got != "boot-token" {
		t.Errorf("without a token file the boot token is the fallback, got %q", got)
	}
	if eventSinkTokenSource(func(string) string { return "" }) != nil {
		t.Error("no credential at all should mean no sink")
	}
}

// A GROUNDED build is placed in its grounded clone — the product repository
// itself — so a Build after a stopped one resumes where the repository is,
// instead of placing phase 01 into an empty directory and refusing to land it
// over a main that already holds phase 02.
func TestAGroundedBuildIsPlacedInItsClone(t *testing.T) {
	env := append(append([]string{}, sandboxEnv...), "ORUN_BASELINE_OUT=/home/daytona/product")
	getenv := func(string) string { return "" }
	child := bootstrapChildEnv(env, getenv, "/tmp/token", "/home/daytona/work/newne")
	if got := lastValue(child, "ORUN_BASELINE_OUT"); got != "/home/daytona/work/newne" {
		t.Fatalf("a grounded build places into %q, want its clone", got)
	}
	// No clone, no change: the platform's directory stands.
	ungrounded := bootstrapChildEnv(env, getenv, "/tmp/token", "")
	if got := lastValue(ungrounded, "ORUN_BASELINE_OUT"); got != "/home/daytona/product" {
		t.Fatalf("an ungrounded build places into %q, want the platform's directory", got)
	}
}

func lastValue(env []string, key string) string {
	v := ""
	for _, kv := range env {
		if k, val, ok := strings.Cut(kv, "="); ok && k == key {
			v = val
		}
	}
	return v
}
