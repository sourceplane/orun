package composition

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"gopkg.in/yaml.v3"
)

func TestLoadStackProfiles_ExplicitPaths(t *testing.T) {
	rootDir := t.TempDir()
	profilesDir := filepath.Join(rootDir, "profiles")
	os.MkdirAll(profilesDir, 0755)

	writeProfileFile(t, filepath.Join(profilesDir, "dry-run.yaml"), "dry-run", "changed", map[string]map[string]interface{}{
		"terraform": {"apply": map[string]interface{}{"enabled": false}},
	})
	writeProfileFile(t, filepath.Join(profilesDir, "release.yaml"), "release", "full", map[string]map[string]interface{}{
		"terraform": {"apply": map[string]interface{}{"enabled": true}},
	})

	stack := model.Stack{
		Spec: model.StackSpec{
			Profiles: []model.StackExportEntry{
				{Name: "dry-run", Path: "profiles/dry-run.yaml"},
				{Name: "release", Path: "profiles/release.yaml"},
			},
		},
	}

	profiles, err := loadStackProfiles(rootDir, stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}

	if profiles["dry-run"].Plan.Scope != "changed" {
		t.Errorf("expected dry-run scope=changed, got %q", profiles["dry-run"].Plan.Scope)
	}
	if profiles["release"].Plan.Scope != "full" {
		t.Errorf("expected release scope=full, got %q", profiles["release"].Plan.Scope)
	}
}

func TestLoadStackProfiles_AutoDiscover(t *testing.T) {
	rootDir := t.TempDir()
	profilesDir := filepath.Join(rootDir, "profiles")
	os.MkdirAll(profilesDir, 0755)

	writeProfileFile(t, filepath.Join(profilesDir, "verify.yaml"), "verify", "changed", nil)

	stack := model.Stack{Spec: model.StackSpec{}}

	profiles, err := loadStackProfiles(rootDir, stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}

	if _, ok := profiles["verify"]; !ok {
		t.Errorf("expected profile 'verify' to be discovered")
	}
}

func TestLoadStackProfiles_NoProfilesDir(t *testing.T) {
	rootDir := t.TempDir()
	stack := model.Stack{Spec: model.StackSpec{}}

	profiles, err := loadStackProfiles(rootDir, stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(profiles) != 0 {
		t.Errorf("expected 0 profiles, got %d", len(profiles))
	}
}

func TestLoadStackTriggers_ExplicitPaths(t *testing.T) {
	rootDir := t.TempDir()
	triggersDir := filepath.Join(rootDir, "triggers")
	os.MkdirAll(triggersDir, 0755)

	writeTriggerFile(t, filepath.Join(triggersDir, "pr.yaml"), "pr", "github", "pull_request", "dry-run")

	stack := model.Stack{
		Spec: model.StackSpec{
			Triggers: []model.StackExportEntry{
				{Name: "pr", Path: "triggers/pr.yaml"},
			},
		},
	}

	triggers, err := loadStackTriggers(rootDir, stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(triggers) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(triggers))
	}
	if triggers[0].Name != "pr" {
		t.Errorf("expected trigger name 'pr', got %q", triggers[0].Name)
	}
	if triggers[0].Plan.Profile != "dry-run" {
		t.Errorf("expected profile ref 'dry-run', got %q", triggers[0].Plan.Profile)
	}
}

func TestLoadStackTriggers_AutoDiscover(t *testing.T) {
	rootDir := t.TempDir()
	triggersDir := filepath.Join(rootDir, "triggers")
	os.MkdirAll(triggersDir, 0755)

	writeTriggerFile(t, filepath.Join(triggersDir, "push-main.yaml"), "push-main", "github", "push", "verify")

	stack := model.Stack{Spec: model.StackSpec{}}

	triggers, err := loadStackTriggers(rootDir, stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(triggers) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(triggers))
	}
	if triggers[0].Name != "push-main" {
		t.Errorf("expected trigger name 'push-main', got %q", triggers[0].Name)
	}
}

