package main

import (
	"testing"

	"github.com/sourceplane/arx/internal/model"
)

func TestResolveRunnerNameDefaultsToLocal(t *testing.T) {
	t.Setenv(runnerEnvVar, "")
	t.Setenv(legacyRunnerEnvVar, "")
	t.Setenv(oldestRunnerEnvVar, "")
	t.Setenv("GITHUB_ACTIONS", "")

	if got := resolveRunnerName(""); got != "local" {
		t.Fatalf("resolveRunnerName() = %q, want local", got)
	}
}

func TestResolveRunnerNameHonorsPrimaryEnvThenAutoDetect(t *testing.T) {
	t.Setenv(runnerEnvVar, "docker")
	t.Setenv(legacyRunnerEnvVar, "")
	t.Setenv(oldestRunnerEnvVar, "")
	t.Setenv("GITHUB_ACTIONS", "true")

	if got := resolveRunnerName(""); got != "docker" {
		t.Fatalf("resolveRunnerName() = %q, want docker", got)
	}
}

func TestResolveRunnerNameHonorsLegacyEnvThenAutoDetect(t *testing.T) {
	t.Setenv(runnerEnvVar, "")
	t.Setenv(legacyRunnerEnvVar, "docker")
	t.Setenv(oldestRunnerEnvVar, "")
	t.Setenv("GITHUB_ACTIONS", "true")

	if got := resolveRunnerName(""); got != "docker" {
		t.Fatalf("resolveRunnerName() = %q, want docker", got)
	}
}

func TestResolveRunnerNameHonorsFlag(t *testing.T) {
	t.Setenv(runnerEnvVar, "docker")
	t.Setenv(legacyRunnerEnvVar, "")
	t.Setenv(oldestRunnerEnvVar, "")
	t.Setenv("GITHUB_ACTIONS", "true")

	if got := resolveRunnerName("github-actions"); got != "github-actions" {
		t.Fatalf("resolveRunnerName() = %q, want github-actions", got)
	}
}

func TestShouldAutoUseGitHubActionsForPlanUseSteps(t *testing.T) {
	t.Setenv(runnerEnvVar, "")
	t.Setenv(legacyRunnerEnvVar, "")
	t.Setenv(oldestRunnerEnvVar, "")
	t.Setenv("GITHUB_ACTIONS", "")

	plan := &model.Plan{
		Jobs: []model.PlanJob{{
			Steps: []model.PlanStep{{Use: "azure/setup-helm@v4.3.0"}},
		}},
	}

	if !shouldAutoUseGitHubActions("", plan) {
		t.Fatal("expected plans with use steps to auto-select github-actions")
	}
}

func TestShouldAutoUseGitHubActionsHonorsExplicitRunnerSettings(t *testing.T) {
	plan := &model.Plan{
		Jobs: []model.PlanJob{{
			Steps: []model.PlanStep{{Use: "azure/setup-helm@v4.3.0"}},
		}},
	}

	t.Setenv(runnerEnvVar, "")
	t.Setenv(legacyRunnerEnvVar, "")
	t.Setenv(oldestRunnerEnvVar, "")
	t.Setenv("GITHUB_ACTIONS", "")
	if shouldAutoUseGitHubActions("local", plan) {
		t.Fatal("expected explicit --runner local to disable auto-detect")
	}

	t.Setenv(legacyRunnerEnvVar, "docker")
	if shouldAutoUseGitHubActions("", plan) {
		t.Fatal("expected legacy runner env var to disable auto-detect")
	}

	t.Setenv(legacyRunnerEnvVar, "")
	t.Setenv(runnerEnvVar, "docker")
	if shouldAutoUseGitHubActions("", plan) {
		t.Fatal("expected primary runner env var to disable auto-detect")
	}
}
