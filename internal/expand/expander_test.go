package expand

import (
	"testing"

	"github.com/sourceplane/arx/internal/model"
	"github.com/sourceplane/arx/internal/normalize"
)

func TestExpandSupportsSubscribedComponentsAlongsideSelectorFallback(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "subscription-test"},
		Environments: map[string]model.Environment{
			"development": {
				Selectors: model.EnvironmentSelectors{
					Components: []string{"legacy-*"},
				},
			},
			"staging": {
				Selectors: model.EnvironmentSelectors{
					Components: []string{"legacy-*"},
				},
			},
			"production": {
				Selectors: model.EnvironmentSelectors{
					Domains: []string{"platform"},
				},
			},
		},
		Components: []model.Component{
			{
				Name:   "legacy-api",
				Type:   "helm",
				Domain: "platform",
			},
			{
				Name:   "subscribed-api",
				Type:   "helm",
				Domain: "platform",
				Subscribe: model.ComponentSubscribe{
					Environments: []string{"development", "production"},
				},
				SourcePath: "services/api/component.yaml",
			},
			{
				Name:   "subscribed-identity",
				Type:   "helm",
				Domain: "identity",
				Subscribe: model.ComponentSubscribe{
					Environments: []string{"production"},
				},
			},
		},
	}

	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent returned error: %v", err)
	}

	instances, err := NewExpander(normalized).Expand()
	if err != nil {
		t.Fatalf("Expand returned error: %v", err)
	}

	assertInstanceNames(t, instances["development"], "legacy-api", "subscribed-api")
	assertInstanceNames(t, instances["staging"], "legacy-api")
	assertInstanceNames(t, instances["production"], "subscribed-api")

	for _, instance := range instances["development"] {
		if instance.ComponentName == "subscribed-api" && instance.SourcePath != "services/api/component.yaml" {
			t.Fatalf("expected subscribed instance to keep source path, got %q", instance.SourcePath)
		}
	}
}

func assertInstanceNames(t *testing.T, instances []*model.ComponentInstance, expected ...string) {
	t.Helper()
	if len(instances) != len(expected) {
		t.Fatalf("expected %d instances, got %d", len(expected), len(instances))
	}
	for index, name := range expected {
		if instances[index].ComponentName != name {
			t.Fatalf("expected instance %d to be %q, got %q", index, name, instances[index].ComponentName)
		}
	}
}
