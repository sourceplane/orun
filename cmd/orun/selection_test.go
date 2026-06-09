package main

import (
	"reflect"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestComputePlanSelection(t *testing.T) {
	instances := map[string][]*model.ComponentInstance{
		"staging": {
			{Environment: "staging", ComponentName: "web"},
			{Environment: "staging", ComponentName: "api"},
		},
		"dev": {
			{Environment: "dev", ComponentName: "api"},
		},
	}

	t.Run("full plan, sorted and deterministic", func(t *testing.T) {
		sel := computePlanSelection(instances, false, false)
		if sel.Mode != "full" {
			t.Errorf("mode = %q, want full", sel.Mode)
		}
		if sel.AllEnvs {
			t.Errorf("allEnvs = true, want false")
		}
		if want := []string{"dev", "staging"}; !reflect.DeepEqual(sel.Envs, want) {
			t.Errorf("envs = %v, want %v", sel.Envs, want)
		}
		if want := []string{"api", "web"}; !reflect.DeepEqual(sel.Components, want) {
			t.Errorf("components = %v, want %v", sel.Components, want)
		}
		if len(sel.PrunedEdges) != 0 {
			t.Errorf("prunedEdges = %v, want empty", sel.PrunedEdges)
		}
	})

	t.Run("scoped sets mode", func(t *testing.T) {
		sel := computePlanSelection(map[string][]*model.ComponentInstance{
			"staging": {{Environment: "staging", ComponentName: "web"}},
		}, true, false)
		if sel.Mode != "scoped" {
			t.Errorf("mode = %q, want scoped", sel.Mode)
		}
	})

	t.Run("explicit all-envs", func(t *testing.T) {
		sel := computePlanSelection(instances, false, true)
		if !sel.AllEnvs {
			t.Errorf("allEnvs = false, want true")
		}
	})

	t.Run("empty instances", func(t *testing.T) {
		sel := computePlanSelection(map[string][]*model.ComponentInstance{}, false, false)
		if len(sel.Envs) != 0 || len(sel.Components) != 0 {
			t.Errorf("expected empty selection, got %+v", sel)
		}
		if sel.Mode != "full" {
			t.Errorf("mode = %q, want full", sel.Mode)
		}
	})
}

func TestComputePrunedEdges(t *testing.T) {
	envs := map[string]model.Environment{
		"dev": {},
		"staging": {
			Promotion: model.EnvironmentPromotion{
				DependsOn: []model.PromotionDependency{
					{Environment: "dev", Satisfy: "same-plan"},
				},
			},
		},
		"prod": {
			Promotion: model.EnvironmentPromotion{
				DependsOn: []model.PromotionDependency{
					{Environment: "staging", Satisfy: "same-plan"},
				},
			},
		},
	}

	t.Run("full plan prunes nothing", func(t *testing.T) {
		instances := map[string][]*model.ComponentInstance{
			"dev":     {{ComponentName: "api", Environment: "dev"}},
			"staging": {{ComponentName: "api", Environment: "staging"}},
		}
		if got := computePrunedEdges(instances, map[string]model.Environment{"dev": {}, "staging": {}}); len(got) != 0 {
			t.Errorf("expected no pruned edges, got %v", got)
		}
	})

	t.Run("scoped env prunes the promotion edge", func(t *testing.T) {
		// Only staging selected; its same-plan dep on dev is dropped.
		instances := map[string][]*model.ComponentInstance{
			"staging": {{ComponentName: "api", Environment: "staging"}},
		}
		got := computePrunedEdges(instances, envs)
		want := []model.PrunedEdge{
			{Kind: "promotion", From: "staging", To: "dev", Reason: "env-not-selected"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("pruned = %v, want %v", got, want)
		}
	})

	t.Run("component edge to absent target is pruned (if-selected)", func(t *testing.T) {
		instances := map[string][]*model.ComponentInstance{
			"dev": {{
				ComponentName: "api", Environment: "dev",
				DependsOn: []model.ResolvedDependency{
					{ComponentName: "shared", Environment: "dev", Include: model.IncludeIfSelected},
				},
			}},
		}
		got := computePrunedEdges(instances, map[string]model.Environment{"dev": {}})
		want := []model.PrunedEdge{
			{Kind: "component", From: "api", To: "shared", Reason: "component-not-selected"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("pruned = %v, want %v", got, want)
		}
	})

	t.Run("include:always edge is not pruned (it would be a hard error)", func(t *testing.T) {
		instances := map[string][]*model.ComponentInstance{
			"dev": {{
				ComponentName: "api", Environment: "dev",
				DependsOn: []model.ResolvedDependency{
					{ComponentName: "shared", Environment: "dev", Include: model.IncludeAlways},
				},
			}},
		}
		if got := computePrunedEdges(instances, map[string]model.Environment{"dev": {}}); len(got) != 0 {
			t.Errorf("include:always must not be recorded as pruned, got %v", got)
		}
	})

	t.Run("deterministic and deduplicated", func(t *testing.T) {
		// api depends on missing "shared" in two envs → one deduped edge.
		instances := map[string][]*model.ComponentInstance{
			"dev": {{
				ComponentName: "api", Environment: "dev",
				DependsOn: []model.ResolvedDependency{{ComponentName: "shared", Environment: "dev"}},
			}},
			"staging": {{
				ComponentName: "api", Environment: "staging",
				DependsOn: []model.ResolvedDependency{{ComponentName: "shared", Environment: "staging"}},
			}},
		}
		got := computePrunedEdges(instances, map[string]model.Environment{"dev": {}, "staging": {}})
		if len(got) != 1 {
			t.Fatalf("expected 1 deduped component edge, got %v", got)
		}
	})
}

func TestCountGatedJobs(t *testing.T) {
	jobs := map[string]*model.JobInstance{
		"a": {Gates: []model.PromotionGate{{Type: "environment-promotion"}}},
		"b": {},
		"c": {Gates: []model.PromotionGate{{Type: "environment-promotion"}, {Type: "environment-promotion"}}},
		"d": nil,
	}
	if got := countGatedJobs(jobs); got != 2 {
		t.Errorf("countGatedJobs = %d, want 2", got)
	}
	if got := countGatedJobs(map[string]*model.JobInstance{"x": {}}); got != 0 {
		t.Errorf("countGatedJobs (no gates) = %d, want 0", got)
	}
}
