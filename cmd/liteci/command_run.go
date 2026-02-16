package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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
}

func runPlan() error {
	plan, err := loadPlan(runPlanFile)
	if err != nil {
		return err
	}

	dryRun := !runExecute
	if dryRun {
		fmt.Println("□ Dry-run mode enabled. Use --execute to run commands.")
	}

	if runRetry && runJobID == "" {
		return fmt.Errorf("--retry requires --job-id")
	}

	r := runner.NewRunner(runWorkDir, runUseWorkDirOverride, os.Stdout, os.Stderr, dryRun, runJobID, runRetry)
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