func TestLoadStackTriggers_NoTriggersDir(t *testing.T) {
	rootDir := t.TempDir()
	stack := model.Stack{Spec: model.StackSpec{}}

	triggers, err := loadStackTriggers(rootDir, stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if triggers != nil {
		t.Errorf("expected nil triggers, got %v", triggers)
	}
}

func TestLoadStackOverridePolicy(t *testing.T) {
	rootDir := t.TempDir()

	policy := model.StackOverridePolicyDocument{
		Kind: "StackOverridePolicy",
		Spec: model.StackOverridePolicySpec{
			Default: "deny",
			Allow: model.OverridePolicyAllow{
				Intent: model.OverridePolicyIntent{
					Environments: model.OverridePolicyEnvironments{
						Defaults: []string{"namespacePrefix"},
					},
				},
			},
		},
	}

	data, _ := yaml.Marshal(policy)
	os.WriteFile(filepath.Join(rootDir, "policy.yaml"), data, 0644)

	stack := model.Stack{
		Spec: model.StackSpec{
			OverridePolicy: &model.StackExportEntry{Name: "default", Path: "policy.yaml"},
		},
	}

	result, err := loadStackOverridePolicy(rootDir, stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected policy, got nil")
	}
	if result.Default != "deny" {
		t.Errorf("expected default=deny, got %q", result.Default)
	}
	if len(result.Allow.Intent.Environments.Defaults) != 1 {
		t.Errorf("expected 1 allowed default, got %d", len(result.Allow.Intent.Environments.Defaults))
	}
}

func TestLoadStackOverridePolicy_NoPolicyDeclared(t *testing.T) {
	rootDir := t.TempDir()
	stack := model.Stack{Spec: model.StackSpec{}}

	result, err := loadStackOverridePolicy(rootDir, stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil, got %+v", result)
	}
}

func TestLoadStackOverridePolicy_AutoDiscover(t *testing.T) {
	rootDir := t.TempDir()
	policiesDir := filepath.Join(rootDir, "policies")
	os.MkdirAll(policiesDir, 0755)

	doc := model.StackOverridePolicyDocument{
		Kind: "StackOverridePolicy",
		Spec: model.StackOverridePolicySpec{
			Default: "deny",
			Allow: model.OverridePolicyAllow{
				Intent: model.OverridePolicyIntent{
					Environments: model.OverridePolicyEnvironments{
						Defaults: []string{"region"},
					},
				},
			},
		},
	}
	data, _ := yaml.Marshal(doc)
	os.WriteFile(filepath.Join(policiesDir, "override-policy.yaml"), data, 0644)

	stack := model.Stack{Spec: model.StackSpec{}}

	result, err := loadStackOverridePolicy(rootDir, stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected auto-discovered policy, got nil")
	}
	if result.Default != "deny" {
		t.Errorf("expected default=deny, got %q", result.Default)
	}
	if len(result.Allow.Intent.Environments.Defaults) != 1 || result.Allow.Intent.Environments.Defaults[0] != "region" {
		t.Errorf("unexpected allowed defaults: %v", result.Allow.Intent.Environments.Defaults)
	}
}

func TestLoadStackOverridePolicy_AutoDiscoverSkipsNonPolicyDocs(t *testing.T) {
	rootDir := t.TempDir()
	policiesDir := filepath.Join(rootDir, "policies")
	os.MkdirAll(policiesDir, 0755)

	// Write a non-policy YAML file first (alphabetically before "override")
	nonPolicy := map[string]interface{}{
		"kind":       "SomethingElse",
		"apiVersion": "v1",
	}
	data, _ := yaml.Marshal(nonPolicy)
	os.WriteFile(filepath.Join(policiesDir, "a-not-a-policy.yaml"), data, 0644)

	// Write the actual policy
	doc := model.StackOverridePolicyDocument{
		Kind: "StackOverridePolicy",
		Spec: model.StackOverridePolicySpec{Default: "allow"},
	}
	data, _ = yaml.Marshal(doc)
	os.WriteFile(filepath.Join(policiesDir, "override-policy.yaml"), data, 0644)

	stack := model.Stack{Spec: model.StackSpec{}}

	result, err := loadStackOverridePolicy(rootDir, stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected auto-discovered policy, got nil")
	}
	if result.Default != "allow" {
		t.Errorf("expected default=allow, got %q", result.Default)
	}
}

func TestLoadStackOverridePolicy_NoPoliciesDir(t *testing.T) {
	rootDir := t.TempDir()
	stack := model.Stack{Spec: model.StackSpec{}}

	result, err := loadStackOverridePolicy(rootDir, stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil when no policies/ directory, got %+v", result)
	}
}

func TestLoadStackResourcesIntoRegistry(t *testing.T) {
	rootDir := t.TempDir()
	profilesDir := filepath.Join(rootDir, "profiles")
	triggersDir := filepath.Join(rootDir, "triggers")
	os.MkdirAll(profilesDir, 0755)
	os.MkdirAll(triggersDir, 0755)

	writeProfileFile(t, filepath.Join(profilesDir, "release.yaml"), "release", "full", nil)
	writeTriggerFile(t, filepath.Join(triggersDir, "push.yaml"), "push", "github", "push", "release")

	stack := model.Stack{
		APIVersion: "orun.io/v1",
		Kind:       "Stack",
		Metadata:   model.StackMetadata{Name: "test"},
		Spec: model.StackSpec{
			Profiles: []model.StackExportEntry{{Name: "release", Path: "profiles/release.yaml"}},
			Triggers: []model.StackExportEntry{{Name: "push", Path: "triggers/push.yaml"}},
		},
	}

	data, _ := yaml.Marshal(stack)
	os.WriteFile(filepath.Join(rootDir, "stack.yaml"), data, 0644)

	registry := &Registry{
		Profiles: make(map[string]model.ExecutionProfile),
	}

	err := loadStackResourcesIntoRegistry(registry, rootDir, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(registry.Profiles) != 1 {
		t.Errorf("expected 1 profile, got %d", len(registry.Profiles))
	}
	if len(registry.Triggers) != 1 {
		t.Errorf("expected 1 trigger, got %d", len(registry.Triggers))
	}
}

func writeProfileFile(t *testing.T, path, name, scope string, controls map[string]map[string]interface{}) {
	t.Helper()
	doc := model.ExecutionProfileDocument{
		APIVersion: "sourceplane.io/v1",
		Kind:       "ExecutionProfile",
		Metadata:   model.Metadata{Name: name},
		Spec: model.ExecutionProfileSpec{
			Plan:     model.ProfilePlan{Scope: scope},
			Controls: controls,
		},
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("failed to marshal profile: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("failed to write profile: %v", err)
	}
}

func writeTriggerFile(t *testing.T, path, name, provider, event, profileRef string) {
	t.Helper()
	doc := model.TriggerBindingDocument{
		APIVersion: "sourceplane.io/v1",
		Kind:       "TriggerBinding",
		Metadata:   model.Metadata{Name: name},
		Spec: model.TriggerBindingSpec{
			On:   model.TriggerOn{Provider: provider, Event: event},
			Plan: model.TriggerPlanRef{ProfileRef: profileRef},
		},
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("failed to marshal trigger: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("failed to write trigger: %v", err)
	}
}
