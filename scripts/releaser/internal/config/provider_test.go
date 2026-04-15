package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProviderManifestNormalizesCurrentProviderShape(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "provider.yaml")

	content := []byte(`apiVersion: tinx.io/v1
kind: Provider
metadata:
  namespace: sourceplane
  name: lite-ci
  version: v0.0.0
spec:
  runtime: binary
  entrypoint: entrypoint
  platforms:
    - os: darwin
      arch: arm64
      binary: bin/darwin/arm64/entrypoint
    - os: linux
      arch: amd64
      binary: bin/linux/amd64/entrypoint
  layers:
    assets:
      root: assets
`)

	if err := os.WriteFile(manifestPath, content, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	manifest, err := LoadProviderManifest(manifestPath)
	if err != nil {
		t.Fatalf("load provider manifest: %v", err)
	}

	if manifest.Entrypoint.Executable != "entrypoint" {
		t.Fatalf("expected normalized entrypoint, got %q", manifest.Entrypoint.Executable)
	}

	if manifest.Assets.Root != "assets" {
		t.Fatalf("expected normalized assets root, got %q", manifest.Assets.Root)
	}

	if len(manifest.Platforms) != 2 {
		t.Fatalf("expected 2 normalized platforms, got %d", len(manifest.Platforms))
	}

	if manifest.Platforms[0].Binary != "bin/darwin/arm64/entrypoint" {
		t.Fatalf("unexpected first platform binary: %q", manifest.Platforms[0].Binary)
	}
	if manifest.Platforms[1].Binary != "bin/linux/amd64/entrypoint" {
		t.Fatalf("unexpected second platform binary: %q", manifest.Platforms[1].Binary)
	}
}
