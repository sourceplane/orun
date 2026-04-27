package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sourceplane/gluon/internal/executor"
	"github.com/sourceplane/gluon/internal/model"
	"github.com/sourceplane/gluon/internal/state"
	"github.com/sourceplane/gluon/internal/ui"
)

type Runner struct {
	WorkDir            string
	UseWorkDirOverride bool
	Stdout             io.Writer
	Stderr             io.Writer
	DryRun             bool
	JobID              string
	Retry              bool
	Verbose            bool
	Color              bool
	Executor           executor.Executor
	Runtime            executor.RuntimeContext
	Store              *state.Store
	ExecID             string
	Concurrency        int
	FilterComponents   []string
	FilterEnv          string
	printMu            sync.Mutex
	stateMu            sync.Mutex
}

// State is kept for backwards compat with tests referencing old types.
type State = state.ExecState
type JobState = state.JobState

type runSummary struct {
	mu        sync.Mutex
	startedAt time.Time
	completed int
	resumed   int
	failed    int
	waiting   int
	cacheHits int
	links     []state.ExecutionLink
	linkIndex map[string]struct{}
}

type runSummarySnapshot struct {
	duration  time.Duration
	completed int
	resumed   int
	failed    int
	waiting   int
	cacheHits int
	links     []state.ExecutionLink
}

func newRunSummary() *runSummary {
	return &runSummary{
		startedAt: time.Now(),
		linkIndex: map[string]struct{}{},
	}
}

func (s *runSummary) addCompleted() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completed++
}

func (s *runSummary) addResumed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resumed++
}

func (s *runSummary) addFailed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed++
}

func (s *runSummary) addWaiting(n int) {
	if n <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.waiting += n
}

func (s *runSummary) addCacheHits(n int) {
	if n <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cacheHits += n
}

func (s *runSummary) addLinks(links []state.ExecutionLink) {
	if len(links) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, link := range links {
		if strings.TrimSpace(link.URL) == "" {
			continue
		}
		key := strings.TrimSpace(link.URL) + "|" + strings.TrimSpace(link.Label)
		if _, exists := s.linkIndex[key]; exists {
			continue
		}
		s.linkIndex[key] = struct{}{}
		s.links = append(s.links, link)
	}
}

func (s *runSummary) snapshot() runSummarySnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return runSummarySnapshot{
		duration:  time.Since(s.startedAt),
		completed: s.completed,
		resumed:   s.resumed,
		failed:    s.failed,
		waiting:   s.waiting,
		cacheHits: s.cacheHits,
		links:     append([]state.ExecutionLink{}, s.links...),
	}
}

