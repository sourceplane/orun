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

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show execution status",
	Long:  "Show the status of executions. By default shows the latest execution.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return showStatus()
	},
}

func registerStatusCommand(root *cobra.Command) {
	root.AddCommand(statusCmd)

	statusCmd.Flags().StringVar(&statusExecID, "exec-id", "", "Show specific execution")
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
			fmt.Println(ui.Dim(color, "No executions yet."))
			fmt.Println()
			fmt.Printf("  Run a plan with:  %s\n", ui.Bold(color, "gluon run"))
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
		fmt.Println(ui.Dim(color, "No executions yet."))
		fmt.Println()
		fmt.Printf("  Run a plan with:  %s\n", ui.Bold(color, "gluon run"))
		return nil
	}

	// Sort running executions first
	sort.SliceStable(execs, func(i, j int) bool {
		iRunning := strings.ToLower(execs[i].Status) == "running"
		jRunning := strings.ToLower(execs[j].Status) == "running"
		if iRunning != jRunning {
			return iRunning
		}
		return false
	})

	fmt.Fprintf(os.Stdout, "%s  %s  %s  %s  %s  %s\n",
		padRight(ui.Bold(color, "EXECUTION"), 38),
		padRight(ui.Bold(color, "STATUS"), 12),
		padRight(ui.Bold(color, "PLAN"), 20),
		padRight(ui.Bold(color, "JOBS"), 8),
		padRight(ui.Bold(color, "DURATION"), 12),
		ui.Bold(color, "AGE"))

	for _, exec := range execs {
		icon := styleStatus(exec.Status, color)
		jobs := fmt.Sprintf("%d/%d", exec.JobDone, exec.JobTotal)
		duration := formatDuration(exec.StartedAt, exec.FinishedAt)
		age := formatAge(exec.StartedAt)

		fmt.Fprintf(os.Stdout, "%s %-37s %-12s %-20s %-8s %-12s %s\n",
			icon, exec.ID, exec.Status, exec.PlanName, jobs, duration, age)
	}

	return nil
}

func showExecution(store *state.Store, execID string, color bool) error {
	meta, _ := store.LoadMetadata(execID)
	st, _ := store.LoadState(execID)

	// Compact execution header
	headerParts := []string{
		fmt.Sprintf("EXECUTION %s", ui.Bold(color, execID)),
	}
	if meta != nil {
		headerParts = append(headerParts,
			fmt.Sprintf("%s %s", styleStatus(meta.Status, color), styleStatusText(meta.Status, color)),
			fmt.Sprintf("%d/%d jobs", meta.JobDone, meta.JobTotal),
		)
		if meta.FinishedAt != "" {
			headerParts = append(headerParts, formatDuration(meta.StartedAt, meta.FinishedAt))
		} else if meta.StartedAt != "" {
			headerParts = append(headerParts, formatAge(meta.StartedAt))
		}
	}
	fmt.Println(strings.Join(headerParts, "  "))

	if meta != nil && meta.PlanName != "" {
		planLine := fmt.Sprintf("Plan: %s", meta.PlanName)
		if meta.PlanID != "" {
			planLine += fmt.Sprintf("  %s", ui.Dim(color, "sha256:"+meta.PlanID))
		}
		fmt.Println(ui.Dim(color, planLine))
	}

	if meta != nil && meta.JobFailed > 0 {
		fmt.Printf("%s  ✓ %d  ✗ %d\n",
			ui.Dim(color, "Jobs:"),
			meta.JobDone-meta.JobFailed,
			meta.JobFailed)
	}

	if st != nil {
		fmt.Println()

		type jobDisplay struct {
			id     string
			status string
			err    string
			dur    string
			steps  map[string]string
		}

		var jobs []jobDisplay
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

		// Sort: running first, then failed, then completed, then pending
		statusOrder := map[string]int{"running": 0, "failed": 1, "completed": 2, "pending": 3}
		sort.Slice(jobs, func(i, j int) bool {
			oi, ok := statusOrder[strings.ToLower(jobs[i].status)]
			if !ok {
				oi = 4
			}
			oj, ok := statusOrder[strings.ToLower(jobs[j].status)]
			if !ok {
				oj = 4
			}
			if oi != oj {
				return oi < oj
			}
			return jobs[i].id < jobs[j].id
		})

		for _, job := range jobs {
			icon := styleStatus(job.status, color)
			durPart := ""
			if job.dur != "" {
				durPart = "  " + ui.Dim(color, job.dur)
			}
			fmt.Fprintf(os.Stdout, "  %s %-50s%s\n", icon, job.id, durPart)
			if job.err != "" {
				fmt.Fprintf(os.Stdout, "    %s %s\n", ui.Dim(color, "↳"), ui.Red(color, job.err))
			}

			if statusDetailed {
				for stepID, stepStatus := range job.steps {
					stepIcon := styleStatus(stepStatus, color)
					fmt.Fprintf(os.Stdout, "      %s %s\n", stepIcon, stepID)
				}
			}
		}
	}

	return nil
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
