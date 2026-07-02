package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/redact"
	"github.com/sourceplane/orun/internal/ui"
)

// IsolationMode controls how each job's working tree is materialized.
//   - IsolationAuto: stage per-job when effective concurrency > 1 (default)
//   - IsolationWorkspace: always stage per job
//   - IsolationNone: share the source tree across all jobs (legacy behavior)
type IsolationMode string

const (
	IsolationAuto      IsolationMode = "auto"
	IsolationWorkspace IsolationMode = "workspace"
	IsolationNone      IsolationMode = "none"
)

// RunnerHooks allow external code to observe step and job lifecycle events
// without coupling the runner to a specific backend implementation.
type RunnerHooks struct {
	// AfterStepLog is called after each step completes with the step output.
	// It is called synchronously; implementations should be non-blocking or
	// fast to avoid delaying execution.
	AfterStepLog func(jobID, stepID, output string)
	// AfterJobTerminal is called when a job reaches a terminal state.
	AfterJobTerminal func(jobID string, success bool, errText string)
	// BeforeJob is called before a job starts. If it returns (true, nil) the job
	// is treated as already complete and execution is skipped. An error cancels
	// the run.
	BeforeJob func(jobID string) (skipExec bool, err error)
	// OnJobStart is called once a job begins executing, with the per-job
	// execution context and its cancel func. Remote state uses this to drive a
	// heartbeat keyed to the job's lifetime and to abort the job (cancel) when
	// the server reports the lease was lost — stopping work another runner has
	// taken over. jobCtx is cancelled automatically when the job finishes.
	OnJobStart func(jobID string, jobCtx context.Context, jobCancel context.CancelFunc)
	// AfterStateUpdate is called after the runner updates its in-memory
	// ExecState on a job/step tick. The object-model working tree uses it to
	// project live state into the content-addressed graph (objrun). The hook
	// runs synchronously but OUTSIDE r.stateMu (so it may call back into
	// the runner, e.g. SnapshotState); implementations MUST be fast and
	// non-blocking.
	AfterStateUpdate func()
	// ResolveJobSecrets resolves a job's secret:// references to plaintext env
	// values, called after the job is claimed (BeforeJob) and before its first
	// step. Fail closed: an error fails the job without starting any step. The
	// returned map is injected as the highest-precedence, non-persisted env
	// layer for this job only, and every value is registered with the
	// redactor first (specs/orun-secrets/runner-integration.md §1).
	ResolveJobSecrets func(jobID string, refs []model.PlanSecretRef) (map[string]string, error)
}

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
	ExecID             string
	// PlanID is the plan checksum short-form, injected as ORUN_PLAN_ID into
	// every step environment. Also used to build ORUN_JOB_RUN_ID.
	PlanID           string
	Concurrency      int
	FilterComponents []string
	FilterEnv        string
	Isolation        IsolationMode
	KeepWorkspaces   bool
	// SkipLocalDepsForJob disables the local dependency-completion check when
	// running a single --job in remote mode. The remote backend's claim API
	// already enforces dependency ordering.
	SkipLocalDepsForJob bool
	// Hooks wires external lifecycle callbacks (remote state, log upload, etc.).
	Hooks *RunnerHooks

	// Redactor masks resolved secret values in step output before it reaches
	// ANY sink — view analysis, the AfterStepLog hook (remote log upload +
	// objrun blob write), the GHA emitter, and the console. Nil-safe; seeded
	// with every value ResolveJobSecrets returns.
	Redactor *redact.Redactor

	// ResumeJobs seeds the run with jobs that already succeeded in a prior run
	// of the same execution (read from the object graph). Seeded jobs are
	// skipped — the run re-executes only the jobs that did not succeed, and
	// counts the seeded ones as "cached". nil disables cross-run resume.
	ResumeJobs map[string]*execmodel.JobState

	printMu sync.Mutex
	stateMu sync.Mutex

	// liveState points at the in-memory ExecState the run mutates, so observers
	// (the object-model working tree) can snapshot live job/step progress without
	// reading the legacy on-disk state.json. Set at the start of Run.
	liveState *execmodel.ExecState

	live          *ui.LiveRegion
	gha           *ui.GHARenderer
	groupMu       sync.Mutex
	currentGroup  string
	finishedAny   bool
	groupMultiEnv bool

	componentJobTotal map[string]int
	componentFinished map[string][]finishedJobEntry

	ComponentConcurrency int
}

