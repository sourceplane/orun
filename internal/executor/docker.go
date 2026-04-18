package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sourceplane/arx/internal/model"
)

type dockerExecutor struct {
	pulledImages map[string]struct{}
}

func (e *dockerExecutor) Name() string {
	return "docker"
}

func (e *dockerExecutor) Prepare(ctx ExecContext) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker CLI not available: %w", err)
	}
	if strings.TrimSpace(ctx.WorkspaceDir) == "" {
		return fmt.Errorf("workspace directory is required for docker runner")
	}
	return nil
}

func (e *dockerExecutor) RunStep(execCtx ExecContext, job model.PlanJob, step model.PlanStep) (string, error) {
	commandCtx := execCtx.Context
	if commandCtx == nil {
		commandCtx = context.Background()
	}

	image := ResolveDockerImage(job.RunsOn)
	if err := e.ensureImage(commandCtx, image); err != nil {
		return "", err
	}

	containerDir, err := containerWorkDir(execCtx.WorkspaceDir, execCtx.WorkDir)
	if err != nil {
		return "", err
	}

	args := []string{
		"run",
		"--rm",
		"-v", fmt.Sprintf("%s:%s", execCtx.WorkspaceDir, WorkspaceMountPath),
		"-w", containerDir,
	}
	for _, value := range EnvironmentList(execCtx.Env) {
		args = append(args, "-e", value)
	}
	args = append(args, image, "sh", "-c", step.Run)

	cmd := exec.CommandContext(commandCtx, "docker", args...)

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err = cmd.Run()
	return strings.TrimRight(buf.String(), "\n"), err
}

func (e *dockerExecutor) Cleanup(ctx ExecContext) error {
	return nil
}

func (e *dockerExecutor) ensureImage(commandCtx context.Context, image string) error {
	if _, ok := e.pulledImages[image]; ok {
		return nil
	}

	cmd := exec.CommandContext(commandCtx, "docker", "pull", image)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Run(); err != nil {
		output := strings.TrimSpace(buf.String())
		if output == "" {
			return fmt.Errorf("failed to pull docker image %s: %w", image, err)
		}
		return fmt.Errorf("failed to pull docker image %s: %w: %s", image, err, output)
	}

	e.pulledImages[image] = struct{}{}
	return nil
}

func ResolveDockerImage(runsOn string) string {
	switch NormalizeRunnerName(runsOn) {
	case "", "ubuntu-22.04":
		return DefaultDockerImage
	case "ubuntu-20.04":
		return "ubuntu:20.04"
	case "ubuntu-24.04":
		return "ubuntu:24.04"
	case "ubuntu-latest":
		return "ubuntu:latest"
	default:
		return strings.TrimSpace(runsOn)
	}
}

func containerWorkDir(workspaceDir, workDir string) (string, error) {
	absWorkspace, err := filepath.Abs(workspaceDir)
	if err != nil {
		return "", fmt.Errorf("resolve workspace dir: %w", err)
	}
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return "", fmt.Errorf("resolve working dir: %w", err)
	}

	rel, err := filepath.Rel(absWorkspace, absWorkDir)
	if err != nil {
		return "", fmt.Errorf("resolve container workdir: %w", err)
	}
	if rel == "." {
		return WorkspaceMountPath, nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("working directory %s is outside mounted workspace %s", absWorkDir, absWorkspace)
	}

	return filepath.ToSlash(filepath.Join(WorkspaceMountPath, rel)), nil
}
