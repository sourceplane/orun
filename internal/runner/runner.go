package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/arx/internal/executor"
	"github.com/sourceplane/arx/internal/model"
	"github.com/sourceplane/arx/internal/ui"
)

const (
	defaultStateFileName = ".arx-state.json"
)

var legacyStateFileNames = []string{".ciz-state.json", ".liteci-state.json"}

// Runner executes a compiled plan in dependency order.
type Runner struct {
	WorkDir            string
	UseWorkDirOverride bool
	Stdout             io.Writer
	Stderr             io.Writer
	DryRun             bool
	JobID              string
	Retry              bool
	Color              bool
	Executor           executor.Executor
	Runtime            executor.RuntimeContext
}

type State struct {
	PlanChecksum string               `json:"planChecksum"`
	Jobs         map[string]*JobState `json:"jobs"`
}

type JobState struct {
	Status     string            `json:"status"`
	StartedAt  string            `json:"startedAt,omitempty"`
	FinishedAt string            `json:"finishedAt,omitempty"`
	Steps      map[string]string `json:"steps"`
	LastError  string            `json:"lastError,omitempty"`
}

type runSummary struct {
	completed int
	skipped   int
	failed    int
	waiting   int
}

func NewRunner(workDir string, useWorkDirOverride bool, stdout, stderr io.Writer, dryRun bool, jobID string, retry bool, exec executor.Executor, runtime executor.RuntimeContext) *Runner {
	return &Runner{
		WorkDir:            workDir,
		UseWorkDirOverride: useWorkDirOverride,
		Stdout:             stdout,
		Stderr:             stderr,
		DryRun:             dryRun,
		JobID:              jobID,
		Retry:              retry,
		Color:              ui.ColorEnabledForWriter(stdout),
		Executor:           exec,
		Runtime:            runtime,
	}
}

