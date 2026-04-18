package executor

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/sourceplane/arx/internal/model"
)

const (
	DefaultDockerImage = "ubuntu:22.04"
	WorkspaceMountPath = "/workspace"
)

// RuntimeContext captures the selected execution backend and environment class.
type RuntimeContext struct {
	Runner      string
	Environment string
}

// ExecContext carries runtime settings for a single executor invocation.
type ExecContext struct {
	Context            context.Context
	WorkspaceDir       string
	WorkDir            string
	UseWorkDirOverride bool
	BaseEnv            map[string]string
	JobEnv             map[string]string
	StepEnv            map[string]string
	Env                map[string]string
	Runtime            RuntimeContext
	Stdout             io.Writer
	Stderr             io.Writer
	DryRun             bool
}

// Executor runs plan steps using a specific backend.
type Executor interface {
	Name() string
	Prepare(ctx ExecContext) error
	RunStep(ctx ExecContext, job model.PlanJob, step model.PlanStep) (output string, err error)
	Cleanup(ctx ExecContext) error
}

// JobFinalizer is implemented by executors that need job-level cleanup or post-step handling.
type JobFinalizer interface {
	FinalizeJob(ctx ExecContext, job model.PlanJob) (output string, err error)
}

func NormalizeRunnerName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func EnvironmentFromList(values []string) map[string]string {
	env := make(map[string]string, len(values))
	for _, value := range values {
		key, raw, found := strings.Cut(value, "=")
		if !found {
			continue
		}
		env[key] = raw
	}
	return env
}

func EnvironmentList(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}

	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, fmt.Sprintf("%s=%s", key, env[key]))
	}
	return values
}

func MergeEnvironment(envs ...map[string]string) map[string]string {
	merged := map[string]string{}
	for _, env := range envs {
		for key, value := range env {
			merged[key] = value
		}
	}
	return merged
}

func JobEnvironment(values map[string]interface{}) map[string]string {
	if len(values) == 0 {
		return nil
	}

	env := make(map[string]string, len(values))
	for key, value := range values {
		env[key] = fmt.Sprint(value)
	}
	return env
}
