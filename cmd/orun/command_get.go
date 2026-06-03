package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
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

var (
	getOutputFormat string
	getPlanRef      string
	getViewMode     string
)

var getCmd = &cobra.Command{
	Use:   "get <resource>",
	Short: "List resources (plans, runs, jobs, components, environments)",
	Long:  "kubectl-style resource listing. Usage: orun get plans, orun get runs, orun get jobs",
}

func registerGetCommand(root *cobra.Command) {
	root.AddCommand(getCmd)

	getCmd.PersistentFlags().StringVarP(&getOutputFormat, "output", "o", "", "Output format: json, yaml, wide")
	getCmd.PersistentFlags().StringVar(&getPlanRef, "plan", "", "Plan reference for job listing")
	getCmd.PersistentFlags().StringVar(&getViewMode, "view", "", "View mode: tree, compact, table")

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
	// M5.c: Prefer the revision-first table when revisions/ has data.
	// Fall back to the legacy plan-hash table when only `.orun/plans/`
	// exists.
	if rows, ok, err := loadRevisionPlanRows(); err != nil {
		return err
	} else if ok {
		return renderRevisionPlanTable(rows)
	}
	// M12 T3: object-model revisions (plans live in the revision graph).
	if rows, ok := objListPlanRows(); ok {
		return renderPlanEntryTable(rows)
	}
	return renderLegacyPlanTable()
}

// revisionPlanRow is the projected row used by `orun get plans` after the
// state-redesign rewire. One row per revision key.
type revisionPlanRow struct {
	RevisionKey  string   `json:"revisionKey"`
	TriggerName  string   `json:"trigger"`
	PlanHash     string   `json:"plan"`
	JobCount     int      `json:"jobs"`
	LatestExec   string   `json:"latestExec,omitempty"`
	LatestStatus string   `json:"status,omitempty"`
	Environments []string `json:"environments,omitempty"`
}