type finishedJobEntry struct {
	job      model.PlanJob
	report   *jobReport
	success  bool
	duration time.Duration
	resumed  bool
}

// inGHA reports whether output should be rendered for the GitHub Actions log
// viewer (collapsible groups, workflow-command annotations, per-job buffering).
func (r *Runner) inGHA() bool { return r.gha != nil }

// State is kept for backwards compat with tests referencing old types.
type State = execmodel.ExecState
type JobState = execmodel.JobState

type runSummary struct {
	mu        sync.Mutex
	startedAt time.Time
	completed int
	resumed   int
	failed    int
	waiting   int
	cacheHits int
	links     []execmodel.ExecutionLink
	linkIndex map[string]struct{}
}

type runSummarySnapshot struct {
	duration  time.Duration
	completed int
	resumed   int
	failed    int
	waiting   int
	cacheHits int
	links     []execmodel.ExecutionLink
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

func (s *runSummary) addLinks(links []execmodel.ExecutionLink) {
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
		links:     append([]execmodel.ExecutionLink{}, s.links...),
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
	execID string,
	concurrency int,
	filterComponents []string,
	filterEnv string,
) *Runner {
	return &Runner{
		WorkDir:              workDir,
		UseWorkDirOverride:   useWorkDirOverride,
		Stdout:               stdout,
		Stderr:               stderr,
		DryRun:               dryRun,
		JobID:                jobID,
		Retry:                retry,
		Verbose:              verbose,
		Color:                ui.ColorEnabledForWriter(stdout),
		Executor:             exec,
		Runtime:              runtime,
		ExecID:               execID,
		Concurrency:          concurrency,
		FilterComponents:     filterComponents,
		FilterEnv:            filterEnv,
		ComponentConcurrency: 1,
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
				"ORUN_CONTEXT": r.Runtime.Environment,
				"ORUN_RUNNER":  r.Runtime.Runner,
				"ORUN_EXEC_ID": r.ExecID,
				"ORUN_PLAN_ID": r.PlanID,
			},
		),
		Runtime: r.Runtime,
		Stdout:  r.Stdout,
		Stderr:  r.Stderr,
		DryRun:  r.DryRun,
	}
	if githubEnv := baseExecContext.BaseEnv["GITHUB_ENV"]; githubEnv != "" {
		baseExecContext.BaseEnv["ORUN_ENV"] = githubEnv
	}
	baseExecContext.Env = executor.MergeEnvironment(baseExecContext.BaseEnv)

	// The runner keeps execution state in memory; persistence is the object
	// model's job (the working tree, driven via the lifecycle hooks).
	persistState := !r.DryRun
	execState := &execmodel.ExecState{
		ExecID:       r.ExecID,
		PlanChecksum: plan.Metadata.Checksum,
		Jobs:         map[string]*execmodel.JobState{},
	}

	// Cross-run resume: seed jobs that already succeeded in a prior run so the
	// execution loop skips them (the existing "completed" → resumed path). Done
	// before the retry override below so an explicit --job retry still re-runs.
	for jobID, js := range r.ResumeJobs {
		if js != nil {
			execState.Jobs[jobID] = js
		}
	}

	if r.JobID != "" && r.Retry {
		execState.Jobs[r.JobID] = nil
	}

	// Expose the live state to observers (object-model working tree).
	r.stateMu.Lock()
	r.liveState = execState
	r.stateMu.Unlock()

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

	r.groupMultiEnv = singleEnvironment(orderedJobs) == ""
	r.initComponentCounts(orderedJobs)
	r.live = ui.NewLiveRegion(r.Stdout, ui.IsInteractiveWriter(r.Stdout), r.Color)
	if ui.IsGitHubActions() {
		r.gha = ui.NewGHARenderer(r.Stdout)
	}

	if r.shouldPrintPreflight(orderedJobs) {
		r.printRunHeader(plan, orderedJobs)
		if r.Verbose {
			r.printReadinessSnapshot(orderedJobs, execState)
		}
	}
	totalJobs := len(orderedJobs)
	summary := newRunSummary()
	r.live.SetHeaderFunc(func(activeRows int) []string {
		return r.dashboardHeaderLines(totalJobs, activeRows, summary)
	})
	r.live.Start()
	defer r.live.Stop()

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
			if !r.inGHA() && (r.Verbose || r.JobID != "") {
				r.printWaiting(job, unmet, execState)
			}
			if r.JobID != "" && !r.SkipLocalDepsForJob {
				return fmt.Errorf("cannot run %s: dependencies not completed (%s)", job.ID, strings.Join(unmet, ", "))
			}
			if r.JobID == "" {
				continue
			}
		}

		executedTarget = true

		jobState := ensureJobState(execState, job)
		if jobState.Status == "completed" {
			summary.addResumed()
			r.printJobResumed(job)
			continue
		}

		// BeforeJob hook: allow external code (e.g. remote claim) to decide
		// whether to skip this job.
		if r.Hooks != nil && r.Hooks.BeforeJob != nil {
			skip, hookErr := r.Hooks.BeforeJob(job.ID)
			if hookErr != nil {
				return hookErr
			}
			if skip {
				jobState.Status = "completed"
				summary.addResumed()
				r.printJobResumed(job)
				continue
			}
		}

		failed := r.executeJob(job, jobState, execState, baseExecContext, persistState, failFast, summary)
		if failed && failFast {
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

	return nil
}

