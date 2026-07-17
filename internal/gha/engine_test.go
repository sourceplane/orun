package gha

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
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
	if got := values["MULTI"]; got != "line1\nline2" {
		t.Fatalf("values[MULTI] = %q, want %q", got, "line1\nline2")
	}
}

func TestDecodeCommandValuePreservesSTSTokenCharacters(t *testing.T) {
	t.Parallel()

	// AWS STS session tokens contain +, /, = which the GitHub Actions runner percent-encodes
	// in ::set-env:: commands. These must survive the handoff layer intact.
	raw := "::set-env name=AWS_SESSION_TOKEN::AQoXnyc1KWCugJh%2F%2FWfgJ2GHaEm4D%2FbVfJi3mEXAMPLEKEY%2FLhDs%2F3tCGz%3D%3D"
	result := processWorkflowCommands(raw, nil)
	want := "AQoXnyc1KWCugJh//WfgJ2GHaEm4D/bVfJi3mEXAMPLEKEY/LhDs/3tCGz=="
	if got := result.Env["AWS_SESSION_TOKEN"]; got != want {
		t.Fatalf("AWS_SESSION_TOKEN = %q, want %q", got, want)
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

func TestEngineRunStepOrunEnvAliasesGithubEnv(t *testing.T) {
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

	job := model.PlanJob{ID: "orun-env-job"}

	// Step 1: write env var using ORUN_ENV (the alias)
	if _, err := engine.RunStep(execCtx, job, model.PlanStep{
		ID:    "set-via-orun-env",
		Run:   `echo "MY_VAR=from_orun_env" >> "$ORUN_ENV"`,
		Shell: "bash",
	}); err != nil {
		t.Fatalf("engine.RunStep() set-via-orun-env error = %v", err)
	}

	// Step 2: verify the env var is available in a subsequent step
	output, err := engine.RunStep(execCtx, job, model.PlanStep{
		ID:    "read-var",
		Run:   `printf '%s' "$MY_VAR"`,
		Shell: "bash",
	})
	if err != nil {
		t.Fatalf("engine.RunStep() read-var error = %v", err)
	}
	if output != "from_orun_env" {
		t.Fatalf("read-var output = %q, want %q", output, "from_orun_env")
	}

	// Step 3: verify ORUN_ENV and GITHUB_ENV point to the same file
	pathOutput, err := engine.RunStep(execCtx, job, model.PlanStep{
		ID:    "compare-paths",
		Run:   `test "$ORUN_ENV" = "$GITHUB_ENV" && printf 'same'`,
		Shell: "bash",
	})
	if err != nil {
		t.Fatalf("engine.RunStep() compare-paths error = %v", err)
	}
	if pathOutput != "same" {
		t.Fatalf("compare-paths output = %q, want %q", pathOutput, "same")
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

func TestParseKVFilePreservesSpecialCharactersInHeredoc(t *testing.T) {
	t.Parallel()

	fakeToken := "IQoJb3JpZ2luX2VjEBYaCXVzLWVhc3QtMSJI+MEY/CIQC5p+Fc/bVfJi3mEXAMPLEKEY/LhDs/3tCGz=="
	content := fmt.Sprintf("AWS_SESSION_TOKEN<<ghadelimiter_abc123\n%s\nghadelimiter_abc123\n", fakeToken)

	filePath := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	values, err := parseKVFile(filePath)
	if err != nil {
		t.Fatalf("parseKVFile() error = %v", err)
	}
	if got := values["AWS_SESSION_TOKEN"]; got != fakeToken {
		t.Fatalf("AWS_SESSION_TOKEN mismatch:\n  got:  %q\n  want: %q", got, fakeToken)
	}
}

func TestParseKVFileSimpleEqualsPreservesSpecialChars(t *testing.T) {
	t.Parallel()

	fakeToken := "IQoJb3Jp+Z2luX2Vj/EBYaCXVz+LWVhc3QtMSJI/MEYCIQC5p+Fc/bVfJi3m=="
	content := fmt.Sprintf("AWS_SESSION_TOKEN=%s\nAWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n", fakeToken)

	filePath := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	values, err := parseKVFile(filePath)
	if err != nil {
		t.Fatalf("parseKVFile() error = %v", err)
	}
	if got := values["AWS_SESSION_TOKEN"]; got != fakeToken {
		t.Fatalf("AWS_SESSION_TOKEN mismatch:\n  got:  %q\n  want: %q", got, fakeToken)
	}
	if got := values["AWS_ACCESS_KEY_ID"]; got != "AKIAIOSFODNN7EXAMPLE" {
		t.Fatalf("AWS_ACCESS_KEY_ID = %q, want AKIAIOSFODNN7EXAMPLE", got)
	}
}

func TestEngineStepToStepEnvPreservesAWSSessionToken(t *testing.T) {
	t.Parallel()

	fakeToken := "IQoJb3JpZ2luX2VjEBYaCXVzLWVhc3QtMSJI+MEY/CIQC5p+Fc/bVfJi3mEXAMPLEKEY/LhDs/3tCGz=="

	workspaceDir := t.TempDir()
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

	job := model.PlanJob{ID: "aws-env-job"}

	// Simulate configure-aws-credentials writing via heredoc to GITHUB_ENV
	writeScript := `cat >> "$GITHUB_ENV" <<'ORUN_HEREDOC_END'
AWS_SESSION_TOKEN<<ghadelimiter_test
` + fakeToken + `
ghadelimiter_test
ORUN_HEREDOC_END`

	if _, err := engine.RunStep(execCtx, job, model.PlanStep{
		ID:    "set-creds",
		Run:   writeScript,
		Shell: "bash",
	}); err != nil {
		t.Fatalf("set-creds step error = %v", err)
	}

	// Read the env var and verify it's exactly the token
	output, err := engine.RunStep(execCtx, job, model.PlanStep{
		ID:    "verify-token",
		Run:   `printf '%s' "$AWS_SESSION_TOKEN"`,
		Shell: "bash",
	})
	if err != nil {
		t.Fatalf("verify-token step error = %v", err)
	}
	if output != fakeToken {
		t.Fatalf("AWS_SESSION_TOKEN corrupted between steps:\n  got:  %q\n  want: %q", output, fakeToken)
	}
}

func TestEngineEnvIsolationBetweenJobs(t *testing.T) {
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
		BaseEnv: map[string]string{
			"PATH": os.Getenv("PATH"),
			"HOME": os.Getenv("HOME"),
		},
	}
	if err := engine.Prepare(execCtx); err != nil {
		t.Fatalf("engine.Prepare() error = %v", err)
	}

	jobA := model.PlanJob{ID: "job-a"}
	jobB := model.PlanJob{ID: "job-b"}

	// Job A sets an env var
	if _, err := engine.RunStep(execCtx, jobA, model.PlanStep{
		ID:    "set",
		Run:   `echo "PRIVATE_VAR=from_job_a" >> "$GITHUB_ENV"`,
		Shell: "bash",
	}); err != nil {
		t.Fatalf("job-a set error = %v", err)
	}

	// Job B should not see it
	output, err := engine.RunStep(execCtx, jobB, model.PlanStep{
		ID:    "read",
		Run:   `printf '%s' "${PRIVATE_VAR:-unset}"`,
		Shell: "bash",
	})
	if err != nil {
		t.Fatalf("job-b read error = %v", err)
	}
	if output != "unset" {
		t.Fatalf("env leaked between jobs: PRIVATE_VAR = %q in job-b", output)
	}
}

// Regression (orun-secrets runner-integration §1): the resolved secret layer
// must reach step processes under the github-actions engine. It was dropped
// entirely — secrets resolved (and brokered credentials minted) successfully
// but steps never saw them. SecretEnv is the TOP layer (beats JobEnv) and is
// never expression-interpolated.
func TestEngineRunStepReceivesSecretEnvAsTopLayer(t *testing.T) {
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
		BaseEnv: map[string]string{
			"PATH": os.Getenv("PATH"),
			"HOME": os.Getenv("HOME"),
		},
		JobEnv: map[string]string{
			"SHADOWED": "from_job_env",
		},
		SecretEnv: map[string]string{
			"TEST_SUPABASE_API": "sbp_minted_0123456789abcdef",
			"SHADOWED":          "from_secret_env",
			// Secrets are opaque: expression-ish content must stay literal.
			"WEIRD_SECRET": "${{ not.evaluated }}",
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

	job := model.PlanJob{ID: "secret-env-job"}

	output, err := engine.RunStep(execCtx, job, model.PlanStep{
		ID:    "read-secret",
		Run:   `printf '%s|%s|%s' "$TEST_SUPABASE_API" "$SHADOWED" "$WEIRD_SECRET"`,
		Shell: "bash",
	})
	if err != nil {
		t.Fatalf("engine.RunStep() read-secret error = %v", err)
	}
	want := "sbp_minted_0123456789abcdef|from_secret_env|${{ not.evaluated }}"
	if output != want {
		t.Fatalf("read-secret output = %q, want %q", output, want)
	}
}
