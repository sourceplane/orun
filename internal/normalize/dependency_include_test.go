package normalize

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestNormalize_DefaultsDependencyIncludeToIfSelected(t *testing.T) {
	intent := &model.Intent{
		Metadata:     model.Metadata{Name: "t"},
		Environments: map[string]model.Environment{"dev": {}},
		Components: []model.Component{
			{Name: "api", Type: "terraform", DependsOn: []model.Dependency{
				{Component: "db"}, // include unset
			}},
			{Name: "db", Type: "terraform"},
		},
	}
	out, err := NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent: %v", err)
	}
	dep := out.Components["api"].DependsOn[0]
	if dep.Include != model.IncludeIfSelected {
		t.Errorf("expected default include=if-selected, got %q", dep.Include)
	}
}

func TestNormalize_PreservesExplicitInclude(t *testing.T) {
	intent := &model.Intent{
		Metadata:     model.Metadata{Name: "t"},
		Environments: map[string]model.Environment{"dev": {}},
		Components: []model.Component{
			{Name: "api", Type: "terraform", DependsOn: []model.Dependency{
				{Component: "migration", Include: model.IncludeAlways, Reason: "schema must be fresh"},
			}},
			{Name: "migration", Type: "terraform"},
		},
	}
	out, err := NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent: %v", err)
	}
	dep := out.Components["api"].DependsOn[0]
	if dep.Include != model.IncludeAlways {
		t.Errorf("expected include=always preserved, got %q", dep.Include)
	}
	if dep.Reason == "" {
		t.Errorf("expected reason preserved")
	}
}

func TestNormalize_RejectsInvalidInclude(t *testing.T) {
	intent := &model.Intent{
		Metadata:     model.Metadata{Name: "t"},
		Environments: map[string]model.Environment{"dev": {}},
		Components: []model.Component{
			{Name: "api", Type: "terraform", DependsOn: []model.Dependency{
				{Component: "db", Include: "sometimes"},
			}},
			{Name: "db", Type: "terraform"},
		},
	}
	_, err := NormalizeIntent(intent)
	if err == nil {
		t.Fatal("expected error for invalid include")
	}
	if !strings.Contains(err.Error(), "include") {
		t.Errorf("expected error mentioning include, got %v", err)
	}
}

func TestNormalize_AcceptsValidEdgeMode(t *testing.T) {
	intent := &model.Intent{
		Metadata:     model.Metadata{Name: "t"},
		Environments: map[string]model.Environment{"dev": {}},
		Components: []model.Component{
			{Name: "api", Type: "terraform", DependsOn: []model.Dependency{
				{Component: "db", Mode: model.DependencyModeAdvisory, ModeRules: []model.DependencyRule{
					{Mode: model.DependencyModeEnforced, When: model.DependencyRuleWhen{TriggerRef: "push"}},
				}},
			}},
			{Name: "db", Type: "terraform"},
		},
	}
	out, err := NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent: %v", err)
	}
	if got := out.Components["api"].DependsOn[0].Mode; got != model.DependencyModeAdvisory {
		t.Errorf("expected mode preserved, got %q", got)
	}
}

func TestNormalize_RejectsInvalidEdgeMode(t *testing.T) {
	for name, dep := range map[string]model.Dependency{
		"mode":      {Component: "db", Mode: "sometimes"},
		"rule mode": {Component: "db", ModeRules: []model.DependencyRule{{Mode: "fast", When: model.DependencyRuleWhen{TriggerRef: "push"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			intent := &model.Intent{
				Metadata:     model.Metadata{Name: "t"},
				Environments: map[string]model.Environment{"dev": {}},
				Components: []model.Component{
					{Name: "api", Type: "terraform", DependsOn: []model.Dependency{dep}},
					{Name: "db", Type: "terraform"},
				},
			}
			_, err := NormalizeIntent(intent)
			if err == nil || !strings.Contains(err.Error(), "enforced|advisory|disabled") {
				t.Fatalf("expected invalid edge mode error, got %v", err)
			}
		})
	}
}