func NewRunner(
	workDir string,
	useWorkDirOverride bool,
	stdout, stderr io.Writer,
	dryRun bool,
	jobID string,
	retry bool,
	verbose bool,
	exec executor.Executor,
	runtime executor.RuntimeContext,
	store *state.Store,
	execID string,
	concurrency int,
	filterComponents []string,
	filterEnv string,
) *Runner {
	return &Runner{
		WorkDir:            workDir,
		UseWorkDirOverride: useWorkDirOverride,
		Stdout:             stdout,
		Stderr:             stderr,
		DryRun:             dryRun,
		JobID:              jobID,
		Retry:              retry,
		Verbose:            verbose,
		Color:              ui.ColorEnabledForWriter(stdout),
		Executor:           exec,
		Runtime:            runtime,
		Store:              store,
		ExecID:             execID,
		Concurrency:        concurrency,
		FilterComponents:   filterComponents,
		FilterEnv:          filterEnv,
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
				"GLUON_CONTEXT": r.Runtime.Environment,
				"GLUON_RUNNER":  r.Runtime.Runner,
				"GLUON_EXEC_ID": r.ExecID,
			},
		),
		Runtime: r.Runtime,
		Stdout:  r.Stdout,
		Stderr:  r.Stderr,
		DryRun:  r.DryRun,
	}
	baseExecContext.Env = executor.MergeEnvironment(baseExecContext.BaseEnv)

	// Create execution and load/create state
	persistState := !r.DryRun
	var execState *state.ExecState

	if r.Store != nil && r.ExecID != "" && persistState {
		if _, err := r.Store.CreateExecution(r.ExecID, plan); err != nil {
			return err
		}
		execState, err = r.Store.LoadState(r.ExecID)
		if err != nil {
			return err
		}
		if execState.PlanChecksum == "" {
			execState.PlanChecksum = plan.Metadata.Checksum
		}

		// Write initial metadata
		r.writeMetadata(plan, execState, "running", nil)
	} else {
		execState = &state.ExecState{
			ExecID:       r.ExecID,
			PlanChecksum: plan.Metadata.Checksum,
			Jobs:         map[string]*state.JobState{},
		}
	}

	if r.JobID != "" && r.Retry {
		execState.Jobs[r.JobID] = nil
	}

	orderedJobs, err := topologicalOrder(plan.Jobs)
	if err != nil {
		return err
	}

	// Apply component/env filters
	orderedJobs = r.filterJobs(orderedJobs)

	if r.JobID != "" {
		if _, err := findJobByID(orderedJobs, r.JobID); err != nil {
			return err
		}
	}

	if r.shouldPrintPreflight(orderedJobs) {
		r.printRunHeader(plan, orderedJobs)
		r.printReadinessSnapshot(orderedJobs, execState)
	}

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
	summary := newRunSummary()
	executedTarget := false

	// Determine concurrency
	concurrency := r.Concurrency
	if concurrency <= 0 {
		concurrency = plan.Execution.Concurrency
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	r.Concurrency = concurrency

	if concurrency > 1 && !r.DryRun {
		return r.runConcurrent(orderedJobs, plan, execState, baseExecContext, persistState, failFast, summary, concurrency)
	}

	for _, job := range orderedJobs {
		if r.JobID != "" && job.ID != r.JobID {
			continue
		}

		unmet := unresolvedDependencies(job, execState)
		if len(unmet) > 0 {
			summary.addWaiting(1)
			r.printWaiting(job, unmet, execState)
			if r.JobID != "" {
				return fmt.Errorf("cannot run %s: dependencies not completed (%s)", job.ID, strings.Join(unmet, ", "))
			}
			continue
		}

		executedTarget = true

		jobState := ensureJobState(execState, job)
		if jobState.Status == "completed" {
			summary.addResumed()
			r.printJobResumed(job)
			continue
		}

		failed := r.executeJob(job, jobState, execState, baseExecContext, persistState, failFast, summary)
		if failed && failFast {
			r.writeMetadata(plan, execState, "failed", summary.snapshot().links)
			return fmt.Errorf("job %s failed (fail-fast enabled)", job.ID)
		}

		if r.JobID != "" {
			break
		}
	}

	if r.JobID != "" && !executedTarget {
		return fmt.Errorf("job not found in runnable set: %s", r.JobID)
	}

	snap := summary.snapshot()
	finalStatus := "completed"
	if snap.failed > 0 {
		finalStatus = "failed"
	}
	r.printRunSummary(summary, finalStatus)
	r.writeMetadata(plan, execState, finalStatus, snap.links)

	return nil
}

