package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/objread"
	"github.com/sourceplane/orun/internal/objview"
	"github.com/sourceplane/orun/internal/statebackend"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

var (
	statusExecID      string
	statusRevision    string
	statusAll         bool
	statusDetailed    bool
	statusJSON        bool
	statusWatch       bool
	statusInterval    time.Duration
	statusRemoteState bool
	statusBackendURL  string
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
	Long:  "Show the current state of a run. Defaults to the latest run. Use --watch to live-tail the dashboard.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return showStatus()
	},
}

func registerStatusCommand(root *cobra.Command) {
	root.AddCommand(statusCmd)

	statusCmd.Flags().StringVar(&statusExecID, "exec-id", "", "Show a specific execution")
	statusCmd.Flags().StringVar(&statusRevision, "revision", "", "Pin the resolution to a revision key")
	statusCmd.Flags().BoolVar(&statusAll, "all", false, "Show all executions")
	statusCmd.Flags().BoolVar(&statusDetailed, "detailed", false, "Show step-level detail")
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "Output in JSON format")
	statusCmd.Flags().BoolVarP(&statusWatch, "watch", "w", false, "Continuously refresh the status view")
	statusCmd.Flags().DurationVar(&statusInterval, "interval", time.Second, "Refresh interval when --watch is set")
	statusCmd.Flags().BoolVar(&statusRemoteState, "remote-state", false, "Fetch status from orun-backend")
	statusCmd.Flags().StringVar(&statusBackendURL, "backend-url", "", "orun-backend URL for remote state (or set ORUN_BACKEND_URL)")
}

