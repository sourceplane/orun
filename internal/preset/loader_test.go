package preset

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestLoadPresetsFromDirectory(t *testing.T) {
	dir := t.TempDir()

	// Create stack.yaml
	stackYAML := `apiVersion: orun.io/v1
kind: Stack
metadata:
  name: test-stack
  version: 1.0.0
registry:
  host: ghcr.io
  namespace: test
  repository: stack
spec:
  intentPresets:
    - name: standard
      path: presets/standard.yaml
`
	if err := os.WriteFile(filepath.Join(dir, "stack.yaml"), []byte(stackYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Create preset file
	if err := os.MkdirAll(filepath.Join(dir, "presets"), 0755); err != nil {
		t.Fatal(err)
	}
	presetYAML := `apiVersion: sourceplane.io/v1alpha1
kind: IntentPreset
metadata:
  name: standard
spec:
  env:
    ORG: testorg
  discovery:
    roots:
      - apps/
  environments:
    dev:
      activation:
        triggerRefs:
          - pull-request
      defaults:
        lane: pr
`
	if err := os.WriteFile(filepath.Join(dir, "presets", "standard.yaml"), []byte(presetYAML), 0644); err != nil {
		t.Fatal(err)
	}

	intent := &model.Intent{
		Extends: []model.ExtendsRef{
			{Source: "test-source", Preset: "standard"},
		},
	}
	sourceRoots := map[string]string{"test-source": dir}

	presets, err := LoadPresetsForIntent(intent, sourceRoots)
	if err != nil {
		t.Fatal(err)
	}

	if len(presets) != 1 {
		t.Fatalf("expected 1 preset, got %d", len(presets))
	}

	p := presets[0]
	if p.Preset.Kind != "IntentPreset" {
		t.Errorf("expected kind IntentPreset, got %s", p.Preset.Kind)
	}
	if p.Preset.Spec.Env["ORG"] != "testorg" {
		t.Errorf("expected ORG=testorg, got %s", p.Preset.Spec.Env["ORG"])
	}
	if len(p.Preset.Spec.Discovery.Roots) != 1 || p.Preset.Spec.Discovery.Roots[0] != "apps/" {
		t.Error("expected discovery roots from preset")
	}
	if p.Provenance.Source != "test-source" {
		t.Errorf("expected provenance source test-source, got %s", p.Provenance.Source)
	}
}

func TestLoadPresetsUnknownSource(t *testing.T) {
	intent := &model.Intent{
		Extends: []model.ExtendsRef{
			{Source: "nonexistent", Preset: "standard"},
		},
	}
	sourceRoots := map[string]string{"other": "/tmp/other"}

	_, err := LoadPresetsForIntent(intent, sourceRoots)
	if err == nil {
		t.Fatal("expected error for unknown source")
	}
}

func TestLoadPresetsUnknownPreset(t *testing.T) {
	dir := t.TempDir()

	stackYAML := `apiVersion: orun.io/v1
kind: Stack
metadata:
  name: test-stack
  version: 1.0.0
registry:
  host: ghcr.io
  namespace: test
  repository: stack
spec:
  intentPresets:
    - name: standard
      path: presets/standard.yaml
`
	if err := os.WriteFile(filepath.Join(dir, "stack.yaml"), []byte(stackYAML), 0644); err != nil {
		t.Fatal(err)
	}

	intent := &model.Intent{
		Extends: []model.ExtendsRef{
			{Source: "test-source", Preset: "nonexistent"},
		},
	}
	sourceRoots := map[string]string{"test-source": dir}

	_, err := LoadPresetsForIntent(intent, sourceRoots)
	if err == nil {
		t.Fatal("expected error for unknown preset")
	}
}

func TestLoadPresetsInvalidKind(t *testing.T) {
	dir := t.TempDir()

	stackYAML := `apiVersion: orun.io/v1
kind: Stack
metadata:
  name: test-stack
  version: 1.0.0
registry:
  host: ghcr.io
  namespace: test
  repository: stack
spec:
  intentPresets:
    - name: bad
      path: presets/bad.yaml
`
	if err := os.WriteFile(filepath.Join(dir, "stack.yaml"), []byte(stackYAML), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "presets"), 0755); err != nil {
		t.Fatal(err)
	}
	badYAML := `apiVersion: sourceplane.io/v1alpha1
kind: WrongKind
metadata:
  name: bad
spec:
  env:
    X: Y
`
	if err := os.WriteFile(filepath.Join(dir, "presets", "bad.yaml"), []byte(badYAML), 0644); err != nil {
		t.Fatal(err)
	}

	intent := &model.Intent{
		Extends: []model.ExtendsRef{
			{Source: "test-source", Preset: "bad"},
		},
	}
	sourceRoots := map[string]string{"test-source": dir}

	_, err := LoadPresetsForIntent(intent, sourceRoots)
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}