func (r *Runner) Run(plan *model.Plan) (runErr error) {
	if plan == nil {
		return fmt.Errorf("plan cannot be nil")
	}
	if len(plan.Jobs) == 0 {
		return fmt.Errorf("plan has no jobs")
	}
	if r.Executor == nil {
		return fmt.Errorf("runner executor cannot be nil")
	}

	workspaceDir, err := r.resolveWorkspaceDir()
	if err != nil {
		return err
	}

	baseExecContext := executor.ExecContext{
		Context:            context.Background(),
		WorkspaceDir:       workspaceDir,
		UseWorkDirOverride: r.UseWorkDirOverride,
		BaseEnv: executor.MergeEnvironment(
			executor.EnvironmentFromList(os.Environ()),
			map[string]string{
				"ARX_CONTEXT":    r.Runtime.Environment,
				"ARX_RUNNER":     r.Runtime.Runner,
				"CIZ_CONTEXT":    r.Runtime.Environment,
				"CIZ_RUNNER":     r.Runtime.Runner,
				"LITECI_CONTEXT": r.Runtime.Environment,
				"LITECI_RUNNER":  r.Runtime.Runner,
			},
		),
		Runtime: r.Runtime,
		Stdout:  r.Stdout,
		Stderr:  r.Stderr,
		DryRun:  r.DryRun,
	}
	baseExecContext.Env = executor.MergeEnvironment(baseExecContext.BaseEnv)

	statePath := r.resolveStateFile(plan)
	state, err := r.loadState(statePath)
	if err != nil {
		return err
	}

	if state.PlanChecksum == "" {
		state.PlanChecksum = plan.Metadata.Checksum
	}
	if plan.Metadata.Checksum != "" && state.PlanChecksum != "" && state.PlanChecksum != plan.Metadata.Checksum {
		return fmt.Errorf("state file checksum mismatch: expected %s, got %s", plan.Metadata.Checksum, state.PlanChecksum)
	}

	if r.JobID != "" && r.Retry {
		state.Jobs[r.JobID] = nil
	}

	persistState := !r.DryRun

	orderedJobs, err := topologicalOrder(plan.Jobs)
	if err != nil {
		return err
	}

	var targetJob *model.PlanJob
	if r.JobID != "" {
		job, err := findJobByID(orderedJobs, r.JobID)
		if err != nil {
			return err
		}
		targetJob = &job
	}

	r.printRunHeader(plan, statePath)
	if targetJob != nil {
		r.printTargetJobSummary(*targetJob, state)
	}

	r.printReadinessSnapshot(orderedJobs, state)

	if !r.DryRun {
		if err := r.Executor.Prepare(baseExecContext); err != nil {
			return fmt.Errorf("prepare runner %s: %w", r.Executor.Name(), err)
		}
		defer func() {
			if cleanupErr := r.Executor.Cleanup(baseExecContext); cleanupErr != nil && runErr == nil {
				runErr = fmt.Errorf("cleanup runner %s: %w", r.Executor.Name(), cleanupErr)
			}
		}()
	}

	failFast := plan.Execution.FailFast
	if !plan.Execution.FailFast {
		// keep explicit false as is
	} else {
		failFast = true
	}

	summary := &runSummary{}

	executedTarget := false

	for _, job := range orderedJobs {
		if r.JobID != "" && job.ID != r.JobID {
			continue
		}

		unmet := unresolvedDependencies(job, state)
		if len(unmet) > 0 {
			summary.waiting++
			r.printWaiting(job, unmet, state)
			if r.JobID != "" {
				return fmt.Errorf("cannot run %s: dependencies not completed (%s)", job.ID, strings.Join(unmet, ", "))
			}
			continue
		}

		executedTarget = true

		jobState := ensureJobState(state, job)
		if jobState.Status == "completed" {
			summary.skipped++
			fmt.Fprintf(r.Stdout, "%s Skip job %s (already completed)\n", ui.Yellow(r.Color, "↷"), job.ID)
			continue
		}

		jobState.Status = "running"
		jobState.FinishedAt = ""
		jobState.LastError = ""
		if jobState.StartedAt == "" {
			jobState.StartedAt = time.Now().UTC().Format(time.RFC3339)
		}
		if persistState {
			if err := r.saveState(statePath, state); err != nil {
				return err
			}
		}

		r.printJobHeader(job)

		jobFailed := false
		jobWorkingDir := r.resolveWorkingDir(job.Path)
		currentPhase := ""
		for idx, step := range job.Steps {
			stepID := stepIdentifier(step)
			stepPhase := normalizeStepPhase(step.Phase)
			if stepPhase != currentPhase {
				r.printPhaseHeader(stepPhase)
				currentPhase = stepPhase
			}
			if jobState.Steps[stepID] == "completed" {
				fmt.Fprintf(r.Stdout, "  │  ↷ Step %d/%d %s (already completed)\n", idx+1, len(job.Steps), stepID)
				continue
			}

			jobState.Steps[stepID] = "running"
			if persistState {
				if err := r.saveState(statePath, state); err != nil {
					return err
				}
			}

			fmt.Fprintf(r.Stdout, "  │  • Step %d/%d  %s\n", idx+1, len(job.Steps), stepID)
			fmt.Fprintf(r.Stdout, "  │    phase: %s    order: %d\n", stepPhase, step.Order)
			workingDir := jobWorkingDir
			fmt.Fprintf(r.Stdout, "  │    cwd: %s\n", workingDir)
			fmt.Fprintf(r.Stdout, "  │    runner: %s\n", r.Executor.Name())
			if step.Run != "" {
				fmt.Fprintf(r.Stdout, "  │    run: %s\n", step.Run)
			}
			if step.Use != "" {
				fmt.Fprintf(r.Stdout, "  │    use: %s\n", step.Use)
			}
			retryCount := r.resolveRetryCount(job, step)
			if retryCount > 0 {
				fmt.Fprintf(r.Stdout, "  │    retries: %d\n", retryCount)
			}
			if timeoutValue := r.resolveTimeout(job, step); timeoutValue != "" {
				fmt.Fprintf(r.Stdout, "  │    timeout: %s\n", timeoutValue)
			}
			if r.DryRun {
				jobState.Steps[stepID] = "completed"
				if persistState {
					if err := r.saveState(statePath, state); err != nil {
						return err
					}
				}
				fmt.Fprintf(r.Stdout, "  │    ✓ completed (dry-run)\n")
				continue
			}

			var output string
			attempts := retryCount + 1
			for attempt := 1; attempt <= attempts; attempt++ {
				if attempts > 1 {
					fmt.Fprintf(r.Stdout, "  │    attempt: %d/%d\n", attempt, attempts)
				}

				execContext, cancel, execErr := r.stepExecContext(baseExecContext, job, step, workingDir)
				if execErr != nil {
					cancel()
					err = execErr
					break
				}

				output, err = r.Executor.RunStep(execContext, job, step)
				cancel()
				r.printStepOutput(output)

				if err == nil {
					break
				}

				if attempt < attempts {
					fmt.Fprintf(r.Stdout, "  │    %s step failed, retrying\n", ui.Yellow(r.Color, "↻"))
				}
			}

			if err != nil {
				jobState.Steps[stepID] = "failed"
				jobState.Status = "failed"
				jobState.LastError = fmt.Sprintf("step %s: %v", stepID, err)
				jobState.FinishedAt = time.Now().UTC().Format(time.RFC3339)
				if persistState {
					if err := r.saveState(statePath, state); err != nil {
						return err
					}
				}

				r.printFailureBlock(err, output, workingDir)
				if strings.EqualFold(step.OnFailure, "continue") {
					fmt.Fprintf(r.Stdout, "  │    %s onFailure=continue, moving to next step\n", ui.Yellow(r.Color, "⚠"))
					continue
				}

				jobFailed = true
				summary.failed++
				if failFast {
					return fmt.Errorf("job %s step %s failed in %s: %w", job.ID, stepID, workingDir, err)
				}
				break
			}

			jobState.Steps[stepID] = "completed"
			if persistState {
				if err := r.saveState(statePath, state); err != nil {
					return err
				}
			}
			fmt.Fprintf(r.Stdout, "  │    %s completed\n", ui.Green(r.Color, "✓"))
		}

		if !r.DryRun {
			if finalizer, ok := r.Executor.(executor.JobFinalizer); ok {
				jobExecContext := baseExecContext
				jobExecContext.WorkDir = jobWorkingDir
				jobExecContext.JobEnv = executor.JobEnvironment(job.Env)
				jobExecContext.StepEnv = nil
				jobExecContext.Env = executor.MergeEnvironment(jobExecContext.BaseEnv, jobExecContext.JobEnv)
				output, finalizeErr := finalizer.FinalizeJob(jobExecContext, job)
				r.printStepOutput(output)
				if finalizeErr != nil {
					jobFailed = true
					jobState.Status = "failed"
					jobState.LastError = fmt.Sprintf("job finalizer: %v", finalizeErr)
					jobState.FinishedAt = time.Now().UTC().Format(time.RFC3339)
					if persistState {
						if err := r.saveState(statePath, state); err != nil {
							return err
						}
					}
					r.printFailureBlock(finalizeErr, output, jobWorkingDir)
					if failFast {
						return fmt.Errorf("job %s finalizer failed in %s: %w", job.ID, jobWorkingDir, finalizeErr)
					}
				}
			}
		}

		if jobState.Status != "failed" {
			jobState.Status = "completed"
			jobState.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			jobState.LastError = ""
			if persistState {
				if err := r.saveState(statePath, state); err != nil {
					return err
				}
			}
			summary.completed++
			fmt.Fprintf(r.Stdout, "  └─ %s Job %s completed\n", ui.Green(r.Color, "✓"), job.ID)
		} else if !jobFailed {
			summary.failed++
		}

		if r.JobID != "" {
			break
		}
	}

	if r.JobID != "" {
		if !executedTarget {
			return fmt.Errorf("job not found in runnable set: %s", r.JobID)
		}
	}

	r.printRunSummary(summary)

	return nil
}

