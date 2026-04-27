package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/gluon/internal/state"
	"github.com/sourceplane/gluon/internal/ui"
	"github.com/spf13/cobra"
)

var (
	statusExecID   string
	statusAll      bool
	statusDetailed bool
	statusJSON     bool
)

type executionCounts struct {
	total     int
	completed int
	failed    int
	running   int
	pending   int
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check a run",
	Long:  "Show the current state of a run. Defaults to the latest run.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return showStatus()
	},
}

func registerStatusCommand(root *cobra.Command) {
	root.AddCommand(statusCmd)

	statusCmd.Flags().StringVar(&statusExecID, "exec-id", "", "Show a specific execution")
	statusCmd.Flags().BoolVar(&statusAll, "all", false, "Show all executions")
	statusCmd.Flags().BoolVar(&statusDetailed, "detailed", false, "Show step-level detail")
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "Output in JSON format")
}

func showStatus() error {
	store := state.NewStore(storeDir())
	color := ui.ColorEnabledForWriter(os.Stdout)

	if statusAll {
		return showAllExecutions(store, color)
	}

	execID := statusExecID
	if execID == "" {
		var err error
		execID, err = store.ResolveExecID("latest")
		if err != nil {
			fmt.Println(ui.Dim(color, "No runs yet."))
			fmt.Println()
			fmt.Printf("  Start one with: %s\n", ui.Bold(color, "gluon run"))
			return nil
		}
	} else {
		var err error
		execID, err = store.ResolveExecID(execID)
		if err != nil {
			return err
		}
	}

	return showExecution(store, execID, color)
}

func showAllExecutions(store *state.Store, color bool) error {
	execs, err := store.ListExecutions()
	if err != nil {
		return err
	}
	if len(execs) == 0 {
		fmt.Println(ui.Dim(color, "No runs yet."))
		fmt.Println()
		fmt.Printf("  Start one with: %s\n", ui.Bold(color, "gluon run"))
		return nil
	}

	sort.SliceStable(execs, func(i, j int) bool {
		iRunning := strings.ToLower(execs[i].Status) == "running"
		jRunning := strings.ToLower(execs[j].Status) == "running"
		if iRunning != jRunning {
			return iRunning
		}
		return execs[i].StartedAt > execs[j].StartedAt
	})

	fmt.Fprintf(os.Stdout, "%s  %s  %s  %s  %s\n",
		padRight(ui.Bold(color, "RUN"), 38),
		padRight(ui.Bold(color, "STATE"), 10),
		padRight(ui.Bold(color, "PLAN"), 22),
		padRight(ui.Bold(color, "RESULT"), 20),
		ui.Bold(color, "AGE"))

	for _, exec := range execs {
		icon := styleStatus(exec.Status, color)
		result := formatExecutionCounts(executionCounts{
			total:     exec.JobTotal,
			completed: exec.JobDone,
			failed:    exec.JobFailed,
		})
		fmt.Fprintf(os.Stdout, "%s %-37s %-10s %-22s %-20s %s\n",
			icon,
			exec.ID,
			statusLabel(exec.Status),
			trimDisplay(exec.PlanName, 22),
			trimDisplay(result, 20),
			formatAge(exec.StartedAt),
		)
	}

	return nil
}

