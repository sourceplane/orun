package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/cockpit/render"
	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/statebackend"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

var (
	logsExecID      string
	logsRevision    string
	logsAll         bool
	logsJob         string
	logsStep        string
	logsFailed      bool
	logsRaw         bool
	logsRemoteState bool
	logsFollow      bool
	logsBackendURL  string
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
	logsCmd.Flags().StringVar(&logsRevision, "revision", "", "Pin the resolution to a revision key")
	logsCmd.Flags().BoolVar(&logsAll, "all", false, "Stream logs across all executions (best-effort)")
	logsCmd.Flags().StringVar(&logsJob, "job", "", "Filter logs by job ID")
	logsCmd.Flags().StringVar(&logsStep, "step", "", "Filter logs by step ID")
	logsCmd.Flags().BoolVar(&logsFailed, "failed", false, "Show only failed jobs or steps")
	logsCmd.Flags().BoolVar(&logsRaw, "raw", false, "Show full raw logs instead of compact excerpts")
	logsCmd.Flags().BoolVar(&logsRemoteState, "remote-state", false, "Fetch logs from orun-backend")
	logsCmd.Flags().BoolVar(&logsFollow, "follow", false, "Live-tail a job's log (requires --remote-state and --job); polls until the job completes")
	logsCmd.Flags().StringVar(&logsBackendURL, "backend-url", "", "orun-backend URL for remote state (or set ORUN_BACKEND_URL)")
}

func isLogsRemoteActive() bool {
	if logsRemoteState {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv(remoteStateEnvVar)), "true") {
		return true
	}
	if intentFile != "" {
		if si, _, err := loadResolvedIntentFile(intentFile); err == nil {
			if si != nil && strings.EqualFold(strings.TrimSpace(si.Execution.State.Mode), "remote") {
				return true
			}
		}
	}
	return false
}

func resolveLogsBackendURL() string {
	var intent *model.Intent
	if intentFile != "" {
		if si, _, err := loadResolvedIntentFile(intentFile); err == nil {
			intent = si
		}
	}
	return resolveBackendURLWithConfig(intent, logsBackendURL)
}

func showLogs() error {
	color := ui.ColorEnabledForWriter(os.Stdout)

	if isLogsRemoteActive() {
		backendURL := resolveLogsBackendURL()
		if backendURL == "" {
			return fmt.Errorf("--remote-state requires --backend-url or ORUN_BACKEND_URL")
		}
		runID := logsExecID
		if runID == "" {
			runID = os.Getenv(execIDEnvVar)
		}
		if runID == "" {
			return fmt.Errorf("--remote-state requires --exec-id or ORUN_EXEC_ID")
		}
		backend, err := newRemoteBackend(backendURL)
		if err != nil {
			return err
		}
		defer backend.Close(context.Background())
		if logsFollow {
			return followRemoteLog(runID, backend, color)
		}
		return showRemoteLogs(runID, backend, color)
	}

	if logsFollow {
		return fmt.Errorf("--follow requires --remote-state (live tail reads from the Orun Cloud backend)")
	}

	// Read logs from the content-addressed object graph.
	reader, ok := openObjectReader()
	if !ok {
		fmt.Println(ui.Dim(color, "No runs yet."))
		return nil
	}
	ref := logsExecID
	if ref == "" {
		ref = "executions/latest"
	}
	if handled, err := objShowLogs(reader, ref, color); handled || err != nil {
		return err
	}
	fmt.Println(ui.Dim(color, "No runs yet."))
	return nil
}

func showRemoteLogs(runID string, backend statebackend.Backend, color bool) error {
	ctx := context.Background()
	st, meta, err := backend.LoadRunState(ctx, runID)
	if err != nil {
		return fmt.Errorf("fetching remote run state: %w", err)
	}

	entries, err := collectRemoteLogEntries(ctx, backend, runID, st)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println(ui.Dim(color, "No logs for this run yet."))
		return nil
	}

	sort.Slice(entries, func(i, j int) bool {
		ci, ei, _ := splitJobID(entries[i].jobID)
		cj, ej, _ := splitJobID(entries[j].jobID)
		if ci != cj {
			return ci < cj
		}
		if ei != ej {
			return ei < ej
		}
		oi := statusSortKey(entries[i].status)
		oj := statusSortKey(entries[j].status)
		if oi != oj {
			return oi < oj
		}
		return entries[i].jobID < entries[j].jobID
	})
	entries = selectRelevantLogEntries(entries)

	counts := executionCountsFromState(meta, st)
	renderLogEntries(runID, meta, counts, entries, color)
	return nil
}

