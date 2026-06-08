package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/objview"
	"github.com/sourceplane/orun/internal/ui"
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
		Use:   "revision [latest|checksum]",
		Short: "Describe a revision from the object-model graph",
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := ""
			if len(args) > 0 {
				ref = args[0]
			}
			return describeRevision(ref)
		},
	})

	describeCmd.AddCommand(&cobra.Command{
		Use:   "trigger [latest|checksum]",
		Short: "Describe the trigger that produced a revision",
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := ""
			if len(args) > 0 {
				ref = args[0]
			}
			return describeTrigger(ref)
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
		if strings.HasPrefix(arg, "execution/") {
			return describeRun(strings.TrimPrefix(arg, "execution/"))
		}
		if strings.HasPrefix(arg, "revision/") {
			return describeRevision(strings.TrimPrefix(arg, "revision/"))
		}
		if strings.HasPrefix(arg, "trigger/") {
			return describeTrigger(strings.TrimPrefix(arg, "trigger/"))
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
	color := ui.ColorEnabledForWriter(os.Stdout)

	reader, ok := openObjectReader()
	if !ok {
		return fmt.Errorf("no executions found")
	}
	r := ref
	if r == "" {
		r = "executions/latest"
	}
	v, err := reader.Get(context.Background(), r)
	if err != nil {
		return fmt.Errorf("execution not found: %s", ref)
	}
	return renderDescribeRun(v.ExecutionID, objview.ToMeta(v), objview.ToState(v), color)
}

// renderDescribeRun prints the execution detail block.
func renderDescribeRun(execID string, meta *execmodel.ExecMetadata, st *execmodel.ExecState, color bool) error {

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
	color := ui.ColorEnabledForWriter(os.Stdout)

	// Resolve the plan from the object-model revision graph.
	plan, ok := objResolvePlan(ref)
	if !ok {
		return fmt.Errorf("plan not found: %s", ref)
	}

	if getOutputFormat == "json" {
		data, _ := json.MarshalIndent(plan, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	planID := execmodel.PlanChecksumShort(plan)
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
	color := ui.ColorEnabledForWriter(os.Stdout)

	// Load latest plan to find job details from the object-model revision graph.
	plan, ok := objResolvePlan("latest")
	if !ok {
		return fmt.Errorf("no plan found")
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

	// Show state from the latest execution if available (object graph).
	var execID string
	var st *execmodel.ExecState
	if reader, ok := openObjectReader(); ok {
		if v, err := reader.Get(context.Background(), "executions/latest"); err == nil {
			execID = v.ExecutionID
			st = objview.ToState(v)
		}
	}
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

	return nil
}

type PlanJobRef struct {
	Job model.PlanJob
}

// describeRevision renders a PlanRevision and the trigger that produced it from
// the object-model revision graph. ref accepts the same grammar as describePlan:
// "" / "latest" → revisions/latest, else a plan checksum (full or unique
// prefix). The legacy per-revision manifest summary (latest-execution line) is
// gone with catalogstore; execution detail now lives in `orun status` /
// `orun catalog history` / `describe run`.
func describeRevision(ref string) error {
	if ref == "latest" {
		ref = ""
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	d, ok := objResolveRevisionDetail(ref)
	if !ok {
		return fmt.Errorf("revision not found: %s", ref)
	}
	rev := d.Revision
	trig := d.Trigger
	revKey := rev.HumanKey
	if revKey == "" {
		revKey = string(d.RevID)
	}
	source := "by-hash"
	if d.FromLatest {
		source = "latest"
	}

	if getOutputFormat == "json" {
		out := map[string]interface{}{
			"revisionKey": revKey,
			"revisionId":  string(d.RevID),
			"planHash":    rev.PlanHash,
			"sourceId":    rev.SourceID,
			"catalogId":   rev.CatalogID,
			"jobCount":    rev.JobCount,
			"scope":       rev.Scope,
			"source":      source,
		}
		if d.HasTrigger {
			out["trigger"] = trig
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	fmt.Fprintf(os.Stdout, "\n%s\n", ui.Bold(color, "Revision: "+revKey))
	fmt.Fprintln(os.Stdout, strings.Repeat("─", 60))
	fmt.Fprintf(os.Stdout, "Revision ID:  %s\n", string(d.RevID))
	fmt.Fprintf(os.Stdout, "Plan Hash:    %s\n", rev.PlanHash)
	fmt.Fprintf(os.Stdout, "Trigger Key:  %s\n", trig.TriggerKey)
	fmt.Fprintf(os.Stdout, "Trigger Name: %s\n", trig.TriggerName)
	fmt.Fprintf(os.Stdout, "Source:       %s\n", source)
	if rev.SourceID != "" {
		fmt.Fprintf(os.Stdout, "Source Snapshot: %s\n", rev.SourceID)
	}
	if rev.CatalogID != "" {
		fmt.Fprintf(os.Stdout, "Catalog Snapshot: %s\n", rev.CatalogID)
	}
	fmt.Fprintf(os.Stdout, "Job Count:    %d\n", rev.JobCount)
	if rev.Scope.Mode != "" {
		fmt.Fprintf(os.Stdout, "Scope:        %s\n", rev.Scope.Mode)
	}
	return nil
}

// describeTrigger renders the trigger occurrence that produced a revision, from
// the object-model graph. ref shares describeRevision's grammar. The object-model
// TriggerOccurrence folds the legacy type/mode into Source.{Flavor,System}; the
// legacy provider/event/action fields are not recorded (a documented v1 gap).
func describeTrigger(ref string) error {
	if ref == "latest" {
		ref = ""
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	d, ok := objResolveRevisionDetail(ref)
	if !ok {
		return fmt.Errorf("revision not found for trigger: %s", ref)
	}
	if !d.HasTrigger {
		return fmt.Errorf("no trigger recorded for revision: %s", ref)
	}
	trig := d.Trigger

	if getOutputFormat == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(trig)
	}

	fmt.Fprintf(os.Stdout, "\n%s\n", ui.Bold(color, "Trigger: "+trig.TriggerKey))
	fmt.Fprintln(os.Stdout, strings.Repeat("─", 60))
	fmt.Fprintf(os.Stdout, "Trigger ID: %s\n", trig.TriggerID)
	fmt.Fprintf(os.Stdout, "Name:       %s\n", trig.TriggerName)
	fmt.Fprintf(os.Stdout, "Type:       %s\n", trig.Source.Flavor)
	if trig.Source.System != "" {
		fmt.Fprintf(os.Stdout, "Mode:       %s\n", trig.Source.System)
	}
	fmt.Fprintf(os.Stdout, "Actor:      %s\n", trig.Actor)
	fmt.Fprintf(os.Stdout, "Scope:      %s\n", trig.Scope.Mode)
	fmt.Fprintf(os.Stdout, "Revision:   %s\n", string(d.RevID))
	return nil
}
