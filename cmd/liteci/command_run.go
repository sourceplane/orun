package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sourceplane/liteci/internal/executor"
	"github.com/sourceplane/liteci/internal/model"
	"github.com/sourceplane/liteci/internal/runner"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	runPlanFile           string
	runExecute            bool
	runWorkDir            string
	runUseWorkDirOverride bool
	runJobID              string
	runRetry              bool
	runRunner             string
	runGHACompat          bool
)

var runCmd = &cobra.Command{
	Use:          "run",
	Short:        "Execute a compiled plan",
	Long:         "Execute the jobs and steps from a generated plan file, similar to an apply phase.",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		runUseWorkDirOverride = cmd.Flags().Changed("workdir")
		return runPlan()
	},
}

func registerRunCommand(root *cobra.Command) {
	root.AddCommand(runCmd)

	runCmd.Flags().StringVarP(&runPlanFile, "plan", "p", "plan.json", "Path to plan file (json or yaml)")
	runCmd.Flags().BoolVarP(&runExecute, "execute", "x", false, "Actually execute commands (default is dry-run)")
	runCmd.Flags().StringVar(&runWorkDir, "workdir", ".", "Override working directory for all jobs (default behavior uses each job path)")
	runCmd.Flags().StringVar(&runJobID, "job-id", "", "Run only a specific job ID (must match plan job id)")
	runCmd.Flags().BoolVar(&runRetry, "retry", false, "Clear existing state for selected --job-id before running")
	runCmd.Flags().StringVar(&runRunner, "runner", "", "Execution backend: local, github-actions, docker")
	runCmd.Flags().BoolVar(&runGHACompat, "gha", false, "Enable GitHub Actions compatibility mode")
}

func runPlan() error {
	if runGHACompat && executor.NormalizeRunnerName(runRunner) != "" && executor.NormalizeRunnerName(runRunner) != "github-actions" {
		return fmt.Errorf("--gha cannot be combined with --runner %q", runRunner)
	}

	plan, err := loadPlan(runPlanFile)
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

	dryRun := !runExecute
	if dryRun {
		fmt.Println("□ Dry-run mode enabled. Use --execute to run commands.")
	}

	if runRetry && runJobID == "" {
		return fmt.Errorf("--retry requires --job-id")
	}

	r := runner.NewRunner(runWorkDir, runUseWorkDirOverride, os.Stdout, os.Stderr, dryRun, runJobID, runRetry, selectedExecutor, runtime)
	if err := r.Run(plan); err != nil {
		return err
	}

	if dryRun {
		fmt.Println("✓ Dry-run complete")
	} else {
		fmt.Println("✓ Run complete")
	}

	return nil
}

func resolveRunnerName(flagValue string) string {
	if normalized := executor.NormalizeRunnerName(flagValue); normalized != "" {
		return normalized
	}
	if normalized := executor.NormalizeRunnerName(os.Getenv("LITECI_RUNNER")); normalized != "" {
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
	if executor.NormalizeRunnerName(os.Getenv("LITECI_RUNNER")) != "" {
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
