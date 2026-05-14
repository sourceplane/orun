package trigger

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func testIntent() *model.Intent {
	return &model.Intent{
		Automation: model.AutomationConfig{
			TriggerBindings: map[string]model.TriggerBinding{
				"github-pull-request": {
					On: model.TriggerMatch{
						Provider:     "github",
						Event:        "pull_request",
						Actions:      []string{"opened", "synchronize"},
						BaseBranches: []string{"main"},
					},
					Plan: model.TriggerPlanOptions{
						Scope: "changed",
						Base:  "pull_request.base.sha",
						Head:  "pull_request.head.sha",
					},
				},
				"github-push-main": {
					On: model.TriggerMatch{
						Provider: "github",
						Event:    "push",
						Branches: []string{"main"},
					},
					Plan: model.TriggerPlanOptions{
						Scope: "changed",
						Base:  "before",
						Head:  "after",
					},
				},
				"github-tag-release": {
					On: model.TriggerMatch{
						Provider: "github",
						Event:    "push",
						Tags:     []string{"v*"},
					},
					Plan: model.TriggerPlanOptions{
						Scope: "full",
					},
				},
			},
		},
		Environments: map[string]model.Environment{
			"development": {
				Activation: model.EnvironmentActivation{
					TriggerRefs: []string{"github-pull-request"},
				},
			},
			"staging": {
				Activation: model.EnvironmentActivation{
					TriggerRefs: []string{"github-push-main"},
				},
			},
			"production": {
				Activation: model.EnvironmentActivation{
					TriggerRefs: []string{"github-tag-release"},
				},
			},
		},
	}
}