// followRemoteLog live-tails a single job's log via the fromSeq cursor until the
// job completes (or the user interrupts with Ctrl-C). Requires --job; multi-job
// follow is a later increment.
func followRemoteLog(runID string, backend statebackend.Backend, color bool) error {
	jobID := strings.TrimSpace(logsJob)
	if jobID == "" {
		return fmt.Errorf("--follow requires --job <jobId> (the job to tail)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	const pollInterval = 1500 * time.Millisecond
	const maxConsecutiveErrors = 5
	fromSeq := 0
	errCount := 0
	fmt.Fprintf(os.Stdout, "%s tailing %s (Ctrl-C to stop)\n", ui.Dim(color, "○"), jobID)
	for {
		content, nextSeq, complete, err := backend.TailJobLog(ctx, runID, jobID, fromSeq)
		if err != nil {
			if ctx.Err() != nil {
				return nil // interrupted
			}
			errCount++
			if errCount >= maxConsecutiveErrors {
				return fmt.Errorf("tailing logs for %s: %w", jobID, err)
			}
			fmt.Fprintf(os.Stderr, "%s tail reconnecting (%v)\n", ui.Yellow(color, "↻"), err)
		} else {
			errCount = 0
			if content != "" {
				fmt.Fprint(os.Stdout, content)
				fromSeq = nextSeq
			}
			if complete {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(pollInterval):
		}
	}
}

func collectRemoteLogEntries(ctx context.Context, backend statebackend.Backend, runID string, st *execmodel.ExecState) ([]logEntry, error) {
	if st == nil {
		return nil, nil
	}
	entries := make([]logEntry, 0, len(st.Jobs))
	for jobID, js := range st.Jobs {
		if logsJob != "" && !strings.Contains(jobID, logsJob) {
			continue
		}
		content, err := backend.ReadJobLog(ctx, runID, jobID)
		if err != nil || strings.TrimSpace(content) == "" {
			continue
		}
		status := "completed"
		if js != nil && js.Status != "" {
			status = js.Status
		}
		if logsFailed && !strings.EqualFold(status, "failed") {
			continue
		}
		entries = append(entries, logEntry{
			jobID:   jobID,
			stepID:  "",
			status:  status,
			content: strings.TrimSpace(content),
		})
	}
	return entries, nil
}

func renderLogEntries(execID string, meta *execmodel.ExecMetadata, counts executionCounts, entries []logEntry, color bool) {
	// Route through the cockpit renderer so `orun logs` and the TUI log
	// pane (Phase 3) share the same brand/header/grouping language.
	st := &execmodel.ExecState{}
	v := viewmodel.LogsView{
		ExecID:  execID,
		Run:     viewmodel.BuildRunView(execID, meta, st),
		Entries: toCockpitLogEntries(entries),
	}
	// Override the synthetic Counts (we built RunView with an empty state
	// so it would have zeroed totals) with the caller's authoritative
	// per-job counts.
	v.Run.Counts = viewmodel.Counts{
		Total:     counts.total,
		Completed: counts.completed,
		Failed:    counts.failed,
		Running:   counts.running,
		Pending:   counts.pending,
	}
	s := cockpitSurface(os.Stdout)
	lines := render.RunLogs(s, v, render.LogsOptions{Raw: logsRaw})
	for _, line := range lines {
		fmt.Fprintln(os.Stdout, line)
	}
	_ = color
}

func toCockpitLogEntries(in []logEntry) []viewmodel.LogEntry {
	out := make([]viewmodel.LogEntry, 0, len(in))
	for _, e := range in {
		comp, env, short := splitJobID(e.jobID)
		all := viewmodel.SplitLines(e.content)
		out = append(out, viewmodel.LogEntry{
			JobID:       e.jobID,
			Component:   comp,
			Environment: env,
			Short:       short,
			StepID:      e.stepID,
			Status:      e.status,
			Lines:       all,
			TotalLines:  len(all),
		})
	}
	viewmodel.SortLogEntries(out)
	return out
}

func renderLogEntriesLegacy(execID string, meta *execmodel.ExecMetadata, counts executionCounts, entries []logEntry, color bool) {
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
		subParts := []string{}
		if strings.TrimSpace(meta.PlanID) != "" {
			subParts = append(subParts, "plan "+meta.PlanID)
		}
		for _, link := range meta.Links {
			label := strings.TrimSpace(link.Label)
			if label == "" {
				label = "link"
			}
			subParts = append(subParts, label+" "+link.URL)
		}
		if len(subParts) > 0 {
			fmt.Fprintln(os.Stdout, "  "+ui.Dim(color, strings.Join(subParts, "  ·  ")))
		}
	}

	currentGroup := ""
	for _, entry := range entries {
		comp, env, short := splitJobID(entry.jobID)
		group := comp
		if env != "" {
			group = comp + "  " + ui.Dim(color, "·  "+env)
		}
		if group != "" && group != currentGroup {
			fmt.Println()
			fmt.Println("  " + ui.Bold(color, group))
			currentGroup = group
		}
		label := short
		if label == "" {
			label = entry.jobID
		}
		stepLabel := entry.stepID
		if stepLabel != "" {
			label += "  " + ui.Dim(color, stepLabel)
		}
		fmt.Printf("    %s %s\n", styleStatus(entry.status, color), ui.Bold(color, label))
		lines := compactLogLines(entry.content, logsRaw)
		for _, line := range lines {
			fmt.Printf("       %s\n", line)
		}
		if !logsRaw {
			totalLines := len(strings.Split(strings.TrimSpace(entry.content), "\n"))
			if totalLines > len(lines) {
				fmt.Printf("       %s\n", ui.Dim(color, fmt.Sprintf("… %d more line%s", totalLines-len(lines), plural(totalLines-len(lines)))))
			}
		}
	}
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
