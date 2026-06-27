package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/model"
)

// TestAutopushEnabled covers the OR of the two sources. The user-config branch
// is exercised via HOME isolation (no file → off); the intent branch is direct.
func TestAutopushEnabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no ~/.orun/config.yaml → cloud.catalog.autopush off

	if autopushEnabled(nil) {
		t.Fatal("autopushEnabled(nil) = true, want false (no config)")
	}
	off := &model.Intent{}
	if autopushEnabled(off) {
		t.Fatal("autopushEnabled(off) = true, want false")
	}
	on := &model.Intent{}
	on.Execution.State.AutopushCatalog = true
	if !autopushEnabled(on) {
		t.Fatal("autopushEnabled(intent autopushCatalog=true) = false, want true")
	}
}

// TestCatalogAutoPublishScope pins the gate: only the clean default branch is
// eligible for auto-publish, so a feature branch / dirty tree / detached state
// never moves the project-wide head.
func TestCatalogAutoPublishScope(t *testing.T) {
	if !catalogAutoPublishScope(catalogmodel.SourceScopeBranchMain) {
		t.Fatal("branch-main must be auto-publishable")
	}
	for _, scope := range []string{
		catalogmodel.SourceScopeBranchFeature,
		catalogmodel.SourceScopeBranchProtected, // conservative: only main for now
		catalogmodel.SourceScopeLocalDirty,
		catalogmodel.SourceScopeLocalNoGit,
		catalogmodel.SourceScopePR,
		catalogmodel.SourceScopeTag,
		catalogmodel.SourceScopeCIEvent,
		"",
	} {
		if catalogAutoPublishScope(scope) {
			t.Fatalf("scope %q must not be auto-publishable", scope)
		}
	}
}

func TestAutopushMarkerRoundTrip(t *testing.T) {
	root := t.TempDir()
	if got := readAutopushMarker(root); got != "" {
		t.Fatalf("readAutopushMarker(empty) = %q, want \"\"", got)
	}
	const digest = "sha256:deadbeef"
	writeAutopushMarker(root, digest)
	if got := readAutopushMarker(root); got != digest {
		t.Fatalf("readAutopushMarker = %q, want %q", got, digest)
	}
}

// TestLoadIntentForCloudConfigReadsAutopushCatalog verifies the new config field
// parses from execution.state.
func TestLoadIntentForCloudConfigReadsAutopushCatalog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "intent.yaml")
	yaml := "apiVersion: orun/v1\nkind: Intent\nmetadata:\n  name: t\n" +
		"execution:\n  state:\n    backendUrl: https://example.test\n    autopushCatalog: true\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	prev := intentFile
	intentFile = path
	t.Cleanup(func() { intentFile = prev })

	intent := loadIntentForCloudConfig()
	if intent == nil {
		t.Fatal("loadIntentForCloudConfig() = nil")
	}
	if !intent.Execution.State.AutopushCatalog {
		t.Fatal("execution.state.autopushCatalog did not parse as true")
	}
}

// TestMaybeAutoPushCatalog_DisabledIsNoOp asserts auto-sync is inert when the
// config flag is off — it must not touch the network, write a marker, or panic.
func TestMaybeAutoPushCatalog_DisabledIsNoOp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(backendURLEnvVar, "")

	dir := t.TempDir()
	path := filepath.Join(dir, "intent.yaml")
	// autopushCatalog absent → defaults false.
	yaml := "apiVersion: orun/v1\nkind: Intent\nmetadata:\n  name: t\n" +
		"execution:\n  state:\n    backendUrl: https://example.test\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	prev := intentFile
	intentFile = path
	t.Cleanup(func() { intentFile = prev })

	// Must return promptly without error/panic.
	maybeAutoPushCatalog(context.Background())
}
