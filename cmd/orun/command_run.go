package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/runner"
	"github.com/sourceplane/orun/internal/state"
	"github.com/sourceplane/orun/internal/statebackend"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	runPlanRef              string
	runDryRun               bool
	runVerbose              bool
	runWorkDir              string
	runUseWorkDirOverride   bool
	runJobID                string
	runRetry                bool
	runRunner               string
	runGHACompat            bool
	runExecID               string
	runConcurrency          int
	runComponentConcurrency int
	runComponent            []string
	runEnv                  string
	runJSON                 bool
	runIsolation            string
	runKeepWorkspaces       bool
	runBackground           bool
	// Remote state flags
	runRemoteState bool
	runBackendURL  string
)

var runCmd = &cobra.Command{
	Use:           "run [component|planhash]",
	Short:         "Run a plan",
	Long:          "Run the jobs in a plan with concise live progress.\n\nPass a component name to scope to that component — a fresh plan is generated and run immediately.\nPass a plan hash (or name) to run a specific saved plan.\nOmit the argument to generate and run a full fresh plan.\n\nUse --changed to limit the generated plan to changed components only.\nUse --dry-run to preview without executing.",
	SilenceErrors: true,
	SilenceUsage:  true,
	Args:          cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runUseWorkDirOverride = cmd.Flags().Changed("workdir")
		if len(args) > 0 {
			runPlanRef = args[0]
		}
		return runPlan()
	},
}

func registerRunCommand(root *cobra.Command) {
	root.AddCommand(runCmd)

	runCmd.Flags().StringVarP(&runPlanRef, "plan", "p", "", "Plan reference: file path, name, or checksum prefix (deprecated: use positional argument)")

	// Plan generation flags — used when no planhash argument is given
	runCmd.Flags().BoolVar(&changedOnly, "changed", false, "Generate plan with only changed components (requires git)")
	runCmd.Flags().StringVar(&baseBranch, "base", "", "Base ref for changed detection (default: main)")
	runCmd.Flags().StringVar(&headRef, "head", "", "Head ref for changed detection (default: HEAD)")
	runCmd.Flags().StringSliceVar(&changedFiles, "files", nil, "Comma-separated changed files (overrides git diff calculation)")
	runCmd.Flags().BoolVar(&uncommitted, "uncommitted", false, "Use only uncommitted changes")
	runCmd.Flags().BoolVar(&untracked, "untracked", false, "Use only untracked files")
	runCmd.Flags().BoolVar(&explainChanged, "explain", false, "Show how --changed refs were resolved")
	runCmd.Flags().BoolVar(&runDryRun, "dry-run", false, "Preview the run without executing jobs")
	runCmd.Flags().BoolVar(&runVerbose, "verbose", false, "Expand step commands and raw logs inline")
	runCmd.Flags().StringVar(&runWorkDir, "workdir", ".", "Override working directory for all jobs")
	runCmd.Flags().StringVar(&runJobID, "job", "", "Run only a specific job (matches plan job ID or prefix)")
	runCmd.Flags().StringVar(&runJobID, "job-id", "", "Run only a specific job ID (deprecated: use --job)")
	runCmd.Flags().BoolVar(&runRetry, "retry", false, "Clear existing state for selected --job before running")
	runCmd.Flags().StringVar(&runRunner, "runner", "", "Execution backend: local, github-actions, docker")
	runCmd.Flags().BoolVar(&runGHACompat, "gha", false, "Enable GitHub Actions compatibility mode")
	runCmd.Flags().StringVar(&runExecID, "exec-id", "", "Execution ID (for resume or CI). Auto-generated if not set")
	runCmd.Flags().IntVar(&runConcurrency, "concurrency", 0, "Override plan concurrency (0 = use plan value)")
	runCmd.Flags().IntVar(&runComponentConcurrency, "component-concurrency", 1, "Max number of components processed concurrently (0 = unlimited). Default 1 keeps the dashboard component-grouped.")
	runCmd.Flags().StringArrayVar(&runComponent, "component", nil, "Filter jobs by component (repeatable)")
	runCmd.Flags().StringVarP(&runEnv, "env", "e", "", "Filter jobs by environment")
	runCmd.Flags().BoolVar(&runJSON, "json", false, "Output in JSON format")
	runCmd.Flags().StringVar(&runIsolation, "isolation", "auto", "Per-job workspace isolation: auto (on when concurrency>1), workspace (always on), none (legacy shared tree)")
	runCmd.Flags().BoolVar(&runKeepWorkspaces, "keep-workspaces", false, "Don't delete per-job staged workspaces after the run (debug)")
	runCmd.Flags().BoolVar(&runBackground, "background", false, "Run the plan detached and return immediately. Track via 'orun status --watch'")

	runCmd.Flags().BoolVar(&runRemoteState, "remote-state", false, "Use orun-backend for distributed run coordination (sets ORUN_REMOTE_STATE=true)")
	runCmd.Flags().StringVar(&runBackendURL, "backend-url", "", "orun-backend URL for remote state (or set ORUN_BACKEND_URL)")

	_ = runCmd.Flags().MarkDeprecated("job-id", "use --job instead")
}