func showExecution(store *state.Store, execID string, color bool) error {
	meta, _ := store.LoadMetadata(execID)
	st, _ := store.LoadState(execID)
	counts := executionCountsFromState(meta, st)

	status := "unknown"
	if meta != nil && strings.TrimSpace(meta.Status) != "" {
		status = meta.Status
	}
	duration := ""
	if meta != nil {
		duration = formatDuration(meta.StartedAt, meta.FinishedAt)
	}

	headerParts := []string{ui.Bold(color, execID)}
	if status != "" {
		headerParts = append(headerParts, fmt.Sprintf("%s %s", styleStatus(status, color), styleStatusText(statusLabel(status), color)))
	}
	if summary := formatExecutionCounts(counts); summary != "" {
		headerParts = append(headerParts, summary)
	}
	if duration != "" {
		headerParts = append(headerParts, duration)
	}
	fmt.Println(strings.Join(headerParts, "  "))

	if meta != nil {
		planValue := strings.TrimSpace(meta.PlanName)
		if planValue == "" {
			planValue = "plan"
		}
		if strings.TrimSpace(meta.PlanID) != "" {
			planValue = planValue + " · " + meta.PlanID
		}
		fmt.Fprintf(os.Stdout, "%-12s %s\n", ui.Dim(color, "Plan"), planValue)
		for _, link := range meta.Links {
			fmt.Fprintf(os.Stdout, "%-12s %s\n", displayLinkLabel(link.Label, color), link.URL)
		}
	}
	fmt.Fprintf(os.Stdout, "%-12s gluon logs --exec-id %s\n", ui.Dim(color, "Logs"), execID)

	type jobDisplay struct {
		id     string
		status string
		err    string
		dur    string
		steps  map[string]string
	}

	var jobs []jobDisplay
	if st != nil {
		for jobID, js := range st.Jobs {
			if js == nil {
				continue
			}
			duration := ""
			if js.StartedAt != "" && js.FinishedAt != "" {
				duration = formatDuration(js.StartedAt, js.FinishedAt)
			}
			jobs = append(jobs, jobDisplay{
				id:     jobID,
				status: js.Status,
				err:    js.LastError,
				dur:    duration,
				steps:  js.Steps,
			})
		}
	}

	if len(jobs) == 0 {
		return nil
	}

	sort.Slice(jobs, func(i, j int) bool {
		oi := statusSortKey(jobs[i].status)
		oj := statusSortKey(jobs[j].status)
		if oi != oj {
			return oi < oj
		}
		return jobs[i].id < jobs[j].id
	})

	fmt.Println()
	for _, job := range jobs {
		icon := styleStatus(job.status, color)
		line := fmt.Sprintf("%s %-24s", icon, job.id)
		if job.dur != "" {
			line += " " + padRight(ui.Dim(color, job.dur), 6)
		}
		if job.err != "" {
			line += "   " + ui.Red(color, job.err)
		}
		fmt.Println(line)
		if statusDetailed {
			stepIDs := make([]string, 0, len(job.steps))
			for stepID := range job.steps {
				stepIDs = append(stepIDs, stepID)
			}
			sort.Strings(stepIDs)
			for _, stepID := range stepIDs {
				fmt.Printf("  %s %s\n", styleStatus(job.steps[stepID], color), stepID)
			}
		}
	}

	return nil
}

func executionCountsFromState(meta *state.ExecMetadata, st *state.ExecState) executionCounts {
	if counts := state.SummarizeExecutionState(st); counts.Total > 0 {
		return executionCounts{
			total:     counts.Total,
			completed: counts.Completed,
			failed:    counts.Failed,
			running:   counts.Running,
			pending:   counts.Pending,
		}
	}
	if meta == nil {
		return executionCounts{}
	}
	return executionCounts{
		total:     meta.JobTotal,
		completed: meta.JobDone,
		failed:    meta.JobFailed,
	}
}

func formatExecutionCounts(counts executionCounts) string {
	parts := make([]string, 0, 3)
	if counts.completed > 0 {
		parts = append(parts, fmt.Sprintf("%d succeeded", counts.completed))
	}
	if counts.failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", counts.failed))
	}
	if counts.running > 0 {
		parts = append(parts, fmt.Sprintf("%d running", counts.running))
	}
	if counts.pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", counts.pending))
	}
	if len(parts) == 0 && counts.total > 0 {
		parts = append(parts, fmt.Sprintf("%d task%s", counts.total, plural(counts.total)))
	}
	return strings.Join(parts, " · ")
}

func statusSortKey(status string) int {
	switch strings.ToLower(status) {
	case "failed":
		return 0
	case "running":
		return 1
	case "completed":
		return 2
	case "pending":
		return 3
	default:
		return 4
	}
}

func statusLabel(status string) string {
	trimmed := strings.TrimSpace(strings.ToLower(status))
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

func trimDisplay(value string, width int) string {
	if len(value) <= width {
		return value
	}
	if width <= 1 {
		return value[:width]
	}
	return value[:width-1] + "…"
}

func displayLinkLabel(label string, color bool) string {
	trimmed := strings.TrimSpace(label)
	if trimmed == "" {
		trimmed = "Link"
	}
	return ui.Dim(color, trimmed)
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func formatDuration(start, end string) string {
	if start == "" {
		return ""
	}
	s, err := time.Parse(time.RFC3339, start)
	if err != nil {
		return ""
	}
	var e time.Time
	if end != "" {
		e, err = time.Parse(time.RFC3339, end)
		if err != nil {
			e = time.Now()
		}
	} else {
		e = time.Now()
	}

	d := e.Sub(s)
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

func formatAge(startedAt string) string {
	if startedAt == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return ""
	}
	d := time.Since(t)
	if d < time.Minute {
		return "now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