func isStatusRemoteActive() bool {
	if statusRemoteState {
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

func resolveStatusBackendURL() string {
	var intent *model.Intent
	if intentFile != "" {
		if si, _, err := loadResolvedIntentFile(intentFile); err == nil {
			intent = si
		}
	}
	return resolveBackendURLWithConfig(intent, statusBackendURL)
}

func showStatus() error {
	color := ui.ColorEnabledForWriter(os.Stdout)

	if isStatusRemoteActive() {
		backendURL := resolveStatusBackendURL()
		if backendURL == "" {
			return fmt.Errorf("--remote-state requires --backend-url or ORUN_BACKEND_URL")
		}
		runID := statusExecID
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
		if statusWatch {
			return watchRemoteExecution(runID, backend, color)
		}
		return showRemoteExecution(runID, backend, color)
	}

	// Local executions are read from the content-addressed object graph.
	reader, ok := openObjectReader()
	if !ok {
		if statusAll {
			return cockpitRenderRunList(nil)
		}
		fmt.Println(ui.Dim(color, "No runs yet."))
		fmt.Println()
		fmt.Printf("  Start one with: %s\n", ui.Bold(color, "orun run"))
		return nil
	}

	if statusAll {
		if handled, err := objStatusList(reader); handled || err != nil {
			return err
		}
		return cockpitRenderRunList(nil)
	}

	ref := statusExecID
	if ref == "" {
		ref = "executions/latest"
	}
	if statusWatch {
		return watchObjectExecution(reader, color)
	}
	if handled, err := objStatusSingle(reader, ref, color); handled || err != nil {
		return err
	}
	fmt.Println(ui.Dim(color, "No runs yet."))
	fmt.Println()
	fmt.Printf("  Start one with: %s\n", ui.Bold(color, "orun run"))
	return nil
}

// watchObjectExecution polls the object graph and renders the most recent run
// (a live working tree if one is in flight, else the latest sealed execution),
// stopping when it reaches a terminal (sealed) state.
func watchObjectExecution(reader *objread.Reader, color bool) error {
	interval := statusInterval
	if interval < 200*time.Millisecond {
		interval = time.Second
	}
	fmt.Print("\x1b[?25l")
	defer fmt.Print("\x1b[?25h\n")
	for {
		list, err := reader.List(context.Background())
		fmt.Print("\x1b[H\x1b[J")
		if err != nil || len(list) == 0 {
			fmt.Println(ui.Dim(color, "Waiting for a run…"))
		} else {
			v, gerr := reader.Get(context.Background(), list[0].ExecutionID)
			if gerr == nil {
				_ = renderExecution(v.ExecutionID, objview.ToMeta(v), objview.ToState(v), color)
				if !v.Live {
					return nil
				}
			}
		}
		time.Sleep(interval)
	}
}

func showRemoteExecution(runID string, backend statebackend.Backend, color bool) error {
	ctx := context.Background()
	st, meta, err := backend.LoadRunState(ctx, runID)
	if err != nil {
		return fmt.Errorf("fetching remote run state: %w", err)
	}
	if statusJSON {
		return renderExecutionJSON(runID, meta, st)
	}
	return renderExecution(runID, meta, st, color)
}

// runEventWaiter is the optional, event-driven refresh seam: a backend that can
// long-poll its run event stream (the native v2 CoordBackend) lets `--watch`
// block until something actually changes instead of re-polling on a timer.
type runEventWaiter interface {
	WaitForRunEvents(ctx context.Context, runID string, sinceSeq, waitSeconds int) (int, error)
}

// watchLongPollSeconds is how long a single event-driven wait blocks before
// looping to refresh — well under the server's 25s cap and typical edge timeouts.
const watchLongPollSeconds = 15

func watchRemoteExecution(runID string, backend statebackend.Backend, color bool) error {
	interval := statusInterval
	if interval < 200*time.Millisecond {
		interval = time.Second
	}
	ctx := context.Background()
	fmt.Print("\x1b[?25l")
	defer fmt.Print("\x1b[?25h\n")

	// Prefer event-driven refresh: block on the run's log stream until an event
	// is appended. Backends without the stream (legacy OP2) fall back to polling.
	waiter, _ := backend.(runEventWaiter)
	sinceSeq := 0
	if waiter != nil {
		if hd, err := waiter.WaitForRunEvents(ctx, runID, 0, 0); err == nil {
			sinceSeq = hd
		}
	}

	for {
		st, meta, err := backend.LoadRunState(ctx, runID)
		fmt.Print("\x1b[H\x1b[J")
		if err != nil {
			fmt.Println(ui.Dim(color, fmt.Sprintf("Error fetching remote state: %v", err)))
		} else {
			if renderErr := renderExecution(runID, meta, st, color); renderErr != nil {
				return renderErr
			}
			if meta != nil {
				s := strings.ToLower(strings.TrimSpace(meta.Status))
				if s == "completed" || s == "failed" || s == "canceled" {
					return nil
				}
			}
		}
		if waiter != nil {
			// Block until the next event (or the long-poll lapses, which refreshes
			// anyway as a liveness heartbeat). On error, fall back to a timed wait.
			if newSeq, waitErr := waiter.WaitForRunEvents(ctx, runID, sinceSeq, watchLongPollSeconds); waitErr == nil {
				sinceSeq = newSeq
				continue
			}
		}
		time.Sleep(interval)
	}
}

func renderExecutionJSON(execID string, meta *execmodel.ExecMetadata, st *execmodel.ExecState) error {
	out := map[string]interface{}{
		"execID": execID,
	}
	if meta != nil {
		out["meta"] = meta
	}
	if st != nil {
		out["state"] = st
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

type jobView struct {
	id     string
	comp   string
	env    string
	short  string
	status string
	err    string
	dur    string
	steps  map[string]string
}

func collectJobViews(st *execmodel.ExecState) []jobView {
	if st == nil {
		return nil
	}
	jobs := make([]jobView, 0, len(st.Jobs))
	for jobID, js := range st.Jobs {
		if js == nil {
			continue
		}
		duration := ""
		if js.StartedAt != "" {
			duration = formatDuration(js.StartedAt, js.FinishedAt)
		}
		comp, env, short := splitJobID(jobID)
		jobs = append(jobs, jobView{
			id:     jobID,
			comp:   comp,
			env:    env,
			short:  short,
			status: js.Status,
			err:    js.LastError,
			dur:    duration,
			steps:  js.Steps,
		})
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].comp != jobs[j].comp {
			return jobs[i].comp < jobs[j].comp
		}
		if jobs[i].env != jobs[j].env {
			return jobs[i].env < jobs[j].env
		}
		oi := statusSortKey(jobs[i].status)
		oj := statusSortKey(jobs[j].status)
		if oi != oj {
			return oi < oj
		}
		return jobs[i].id < jobs[j].id
	})
	return jobs
}

func renderExecution(execID string, meta *execmodel.ExecMetadata, st *execmodel.ExecState, color bool) error {
	// Cockpit bridge: unified renderer shared with TUI.
	if _, err := cockpitRenderExecution(execID, meta, st); err == nil {
		return nil
	}
	// Fallback: legacy renderer (kept until Phase 2 deprecation pass).
	return renderExecutionLegacy(execID, meta, st, color)
}

func renderExecutionLegacy(execID string, meta *execmodel.ExecMetadata, st *execmodel.ExecState, color bool) error {
	counts := executionCountsFromState(meta, st)

	status := "unknown"
	if meta != nil && strings.TrimSpace(meta.Status) != "" {
		status = meta.Status
	}
	duration := ""
	if meta != nil {
		duration = formatDuration(meta.StartedAt, meta.FinishedAt)
	}

	planName := ""
	planID := ""
	if meta != nil {
		planName = strings.TrimSpace(meta.PlanName)
		planID = strings.TrimSpace(meta.PlanID)
	}

	jobs := collectJobViews(st)
	componentSet := map[string]struct{}{}
	for _, j := range jobs {
		if j.comp != "" {
			componentSet[j.comp] = struct{}{}
		}
	}

	// ── Header (▲ orun + Plan/Run + Scope) ────────────────────────────
	if planName == "" {
		planName = "plan"
	}
	fmt.Fprintf(os.Stdout, "\n%s %s\n",
		ui.BoldCyan(color, "▲ orun"),
		ui.Bold(color, planName),
	)
	subParts := []string{}
	if planID != "" {
		subParts = append(subParts, "Plan: "+planID)
	}
	subParts = append(subParts, "Run: "+execID)
	subParts = append(subParts, "State: "+statusLabel(status))
	if duration != "" {
		subParts = append(subParts, "Duration: "+duration)
	}
	fmt.Println("  " + ui.Dim(color, strings.Join(subParts, "  ·  ")))

	scopeParts := []string{}
	if len(componentSet) > 0 {
		scopeParts = append(scopeParts, fmt.Sprintf("%d component%s", len(componentSet), plural(len(componentSet))))
	}
	if counts.total > 0 {
		scopeParts = append(scopeParts, fmt.Sprintf("%d job%s", counts.total, plural(counts.total)))
	}
	if len(scopeParts) > 0 {
		fmt.Println("  " + ui.Dim(color, "Scope: "+strings.Join(scopeParts, " · ")))
	}
	fmt.Println()

	// ── Status legend + progress bar ──────────────────────────────────
	if counts.total > 0 {
		statusParts := []string{
			fmt.Sprintf("%s %d succeeded", ui.Green(color, "✓"), counts.completed),
			fmt.Sprintf("%s %d running", ui.Cyan(color, "●"), counts.running),
			fmt.Sprintf("%s %d queued", ui.Dim(color, "○"), counts.pending),
		}
		if counts.failed > 0 {
			statusParts = append(statusParts, fmt.Sprintf("%s %d failed", ui.Red(color, "✗"), counts.failed))
		}
		pct := 0
		if counts.total > 0 {
			pct = (counts.completed + counts.failed) * 100 / counts.total
		}
		fmt.Println("  " + ui.Dim(color, "Status:   ") + strings.Join(statusParts, "  ·  "))
		fmt.Println("  " + ui.Dim(color, "Progress: ") + ui.RenderProgressBar(pct, 32) + " " + fmt.Sprintf("%3d%%", pct))
		fmt.Println()
	}

	if meta != nil {
		for _, link := range meta.Links {
			fmt.Fprintf(os.Stdout, "  %s %s\n", displayLinkLabel(link.Label, color), link.URL)
		}
	}

	if len(jobs) == 0 {
		fmt.Fprintf(os.Stdout, "  %s orun logs --exec-id %s\n", ui.Dim(color, "Logs:"), execID)
		return nil
	}

	// Detect multi-environment so we can label envs separately under each
	// component group.
	multiEnv := false
	seenEnv := ""
	for _, j := range jobs {
		if j.env == "" {
			continue
		}
		if seenEnv == "" {
			seenEnv = j.env
			continue
		}
		if j.env != seenEnv {
			multiEnv = true
			break
		}
	}

	// Group jobs by component, preserving the sort order computed above.
	type componentGroup struct {
		comp string
		rows []jobView
	}
	var groups []*componentGroup
	groupIdx := map[string]int{}
	for _, j := range jobs {
		key := j.comp
		if key == "" {
			key = j.short
		}
		if i, ok := groupIdx[key]; ok {
			groups[i].rows = append(groups[i].rows, j)
		} else {
			groupIdx[key] = len(groups)
			groups = append(groups, &componentGroup{comp: key, rows: []jobView{j}})
		}
	}

	// Partition into completed, active, queued buckets.
	type renderGroup struct {
		comp     string
		rows     []jobView
		state    string // completed / active / queued / failed
		duration string
	}
	completed := []renderGroup{}
	active := []renderGroup{}
	queued := []renderGroup{}
	for _, g := range groups {
		state := "completed"
		hasFailed := false
		hasRunning := false
		hasPending := false
		for _, r := range g.rows {
			s := strings.ToLower(r.status)
			switch s {
			case "failed":
				hasFailed = true
			case "running":
				hasRunning = true
			case "pending", "":
				hasPending = true
			}
		}
		switch {
		case hasFailed && !hasRunning && !hasPending:
			state = "failed"
		case hasRunning:
			state = "active"
		case hasPending && !hasRunning:
			state = "queued"
		}
		// Aggregate duration only for fully-terminated groups.
		dur := ""
		if state == "completed" || state == "failed" {
			dur = aggregateDuration(g.rows)
		}
		rg := renderGroup{comp: g.comp, rows: g.rows, state: state, duration: dur}
		switch state {
		case "completed", "failed":
			completed = append(completed, rg)
		case "active":
			active = append(active, rg)
		default:
			queued = append(queued, rg)
		}
	}

	for _, g := range completed {
		envCount := envCountFor(g.rows)
		envLabel := fmt.Sprintf("%d env%s", envCount, plural(envCount))
		marker := fmt.Sprintf("%s %s done", envLabel, g.state)
		if g.state == "failed" {
			marker = ui.Red(color, fmt.Sprintf("%s failed", envLabel))
		} else {
			marker = ui.Dim(color, marker)
		}
		line := fmt.Sprintf("  %s %s",
			styleStatus(g.state, color),
			ui.Bold(color, g.comp),
		)
		line += "  " + marker
		if g.duration != "" {
			line += "  " + ui.Dim(color, "("+g.duration+")")
		}
		fmt.Println(line)
	}

	if len(active) > 0 {
		fmt.Println()
		fmt.Println("  " + ui.Dim(color, "Active"))
		fmt.Println("  " + ui.Dim(color, "│"))
		for ai, g := range active {
			fmt.Printf("  %s %s\n", ui.Cyan(color, "●"), ui.Bold(color, g.comp))
			for ri, r := range g.rows {
				connector := "├─"
				if ri == len(g.rows)-1 {
					connector = "└─"
				}
				envText := ""
				if multiEnv && r.env != "" {
					envText = ui.Bold(color, r.env) + "  "
				}
				stepFraction := stepProgressFraction(r.steps)
				bar := ""
				if stepFraction != "" {
					bar = " " + ui.Dim(color, "["+stepFraction+"]")
				}
				stepLabel := r.short
				if stepLabel == "" {
					stepLabel = r.id
				}
				fmt.Printf("  %s  %s %s %s%s%s\n",
					ui.Dim(color, "│"),
					ui.Dim(color, connector),
					styleStatus(r.status, color),
					envText,
					ui.Dim(color, stepLabel),
					bar,
				)
				if statusDetailed {
					stepIDs := make([]string, 0, len(r.steps))
					for sid := range r.steps {
						stepIDs = append(stepIDs, sid)
					}
					sort.Strings(stepIDs)
					for _, sid := range stepIDs {
						fmt.Printf("  %s     %s %s\n",
							ui.Dim(color, "│"),
							styleStatus(r.steps[sid], color),
							ui.Dim(color, sid))
					}
				}
			}
			if ai < len(active)-1 {
				fmt.Println("  " + ui.Dim(color, "│"))
			}
		}
		fmt.Println()
	}

	if len(queued) > 0 {
		shown := 3
		if len(queued) < shown {
			shown = len(queued)
		}
		for i := 0; i < shown; i++ {
			g := queued[i]
			envCount := envCountFor(g.rows)
			fmt.Printf("  %s %s  %s\n",
				ui.Dim(color, "○"),
				ui.Bold(color, g.comp),
				ui.Dim(color, fmt.Sprintf("queued (%d env%s)", envCount, plural(envCount))),
			)
		}
		if rem := len(queued) - shown; rem > 0 {
			fmt.Printf("  %s %s\n",
				ui.Dim(color, "○"),
				ui.Dim(color, fmt.Sprintf("+ %d more component%s queued", rem, plural(rem))),
			)
		}
		fmt.Println()
	}

	fmt.Fprintf(os.Stdout, "  %s orun logs --exec-id %s\n", ui.Dim(color, "Logs:"), execID)
	return nil
}

// stepProgressFraction returns "n/m" if step counts are available, "" otherwise.
func stepProgressFraction(steps map[string]string) string {
	if len(steps) == 0 {
		return ""
	}
	done := 0
	for _, s := range steps {
		st := strings.ToLower(s)
		if st == "completed" || st == "failed" {
			done++
		}
	}
	return fmt.Sprintf("%d/%d", done, len(steps))
}

func envCountFor(rows []jobView) int {
	envs := map[string]struct{}{}
	for _, r := range rows {
		envs[r.env] = struct{}{}
	}
	return len(envs)
}

func aggregateDuration(rows []jobView) string {
	var earliest, latest time.Time
	hasAny := false
	for _, r := range rows {
		// We only have formatted strings here; skip aggregation if present.
		if r.dur == "" {
			continue
		}
		_ = earliest
		_ = latest
		hasAny = true
	}
	if !hasAny {
		return ""
	}
	// Fall back to longest individual duration string for a quick signal.
	longest := ""
	for _, r := range rows {
		if len(r.dur) > len(longest) {
			longest = r.dur
		}
	}
	return longest
}

// splitJobID parses a job ID into component, env, and name.
// Supports both the new format ("component.environment.job") and the legacy
// format ("component@environment.job") for backward compatibility with old
// state files.
func splitJobID(id string) (component, env, name string) {
	// Legacy format: component@environment.job
	if at := strings.Index(id, "@"); at >= 0 {
		component = id[:at]
		rest := id[at+1:]
		if dot := strings.Index(rest, "."); dot >= 0 {
			return component, rest[:dot], rest[dot+1:]
		}
		return component, rest, ""
	}
	// New format: component.environment.job
	parts := strings.SplitN(id, ".", 3)
	if len(parts) == 3 {
		return parts[0], parts[1], parts[2]
	}
	if len(parts) == 2 {
		return "", parts[0], parts[1]
	}
	return "", "", id
}

func executionCountsFromState(meta *execmodel.ExecMetadata, st *execmodel.ExecState) executionCounts {
	if counts := execmodel.SummarizeExecutionState(st); counts.Total > 0 {
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