func (r *Runner) printRunHeader(plan *model.Plan, statePath string) {
	fmt.Fprintln(r.Stdout, ui.Cyan(r.Color, "┌──────────────────────────────────────────────────────────┐"))
	fmt.Fprintln(r.Stdout, ui.BoldCyan(r.Color, "│ arx run                                                  │"))
	fmt.Fprintln(r.Stdout, ui.Cyan(r.Color, "├──────────────────────────────────────────────────────────┤"))
	fmt.Fprintf(r.Stdout, "│ plan:  %s (%s)\n", plan.Metadata.Name, plan.Metadata.Checksum)
	if r.JobID != "" {
		fmt.Fprintf(r.Stdout, "│ jobs:  1 targeted (%d total in plan)\n", len(plan.Jobs))
	} else {
		fmt.Fprintf(r.Stdout, "│ jobs:  %d\n", len(plan.Jobs))
	}
	fmt.Fprintf(r.Stdout, "│ state: %s\n", statePath)
	mode := "execute"
	if r.DryRun {
		mode = "dry-run"
	}
	fmt.Fprintf(r.Stdout, "│ mode:  %s\n", mode)
	fmt.Fprintf(r.Stdout, "│ runner: %s\n", r.Executor.Name())
	if r.UseWorkDirOverride {
		fmt.Fprintf(r.Stdout, "│ cwd:   override (%s)\n", r.WorkDir)
	} else {
		fmt.Fprintln(r.Stdout, "│ cwd:   component-path (job.path)")
	}
	if r.JobID != "" {
		fmt.Fprintf(r.Stdout, "│ target: %s\n", r.JobID)
	}
	fmt.Fprintln(r.Stdout, ui.Cyan(r.Color, "└──────────────────────────────────────────────────────────┘"))
}

