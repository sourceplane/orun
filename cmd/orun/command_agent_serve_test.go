package main

import (
	"strings"
	"testing"
)

// TestCheckServeIdentityEnvPropagationSplit is the diagnostic that turns the
// 30-min silent lease_lost into a one-line routing decision: when the identity
// trio is empty but the session token IS present, the error must name the
// control-plane env-propagation split and point the fix at orun-cloud, not orun.
func TestCheckServeIdentityEnvPropagationSplit(t *testing.T) {
	err := checkServeIdentity("", "", "", "tok-abc")
	if err == nil {
		t.Fatal("empty identity with a present token must be an error")
	}
	msg := err.Error()
	for _, want := range []string{"ORUN_CLOUD_API", "ORUN_ORG_ID", "ORUN_SESSION_ID", "env-propagation", "orun-cloud"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("split diagnostic should mention %q; got: %v", want, err)
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
