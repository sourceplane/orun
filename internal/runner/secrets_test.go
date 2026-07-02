package runner

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/redact"
)

// envCapturingExecutor records each step's merged Env and echoes a secret.
type envCapturingExecutor struct {
	mu   sync.Mutex
	envs map[string]map[string]string // jobID/stepID -> Env
	echo string                       // returned as step output
}

func (*envCapturingExecutor) Name() string                       { return "cap" }
func (*envCapturingExecutor) Prepare(executor.ExecContext) error { return nil }
func (*envCapturingExecutor) Cleanup(executor.ExecContext) error { return nil }
func (e *envCapturingExecutor) RunStep(ctx executor.ExecContext, job model.PlanJob, step model.PlanStep) (string, error) {
	e.mu.Lock()
	if e.envs == nil {
		e.envs = map[string]map[string]string{}
	}
	copied := make(map[string]string, len(ctx.Env))
	for k, v := range ctx.Env {
		copied[k] = v
	}
	e.envs[job.ID+"/"+step.ID] = copied
	e.mu.Unlock()
	return e.echo, nil
}

func secretPlan() *model.Plan {
	return &model.Plan{Execution: model.PlanExecution{FailFast: true}, Jobs: []model.PlanJob{{
		ID:        "api@deploy",
		Name:      "api",
		Component: "api",
		Env:       map[string]interface{}{"LOG_LEVEL": "info", "DATABASE_URL": "overridden-by-secret"},
		SecretRefs: []model.PlanSecretRef{
			{AsEnv: "DATABASE_URL", Ref: "secret://acme/api/prod/DATABASE_URL"},
		},
		Steps: []model.PlanStep{{ID: "deploy", Name: "deploy"}},
	}}}
}

func newSecretTestRunner(exec executor.Executor) *Runner {
	// Positional args mirror resume_test.go's NewRunner call.
	return NewRunner("/tmp", true, io.Discard, io.Discard, false, "", false, false,
		exec, executor.RuntimeContext{}, "exec_secrets", 1, nil, "")
}

func TestSecretsInjectedAsTopEnvLayer(t *testing.T) {
	exec := &envCapturingExecutor{echo: "ok"}
	r := newSecretTestRunner(exec)
	r.Redactor = redact.New()
	r.Hooks = &RunnerHooks{
		ResolveJobSecrets: func(jobID string, refs []model.PlanSecretRef) (map[string]string, error) {
			return map[string]string{"DATABASE_URL": "postgres://real-secret-value"}, nil
		},
	}

	if err := r.Run(secretPlan()); err != nil {
		t.Fatalf("run: %v", err)
	}
	env := exec.envs["api@deploy/deploy"]
	if env["DATABASE_URL"] != "postgres://real-secret-value" {
		t.Errorf("secret env must be the highest-precedence layer, got %q", env["DATABASE_URL"])
	}
	if env["LOG_LEVEL"] != "info" {
		t.Errorf("plain env must be preserved, got %q", env["LOG_LEVEL"])
	}
}

func TestSecretsRedactedFromAllOutputSinks(t *testing.T) {
	const secret = "postgres://real-secret-value"
	exec := &envCapturingExecutor{echo: "connecting to " + secret + " now"}
	r := newSecretTestRunner(exec)
	r.Redactor = redact.New()

	var hookOutput string
	r.Hooks = &RunnerHooks{
		ResolveJobSecrets: func(string, []model.PlanSecretRef) (map[string]string, error) {
			return map[string]string{"DATABASE_URL": secret}, nil
		},
		AfterStepLog: func(jobID, stepID, output string) {
			hookOutput = output
		},
	}

	if err := r.Run(secretPlan()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(hookOutput, secret) {
		t.Fatalf("secret leaked into AfterStepLog output: %q", hookOutput)
	}
	if !strings.Contains(hookOutput, redact.Mask) {
		t.Errorf("expected mask in step output, got %q", hookOutput)
	}
}

func TestSecretsFailClosedWithoutResolver(t *testing.T) {
	exec := &envCapturingExecutor{echo: "ok"}
	r := newSecretTestRunner(exec)
	// No ResolveJobSecrets hook wired.
	err := r.Run(secretPlan())
	if err == nil {
		t.Fatal("expected run failure when secrets cannot be resolved")
	}
	if len(exec.envs) != 0 {
		t.Errorf("no step may run when resolution fails, ran: %v", exec.envs)
	}
}

func TestSecretsFailClosedOnResolverError(t *testing.T) {
	exec := &envCapturingExecutor{echo: "ok"}
	r := newSecretTestRunner(exec)
	r.Hooks = &RunnerHooks{
		ResolveJobSecrets: func(string, []model.PlanSecretRef) (map[string]string, error) {
			return nil, errors.New("policy denial: laptops-never-prod")
		},
	}
	err := r.Run(secretPlan())
	if err == nil {
		t.Fatal("expected run failure on resolver denial")
	}
	if len(exec.envs) != 0 {
		t.Errorf("no step may run after a denial, ran: %v", exec.envs)
	}
}

func TestSecretsMissingKeyFailsClosed(t *testing.T) {
	exec := &envCapturingExecutor{echo: "ok"}
	r := newSecretTestRunner(exec)
	r.Hooks = &RunnerHooks{
		ResolveJobSecrets: func(string, []model.PlanSecretRef) (map[string]string, error) {
			return map[string]string{}, nil // resolver returned, but without the key
		},
	}
	if err := r.Run(secretPlan()); err == nil {
		t.Fatal("expected run failure when a declared ref has no value")
	}
}

func TestDryRunSkipsSecretResolution(t *testing.T) {
	exec := &envCapturingExecutor{echo: "ok"}
	r := NewRunner("/tmp", true, io.Discard, io.Discard, true /* dry-run */, "", false, false,
		exec, executor.RuntimeContext{}, "exec_secrets_dry", 1, nil, "")
	// No resolver wired: dry-run must not need one.
	if err := r.Run(secretPlan()); err != nil {
		t.Fatalf("dry-run must not resolve secrets: %v", err)
	}
}
