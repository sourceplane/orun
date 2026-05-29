package trigger

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestValidateDependencyRules_Valid(t *testing.T) {
	intent := &model.Intent{
		Automation: model.AutomationConfig{
			TriggerBindings: map[string]model.TriggerBinding{
				"github-pull-request": {On: model.TriggerMatch{Provider: "github", Event: "pull_request"}},
				"github-push-main":    {On: model.TriggerMatch{Provider: "github", Event: "push"}},
			},
		},
		Environments: map[string]model.Environment{
			"dev-preview": {DependencyMode: model.DependencyModeAdvisory},
			"staging":     {DependencyMode: model.DependencyModeEnforced},
		},
		Components: []model.Component{
			{
				Name: "api",
				Type: "terraform",
				Subscribe: model.ComponentSubscribe{
					Environments: []model.EnvironmentSubscription{
						{
							Name:           "dev-preview",
							DependencyMode: model.DependencyModeAdvisory,
							DependencyRules: []model.DependencyRule{
								{Mode: model.DependencyModeEnforced, When: model.DependencyRuleWhen{TriggerRef: "github-push-main"}},
							},
						},
					},
				},
			},
		},
	}
	if errs := ValidateDependencyRules(intent); len(errs) > 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestValidateDependencyRules_InvalidEnvMode(t *testing.T) {
	intent := &model.Intent{
		Environments: map[string]model.Environment{
			"bad": {DependencyMode: "fast"},
		},
	}
	errs := ValidateDependencyRules(intent)
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), "enforced|advisory|disabled") {
		t.Fatalf("expected invalid-mode error, got %v", errs)
	}
}

func TestValidateDependencyRules_UnknownTriggerRef(t *testing.T) {
	intent := &model.Intent{
		Automation: model.AutomationConfig{
			TriggerBindings: map[string]model.TriggerBinding{
				"existing": {On: model.TriggerMatch{Provider: "github", Event: "push"}},
			},
		},
		Components: []model.Component{
			{
				Name: "api",
				Subscribe: model.ComponentSubscribe{
					Environments: []model.EnvironmentSubscription{
						{
							Name: "dev",
							DependencyRules: []model.DependencyRule{
								{Mode: model.DependencyModeAdvisory, When: model.DependencyRuleWhen{TriggerRef: "nope"}},
							},
						},
					},
				},
			},
		},
	}
	errs := ValidateDependencyRules(intent)
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), "does not exist") {
		t.Fatalf("expected unknown-triggerRef error, got %v", errs)
	}
}

func TestValidateDependencyRules_RuleMissingMode(t *testing.T) {
	intent := &model.Intent{
		Automation: model.AutomationConfig{
			TriggerBindings: map[string]model.TriggerBinding{
				"github-pull-request": {On: model.TriggerMatch{Provider: "github", Event: "pull_request"}},
			},
		},
		Components: []model.Component{
			{
				Name: "api",
				Subscribe: model.ComponentSubscribe{
					Environments: []model.EnvironmentSubscription{
						{
							Name: "dev",
							DependencyRules: []model.DependencyRule{
								{When: model.DependencyRuleWhen{TriggerRef: "github-pull-request"}},
							},
						},
					},
				},
			},
		},
	}
	errs := ValidateDependencyRules(intent)
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), ".mode is required") {
		t.Fatalf("expected missing-mode error, got %v", errs)
	}
}
