package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sourceplane/gluon/internal/model"
	"github.com/sourceplane/gluon/internal/state"
	"github.com/sourceplane/gluon/internal/ui"
	"github.com/spf13/cobra"
)

var describeCmd = &cobra.Command{
	Use:   "describe <resource> <name>",
	Short: "Show detailed information about a resource",
	Long:  "Show detailed information. Supports slash notation: describe run/latest, describe job/<id>, describe plan/latest",
}

func registerDescribeCommand(root *cobra.Command) {
	root.AddCommand(describeCmd)

	describeCmd.AddCommand(&cobra.Command{
		Use:     "run [id]",
		Short:   "Describe an execution",
		Aliases: []string{"execution"},
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := "latest"
			if len(args) > 0 {
				ref = args[0]
			}
			return describeRun(ref)
		},
	})

	describeCmd.AddCommand(&cobra.Command{
		Use:   "plan [id|name]",
		Short: "Describe a plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := "latest"
			if len(args) > 0 {
				ref = args[0]
			}
			return describePlan(ref)
		},
	})

	describeCmd.AddCommand(&cobra.Command{
		Use:   "job <job-id>",
		Short: "Describe a job",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("job ID required")
			}
			return describeJob(args[0])
		},
	})

	describeCmd.AddCommand(&cobra.Command{
		Use:     "component <name>",
		Short:   "Describe a component",
		Aliases: []string{"comp"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return listComponents(args)
		},
	})

	// Support slash notation on the parent command itself
	describeCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		arg := args[0]
		if strings.HasPrefix(arg, "run/") {
			return describeRun(strings.TrimPrefix(arg, "run/"))
		}
		if strings.HasPrefix(arg, "plan/") {
			return describePlan(strings.TrimPrefix(arg, "plan/"))
		}
		if strings.HasPrefix(arg, "job/") {
			return describeJob(strings.TrimPrefix(arg, "job/"))
		}
		if strings.HasPrefix(arg, "component/") {
			return listComponents([]string{strings.TrimPrefix(arg, "component/")})
		}
		return cmd.Help()
	}
}

func describeRun(ref string) error {
	store := state.NewStore(storeDir())
	color := ui.ColorEnabledForWriter(os.Stdout)

	execID, err := store.ResolveExecID(ref)
	if err != nil {
		return err
	}

	meta, _ := store.LoadMetadata(execID)
	st, _ := store.LoadState(execID)

	fmt.Fprintf(os.Stdout, "\n%s\n", ui.Bold(color, "Execution: "+execID))
	fmt.Fprintln(os.Stdout, strings.Repeat("─", 60))

	if meta != nil {
		fmt.Fprintf(os.Stdout, "Plan:        %s\n", meta.PlanName)
		fmt.Fprintf(os.Stdout, "Plan ID:     %s\n", meta.PlanID)
		fmt.Fprintf(os.Stdout, "Status:      %s %s\n", styleStatus(meta.Status, color), meta.Status)
		fmt.Fprintf(os.Stdout, "Started:     %s\n", meta.StartedAt)
		if meta.FinishedAt != "" {
			fmt.Fprintf(os.Stdout, "Finished:    %s\n", meta.FinishedAt)
			fmt.Fprintf(os.Stdout, "Duration:    %s\n", formatDuration(meta.StartedAt, meta.FinishedAt))
		}
		fmt.Fprintf(os.Stdout, "Trigger:     %s\n", meta.Trigger)
		fmt.Fprintf(os.Stdout, "User:        %s\n", meta.User)
		fmt.Fprintf(os.Stdout, "Jobs:        %d total, %d completed, %d failed\n", meta.JobTotal, meta.JobDone, meta.JobFailed)
	}

	if st != nil {
		fmt.Fprintf(os.Stdout, "\n%s\n", ui.Bold(color, "Jobs"))
		for jobID, js := range st.Jobs {
			if js == nil {
				continue
			}
			icon := styleStatus(js.Status, color)
			duration := ""
			if js.StartedAt != "" && js.FinishedAt != "" {
				duration = formatDuration(js.StartedAt, js.FinishedAt)
			}
			fmt.Fprintf(os.Stdout, "  %s %-50s %-10s %s\n", icon, jobID, js.Status, duration)
			if js.LastError != "" {
				fmt.Fprintf(os.Stdout, "    %s %s\n", ui.Dim(color, "↳"), ui.Red(color, js.LastError))
			}

			for stepID, stepStatus := range js.Steps {
				stepIcon := styleStatus(stepStatus, color)
				fmt.Fprintf(os.Stdout, "      %s %s\n", stepIcon, stepID)
			}
		}
	}

	return nil
}