func (r *Runner) printTargetJobSummary(job model.PlanJob, state *State) {
	status := "pending"
	if state != nil {
		if st, ok := state.Jobs[job.ID]; ok && st != nil && strings.TrimSpace(st.Status) != "" {
			status = st.Status
		}
	}

	fmt.Fprintln(r.Stdout, "\n"+ui.BoldCyan(r.Color, "Target job summary"))
	fmt.Fprintf(r.Stdout, "  ├─ id: %s\n", job.ID)
	fmt.Fprintf(r.Stdout, "  ├─ component: %s\n", job.Component)
	fmt.Fprintf(r.Stdout, "  ├─ environment: %s\n", job.Environment)
	fmt.Fprintf(r.Stdout, "  ├─ steps: %d\n", len(job.Steps))
	fmt.Fprintf(r.Stdout, "  ├─ dependencies: %d\n", len(job.DependsOn))
	fmt.Fprintf(r.Stdout, "  └─ state: %s\n", status)
}

func (r *Runner) printReadinessSnapshot(jobs []model.PlanJob, state *State) {
	fmt.Fprintln(r.Stdout, "\n"+ui.BoldCyan(r.Color, "Readiness snapshot"))
	for _, job := range jobs {
		if r.JobID != "" && job.ID != r.JobID {
			continue
		}

		jobState := state.Jobs[job.ID]
		if jobState != nil && jobState.Status == "completed" {
			fmt.Fprintf(r.Stdout, "  ├─ %s %s (completed from state)\n", ui.Green(r.Color, "✓"), job.ID)
			continue
		}

		unmet := unresolvedDependencies(job, state)
		if len(unmet) > 0 {
			fmt.Fprintf(r.Stdout, "  ├─ %s %s waiting for: %s\n", ui.Yellow(r.Color, "⏳"), job.ID, strings.Join(unmet, ", "))
			continue
		}

		fmt.Fprintf(r.Stdout, "  ├─ %s %s ready\n", ui.Cyan(r.Color, "▶"), job.ID)
	}
}

