package preset

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestMergePresetsNoExtends(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "test"},
		Env:      map[string]string{"KEY": "value"},
	}
	result, err := MergePresets(intent, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Intent.Env["KEY"] != "value" {
		t.Error("expected intent unchanged when no presets")
	}
}

func TestMergePresetsEnvDeepMergeRepoWins(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "test"},
		Env:      map[string]string{"SHARED": "repo-value", "REPO_ONLY": "yes"},
	}
	presets := []*ResolvedPreset{{
		Preset: model.IntentPreset{
			Spec: model.IntentPresetSpec{
				Env: map[string]string{"SHARED": "preset-value", "PRESET_ONLY": "yes"},
			},
		},
		Provenance: model.PresetProvenance{Source: "src", Preset: "p1"},
	}}

	result, err := MergePresets(intent, presets)
	if err != nil {
		t.Fatal(err)
	}

	if result.Intent.Env["SHARED"] != "repo-value" {
		t.Errorf("expected repo to win for SHARED, got %q", result.Intent.Env["SHARED"])
	}
	if result.Intent.Env["PRESET_ONLY"] != "yes" {
		t.Error("expected preset-only env var to be added")
	}
	if result.Intent.Env["REPO_ONLY"] != "yes" {
		t.Error("expected repo-only env var to be preserved")
	}
}

func TestMergePresetsDiscoveryRootsUnion(t *testing.T) {
	intent := &model.Intent{
		Metadata:  model.Metadata{Name: "test"},
		Discovery: model.Discovery{Roots: []string{"apps/", "infra/"}},
	}
	presets := []*ResolvedPreset{{
		Preset: model.IntentPreset{
			Spec: model.IntentPresetSpec{
				Discovery: model.Discovery{Roots: []string{"infra/", "deploy/", "charts/"}},
			},
		},
		Provenance: model.PresetProvenance{Source: "src", Preset: "p1"},
	}}

	result, err := MergePresets(intent, presets)
	if err != nil {
		t.Fatal(err)
	}

	roots := result.Intent.Discovery.Roots
	if len(roots) != 4 {
		t.Fatalf("expected 4 roots (union), got %d: %v", len(roots), roots)
	}
	expected := map[string]bool{"apps/": true, "infra/": true, "deploy/": true, "charts/": true}
	for _, r := range roots {
		if !expected[r] {
			t.Errorf("unexpected root: %s", r)
		}
	}
}

func TestMergePresetsEnvironmentsDeepMerge(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "test"},
		Environments: map[string]model.Environment{
			"production": {
				Defaults: map[string]interface{}{"region": "us-east-1", "replicas": 3},
			},
		},
	}
	presets := []*ResolvedPreset{{
		Preset: model.IntentPreset{
			Spec: model.IntentPresetSpec{
				Environments: map[string]model.Environment{
					"production": {
						Defaults: map[string]interface{}{"region": "eu-west-1", "lane": "release"},
					},
				},
			},
		},
		Provenance: model.PresetProvenance{Source: "src", Preset: "p1"},
	}}

	result, err := MergePresets(intent, presets)
	if err != nil {
		t.Fatal(err)
	}

	prod := result.Intent.Environments["production"]
	if prod.Defaults["region"] != "us-east-1" {
		t.Errorf("expected repo to win for region, got %v", prod.Defaults["region"])
	}
	if prod.Defaults["lane"] != "release" {
		t.Errorf("expected preset lane default to be added, got %v", prod.Defaults["lane"])
	}
	if prod.Defaults["replicas"] != 3 {
		t.Errorf("expected repo replicas preserved, got %v", prod.Defaults["replicas"])
	}
}