// resolveEffectiveWorkDir returns the working directory to use for the runner.
// When no explicit --workdir override is set, it prefers intentRootDir (the
// project root where intent.yaml lives) over "." so that component-relative
// paths in the plan resolve correctly regardless of where the CLI is invoked.
func resolveEffectiveWorkDir(useOverride bool, workDir, intentRootDir string) string {
	if useOverride {
		return workDir
	}
	if workDir != "." {
		return workDir
	}
	if intentRootDir != "" {
		return intentRootDir
	}
	if abs, err := filepath.Abs(workDir); err == nil {
		return abs
	}
	return workDir
}

// isRemoteStateActive returns true when remote state should be used based on
// flag > ORUN_REMOTE_STATE > intent.execution.state.mode resolution.
func isRemoteStateActive(intent *model.Intent) bool {
	if runRemoteState {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv(remoteStateEnvVar)), "true") {
		return true
	}
	if intent != nil && strings.EqualFold(strings.TrimSpace(intent.Execution.State.Mode), "remote") {
		return true
	}
	return false
}

// resolveBackendURL returns the backend URL from flag > env > intent.
func resolveBackendURL(intent *model.Intent) string {
	if u := strings.TrimSpace(runBackendURL); u != "" {
		return u
	}
	if u := strings.TrimSpace(os.Getenv(backendURLEnvVar)); u != "" {
		return u
	}
	if intent != nil && strings.TrimSpace(intent.Execution.State.BackendURL) != "" {
		return strings.TrimSpace(intent.Execution.State.BackendURL)
	}
	return ""
}

