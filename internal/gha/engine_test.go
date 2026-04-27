package gha

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/gluon/internal/model"
)

func TestParseKVFileSupportsHeredocAndBOM(t *testing.T) {
	t.Parallel()

	filePath := filepath.Join(t.TempDir(), "env")
	content := "\ufeffFOO=bar\nMULTI<<EOF\nline1\nline2\nEOF\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	values, err := parseKVFile(filePath)
	if err != nil {
		t.Fatalf("parseKVFile() error = %v", err)
	}
	if got := values["FOO"]; got != "bar" {
		t.Fatalf("values[FOO] = %q, want bar", got)
	}
	if got := values["MULTI"]; got != "line1\nline2\n" {
		t.Fatalf("values[MULTI] = %q, want %q", got, "line1\nline2\n")
	}
}

func TestProcessWorkflowCommandsParsesLegacyCommands(t *testing.T) {
	t.Parallel()

	raw := strings.Join([]string{
		"::group::Installing v4.1.4",
		"::debug::cache hit",
		"::add-mask::secret",
		"::set-output name=result::done",
		"::save-state name=token::secret",
		"::add-path::/tmp/bin",
		"::set-env name=GREETING::hello",
		"visible output",
		"secret",
	}, "\n")

	result := processWorkflowCommands(raw, nil)
	if got := result.Outputs["result"]; got != "done" {
		t.Fatalf("result.Outputs[result] = %q, want done", got)
	}
	if got := result.State["token"]; got != "secret" {
		t.Fatalf("result.State[token] = %q, want secret", got)
	}
	if got := result.Env["GREETING"]; got != "hello" {
		t.Fatalf("result.Env[GREETING] = %q, want hello", got)
	}
	if len(result.Paths) != 1 || result.Paths[0] != "/tmp/bin" {
		t.Fatalf("result.Paths = %#v, want [/tmp/bin]", result.Paths)
	}
	if strings.Contains(result.Output, "::group::") || strings.Contains(result.Output, "::debug::") || strings.Contains(result.Output, "::set-output") {
		t.Fatalf("result.Output = %q, want workflow command lines removed", result.Output)
	}
	if !strings.Contains(result.Output, "visible output") {
		t.Fatalf("result.Output = %q, want visible payload preserved", result.Output)
	}
	if !strings.Contains(result.Output, "***") {
		t.Fatalf("result.Output = %q, want masked content", result.Output)
	}
}

func TestEngineJobsGetIsolatedHomeDirectories(t *testing.T) {
	t.Parallel()

	workspaceDir := t.TempDir()
	engine := NewEngine(Options{
		CacheDir:     filepath.Join(workspaceDir, ".cache", "actions"),
		ToolCacheDir: filepath.Join(workspaceDir, ".cache", "tools"),
	})
	execCtx := ExecContext{
		Context:      context.Background(),
		WorkspaceDir: workspaceDir,
		WorkDir:      workspaceDir,
		BaseEnv:      map[string]string{"PATH": os.Getenv("PATH")},
	}
	if err := engine.Prepare(execCtx); err != nil {
		t.Fatalf("engine.Prepare() error = %v", err)
	}

	jobA := model.PlanJob{ID: "job-a"}
	jobB := model.PlanJob{ID: "job-b"}

	homeA, err := engine.RunStep(execCtx, jobA, model.PlanStep{ID: "home", Run: `printf '%s' "$HOME"`, Shell: "bash"})
	if err != nil {
		t.Fatalf("job-a RunStep() error = %v", err)
	}
	homeB, err := engine.RunStep(execCtx, jobB, model.PlanStep{ID: "home", Run: `printf '%s' "$HOME"`, Shell: "bash"})
	if err != nil {
		t.Fatalf("job-b RunStep() error = %v", err)
	}

	realHome := os.Getenv("HOME")
	if homeA == realHome {
		t.Fatalf("job-a HOME = %q, should not equal real HOME", homeA)
	}
	if homeB == realHome {
		t.Fatalf("job-b HOME = %q, should not equal real HOME", homeB)
	}
	if homeA == homeB {
		t.Fatalf("job-a and job-b share HOME = %q, should be isolated", homeA)
	}
}

func TestEngineRunStepLocalCompositeActionPropagatesOutputsAndEnv(t *testing.T) {
	t.Parallel()

	workspaceDir := t.TempDir()
	actionDir := filepath.Join(workspaceDir, ".github", "actions", "greet")
	if err := os.MkdirAll(actionDir, 0755); err != nil {
		t.Fatalf("create action directory: %v", err)
	}

	actionMetadata := strings.TrimSpace(`
name: greet
inputs:
  greeting:
    default: hello
outputs:
  message:
    value: ${{ steps.build.outputs.message }}
runs:
  using: composite
  steps:
    - id: seed
      shell: bash
      run: |
        echo "GREETING=$INPUT_GREETING" >> "$GITHUB_ENV"
        echo "enabled=true" >> "$GITHUB_OUTPUT"
    - id: build
      if: ${{ steps.seed.outputs.enabled == 'true' }}
      shell: bash
      run: |
        printf 'message=%s world\n' "$GREETING" >> "$GITHUB_OUTPUT"
`) + "\n"
	if err := os.WriteFile(filepath.Join(actionDir, "action.yml"), []byte(actionMetadata), 0644); err != nil {
		t.Fatalf("write action metadata: %v", err)
	}

	engine := NewEngine(Options{
		CacheDir:     filepath.Join(workspaceDir, ".cache", "actions"),
		ToolCacheDir: filepath.Join(workspaceDir, ".cache", "tools"),
	})
	execCtx := ExecContext{
		Context:      context.Background(),
		WorkspaceDir: workspaceDir,
		WorkDir:      workspaceDir,
		BaseEnv: map[string]string{
			"PATH": os.Getenv("PATH"),
			"HOME": os.Getenv("HOME"),
		},
	}
	if err := engine.Prepare(execCtx); err != nil {
		t.Fatalf("engine.Prepare() error = %v", err)
	}
	t.Cleanup(func() {
		if err := engine.Cleanup(execCtx); err != nil {
			t.Fatalf("engine.Cleanup() error = %v", err)
		}
	})

	job := model.PlanJob{ID: "gha-job"}
	if _, err := engine.RunStep(execCtx, job, model.PlanStep{
		ID:   "local",
		Use:  "./.github/actions/greet",
		With: map[string]interface{}{"greeting": "hello"},
	}); err != nil {
		t.Fatalf("engine.RunStep() local action error = %v", err)
	}

	output, err := engine.RunStep(execCtx, job, model.PlanStep{
		ID:    "verify",
		Run:   `printf '%s|%s' "$GREETING" '${{ steps.local.outputs.message }}'`,
		Shell: "bash",
	})
	if err != nil {
		t.Fatalf("engine.RunStep() verify error = %v", err)
	}
	if output != "hello|hello world" {
		t.Fatalf("verify output = %q, want %q", output, "hello|hello world")
	}

	finalOutput, err := engine.FinalizeJob(execCtx, job)
	if err != nil {
		t.Fatalf("engine.FinalizeJob() error = %v", err)
	}
	if finalOutput != "" {
		t.Fatalf("engine.FinalizeJob() output = %q, want empty", finalOutput)
	}
}