func TestResolveActiveEnvironments_NoTrigger(t *testing.T) {
	intent := testIntent()
	ctx := model.TriggerContext{Mode: "none"}

	envs, resolution, err := ResolveActiveEnvironments(intent, ctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolution != nil {
		t.Error("expected nil resolution for no-trigger mode")
	}
	if len(envs) != 3 {
		t.Errorf("expected 3 environments, got %d: %v", len(envs), envs)
	}
}

func TestResolveActiveEnvironments_NoTriggerWithEnvFilter(t *testing.T) {
	intent := testIntent()
	ctx := model.TriggerContext{Mode: "none"}

	envs, _, err := ResolveActiveEnvironments(intent, ctx, "staging")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(envs) != 1 || envs[0] != "staging" {
		t.Errorf("expected [staging], got %v", envs)
	}
}

func TestResolveActiveEnvironments_NamedTrigger(t *testing.T) {
	intent := testIntent()
	ctx := model.TriggerContext{
		Mode:        "named-trigger",
		TriggerName: "github-pull-request",
	}

	envs, resolution, err := ResolveActiveEnvironments(intent, ctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(envs) != 1 || envs[0] != "development" {
		t.Errorf("expected [development], got %v", envs)
	}
	if resolution.PlanScope != "changed" {
		t.Errorf("expected scope=changed, got %q", resolution.PlanScope)
	}
	if len(resolution.MatchedTriggerNames) != 1 || resolution.MatchedTriggerNames[0] != "github-pull-request" {
		t.Errorf("unexpected matched triggers: %v", resolution.MatchedTriggerNames)
	}
}

func TestResolveActiveEnvironments_NamedTriggerNotFound(t *testing.T) {
	intent := testIntent()
	ctx := model.TriggerContext{
		Mode:        "named-trigger",
		TriggerName: "nonexistent",
	}

	_, _, err := ResolveActiveEnvironments(intent, ctx, "")
	if err == nil {
		t.Fatal("expected error for nonexistent trigger")
	}
}

func TestResolveActiveEnvironments_NamedTriggerWithInvalidEnv(t *testing.T) {
	intent := testIntent()
	ctx := model.TriggerContext{
		Mode:        "named-trigger",
		TriggerName: "github-pull-request",
	}

	_, _, err := ResolveActiveEnvironments(intent, ctx, "production")
	if err == nil {
		t.Fatal("expected error when env is not activated by trigger")
	}
}

func TestResolveActiveEnvironments_EventFile_PullRequest(t *testing.T) {
	intent := testIntent()
	event := &model.NormalizedEvent{
		Provider:   "github",
		Event:      "pull_request",
		Action:     "synchronize",
		BaseBranch: "main",
		Branch:     "feature/x",
		BaseSHA:    "base111",
		HeadSHA:    "head222",
		Raw: map[string]any{
			"pull_request": map[string]any{
				"base": map[string]any{"sha": "base111"},
				"head": map[string]any{"sha": "head222"},
			},
		},
	}
	ctx := model.TriggerContext{Mode: "event-file", Event: event}

	envs, resolution, err := ResolveActiveEnvironments(intent, ctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(envs) != 1 || envs[0] != "development" {
		t.Errorf("expected [development], got %v", envs)
	}
	if resolution.PlanScope != "changed" {
		t.Errorf("expected scope=changed, got %q", resolution.PlanScope)
	}
	if resolution.Base != "base111" {
		t.Errorf("expected base=base111, got %q", resolution.Base)
	}
	if resolution.Head != "head222" {
		t.Errorf("expected head=head222, got %q", resolution.Head)
	}
}

func TestResolveActiveEnvironments_EventFile_PushMain(t *testing.T) {
	intent := testIntent()
	event := &model.NormalizedEvent{
		Provider: "github",
		Event:    "push",
		Branch:   "main",
		BaseSHA:  "before1",
		HeadSHA:  "after1",
		Raw: map[string]any{
			"before": "before1",
			"after":  "after1",
		},
	}
	ctx := model.TriggerContext{Mode: "event-file", Event: event}

	envs, resolution, err := ResolveActiveEnvironments(intent, ctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(envs) != 1 || envs[0] != "staging" {
		t.Errorf("expected [staging], got %v", envs)
	}
	if resolution.PlanScope != "changed" {
		t.Errorf("expected scope=changed, got %q", resolution.PlanScope)
	}
}

func TestResolveActiveEnvironments_EventFile_TagRelease(t *testing.T) {
	intent := testIntent()
	event := &model.NormalizedEvent{
		Provider: "github",
		Event:    "push",
		Tag:      "v1.5.0",
		BaseSHA:  "000",
		HeadSHA:  "111",
		Raw:      map[string]any{},
	}
	ctx := model.TriggerContext{Mode: "event-file", Event: event}

	envs, resolution, err := ResolveActiveEnvironments(intent, ctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(envs) != 1 || envs[0] != "production" {
		t.Errorf("expected [production], got %v", envs)
	}
	if resolution.PlanScope != "full" {
		t.Errorf("expected scope=full, got %q", resolution.PlanScope)
	}
}

func TestResolveActiveEnvironments_EventFile_NoMatch(t *testing.T) {
	intent := testIntent()
	event := &model.NormalizedEvent{
		Provider: "github",
		Event:    "pull_request",
		Action:   "closed",
		Raw:      map[string]any{},
	}
	ctx := model.TriggerContext{Mode: "event-file", Event: event}

	_, _, err := ResolveActiveEnvironments(intent, ctx, "")
	if err == nil {
		t.Fatal("expected error when no trigger matches")
	}
}

func TestResolvePath(t *testing.T) {
	raw := map[string]any{
		"before": "aaa",
		"after":  "bbb",
		"pull_request": map[string]any{
			"base": map[string]any{"sha": "base-sha"},
			"head": map[string]any{"sha": "head-sha"},
		},
	}

	tests := []struct {
		path     string
		expected string
		ok       bool
	}{
		{"before", "aaa", true},
		{"after", "bbb", true},
		{"pull_request.base.sha", "base-sha", true},
		{"pull_request.head.sha", "head-sha", true},
		{"nonexistent", "", false},
		{"pull_request.nonexist.sha", "", false},
		{"", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got, ok := ResolvePath(raw, tc.path)
			if ok != tc.ok {
				t.Errorf("ResolvePath(%q): ok=%v, want %v", tc.path, ok, tc.ok)
			}
			if got != tc.expected {
				t.Errorf("ResolvePath(%q) = %q, want %q", tc.path, got, tc.expected)
			}
		})
	}
}