func (r *Runner) printWaiting(job model.PlanJob, unmet []string, state *State) {
	fmt.Fprintf(r.Stdout, "\n%s Job %s waiting for dependencies\n", ui.Yellow(r.Color, "⏳"), job.ID)
	for _, dep := range unmet {
		status := "pending"
		if depState, ok := state.Jobs[dep]; ok && depState != nil && depState.Status != "" {
			status = depState.Status
		}
		fmt.Fprintf(r.Stdout, "  ├─ %s (%s)\n", dep, status)
	}
}

func (r *Runner) printRunSummary(summary *runSummary) {
	title := "run summary"
	if r.JobID != "" {
		title = "target job summary"
	}

	fmt.Fprintln(r.Stdout, "\n"+ui.Cyan(r.Color, "┌──────────────────────────────────────────────────────────┐"))
	fmt.Fprintln(r.Stdout, ui.BoldCyan(r.Color, fmt.Sprintf("│ %-56s │", title)))
	fmt.Fprintln(r.Stdout, ui.Cyan(r.Color, "├──────────────────────────────────────────────────────────┤"))
	fmt.Fprintf(r.Stdout, "│ completed: %d\n", summary.completed)
	fmt.Fprintf(r.Stdout, "│ skipped:   %d\n", summary.skipped)
	fmt.Fprintf(r.Stdout, "│ waiting:   %d\n", summary.waiting)
	fmt.Fprintf(r.Stdout, "│ failed:    %s\n", ui.Red(r.Color, fmt.Sprintf("%d", summary.failed)))
	fmt.Fprintln(r.Stdout, ui.Cyan(r.Color, "└──────────────────────────────────────────────────────────┘"))
}

func normalizeStepPhase(phase string) string {
	p := strings.TrimSpace(strings.ToLower(phase))
	if p == "" {
		return "main"
	}
	return p
}

func (r *Runner) resolveStateFile(plan *model.Plan) string {
	stateFile := defaultStateFileName
	if plan != nil && strings.TrimSpace(plan.Execution.StateFile) != "" {
		stateFile = strings.TrimSpace(plan.Execution.StateFile)
	}
	if filepath.IsAbs(stateFile) {
		return stateFile
	}

	statePath := filepath.Join(r.WorkDir, stateFile)
	if stateFile != defaultStateFileName {
		return statePath
	}

	if !fileExists(statePath) {
		for _, legacyStateFileName := range legacyStateFileNames {
			legacyPath := filepath.Join(r.WorkDir, legacyStateFileName)
			if fileExists(legacyPath) {
				return legacyPath
			}
		}
	}

	return statePath
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (r *Runner) loadState(statePath string) (*State, error) {
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{Jobs: map[string]*JobState{}}, nil
		}
		return nil, fmt.Errorf("failed to read state file %s: %w", statePath, err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file %s: %w", statePath, err)
	}
	if state.Jobs == nil {
		state.Jobs = map[string]*JobState{}
	}

	return &state, nil
}

func (r *Runner) saveState(statePath string, state *State) error {
	if state == nil {
		return fmt.Errorf("state cannot be nil")
	}

	dir := filepath.Dir(statePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create state directory: %w", err)
		}
	}

	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize state: %w", err)
	}

	tmp := statePath + ".tmp"
	if err := os.WriteFile(tmp, payload, 0644); err != nil {
		return fmt.Errorf("failed to write temp state file: %w", err)
	}
	if err := os.Rename(tmp, statePath); err != nil {
		return fmt.Errorf("failed to atomically replace state file: %w", err)
	}

	return nil
}

func ensureJobState(state *State, job model.PlanJob) *JobState {
	jobState, exists := state.Jobs[job.ID]
	if !exists || jobState == nil {
		jobState = &JobState{
			Status: "pending",
			Steps:  map[string]string{},
		}
		state.Jobs[job.ID] = jobState
	}
	if jobState.Steps == nil {
		jobState.Steps = map[string]string{}
	}

	for _, step := range job.Steps {
		stepID := stepIdentifier(step)
		if _, ok := jobState.Steps[stepID]; !ok {
			jobState.Steps[stepID] = "pending"
		}
	}

	return jobState
}

