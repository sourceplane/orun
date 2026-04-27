package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sourceplane/gluon/internal/state"
	"github.com/sourceplane/gluon/internal/ui"
	"github.com/spf13/cobra"
)

var (
	logsExecID string
	logsJob    string
	logsStep   string
	logsFailed bool
	logsRaw    bool
)

type logEntry struct {
	jobID   string
	stepID  string
	status  string
	content string
}

var logsCmd = &cobra.Command{
	Use:   "logs [run/<exec-id>] [job/<job-id>]",
	Short: "View logs for a run",
	Long:  "Show the most relevant logs from a run. Supports resource slash notation: logs run/<id>, logs job/<id>.",
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, arg := range args {
			if strings.HasPrefix(arg, "run/") {
				logsExecID = strings.TrimPrefix(arg, "run/")
			} else if strings.HasPrefix(arg, "job/") {
				logsJob = strings.TrimPrefix(arg, "job/")
			}
		}
		return showLogs()
	},
}

func registerLogsCommand(root *cobra.Command) {
	root.AddCommand(logsCmd)

	logsCmd.Flags().StringVar(&logsExecID, "exec-id", "", "Execution ID")
	logsCmd.Flags().StringVar(&logsJob, "job", "", "Filter logs by job ID")
	logsCmd.Flags().StringVar(&logsStep, "step", "", "Filter logs by step ID")
	logsCmd.Flags().BoolVar(&logsFailed, "failed", false, "Show only failed jobs or steps")
	logsCmd.Flags().BoolVar(&logsRaw, "raw", false, "Show full raw logs instead of compact excerpts")
}

func showLogs() error {
	store := state.NewStore(storeDir())
	color := ui.ColorEnabledForWriter(os.Stdout)

	execID := logsExecID
	if execID == "" {
		var err error
		execID, err = store.ResolveExecID("latest")
		if err != nil {
			fmt.Println(ui.Dim(color, "No runs yet."))
			return nil
		}
	} else {
		var err error
		execID, err = store.ResolveExecID(execID)
		if err != nil {
			return err
		}
	}

	meta, _ := store.LoadMetadata(execID)
	st, _ := store.LoadState(execID)
	counts := executionCountsFromState(meta, st)
	entries, err := loadLogEntries(store, execID, st)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println(ui.Dim(color, "No logs for this run yet."))
		return nil
	}

	sort.Slice(entries, func(i, j int) bool {
		oi := statusSortKey(entries[i].status)
		oj := statusSortKey(entries[j].status)
		if oi != oj {
			return oi < oj
		}
		if entries[i].jobID != entries[j].jobID {
			return entries[i].jobID < entries[j].jobID
		}
		return entries[i].stepID < entries[j].stepID
	})
	entries = selectRelevantLogEntries(entries)

	status := "unknown"
	duration := ""
	if meta != nil {
		status = statusLabel(meta.Status)
		duration = formatDuration(meta.StartedAt, meta.FinishedAt)
	}
	headerParts := []string{ui.Bold(color, execID)}
	if status != "" {
		headerParts = append(headerParts, fmt.Sprintf("%s %s", styleStatus(status, color), styleStatusText(status, color)))
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
	fmt.Println()

	for _, entry := range entries {
		fmt.Printf("%s %s / %s\n", styleStatus(entry.status, color), entry.jobID, entry.stepID)
		lines := compactLogLines(entry.content, logsRaw)
		for _, line := range lines {
			fmt.Println(line)
		}
		if !logsRaw {
			totalLines := len(strings.Split(strings.TrimSpace(entry.content), "\n"))
			if totalLines > len(lines) {
				fmt.Printf("%s\n", ui.Dim(color, fmt.Sprintf("… %d more line%s", totalLines-len(lines), plural(totalLines-len(lines)))))
			}
		}
		fmt.Println()
	}

	return nil
}

func loadLogEntries(store *state.Store, execID string, st *state.ExecState) ([]logEntry, error) {
	logsBase := filepath.Join(store.ExecPath(execID), "logs")
	if _, err := os.Stat(logsBase); os.IsNotExist(err) {
		return nil, nil
	}

	entries := make([]logEntry, 0)
	err := filepath.Walk(logsBase, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".log") {
			return nil
		}

		rel, _ := filepath.Rel(logsBase, path)
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		jobID := ""
		stepID := ""
		if len(parts) >= 1 {
			jobID = parts[0]
		}
		if len(parts) >= 2 {
			stepID = strings.TrimSuffix(parts[1], ".log")
		}

		if logsJob != "" && !strings.Contains(jobID, logsJob) {
			return nil
		}
		if logsStep != "" && !strings.Contains(stepID, logsStep) {
			return nil
		}

		status := "completed"
		if st != nil {
			if js, ok := st.Jobs[jobID]; ok && js != nil {
				if stepStatus, ok := js.Steps[stepID]; ok && strings.TrimSpace(stepStatus) != "" {
					status = stepStatus
				} else if strings.TrimSpace(js.Status) != "" {
					status = js.Status
				}
			}
		}
		if logsFailed && !strings.EqualFold(status, "failed") {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			return nil
		}

		entries = append(entries, logEntry{
			jobID:   jobID,
			stepID:  stepID,
			status:  status,
			content: content,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func compactLogLines(content string, raw bool) []string {
	lines := splitNonEmptyLines(content)
	if raw || len(lines) <= 8 {
		return lines
	}
	return lines[:8]
}

func splitNonEmptyLines(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	lines := make([]string, 0)
	for _, line := range strings.Split(strings.TrimSpace(value), "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines
}

func selectRelevantLogEntries(entries []logEntry) []logEntry {
	if logsRaw || logsFailed || logsJob != "" || logsStep != "" {
		return entries
	}

	hasFailures := false
	for _, entry := range entries {
		if strings.EqualFold(entry.status, "failed") {
			hasFailures = true
			break
		}
	}

	filtered := make([]logEntry, 0, len(entries))
	for _, entry := range entries {
		if hasFailures {
			if strings.EqualFold(entry.status, "failed") || logHasURL(entry.content) {
				filtered = append(filtered, entry)
			}
			continue
		}
		if logHasURL(entry.content) {
			filtered = append(filtered, entry)
		}
	}
	if len(filtered) == 0 {
		return entries
	}
	return filtered
}

func logHasURL(content string) bool {
	return strings.Contains(content, "https://") || strings.Contains(content, "http://")
}
