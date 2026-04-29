package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sourceplane/gluon/internal/executor"
	"github.com/sourceplane/gluon/internal/model"
	"github.com/sourceplane/gluon/internal/runner"
	"github.com/sourceplane/gluon/internal/state"
	"github.com/sourceplane/gluon/internal/ui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	runPlanRef            string
	runDryRun             bool
	runVerbose            bool
	runWorkDir            string
	runUseWorkDirOverride bool
	runJobID              string
	runRetry              bool
	runRunner             string
	runGHACompat          bool
	runExecID             string
	runConcurrency        int
	runComponentConcurrency int
	runComponent          []string
	runEnv                string
	runJSON               bool
	runIsolation          string
	runKeepWorkspaces     bool
	runBackground         bool
)

var runCmd = &cobra.Command{
	Use:           "run",
	Short:         "Run a plan",
	Long:          "Run the jobs in a plan with concise live progress. Use --dry-run to preview without executing.",
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(cmd *cobra.Command, args []string) error {
		runUseWorkDirOverride = cmd.Flags().Changed("workdir")
		return runPlan()
	},
}

func registerRunCommand(root *cobra.Command) {
	root.AddCommand(runCmd)

	runCmd.Flags().StringVarP(&runPlanRef, "plan", "p", "", "Plan reference: file path, name, checksum prefix, or 'latest' (default: latest)")
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
	runCmd.Flags().BoolVar(&runBackground, "background", false, "Run the plan detached and return immediately. Track via 'gluon status --watch'")

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

func runPlan() error {
	if runGHACompat && executor.NormalizeRunnerName(runRunner) != "" && executor.NormalizeRunnerName(runRunner) != "github-actions" {
		return fmt.Errorf("--gha cannot be combined with --runner %q", runRunner)
	}

	store := state.NewStore(storeDir())

	// Migrate legacy state if present
	if migrated, _ := store.MigrateLegacyState(storeDir()); migrated {
		fmt.Println("✓ Migrated legacy .gluon-state.json to .gluon/executions/")
	}

	// Resolve plan reference
	plan, err := resolveAndLoadPlan(store)
	if err != nil {
		return err
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

	// Resolve execution ID
	execID := runExecID
	if execID == "" {
		execID = os.Getenv("GLUON_EXEC_ID")
	}
	if execID == "" {
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
	r.Isolation = runner.IsolationMode(strings.ToLower(strings.TrimSpace(runIsolation)))
	r.KeepWorkspaces = runKeepWorkspaces
	r.ComponentConcurrency = runComponentConcurrency
	if err := r.Run(plan); err != nil {
		return err
	}

	return nil
}

func resolveAndLoadPlan(store *state.Store) (*model.Plan, error) {
	ref := runPlanRef
	if ref == "" {
		ref = os.Getenv("GLUON_PLAN_ID")
	}

	// If no plan ref given, try latest from store
	if ref == "" {
		path, err := store.ResolvePlanRef("latest")
		if err != nil {
			// No plan exists yet — auto-generate
			fmt.Println("No saved plan found. Generating one from intent...")
			if genErr := generatePlan(); genErr != nil {
				return nil, fmt.Errorf("failed to auto-generate plan: %w", genErr)
			}
			path, err = store.ResolvePlanRef("latest")
			if err != nil {
				return nil, fmt.Errorf("failed to load generated plan: %w", err)
			}
		}
		return loadPlan(path)
	}

	// Try store resolution first (name, checksum prefix, latest)
	path, err := store.ResolvePlanRef(ref)
	if err != nil {
		// Fall back to direct file path
		if fileExistsCheck(ref) {
			return loadPlan(ref)
		}
		return nil, fmt.Errorf("plan not found: %s", ref)
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

	if len(plan.Jobs) == 0 {
		return nil, fmt.Errorf("plan contains no jobs")
	}

	return &plan, nil
}

func fileExistsCheck(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