func stepIdentifier(step model.PlanStep) string {
	if strings.TrimSpace(step.ID) != "" {
		return strings.TrimSpace(step.ID)
	}
	if strings.TrimSpace(step.Name) != "" {
		return strings.TrimSpace(step.Name)
	}
	if strings.TrimSpace(step.Use) != "" {
		return strings.TrimSpace(step.Use)
	}
	return "unnamed-step"
}

func unresolvedDependencies(job model.PlanJob, state *State) []string {
	missing := make([]string, 0)
	for _, dep := range job.DependsOn {
		depState, exists := state.Jobs[dep]
		if !exists || depState == nil || depState.Status != "completed" {
			missing = append(missing, dep)
		}
	}
	return missing
}

func (r *Runner) resolveWorkingDir(path string) string {
	if r.UseWorkDirOverride {
		if filepath.IsAbs(r.WorkDir) {
			return r.WorkDir
		}
		base, err := filepath.Abs(r.WorkDir)
		if err == nil {
			return base
		}
		return r.WorkDir
	}

	if path == "" || path == "./" {
		return r.WorkDir
	}

	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(r.WorkDir, path)
}

func (r *Runner) resolveWorkspaceDir() (string, error) {
	workspaceDir := r.WorkDir
	if strings.TrimSpace(workspaceDir) == "" {
		workspaceDir = "."
	}
	resolved, err := filepath.Abs(workspaceDir)
	if err != nil {
		return "", fmt.Errorf("resolve workspace directory %s: %w", workspaceDir, err)
	}
	return resolved, nil
}

func (r *Runner) resolveRetryCount(job model.PlanJob, step model.PlanStep) int {
	if step.Retry > 0 {
		return step.Retry
	}
	if job.Retries > 0 {
		return job.Retries
	}
	return 0
}

func (r *Runner) resolveTimeout(job model.PlanJob, step model.PlanStep) string {
	if strings.TrimSpace(step.Timeout) != "" {
		return strings.TrimSpace(step.Timeout)
	}
	return strings.TrimSpace(job.Timeout)
}

func (r *Runner) stepExecContext(base executor.ExecContext, job model.PlanJob, step model.PlanStep, workingDir string) (executor.ExecContext, func(), error) {
	stepContext := base.Context
	if stepContext == nil {
		stepContext = context.Background()
	}

	cancel := func() {}
	if timeoutValue := r.resolveTimeout(job, step); timeoutValue != "" {
		duration, err := time.ParseDuration(timeoutValue)
		if err != nil {
			return executor.ExecContext{}, cancel, fmt.Errorf("invalid timeout %q for job %s step %s: %w", timeoutValue, job.ID, stepIdentifier(step), err)
		}
		stepContext, cancel = context.WithTimeout(stepContext, duration)
	}

	execContext := base
	execContext.Context = stepContext
	execContext.JobEnv = executor.JobEnvironment(job.Env)
	execContext.StepEnv = executor.JobEnvironment(step.Env)
	execContext.WorkDir = r.resolveStepWorkingDir(workingDir, step.WorkingDirectory)
	execContext.Env = executor.MergeEnvironment(execContext.BaseEnv, execContext.JobEnv, execContext.StepEnv)
	return execContext, cancel, nil
}

func (r *Runner) resolveStepWorkingDir(baseWorkingDir, stepWorkingDir string) string {
	resolved := strings.TrimSpace(stepWorkingDir)
	if resolved == "" {
		return baseWorkingDir
	}
	if filepath.IsAbs(resolved) {
		return resolved
	}
	return filepath.Join(baseWorkingDir, resolved)
}

func (r *Runner) printStepOutput(output string) {
	if strings.TrimSpace(output) == "" {
		return
	}

	fmt.Fprintln(r.Stdout, "  │    output:")
	for _, line := range strings.Split(output, "\n") {
		fmt.Fprintf(r.Stdout, "  │      %s\n", line)
	}
}

