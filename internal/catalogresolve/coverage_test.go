package catalogresolve

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverAndLoad_YmlForm_AnnotationPerKeyInherit(t *testing.T) {
	root := fixturePath(t, "yml_form")
	res, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("DiscoverAndLoad: %v", err)
	}
	if len(res.Manifests) != 1 {
		t.Fatalf("manifests = %d, want 1", len(res.Manifests))
	}
	m := res.Manifests[0]
	if !strings.HasSuffix(m.SourceFile, ".yml") {
		t.Errorf("SourceFile = %q, want .yml form", m.SourceFile)
	}
	// authored annotation `pre-existing: authored` must win.
	if got := m.Component.Metadata.Annotations["pre-existing"]; got != "authored" {
		t.Errorf("authored annotation clobbered: got %q, want \"authored\"", got)
	}
	// intent default `added-by-intent` must be filled (cold-start —
	// authored map merges with new key from defaults).
	if got := m.Component.Metadata.Annotations["added-by-intent"]; got != "hello" {
		t.Errorf("inherited annotation = %q, want \"hello\"", got)
	}
	// authored had no labels; cold-start inheritance allocates the map.
	if got := m.Component.Metadata.Labels["filled"]; got != "yes" {
		t.Errorf("inherited label = %q, want \"yes\"", got)
	}
}

func TestDiscoverAndLoad_RelativeIntentPath(t *testing.T) {
	root := fixturePath(t, "canonical")
	res, err := DiscoverAndLoad(context.Background(), Options{
		WorkspaceRoot: root,
		IntentPath:    "intent.yaml",
	})
	if err != nil {
		t.Fatalf("DiscoverAndLoad: %v", err)
	}
	if res.IntentPath != "intent.yaml" {
		t.Errorf("IntentPath = %q, want \"intent.yaml\"", res.IntentPath)
	}
}

func TestDiscoverAndLoad_AbsoluteIntentPath(t *testing.T) {
	root := fixturePath(t, "canonical")
	abs, err := filepath.Abs(filepath.Join(root, "intent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := DiscoverAndLoad(context.Background(), Options{
		WorkspaceRoot: root,
		IntentPath:    abs,
	})
	if err != nil {
		t.Fatalf("DiscoverAndLoad: %v", err)
	}
	if res.IntentPath == "" {
		t.Errorf("IntentPath empty when absolute path supplied")
	}
}

func TestDiscoverAndLoad_WorkspaceRootIsFile(t *testing.T) {
	root := fixturePath(t, "canonical")
	asFile := filepath.Join(root, "intent.yaml")
	_, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: asFile})
	var werr *ErrWorkspaceInvalid
	if !errors.As(err, &werr) {
		t.Fatalf("error type = %T, want *ErrWorkspaceInvalid: %v", err, err)
	}
	if !strings.Contains(werr.Error(), "not a directory") {
		t.Errorf("Error() = %q, want substring \"not a directory\"", werr.Error())
	}
}

func TestDiscoverAndLoad_ContextCancelled(t *testing.T) {
	root := fixturePath(t, "canonical")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := DiscoverAndLoad(ctx, Options{WorkspaceRoot: root})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestDiscoverAndLoad_EmptyWorkspaceProducesEmptyResult(t *testing.T) {
	root := t.TempDir()
	// empty dir — no component.yaml, no intent.yaml.
	res, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("DiscoverAndLoad on empty dir: %v", err)
	}
	if len(res.Manifests) != 0 {
		t.Errorf("manifests = %d, want 0", len(res.Manifests))
	}
	if res.IntentPath != "" {
		t.Errorf("IntentPath = %q, want empty", res.IntentPath)
	}
}

func TestDiscoverAndLoad_IntentReadError(t *testing.T) {
	root := t.TempDir()
	intentPath := filepath.Join(root, "intent.yaml")
	// Write a file we'll then make unreadable.
	if err := os.WriteFile(intentPath, []byte("catalog: {}\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(intentPath, 0o644) })
	// On platforms where the test runner is root, mode 0o000 is still
	// readable; treat that as a skip rather than a failure.
	if _, err := os.ReadFile(intentPath); err == nil {
		t.Skip("file is still readable (likely running as root); skipping permission test")
	}
	_, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	var bad *ErrIntentInvalid
	if !errors.As(err, &bad) {
		t.Fatalf("error type = %T, want *ErrIntentInvalid: %v", err, err)
	}
}