func runPlan() error {
	if runGHACompat && executor.NormalizeRunnerName(runRunner) != "" && executor.NormalizeRunnerName(runRunner) != "github-actions" {
		return fmt.Errorf("--gha cannot be combined with --runner %q", runRunner)
	}

	store := state.NewStore(storeDir())

	// Migrate legacy state if present
	if migrated, _ := store.MigrateLegacyState(storeDir()); migrated {
		fmt.Println("✓ Migrated legacy .orun-state.json to .orun/executions/")
	}

	// Resolve plan reference
	plan, err := resolveAndLoadPlan(store)
	if err != nil {
		return err
	}

	// Load intent to check execution.state config (best-effort).
	var loadedIntent *model.Intent
	if intentFile != "" {
		if si, _, loadErr := loadResolvedIntentFile(intentFile); loadErr == nil {
			loadedIntent = si
		}
	}

	remoteActive := isRemoteStateActive(loadedIntent)
	backendURL := resolveBackendURL(loadedIntent)

	if remoteActive && backendURL == "" {
		return fmt.Errorf("--remote-state requires --backend-url or ORUN_BACKEND_URL (or intent.yaml execution.state.backendUrl)")
	}

	runnerName := resolveRunnerName(runRunner)
	if runGHACompat {
		runnerName = "github-actions"
	} else if shouldAutoUseGitHubActions(runRunner, plan) {
		runnerName = "github-actions"
	}
	runtime := runtimeContextForRunner(runnerName)
	selectedExecutor, err := executor.Get(runnerName)
	if err != nil {
		return err
	}
	if !runUseWorkDirOverride && runnerName == "github-actions" {
		if workspace := strings.TrimSpace(os.Getenv("GITHUB_WORKSPACE")); workspace != "" {
			runWorkDir = workspace
		}
	}
	runWorkDir = resolveEffectiveWorkDir(runUseWorkDirOverride, runWorkDir, intentRoot)

	planID := state.PlanChecksumShort(plan)

	// Resolve execution ID
	execID := runExecID
	if execID == "" {
		execID = os.Getenv(execIDEnvVar)
	}
	if remoteActive {
		execID = remotestate.DeriveRunID(planID, execID)
	} else if execID == "" {
		execID = state.GenerateExecID(plan.Metadata.Name)
	}

	if runRetry && runJobID == "" {
		return fmt.Errorf("--retry requires --job")
	}

	// If --background and we are NOT already the detached child, fork ourselves
	// and exit. The child re-enters runPlan with the same flags minus
	// --background and an explicit --exec-id.
	if runBackground && !isBackgroundChild() {
		if runDryRun {
			return fmt.Errorf("--background cannot be combined with --dry-run")
		}
		if _, err := store.CreateExecution(execID, plan); err != nil {
			return err
		}
		color := ui.ColorEnabledForWriter(os.Stdout)
		return startBackgroundRun(execID, store, color)
	}

	// Concurrency override
	concurrency := plan.Execution.Concurrency
	if runConcurrency > 0 {
		concurrency = runConcurrency
	}

	// Context-aware scoping: auto-detect component from CWD
	if len(runComponent) == 0 && !allFlag && intentRoot != "" {
		if scopeIntent, _, loadErr := loadResolvedIntentFile(intentFile); loadErr == nil {
			scope, _ := ResolveScope(scopeIntent, runComponent, allFlag, runJSON)
			if scope != nil && scope.WasAutoScoped {
				runComponent = scope.ScopedComponents

				if plan.Metadata.Scope != nil && !sameStringSlice(scope.ScopedComponents, plan.Metadata.Scope.Components) {
					color := ui.ColorEnabledForWriter(os.Stderr)
					fmt.Fprintf(os.Stderr, "%s plan was generated for [%s] but current scope is [%s]\n",
						ui.Yellow(color, "warning:"),
						strings.Join(plan.Metadata.Scope.Components, ", "),
						strings.Join(scope.ScopedComponents, ", "))
				}
			}
		}
	}

	if len(plan.Jobs) == 0 {
		color := ui.ColorEnabledForWriter(os.Stdout)
		fmt.Fprintf(os.Stdout, "%s no jobs to run\n", ui.Green(color, "✓"))
		return nil
	}

	r := runner.NewRunner(
		runWorkDir,
		runUseWorkDirOverride,
		os.Stdout,
		os.Stderr,
		runDryRun,
		runJobID,
		runRetry,
		runVerbose,
		selectedExecutor,
		runtime,
		store,
		execID,
		concurrency,
		runComponent,
		runEnv,
	)
	r.PlanID = planID
	r.Isolation = runner.IsolationMode(strings.ToLower(strings.TrimSpace(runIsolation)))
	r.KeepWorkspaces = runKeepWorkspaces
	r.ComponentConcurrency = runComponentConcurrency

	if remoteActive {
		if err := setupRemoteStateHooks(r, plan, planID, execID, backendURL); err != nil {
			var alreadyDone *jobAlreadyCompleteError
			if errors.As(err, &alreadyDone) {
				return nil
			}
			return err
		}
	} else if !runDryRun {
		setupLocalStateHooks(r, plan, execID)
	}

	if err := r.Run(plan); err != nil {
		return err
	}

	return nil
}

