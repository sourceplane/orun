package main

import (
	"encoding/json"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestGithubMatrixJSON_CarriesIndex(t *testing.T) {
	plan := &model.Plan{Jobs: []model.PlanJob{
		{ID: "a.dev.terraform", UID: "u1", Index: 0, Component: "a", Environment: "dev"},
		{ID: "b.dev.terraform", UID: "u2", Index: 1, Component: "b", Environment: "dev"},
	}}
	var out struct {
		Include []map[string]any `json:"include"`
	}
	if err := json.Unmarshal([]byte(githubMatrixJSON(plan)), &out); err != nil {
		t.Fatalf("matrix is not JSON: %v", err)
	}
	if len(out.Include) != 2 {
		t.Fatalf("entries = %d", len(out.Include))
	}
	if out.Include[1]["index"] != float64(1) || out.Include[1]["id"] != "b.dev.terraform" {
		t.Fatalf("entry = %v", out.Include[1])
	}
	if githubMatrixJSON(&model.Plan{}) != `{"include":[]}` {
		t.Fatalf("empty plan = %s", githubMatrixJSON(&model.Plan{}))
	}
}

func TestGithubLaneMatrixJSON(t *testing.T) {
	jobs := []model.PlanJob{
		{ID: "a.dev", Index: 0, Component: "a", Environment: "dev", CheckName: "a · dev · Deploy", Lane: "dev/auto-1"},
		{ID: "b.dev", Index: 1, Component: "b", Environment: "dev", CheckName: "b · dev · Deploy", RunsOn: "big", Lane: "dev/auto-1"},
	}
	type out struct {
		Include []laneMatrixEntry `json:"include"`
	}

	// Without lanes: one entry per job, named by its check.
	var o out
	s, n := githubLaneMatrixJSON(&model.Plan{Jobs: jobs})
	if err := json.Unmarshal([]byte(s), &o); err != nil || n != 2 {
		t.Fatalf("%v %d %s", err, n, s)
	}
	if o.Include[1] != (laneMatrixEntry{Lane: "b.dev", Name: "b · dev · Deploy", Index: 1, Env: "dev", Jobs: 1, RunsOn: "big"}) {
		t.Fatalf("entry = %+v", o.Include[1])
	}

	// With lanes: one entry per lane.
	plan := &model.Plan{Jobs: jobs, Lanes: []model.PlanLane{{ID: "dev/auto-1", Name: "dev · auto 1/1 · a, b", Index: 0, Environment: "dev", Jobs: []string{"a.dev", "b.dev"}, Concurrency: 4}}}
	o = out{}
	s, n = githubLaneMatrixJSON(plan)
	if err := json.Unmarshal([]byte(s), &o); err != nil || n != 1 {
		t.Fatalf("%v %d %s", err, n, s)
	}
	if o.Include[0] != (laneMatrixEntry{Lane: "dev/auto-1", Name: "dev · auto 1/1 · a, b", Index: 0, Env: "dev", Jobs: 2}) {
		t.Fatalf("entry = %+v", o.Include[0])
	}
	if s, n := githubLaneMatrixJSON(&model.Plan{}); s != `{"include":[]}` || n != 0 {
		t.Fatalf("empty = %s %d", s, n)
	}
}