func (r *Runner) printPhaseHeader(phase string) {
	title := "Main commands"
	switch phase {
	case "pre":
		title = "Pre-steps"
	case "post":
		title = "Post-steps"
	}

	fmt.Fprintf(r.Stdout, "\n  ├─ %s\n", ui.Cyan(r.Color, title))
}

func (r *Runner) printJobHeader(job model.PlanJob) {
	fmt.Fprintf(r.Stdout, "\n%s Job %s\n", ui.Cyan(r.Color, "╭─"), ui.Bold(r.Color, job.ID))
	fmt.Fprintf(r.Stdout, "│  component: %s\n", job.Component)
	fmt.Fprintf(r.Stdout, "│  environment: %s\n", job.Environment)
	fmt.Fprintf(r.Stdout, "│  status: %s\n", ui.Green(r.Color, "ready"))
}

func (r *Runner) printFailureBlock(err error, output, workingDir string) {
	fmt.Fprintf(r.Stdout, "  │    %s failed: %s\n", ui.Red(r.Color, "✗"), ui.Red(r.Color, summarizeExecError(err)))
	if hint := stepFailureHint(err, output, workingDir); hint != "" {
		fmt.Fprintf(r.Stdout, "  │    %s %s\n", ui.Yellow(r.Color, "hint:"), hint)
	}
}

func summarizeExecError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "command timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "command canceled"
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		return fmt.Sprintf("command exited with code %d", exitErr.ExitCode())
	}

	return err.Error()
}

func stepFailureHint(err error, output, workingDir string) string {
	if err == nil {
		return ""
	}

	combined := strings.ToLower(err.Error() + "\n" + output)
	if strings.Contains(combined, "no such file or directory") {
		return fmt.Sprintf("file/path not found from cwd %s. Verify component path, relative file paths, or set --workdir to override globally", workingDir)
	}

	if strings.Contains(combined, "permission denied") {
		return "permission denied. Verify executable permissions and access to the target directory"
	}

	if strings.Contains(combined, "command timed out") || strings.Contains(combined, "context deadline exceeded") {
		return "command exceeded its configured timeout. Increase step.timeout or job.timeout if the runtime is expected to take longer"
	}

	if strings.Contains(combined, "unable to find image") || strings.Contains(combined, "pull access denied") {
		return "docker image could not be pulled. Set job.runsOn to a valid container image when using --runner docker"
	}

	if strings.Contains(combined, "executable file not found") || strings.Contains(combined, "command not found") {
		return "required CLI is not available in this runner environment"
	}

	return ""
}

func topologicalOrder(jobs []model.PlanJob) ([]model.PlanJob, error) {
	jobsByID := make(map[string]model.PlanJob, len(jobs))
	inDegree := make(map[string]int, len(jobs))
	dependents := make(map[string][]string, len(jobs))

	for _, job := range jobs {
		jobsByID[job.ID] = job
		inDegree[job.ID] = 0
		dependents[job.ID] = []string{}
	}

	for _, job := range jobs {
		for _, dep := range job.DependsOn {
			if _, exists := jobsByID[dep]; !exists {
				return nil, fmt.Errorf("job %s depends on unknown job %s", job.ID, dep)
			}
			inDegree[job.ID]++
			dependents[dep] = append(dependents[dep], job.ID)
		}
	}

	queue := make([]string, 0)
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)

	ordered := make([]model.PlanJob, 0, len(jobs))
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		ordered = append(ordered, jobsByID[current])

		for _, dep := range dependents[current] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
		sort.Strings(queue)
	}

	if len(ordered) != len(jobs) {
		return nil, fmt.Errorf("cycle detected in plan jobs")
	}

	return ordered, nil
}

func findJobByID(jobs []model.PlanJob, jobID string) (model.PlanJob, error) {
	for _, job := range jobs {
		if job.ID == jobID {
			return job, nil
		}
	}
	return model.PlanJob{}, fmt.Errorf("job not found: %s", jobID)
}
