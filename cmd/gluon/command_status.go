package main

import (
	"fmt"
	"os"
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
	store := state.NewStore(".")
	color := ui.ColorEnabledForWriter(os.Stdout)

	if statusAll {
		return showAllExecutions(store, color)
	}

	execID := statusExecID
	if execID == "" {
		var err error
		execID, err = store.ResolveExecID("latest")
		if err != nil {
			fmt.Println("No executions found.")
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
		fmt.Println("No executions found.")
		return nil
	}

	fmt.Fprintf(os.Stdout, "%-40s %-12s %-20s %-8s %-12s %s\n",
		"EXECUTION", "STATUS", "PLAN", "JOBS", "DURATION", "AGE")

	for _, exec := range execs {
		statusIcon := statusSymbol(exec.Status, color)
		jobs := fmt.Sprintf("%d/%d", exec.JobDone, exec.JobTotal)
		duration := formatDuration(exec.StartedAt, exec.FinishedAt)
		age := formatAge(exec.StartedAt)

		fmt.Fprintf(os.Stdout, "%s %-38s %-12s %-20s %-8s %-12s %s\n",
			statusIcon, exec.ID, exec.Status, exec.PlanName, jobs, duration, age)
	}

	return nil
}

func showExecution(store *state.Store, execID string, color bool) error {
	meta, _ := store.LoadMetadata(execID)
	st, _ := store.LoadState(execID)

	fmt.Fprintf(os.Stdout, "Execution  %s\n", ui.Bold(color, execID))

	if meta != nil {
		if meta.PlanName != "" {
			fmt.Fprintf(os.Stdout, "Plan       %s", meta.PlanName)
			if meta.PlanID != "" {
				fmt.Fprintf(os.Stdout, "  %s", ui.Dim(color, "sha256:"+meta.PlanID))
			}
			fmt.Fprintln(os.Stdout)
		}
		fmt.Fprintf(os.Stdout, "Status     %s %s\n", statusSymbol(meta.Status, color), meta.Status)
		fmt.Fprintf(os.Stdout, "Started    %s\n", meta.StartedAt)
		if meta.FinishedAt != "" {
			fmt.Fprintf(os.Stdout, "Duration   %s\n", formatDuration(meta.StartedAt, meta.FinishedAt))
		}
		fmt.Fprintf(os.Stdout, "Jobs       %d/%d  ✓ %d  ✗ %d\n", meta.JobDone, meta.JobTotal, meta.JobDone, meta.JobFailed)
	}

	if st != nil && (statusDetailed || meta == nil) {
		fmt.Fprintln(os.Stdout)
		for jobID, js := range st.Jobs {
			if js == nil {
				continue
			}
			icon := statusSymbol(js.Status, color)
			duration := ""
			if js.StartedAt != "" && js.FinishedAt != "" {
				duration = formatDuration(js.StartedAt, js.FinishedAt)
			}
			fmt.Fprintf(os.Stdout, "  %s %-50s %s\n", icon, jobID, duration)
			if js.LastError != "" {
				fmt.Fprintf(os.Stdout, "    %s %s\n", ui.Dim(color, "↳"), ui.Red(color, js.LastError))
			}

			if statusDetailed {
				for stepID, stepStatus := range js.Steps {
					stepIcon := statusSymbol(stepStatus, color)
					fmt.Fprintf(os.Stdout, "      %s %s\n", stepIcon, stepID)
				}
			}
		}
	}

	return nil
}

func statusSymbol(status string, color bool) string {
	switch strings.ToLower(status) {
	case "completed":
		return ui.Green(color, "✓")
	case "failed":
		return ui.Red(color, "✗")
	case "running":
		return ui.Blue(color, "●")
	case "pending":
		return ui.Dim(color, "○")
	default:
		return ui.Dim(color, "?")
	}
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
