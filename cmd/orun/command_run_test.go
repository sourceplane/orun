package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestResolveRunnerNameDefaultsToLocal(t *testing.T) {
	t.Setenv(runnerEnvVar, "")
	t.Setenv("GITHUB_ACTIONS", "")

	if got := resolveRunnerName(""); got != "local" {
		t.Fatalf("resolveRunnerName() = %q, want local", got)
	}
}

func TestResolveRunnerNameHonorsPrimaryEnvThenAutoDetect(t *testing.T) {
	t.Setenv(runnerEnvVar, "docker")
	t.Setenv("GITHUB_ACTIONS", "true")

	if got := resolveRunnerName(""); got != "docker" {
		t.Fatalf("resolveRunnerName() = %q, want docker", got)
	}
}

func TestResolveRunnerNameHonorsFlag(t *testing.T) {
	t.Setenv(runnerEnvVar, "docker")
	t.Setenv("GITHUB_ACTIONS", "true")

	if got := resolveRunnerName("github-actions"); got != "github-actions" {
		t.Fatalf("resolveRunnerName() = %q, want github-actions", got)
	}
}

func TestShouldAutoUseGitHubActionsForPlanUseSteps(t *testing.T) {
	t.Setenv(runnerEnvVar, "")
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
	t.Setenv("GITHUB_ACTIONS", "")
	if shouldAutoUseGitHubActions("local", plan) {
		t.Fatal("expected explicit --runner local to disable auto-detect")
	}

	t.Setenv(runnerEnvVar, "docker")
	if shouldAutoUseGitHubActions("", plan) {
		t.Fatal("expected primary runner env var to disable auto-detect")
	}
}

func TestRunCommandRegistersVerboseFlag(t *testing.T) {
	if runCmd.Flags().Lookup("verbose") == nil {
		t.Fatal("expected run command to register --verbose")
	}
}

func TestRunCommandRegistersChangedFlag(t *testing.T) {
	if runCmd.Flags().Lookup("changed") == nil {
		t.Fatal("expected run command to register --changed")
	}
}

func TestRunCommandRegistersChangedDependencyFlags(t *testing.T) {
	for _, name := range []string{"base", "head", "files", "uncommitted", "untracked", "explain"} {
		if runCmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected run command to register --%s", name)
		}
	}
}

func TestRunCommandAcceptsPositionalArg(t *testing.T) {
	if runCmd.Args == nil {
		t.Fatal("expected run command to have an Args validator")
	}
	if err := runCmd.Args(runCmd, []string{"abc123"}); err != nil {
		t.Fatalf("expected single positional arg to be accepted: %v", err)
	}
}

func TestRunCommandRejectsTooManyArgs(t *testing.T) {
	if err := runCmd.Args(runCmd, []string{"abc123", "def456"}); err == nil {
		t.Fatal("expected two positional args to be rejected")
	}
}

func TestRunCommandUseSyntaxIncludesComponentAndPlanhash(t *testing.T) {
	if !strings.Contains(runCmd.Use, "component") || !strings.Contains(runCmd.Use, "planhash") {
		t.Fatalf("expected runCmd.Use to reference component and planhash, got %q", runCmd.Use)
	}
}

func TestResolveEffectiveWorkDirUsesIntentRoot(t *testing.T) {
	got := resolveEffectiveWorkDir(false, ".", "/abs/project/root")
	if got != "/abs/project/root" {
		t.Fatalf("resolveEffectiveWorkDir() = %q, want /abs/project/root", got)
	}
}

func TestResolveEffectiveWorkDirHonorsOverride(t *testing.T) {
	got := resolveEffectiveWorkDir(true, "/explicit/dir", "/abs/project/root")
	if got != "/explicit/dir" {
		t.Fatalf("resolveEffectiveWorkDir() = %q, want /explicit/dir", got)
	}
}

func TestResolveEffectiveWorkDirFallsBackToAbsCWD(t *testing.T) {
	got := resolveEffectiveWorkDir(false, ".", "")
	if got == "." {
		t.Fatal("resolveEffectiveWorkDir() should resolve '.' to an absolute path when no intentRoot")
	}
}

func TestResolveEffectiveWorkDirPreservesNonDotWorkDir(t *testing.T) {
	// When workdir was already changed (e.g. from GITHUB_WORKSPACE) before calling
	// and intentRoot is empty, the non-"." workDir should be preserved.
	got := resolveEffectiveWorkDir(false, "/github/workspace", "")
	if got != "/github/workspace" {
		t.Fatalf("resolveEffectiveWorkDir() = %q, want /github/workspace", got)
	}
}

func TestResolveEffectiveWorkDirPrefersIntentRootOverNonDotWorkDir(t *testing.T) {
	// When intentRoot is set (intent in a subdirectory), it must win over GITHUB_WORKSPACE
	// so component paths like "infra/infra-1" resolve relative to the intent dir.
	got := resolveEffectiveWorkDir(false, ".", "/github/workspace/examples")
	if got != "/github/workspace/examples" {
		t.Fatalf("resolveEffectiveWorkDir() = %q, want /github/workspace/examples", got)
	}
}

func TestPlanWorkDirRecoveredWhenIntentRootEmpty(t *testing.T) {
	// Simulates: orun plan --intent examples/intent.yaml generates plan with
	// workDir="examples"; orun run in GHA from workspace root recovers intentRoot.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	planWorkDir := "examples"
	got := filepath.Join(cwd, filepath.FromSlash(planWorkDir))
	want := filepath.Join(cwd, "examples")
	if got != want {
		t.Fatalf("recovered intentRoot = %q, want %q", got, want)
	}
	// Verify resolveEffectiveWorkDir then returns the intent dir (not ".")
	final := resolveEffectiveWorkDir(false, ".", got)
	if final != want {
		t.Fatalf("resolveEffectiveWorkDir() = %q, want %q", final, want)
	}
}
