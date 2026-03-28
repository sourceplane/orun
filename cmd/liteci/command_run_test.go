package main

import "testing"

func TestResolveRunnerNameDefaultsToLocal(t *testing.T) {
	t.Setenv("LITECI_RUNNER", "")
	t.Setenv("GITHUB_ACTIONS", "")

	if got := resolveRunnerName(""); got != "local" {
		t.Fatalf("resolveRunnerName() = %q, want local", got)
	}
}

func TestResolveRunnerNameHonorsEnvThenAutoDetect(t *testing.T) {
	t.Setenv("LITECI_RUNNER", "docker")
	t.Setenv("GITHUB_ACTIONS", "true")

	if got := resolveRunnerName(""); got != "docker" {
		t.Fatalf("resolveRunnerName() = %q, want docker", got)
	}
}

func TestResolveRunnerNameHonorsFlag(t *testing.T) {
	t.Setenv("LITECI_RUNNER", "docker")
	t.Setenv("GITHUB_ACTIONS", "true")

	if got := resolveRunnerName("github-actions"); got != "github-actions" {
		t.Fatalf("resolveRunnerName() = %q, want github-actions", got)
	}
}
