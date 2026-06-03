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
	"github.com/sourceplane/orun/internal/revision"
	"github.com/sourceplane/orun/internal/state"
	"github.com/sourceplane/orun/internal/statestore"
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
		Use:   "revision [key]",
		Short: "Describe a revision (revisions/<key>/{revision,trigger,manifest}.json)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := ""
			if len(args) > 0 {
				ref = args[0]
			}
			return describeRevision(ref)
		},
	})

	describeCmd.AddCommand(&cobra.Command{
		Use:   "trigger [key]",
		Short: "Describe a trigger (revisions/<key>/trigger.json)",
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

	// M12 T3: read the execution from the object graph when active.
	if reader, ok := openObjectReader(); ok {
		r := ref
		if r == "" {
			r = "executions/latest"
		}
		if v, err := reader.Get(context.Background(), r); err == nil {
			return renderDescribeRun(v.ExecutionID, objview.ToMeta(v), objview.ToState(v), nil, color)
		}
	}

	store := state.NewStore(storeDir())

	// M5.c: route through the seven-branch resolver. On miss
	// (e.g. a workspace that hasn't materialized any revisions yet)
	// fall back to the legacy state.Store resolver so existing
	// `.orun/executions/<id>/` rows keep working.
	var execID string
	var rx *resolvedExec
	if r, err := resolveExecutionForRead(context.Background(), ref, ""); err == nil {
		rx = r
		execID = r.LegacyExecID
	} else {
		var err2 error
		execID, err2 = store.ResolveExecID(ref)
		if err2 != nil {
			return err2
		}
	}

	meta, _ := store.LoadMetadata(execID)
	st, _ := store.LoadState(execID)
	return renderDescribeRun(execID, meta, st, rx, color)
}

// renderDescribeRun prints the execution detail block, shared by the
// object-model and legacy paths.
func renderDescribeRun(execID string, meta *execmodel.ExecMetadata, st *execmodel.ExecState, rx *resolvedExec, color bool) error {

	fmt.Fprintf(os.Stdout, "\n%s\n", ui.Bold(color, "Execution: "+execID))
	fmt.Fprintln(os.Stdout, strings.Repeat("─", 60))

	if rx != nil {
		fmt.Fprintf(os.Stdout, "Revision Key: %s\n", rx.RevisionKey)
		fmt.Fprintf(os.Stdout, "Execution Key: %s\n", rx.ExecutionKey)
		fmt.Fprintf(os.Stdout, "Legacy Exec ID: %s\n", rx.LegacyExecID)
		fmt.Fprintf(os.Stdout, "Source: %s\n", string(rx.Source))
	}

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

	// M12 T3: resolve the plan from the object-model revision graph when active.
	plan, ok := objResolvePlan(ref)
	if !ok {
		store := state.NewStore(storeDir())
		path, err := store.ResolvePlanRef(ref)
		if err != nil {
			return err
		}
		plan, err = loadPlan(path)
		if err != nil {
			return err
		}
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

	// Load latest plan to find job details (object model first).
	plan, ok := objResolvePlan("latest")
	if !ok {
		store := state.NewStore(storeDir())
		path, err := store.ResolvePlanRef("latest")
		if err != nil {
			return fmt.Errorf("no plan found: %w", err)
		}
		plan, err = loadPlan(path)
		if err != nil {
			return err
		}
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

	// Show state from latest execution if available (object model first).
	var execID string
	var st *execmodel.ExecState
	if reader, ok := openObjectReader(); ok {
		if v, err := reader.Get(context.Background(), "executions/latest"); err == nil {
			execID = v.ExecutionID
			st = objview.ToState(v)
		}
	}
	if st == nil {
		store := state.NewStore(storeDir())
		if id, resolveErr := store.ResolveExecID("latest"); resolveErr == nil {
			execID = id
			st, _ = store.LoadState(id)
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

// describeRevision renders the revision document and its embedded
// trigger summary. ref="" resolves the latest revision; the literal
// "latest" is normalized to "" so users can type either form (Option A
// from ai/proposals/task-0019-spec-update.md — CLI-side normalization,
// no resolver change).
func describeRevision(ref string) error {
	if ref == "latest" {
		ref = ""
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	store, _, err := openLocalStateStore()
	if err != nil {
		return err
	}
	revRef, err := revision.ResolveRevision(context.Background(), store, ref, revision.ResolveOptions{})
	if err != nil {
		return fmt.Errorf("resolve revision: %w", err)
	}
	rev := revRef.Revision
	trig := revRef.Trigger
	revKey := rev.RevisionKey

	if getOutputFormat == "json" {
		out := map[string]interface{}{
			"revisionKey": revKey,
			"revision":    rev,
			"trigger":     trig,
			"source":      string(revRef.Source),
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	fmt.Fprintf(os.Stdout, "\n%s\n", ui.Bold(color, "Revision: "+revKey))
	fmt.Fprintln(os.Stdout, strings.Repeat("─", 60))
	fmt.Fprintf(os.Stdout, "Revision ID:  %s\n", rev.RevisionID)
	fmt.Fprintf(os.Stdout, "Plan Hash:    %s\n", rev.PlanHash)
	fmt.Fprintf(os.Stdout, "Trigger Key:  %s\n", trig.TriggerKey)
	fmt.Fprintf(os.Stdout, "Trigger Name: %s\n", trig.TriggerName)
	fmt.Fprintf(os.Stdout, "Source:       %s\n", string(revRef.Source))
	if revRef.NamedRefName != "" {
		fmt.Fprintf(os.Stdout, "Named Ref:    %s\n", revRef.NamedRefName)
	}

	if rev.SourceSnapshotKey != "" {
		fmt.Fprintf(os.Stdout, "Source Snapshot: %s\n", rev.SourceSnapshotKey)
	}
	if rev.CatalogSnapshotKey != "" {
		fmt.Fprintf(os.Stdout, "Catalog Snapshot: %s\n", rev.CatalogSnapshotKey)
	}

	// Latest execution under this revision (manifest summary).
	manifestPath := statestore.ManifestPath(revKey)
	if raw, _, mErr := store.Read(context.Background(), manifestPath); mErr == nil {
		var manifest struct {
			Summary struct {
				JobCount              int      `json:"jobCount"`
				ActiveEnvironments    []string `json:"activeEnvironments"`
				LatestExecutionKey    string   `json:"latestExecutionKey,omitempty"`
				LatestExecutionStatus string   `json:"latestExecutionStatus,omitempty"`
			} `json:"summary"`
		}
		if jErr := json.Unmarshal(raw, &manifest); jErr == nil {
			fmt.Fprintf(os.Stdout, "Job Count:    %d\n", manifest.Summary.JobCount)
			if len(manifest.Summary.ActiveEnvironments) > 0 {
				fmt.Fprintf(os.Stdout, "Environments: %s\n", strings.Join(manifest.Summary.ActiveEnvironments, ", "))
			}
			if manifest.Summary.LatestExecutionKey != "" {
				fmt.Fprintf(os.Stdout, "Latest Exec:  %s (%s)\n",
					manifest.Summary.LatestExecutionKey, manifest.Summary.LatestExecutionStatus)
			}
		}
	}
	return nil
}

// describeTrigger renders trigger.json under a revision. ref="" resolves
// the latest revision and shows its trigger; the literal "latest" is
// normalized to "" for consistency with describeRevision (Option A).
func describeTrigger(ref string) error {
	if ref == "latest" {
		ref = ""
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	store, _, err := openLocalStateStore()
	if err != nil {
		return err
	}
	revRef, err := revision.ResolveRevision(context.Background(), store, ref, revision.ResolveOptions{})
	if err != nil {
		return fmt.Errorf("resolve revision for trigger: %w", err)
	}
	trig := revRef.Trigger

	if getOutputFormat == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(trig)
	}

	fmt.Fprintf(os.Stdout, "\n%s\n", ui.Bold(color, "Trigger: "+trig.TriggerKey))
	fmt.Fprintln(os.Stdout, strings.Repeat("─", 60))
	fmt.Fprintf(os.Stdout, "Trigger ID: %s\n", trig.TriggerID)
	fmt.Fprintf(os.Stdout, "Name:       %s\n", trig.TriggerName)
	fmt.Fprintf(os.Stdout, "Type:       %s\n", trig.TriggerType)
	fmt.Fprintf(os.Stdout, "Mode:       %s\n", trig.Mode)
	fmt.Fprintf(os.Stdout, "Provider:   %s\n", trig.Provider)
	fmt.Fprintf(os.Stdout, "Event:      %s\n", trig.Event)
	if trig.Action != "" {
		fmt.Fprintf(os.Stdout, "Action:     %s\n", trig.Action)
	}
	fmt.Fprintf(os.Stdout, "Scope:      %s\n", trig.PlanScope.Mode)
	fmt.Fprintf(os.Stdout, "Revision:   %s\n", revRef.Revision.RevisionKey)
	return nil
}
