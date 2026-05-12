package normalize

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestEnforceOverridePolicy_NilPolicy(t *testing.T) {
	intent := &model.Intent{
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{
				"release": {Controls: map[string]map[string]interface{}{
					"terraform": {"apply": map[string]interface{}{"enabled": true}},
				}},
			},
		},
	}

	err := EnforceOverridePolicy(intent, nil, nil)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestEnforceOverridePolicy_AllowDefault(t *testing.T) {
	policy := &model.StackOverridePolicySpec{Default: "allow"}
	intent := &model.Intent{
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{
				"release": {Controls: map[string]map[string]interface{}{
					"terraform": {"apply": map[string]interface{}{"enabled": true}},
				}},
			},
		},
	}

	err := EnforceOverridePolicy(intent, policy, map[string]model.ExecutionProfile{"release": {}})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestEnforceOverridePolicy_DenyDefault_AllowedOverride(t *testing.T) {
	policy := &model.StackOverridePolicySpec{
		Default: "deny",
		Allow: model.OverridePolicyAllow{
			Intent: model.OverridePolicyIntent{
				Profiles: map[string]model.OverridePolicyProfile{
					"release": {
						Controls: map[string]map[string]model.OverridePolicyControl{
							"terraform": {
								"apply": {},
							},
						},
					},
				},
			},
		},
	}

	intent := &model.Intent{
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{
				"release": {Controls: map[string]map[string]interface{}{
					"terraform": {"apply": map[string]interface{}{"enabled": true}},
				}},
			},
		},
	}

	stackProfiles := map[string]model.ExecutionProfile{"release": {}}

	err := EnforceOverridePolicy(intent, policy, stackProfiles)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestEnforceOverridePolicy_DenyDefault_DisallowedOverride(t *testing.T) {
	policy := &model.StackOverridePolicySpec{
		Default: "deny",
		Allow: model.OverridePolicyAllow{
			Intent: model.OverridePolicyIntent{
				Profiles: map[string]model.OverridePolicyProfile{
					"release": {
						Controls: map[string]map[string]model.OverridePolicyControl{
							"terraform": {
								"plan": {},
							},
						},
					},
				},
			},
		},
	}

	intent := &model.Intent{
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{
				"release": {Controls: map[string]map[string]interface{}{
					"terraform": {"apply": map[string]interface{}{"enabled": true}},
				}},
			},
		},
	}

	stackProfiles := map[string]model.ExecutionProfile{"release": {}}

	err := EnforceOverridePolicy(intent, policy, stackProfiles)
	if err == nil {
		t.Fatal("expected error for disallowed override")
	}
}

func TestEnforceOverridePolicy_ExplicitDenyList(t *testing.T) {
	policy := &model.StackOverridePolicySpec{
		Default: "deny",
		Deny:    []string{"profiles.*.controls.terraform.init"},
		Allow: model.OverridePolicyAllow{
			Intent: model.OverridePolicyIntent{
				Profiles: map[string]model.OverridePolicyProfile{
					"release": {
						Controls: map[string]map[string]model.OverridePolicyControl{
							"terraform": {
								"init": {},
							},
						},
					},
				},
			},
		},
	}

	intent := &model.Intent{
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{
				"release": {Controls: map[string]map[string]interface{}{
					"terraform": {"init": map[string]interface{}{"backend": false}},
				}},
			},
		},
	}

	stackProfiles := map[string]model.ExecutionProfile{"release": {}}

	err := EnforceOverridePolicy(intent, policy, stackProfiles)
	if err == nil {
		t.Fatal("expected error for explicitly denied path")
	}
}

