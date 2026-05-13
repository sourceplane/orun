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
	return resolveBackendURLWithConfig(intent, runBackendURL)
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

	// When intent auto-discovery failed (intentRoot is empty), recover it from
	// the plan's embedded workDir. This handles the common GHA case where the
	// intent lives in a repo subdirectory and orun run is invoked from the
	// workspace root (where walking upward doesn't find intent.yaml).
	if intentRoot == "" && strings.TrimSpace(plan.Metadata.WorkDir) != "" {
		if cwd, err := os.Getwd(); err == nil {
			intentRoot = filepath.Join(cwd, filepath.FromSlash(plan.Metadata.WorkDir))
		}
	}

	if !runUseWorkDirOverride && runnerName == "github-actions" {
		// Only use GITHUB_WORKSPACE when we have no intentRoot — if the intent lives
		// in a subdirectory of the repo (e.g. examples/intent.yaml), intentRoot is the
		// correct base for component paths, not the workspace root.
		if workspace := strings.TrimSpace(os.Getenv("GITHUB_WORKSPACE")); workspace != "" && intentRoot == "" {
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
		execID = remotestate.DeriveRunID(execID)
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
	ctx := context.Background()

	repo, err := resolveRepoContext(backendURL)
	if err != nil && os.Getenv("GITHUB_ACTIONS") != "true" {
		return err
	}
	namespaceID := ""
	if repo != nil {
		namespaceID = repo.NamespaceID
	}

	// Outside GitHub Actions, auto-resolve namespace ID from the CLI session when
	// no cached link exists. ORUN_TOKEN is not a CLI session token, so it cannot
	// call the session link endpoint — require a pre-cached namespace link instead.
	if namespaceID == "" && os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ORUN_TOKEN") != "" {
			return fmt.Errorf(
				"local remote-state with ORUN_TOKEN requires a pre-linked namespace; " +
					"run `orun cloud link --backend-url %s` first to cache the namespace ID",
				backendURL,
			)
		}
		if repo == nil || repo.RepoFullName == "" {
			return fmt.Errorf(
				"local remote-state requires a GitHub remote to resolve the namespace; " +
					"no GitHub remote detected in this workspace",
			)
		}
		resolved, resolveErr := autoResolveNamespace(ctx, backendURL, repo.RepoFullName)
		if resolveErr != nil {
			return resolveErr
		}
		namespaceID = resolved.NamespaceID
		_ = persistRepoLink(backendURL, repo, resolved)
	}

	tokenSrc, resolvedNamespaceID, githubLogin, err := remotestate.ResolveTokenSource(ctx, remotestate.ResolveOptions{
		BackendURL:   backendURL,
		Version:      version,
		Interactive:  termIsInteractive(),
		RequireLogin: true,
		NamespaceID:  namespaceID,
	})
	if err != nil {
		return fmt.Errorf("remote state auth: %w", err)
	}
	if namespaceID == "" {
		namespaceID = resolvedNamespaceID
	}

	client := remotestate.NewClient(backendURL, version, tokenSrc)
	runnerID := statebackend.DeriveRunnerID()
	backend := statebackend.NewRemoteStateBackend(client, runnerID)

	actor := os.Getenv("GITHUB_ACTOR")
	if actor == "" {
		actor = githubLogin
	}
	repoFullName := ""
	if os.Getenv("GITHUB_ACTIONS") != "true" && repo != nil {
		repoFullName = repo.RepoFullName
	}
	handle, err := backend.InitRun(ctx, plan, statebackend.InitRunOptions{
		RunID:        execID,
		NamespaceID:  namespaceID,
		RepoFullName: repoFullName,
		DryRun:       r.DryRun,
		Actor:        actor,
		TriggerType:  triggerTypeForRemoteRun(),
	})
	if err != nil {
		var apiErr *remotestate.APIError
		if errors.As(err, &apiErr) && apiErr.Code == "INVALID_REQUEST" && strings.Contains(apiErr.Message, "namespace") {
			return fmt.Errorf("initializing remote run: namespace not resolved; run `orun auth login` and retry, or `orun cloud link --backend-url %s`", backendURL)
		}
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
			updateJobWithRetry(ctx, backend, handle.RunID, jobID, runnerID, status, errText)
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
			waitErr := waitForJobRunnable(ctx, backend, runID, jobID, job.DependsOn, plan, delay, deadline)
			if waitErr != nil && !errors.Is(waitErr, errDepHeartbeatStale) {
				return waitErr
			}
			// errDepHeartbeatStale: a dep's heartbeat expired — retry the claim
			// so the coordinator can sweep the stale dep and return depsBlocked.
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

// errDepHeartbeatStale is returned by waitForJobRunnable when a dependency's
// heartbeat has expired. The caller should retry the claim so the coordinator
// can sweep the stale dep inline and return depsBlocked.
var errDepHeartbeatStale = errors.New("dependency heartbeat stale")

// waitForJobRunnable polls /runnable until jobID appears or deadline is exceeded.
// deps lists the job's declared dependencies; on each poll that doesn't find the
// job, LoadRunState is called to detect upstream failures early and avoid waiting
// the full depWaitTimeout when a dependency has already failed or is permanently blocked.
func waitForJobRunnable(ctx context.Context, backend statebackend.Backend, runID, jobID string, deps []string, plan *model.Plan, initialDelay time.Duration, deadline time.Time) error {
	const pollMax = 15 * time.Second
	const heartbeatTimeout = 5 * time.Minute // matches coordinator HEARTBEAT_TIMEOUT_MS
	if initialDelay <= 0 {
		initialDelay = 2 * time.Second
	}
	poll := initialDelay
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
		// Check if any dependency has already failed, is permanently blocked (its own
		// deps failed but it never ran), or has an expired heartbeat.
		if len(deps) > 0 {
			if execState, _, stateErr := backend.LoadRunState(ctx, runID); stateErr == nil && execState != nil {
				for _, dep := range deps {
					js, ok := execState.Jobs[dep]
					if !ok || js == nil {
						continue
					}
					if js.Status == "failed" {
						return fmt.Errorf("job %s: dependency %s failed", jobID, dep)
					}
					// A "pending" dep that is transitively blocked (one of its own deps
					// failed) will never become runnable — fail fast rather than waiting.
					if js.Status == "pending" && plan != nil && isTransitivelyBlocked(execState, plan, dep) {
						return fmt.Errorf("job %s: dependency %s is permanently blocked (upstream failure)", jobID, dep)
					}
					// When a "running" dep's heartbeat has expired, exit the wait loop
					// so performRemoteJobClaim retries the claim. The coordinator will
					// sweep the stale dep on the next claim and return depsBlocked.
					if js.Status == "running" && js.HeartbeatAt != "" {
						if hb, hbErr := time.Parse(time.RFC3339, js.HeartbeatAt); hbErr == nil {
							if time.Since(hb) > heartbeatTimeout {
								return errDepHeartbeatStale
							}
						}
					}
				}
			}
		}
		poll = nextBackoff(poll, pollMax)
	}
}