func (r *Runner) executeJob(job model.PlanJob, jobState *execmodel.JobState, execState *execmodel.ExecState, baseExecContext executor.ExecContext, persistState, failFast bool, summary *runSummary) bool {
	// Per-job cancellable context: steps run under it, and OnJobStart hands the
	// cancel to remote state so a lost lease aborts just this job (not the run).
	// Cancelled on return so the job's heartbeat goroutine stops (no leak).
	jobCtx, jobCancel := context.WithCancel(baseExecContext.Context)
	defer jobCancel()
	baseExecContext.Context = jobCtx
	if r.Hooks != nil && r.Hooks.OnJobStart != nil {
		r.Hooks.OnJobStart(job.ID, jobCtx, jobCancel)
	}

	r.updateState(persistState, execState, func() {
		jobState.Status = "running"
		jobState.FinishedAt = ""
		jobState.LastError = ""
		if jobState.StartedAt == "" {
			jobState.StartedAt = time.Now().UTC().Format(time.RFC3339)
		}
	})

	r.printJobHeader(job, execState)

	jobFailed := false
	jobStartedAt := time.Now()
	jobReport := newJobReport(job, r.DryRun)

	// Resolve secret references before any step runs (fail-closed). Values are
	// registered with the redactor FIRST, live only in this job's memory, and
	// are merged as the top env layer per step — never into job.Env or state.
	var jobSecretEnv map[string]string
	if len(job.SecretRefs) > 0 && !r.DryRun {
		resolved, resolveErr := r.resolveJobSecrets(job)
		if resolveErr != nil {
			r.updateState(persistState, execState, func() {
				jobState.Status = "failed"
				jobState.LastError = fmt.Sprintf("secret resolution: %v", resolveErr)
				jobState.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			})
			fmt.Fprintf(r.Stderr, "  %s %s: secret resolution failed: %v\n", ui.Red(r.Color, "✗"), job.ID, resolveErr)
			summary.addFailed()
			if r.Hooks != nil && r.Hooks.AfterJobTerminal != nil {
				r.Hooks.AfterJobTerminal(job.ID, false, jobState.LastError)
			}
			return true
		}
		jobSecretEnv = resolved
	}

	// Per-job workspace isolation. We stage the source tree into a job-private
	// directory so concurrent jobs don't race on node_modules / .turbo / dist
	// rewrites. UseWorkDirOverride means the user pinned a workdir explicitly —
	// honor that and skip staging.
	stagedRoot := ""
	if r.shouldIsolate() && !r.DryRun && !r.UseWorkDirOverride {
		staged, err := stageJobWorkspace(baseExecContext.WorkspaceDir, r.ExecID, job.ID, r.KeepWorkspaces)
		if err != nil {
			fmt.Fprintf(r.Stderr, "warning: workspace isolation failed for %s, falling back to shared tree: %v\n", job.ID, err)
		} else {
			defer staged.Cleanup()
			stagedRoot = staged.Path()
			baseExecContext.WorkspaceDir = stagedRoot
			baseExecContext.WorkDir = stagedRoot
		}
	}

	jobWorkingDir := r.resolveWorkingDir(job.Path)
	if stagedRoot != "" {
		jobWorkingDir = resolveWorkingDirAt(stagedRoot, job.Path)
	}
	currentPhase := ""
	for idx, step := range job.Steps {
		stepID := stepIdentifier(step)
		stepPhase := normalizeStepPhase(step.Phase)
		if stepPhase != currentPhase {
			if stepPhase != "main" {
				if r.inGHA() {
					r.ghaPrintPhaseHeader(job, stepPhase)
				} else {
					r.printPhaseHeader(stepPhase)
				}
			}
			currentPhase = stepPhase
		}
		if jobState.Steps[stepID] == "completed" {
			if r.inGHA() {
				r.ghaPrintStepSkipped(job, stepID, idx+1, len(job.Steps))
			} else {
				r.printStepSkipped(stepID, idx+1, len(job.Steps))
			}
			jobReport.observeStepDone(stepID, true, true, 0)
			continue
		}

		r.updateState(persistState, execState, func() {
			jobState.Steps[stepID] = "running"
		})

		workingDir := r.resolveStepWorkingDir(jobWorkingDir, step.WorkingDirectory)
		retryCount := r.resolveRetryCount(job, step)
		timeoutValue := r.resolveTimeout(job, step)
		stepStartedAt := time.Now()
		r.updateLiveStep(job, stepID, idx+1, len(job.Steps))
		r.printStepStart(stepID, idx+1, len(job.Steps))
		r.printStepContext(step, workingDir, timeoutValue, retryCount)
		if r.DryRun {
			r.updateState(persistState, execState, func() {
				jobState.Steps[stepID] = "completed"
			})
			r.printStepDryRun()
			if r.inGHA() {
				r.ghaPrintStepDryRun(job, stepID, idx+1)
			}
			continue
		}

		var output string
		var stepErr error
		var ghaOutput strings.Builder // accumulates per-attempt output for GHA
		attempts := retryCount + 1
		for attempt := 1; attempt <= attempts; attempt++ {
			if attempts > 1 && attempt > 1 {
				if r.inGHA() {
					fmt.Fprintf(&ghaOutput, "\n↻ retry %d/%d\n", attempt, attempts)
				} else {
					r.printStepRetry(attempt, attempts)
				}
			}

			execContext, cancel, execErr := r.stepExecContext(baseExecContext, job, step, workingDir, jobSecretEnv)
			if execErr != nil {
				cancel()
				stepErr = execErr
				break
			}

			output, stepErr = r.Executor.RunStep(execContext, job, step)
			cancel()

			// The single redaction site: mask resolved secret values before the
			// output reaches ANY sink (view analysis, AfterStepLog → remote log +
			// objrun blob, GHA emitter, console). Hooks must not redact
			// themselves — remote setup replaces r.Hooks while objrun chains, so
			// hook-level redaction would be ordering-fragile (Invariant 5).
			output = r.Redactor.Filter(output)

			if r.inGHA() && attempts > 1 {
				ghaOutput.WriteString(output)
			}

			if stepErr == nil {
				break
			}

			if attempt < attempts && !r.inGHA() {
				fmt.Fprintf(r.Stdout, "  │ %s retrying after failure\n", ui.Yellow(r.Color, "↻"))
			}
		}
		stepDuration := time.Since(stepStartedAt)
		view := analyzeStepOutput(step, output)
		jobReport.observeStep(job.ID, stepID, view)

		// Write step log
		if r.Hooks != nil && r.Hooks.AfterStepLog != nil && strings.TrimSpace(output) != "" {
			r.Hooks.AfterStepLog(job.ID, stepID, output)
		}

		if r.inGHA() {
			ghaDisplayOutput := output
			if ghaOutput.Len() > 0 {
				ghaDisplayOutput = ghaOutput.String()
			}
			r.ghaEmitStep(job, stepID, idx+1, ghaDisplayOutput, stepErr == nil, stepDuration, stepErr, view.headline)
		}

		jobReport.observeStepDone(stepID, stepErr == nil, false, stepDuration)

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
			if r.Hooks != nil && r.Hooks.AfterJobTerminal != nil {
				r.Hooks.AfterJobTerminal(job.ID, false, jobState.LastError)
			}
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
			jobExecContext.SecretEnv = jobSecretEnv
			jobExecContext.Env = executor.MergeEnvironment(jobExecContext.BaseEnv, jobExecContext.JobEnv, jobSecretEnv)
			output, finalizeErr := finalizer.FinalizeJob(jobExecContext, job)
			output = r.Redactor.Filter(output)
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
				if r.Hooks != nil && r.Hooks.AfterJobTerminal != nil {
					r.Hooks.AfterJobTerminal(job.ID, false, jobState.LastError)
				}
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
		if r.Hooks != nil && r.Hooks.AfterJobTerminal != nil {
			r.Hooks.AfterJobTerminal(job.ID, true, "")
		}
	} else if !jobFailed {
		summary.addFailed()
		r.printJobFooter(job, jobReport, false, time.Since(jobStartedAt))
		if r.Hooks != nil && r.Hooks.AfterJobTerminal != nil {
			r.Hooks.AfterJobTerminal(job.ID, false, jobState.LastError)
		}
	}

	return jobFailed
}

