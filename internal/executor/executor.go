package executor

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/model"
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
	// SecretEnv holds resolved secret values for the current job — the
	// highest-precedence layer in Env. In-memory only: never serialized,
	// never written to state, redacted from all captured output.
	SecretEnv map[string]string
	Env       map[string]string
	Runtime   RuntimeContext
	Stdout    io.Writer
	Stderr    io.Writer
	DryRun    bool
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

// CacheObserver is implemented by executors that can tell whether the step
// they just ran for a job was served from a cache they control (the tool
// cache, for a setup action). hit is meaningful only when observed is true;
// a step the executor has no cache verdict for reports observed false.
type CacheObserver interface {
	LastStepCacheHit(jobID string) (hit bool, observed bool)
}

// StepExports is what a step left for the steps after it: the environment
// it exported, the PATH entries it added and its outputs.
type StepExports struct {
	Env     map[string]string
	Paths   []string
	Outputs map[string]string
}

// ExportSharer is an executor that can hand one job's step exports to
// another job, so a lane's setup actions run once: the first job runs
// `setup-node`, and what it exported is applied to every other job of the
// lane with the same step (specs/orun-cache-efficiency CE4). LastStepExports
// reports the job's last step's exports and whether another job may take
// them (not when a value points into the job's own temporary directory);
// ImportStepExports applies them to a job as if the step had run there.
type ExportSharer interface {
	LastStepExports(jobID string) (exports StepExports, shareable bool)
	ImportStepExports(ctx ExecContext, job model.PlanJob, step model.PlanStep, exports StepExports) error
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
