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
