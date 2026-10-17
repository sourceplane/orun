package trigger

import (
	"reflect"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func boolPtr(b bool) *bool { return &b }

// dispatchIntent binds github-dispatch to workflow_dispatch with a selected
// scope, beside a push binding, over dev/stage/prod.
func dispatchIntent() *model.Intent {
	return &model.Intent{
		Automation: model.AutomationConfig{
			TriggerBindings: map[string]model.TriggerBinding{
				"github-push-main": {
					On:   model.TriggerMatch{Provider: "github", Event: "push", Branches: []string{"main"}},
					Plan: model.TriggerPlanOptions{Scope: "changed", Base: "before", Head: "after"},
				},
				"github-dispatch": {
					On: model.TriggerMatch{Provider: "github", Event: "workflow_dispatch", Branches: []string{"main"}},
					Plan: model.TriggerPlanOptions{
						Scope:        model.PlanScopeSelected,
						Components:   "inputs.component",
						Environments: "inputs.environment",
						AutoPush:     boolPtr(false),
					},
				},
			},
		},
		Environments: map[string]model.Environment{
			"dev":   {Activation: model.EnvironmentActivation{TriggerRefs: []string{"github-push-main", "github-dispatch"}}},
			"stage": {Activation: model.EnvironmentActivation{TriggerRefs: []string{"github-push-main", "github-dispatch"}}},
			"prod":  {Activation: model.EnvironmentActivation{TriggerRefs: []string{"github-push-main"}}},
		},
	}
}

func dispatchEvent(inputs map[string]any) *model.NormalizedEvent {
	return &model.NormalizedEvent{
		Provider: "github",
		Event:    "workflow_dispatch",
		Ref:      "refs/heads/main",
		RefType:  "branch",
		Branch:   "main",
		Raw:      map[string]any{"ref": "refs/heads/main", "inputs": inputs},
	}
}

func TestResolveList(t *testing.T) {
	raw := map[string]any{"inputs": map[string]any{
		"one":    "deploy-worker",
		"many":   " web-console-next, api-edge ,,api-edge",
		"list":   []any{"a", " b "},
		"badlst": []any{"a", 3},
		"null":   nil,
		"empty":  " , ",
		"num":    7.0,
	}}
	ok := map[string][]string{
		"inputs.one":  {"deploy-worker"},
		"inputs.many": {"web-console-next", "api-edge"},
		"inputs.list": {"a", "b"},
	}
	for path, want := range ok {
		got, err := ResolveList(raw, path)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("ResolveList(%q) = %v, %v; want %v", path, got, err, want)
		}
	}
	for path, msg := range map[string]string{
		"inputs.missing": "has no value",
		"inputs.null":    "has no value",
		"inputs.empty":   "is empty",
		"inputs.num":     "must be a string or a list",
		"inputs.badlst":  "item 1 is not a string",
		"inputs.one.x":   "has no value",
	} {
		if _, err := ResolveList(raw, path); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("ResolveList(%q) error = %v, want it to mention %q", path, err, msg)
		}
	}
}

func TestResolveActiveEnvironments_SelectedFromEvent(t *testing.T) {
	ctx := model.TriggerContext{Mode: "event-file", Event: dispatchEvent(map[string]any{
		"component": "deploy-worker,api-edge", "environment": "stage", "deploy_id": "dpl_1",
	})}
	envs, res, err := ResolveActiveEnvironments(dispatchIntent(), ctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(envs, []string{"stage"}) {
		t.Errorf("envs = %v, want [stage] (the event's, not every env the trigger activates)", envs)
	}
	if res.PlanScope != model.PlanScopeSelected {
		t.Errorf("scope = %q, want selected", res.PlanScope)
	}
	if !reflect.DeepEqual(res.SelectedComponents, []string{"deploy-worker", "api-edge"}) {
		t.Errorf("components = %v", res.SelectedComponents)
	}
	if !reflect.DeepEqual(res.SelectedEnvironments, []string{"stage"}) {
		t.Errorf("selected envs = %v", res.SelectedEnvironments)
	}
	if !res.AutoPushDisabled {
		t.Errorf("autopush: false on the binding should disable the push")
	}
}

func TestResolveActiveEnvironments_SelectedRefusals(t *testing.T) {
	cases := map[string]struct {
		inputs map[string]any
		want   string
	}{
		"empty component":                   {map[string]any{"component": "", "environment": "stage"}, "plan.components"},
		"missing environment":               {map[string]any{"component": "api-edge"}, "plan.environments"},
		"env the trigger does not activate": {map[string]any{"component": "api-edge", "environment": "prod"}, "environment prod is not activated by trigger github-dispatch"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := model.TriggerContext{Mode: "event-file", Event: dispatchEvent(tc.inputs)}
			if _, _, err := ResolveActiveEnvironments(dispatchIntent(), ctx, ""); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestResolveActiveEnvironments_SelectedNamedNeedsEnv(t *testing.T) {
	ctx := model.TriggerContext{Mode: "named-trigger", TriggerName: "github-dispatch"}
	if _, _, err := ResolveActiveEnvironments(dispatchIntent(), ctx, ""); err == nil || !strings.Contains(err.Error(), "--env") {
		t.Fatalf("a named selected trigger without --env should fail naming the flag, got %v", err)
	}
	envs, res, err := ResolveActiveEnvironments(dispatchIntent(), ctx, "dev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(envs, []string{"dev"}) || res.PlanScope != model.PlanScopeSelected || len(res.SelectedComponents) != 0 {
		t.Errorf("envs=%v scope=%q components=%v; want [dev], selected, none (flags supply them)", envs, res.PlanScope, res.SelectedComponents)
	}
	if !res.AutoPushDisabled {
		t.Errorf("autopush: false should hold under a named trigger too")
	}
}

func TestResolveActiveEnvironments_OtherBindingsUnchanged(t *testing.T) {
	ev := &model.NormalizedEvent{Provider: "github", Event: "push", Branch: "main", RefType: "branch",
		Raw: map[string]any{"before": "b1", "after": "a1"}}
	envs, res, err := ResolveActiveEnvironments(dispatchIntent(), model.TriggerContext{Mode: "event-file", Event: ev}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(envs, []string{"dev", "prod", "stage"}) || res.PlanScope != "changed" {
		t.Errorf("push: envs=%v scope=%q", envs, res.PlanScope)
	}
	if res.AutoPushDisabled || res.SelectedComponents != nil {
		t.Errorf("a push plan must not pick up the dispatch binding's options: %+v", res)
	}
}
