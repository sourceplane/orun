package main

import (
	"context"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/agent"
	"github.com/sourceplane/orun/internal/nodes"
)

// TestServeInitsBareObjectStore reproduces the cloud-serve boot condition: a
// bare sandbox cwd with no `.orun` (no prior `orun plan`). serve's guard used to
// return "no object store — pull the brief first" and exit 1, leaving every
// session's console dark. The fix initializes an empty store; assert the store
// materializes and an interactive brief seals into it from env alone.
func TestServeInitsBareObjectStore(t *testing.T) {
	t.Chdir(t.TempDir())

	// Bare cwd: the strict guard reports "no store", exactly the serve path.
	if _, _, _, ok := openObjectStores(); ok {
		t.Fatal("precondition: a bare cwd must have no object store")
	}

	// The fix's fallback: initialize an empty writable store.
	store, _, _, err := openObjectModel()
	if err != nil {
		t.Fatalf("openObjectModel on bare cwd should initialize a store: %v", err)
	}

	// And an interactive brief (no task/spec/persona) seals into it — proving
	// serve needs only a writable store, not a pre-pulled graph.
	brief, err := agent.AssembleBrief(context.Background(), store, agent.BriefInput{
		RunKind: nodes.RunKindInteractive,
	})
	if err != nil {
		t.Fatalf("assemble interactive brief into fresh store: %v", err)
	}
	if brief.ID == "" {
		t.Fatal("assembled brief must have a content id")
	}

	// After init the strict guard now passes — the store is real on disk.
	if _, _, _, ok := openObjectStores(); !ok {
		t.Fatal("after init, openObjectStores must succeed")
	}
}

// TestCheckServeIdentityMissingWithToken: when the identity trio is empty but
// the session token IS present, the error must flag that serve can't heartbeat
// and point at the toolbox-exec/orun-cloud bootstrap as the likely cause —
// without ever echoing the token.
func TestCheckServeIdentityMissingWithToken(t *testing.T) {
	err := checkServeIdentity("", "", "", "tok-abc")
	if err == nil {
		t.Fatal("empty identity with a present token must be an error")
	}
	msg := err.Error()
	for _, want := range []string{"ORUN_CLOUD_API", "ORUN_ORG_ID", "ORUN_SESSION_ID", "heartbeat", "orun-cloud"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("diagnostic should mention %q; got: %v", want, err)
		}
	}
	if strings.Contains(msg, "tok-abc") {
		t.Fatal("the diagnostic must never echo the token value")
	}
}

func TestCheckServeIdentityAllPresent(t *testing.T) {
	if err := checkServeIdentity("https://api", "org_1", "as_1", "tok"); err != nil {
		t.Fatalf("all four present should pass: %v", err)
	}
}

// TestCheckServeIdentityTotalMiss: with even the token absent it is not the
// export-prefix split (that injects the token), so the plain missing-env error
// is the honest one — don't misroute it to orun-cloud.
func TestCheckServeIdentityTotalMiss(t *testing.T) {
	err := checkServeIdentity("", "", "", "")
	if err == nil {
		t.Fatal("all-empty must be an error")
	}
	if strings.Contains(err.Error(), "env-propagation") {
		t.Fatalf("all-empty is not the export-prefix split; got: %v", err)
	}
}

func TestRedactSecretNeverLeaks(t *testing.T) {
	if got := redactSecret(""); got != "<MISSING>" {
		t.Fatalf("empty token should be <MISSING>, got %q", got)
	}
	got := redactSecret("super-secret-token")
	if strings.Contains(got, "super-secret-token") {
		t.Fatalf("redaction leaked the token: %q", got)
	}
	if !strings.Contains(got, "len=") {
		t.Fatalf("redaction should report length, got %q", got)
	}
}

// TestHarnessPlatformEnvSeedsBackendURL: the harness env carries the backend
// URL the CLI resolves from, seeded from the dial-home identity, so the
// `orun mcp serve` it spawns mounts the platform plane instead of booting
// "degraded: no backend URL" (the skipped-Step-1b transcript). An explicit
// ORUN_BACKEND_URL in the sandbox still wins; no identity, nothing seeded.
func TestHarnessPlatformEnvSeedsBackendURL(t *testing.T) {
	envOf := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	has := func(env []string, want string) bool {
		for _, e := range env {
			if e == want {
				return true
			}
		}
		return false
	}

	env := harnessPlatformEnv(envOf(map[string]string{
		"ORUN_CLOUD_API": "https://api-edge-test.oruncloud.workers.dev",
		"ORUN_WORKSPACE": "org_1",
	}), "/tmp/tok")
	for _, want := range []string{
		"ORUN_TOKEN_FILE=/tmp/tok",
		"ORUN_WORKSPACE=org_1",
		"ORUN_BACKEND_URL=https://api-edge-test.oruncloud.workers.dev",
	} {
		if !has(env, want) {
			t.Errorf("harness env missing %q: %v", want, env)
		}
	}

	env = harnessPlatformEnv(envOf(map[string]string{
		"ORUN_CLOUD_API":   "https://api-edge-test.oruncloud.workers.dev",
		"ORUN_BACKEND_URL": "https://backend.example",
	}), "/tmp/tok")
	if has(env, "ORUN_BACKEND_URL=https://api-edge-test.oruncloud.workers.dev") {
		t.Errorf("an explicit ORUN_BACKEND_URL must not be overridden: %v", env)
	}

	env = harnessPlatformEnv(envOf(map[string]string{}), "/tmp/tok")
	for _, e := range env {
		if strings.HasPrefix(e, "ORUN_BACKEND_URL=") || strings.HasPrefix(e, "ORUN_WORKSPACE=") {
			t.Errorf("nothing to seed from must seed nothing: %v", env)
		}
	}
}
