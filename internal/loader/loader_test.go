package loader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/model"
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

	cachePath := filepath.Join(rootDir, ".orun", "component-tree.yaml")
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

func TestLoadResolvedIntent_PreservesComponentRootEnv(t *testing.T) {
	rootDir := t.TempDir()
	intentPath := filepath.Join(rootDir, "intent.yaml")

	writeTestFile(t, intentPath, `apiVersion: sourceplane.io/v1
kind: Intent
metadata:
  name: env-preservation-test
discovery:
  roots:
    - services
components:
  - name: inline-api
    type: helm
    domain: platform
`)

	writeTestFile(t, filepath.Join(rootDir, "services", "api", "component.yaml"), `apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: subscribed-api
spec:
  type: helm
  domain: platform
  env:
    REGION: us-east-1
    DEBUG: "true"
    DB_URL: postgres://localhost/mydb
  subscribe:
    environments:
      - development
`)

	intent, _, err := LoadResolvedIntent(intentPath)
	if err != nil {
		t.Fatalf("LoadResolvedIntent returned error: %v", err)
	}

	var discovered *model.Component
	for i, c := range intent.Components {
		if c.Name == "subscribed-api" {
			discovered = &intent.Components[i]
			break
		}
	}
	if discovered == nil {
		t.Fatal("expected discovered component 'subscribed-api' to be found in merged intent")
	}

	expected := map[string]string{
		"REGION":  "us-east-1",
		"DEBUG":   "true",
		"DB_URL":  "postgres://localhost/mydb",
	}
	for k, want := range expected {
		if got := discovered.Env[k]; got != want {
			t.Errorf("discovered component Env[%q] = %q, want %q", k, got, want)
		}
	}
	if len(discovered.Env) != len(expected) {
		t.Errorf("discovered component Env has %d keys, want %d: %v", len(discovered.Env), len(expected), discovered.Env)
	}
}

func TestLoadResolvedIntent_RoundTripThroughCachePreservesEnv(t *testing.T) {
	rootDir := t.TempDir()
	intentPath := filepath.Join(rootDir, "intent.yaml")

	writeTestFile(t, intentPath, `apiVersion: sourceplane.io/v1
kind: Intent
metadata:
  name: cache-env-test
discovery:
  roots:
    - services
components:
  - name: inline-api
    type: helm
    domain: platform
`)

	writeTestFile(t, filepath.Join(rootDir, "services", "api", "component.yaml"), `apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: cached-api
spec:
  type: terraform
  env:
    CACHE_KEY: preserved
    LOG_LEVEL: debug
  subscribe:
    environments:
      - development
`)

	// First load: writes the cache
	intent1, tree, err := LoadResolvedIntent(intentPath)
	if err != nil {
		t.Fatalf("first LoadResolvedIntent failed: %v", err)
	}
	if err := WriteComponentTreeCache(intentPath, tree); err != nil {
		t.Fatalf("WriteComponentTreeCache failed: %v", err)
	}

	var firstComp *model.Component
	for i, c := range intent1.Components {
		if c.Name == "cached-api" {
			firstComp = &intent1.Components[i]
			break
		}
	}
	if firstComp == nil {
		t.Fatal("expected cached-api in first load")
	}
	if firstComp.Env["CACHE_KEY"] != "preserved" {
		t.Fatalf("first load: expected Env[CACHE_KEY]='preserved', got %q", firstComp.Env["CACHE_KEY"])
	}

	// Second load: reads from cache (same content, same mtime)
	intent2, _, err := LoadResolvedIntent(intentPath)
	if err != nil {
		t.Fatalf("second LoadResolvedIntent (from cache) failed: %v", err)
	}

	var secondComp *model.Component
	for i, c := range intent2.Components {
		if c.Name == "cached-api" {
			secondComp = &intent2.Components[i]
			break
		}
	}
	if secondComp == nil {
		t.Fatal("expected cached-api in second load")
	}

	expected := map[string]string{
		"CACHE_KEY": "preserved",
		"LOG_LEVEL": "debug",
	}
	for k, want := range expected {
		if got := secondComp.Env[k]; got != want {
			t.Errorf("cached load: Env[%q] = %q, want %q", k, got, want)
		}
	}
	if len(secondComp.Env) != len(expected) {
		t.Errorf("cached load: Env has %d keys, want %d: %v", len(secondComp.Env), len(expected), secondComp.Env)
	}
}
