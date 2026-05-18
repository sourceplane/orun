package composition

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func testComposition() *Composition {
	return &Composition{
		Name:           "terraform",
		DefaultProfile: "plan-only",
		ExecutionProfiles: map[string]model.ExecutionProfile{
			"plan-only": {Jobs: map[string]model.ProfileJobSpec{
				"terraform": {IncludeCapabilities: []string{"plan"}},
			}},
			"apply": {Jobs: map[string]model.ProfileJobSpec{
				"terraform": {IncludeCapabilities: []string{"plan", "apply"}},
			}},
			"verify": {Jobs: map[string]model.ProfileJobSpec{
				"terraform": {IncludeCapabilities: []string{"validate"}},
			}},
		},
	}
}

func TestResolveProfileWithRules_NoRules(t *testing.T) {
	comp := testComposition()
	sub := &model.EnvironmentSubscription{
		Name:    "dev",
		Profile: "plan-only",
	}

	resolved, matchedRule, err := ResolveProfileWithRules("terraform", comp, sub, []string{"github-push-main"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matchedRule != nil {
		t.Fatalf("expected no matched rule, got %+v", matchedRule)
	}
	if resolved.Name != "plan-only" {
		t.Errorf("expected profile plan-only, got %s", resolved.Name)
	}
	if resolved.Source != "subscription" {
		t.Errorf("expected source subscription, got %s", resolved.Source)
	}
}

func TestResolveProfileWithRules_RuleMatches(t *testing.T) {
	comp := testComposition()
	sub := &model.EnvironmentSubscription{
		Name:    "dev",
		Profile: "plan-only",
		ProfileRules: []model.ProfileRule{
			{Profile: "apply", When: model.ProfileRuleWhen{TriggerRef: "github-push-main"}},
		},
	}

	resolved, matchedRule, err := ResolveProfileWithRules("terraform", comp, sub, []string{"github-push-main"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matchedRule == nil {
		t.Fatal("expected matched rule, got nil")
	}
	if matchedRule.TriggerRef != "github-push-main" {
		t.Errorf("expected triggerRef github-push-main, got %s", matchedRule.TriggerRef)
	}
	if matchedRule.Profile != "apply" {
		t.Errorf("expected rule profile apply, got %s", matchedRule.Profile)
	}
	if resolved.Name != "apply" {
		t.Errorf("expected profile apply, got %s", resolved.Name)
	}
	if resolved.Source != "subscription-rule" {
		t.Errorf("expected source subscription-rule, got %s", resolved.Source)
	}
	if resolved.Ref != "terraform.apply" {
		t.Errorf("expected ref terraform.apply, got %s", resolved.Ref)
	}
}

func TestResolveProfileWithRules_NoTriggerMatch(t *testing.T) {
	comp := testComposition()
	sub := &model.EnvironmentSubscription{
		Name:    "dev",
		Profile: "plan-only",
		ProfileRules: []model.ProfileRule{
			{Profile: "apply", When: model.ProfileRuleWhen{TriggerRef: "github-push-main"}},
		},
	}

	resolved, matchedRule, err := ResolveProfileWithRules("terraform", comp, sub, []string{"github-pull-request"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matchedRule != nil {
		t.Fatalf("expected no matched rule, got %+v", matchedRule)
	}
	if resolved.Name != "plan-only" {
		t.Errorf("expected fallback profile plan-only, got %s", resolved.Name)
	}
	if resolved.Source != "subscription" {
		t.Errorf("expected source subscription, got %s", resolved.Source)
	}
}

func TestResolveProfileWithRules_MultipleRulesFirstWins(t *testing.T) {
	comp := testComposition()
	sub := &model.EnvironmentSubscription{
		Name:    "dev",
		Profile: "plan-only",
		ProfileRules: []model.ProfileRule{
			{Profile: "apply", When: model.ProfileRuleWhen{TriggerRef: "github-push-main"}},
			{Profile: "verify", When: model.ProfileRuleWhen{TriggerRef: "github-push-main"}},
		},
	}

	resolved, matchedRule, err := ResolveProfileWithRules("terraform", comp, sub, []string{"github-push-main"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matchedRule == nil {
		t.Fatal("expected matched rule")
	}
	if matchedRule.RuleIndex != 0 {
		t.Errorf("expected first rule (index 0) to win, got index %d", matchedRule.RuleIndex)
	}
	if resolved.Name != "apply" {
		t.Errorf("expected first matching profile apply, got %s", resolved.Name)
	}
}

func TestResolveProfileWithRules_EmptyMatchedTriggers(t *testing.T) {
	comp := testComposition()
	sub := &model.EnvironmentSubscription{
		Name:    "dev",
		Profile: "plan-only",
		ProfileRules: []model.ProfileRule{
			{Profile: "apply", When: model.ProfileRuleWhen{TriggerRef: "github-push-main"}},
		},
	}

	resolved, matchedRule, err := ResolveProfileWithRules("terraform", comp, sub, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matchedRule != nil {
		t.Fatalf("expected no matched rule with empty triggers, got %+v", matchedRule)
	}
	if resolved.Name != "plan-only" {
		t.Errorf("expected fallback profile plan-only, got %s", resolved.Name)
	}
}

func TestResolveProfileWithRules_NilSubscription(t *testing.T) {
	comp := testComposition()

	resolved, matchedRule, err := ResolveProfileWithRules("terraform", comp, nil, []string{"github-push-main"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matchedRule != nil {
		t.Fatalf("expected no matched rule for nil subscription, got %+v", matchedRule)
	}
	if resolved.Name != "plan-only" {
		t.Errorf("expected composition-default profile, got %s", resolved.Name)
	}
	if resolved.Source != "composition-default" {
		t.Errorf("expected source composition-default, got %s", resolved.Source)
	}
}

func TestResolveProfileWithRules_InvalidRuleProfile(t *testing.T) {
	comp := testComposition()
	sub := &model.EnvironmentSubscription{
		Name:    "dev",
		Profile: "plan-only",
		ProfileRules: []model.ProfileRule{
			{Profile: "nonexistent", When: model.ProfileRuleWhen{TriggerRef: "github-push-main"}},
		},
	}

	_, _, err := ResolveProfileWithRules("terraform", comp, sub, []string{"github-push-main"})
	if err == nil {
		t.Fatal("expected error for nonexistent profile in rule")
	}
}
