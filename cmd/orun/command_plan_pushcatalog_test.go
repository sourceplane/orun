package main

import (
	"context"
	"strings"
	"testing"
)

// TestPlanPushCatalogFlagRegistered guards the wiring of the explicit opt-in.
func TestPlanPushCatalogFlagRegistered(t *testing.T) {
	f := planCmd.Flags().Lookup("push-catalog")
	if f == nil {
		t.Fatal("plan --push-catalog flag is not registered")
	}
	if f.Value.Type() != "bool" {
		t.Fatalf("push-catalog flag type = %q, want bool", f.Value.Type())
	}
}

// TestPushCatalogAfterPlan_NoBackendConfigured asserts the explicit flag fails
// loud with an actionable message when no backend is configured — rather than
// panicking or silently doing nothing.
func TestPushCatalogAfterPlan_NoBackendConfigured(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ORUN_BACKEND_URL", "")

	saved := intentFile
	intentFile = "" // no intent → loadIntentForCloudConfig returns nil
	t.Cleanup(func() { intentFile = saved })

	err := pushCatalogAfterPlan(context.Background())
	if err == nil {
		t.Fatal("pushCatalogAfterPlan = nil, want a 'missing backend URL' error")
	}
	if !strings.Contains(err.Error(), "backend URL") {
		t.Fatalf("error = %q, want a backend-URL hint", err.Error())
	}
}
