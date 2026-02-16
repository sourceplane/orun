package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/liteci/internal/model"
)

// Runner executes a compiled plan in dependency order.
type Runner struct {
	WorkDir            string
	UseWorkDirOverride bool
	Stdout             io.Writer
	Stderr             io.Writer
	DryRun             bool
	JobID              string
	Retry              bool
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

func NewRunner(workDir string, useWorkDirOverride bool, stdout, stderr io.Writer, dryRun bool, jobID string, retry bool) *Runner {
	return &Runner{
		WorkDir:            workDir,
		UseWorkDirOverride: useWorkDirOverride,
		Stdout:             stdout,
		Stderr:             stderr,
		DryRun:             dryRun,
		JobID:              jobID,
		Retry:              retry,
	}
}

func (r *Runner) Run(plan *model.Plan) error {
	if plan == nil {
		return fmt.Errorf("plan cannot be nil")
	}
	if len(plan.Jobs) == 0 {
		return fmt.Errorf("plan has no jobs")
	}

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

	r.printRunHeader(plan, statePath)

	orderedJobs, err := topologicalOrder(plan.Jobs)
	if err != nil {
		return err
	}

	r.printReadinessSnapshot(orderedJobs, state)

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
			fmt.Fprintf(r.Stdout, "↷ Skip job %s (already completed)\n", job.ID)
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
			workingDir := r.resolveWorkingDir(job.Path)
			fmt.Fprintf(r.Stdout, "  │    cwd: %s\n", workingDir)
			fmt.Fprintf(r.Stdout, "  │    run: %s\n", step.Run)
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

			output, err := r.executeStep(step.Run, workingDir)
			r.printStepOutput(output)

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
					fmt.Fprintf(r.Stdout, "  │    ⚠ onFailure=continue, moving to next step\n")
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
			fmt.Fprintf(r.Stdout, "  │    ✓ completed\n")
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
			fmt.Fprintf(r.Stdout, "  └─ ✓ Job %s completed\n", job.ID)
		} else if !jobFailed {
			summary.failed++
		}

		if r.JobID != "" {
			break
		}
	}

	if r.JobID != "" {
		if _, exists := state.Jobs[r.JobID]; !exists {
			return fmt.Errorf("job not found: %s", r.JobID)
		}
		if !executedTarget {
			return fmt.Errorf("job not found in runnable set: %s", r.JobID)
		}
	}

	r.printRunSummary(summary)

	return nil
}

func (r *Runner) printRunHeader(plan *model.Plan, statePath string) {
	fmt.Fprintln(r.Stdout, "┌──────────────────────────────────────────────────────────┐")
	fmt.Fprintln(r.Stdout, "│ liteci run                                               │")
	fmt.Fprintln(r.Stdout, "├──────────────────────────────────────────────────────────┤")
	fmt.Fprintf(r.Stdout, "│ plan:  %s (%s)\n", plan.Metadata.Name, plan.Metadata.Checksum)
	fmt.Fprintf(r.Stdout, "│ jobs:  %d\n", len(plan.Jobs))
	fmt.Fprintf(r.Stdout, "│ state: %s\n", statePath)
	mode := "execute"
	if r.DryRun {
		mode = "dry-run"
	}
	fmt.Fprintf(r.Stdout, "│ mode:  %s\n", mode)
	if r.UseWorkDirOverride {
		fmt.Fprintf(r.Stdout, "│ cwd:   override (%s)\n", r.WorkDir)
	} else {
		fmt.Fprintln(r.Stdout, "│ cwd:   component-path (job.path)")
	}
	if r.JobID != "" {
		fmt.Fprintf(r.Stdout, "│ target: %s\n", r.JobID)
	}
	fmt.Fprintln(r.Stdout, "└──────────────────────────────────────────────────────────┘")
}

func (r *Runner) printReadinessSnapshot(jobs []model.PlanJob, state *State) {
	fmt.Fprintln(r.Stdout, "\nReadiness snapshot")
	for _, job := range jobs {
		if r.JobID != "" && job.ID != r.JobID {
			continue
		}

		jobState := state.Jobs[job.ID]
		if jobState != nil && jobState.Status == "completed" {
			fmt.Fprintf(r.Stdout, "  ├─ ✓ %s (completed from state)\n", job.ID)
			continue
		}

		unmet := unresolvedDependencies(job, state)
		if len(unmet) > 0 {
			fmt.Fprintf(r.Stdout, "  ├─ ⏳ %s waiting for: %s\n", job.ID, strings.Join(unmet, ", "))
			continue
		}

		fmt.Fprintf(r.Stdout, "  ├─ ▶ %s ready\n", job.ID)
	}
}

func (r *Runner) printWaiting(job model.PlanJob, unmet []string, state *State) {
	fmt.Fprintf(r.Stdout, "\n⏳ Job %s waiting for dependencies\n", job.ID)
	for _, dep := range unmet {
		status := "pending"
		if depState, ok := state.Jobs[dep]; ok && depState != nil && depState.Status != "" {
			status = depState.Status
		}
		fmt.Fprintf(r.Stdout, "  ├─ %s (%s)\n", dep, status)
	}
}

func (r *Runner) printRunSummary(summary *runSummary) {
	fmt.Fprintln(r.Stdout, "\n┌──────────────────────────────────────────────────────────┐")
	fmt.Fprintln(r.Stdout, "│ run summary                                              │")
	fmt.Fprintln(r.Stdout, "├──────────────────────────────────────────────────────────┤")
	fmt.Fprintf(r.Stdout, "│ completed: %d\n", summary.completed)
	fmt.Fprintf(r.Stdout, "│ skipped:   %d\n", summary.skipped)
	fmt.Fprintf(r.Stdout, "│ waiting:   %d\n", summary.waiting)
	fmt.Fprintf(r.Stdout, "│ failed:    %d\n", summary.failed)
	fmt.Fprintln(r.Stdout, "└──────────────────────────────────────────────────────────┘")
}

func normalizeStepPhase(phase string) string {
	p := strings.TrimSpace(strings.ToLower(phase))
	if p == "" {
		return "main"
	}
	return p
}

func (r *Runner) resolveStateFile(plan *model.Plan) string {
	stateFile := ".liteci-state.json"
	if plan != nil && strings.TrimSpace(plan.Execution.StateFile) != "" {
		stateFile = strings.TrimSpace(plan.Execution.StateFile)
	}
	if filepath.IsAbs(stateFile) {
		return stateFile
	}
	return filepath.Join(r.WorkDir, stateFile)
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

func (r *Runner) executeStep(command, workingDir string) (string, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = workingDir

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	return strings.TrimRight(buf.String(), "\n"), err
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

	fmt.Fprintf(r.Stdout, "\n  ├─ %s\n", title)
}

func (r *Runner) printJobHeader(job model.PlanJob) {
	fmt.Fprintf(r.Stdout, "\n╭─ Job %s\n", job.ID)
	fmt.Fprintf(r.Stdout, "│  component: %s\n", job.Component)
	fmt.Fprintf(r.Stdout, "│  environment: %s\n", job.Environment)
	fmt.Fprintln(r.Stdout, "│  status: ready")
}

func (r *Runner) printFailureBlock(err error, output, workingDir string) {
	fmt.Fprintf(r.Stdout, "  │    ✗ failed: %s\n", summarizeExecError(err))
	if hint := stepFailureHint(err, output, workingDir); hint != "" {
		fmt.Fprintf(r.Stdout, "  │    hint: %s\n", hint)
	}
}

func summarizeExecError(err error) string {
	if err == nil {
		return ""
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
