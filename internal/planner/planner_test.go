package planner

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestControlsInTemplateContext(t *testing.T) {
	jp := NewJobPlanner(map[string]*CompositionInfo{
		"terraform": {
			Type: "terraform",
			DefaultJob: &model.JobSpec{
				Name: "validate",
				Steps: []model.Step{
					{Name: "check", Run: "echo plan={{(index .controls \"plan\").enabled}}"},
				},
			},
		},
	})

	instances := map[string][]*model.ComponentInstance{
		"dev": {
			{
				ComponentName: "vpc",
				Environment:   "dev",
				Type:          "terraform",
				Inputs:        map[string]interface{}{},
				Controls: map[string]interface{}{
					"plan": map[string]interface{}{"enabled": true},
				},
				Policies:  map[string]interface{}{},
				DependsOn: []model.ResolvedDependency{},
				Enabled:   true,
			},
		},
	}

	jobs, err := jp.PlanJobs(instances)
	if err != nil {
		t.Fatalf("PlanJobs error: %v", err)
	}

	for _, job := range jobs {
		if len(job.Steps) == 0 {
			t.Fatal("expected at least one step")
		}
		if job.Steps[0].Run != "echo plan=true" {
			t.Fatalf("expected controls rendered in step, got %q", job.Steps[0].Run)
		}
	}
}

func TestWhenStepSkipped(t *testing.T) {
	jp := NewJobPlanner(map[string]*CompositionInfo{
		"terraform": {
			Type: "terraform",
			DefaultJob: &model.JobSpec{
				Name: "validate",
				Steps: []model.Step{
					{Name: "always", Run: "echo always"},
					{Name: "skipped", Run: "echo skipped", When: "{{(index .controls \"apply\").enabled}}"},
				},
			},
		},
	})

	instances := map[string][]*model.ComponentInstance{
		"dev": {
			{
				ComponentName: "vpc",
				Environment:   "dev",
				Type:          "terraform",
				Inputs:        map[string]interface{}{},
				Controls: map[string]interface{}{
					"apply": map[string]interface{}{"enabled": false},
				},
				Policies:  map[string]interface{}{},
				DependsOn: []model.ResolvedDependency{},
				Enabled:   true,
			},
		},
	}

	jobs, err := jp.PlanJobs(instances)
	if err != nil {
		t.Fatalf("PlanJobs error: %v", err)
	}

	for _, job := range jobs {
		if len(job.Steps) != 2 {
			t.Fatalf("expected 2 steps, got %d", len(job.Steps))
		}
		if job.Steps[0].Skipped {
			t.Fatal("first step should not be skipped")
		}
		if !job.Steps[1].Skipped {
			t.Fatal("second step should be skipped")
		}
	}
}

func TestWhenJobSkipped(t *testing.T) {
	jp := NewJobPlanner(map[string]*CompositionInfo{
		"terraform": {
			Type: "terraform",
			DefaultJob: &model.JobSpec{
				Name: "apply",
				When: "{{(index .controls \"apply\").enabled}}",
				Steps: []model.Step{
					{Name: "apply", Run: "terraform apply"},
				},
			},
		},
	})

	instances := map[string][]*model.ComponentInstance{
		"dev": {
			{
				ComponentName: "vpc",
				Environment:   "dev",
				Type:          "terraform",
				Inputs:        map[string]interface{}{},
				Controls: map[string]interface{}{
					"apply": map[string]interface{}{"enabled": false},
				},
				Policies:  map[string]interface{}{},
				DependsOn: []model.ResolvedDependency{},
				Enabled:   true,
			},
		},
	}

	jobs, err := jp.PlanJobs(instances)
	if err != nil {
		t.Fatalf("PlanJobs error: %v", err)
	}

	for _, job := range jobs {
		if !job.Skipped {
			t.Fatal("job should be skipped")
		}
		if job.SkipReason == "" {
			t.Fatal("expected skip reason")
		}
	}
}

func TestIsTruthy(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"1", true},
		{"yes", true},
		{"false", false},
		{"0", false},
		{"no", false},
		{"", false},
		{"  true  ", true},
	}

	for _, tc := range cases {
		got := isTruthy(tc.input)
		if got != tc.want {
			t.Errorf("isTruthy(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