// setupRemoteStateHooks initialises the backend, performs InitRun, and wires
// hooks for per-job claim, heartbeat, log upload, and terminal update.
func setupRemoteStateHooks(r *runner.Runner, plan *model.Plan, planID, execID, backendURL string) error {
	tokenSrc, err := remotestate.ResolveTokenSource()
	if err != nil {
		return fmt.Errorf("remote state auth: %w", err)
	}

	client := remotestate.NewClient(backendURL, version, tokenSrc)
	runnerID := statebackend.DeriveRunnerID()
	backend := statebackend.NewRemoteStateBackend(client, runnerID)

	ctx := context.Background()
	actor := os.Getenv("GITHUB_ACTOR")
	handle, err := backend.InitRun(ctx, plan, statebackend.InitRunOptions{
		RunID:       execID,
		DryRun:      r.DryRun,
		Actor:       actor,
		TriggerType: "ci",
	})
	if err != nil {
		return fmt.Errorf("initializing remote run: %w", err)
	}
	// Use the run ID returned by the backend (idempotent join may return existing ID).
	r.ExecID = handle.RunID

	// For remote --job mode, perform the claim loop before the runner executes
	// and disable the local dependency check.
	if r.JobID != "" {
		if err := performRemoteJobClaim(ctx, backend, handle.RunID, plan, r.JobID, runnerID, r.Stdout, r.Color); err != nil {
			return err
		}
		r.SkipLocalDepsForJob = true
	}

	// Accumulate step logs per job so they can be uploaded as a single request.
	var logMu sync.Mutex
	jobLogs := map[string]string{}

	r.Hooks = &runner.RunnerHooks{
		BeforeJob: func(jobID string) (bool, error) {
			if r.JobID != "" {
				// Already claimed by the explicit job flow above.
				return false, nil
			}
			result, claimErr := backend.ClaimJob(ctx, handle.RunID, findJobByIDInPlan(plan, jobID), runnerID)
			if claimErr != nil {
				return false, claimErr
			}
			if result.DepsBlocked {
				return false, fmt.Errorf("job %s: upstream dependencies are blocked or failed", jobID)
			}
			if !result.Claimed {
				if strings.EqualFold(result.CurrentStatus, "success") {
					return true, nil // already done by another runner
				}
				if strings.EqualFold(result.CurrentStatus, "failed") {
					return false, fmt.Errorf("job %s: already failed on another runner", jobID)
				}
				return true, nil // skip — running elsewhere or unknown
			}
			// Start heartbeat goroutine for claimed job.
			go runHeartbeat(ctx, backend, handle.RunID, jobID, runnerID)
			return false, nil
		},
		AfterStepLog: func(jobID, stepID, output string) {
			logMu.Lock()
			prev := jobLogs[jobID]
			sep := ""
			if prev != "" {
				sep = "\n"
			}
			jobLogs[jobID] = prev + sep + "=== " + stepID + " ===\n" + output
			content := jobLogs[jobID]
			logMu.Unlock()
			// Best-effort upload; ignore errors to not interrupt execution.
			_ = backend.AppendStepLog(ctx, handle.RunID, jobID, content)
		},
		AfterJobTerminal: func(jobID string, success bool, errText string) {
			status := statebackend.JobStatusSuccess
			if !success {
				status = statebackend.JobStatusFailed
			}
			// Best-effort; ignore errors.
			_ = backend.UpdateJob(ctx, handle.RunID, jobID, runnerID, status, errText)
		},
	}

	// For explicit --job mode, the BeforeJob hook is not used; the claim was
	// already performed. Wire only heartbeat + terminal hooks.
	if r.JobID != "" {
		jobID := r.JobID
		go runHeartbeat(ctx, backend, handle.RunID, jobID, runnerID)
		origAfterTerminal := r.Hooks.AfterJobTerminal
		r.Hooks.AfterJobTerminal = func(jid string, success bool, errText string) {
			if jid == jobID {
				// AfterJobTerminal is already wired above; no-op here since the
				// general hook handles it.
			}
			if origAfterTerminal != nil {
				origAfterTerminal(jid, success, errText)
			}
		}
		r.Hooks.BeforeJob = nil
	}

	return nil
}

// setupLocalStateHooks wires BeforeJob and AfterJobTerminal hooks that use
// the file-lock-based FileStateBackend for cross-process claim safety.
func setupLocalStateHooks(r *runner.Runner, plan *model.Plan, execID string) {
	backend := statebackend.NewFileStateBackend(r.Store)
	backend.InitRunPlan(plan)
	ctx := context.Background()

	r.Hooks = &runner.RunnerHooks{
		BeforeJob: func(jobID string) (bool, error) {
			job := findJobByIDInPlan(plan, jobID)
			result, err := backend.ClaimJob(ctx, execID, job, "local")
			if err != nil {
				return false, err
			}
			if result.DepsBlocked {
				return false, fmt.Errorf("job %s: upstream dependencies failed", jobID)
			}
			if !result.Claimed {
				if result.CurrentStatus == "completed" || result.CurrentStatus == "running" {
					return true, nil
				}
				if result.CurrentStatus == "failed" {
					return false, fmt.Errorf("job %s: previously failed", jobID)
				}
				if result.DepsWaiting {
					return false, fmt.Errorf("job %s: waiting on dependencies", jobID)
				}
				return true, nil
			}
			return false, nil
		},
		AfterJobTerminal: func(jobID string, success bool, errText string) {
			status := statebackend.JobStatusSuccess
			if !success {
				status = statebackend.JobStatusFailed
			}
			_ = backend.UpdateJob(ctx, execID, jobID, "local", status, errText)
		},
	}
}

