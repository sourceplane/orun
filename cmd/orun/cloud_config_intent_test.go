package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

// TestLoadIntentForCloudConfigReadsBackendURL verifies that the auth/cloud
// command groups pick up execution.state.backendUrl straight from the
// discovered intent.yaml (UO0) — the case the reproduced bug failed on.
func TestLoadIntentForCloudConfigReadsBackendURL(t *testing.T) {
	t.Setenv(backendURLEnvVar, "")
	dir := t.TempDir()
	path := filepath.Join(dir, "intent.yaml")
	const backend = "https://api-edge-stage.oruncloud.workers.dev"
	yaml := "apiVersion: orun/v1\nkind: Intent\nmetadata:\n  name: t\nexecution:\n  state:\n    mode: remote\n    backendUrl: " + backend + "\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	prev := intentFile
	intentFile = path
	t.Cleanup(func() { intentFile = prev })

	intent := loadIntentForCloudConfig()
	if intent == nil {
		t.Fatal("loadIntentForCloudConfig() = nil, want intent")
	}
	if got := intent.Execution.State.BackendURL; got != backend {
		t.Fatalf("intent backendUrl = %q, want %q", got, backend)
	}
	// The auth/cloud commands resolve through requireBackendURL with this intent
	// and no flag/env: the intent layer must win with no error.
	got, err := requireBackendURL(intent, "")
	if err != nil {
		t.Fatalf("requireBackendURL() error = %v", err)
	}
	if got != backend {
		t.Fatalf("requireBackendURL() = %q, want %q", got, backend)
	}
}

// TestLoadIntentForCloudConfigMissingReturnsNil verifies the helper is
// best-effort: outside a repo (or with no intent.yaml) it returns nil so
// resolution falls through to flag/env/user-config exactly as before.
func TestLoadIntentForCloudConfigMissingReturnsNil(t *testing.T) {
	prev := intentFile
	intentFile = filepath.Join(t.TempDir(), "does-not-exist.yaml")
	t.Cleanup(func() { intentFile = prev })

	if intent := loadIntentForCloudConfig(); intent != nil {
		t.Fatalf("loadIntentForCloudConfig() = %+v, want nil", intent)
	}
}

// TestCommandResolvesCloudConfig pins which command groups opt into
// intent-based backend-URL discovery: the auth and cloud groups yes; run and
// the OCI-registry `login` no (so the catalog auto-refresh hook and registry
// login are unaffected).
func TestCommandResolvesCloudConfig(t *testing.T) {
	root := &cobra.Command{Use: "orun"}
	auth := &cobra.Command{Use: "auth"}
	authLogin := &cobra.Command{Use: "login"}
	auth.AddCommand(authLogin)
	cloud := &cobra.Command{Use: "cloud"}
	cloudLink := &cobra.Command{Use: "link"}
	cloud.AddCommand(cloudLink)
	registryLogin := &cobra.Command{Use: "login"} // OCI registry login (command_publish.go)
	run := &cobra.Command{Use: "run"}
	root.AddCommand(auth, cloud, registryLogin, run)

	for _, c := range []*cobra.Command{auth, authLogin, cloud, cloudLink} {
		if !commandResolvesCloudConfig(c) {
			t.Errorf("commandResolvesCloudConfig(%q) = false, want true", c.Name())
		}
	}
	for _, c := range []*cobra.Command{registryLogin, run} {
		if commandResolvesCloudConfig(c) {
			t.Errorf("commandResolvesCloudConfig(%q) = true, want false", c.Name())
		}
	}
}
