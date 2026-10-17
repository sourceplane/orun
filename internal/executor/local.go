package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/outtee"
	"github.com/sourceplane/orun/internal/procgroup"
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
	procgroup.Configure(cmd)
	cmd.Dir = execCtx.WorkDir
	cmd.Env = EnvironmentList(execCtx.Env)

	var buf bytes.Buffer
	// The step's live sink, when the runner gave one, reads the output as it
	// arrives (saas-live-runs LR8); the returned output is unchanged.
	capture := outtee.Capture(commandCtx, &buf)
	cmd.Stdout = capture
	cmd.Stderr = capture

	err := cmd.Run()
	return strings.TrimRight(buf.String(), "\n"), err
}

func (e *localExecutor) Cleanup(ctx ExecContext) error {
	return nil
}
