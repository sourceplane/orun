package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestRenderPlanLanes(t *testing.T) {
	steps := func(names ...string) []model.PlanStep {
		out := make([]model.PlanStep, len(names))
		for i, n := range names {
			out[i] = model.PlanStep{ID: n, Name: n}
		}
		return out
	}
	plan := &model.Plan{Jobs: []model.PlanJob{
		{ID: "api.staging.ship", Component: "api", Environment: "staging", Profile: "node-service.verify", Steps: steps("test", "build")},
		{ID: "web.staging.ship", Component: "web", Environment: "staging", Profile: "node-service.verify", Steps: steps("test", "build"), DependsOn: []string{"api.staging.ship"}},
		{ID: "api.production.ship", Component: "api", Environment: "production", Profile: "node-service.release", Steps: steps("test", "build", "package"), DependsOn: []string{"api.staging.ship"}},
		{ID: "web.production.ship", Component: "web", Environment: "production", Profile: "node-service.release", Steps: steps("test", "build", "package"), DependsOn: []string{"api.production.ship", "web.staging.ship"}},
	}}
	var buf bytes.Buffer
	renderPlanLanes(&buf, plan, false)
	want := strings.Join([]string{
		"  staging",
		"  ├─ api  verify   test → build",
		"  └─ web  verify   test → build            after api",
		"  production                               after staging",
		"  ├─ api  release  test → build → package",
		"  └─ web  release  test → build → package  after api",
		"",
	}, "\n")
	if got := buf.String(); got != want {
		t.Fatalf("lanes:\n%s\nwant:\n%s", got, want)
	}
}