func describePlan(ref string) error {
	store := state.NewStore(storeDir())
	color := ui.ColorEnabledForWriter(os.Stdout)

	path, err := store.ResolvePlanRef(ref)
	if err != nil {
		return err
	}

	plan, err := loadPlan(path)
	if err != nil {
		return err
	}

	if getOutputFormat == "json" {
		data, _ := json.MarshalIndent(plan, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	planID := state.PlanChecksumShort(plan)
	fmt.Fprintf(os.Stdout, "\n%s\n", ui.Bold(color, "Plan: "+plan.Metadata.Name))
	fmt.Fprintln(os.Stdout, strings.Repeat("─", 60))
	fmt.Fprintf(os.Stdout, "Plan ID:      %s\n", planID)
	fmt.Fprintf(os.Stdout, "Generated:    %s\n", plan.Metadata.GeneratedAt)
	fmt.Fprintf(os.Stdout, "Checksum:     %s\n", plan.Metadata.Checksum)
	fmt.Fprintf(os.Stdout, "Concurrency:  %d\n", plan.Execution.Concurrency)
	fmt.Fprintf(os.Stdout, "Fail Fast:    %v\n", plan.Execution.FailFast)
	fmt.Fprintf(os.Stdout, "Jobs:         %d\n", len(plan.Jobs))

	if len(plan.Spec.CompositionSources) > 0 {
		fmt.Fprintf(os.Stdout, "\n%s\n", ui.Bold(color, "Composition Sources"))
		for _, src := range plan.Spec.CompositionSources {
			fmt.Fprintf(os.Stdout, "  ├─ %s (%s)\n", src.Name, src.Kind)
		}
	}

	fmt.Fprintf(os.Stdout, "\n%s\n", ui.Bold(color, "Jobs"))
	for _, job := range plan.Jobs {
		deps := ""
		if len(job.DependsOn) > 0 {
			deps = " → " + strings.Join(job.DependsOn, ", ")
		}
		fmt.Fprintf(os.Stdout, "  ├─ %s (%d steps)%s\n", job.ID, len(job.Steps), deps)
	}

	return nil
}

func describeJob(jobRef string) error {
	store := state.NewStore(storeDir())
	color := ui.ColorEnabledForWriter(os.Stdout)

	// Load latest plan to find job details
	path, err := store.ResolvePlanRef("latest")
	if err != nil {
		return fmt.Errorf("no plan found: %w", err)
	}

	plan, err := loadPlan(path)
	if err != nil {
		return err
	}

	var job *PlanJobRef
	for _, j := range plan.Jobs {
		if j.ID == jobRef || strings.HasPrefix(j.ID, jobRef) {
			j := j
			job = &PlanJobRef{Job: j}
			break
		}
	}
	if job == nil {
		return fmt.Errorf("job not found: %s", jobRef)
	}

	fmt.Fprintf(os.Stdout, "\n%s\n", ui.Bold(color, "Job: "+job.Job.ID))
	fmt.Fprintln(os.Stdout, strings.Repeat("─", 60))
	fmt.Fprintf(os.Stdout, "Component:    %s\n", job.Job.Component)
	fmt.Fprintf(os.Stdout, "Environment:  %s\n", job.Job.Environment)
	fmt.Fprintf(os.Stdout, "Composition:  %s\n", job.Job.Composition)
	fmt.Fprintf(os.Stdout, "Path:         %s\n", job.Job.Path)
	fmt.Fprintf(os.Stdout, "Timeout:      %s\n", job.Job.Timeout)
	fmt.Fprintf(os.Stdout, "Retries:      %d\n", job.Job.Retries)

	if len(job.Job.DependsOn) > 0 {
		fmt.Fprintf(os.Stdout, "Dependencies: %s\n", strings.Join(job.Job.DependsOn, ", "))
	}

	fmt.Fprintf(os.Stdout, "\n%s (%d)\n", ui.Bold(color, "Steps"), len(job.Job.Steps))
	for i, step := range job.Job.Steps {
		stepID := step.ID
		if stepID == "" {
			stepID = step.Name
		}
		fmt.Fprintf(os.Stdout, "  %d. %s", i+1, stepID)
		if step.Phase != "" && step.Phase != "main" {
			fmt.Fprintf(os.Stdout, " [%s]", step.Phase)
		}
		fmt.Fprintln(os.Stdout)
		if step.Run != "" {
			for _, line := range strings.Split(strings.TrimSpace(step.Run), "\n") {
				fmt.Fprintf(os.Stdout, "     %s\n", strings.TrimSpace(line))
			}
		}
		if step.Use != "" {
			fmt.Fprintf(os.Stdout, "     use: %s\n", step.Use)
		}
	}

	// Show state from latest execution if available
	execID, resolveErr := store.ResolveExecID("latest")
	if resolveErr == nil {
		st, _ := store.LoadState(execID)
		if st != nil {
			if js, ok := st.Jobs[job.Job.ID]; ok && js != nil {
				fmt.Fprintf(os.Stdout, "\n%s (from execution %s)\n", ui.Bold(color, "State"), execID)
				fmt.Fprintf(os.Stdout, "  Status:   %s %s\n", styleStatus(js.Status, color), js.Status)
				if js.StartedAt != "" {
					fmt.Fprintf(os.Stdout, "  Started:  %s\n", js.StartedAt)
				}
				if js.FinishedAt != "" {
					fmt.Fprintf(os.Stdout, "  Finished: %s\n", js.FinishedAt)
				}
				if js.LastError != "" {
					fmt.Fprintf(os.Stdout, "  Error:    %s\n", ui.Red(color, js.LastError))
				}
			}
		}
	}

	return nil
}

type PlanJobRef struct {
	Job model.PlanJob
}