// performRemoteJobClaim executes the dependency-wait and claim loop for
// single-job remote execution.
func performRemoteJobClaim(
	ctx context.Context,
	backend statebackend.Backend,
	runID string,
	plan *model.Plan,
	jobID string,
	runnerID string,
	stdout interface{ Write([]byte) (int, error) },
	color bool,
) error {
	job := findJobByIDInPlan(plan, jobID)
	if job.ID == "" {
		return fmt.Errorf("job %q not found in plan", jobID)
	}

	const (
		depWaitTimeout = 30 * time.Minute
		initDelay      = 2 * time.Second
		maxDelay       = 60 * time.Second
	)
	deadline := time.Now().Add(depWaitTimeout)
	delay := initDelay

	for {
		result, err := backend.ClaimJob(ctx, runID, job, runnerID)
		if err != nil {
			return fmt.Errorf("claiming job %s: %w", jobID, err)
		}

		if result.Claimed {
			if result.Takeover {
				fmt.Fprintf(stdout, "  %s taking over job %s from a previous runner\n",
					ui.Yellow(color, "↻"), jobID)
			}
			return nil
		}

		switch {
		case result.DepsBlocked:
			return fmt.Errorf("job %s: upstream dependencies are blocked or failed; cannot proceed", jobID)
		case strings.EqualFold(result.CurrentStatus, "success"):
			fmt.Fprintf(stdout, "  %s job %s already completed by another runner\n",
				ui.Green(color, "✓"), jobID)
			// Signal the caller that we should exit 0.
			return &jobAlreadyCompleteError{jobID: jobID}
		case strings.EqualFold(result.CurrentStatus, "failed"):
			return fmt.Errorf("job %s: already failed on another runner", jobID)
		case result.DepsWaiting:
			// Poll /runnable until our job appears, then retry claim.
			if time.Now().After(deadline) {
				return fmt.Errorf("job %s: dependency wait timeout (%s) exceeded", jobID, depWaitTimeout)
			}
			fmt.Fprintf(stdout, "  %s waiting for dependencies of %s...\n",
				ui.Dim(color, "○"), jobID)
			if waitErr := waitForJobRunnable(ctx, backend, runID, jobID, delay, deadline); waitErr != nil {
				return waitErr
			}
		case strings.EqualFold(result.CurrentStatus, "running"):
			if time.Now().After(deadline) {
				return fmt.Errorf("job %s: wait timeout exceeded while another runner holds the job", jobID)
			}
			fmt.Fprintf(stdout, "  %s job %s is running on another runner, waiting...\n",
				ui.Cyan(color, "●"), jobID)
			if waitErr := sleepOrDone(ctx, delay); waitErr != nil {
				return waitErr
			}
		default:
			if time.Now().After(deadline) {
				return fmt.Errorf("job %s: claim timeout exceeded", jobID)
			}
			if waitErr := sleepOrDone(ctx, delay); waitErr != nil {
				return waitErr
			}
		}

		delay = nextBackoff(delay, maxDelay)
	}
}

// jobAlreadyCompleteError signals that a job was already completed by another runner.
type jobAlreadyCompleteError struct{ jobID string }

func (e *jobAlreadyCompleteError) Error() string {
	return fmt.Sprintf("job %s already completed by another runner", e.jobID)
}

// waitForJobRunnable polls /runnable until jobID appears or deadline is exceeded.
func waitForJobRunnable(ctx context.Context, backend statebackend.Backend, runID, jobID string, delay time.Duration, deadline time.Time) error {
	const (
		pollInit = 2 * time.Second
		pollMax  = 15 * time.Second
	)
	poll := pollInit
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("job %s: dependency wait timeout exceeded while polling /runnable", jobID)
		}
		if err := sleepOrDone(ctx, poll); err != nil {
			return err
		}
		jobs, err := backend.RunnableJobs(ctx, runID)
		if err != nil {
			return fmt.Errorf("polling runnable jobs: %w", err)
		}
		for _, id := range jobs {
			if id == jobID {
				return nil
			}
		}
		poll = nextBackoff(poll, pollMax)
	}
}

