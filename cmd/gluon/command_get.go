package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sourceplane/gluon/internal/state"
	"github.com/sourceplane/gluon/internal/ui"
	"github.com/spf13/cobra"
)

var (
	getOutputFormat string
	getPlanRef      string
)

var getCmd = &cobra.Command{
	Use:   "get <resource>",
	Short: "List resources (plans, runs, jobs, components, environments)",
	Long:  "kubectl-style resource listing. Usage: gluon get plans, gluon get runs, gluon get jobs",
}

func registerGetCommand(root *cobra.Command) {
	root.AddCommand(getCmd)

	getCmd.PersistentFlags().StringVarP(&getOutputFormat, "output", "o", "", "Output format: json, yaml, wide")
	getCmd.PersistentFlags().StringVar(&getPlanRef, "plan", "", "Plan reference for job listing")

	getCmd.AddCommand(&cobra.Command{
		Use:     "plans",
		Short:   "List stored plans",
		Aliases: []string{"plan"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return getPlans()
		},
	})

	getCmd.AddCommand(&cobra.Command{
		Use:     "runs",
		Short:   "List executions",
		Aliases: []string{"run", "executions"},
		RunE: func(cmd *cobra.Command, args []string) error {
			statusAll = true
			return showStatus()
		},
	})

	getCmd.AddCommand(&cobra.Command{
		Use:     "jobs",
		Short:   "List jobs in a plan",
		Aliases: []string{"job"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJobs()
		},
	})

	getCmd.AddCommand(&cobra.Command{
		Use:     "components",
		Short:   "List components from intent",
		Aliases: []string{"component"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return listComponents(args)
		},
	})

	getCmd.AddCommand(&cobra.Command{
		Use:     "environments",
		Short:   "List environments from intent",
		Aliases: []string{"environment", "env", "envs"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return getEnvironments()
		},
	})
}

func getPlans() error {
	store := state.NewStore(".")
	plans, err := store.ListPlans()
	if err != nil {
		return err
	}
	if len(plans) == 0 {
		fmt.Println("No plans found. Run 'gluon plan' to generate one.")
		return nil
	}

	if getOutputFormat == "json" {
		data, _ := json.MarshalIndent(plans, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	color := ui.ColorEnabledForWriter(os.Stdout)
	fmt.Fprintf(os.Stdout, "%-20s %-14s %-6s %-24s %s\n",
		"NAME", "ID", "JOBS", "GENERATED", "AGE")

	for _, p := range plans {
		age := formatAge(p.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
		generated := p.CreatedAt.Format("2006-01-02 15:04:05")
		name := p.Name
		if name == "latest" {
			name = ui.Bold(color, "latest")
		}
		fmt.Fprintf(os.Stdout, "%-20s %-14s %-6d %-24s %s\n",
			name, p.Checksum, p.Jobs, generated, age)
	}

	return nil
}

func getJobs() error {
	store := state.NewStore(".")

	ref := getPlanRef
	if ref == "" {
		ref = "latest"
	}

	path, err := store.ResolvePlanRef(ref)
	if err != nil {
		return fmt.Errorf("no plan found: %w", err)
	}

	plan, err := loadPlan(path)
	if err != nil {
		return err
	}

	// Try to get state from latest execution
	var execState *state.ExecState
	execID, resolveErr := store.ResolveExecID("latest")
	if resolveErr == nil {
		execState, _ = store.LoadState(execID)
	}

	if getOutputFormat == "json" {
		data, _ := json.MarshalIndent(plan.Jobs, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	color := ui.ColorEnabledForWriter(os.Stdout)
	fmt.Fprintf(os.Stdout, "%-50s %-18s %-14s %s\n",
		"JOB ID", "COMPONENT", "ENV", "STATUS")

	for _, job := range plan.Jobs {
		status := "pending"
		if execState != nil {
			if js, ok := execState.Jobs[job.ID]; ok && js != nil {
				status = js.Status
			}
		}
		icon := statusSymbol(status, color)
		fmt.Fprintf(os.Stdout, "%s %-48s %-18s %-14s %s\n",
			icon, job.ID, job.Component, job.Environment, status)
	}

	return nil
}

func getEnvironments() error {
	intent, _, err := loadResolvedIntentFile(intentFile)
	if err != nil {
		return fmt.Errorf("failed to load intent: %w", err)
	}

	if getOutputFormat == "json" {
		data, _ := json.MarshalIndent(intent.Environments, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	color := ui.ColorEnabledForWriter(os.Stdout)
	_ = color
	fmt.Fprintf(os.Stdout, "%-20s %-30s %s\n", "NAME", "SELECTORS", "DEFAULTS")

	for name, env := range intent.Environments {
		selectors := []string{}
		if len(env.Selectors.Components) > 0 {
			selectors = append(selectors, fmt.Sprintf("components=%s", strings.Join(env.Selectors.Components, ",")))
		}
		if len(env.Selectors.Domains) > 0 {
			selectors = append(selectors, fmt.Sprintf("domains=%s", strings.Join(env.Selectors.Domains, ",")))
		}
		selectorStr := strings.Join(selectors, " ")

		defaults := []string{}
		for k, v := range env.Defaults {
			defaults = append(defaults, fmt.Sprintf("%s=%v", k, v))
		}
		defaultStr := strings.Join(defaults, " ")

		fmt.Fprintf(os.Stdout, "%-20s %-30s %s\n", name, selectorStr, defaultStr)
	}

	return nil
}