func (r *Runner) executeJob(job model.PlanJob, jobState *state.JobState, execState *state.ExecState, baseExecContext executor.ExecContext, persistState, failFast bool, summary *runSummary) bool {
	r.updateState(persistState, execState, func() {
		jobState.Status = "running"
		jobState.FinishedAt = ""
		jobState.LastError = ""
		if jobState.StartedAt == "" {
			jobState.StartedAt = time.Now().UTC().Format(time.RFC3339)
		}
	})

	r.printJobHeader(job)

	jobFailed := false
	jobStartedAt := time.Now()
	jobReport := newJobReport(job, r.DryRun)
	jobWorkingDir := r.resolveWorkingDir(job.Path)
	currentPhase := ""
	for idx, step := range job.Steps {
		stepID := stepIdentifier(step)
		stepPhase := normalizeStepPhase(step.Phase)
		if stepPhase != currentPhase {
			if stepPhase != "main" {
				r.printPhaseHeader(stepPhase)
			}
			currentPhase = stepPhase
		}
		if jobState.Steps[stepID] == "completed" {
			r.printStepSkipped(stepID, idx+1, len(job.Steps))
			continue
		}

		r.updateState(persistState, execState, func() {
			jobState.Steps[stepID] = "running"
		})

		workingDir := r.resolveStepWorkingDir(jobWorkingDir, step.WorkingDirectory)
		retryCount := r.resolveRetryCount(job, step)
		timeoutValue := r.resolveTimeout(job, step)
		stepStartedAt := time.Now()
		r.printStepStart(stepID, idx+1, len(job.Steps))
		r.printStepContext(step, workingDir, timeoutValue, retryCount)
		if r.DryRun {
			r.updateState(persistState, execState, func() {
				jobState.Steps[stepID] = "completed"
			})
			r.printStepDryRun()
			continue
		}

		var output string
		var stepErr error
		attempts := retryCount + 1
		for attempt := 1; attempt <= attempts; attempt++ {
			if attempts > 1 && attempt > 1 {
				r.printStepRetry(attempt, attempts)
			}

			execContext, cancel, execErr := r.stepExecContext(baseExecContext, job, step, workingDir)
			if execErr != nil {
				cancel()
				stepErr = execErr
				break
			}

			output, stepErr = r.Executor.RunStep(execContext, job, step)
			cancel()

			if stepErr == nil {
				break
			}

			if attempt < attempts {
				fmt.Fprintf(r.Stdout, "  │ %s retrying after failure\n", ui.Yellow(r.Color, "↻"))
			}
		}
		stepDuration := time.Since(stepStartedAt)
		view := analyzeStepOutput(step, output)
		jobReport.observeStep(job.ID, stepID, view)

		// Write step log
		r.writeStepLog(job.ID, stepID, output)

		if stepErr != nil {
			r.updateState(persistState, execState, func() {
				jobState.Steps[stepID] = "failed"
				jobState.Status = "failed"
				jobState.LastError = fmt.Sprintf("step %s: %v", stepID, stepErr)
				jobState.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			})

			r.printStepFailure(job, step, view, stepDuration, stepErr, workingDir)
			if strings.EqualFold(step.OnFailure, "continue") {
				r.printStepContinuation()
				continue
			}

			jobFailed = true
			summary.addFailed()
			break
		}

		r.updateState(persistState, execState, func() {
			jobState.Steps[stepID] = "completed"
		})
		r.printStepSuccess(step, view, stepDuration)
	}

	if !r.DryRun {
		if finalizer, ok := r.Executor.(executor.JobFinalizer); ok {
			jobExecContext := baseExecContext
			jobExecContext.WorkDir = jobWorkingDir
			jobExecContext.JobEnv = executor.JobEnvironment(job.Env)
			jobExecContext.StepEnv = nil
			jobExecContext.Env = executor.MergeEnvironment(jobExecContext.BaseEnv, jobExecContext.JobEnv)
			output, finalizeErr := finalizer.FinalizeJob(jobExecContext, job)
			if strings.TrimSpace(output) != "" {
				if r.Verbose || finalizeErr != nil {
					r.printBlock("post-job logs", splitDisplayLines(output))
				} else {
					r.printInlineDetail("post-job logs", "(collapsed; use --verbose to expand)")
				}
			}
			if finalizeErr != nil {
				jobFailed = true
				r.updateState(persistState, execState, func() {
					jobState.Status = "failed"
					jobState.LastError = fmt.Sprintf("job finalizer: %v", finalizeErr)
					jobState.FinishedAt = time.Now().UTC().Format(time.RFC3339)
				})
				r.printFailureBlock(finalizeErr, output, jobWorkingDir)
			}
		}
	}

	summary.addCacheHits(jobReport.cacheHits)
	summary.addLinks(jobReport.links)

	if jobState.Status != "failed" {
		r.updateState(persistState, execState, func() {
			jobState.Status = "completed"
			jobState.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			jobState.LastError = ""
		})
		summary.addCompleted()
		r.printJobFooter(job, jobReport, true, time.Since(jobStartedAt))
	} else if !jobFailed {
		summary.addFailed()
		r.printJobFooter(job, jobReport, false, time.Since(jobStartedAt))
	}

	return jobFailed
}

