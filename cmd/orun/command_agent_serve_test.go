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