func TestEnforceOverridePolicy_EnvironmentDefaults(t *testing.T) {
	policy := &model.StackOverridePolicySpec{
		Default: "deny",
		Allow: model.OverridePolicyAllow{
			Intent: model.OverridePolicyIntent{
				Environments: model.OverridePolicyEnvironments{
					Defaults: []string{"namespacePrefix", "region"},
				},
			},
		},
	}

	intent := &model.Intent{
		Environments: map[string]model.Environment{
			"dev": {
				Defaults: map[string]interface{}{
					"namespacePrefix": "dev-",
				},
			},
		},
	}

	err := EnforceOverridePolicy(intent, policy, nil)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestEnforceOverridePolicy_EnvironmentDefaults_Denied(t *testing.T) {
	policy := &model.StackOverridePolicySpec{
		Default: "deny",
		Allow: model.OverridePolicyAllow{
			Intent: model.OverridePolicyIntent{
				Environments: model.OverridePolicyEnvironments{
					Defaults: []string{"namespacePrefix"},
				},
			},
		},
	}

	intent := &model.Intent{
		Environments: map[string]model.Environment{
			"dev": {
				Defaults: map[string]interface{}{
					"secretKey": "bad-value",
				},
			},
		},
	}

	err := EnforceOverridePolicy(intent, policy, nil)
	if err == nil {
		t.Fatal("expected error for disallowed environment default")
	}
}

func TestEnforceOverridePolicy_EnvironmentPolicies(t *testing.T) {
	policy := &model.StackOverridePolicySpec{
		Default: "deny",
		Allow: model.OverridePolicyAllow{
			Intent: model.OverridePolicyIntent{
				Environments: model.OverridePolicyEnvironments{
					Policies: []string{"requireApproval"},
				},
			},
		},
	}

	intent := &model.Intent{
		Environments: map[string]model.Environment{
			"prod": {
				Policies: map[string]interface{}{
					"requireApproval": "true",
				},
			},
		},
	}

	err := EnforceOverridePolicy(intent, policy, nil)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestEnforceOverridePolicy_WildcardEnvironmentDefaults(t *testing.T) {
	policy := &model.StackOverridePolicySpec{
		Default: "deny",
		Allow: model.OverridePolicyAllow{
			Intent: model.OverridePolicyIntent{
				Environments: model.OverridePolicyEnvironments{
					Defaults: []string{"cluster.*"},
				},
			},
		},
	}

	intent := &model.Intent{
		Environments: map[string]model.Environment{
			"dev": {
				Defaults: map[string]interface{}{
					"cluster.name": "dev-cluster",
				},
			},
		},
	}

	err := EnforceOverridePolicy(intent, policy, nil)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestEnforceOverridePolicy_IntentOnlyProfile(t *testing.T) {
	policy := &model.StackOverridePolicySpec{
		Default: "deny",
	}

	intent := &model.Intent{
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{
				"custom": {Controls: map[string]map[string]interface{}{
					"terraform": {"apply": map[string]interface{}{"enabled": true}},
				}},
			},
		},
	}

	// "custom" profile is NOT in the stack, so the override check should skip it
	stackProfiles := map[string]model.ExecutionProfile{"release": {}}

	err := EnforceOverridePolicy(intent, policy, stackProfiles)
	if err != nil {
		t.Fatalf("expected nil error for intent-only profile, got: %v", err)
	}
}

func TestMergeStackResources(t *testing.T) {
	normalized := &model.NormalizedIntent{
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{
				"custom": {Description: "intent custom"},
			},
		},
		Automation: model.IntentAutomation{
			Triggers: []model.AutomationTrigger{
				{Name: "intent-trigger"},
			},
		},
	}

	resources := &StackResources{
		Profiles: map[string]model.ExecutionProfile{
			"custom":  {Description: "stack custom"},
			"release": {Description: "stack release"},
		},
		Triggers: []model.AutomationTrigger{
			{Name: "stack-trigger"},
		},
		OverridePolicy: &model.StackOverridePolicySpec{Default: "deny"},
	}

	MergeStackResources(normalized, resources)

	// Intent profile wins on collision
	if normalized.Execution.Profiles["custom"].Description != "intent custom" {
		t.Errorf("expected intent profile to win on collision, got %q", normalized.Execution.Profiles["custom"].Description)
	}

	// Stack profile added when not in intent
	if normalized.Execution.Profiles["release"].Description != "stack release" {
		t.Errorf("expected stack profile 'release' to be added, got %q", normalized.Execution.Profiles["release"].Description)
	}

	// Intent triggers come first, stack triggers appended
	if len(normalized.Automation.Triggers) != 2 {
		t.Fatalf("expected 2 triggers, got %d", len(normalized.Automation.Triggers))
	}
	if normalized.Automation.Triggers[0].Name != "intent-trigger" {
		t.Errorf("expected first trigger to be 'intent-trigger', got %q", normalized.Automation.Triggers[0].Name)
	}
	if normalized.Automation.Triggers[1].Name != "stack-trigger" {
		t.Errorf("expected second trigger to be 'stack-trigger', got %q", normalized.Automation.Triggers[1].Name)
	}

	// Override policy stored
	if normalized.OverridePolicy == nil {
		t.Fatal("expected override policy to be set")
	}
}

func TestMergeStackResources_NilResources(t *testing.T) {
	normalized := &model.NormalizedIntent{
		Execution: model.IntentExecution{
			Profiles: map[string]model.ExecutionProfile{
				"custom": {Description: "intent"},
			},
		},
	}

	MergeStackResources(normalized, nil)

	if normalized.Execution.Profiles["custom"].Description != "intent" {
		t.Errorf("expected no change, got %q", normalized.Execution.Profiles["custom"].Description)
	}
}
