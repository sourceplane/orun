package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sourceplane/arx/internal/model"
)

type localExecutor struct{}

func (e *localExecutor) Name() string {
	return "local"
}

func (e *localExecutor) Prepare(ctx ExecContext) error {
	if _, err := exec.LookPath("sh"); err != nil {
		return fmt.Errorf("shell not available: %w", err)
	}
	return nil
}

func (e *localExecutor) RunStep(execCtx ExecContext, job model.PlanJob, step model.PlanStep) (string, error) {
	if strings.TrimSpace(step.Use) != "" {
		return "", fmt.Errorf("step %q uses GitHub Actions syntax (%s); rerun with --gha or --runner github-actions", step.Name, step.Use)
	}

	commandCtx := execCtx.Context
	if commandCtx == nil {
		commandCtx = context.Background()
	}

	cmd := exec.CommandContext(commandCtx, "sh", "-c", step.Run)
	cmd.Dir = execCtx.WorkDir
	cmd.Env = EnvironmentList(execCtx.Env)

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	return strings.TrimRight(buf.String(), "\n"), err
}

func (e *localExecutor) Cleanup(ctx ExecContext) error {
	return nil
}
