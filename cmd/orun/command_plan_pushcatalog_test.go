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

// TestPushCatalogAfterPlan_NothingConfiguredDialsTheCloud: with no
// --backend-url, ORUN_BACKEND_URL, intent or config file, the backend chain
// ends at the production API instead of refusing — so the explicit flag gets
// past the URL and fails on the NEXT gate (this bare directory has no
// catalogs/current), still loud and actionable, never a panic or a silent
// no-op. (This test used to want a "backend URL" hint here; that described
// the chain before it had a last rung.)
func TestPushCatalogAfterPlan_NothingConfiguredDialsTheCloud(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ORUN_BACKEND_URL", "")
	t.Chdir(t.TempDir()) // no local catalog here

	saved := intentFile
	intentFile = "" // no intent → loadIntentForCloudConfig returns nil
	t.Cleanup(func() { intentFile = saved })

	err := pushCatalogAfterPlan(context.Background())
	if err == nil {
		t.Fatal("pushCatalogAfterPlan = nil in an empty directory, want the no-catalog error")
	}
	if strings.Contains(err.Error(), "backend URL") {
		t.Fatalf("error = %q: the chain refused instead of falling to the default", err.Error())
	}
	if !strings.Contains(err.Error(), "no local catalog") {
		t.Fatalf("error = %q, want the gate after the URL (no local catalog)", err.Error())
	}
}
