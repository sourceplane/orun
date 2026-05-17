package planner

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestResolvePromotionDependencies_SameComponentSamePlan(t *testing.T) {
	jobInstances := map[string]*model.JobInstance{
		"web.dev.deploy": {
			ID: "web.dev.deploy", Component: "web", Environment: "dev",
			DependsOn: []string{},
		},
		"web.staging.deploy": {
			ID: "web.staging.deploy", Component: "web", Environment: "staging",
			DependsOn: []string{},
		},
		"api.dev.deploy": {
			ID: "api.dev.deploy", Component: "api", Environment: "dev",
			DependsOn: []string{},
		},
		"api.staging.deploy": {
			ID: "api.staging.deploy", Component: "api", Environment: "staging",
			DependsOn: []string{},
		},
	}

	compInstances := map[string][]*model.ComponentInstance{
		"dev":     {{ComponentName: "web", Environment: "dev"}, {ComponentName: "api", Environment: "dev"}},
		"staging": {{ComponentName: "web", Environment: "staging"}, {ComponentName: "api", Environment: "staging"}},
	}

	environments := map[string]model.Environment{
		"dev": {},
		"staging": {
			Promotion: model.EnvironmentPromotion{
				DependsOn: []model.PromotionDependency{{
					Environment: "dev",
					Strategy:    "same-component",
					Condition:   "success",
					Satisfy:     "same-plan-or-previous-success",
					Match:       model.PromotionMatch{Revision: "source"},
				}},
			},
		},
	}

	err := ResolvePromotionDependencies(jobInstances, compInstances, environments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// web.staging.deploy should depend on web.dev.deploy
	webStaging := jobInstances["web.staging.deploy"]
	if !containsStr(webStaging.DependsOn, "web.dev.deploy") {
		t.Errorf("web.staging.deploy should depend on web.dev.deploy, got: %v", webStaging.DependsOn)
	}

	// api.staging.deploy should depend on api.dev.deploy
	apiStaging := jobInstances["api.staging.deploy"]
	if !containsStr(apiStaging.DependsOn, "api.dev.deploy") {
		t.Errorf("api.staging.deploy should depend on api.dev.deploy, got: %v", apiStaging.DependsOn)
	}

	// No gates should be added
	if len(webStaging.Gates) != 0 {
		t.Errorf("expected no gates for web.staging.deploy, got: %v", webStaging.Gates)
	}
}

func TestResolvePromotionDependencies_EnvironmentBarrier(t *testing.T) {
	jobInstances := map[string]*model.JobInstance{
		"web.dev.deploy": {
			ID: "web.dev.deploy", Component: "web", Environment: "dev",
			DependsOn: []string{},
		},
		"api.dev.deploy": {
			ID: "api.dev.deploy", Component: "api", Environment: "dev",
			DependsOn: []string{},
		},
		"web.prod.deploy": {
			ID: "web.prod.deploy", Component: "web", Environment: "prod",
			DependsOn: []string{},
		},
	}

	compInstances := map[string][]*model.ComponentInstance{
		"dev":  {{ComponentName: "web", Environment: "dev"}, {ComponentName: "api", Environment: "dev"}},
		"prod": {{ComponentName: "web", Environment: "prod"}},
	}

	environments := map[string]model.Environment{
		"dev": {},
		"prod": {
			Promotion: model.EnvironmentPromotion{
				DependsOn: []model.PromotionDependency{{
					Environment: "dev",
					Strategy:    "environment-barrier",
					Condition:   "success",
					Satisfy:     "same-plan",
					Match:       model.PromotionMatch{Revision: "source"},
				}},
			},
		},
	}

	err := ResolvePromotionDependencies(jobInstances, compInstances, environments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// web.prod.deploy should depend on ALL dev jobs
	webProd := jobInstances["web.prod.deploy"]
	if !containsStr(webProd.DependsOn, "web.dev.deploy") {
		t.Errorf("web.prod.deploy should depend on web.dev.deploy, got: %v", webProd.DependsOn)
	}
	if !containsStr(webProd.DependsOn, "api.dev.deploy") {
		t.Errorf("web.prod.deploy should depend on api.dev.deploy, got: %v", webProd.DependsOn)
	}
}

func TestResolvePromotionDependencies_CrossPlanGates(t *testing.T) {
	jobInstances := map[string]*model.JobInstance{
		"web.prod.release": {
			ID: "web.prod.release", Component: "web", Environment: "prod",
			DependsOn: []string{},
		},
		"api.prod.release": {
			ID: "api.prod.release", Component: "api", Environment: "prod",
			DependsOn: []string{},
		},
	}

	compInstances := map[string][]*model.ComponentInstance{
		"prod": {{ComponentName: "web", Environment: "prod"}, {ComponentName: "api", Environment: "prod"}},
	}

	environments := map[string]model.Environment{
		"staging": {},
		"prod": {
			Promotion: model.EnvironmentPromotion{
				DependsOn: []model.PromotionDependency{{
					Environment: "staging",
					Strategy:    "same-component",
					Condition:   "success",
					Satisfy:     "same-plan-or-previous-success",
					Match:       model.PromotionMatch{Revision: "source"},
				}},
			},
		},
	}

	err := ResolvePromotionDependencies(jobInstances, compInstances, environments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No DAG edges since staging is not active
	webProd := jobInstances["web.prod.release"]
	if len(webProd.DependsOn) != 0 {
		t.Errorf("expected no dependsOn for web.prod.release, got: %v", webProd.DependsOn)
	}

	// Should have gates
	if len(webProd.Gates) != 1 {
		t.Fatalf("expected 1 gate for web.prod.release, got %d", len(webProd.Gates))
	}
	gate := webProd.Gates[0]
	if gate.Type != "environment-promotion" {
		t.Errorf("expected gate type environment-promotion, got %s", gate.Type)
	}
	if gate.Environment != "staging" {
		t.Errorf("expected gate environment staging, got %s", gate.Environment)
	}
	if gate.Component != "web" {
		t.Errorf("expected gate component web, got %s", gate.Component)
	}
	if gate.Match["revision"] != "source" {
		t.Errorf("expected gate match revision=source, got %v", gate.Match)
	}

	// api should also get a gate
	apiProd := jobInstances["api.prod.release"]
	if len(apiProd.Gates) != 1 {
		t.Fatalf("expected 1 gate for api.prod.release, got %d", len(apiProd.Gates))
	}
	if apiProd.Gates[0].Component != "api" {
		t.Errorf("expected gate component api, got %s", apiProd.Gates[0].Component)
	}
}

func TestResolvePromotionDependencies_SamePlanOnlyError(t *testing.T) {
	jobInstances := map[string]*model.JobInstance{
		"web.prod.release": {
			ID: "web.prod.release", Component: "web", Environment: "prod",
			DependsOn: []string{},
		},
	}

	compInstances := map[string][]*model.ComponentInstance{
		"prod": {{ComponentName: "web", Environment: "prod"}},
	}

	environments := map[string]model.Environment{
		"staging": {},
		"prod": {
			Promotion: model.EnvironmentPromotion{
				DependsOn: []model.PromotionDependency{{
					Environment: "staging",
					Strategy:    "same-component",
					Condition:   "success",
					Satisfy:     "same-plan",
					Match:       model.PromotionMatch{Revision: "source"},
				}},
			},
		},
	}

	err := ResolvePromotionDependencies(jobInstances, compInstances, environments)
	if err == nil {
		t.Fatal("expected error when same-plan dependency is not active")
	}
}

func TestResolvePromotionDependencies_ComponentNotInBothEnvs(t *testing.T) {
	jobInstances := map[string]*model.JobInstance{
		"web.dev.deploy": {
			ID: "web.dev.deploy", Component: "web", Environment: "dev",
			DependsOn: []string{},
		},
		"api.staging.deploy": {
			ID: "api.staging.deploy", Component: "api", Environment: "staging",
			DependsOn: []string{},
		},
	}

	compInstances := map[string][]*model.ComponentInstance{
		"dev":     {{ComponentName: "web", Environment: "dev"}},
		"staging": {{ComponentName: "api", Environment: "staging"}},
	}

	environments := map[string]model.Environment{
		"dev": {},
		"staging": {
			Promotion: model.EnvironmentPromotion{
				DependsOn: []model.PromotionDependency{{
					Environment: "dev",
					Strategy:    "same-component",
					Condition:   "success",
					Satisfy:     "same-plan",
					Match:       model.PromotionMatch{Revision: "source"},
				}},
			},
		},
	}

	err := ResolvePromotionDependencies(jobInstances, compInstances, environments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// api.staging.deploy has no matching component in dev, so no edge should be added
	apiStaging := jobInstances["api.staging.deploy"]
	if len(apiStaging.DependsOn) != 0 {
		t.Errorf("expected no dependsOn for api.staging.deploy, got: %v", apiStaging.DependsOn)
	}
}

func TestResolvePromotionDependencies_ChainedPromotions(t *testing.T) {
	jobInstances := map[string]*model.JobInstance{
		"web.dev.deploy": {
			ID: "web.dev.deploy", Component: "web", Environment: "dev",
			DependsOn: []string{},
		},
		"web.staging.deploy": {
			ID: "web.staging.deploy", Component: "web", Environment: "staging",
			DependsOn: []string{},
		},
		"web.prod.deploy": {
			ID: "web.prod.deploy", Component: "web", Environment: "prod",
			DependsOn: []string{},
		},
	}

	compInstances := map[string][]*model.ComponentInstance{
		"dev":     {{ComponentName: "web", Environment: "dev"}},
		"staging": {{ComponentName: "web", Environment: "staging"}},
		"prod":    {{ComponentName: "web", Environment: "prod"}},
	}

	environments := map[string]model.Environment{
		"dev": {},
		"staging": {
			Promotion: model.EnvironmentPromotion{
				DependsOn: []model.PromotionDependency{{
					Environment: "dev",
					Strategy:    "same-component",
					Condition:   "success",
					Satisfy:     "same-plan",
					Match:       model.PromotionMatch{Revision: "source"},
				}},
			},
		},
		"prod": {
			Promotion: model.EnvironmentPromotion{
				DependsOn: []model.PromotionDependency{{
					Environment: "staging",
					Strategy:    "same-component",
					Condition:   "success",
					Satisfy:     "same-plan",
					Match:       model.PromotionMatch{Revision: "source"},
				}},
			},
		},
	}

	err := ResolvePromotionDependencies(jobInstances, compInstances, environments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	webStaging := jobInstances["web.staging.deploy"]
	if !containsStr(webStaging.DependsOn, "web.dev.deploy") {
		t.Errorf("web.staging.deploy should depend on web.dev.deploy, got: %v", webStaging.DependsOn)
	}

	webProd := jobInstances["web.prod.deploy"]
	if !containsStr(webProd.DependsOn, "web.staging.deploy") {
		t.Errorf("web.prod.deploy should depend on web.staging.deploy, got: %v", webProd.DependsOn)
	}
}

func containsStr(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
