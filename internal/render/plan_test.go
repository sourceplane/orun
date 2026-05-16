package render

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestBuildPlanJobEnvWithExplicitEnv(t *testing.T) {
	job := &model.JobInstance{
		Config: map[string]interface{}{
			"stackName": "my-stack",
			"region":    "us-east-1",
		},
		Env: map[string]string{
			"AWS_REGION": "us-east-1",
			"TF_LOG":     "WARN",
		},
	}

	result := buildPlanJobEnv(job)

	if len(result) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(result))
	}
	if result["AWS_REGION"] != "us-east-1" {
		t.Errorf("AWS_REGION = %v, want us-east-1", result["AWS_REGION"])
	}
	if result["TF_LOG"] != "WARN" {
		t.Errorf("TF_LOG = %v, want WARN", result["TF_LOG"])
	}
}

func TestBuildPlanJobEnvFallsBackToConfig(t *testing.T) {
	job := &model.JobInstance{
		Config: map[string]interface{}{
			"stackName": "my-stack",
			"region":    "us-east-1",
		},
		Env: nil,
	}

	result := buildPlanJobEnv(job)

	if len(result) != 2 {
		t.Fatalf("expected 2 config entries (fallback), got %d", len(result))
	}
	if result["stackName"] != "my-stack" {
		t.Errorf("stackName = %v, want my-stack", result["stackName"])
	}
}

func TestBuildPlanJobEnvEmptyEnvFallsBack(t *testing.T) {
	job := &model.JobInstance{
		Config: map[string]interface{}{
			"key": "value",
		},
		Env: map[string]string{},
	}

	result := buildPlanJobEnv(job)

	if result["key"] != "value" {
		t.Errorf("expected fallback to Config when Env is empty, got %v", result)
	}
}
