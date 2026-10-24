package main

import (
	"encoding/json"
	"reflect"
	"strings"
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
		{ID: "b.dev", Index: 1, Component: "b", Environment: "dev", CheckName: "b · dev · Deploy", RunsOn: "big", Lane: "dev/auto-1", DependsOn: []string{"a.dev"}},
	}

	// Without lanes: one entry per job, named by its check, at the job's depth
	// with its dependencies as the lanes it waits on.
	var o laneMatrix
	s, n := githubLaneMatrixJSON(&model.Plan{Jobs: jobs})
	if err := json.Unmarshal([]byte(s), &o); err != nil || n != 2 {
		t.Fatalf("%v %d %s", err, n, s)
	}
	want := laneMatrixEntry{Lane: "b.dev", Name: "b · dev · Deploy", Index: 1, Env: "dev", Jobs: 1, RunsOn: "big", Level: 1, UpstreamLanes: []string{"a.dev"}}
	if !reflect.DeepEqual(o.Include[1], want) {
		t.Fatalf("entry = %+v, want %+v", o.Include[1], want)
	}
	if o.Include[0].Level != 0 || len(o.Include[0].UpstreamLanes) != 0 || !strings.Contains(s, `"upstreamLanes":[]`) {
		t.Fatalf("a job with no dependencies is level 0 and waits on nothing, as an empty array: %s", s)
	}

	// With lanes: one entry per lane, carrying the lane's level and upstreams.
	plan := &model.Plan{Jobs: jobs, Lanes: []model.PlanLane{
		{ID: "dev/auto-1", Name: "dev · auto 1/1 · a, b", Index: 0, Environment: "dev", Jobs: []string{"a.dev", "b.dev"}, Concurrency: 4},
		{ID: "prod/auto-1", Name: "prod · auto 1/1 · a, b", Index: 1, Environment: "prod", Jobs: []string{"a.prod"}, Concurrency: 4, Level: 1, UpstreamLanes: []string{"dev/auto-1"}, UpstreamJobs: []string{"a.dev"}},
	}}
	o = laneMatrix{}
	s, n = githubLaneMatrixJSON(plan)
	if err := json.Unmarshal([]byte(s), &o); err != nil || n != 2 {
		t.Fatalf("%v %d %s", err, n, s)
	}
	want = laneMatrixEntry{Lane: "dev/auto-1", Name: "dev · auto 1/1 · a, b", Index: 0, Env: "dev", Jobs: 2, UpstreamLanes: []string{}}
	if !reflect.DeepEqual(o.Include[0], want) {
		t.Fatalf("entry = %+v, want %+v", o.Include[0], want)
	}
	want = laneMatrixEntry{Lane: "prod/auto-1", Name: "prod · auto 1/1 · a, b", Index: 1, Env: "prod", Jobs: 1, Level: 1, UpstreamLanes: []string{"dev/auto-1"}}
	if !reflect.DeepEqual(o.Include[1], want) {
		t.Fatalf("entry = %+v, want %+v", o.Include[1], want)
	}
	if s, n := githubLaneMatrixJSON(&model.Plan{}); s != `{"include":[]}` || n != 0 {
		t.Fatalf("empty = %s %d", s, n)
	}

	// Level by level: one matrix per level, in level order, the lanes in
	// matrix order within a level.
	levels := githubLaneMatrixLevels(plan)
	if len(levels) != 2 {
		t.Fatalf("levels = %v", levels)
	}
	var l0, l1 laneMatrix
	if err := json.Unmarshal([]byte(levels[0]), &l0); err != nil || len(l0.Include) != 1 || l0.Include[0].Lane != "dev/auto-1" {
		t.Fatalf("level 0 = %s (%v)", levels[0], err)
	}
	if err := json.Unmarshal([]byte(levels[1]), &l1); err != nil || len(l1.Include) != 1 || l1.Include[0].Lane != "prod/auto-1" {
		t.Fatalf("level 1 = %s (%v)", levels[1], err)
	}
	if got := githubLaneMatrixLevelsJSON(plan); got != "["+levels[0]+","+levels[1]+"]" {
		t.Fatalf("levels json = %s", got)
	}
	if got := githubLaneMatrixLevelsJSON(&model.Plan{}); got != "[]" {
		t.Fatalf("empty levels = %s", got)
	}
	// A job-mode plan (no lanes) is levelled by job depth.
	if levels := githubLaneMatrixLevels(&model.Plan{Jobs: jobs}); len(levels) != 2 || !strings.Contains(levels[1], `"lane":"b.dev"`) {
		t.Fatalf("job-mode levels = %v", levels)
	}
}
