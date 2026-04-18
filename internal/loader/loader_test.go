package loader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadResolvedIntentDiscoversComponentsAndWritesCache(t *testing.T) {
	t.Helper()

	rootDir := t.TempDir()
	intentPath := filepath.Join(rootDir, "intent.yaml")

	writeTestFile(t, intentPath, `apiVersion: sourceplane.io/v1
kind: Intent
metadata:
  name: discovery-test
discovery:
  roots:
    - services
components:
  - name: inline-api
    type: helm
    domain: platform
    path: services/inline-api
`)

	writeTestFile(t, filepath.Join(rootDir, "services", "api", "component.yaml"), `apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: subscribed-api
spec:
  type: helm
  domain: platform
  subscribe:
    environments:
      - development
      - production
  inputs:
    releaseName: subscribed-api
`)

	writeTestFile(t, filepath.Join(rootDir, "infra", "network", "component.yaml"), `apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: ignored-network
spec:
  type: terraform
  domain: platform
`)

	intent, tree, err := LoadResolvedIntent(intentPath)
	if err != nil {
		t.Fatalf("LoadResolvedIntent returned error: %v", err)
	}

	if got := len(intent.Components); got != 2 {
		t.Fatalf("expected 2 merged components, got %d", got)
	}

	var discoveredFound bool
	for _, component := range intent.Components {
		if component.Name != "subscribed-api" {
			continue
		}
		discoveredFound = true
		if component.SourcePath != "services/api/component.yaml" {
			t.Fatalf("expected discovered source path to be preserved, got %q", component.SourcePath)
		}
		if component.Path != "services/api" {
			t.Fatalf("expected discovered component path to default to its directory, got %q", component.Path)
		}
		if len(component.Subscribe.Environments) != 2 {
			t.Fatalf("expected discovered environments to be loaded, got %v", component.Subscribe.Environments)
		}
	}
	if !discoveredFound {
		t.Fatalf("expected discovered component to be merged into the intent")
	}

	if got := len(tree.Components); got != 2 {
		t.Fatalf("expected component tree to contain 2 components, got %d", got)
	}
	if len(tree.Discovery.Roots) != 1 || tree.Discovery.Roots[0] != "services" {
		t.Fatalf("expected discovery roots to be preserved, got %v", tree.Discovery.Roots)
	}

	if err := WriteComponentTreeCache(intentPath, tree); err != nil {
		t.Fatalf("WriteComponentTreeCache returned error: %v", err)
	}

	cachePath := filepath.Join(rootDir, ".arx", "component-tree.yaml")
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected component tree cache to be written: %v", err)
	}
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create test directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file %s: %v", path, err)
	}
}