func TestMergePresetsNewEnvironmentFromPreset(t *testing.T) {
	intent := &model.Intent{
		Metadata:     model.Metadata{Name: "test"},
		Environments: map[string]model.Environment{},
	}
	presets := []*ResolvedPreset{{
		Preset: model.IntentPreset{
			Spec: model.IntentPresetSpec{
				Environments: map[string]model.Environment{
					"staging": {
						Defaults: map[string]interface{}{"lane": "verify"},
						Activation: model.EnvironmentActivation{
							TriggerRefs: []string{"push-main"},
						},
					},
				},
			},
		},
		Provenance: model.PresetProvenance{Source: "src", Preset: "p1"},
	}}

	result, err := MergePresets(intent, presets)
	if err != nil {
		t.Fatal(err)
	}

	staging, exists := result.Intent.Environments["staging"]
	if !exists {
		t.Fatal("expected staging environment from preset to be added")
	}
	if staging.Defaults["lane"] != "verify" {
		t.Errorf("expected lane default, got %v", staging.Defaults["lane"])
	}
	if len(staging.Activation.TriggerRefs) != 1 || staging.Activation.TriggerRefs[0] != "push-main" {
		t.Error("expected trigger ref from preset")
	}
}

func TestMergePresetsTriggerBindingsMergeByName(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "test"},
		Automation: model.AutomationConfig{
			TriggerBindings: map[string]model.TriggerBinding{
				"push-main": {On: model.TriggerMatch{Provider: "github", Event: "push"}},
			},
		},
	}
	presets := []*ResolvedPreset{{
		Preset: model.IntentPreset{
			Spec: model.IntentPresetSpec{
				Automation: model.AutomationConfig{
					TriggerBindings: map[string]model.TriggerBinding{
						"push-main":     {On: model.TriggerMatch{Provider: "github", Event: "push-preset"}},
						"pull-request":  {On: model.TriggerMatch{Provider: "github", Event: "pull_request"}},
					},
				},
			},
		},
		Provenance: model.PresetProvenance{Source: "src", Preset: "p1"},
	}}

	result, err := MergePresets(intent, presets)
	if err != nil {
		t.Fatal(err)
	}

	bindings := result.Intent.Automation.TriggerBindings
	if bindings["push-main"].On.Event != "push" {
		t.Error("expected repo trigger binding to win for push-main")
	}
	if _, exists := bindings["pull-request"]; !exists {
		t.Error("expected new preset trigger binding to be added")
	}
}

func TestMergePresetsGroupDefaultsDeepMerge(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "test"},
		Groups: map[string]model.Group{
			"platform": {
				Defaults: map[string]interface{}{"version": "1.0"},
			},
		},
	}
	presets := []*ResolvedPreset{{
		Preset: model.IntentPreset{
			Spec: model.IntentPresetSpec{
				Groups: map[string]model.Group{
					"platform": {
						Defaults: map[string]interface{}{"version": "2.0", "backend": "s3"},
					},
				},
			},
		},
		Provenance: model.PresetProvenance{Source: "src", Preset: "p1"},
	}}

	result, err := MergePresets(intent, presets)
	if err != nil {
		t.Fatal(err)
	}

	group := result.Intent.Groups["platform"]
	if group.Defaults["version"] != "1.0" {
		t.Errorf("expected repo to win for version, got %v", group.Defaults["version"])
	}
	if group.Defaults["backend"] != "s3" {
		t.Error("expected preset backend default to be added")
	}
}

func TestMergePresetsPoliciesAdditive(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "test"},
		Environments: map[string]model.Environment{
			"production": {
				Policies: map[string]interface{}{"repoPolicy": true},
			},
		},
	}
	presets := []*ResolvedPreset{{
		Preset: model.IntentPreset{
			Spec: model.IntentPresetSpec{
				Environments: map[string]model.Environment{
					"production": {
						Policies: map[string]interface{}{"presetPolicy": true},
					},
				},
			},
		},
		Provenance: model.PresetProvenance{Source: "src", Preset: "p1"},
	}}

	result, err := MergePresets(intent, presets)
	if err != nil {
		t.Fatal(err)
	}

	policies := result.Intent.Environments["production"].Policies
	if policies["repoPolicy"] != true {
		t.Error("expected repo policy preserved")
	}
	if policies["presetPolicy"] != true {
		t.Error("expected preset policy to be added (additive)")
	}
}