// isTransitivelyBlocked returns true if any dependency of depID (recursively)
// is in "failed" status, meaning depID can never become runnable.
func isTransitivelyBlocked(execState *state.ExecState, plan *model.Plan, depID string) bool {
	depJob := findJobByIDInPlan(plan, depID)
	for _, transitiveDep := range depJob.DependsOn {
		js, ok := execState.Jobs[transitiveDep]
		if !ok || js == nil {
			continue
		}
		if js.Status == "failed" {
			return true
		}
		if isTransitivelyBlocked(execState, plan, transitiveDep) {
			return true
		}
	}
	return false
}

// updateJobWithRetry calls UpdateJob with exponential backoff for up to 90 s.
// Terminal status must reach the coordinator so dependent jobs don't get stuck.
// The heartbeat goroutine (running in the background) keeps the coordinator's
// heartbeatAt fresh during retries, preventing false "heartbeat timed out"
// errors in downstream dep-wait polling.
func updateJobWithRetry(ctx context.Context, backend statebackend.Backend, runID, jobID, runnerID string, status statebackend.JobStatus, errText string) {
	const maxDuration = 90 * time.Second
	deadline := time.Now().Add(maxDuration)
	delay := 2 * time.Second
	for {
		if err := backend.UpdateJob(ctx, runID, jobID, runnerID, status, errText); err == nil {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if delay < 30*time.Second {
			delay *= 2
		}
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

func termIsInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func triggerTypeForRemoteRun() string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("GITHUB_ACTIONS")), "true") {
		return "ci"
	}
	return "local"
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
