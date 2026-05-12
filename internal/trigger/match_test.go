package trigger

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func triggers() []model.AutomationTrigger {
	return []model.AutomationTrigger{
		{
			Name: "github-pull-request",
			On: model.TriggerOn{
				Provider: "github",
				Event:    "pull_request",
				Actions:  []string{"opened", "synchronize", "reopened"},
			},
			Plan: model.TriggerPlan{Profile: "dry-run"},
		},
		{
			Name: "github-push-main",
			On: model.TriggerOn{
				Provider: "github",
				Event:    "push",
				Branches: []string{"main"},
			},
			Plan: model.TriggerPlan{Profile: "verify"},
		},
		{
			Name: "github-tag-release",
			On: model.TriggerOn{
				Provider: "github",
				Event:    "push",
				Tags:     []string{"v*"},
			},
			Plan: model.TriggerPlan{Profile: "release"},
		},
	}
}

func TestMatch_PREvent(t *testing.T) {
	event := &model.EventContext{
		Provider: "github",
		Event:    "pull_request",
		Action:   "opened",
		Branch:   "main",
	}
	result := Match(triggers(), event)
	if result == nil {
		t.Fatal("expected match")
	}
	if result.Trigger.Name != "github-pull-request" {
		t.Fatalf("trigger = %q, want github-pull-request", result.Trigger.Name)
	}
	if result.Profile != "dry-run" {
		t.Fatalf("profile = %q, want dry-run", result.Profile)
	}
}

func TestMatch_PushToMain(t *testing.T) {
	event := &model.EventContext{
		Provider: "github",
		Event:    "push",
		Branch:   "main",
		Ref:      "refs/heads/main",
	}
	result := Match(triggers(), event)
	if result == nil {
		t.Fatal("expected match")
	}
	if result.Trigger.Name != "github-push-main" {
		t.Fatalf("trigger = %q, want github-push-main", result.Trigger.Name)
	}
	if result.Profile != "verify" {
		t.Fatalf("profile = %q, want verify", result.Profile)
	}
}

func TestMatch_TagPush(t *testing.T) {
	event := &model.EventContext{
		Provider: "github",
		Event:    "push",
		Branch:   "",
		Ref:      "refs/tags/v1.0.0",
	}
	result := Match(triggers(), event)
	if result == nil {
		t.Fatal("expected match")
	}
	if result.Trigger.Name != "github-tag-release" {
		t.Fatalf("trigger = %q, want github-tag-release", result.Trigger.Name)
	}
	if result.Profile != "release" {
		t.Fatalf("profile = %q, want release", result.Profile)
	}
}

func TestMatch_NoMatch(t *testing.T) {
	event := &model.EventContext{
		Provider: "github",
		Event:    "workflow_dispatch",
	}
	result := Match(triggers(), event)
	if result != nil {
		t.Fatalf("expected nil, got trigger %q", result.Trigger.Name)
	}
}

func TestMatch_NilEvent(t *testing.T) {
	result := Match(triggers(), nil)
	if result != nil {
		t.Fatal("expected nil for nil event")
	}
}

func TestMatch_FirstMatchWins(t *testing.T) {
	trigs := []model.AutomationTrigger{
		{
			Name: "first",
			On:   model.TriggerOn{Event: "push", Branches: []string{"main"}},
			Plan: model.TriggerPlan{Profile: "first-profile"},
		},
		{
			Name: "second",
			On:   model.TriggerOn{Event: "push", Branches: []string{"main"}},
			Plan: model.TriggerPlan{Profile: "second-profile"},
		},
	}
	event := &model.EventContext{Event: "push", Branch: "main"}
	result := Match(trigs, event)
	if result == nil {
		t.Fatal("expected match")
	}
	if result.Trigger.Name != "first" {
		t.Fatalf("trigger = %q, want first", result.Trigger.Name)
	}
}

func TestMatch_BranchGlob(t *testing.T) {
	trigs := []model.AutomationTrigger{
		{
			Name: "release-branch",
			On:   model.TriggerOn{Event: "push", Branches: []string{"release/*"}},
			Plan: model.TriggerPlan{Profile: "verify"},
		},
	}
	event := &model.EventContext{Event: "push", Branch: "release/v2"}
	result := Match(trigs, event)
	if result == nil {
		t.Fatal("expected match for branch glob")
	}
}

func TestMatch_BranchGlobNoMatch(t *testing.T) {
	trigs := []model.AutomationTrigger{
		{
			Name: "release-branch",
			On:   model.TriggerOn{Event: "push", Branches: []string{"release/*"}},
			Plan: model.TriggerPlan{Profile: "verify"},
		},
	}
	event := &model.EventContext{Event: "push", Branch: "feature/x"}
	result := Match(trigs, event)
	if result != nil {
		t.Fatal("expected no match for non-matching branch")
	}
}

func TestMatch_ActionFilter(t *testing.T) {
	trigs := []model.AutomationTrigger{
		{
			Name: "pr-ready",
			On: model.TriggerOn{
				Event:   "pull_request",
				Actions: []string{"ready_for_review"},
			},
			Plan: model.TriggerPlan{Profile: "verify"},
		},
	}

	noMatch := &model.EventContext{Event: "pull_request", Action: "opened"}
	if Match(trigs, noMatch) != nil {
		t.Fatal("expected no match for action=opened")
	}

	match := &model.EventContext{Event: "pull_request", Action: "ready_for_review"}
	if Match(trigs, match) == nil {
		t.Fatal("expected match for action=ready_for_review")
	}
}

func TestMatch_ProviderFilter(t *testing.T) {
	trigs := []model.AutomationTrigger{
		{
			Name: "github-only",
			On:   model.TriggerOn{Provider: "github", Event: "push"},
			Plan: model.TriggerPlan{Profile: "verify"},
		},
	}

	gitlab := &model.EventContext{Provider: "gitlab", Event: "push"}
	if Match(trigs, gitlab) != nil {
		t.Fatal("expected no match for gitlab provider")
	}

	github := &model.EventContext{Provider: "github", Event: "push"}
	if Match(trigs, github) == nil {
		t.Fatal("expected match for github provider")
	}
}

func TestMatchByName_Found(t *testing.T) {
	result := MatchByName(triggers(), "github-push-main")
	if result == nil {
		t.Fatal("expected match")
	}
	if result.Profile != "verify" {
		t.Fatalf("profile = %q, want verify", result.Profile)
	}
}

func TestMatchByName_NotFound(t *testing.T) {
	result := MatchByName(triggers(), "nonexistent")
	if result != nil {
		t.Fatal("expected nil for unknown trigger name")
	}
}

func TestMatchesGlob(t *testing.T) {
	cases := []struct {
		pattern string
		value   string
		want    bool
	}{
		{"*", "anything", true},
		{"main", "main", true},
		{"main", "develop", false},
		{"release/*", "release/v1", true},
		{"release/*", "main", false},
		{"v*", "v1.0.0", true},
		{"v*", "2.0.0", false},
	}
	for _, tc := range cases {
		got := matchesGlob(tc.pattern, tc.value)
		if got != tc.want {
			t.Errorf("matchesGlob(%q, %q) = %v, want %v", tc.pattern, tc.value, got, tc.want)
		}
	}
}

func TestMatch_PushToFeatureBranch_NoMatch(t *testing.T) {
	event := &model.EventContext{
		Provider: "github",
		Event:    "push",
		Branch:   "feature/x",
		Ref:      "refs/heads/feature/x",
	}
	result := Match(triggers(), event)
	if result != nil {
		t.Fatalf("expected no match for push to feature branch, got %q", result.Trigger.Name)
	}
}
