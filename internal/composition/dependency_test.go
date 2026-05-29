package composition

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestResolveDependencyMode_DefaultEnforced(t *testing.T) {
	got, err := ResolveDependencyMode(model.Environment{}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Mode != model.DependencyModeEnforced || got.Source != "default" {
		t.Fatalf("want default/enforced, got %+v", got)
	}
}

func TestResolveDependencyMode_EnvironmentLevel(t *testing.T) {
	env := model.Environment{DependencyMode: model.DependencyModeAdvisory}
	got, err := ResolveDependencyMode(env, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Mode != model.DependencyModeAdvisory || got.Source != "environment" {
		t.Fatalf("want environment/advisory, got %+v", got)
	}
}

func TestResolveDependencyMode_SubscriptionOverridesEnv(t *testing.T) {
	env := model.Environment{DependencyMode: model.DependencyModeAdvisory}
	sub := &model.EnvironmentSubscription{DependencyMode: model.DependencyModeEnforced}
	got, err := ResolveDependencyMode(env, sub, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Mode != model.DependencyModeEnforced || got.Source != "subscription" {
		t.Fatalf("want subscription/enforced, got %+v", got)
	}
}

func TestResolveDependencyMode_SubscriptionRuleFirstMatchWins(t *testing.T) {
	sub := &model.EnvironmentSubscription{
		DependencyMode: model.DependencyModeEnforced,
		DependencyRules: []model.DependencyRule{
			{Mode: model.DependencyModeAdvisory, When: model.DependencyRuleWhen{TriggerRef: "github-pull-request"}},
			{Mode: model.DependencyModeEnforced, When: model.DependencyRuleWhen{TriggerRef: "github-push-main"}},
		},
	}
	got, err := ResolveDependencyMode(model.Environment{}, sub, []string{"github-pull-request"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Mode != model.DependencyModeAdvisory ||
		got.Source != "subscription-rule" ||
		got.RuleTriggerRef != "github-pull-request" {
		t.Fatalf("want advisory rule for PR, got %+v", got)
	}
}

func TestResolveDependencyMode_NoMatchedRulesFallsThrough(t *testing.T) {
	sub := &model.EnvironmentSubscription{
		DependencyMode: model.DependencyModeAdvisory,
		DependencyRules: []model.DependencyRule{
			{Mode: model.DependencyModeEnforced, When: model.DependencyRuleWhen{TriggerRef: "github-tag-release"}},
		},
	}
	got, err := ResolveDependencyMode(model.Environment{}, sub, []string{"github-pull-request"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Mode != model.DependencyModeAdvisory || got.Source != "subscription" {
		t.Fatalf("want subscription fallback, got %+v", got)
	}
}

func TestResolveDependencyMode_InvalidModeRejected(t *testing.T) {
	env := model.Environment{DependencyMode: "bogus"}
	_, err := ResolveDependencyMode(env, nil, nil)
	if err == nil {
		t.Fatalf("expected error for invalid mode")
	}
}

func TestResolveDependencyMode_DisabledMode(t *testing.T) {
	sub := &model.EnvironmentSubscription{DependencyMode: model.DependencyModeDisabled}
	got, err := ResolveDependencyMode(model.Environment{}, sub, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Mode != model.DependencyModeDisabled {
		t.Fatalf("want disabled, got %+v", got)
	}
}