// loadRevisionPlanRows scans revisions/<key>/manifest.json and projects each
// into a revisionPlanRow. ok=false signals the new layout is empty (no
// revisions yet) so the caller can fall back to the legacy table.
func loadRevisionPlanRows() ([]revisionPlanRow, bool, error) {
	store, _, err := openLocalStateStore()
	if err != nil {
		// Treat any open-failure as "no new layout" — the legacy
		// fallback will still try the on-disk plans/ dir.
		return nil, false, nil
	}
	ctx := context.Background()
	infos, err := store.List(ctx, "revisions")
	if err != nil || len(infos) == 0 {
		return nil, false, nil
	}
	keys := map[string]struct{}{}
	for _, info := range infos {
		// Path looks like revisions/<key>/...
		parts := strings.SplitN(info.Path, "/", 3)
		if len(parts) < 2 || parts[0] != "revisions" {
			continue
		}
		if parts[1] == "" {
			continue
		}
		keys[parts[1]] = struct{}{}
	}
	if len(keys) == 0 {
		return nil, false, nil
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	rows := make([]revisionPlanRow, 0, len(sorted))
	for _, revKey := range sorted {
		row := revisionPlanRow{RevisionKey: revKey}
		// Pull job count + latest exec via the manifest summary.
		mPath := statestore.ManifestPath(revKey)
		if raw, _, mErr := store.Read(ctx, mPath); mErr == nil {
			var manifest revision.RevisionManifest
			if jErr := json.Unmarshal(raw, &manifest); jErr == nil {
				row.JobCount = manifest.Summary.JobCount
				row.Environments = manifest.Summary.ActiveEnvironments
				row.LatestExec = manifest.Summary.LatestExecutionKey
				row.LatestStatus = manifest.Summary.LatestExecutionStatus
				row.TriggerName = manifest.Trigger.Name
				row.PlanHash = shortHash(manifest.Revision.PlanHash)
			}
		}
		// If manifest didn't supply a trigger name, fall back to
		// trigger.json on the revision dir.
		if row.TriggerName == "" {
			tPath := statestore.TriggerPath(revKey)
			if raw, _, tErr := store.Read(ctx, tPath); tErr == nil {
				var trig struct {
					Name string `json:"name"`
				}
				_ = json.Unmarshal(raw, &trig)
				row.TriggerName = trig.Name
			}
		}
		rows = append(rows, row)
	}
	return rows, true, nil
}

// shortHash returns the first 7 chars of a hex hash, stripping a "sha256-"
// prefix if present. Used to mirror the existing plan-id formatting.
func shortHash(h string) string {
	h = strings.TrimPrefix(h, "sha256-")
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

func renderRevisionPlanTable(rows []revisionPlanRow) error {
	if getOutputFormat == "json" {
		out := map[string]interface{}{
			"revisions": rows,
		}
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	}

	color := ui.ColorEnabledForWriter(os.Stdout)
	fmt.Fprintf(os.Stdout, "%s  %s  %s  %s  %s  %s\n",
		padRight(ui.Bold(color, "REVISION"), 38),
		padRight(ui.Bold(color, "TRIGGER"), 24),
		padRight(ui.Bold(color, "PLAN"), 8),
		padRight(ui.Bold(color, "JOBS"), 5),
		padRight(ui.Bold(color, "LATEST EXEC"), 16),
		ui.Bold(color, "STATUS"))

	for _, row := range rows {
		latestExec := row.LatestExec
		if latestExec == "" {
			latestExec = "—"
		}
		status := row.LatestStatus
		if status == "" {
			status = "—"
		}
		trigger := row.TriggerName
		if trigger == "" {
			trigger = "—"
		}
		planHash := row.PlanHash
		if planHash == "" {
			planHash = "—"
		}
		fmt.Fprintf(os.Stdout, "%-38s  %-24s  %-8s  %-5d  %-16s  %s\n",
			row.RevisionKey, trigger, planHash, row.JobCount, latestExec, status)
	}

	fmt.Println()
	count := len(rows)
	noun := "revision"
	if count != 1 {
		noun = "revisions"
	}
	fmt.Println(ui.Dim(color, fmt.Sprintf("%d %s", count, noun)))
	return nil
}

func renderLegacyPlanTable() error {
	store := state.NewStore(storeDir())
	plans, err := store.ListPlans()
	if err != nil {
		return err
	}
	return renderPlanEntryTable(plans)
}

// renderPlanEntryTable renders a plan listing (legacy store or object-model
// revisions) as the `orun get plans` table / JSON.
func renderPlanEntryTable(plans []execmodel.PlanEntry) error {
	if len(plans) == 0 {
		if getOutputFormat == "json" {
			fmt.Fprintln(os.Stdout, "[]")
			return nil
		}
		color := ui.ColorEnabledForWriter(os.Stdout)
		fmt.Println(ui.Dim(color, "No plans yet."))
		fmt.Println()
		fmt.Printf("  Generate one with:  %s\n", ui.Bold(color, "orun plan"))
		return nil
	}

	if getOutputFormat == "json" {
		data, _ := json.MarshalIndent(plans, "", "  ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	}

	color := ui.ColorEnabledForWriter(os.Stdout)

	fmt.Fprintf(os.Stdout, "%s  %s  %s  %s\n",
		padRight(ui.Bold(color, "REVISION"), 14),
		padRight(ui.Bold(color, "JOBS"), 6),
		padRight(ui.Bold(color, "AGE"), 8),
		ui.Bold(color, "STATUS"))

	var latestChecksum string
	for _, p := range plans {
		if p.Name == "latest" {
			latestChecksum = p.Checksum
		}
	}

	for _, p := range plans {
		age := formatAge(p.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
		name := p.Name
		if name == "latest" {
			name = ui.Bold(color, "latest")
		}
		status := styleOK("● ready")
		fmt.Fprintf(os.Stdout, "%-14s  %-6d  %-8s  %s\n",
			name, p.Jobs, age, status)
	}

	fmt.Println()
	count := len(plans)
	summary := fmt.Sprintf("%d revision", count)
	if count != 1 {
		summary += "s"
	}
	if latestChecksum != "" {
		summary += fmt.Sprintf(" · latest: %s", latestChecksum)
	}
	fmt.Println(ui.Dim(color, summary))

	return nil
}

func getJobs() error {
	store := state.NewStore(storeDir())

	ref := getPlanRef
	if ref == "" {
		ref = "latest"
	}

	// M12 T3: resolve the plan from the object-model revision graph when active;
	// fall back to the legacy plan store.
	var plan *model.Plan
	if p, ok := objResolvePlan(ref); ok {
		plan = p
	} else {
		path, err := store.ResolvePlanRef(ref)
		if err != nil {
			color := ui.ColorEnabledForWriter(os.Stdout)
			fmt.Println(ui.Dim(color, "No jobs found."))
			fmt.Println()
			fmt.Printf("  Generate a plan first:  %s\n", ui.Bold(color, "orun plan"))
			return nil
		}
		plan, err = loadPlan(path)
		if err != nil {
			return err
		}
	}

	// Context-aware scoping: filter jobs by detected component
	if !allFlag && intentRoot != "" {
		if scopeIntent, _, loadErr := loadResolvedIntentFile(intentFile); loadErr == nil {
			scope, _ := ResolveScope(scopeIntent, nil, allFlag, getOutputFormat == "json")
			if scope != nil && scope.WasAutoScoped {
				scopeSet := make(map[string]bool, len(scope.ScopedComponents))
				for _, c := range scope.ScopedComponents {
					scopeSet[c] = true
				}
				var filteredJobs []model.PlanJob
				for _, job := range plan.Jobs {
					if scopeSet[job.Component] {
						filteredJobs = append(filteredJobs, job)
					}
				}
				plan.Jobs = filteredJobs
			}
		}
	}

	// M12 T3: overlay execution status from the object graph when active.
	var execState *execmodel.ExecState
	if reader, ok := openObjectReader(); ok {
		if v, err := reader.Get(context.Background(), "executions/latest"); err == nil {
			execState = objview.ToState(v)
		}
	}
	if execState == nil {
		if execID, resolveErr := store.ResolveExecID("latest"); resolveErr == nil {
			execState, _ = store.LoadState(execID)
		}
	}

	if getOutputFormat == "json" {
		data, _ := json.MarshalIndent(plan.Jobs, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	color := ui.ColorEnabledForWriter(os.Stdout)
	view := resolveViewMode(getViewMode, getOutputFormat)

	switch view {
	case "table":
		getJobsTable(plan, execState, color)
	case "compact":
		getJobsCompact(plan, execState, color)
	default:
		getJobsTree(plan, execState, color)
	}

	return nil
}

func resolveViewMode(viewFlag, outputFlag string) string {
	if viewFlag != "" {
		return viewFlag
	}
	if outputFlag == "wide" {
		return "table"
	}
	return "tree"
}

func getJobsTree(plan *model.Plan, execState *state.ExecState, color bool) {
	checksum := ""
	if plan.Metadata.Checksum != "" {
		cs := strings.TrimPrefix(plan.Metadata.Checksum, "sha256-")
		if len(cs) > 7 {
			cs = cs[:7]
		}
		checksum = cs
	}

	header := fmt.Sprintf("PLAN: %s", ui.Bold(color, plan.Metadata.Name))
	if checksum != "" {
		header += fmt.Sprintf(" (%s)", ui.Dim(color, checksum))
	}
	header += fmt.Sprintf(" · %d jobs", len(plan.Jobs))
	fmt.Println(header)
	fmt.Println()

	type jobEntry struct {
		job    model.PlanJob
		status string
	}

	componentMap := make(map[string]map[string][]jobEntry)
	compositionMap := make(map[string]string)
	for _, job := range plan.Jobs {
		if componentMap[job.Component] == nil {
			componentMap[job.Component] = make(map[string][]jobEntry)
		}
		status := "pending"
		if execState != nil {
			if js, ok := execState.Jobs[job.ID]; ok && js != nil {
				status = js.Status
			}
		}
		componentMap[job.Component][job.Environment] = append(
			componentMap[job.Component][job.Environment],
			jobEntry{job: job, status: status},
		)
		if job.Composition != "" {
			compositionMap[job.Component] = job.Composition
		}
	}

	components := sortedKeys(componentMap)
	for _, comp := range components {
		fmt.Println(ui.Bold(color, comp))
		envMap := componentMap[comp]
		envs := sortedKeys(envMap)
		for _, env := range envs {
			entries := envMap[env]
			fmt.Printf("  %s\n", ui.Cyan(color, env))
			for _, entry := range entries {
				icon := styleStatus(entry.status, color)
				displayName := shortenJobName(entry.job.Name, compositionMap[comp])
				statusText := ui.Dim(color, entry.status)
				if entry.status != "pending" {
					statusText = styleStatusText(entry.status, color)
				}
				fmt.Printf("    %s %-30s %s\n", icon, displayName, statusText)
			}
		}
		fmt.Println()
	}
}

func getJobsCompact(plan *model.Plan, execState *state.ExecState, color bool) {
	compositionMap := make(map[string]string)
	for _, job := range plan.Jobs {
		if job.Composition != "" {
			compositionMap[job.Component] = job.Composition
		}
	}

	for _, job := range plan.Jobs {
		status := "pending"
		if execState != nil {
			if js, ok := execState.Jobs[job.ID]; ok && js != nil {
				status = js.Status
			}
		}
		icon := styleStatus(status, color)
		displayName := shortenJobName(job.Name, compositionMap[job.Component])
		fmt.Fprintf(os.Stdout, "%s  %-24s %-12s %s\n",
			icon, job.Component, job.Environment, displayName)
	}
}

func getJobsTable(plan *model.Plan, execState *state.ExecState, color bool) {
	fmt.Fprintf(os.Stdout, "%-50s %-18s %-14s %s\n",
		"JOB ID", "COMPONENT", "ENV", "STATUS")

	for _, job := range plan.Jobs {
		status := "pending"
		if execState != nil {
			if js, ok := execState.Jobs[job.ID]; ok && js != nil {
				status = js.Status
			}
		}
		icon := styleStatus(status, color)
		fmt.Fprintf(os.Stdout, "%s %-48s %-18s %-14s %s\n",
			icon, job.ID, job.Component, job.Environment, status)
	}
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

	envNames := make([]string, 0, len(intent.Environments))
	for name := range intent.Environments {
		envNames = append(envNames, name)
	}
	sort.Strings(envNames)

	fmt.Printf("%s  %d\n\n", ui.Bold(color, "ENVIRONMENTS"), len(envNames))

	for _, name := range envNames {
		env := intent.Environments[name]
		policies := []string{}
		for k, v := range env.Policies {
			policies = append(policies, fmt.Sprintf("%s=%v", k, v))
		}

		defaults := []string{}
		for typeName, params := range env.ParameterDefaults {
			for k, v := range params {
				defaults = append(defaults, fmt.Sprintf("%s.%s=%v", typeName, k, v))
			}
		}

		meta := []string{}
		if policyStr := strings.Join(policies, " "); policyStr != "" {
			meta = append(meta, policyStr)
		}
		if defaultStr := strings.Join(defaults, " "); defaultStr != "" {
			meta = append(meta, defaultStr)
		}

		if len(meta) > 0 {
			fmt.Fprintf(os.Stdout, "%-14s %s\n", ui.Bold(color, name), ui.Dim(color, strings.Join(meta, "  ")))
		} else {
			fmt.Fprintf(os.Stdout, "%s\n", ui.Bold(color, name))
		}
	}

	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
