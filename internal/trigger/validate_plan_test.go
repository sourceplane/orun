package trigger

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func planOptionsIntent(plan model.TriggerPlanOptions) *model.Intent {
	return &model.Intent{Automation: model.AutomationConfig{TriggerBindings: map[string]model.TriggerBinding{
		"t": {On: model.TriggerMatch{Provider: "github", Event: "workflow_dispatch"}, Plan: plan},
	}}}
}

func TestValidatePlanOptions(t *testing.T) {
	valid := []model.TriggerPlanOptions{
		{},
		{Scope: "full"},
		{Scope: "changed", Base: "before", Head: "after"},
		{Scope: "selected", Components: "inputs.component", Environments: "inputs.environment"},
		{Scope: "selected", Components: "c", Environments: "e", AutoPush: boolPtr(false)},
		{AutoPush: boolPtr(true)},
	}
	for _, p := range valid {
		if errs := ValidatePlanOptions(planOptionsIntent(p)); len(errs) != 0 {
			t.Errorf("%+v: unexpected errors %v", p, errs)
		}
	}

	invalid := map[string]struct {
		plan model.TriggerPlanOptions
		want string
	}{
		"unknown scope":               {model.TriggerPlanOptions{Scope: "everything"}, `plan.scope "everything" is invalid`},
		"selected without components": {model.TriggerPlanOptions{Scope: "selected", Environments: "inputs.environment"}, "plan.components is required with scope: selected"},
		"selected without envs":       {model.TriggerPlanOptions{Scope: "selected", Components: "inputs.component"}, "plan.environments is required with scope: selected"},
		"components on full":          {model.TriggerPlanOptions{Scope: "full", Components: "inputs.component"}, "plan.components is only valid with scope: selected"},
		"environments on changed":     {model.TriggerPlanOptions{Scope: "changed", Environments: "inputs.environment"}, "plan.environments is only valid with scope: selected"},
		"empty segment":               {model.TriggerPlanOptions{Scope: "selected", Components: "inputs..component", Environments: "e"}, "is not a dot path"},
		"trailing dot":                {model.TriggerPlanOptions{Scope: "selected", Components: "c", Environments: "inputs."}, "is not a dot path"},
	}
	for name, tc := range invalid {
		t.Run(name, func(t *testing.T) {
			errs := ValidatePlanOptions(planOptionsIntent(tc.plan))
			if len(errs) == 0 {
				t.Fatalf("expected an error mentioning %q", tc.want)
			}
			joined := FormatErrors(errs).Error()
			if !strings.Contains(joined, tc.want) || !strings.Contains(joined, `trigger binding "t"`) {
				t.Errorf("error %q should name the binding and mention %q", joined, tc.want)
			}
		})
	}
}
