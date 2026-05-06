package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestCollectChangedComponents_ComponentManifestChangeOnlyMatchesOwningComponent(t *testing.T) {
	normalized := &model.NormalizedIntent{
		Components: map[string]model.Component{
			"docs-site-direct-upload": {
				Name:       "docs-site-direct-upload",
				Path:       "website",
				SourcePath: "website/component.yaml",
			},
			"api-edge-worker": {
				Name:       "api-edge-worker",
				Path:       "apps/api-edge",
				SourcePath: "apps/api-edge/component.yaml",
			},
		},
	}

	instances := map[string][]*model.ComponentInstance{
		"production": {
			{ComponentName: "docs-site-direct-upload", Path: "website"},
			{ComponentName: "api-edge-worker", Path: "apps/api-edge"},
		},
	}

	changed := collectChangedComponents(normalized, instances, map[string]struct{}{
		"website/component.yaml": {},
	}, "intent.yaml")

	if !changed["docs-site-direct-upload"] {
		t.Fatal("expected docs-site-direct-upload to be marked changed")
	}
	if changed["api-edge-worker"] {
		t.Fatal("did not expect api-edge-worker to be marked changed by another component manifest")
	}
}

func TestCollectChangedComponents_IntentChangeMarksAllComponents(t *testing.T) {
	normalized := &model.NormalizedIntent{
		Components: map[string]model.Component{
			"docs-site-direct-upload": {
				Name:       "docs-site-direct-upload",
				Path:       "website",
				SourcePath: "website/component.yaml",
			},
			"api-edge-worker": {
				Name:       "api-edge-worker",
				Path:       "apps/api-edge",
				SourcePath: "apps/api-edge/component.yaml",
			},
		},
	}

	changed := collectChangedComponents(normalized, nil, map[string]struct{}{
		"nested/intent.yaml": {},
	}, "intent.yaml")

	if !changed["docs-site-direct-upload"] || !changed["api-edge-worker"] {
		t.Fatal("expected intent.yaml change to mark all components changed")
	}
}

func TestCollectChangedComponents_AbsoluteIntentPathMatchesRelativeChangedFiles(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	normalized := &model.NormalizedIntent{
		Components: map[string]model.Component{
			"docs-site-direct-upload": {
				Name:       "docs-site-direct-upload",
				Path:       "website",
				SourcePath: "website/component.yaml",
			},
			"api-edge-worker": {
				Name:       "api-edge-worker",
				Path:       "apps/api-edge",
				SourcePath: "apps/api-edge/component.yaml",
			},
		},
	}

	// Simulate auto-discovery returning an absolute intentPath (the real scenario that was broken).
	absIntentPath := filepath.Join(cwd, "intent.yaml")
	changed := collectChangedComponents(normalized, nil, map[string]struct{}{
		"website/component.yaml": {},
	}, absIntentPath)

	if !changed["docs-site-direct-upload"] {
		t.Fatal("expected docs-site-direct-upload to be changed when using absolute intentPath")
	}
	if changed["api-edge-worker"] {
		t.Fatal("did not expect api-edge-worker to be changed")
	}
}