func (r *Runner) runConcurrent(jobs []model.PlanJob, plan *model.Plan, execState *state.ExecState, baseExecContext executor.ExecContext, persistState, failFast bool, summary *runSummary, concurrency int) error {
	sem := make(chan struct{}, concurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error

	completed := make(map[string]bool)
	for jobID, js := range execState.Jobs {
		if js != nil && js.Status == "completed" {
			completed[jobID] = true
		}
	}

	jobMap := make(map[string]model.PlanJob)
	for _, job := range jobs {
		jobMap[job.ID] = job
	}

	pending := make(map[string]bool)
	for _, job := range jobs {
		if !completed[job.ID] {
			pending[job.ID] = true
		}
	}

	for len(pending) > 0 {
		mu.Lock()
		if failFast && firstErr != nil {
			mu.Unlock()
			break
		}

		var ready []model.PlanJob
		for id := range pending {
			job := jobMap[id]
			allDepsMet := true
			for _, dep := range job.DependsOn {
				if !completed[dep] {
					allDepsMet = false
					break
				}
			}
			if allDepsMet {
				ready = append(ready, job)
			}
		}
		mu.Unlock()

		if len(ready) == 0 {
			wg.Wait()
			mu.Lock()
			stillPending := len(pending) > 0
			mu.Unlock()
			if stillPending && firstErr == nil {
				summary.addWaiting(len(pending))
				break
			}
			break
		}

		for _, job := range ready {
			mu.Lock()
			delete(pending, job.ID)
			mu.Unlock()

			wg.Add(1)
			sem <- struct{}{}

			go func(j model.PlanJob) {
				defer func() {
					<-sem
					wg.Done()
				}()

				mu.Lock()
				if failFast && firstErr != nil {
					mu.Unlock()
					return
				}
				r.stateMu.Lock()
				jobState := ensureJobState(execState, j)
				alreadyCompleted := jobState.Status == "completed"
				r.stateMu.Unlock()
				if alreadyCompleted {
					mu.Unlock()
					mu.Lock()
					completed[j.ID] = true
					summary.addResumed()
					r.printJobResumed(j)
					mu.Unlock()
					return
				}
				mu.Unlock()

				failed := r.executeJob(j, jobState, execState, baseExecContext, persistState, failFast, summary)

				mu.Lock()
				if failed {
					if firstErr == nil {
						firstErr = fmt.Errorf("job %s failed", j.ID)
					}
				} else {
					completed[j.ID] = true
				}
				mu.Unlock()
			}(job)
		}

		wg.Wait()
	}

	wg.Wait()

	snap := summary.snapshot()
	finalStatus := "completed"
	if snap.failed > 0 || firstErr != nil {
		finalStatus = "failed"
	}
	r.printRunSummary(summary, finalStatus)
	r.writeMetadata(plan, execState, finalStatus, snap.links)

	return firstErr
}

func (r *Runner) filterJobs(jobs []model.PlanJob) []model.PlanJob {
	if len(r.FilterComponents) == 0 && r.FilterEnv == "" {
		return jobs
	}

	var filtered []model.PlanJob
	for _, job := range jobs {
		if r.FilterEnv != "" && job.Environment != r.FilterEnv {
			continue
		}
		if len(r.FilterComponents) > 0 {
			match := false
			for _, c := range r.FilterComponents {
				if job.Component == c {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		filtered = append(filtered, job)
	}
	return filtered
}

func (r *Runner) updateState(persist bool, execState *state.ExecState, update func()) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if update != nil {
		update()
	}
	if !persist || r.Store == nil || r.ExecID == "" {
		return
	}
	r.Store.SaveState(r.ExecID, execState)
}

func (r *Runner) persistState(persist bool, execState *state.ExecState) {
	r.updateState(persist, execState, nil)
}

func (r *Runner) writeMetadata(plan *model.Plan, execState *state.ExecState, status string, links []state.ExecutionLink) {
	if r.Store == nil || r.ExecID == "" || r.DryRun {
		return
	}

	u, _ := user.Current()
	username := ""
	if u != nil {
		username = u.Username
	}
	counts := state.SummarizeExecutionState(execState)
	jobTotal := len(plan.Jobs)
	if counts.Total > 0 {
		jobTotal = counts.Total
	}

	meta := &state.ExecMetadata{
		ExecID:    r.ExecID,
		PlanID:    state.PlanChecksumShort(plan),
		PlanName:  plan.Metadata.Name,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Status:    status,
		Trigger:   "cli",
		User:      username,
		DryRun:    r.DryRun,
		JobTotal:  jobTotal,
		JobDone:   counts.Completed,
		JobFailed: counts.Failed,
		Links:     append([]state.ExecutionLink{}, links...),
	}

	existing, _ := r.Store.LoadMetadata(r.ExecID)
	if existing != nil {
		meta.StartedAt = existing.StartedAt
		if len(meta.Links) == 0 {
			meta.Links = append([]state.ExecutionLink{}, existing.Links...)
		}
		if status != "running" {
			meta.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		}
	}

	r.Store.SaveMetadata(r.ExecID, meta)
}

func (r *Runner) writeStepLog(jobID, stepID, output string) {
	if r.Store == nil || r.ExecID == "" || r.DryRun || strings.TrimSpace(output) == "" {
		return
	}
	logPath := r.Store.LogPath(r.ExecID, jobID, stepID)
	os.MkdirAll(filepath.Dir(logPath), 0755)
	os.WriteFile(logPath, []byte(output), 0644)
}

func (r *Runner) printRunHeader(plan *model.Plan, jobs []model.PlanJob) {
	planLabel := strings.TrimSpace(plan.Metadata.Name)
	if planLabel == "" {
		planLabel = "plan"
	}
	planID := state.PlanChecksumShort(plan)
	scopeParts := make([]string, 0, 4)
	if env := singleEnvironment(jobs); env != "" {
		scopeParts = append(scopeParts, env)
	}
	scopeParts = append(scopeParts, fmt.Sprintf("%d task%s", len(jobs), pluralSuffix(len(jobs))))
	if r.Concurrency > 1 {
		scopeParts = append(scopeParts, fmt.Sprintf("%d-way parallel", r.Concurrency))
	}
	scopeParts = append(scopeParts, displayRunnerName(r.Executor.Name()))

	r.withPrintLock(func() {
		fmt.Fprintf(r.Stdout, "%s  %s\n", ui.BoldCyan(r.Color, "gluon run"), ui.Bold(r.Color, planLabel))
		fmt.Fprintln(r.Stdout, ui.Dim(r.Color, strings.Join(scopeParts, " · ")))
		fmt.Fprintln(r.Stdout)
		if r.ExecID != "" && !r.DryRun {
			fmt.Fprintf(r.Stdout, "%-12s %s\n", ui.Dim(r.Color, "Run"), r.ExecID)
		}
		planValue := planLabel
		if planID != "" {
			planValue = planValue + " · " + planID
		}
		fmt.Fprintf(r.Stdout, "%-12s %s\n", ui.Dim(r.Color, "Plan"), planValue)
		if r.JobID != "" {
			fmt.Fprintf(r.Stdout, "%-12s %s\n", ui.Dim(r.Color, "Target"), r.JobID)
		}
		fmt.Fprintln(r.Stdout)
	})
}

func (r *Runner) printTargetJobSummary(job model.PlanJob, execState *state.ExecState) {
	status := "pending"
	if execState != nil {
		if st, ok := execState.Jobs[job.ID]; ok && st != nil && strings.TrimSpace(st.Status) != "" {
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

func (r *Runner) printReadinessSnapshot(jobs []model.PlanJob, execState *state.ExecState) {
	ready := 0
	waiting := 0
	resumed := 0
	waitingLines := make([]string, 0)
	resumedLines := make([]string, 0)
	for _, job := range jobs {
		if r.JobID != "" && job.ID != r.JobID {
			continue
		}

		jobState := execState.Jobs[job.ID]
		if jobState != nil && jobState.Status == "completed" {
			resumed++
			resumedLines = append(resumedLines, fmt.Sprintf("%s reused previous run", jobDisplayName(job)))
			continue
		}

		unmet := unresolvedDependencies(job, execState)
		if len(unmet) > 0 {
			waiting++
			waitingLines = append(waitingLines, fmt.Sprintf("%s waiting on %s", jobDisplayName(job), strings.Join(unmet, ", ")))
			continue
		}

		ready++
	}
	r.withPrintLock(func() {
		fmt.Fprintf(r.Stdout, "%-12s %d ready", ui.Dim(r.Color, "State"), ready)
		if resumed > 0 {
			fmt.Fprintf(r.Stdout, " · %d resumed", resumed)
		}
		if waiting > 0 {
			fmt.Fprintf(r.Stdout, " · %d waiting", waiting)
		}
		fmt.Fprintln(r.Stdout)
		for _, line := range waitingLines {
			fmt.Fprintf(r.Stdout, "  %s %s\n", ui.Yellow(r.Color, "⏳"), line)
		}
		for _, line := range resumedLines {
			fmt.Fprintf(r.Stdout, "  %s %s\n", ui.Cyan(r.Color, "⚡"), line)
		}
		fmt.Fprintln(r.Stdout)
	})
}

func (r *Runner) printWaiting(job model.PlanJob, unmet []string, execState *state.ExecState) {
	dependencies := make([]string, 0, len(unmet))
	for _, dep := range unmet {
		status := "pending"
		if depState, ok := execState.Jobs[dep]; ok && depState != nil && depState.Status != "" {
			status = depState.Status
		}
		dependencies = append(dependencies, fmt.Sprintf("%s (%s)", dep, status))
	}
	r.withPrintLock(func() {
		fmt.Fprintf(r.Stdout, "%s %-22s waiting on %s\n", ui.Yellow(r.Color, "⏳"), ui.Bold(r.Color, jobDisplayName(job)), strings.Join(dependencies, ", "))
	})
}

func singleEnvironment(jobs []model.PlanJob) string {
	if len(jobs) == 0 {
		return ""
	}
	env := strings.TrimSpace(jobs[0].Environment)
	if env == "" {
		return ""
	}
	for _, job := range jobs[1:] {
		if strings.TrimSpace(job.Environment) != env {
			return ""
		}
	}
	return env
}

func normalizeStepPhase(phase string) string {
	p := strings.TrimSpace(strings.ToLower(phase))
	if p == "" {
		return "main"
	}
	return p
}

func (r *Runner) resolveStateFile(plan *model.Plan) string {
	if r.Store != nil && r.ExecID != "" {
		return r.Store.StatePath(r.ExecID)
	}
	return filepath.Join(r.WorkDir, ".gluon-state.json")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (r *Runner) loadState(statePath string) (*state.ExecState, error) {
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &state.ExecState{Jobs: map[string]*state.JobState{}}, nil
		}
		return nil, fmt.Errorf("failed to read state file %s: %w", statePath, err)
	}

	var st state.ExecState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("failed to parse state file %s: %w", statePath, err)
	}
	if st.Jobs == nil {
		st.Jobs = map[string]*state.JobState{}
	}

	return &st, nil
}

func (r *Runner) saveState(statePath string, st *state.ExecState) error {
	if st == nil {
		return fmt.Errorf("state cannot be nil")
	}

	dir := filepath.Dir(statePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create state directory: %w", err)
		}
	}

	payload, err := json.MarshalIndent(st, "", "  ")
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

func ensureJobState(execState *state.ExecState, job model.PlanJob) *state.JobState {
	jobState, exists := execState.Jobs[job.ID]
	if !exists || jobState == nil {
		jobState = &state.JobState{
			Status: "pending",
			Steps:  map[string]string{},
		}
		execState.Jobs[job.ID] = jobState
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

func unresolvedDependencies(job model.PlanJob, execState *state.ExecState) []string {
	missing := make([]string, 0)
	for _, dep := range job.DependsOn {
		depState, exists := execState.Jobs[dep]
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

func (r *Runner) printPhaseHeader(phase string) {
	title := "Phase"
	switch phase {
	case "pre":
		title = "Pre-steps"
	case "post":
		title = "Post-steps"
	}

	fmt.Fprintf(r.Stdout, "\n  %s %s\n", ui.Cyan(r.Color, "◦"), ui.Cyan(r.Color, title))
}

func (r *Runner) printJobHeader(job model.PlanJob) {
	if r.Verbose {
		r.withPrintLock(func() {
			fmt.Fprintf(r.Stdout, "\n%s Job %s\n", ui.Cyan(r.Color, "╭─"), ui.Bold(r.Color, job.ID))
			fmt.Fprintf(r.Stdout, "│ component: %s   env: %s\n", job.Component, job.Environment)
		})
		return
	}
	r.withPrintLock(func() {
		fmt.Fprintf(r.Stdout, "%s %s\n", ui.Blue(r.Color, "●"), ui.Bold(r.Color, jobDisplayName(job)))
	})
}

func (r *Runner) printFailureBlock(err error, output, workingDir string) {
	r.withPrintLock(func() {
		fmt.Fprintf(r.Stdout, "  │    %s failed: %s\n", ui.Red(r.Color, "✗"), ui.Red(r.Color, summarizeExecError(err)))
		if hint := stepFailureHint(err, workingDir); hint != "" {
			fmt.Fprintf(r.Stdout, "  │    %s %s\n", ui.Yellow(r.Color, "hint:"), hint)
		}
	})
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

func stepFailureHint(err error, workingDir string) string {
	if err == nil {
		return ""
	}

	combined := strings.ToLower(err.Error())
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
