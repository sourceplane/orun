package expand

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/normalize"
)

// TestExpandResolvesEdgeDependencyMode verifies that dependsOn[].mode and
// modeRules are resolved per edge against the matched triggers, and that an
// edge without either carries no override (it follows the lane).
func TestExpandResolvesEdgeDependencyMode(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "edge-mode"},
		Environments: map[string]model.Environment{
			"prod": {
				DependencyMode: model.DependencyModeEnforced,
				Selectors:      model.EnvironmentSelectors{Components: []string{"*"}},
			},
		},
		Components: []model.Component{
			{Name: "db-migrate", Type: "terraform"},
			{Name: "peer", Type: "terraform"},
			{Name: "infra", Type: "terraform"},
			{
				Name: "api",
				Type: "terraform",
				DependsOn: []model.Dependency{
					{Component: "db-migrate", Mode: model.DependencyModeAdvisory, ModeRules: []model.DependencyRule{
						{Mode: model.DependencyModeEnforced, When: model.DependencyRuleWhen{TriggerRef: "github-push-main"}},
					}},
					{Component: "peer", Mode: model.DependencyModeAdvisory},
					{Component: "infra"},
				},
			},
		},
	}
	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent: %v", err)
	}
	instances, err := NewExpander(normalized).WithMatchedTriggers([]string{"github-push-main"}).Expand()
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}

	var api *model.ComponentInstance
	for _, inst := range instances["prod"] {
		if inst.ComponentName == "api" {
			api = inst
		}
	}
	if api == nil {
		t.Fatal("api instance not expanded")
	}
	if api.DependencyMode != model.DependencyModeEnforced {
		t.Errorf("lane mode = %q, want enforced", api.DependencyMode)
	}
	want := map[string]model.ResolvedDependency{
		"db-migrate": {Mode: model.DependencyModeEnforced, ModeSource: "edge-rule", ModeRuleTriggerRef: "github-push-main"},
		"peer":       {Mode: model.DependencyModeAdvisory, ModeSource: "edge"},
		"infra":      {},
	}
	for _, d := range api.DependsOn {
		w := want[d.ComponentName]
		if d.Mode != w.Mode || d.ModeSource != w.ModeSource || d.ModeRuleTriggerRef != w.ModeRuleTriggerRef {
			t.Errorf("%s: mode=%q source=%q rule=%q, want %+v", d.ComponentName, d.Mode, d.ModeSource, d.ModeRuleTriggerRef, w)
		}
	}
}