// runHeartbeat sends heartbeats every 30 seconds until the context is cancelled.
func runHeartbeat(ctx context.Context, backend statebackend.Backend, runID, jobID, runnerID string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Best-effort; ignore errors.
			_, _ = backend.Heartbeat(ctx, runID, jobID, runnerID)
		}
	}
}

func nextBackoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		return max
	}
	return next
}

func sleepOrDone(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// findJobByIDInPlan returns the plan job with the given ID or an empty PlanJob.
func findJobByIDInPlan(plan *model.Plan, jobID string) model.PlanJob {
	for _, job := range plan.Jobs {
		if job.ID == jobID {
			return job
		}
	}
	return model.PlanJob{}
}

func resolveAndLoadPlan(store *state.Store) (*model.Plan, error) {
	ref := runPlanRef
	if ref == "" {
		ref = os.Getenv("ORUN_PLAN_ID")
	}

	// If a specific ref was given, try to resolve it as a saved plan first.
	if ref != "" {
		if path, err := store.ResolvePlanRef(ref); err == nil {
			return loadPlan(path)
		}
		if fileExistsCheck(ref) {
			return loadPlan(ref)
		}
		// Not a plan ref or file path — treat as a component name to scope the fresh plan.
		planComponents = []string{ref}
		runComponent = []string{ref}
	}

	// Generate a fresh plan from current intent, respecting any filters.
	// Sync run-time env filter into the plan-generation global when not already set.
	if environment == "" {
		environment = runEnv
	}
	if len(planComponents) == 0 && len(runComponent) > 0 {
		planComponents = runComponent
	}
	if genErr := generatePlan(); genErr != nil {
		return nil, fmt.Errorf("failed to generate plan: %w", genErr)
	}
	path, err := store.ResolvePlanRef("latest")
	if err != nil {
		return nil, fmt.Errorf("failed to load generated plan: %w", err)
	}
	return loadPlan(path)
}

func resolveRunnerName(flagValue string) string {
	if normalized := executor.NormalizeRunnerName(flagValue); normalized != "" {
		return normalized
	}
	if normalized := executor.NormalizeRunnerName(runnerEnvValue()); normalized != "" {
		return normalized
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("GITHUB_ACTIONS")), "true") {
		return "github-actions"
	}
	return "local"
}

func shouldAutoUseGitHubActions(flagValue string, plan *model.Plan) bool {
	if !planUsesGitHubActions(plan) {
		return false
	}
	if executor.NormalizeRunnerName(flagValue) != "" {
		return false
	}
	if executor.NormalizeRunnerName(runnerEnvValue()) != "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("GITHUB_ACTIONS")), "true") {
		return false
	}
	return true
}

func planUsesGitHubActions(plan *model.Plan) bool {
	if plan == nil {
		return false
	}
	for _, job := range plan.Jobs {
		for _, step := range job.Steps {
			if strings.TrimSpace(step.Use) != "" {
				return true
			}
		}
	}
	return false
}

func runtimeContextForRunner(runnerName string) executor.RuntimeContext {
	switch executor.NormalizeRunnerName(runnerName) {
	case "docker":
		return executor.RuntimeContext{Runner: "docker", Environment: "container"}
	case "github-actions":
		return executor.RuntimeContext{Runner: "github-actions", Environment: "ci"}
	default:
		return executor.RuntimeContext{Runner: "local", Environment: "local"}
	}
}

func loadPlan(path string) (*model.Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read plan file %s: %w", path, err)
	}

	var plan model.Plan
	ext := filepath.Ext(path)
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &plan); err != nil {
			return nil, fmt.Errorf("failed to parse YAML plan: %w", err)
		}
	default:
		if err := json.Unmarshal(data, &plan); err != nil {
			if yamlErr := yaml.Unmarshal(data, &plan); yamlErr != nil {
				return nil, fmt.Errorf("failed to parse plan file as JSON or YAML: %w", err)
			}
		}
	}

	return &plan, nil
}

func fileExistsCheck(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
