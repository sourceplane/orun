package expand

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/normalize"
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

	instances, err := NewExpander(normalized, nil, nil, nil).Expand()
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

func TestResolveControlsFromProfile(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "profile-test"},
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{
				"dry-run": {
					Controls: map[string]map[string]interface{}{
						"terraform": {
							"plan":  map[string]interface{}{"enabled": false},
							"apply": map[string]interface{}{"enabled": false},
						},
					},
				},
			},
		},
		Environments: map[string]model.Environment{
			"dev": {
				Selectors: model.EnvironmentSelectors{Components: []string{"*"}},
				Execution: model.EnvironmentExecution{Profile: "dry-run"},
			},
		},
		Components: []model.Component{
			{Name: "infra-vpc", Type: "terraform"},
		},
	}

	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent error: %v", err)
	}

	instances, err := NewExpander(normalized, nil, nil, nil).Expand()
	if err != nil {
		t.Fatalf("Expand error: %v", err)
	}

	inst := instances["dev"][0]
	plan, ok := inst.Controls["plan"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected controls.plan to be a map, got %T", inst.Controls["plan"])
	}
	if plan["enabled"] != false {
		t.Fatalf("expected plan.enabled=false, got %v", plan["enabled"])
	}
}

func TestResolveControlsPrecedenceOrder(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "precedence-test"},
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{
				"verify": {
					Controls: map[string]map[string]interface{}{
						"terraform": {
							"plan": map[string]interface{}{"enabled": true},
						},
					},
				},
			},
		},
		Environments: map[string]model.Environment{
			"staging": {
				Selectors: model.EnvironmentSelectors{Components: []string{"*"}},
				Execution: model.EnvironmentExecution{
					Profile: "verify",
					ControlOverrides: map[string]map[string]interface{}{
						"terraform": {
							"plan": map[string]interface{}{"enabled": false},
						},
					},
				},
			},
		},
		Components: []model.Component{
			{Name: "infra-vpc", Type: "terraform"},
		},
	}

	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent error: %v", err)
	}

	instances, err := NewExpander(normalized, nil, nil, nil).Expand()
	if err != nil {
		t.Fatalf("Expand error: %v", err)
	}

	inst := instances["staging"][0]
	plan, ok := inst.Controls["plan"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected controls.plan to be a map, got %T", inst.Controls["plan"])
	}
	if plan["enabled"] != false {
		t.Fatalf("env override should win: expected plan.enabled=false, got %v", plan["enabled"])
	}
}

func TestResolveControlsNoProfile(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "no-profile-test"},
		Environments: map[string]model.Environment{
			"dev": {
				Selectors: model.EnvironmentSelectors{Components: []string{"*"}},
			},
		},
		Components: []model.Component{
			{Name: "infra-vpc", Type: "terraform"},
		},
	}

	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent error: %v", err)
	}

	controlDefaults := map[string]map[string]interface{}{
		"terraform": {
			"fmt": map[string]interface{}{"enabled": true},
		},
	}

	instances, err := NewExpander(normalized, controlDefaults, nil, nil).Expand()
	if err != nil {
		t.Fatalf("Expand error: %v", err)
	}

	inst := instances["dev"][0]
	fmt, ok := inst.Controls["fmt"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected controls.fmt to be a map, got %T", inst.Controls["fmt"])
	}
	if fmt["enabled"] != true {
		t.Fatalf("expected fmt.enabled=true from defaults, got %v", fmt["enabled"])
	}
}
