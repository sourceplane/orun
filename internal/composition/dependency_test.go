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

func TestResolveEdgeDependencyMode_NoOverride(t *testing.T) {
	got, err := ResolveEdgeDependencyMode(model.Dependency{Component: "db"}, []string{"github-push-main"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != (ResolvedEdgeDependencyMode{}) {
		t.Fatalf("want empty (follow lane), got %+v", got)
	}
}

func TestResolveEdgeDependencyMode_EdgeMode(t *testing.T) {
	got, err := ResolveEdgeDependencyMode(model.Dependency{Component: "db", Mode: model.DependencyModeAdvisory}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Mode != model.DependencyModeAdvisory || got.Source != "edge" || got.RuleTriggerRef != "" {
		t.Fatalf("want edge/advisory, got %+v", got)
	}
}

func TestResolveEdgeDependencyMode_RuleFirstMatchWins(t *testing.T) {
	dep := model.Dependency{
		Component: "db",
		Mode:      model.DependencyModeAdvisory,
		ModeRules: []model.DependencyRule{
			{Mode: model.DependencyModeEnforced, When: model.DependencyRuleWhen{TriggerRef: "github-push-main"}},
			{Mode: model.DependencyModeDisabled, When: model.DependencyRuleWhen{TriggerRef: "github-push-main"}},
		},
	}
	got, err := ResolveEdgeDependencyMode(dep, []string{"github-push-main"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Mode != model.DependencyModeEnforced || got.Source != "edge-rule" || got.RuleTriggerRef != "github-push-main" {
		t.Fatalf("want edge-rule/enforced@github-push-main, got %+v", got)
	}

	// No matching trigger: falls back to the edge's own mode.
	got, err = ResolveEdgeDependencyMode(dep, []string{"github-pull-request"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Mode != model.DependencyModeAdvisory || got.Source != "edge" {
		t.Fatalf("want edge/advisory fallback, got %+v", got)
	}

	// No matching trigger and no edge mode: follow the lane.
	dep.Mode = ""
	got, err = ResolveEdgeDependencyMode(dep, []string{"github-pull-request"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Mode != "" {
		t.Fatalf("want lane fallback (empty), got %+v", got)
	}
}

func TestResolveEdgeDependencyMode_InvalidMode(t *testing.T) {
	if _, err := ResolveEdgeDependencyMode(model.Dependency{Component: "db", Mode: "sometimes"}, nil); err == nil {
		t.Fatal("expected error for invalid edge mode")
	}
	dep := model.Dependency{Component: "db", ModeRules: []model.DependencyRule{
		{Mode: "", When: model.DependencyRuleWhen{TriggerRef: "x"}},
	}}
	if _, err := ResolveEdgeDependencyMode(dep, []string{"x"}); err == nil {
		t.Fatal("expected error for empty rule mode")
	}
}
