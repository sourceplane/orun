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