func (r *Runner) runConcurrent(jobs []model.PlanJob, plan *model.Plan, execState *execmodel.ExecState, baseExecContext executor.ExecContext, persistState, failFast bool, summary *runSummary, concurrency int) error {
	if r.JobID != "" {
		var filtered []model.PlanJob
		for _, job := range jobs {
			if job.ID == r.JobID {
				filtered = append(filtered, job)
				break
			}
		}
		jobs = filtered
	}

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

	// Component-aware scheduling: cap how many distinct components are
	// concurrently in-flight. activeComps tracks components with at least one
	// running or scheduled-but-not-finished job. compRemaining counts pending
	// jobs per component so we know when a component is fully drained.
	activeComps := map[string]int{}
	compRemaining := map[string]int{}
	for _, job := range jobs {
		if !completed[job.ID] {
			compRemaining[job.Component]++
		}
	}
	compCap := r.ComponentConcurrency
	if compCap <= 0 {
		compCap = 0 // 0 = unlimited
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
			// In single-job mode with SkipLocalDepsForJob, the remote coordinator
			// has already verified deps via the claim loop — skip the local check.
			allDepsMet := r.JobID != "" && r.SkipLocalDepsForJob
			if !allDepsMet {
				allDepsMet = true
				for _, dep := range job.DependsOn {
					if !completed[dep] {
						allDepsMet = false
						break
					}
				}
			}
			if allDepsMet {
				ready = append(ready, job)
			}
		}

		// Partition ready jobs: those for already-active components first.
		var readyActive, readyNew []model.PlanJob
		for _, job := range ready {
			if _, ok := activeComps[job.Component]; ok {
				readyActive = append(readyActive, job)
			} else {
				readyNew = append(readyNew, job)
			}
		}

		// Pick which jobs to actually launch this iteration.
		var pick []model.PlanJob
		pick = append(pick, readyActive...)
		// Add jobs from new components, respecting compCap. Jobs whose
		// component becomes active in this same batch should also be picked.
		// If no active components have ready work, allow expansion to avoid
		// deadlock.
		for _, job := range readyNew {
			if _, alreadyActive := activeComps[job.Component]; alreadyActive {
				pick = append(pick, job)
				continue
			}
			if compCap > 0 && len(activeComps) >= compCap && len(readyActive) > 0 {
				continue
			}
			if compCap > 0 && len(activeComps) >= compCap && len(readyActive) == 0 && len(pick) > 0 {
				continue
			}
			activeComps[job.Component] = 0
			pick = append(pick, job)
		}
		// Account active counts for picked jobs.
		for _, job := range pick {
			activeComps[job.Component]++
		}

		if len(pick) == 0 {
			mu.Unlock()
			wg.Wait()
			mu.Lock()
			stillPending := len(pending) > 0
			mu.Unlock()
			if stillPending && firstErr == nil {
				summary.addWaiting(len(pending))
			}
			break
		}
		mu.Unlock()

		for _, job := range pick {
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
					completed[j.ID] = true
					summary.addResumed()
					compRemaining[j.Component]--
					activeComps[j.Component]--
					if activeComps[j.Component] <= 0 {
						delete(activeComps, j.Component)
					}
					mu.Unlock()
					r.printJobResumed(j)
					return
				}

				// BeforeJob hook: allow external code to decide whether to skip.
				if r.Hooks != nil && r.Hooks.BeforeJob != nil {
					skip, hookErr := r.Hooks.BeforeJob(j.ID)
					if hookErr != nil {
						if firstErr == nil {
							firstErr = hookErr
						}
						compRemaining[j.Component]--
						activeComps[j.Component]--
						if activeComps[j.Component] <= 0 {
							delete(activeComps, j.Component)
						}
						mu.Unlock()
						return
					}
					if skip {
						completed[j.ID] = true
						summary.addResumed()
						compRemaining[j.Component]--
						activeComps[j.Component]--
						if activeComps[j.Component] <= 0 {
							delete(activeComps, j.Component)
						}
						mu.Unlock()
						r.printJobResumed(j)
						return
					}
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
				compRemaining[j.Component]--
				activeComps[j.Component]--
				if activeComps[j.Component] <= 0 {
					delete(activeComps, j.Component)
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

func (r *Runner) initComponentCounts(jobs []model.PlanJob) {
	r.componentJobTotal = make(map[string]int, len(jobs))
	r.componentFinished = make(map[string][]finishedJobEntry)
	for _, job := range jobs {
		if r.JobID != "" && job.ID != r.JobID {
			continue
		}
		key := strings.TrimSpace(job.Component)
		if key == "" {
			key = strings.TrimSpace(job.ID)
		}
		r.componentJobTotal[key]++
	}
}

func (r *Runner) updateState(persist bool, execState *execmodel.ExecState, update func()) {
	r.stateMu.Lock()
	if update != nil {
		update()
	}
	fireHook := persist && r.ExecID != ""
	r.stateMu.Unlock()

	// AfterStateUpdate runs OUTSIDE r.stateMu: observers (the object-model
	// working tree) call back into the runner — e.g. SnapshotState, which takes
	// r.stateMu — so firing under the lock would self-deadlock. The hook reads
	// already-persisted/snapshot state, so it does not need the lock held.
	if fireHook && r.Hooks != nil && r.Hooks.AfterStateUpdate != nil {
		r.Hooks.AfterStateUpdate()
	}
}

func (r *Runner) persistState(persist bool, execState *execmodel.ExecState) {
	r.updateState(persist, execState, nil)
}

// SnapshotState returns a deep copy of the runner's current in-memory ExecState,
// taken under the state lock. It lets observers (the object-model working tree)
// project live job/step progress without depending on the legacy on-disk
// state.json — the seam that lets the object-model run path stand on its own.
// Returns nil before Run has initialized state.
func (r *Runner) SnapshotState() *execmodel.ExecState {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.liveState == nil {
		return nil
	}
	out := &execmodel.ExecState{
		ExecID:       r.liveState.ExecID,
		PlanChecksum: r.liveState.PlanChecksum,
		Jobs:         make(map[string]*execmodel.JobState, len(r.liveState.Jobs)),
	}
	for id, js := range r.liveState.Jobs {
		if js == nil {
			out.Jobs[id] = nil
			continue
		}
		cp := *js
		if js.Steps != nil {
			cp.Steps = make(map[string]string, len(js.Steps))
			for k, v := range js.Steps {
				cp.Steps[k] = v
			}
		}
		out.Jobs[id] = &cp
	}
	return out
}

func (r *Runner) printRunHeader(plan *model.Plan, jobs []model.PlanJob) {
	planLabel := strings.TrimSpace(plan.Metadata.Name)
	if planLabel == "" {
		planLabel = "plan"
	}
	planID := execmodel.PlanChecksumShort(plan)

	componentSet := map[string]struct{}{}
	for _, j := range jobs {
		c := strings.TrimSpace(j.Component)
		if c != "" {
			componentSet[c] = struct{}{}
		}
	}
	scopeParts := make([]string, 0, 5)
	if len(componentSet) > 0 {
		scopeParts = append(scopeParts, fmt.Sprintf("%d component%s", len(componentSet), pluralSuffix(len(componentSet))))
	}
	scopeParts = append(scopeParts, fmt.Sprintf("%d job%s", len(jobs), pluralSuffix(len(jobs))))
	if r.Concurrency > 1 {
		scopeParts = append(scopeParts, fmt.Sprintf("%d× parallel", r.Concurrency))
	}
	scopeParts = append(scopeParts, displayRunnerName(r.Executor.Name()))
	if env := singleEnvironment(jobs); env != "" {
		scopeParts = append(scopeParts, env)
	}

	r.withPrintLock(func() {
		fmt.Fprintf(r.Stdout, "\n%s %s\n",
			ui.BoldCyan(r.Color, "▲ orun"),
			ui.Bold(r.Color, planLabel),
		)
		subParts := []string{}
		if planID != "" {
			subParts = append(subParts, "Plan: "+planID)
		}
		if r.ExecID != "" && !r.DryRun {
			subParts = append(subParts, "Run: "+r.ExecID)
		}
		if r.JobID != "" {
			subParts = append(subParts, "Target: "+r.JobID)
		}
		if len(subParts) > 0 {
			fmt.Fprintln(r.Stdout, "  "+ui.Dim(r.Color, strings.Join(subParts, "  ·  ")))
		}
		fmt.Fprintln(r.Stdout, "  "+ui.Dim(r.Color, "Scope: "+strings.Join(scopeParts, " · ")))
		fmt.Fprintln(r.Stdout)
	})
}

// dashboardHeaderLines builds the sticky header rendered above the active
// section: a one-glance status legend plus a progress bar.
func (r *Runner) dashboardHeaderLines(totalJobs int, running int, summary *runSummary) []string {
	if summary == nil {
		return nil
	}
	snap := summary.snapshot()
	succeeded := snap.completed + snap.resumed
	failed := snap.failed
	queued := totalJobs - succeeded - failed - running
	if queued < 0 {
		queued = 0
	}

	parts := make([]string, 0, 4)
	parts = append(parts, fmt.Sprintf("%s %d succeeded", ui.Green(r.Color, "✓"), succeeded))
	parts = append(parts, fmt.Sprintf("%s %d running", ui.Cyan(r.Color, "●"), running))
	parts = append(parts, fmt.Sprintf("%s %d queued", ui.Dim(r.Color, "○"), queued))
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%s %d failed", ui.Red(r.Color, "✗"), failed))
	}

	pct := 0
	if totalJobs > 0 {
		pct = (succeeded + failed) * 100 / totalJobs
	}
	bar := ui.RenderProgressBar(pct, 32)

	return []string{
		"  " + ui.Dim(r.Color, "Status:   ") + strings.Join(parts, "  ·  "),
		"  " + ui.Dim(r.Color, "Progress: ") + bar + " " + fmt.Sprintf("%3d%%", pct),
	}
}

func (r *Runner) printTargetJobSummary(job model.PlanJob, execState *execmodel.ExecState) {
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

func (r *Runner) printReadinessSnapshot(jobs []model.PlanJob, execState *execmodel.ExecState) {
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

func (r *Runner) printWaiting(job model.PlanJob, unmet []string, execState *execmodel.ExecState) {
	dependencies := make([]string, 0, len(unmet))
	for _, dep := range unmet {
		status := "pending"
		if depState, ok := execState.Jobs[dep]; ok && depState != nil && depState.Status != "" {
			status = depState.Status
		}
		dependencies = append(dependencies, fmt.Sprintf("%s (%s)", dep, status))
	}
	if r.live != nil {
		r.live.Print(fmt.Sprintf("    %s %s  %s",
			ui.Yellow(r.Color, "⏳"),
			ui.Bold(r.Color, shortJobName(job)),
			ui.Dim(r.Color, "waiting on "+strings.Join(dependencies, ", ")),
		))
		return
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
	return filepath.Join(r.WorkDir, ".orun-state.json")
}

func ensureJobState(execState *execmodel.ExecState, job model.PlanJob) *execmodel.JobState {
	jobState, exists := execState.Jobs[job.ID]
	if !exists || jobState == nil {
		jobState = &execmodel.JobState{
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

func unresolvedDependencies(job model.PlanJob, execState *execmodel.ExecState) []string {
	missing := make([]string, 0)
	for _, dep := range job.DependsOn {
		depState, exists := execState.Jobs[dep]
		if !exists || depState == nil || depState.Status != "completed" {
			missing = append(missing, dep)
		}
	}
	return missing
}

func (r *Runner) shouldIsolate() bool {
	switch r.Isolation {
	case IsolationNone:
		return false
	case IsolationWorkspace:
		return true
	default:
		return r.Concurrency > 1
	}
}

// resolveWorkingDirAt computes a job's working directory anchored at the given
// base (typically a per-job staged workspace root). Mirrors resolveWorkingDir
// semantics but ignores r.WorkDir / r.UseWorkDirOverride — the caller has
// already decided to redirect everything under base.
func resolveWorkingDirAt(base, path string) string {
	if path == "" || path == "./" {
		return base
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(base, path)
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

// resolveJobSecrets resolves the job's secret references through the
// ResolveJobSecrets hook (fail-closed: no hook wired means references cannot
// be satisfied) and registers every value with the redactor BEFORE any step
// that could echo it runs.
func (r *Runner) resolveJobSecrets(job model.PlanJob) (map[string]string, error) {
	if r.Hooks == nil || r.Hooks.ResolveJobSecrets == nil {
		return nil, fmt.Errorf("job declares %d secret reference(s) but no secret resolver is configured — run against an Orun Cloud backend (orun auth login) or provide ORUN_SECRET_<KEY> overrides for a local run", len(job.SecretRefs))
	}
	resolved, err := r.Hooks.ResolveJobSecrets(job.ID, job.SecretRefs)
	if err != nil {
		return nil, err
	}
	for _, ref := range job.SecretRefs {
		if _, ok := resolved[ref.AsEnv]; !ok {
			return nil, fmt.Errorf("secret resolver returned no value for %s", ref.AsEnv)
		}
	}
	if r.Redactor != nil {
		values := make([]string, 0, len(resolved))
		for _, v := range resolved {
			values = append(values, v)
		}
		r.Redactor.Add(values...)
	}
	return resolved, nil
}

func (r *Runner) stepExecContext(base executor.ExecContext, job model.PlanJob, step model.PlanStep, workingDir string, secretEnv map[string]string) (executor.ExecContext, func(), error) {
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
	// Inject job-level runtime IDs so steps can reference the current job.
	jobRuntimeEnv := map[string]string{
		"ORUN_JOB_ID":      job.ID,
		"ORUN_JOB_UID":     job.UID,
		"ORUN_JOB_RUN_ID":  r.ExecID + "/" + job.UID,
		"ORUN_ENVIRONMENT": job.Environment,
		"ORUN_COMPONENT":   job.Component,
	}
	execContext.JobEnv = executor.MergeEnvironment(execContext.JobEnv, jobRuntimeEnv)
	execContext.StepEnv = executor.JobEnvironment(step.Env)
	execContext.SecretEnv = secretEnv
	execContext.WorkDir = r.resolveStepWorkingDir(workingDir, step.WorkingDirectory)
	// Secret env is the highest-precedence layer and lives only in this
	// context (never in job.Env, the plan, or persisted state).
	execContext.Env = executor.MergeEnvironment(execContext.BaseEnv, execContext.JobEnv, execContext.StepEnv, secretEnv)
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

func (r *Runner) printJobHeader(job model.PlanJob, execState *execmodel.ExecState) {
	if r.inGHA() {
		r.ghaPrintJobHeader(job, execState)
		return
	}
	r.emitGroupHeader(job)
	if r.Verbose {
		r.withPrintLock(func() {
			fmt.Fprintf(r.Stdout, "\n%s Job %s\n", ui.Cyan(r.Color, "╭─"), ui.Bold(r.Color, job.ID))
			fmt.Fprintf(r.Stdout, "│ component: %s   env: %s\n", job.Component, job.Environment)
		})
		return
	}
	if r.live != nil {
		group := strings.TrimSpace(job.Component)
		short := shortJobName(job)
		if group == "" {
			group = short
		}
		envLabel := strings.TrimSpace(job.Environment)
		var label string
		if envLabel != "" && r.groupMultiEnv {
			label = fmt.Sprintf("%s  %s  %s",
				ui.Bold(r.Color, envLabel),
				ui.Dim(r.Color, short),
				ui.Dim(r.Color, "starting..."))
		} else {
			label = fmt.Sprintf("%s  %s",
				ui.Bold(r.Color, short),
				ui.Dim(r.Color, "starting..."))
		}
		r.live.SetRowDetail(job.ID, group, label, "")
	}
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