func TestMergePresetsMultiplePresetsOrder(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "test"},
		Env:      map[string]string{"TOP": "repo"},
	}
	presets := []*ResolvedPreset{
		{
			Preset: model.IntentPreset{
				Spec: model.IntentPresetSpec{
					Env: map[string]string{"FIRST": "first-value", "CONFLICT": "first"},
				},
			},
			Provenance: model.PresetProvenance{Source: "s1", Preset: "p1"},
		},
		{
			Preset: model.IntentPreset{
				Spec: model.IntentPresetSpec{
					Env: map[string]string{"SECOND": "second-value", "CONFLICT": "second"},
				},
			},
			Provenance: model.PresetProvenance{Source: "s2", Preset: "p2"},
		},
	}

	result, err := MergePresets(intent, presets)
	if err != nil {
		t.Fatal(err)
	}

	if result.Intent.Env["TOP"] != "repo" {
		t.Error("expected repo env to be preserved")
	}
	if result.Intent.Env["FIRST"] != "first-value" {
		t.Error("expected first preset env added")
	}
	if result.Intent.Env["SECOND"] != "second-value" {
		t.Error("expected second preset env added")
	}
	// First preset sets CONFLICT, second cannot overwrite (repo-wins logic applies to first-set value)
	if result.Intent.Env["CONFLICT"] != "first" {
		t.Errorf("expected first preset to win for CONFLICT (set first), got %q", result.Intent.Env["CONFLICT"])
	}
}

func TestMergePresetsDeterminism(t *testing.T) {
	intent := &model.Intent{
		Metadata:     model.Metadata{Name: "test"},
		Env:          map[string]string{"A": "1"},
		Environments: map[string]model.Environment{"prod": {Defaults: map[string]interface{}{"x": "y"}}},
		Groups:       map[string]model.Group{"g1": {Defaults: map[string]interface{}{"d": "v"}}},
	}
	presets := []*ResolvedPreset{{
		Preset: model.IntentPreset{
			Spec: model.IntentPresetSpec{
				Env:          map[string]string{"B": "2", "C": "3"},
				Discovery:    model.Discovery{Roots: []string{"a/", "b/"}},
				Environments: map[string]model.Environment{"staging": {Defaults: map[string]interface{}{"lane": "verify"}}},
				Groups:       map[string]model.Group{"g2": {Defaults: map[string]interface{}{"e": "f"}}},
			},
		},
		Provenance: model.PresetProvenance{Source: "src", Preset: "p1"},
	}}

	var firstEnv map[string]string
	for i := 0; i < 100; i++ {
		result, err := MergePresets(intent, presets)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			firstEnv = result.Intent.Env
		} else {
			for k, v := range firstEnv {
				if result.Intent.Env[k] != v {
					t.Fatalf("non-deterministic on iteration %d: key %s, expected %q got %q", i, k, v, result.Intent.Env[k])
				}
			}
		}
	}
}

func TestMergePresetsProvenance(t *testing.T) {
	intent := &model.Intent{
		Metadata:     model.Metadata{Name: "test"},
		Environments: map[string]model.Environment{},
	}
	presets := []*ResolvedPreset{{
		Preset: model.IntentPreset{
			Spec: model.IntentPresetSpec{
				Env:          map[string]string{"KEY": "val"},
				Environments: map[string]model.Environment{"dev": {Defaults: map[string]interface{}{"lane": "pr"}}},
			},
		},
		Provenance: model.PresetProvenance{Source: "aws-platform", Preset: "github-actions"},
	}}

	result, err := MergePresets(intent, presets)
	if err != nil {
		t.Fatal(err)
	}

	if _, exists := result.Provenance["env.KEY"]; !exists {
		t.Error("expected provenance for env.KEY")
	}
	if _, exists := result.Provenance["environments.dev"]; !exists {
		t.Error("expected provenance for environments.dev")
	}
	if result.Provenance["env.KEY"][0].Source != "aws-platform" {
		t.Error("expected provenance source to be aws-platform")
	}
}
