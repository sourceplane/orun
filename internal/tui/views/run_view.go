package views

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/events"
	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/theme"
)

// RunViewModel is the minimal live timeline rendered by the Run Dashboard
// during a streaming RunPlan. It owns a per-job status map and stops
// re-arming the event-wait command once a RunEventRunDone arrives.
type RunViewModel struct {
	// Events is the streaming channel returned by OrunService.RunPlan.
	// When nil the view renders an idle placeholder and never re-arms.
	Events <-chan services.RunEvent

	// DryRun records that the current run is a dry-run preview, surfaced
	// in the header for user reassurance (matches Spec 6.9 / 14.8).
	DryRun bool

	// ExecID is the most recently observed execution ID (carried in
	// RunEvent.JobID-adjacent payloads; reserved for log tailing).
	ExecID string

	rows    map[string]*runRow
	order   []string
	cursor  int
	done    bool
	endedAt time.Time
	startAt time.Time
}

type runRow struct {
	JobID     string
	Component string
	Env       string
	Status    string
	Err       string
	StartedAt time.Time
	EndedAt   time.Time
}

// NewRunViewModel constructs an empty Run Dashboard.
func NewRunViewModel() RunViewModel {
	return RunViewModel{rows: map[string]*runRow{}}
}

// Init starts waiting for the first event when Events is non-nil. The
// root model is responsible for installing an Events channel before
// switching mode to ModeRunDashboard.
func (m RunViewModel) Init() tea.Cmd {
	if m.Events == nil || m.done {
		return nil
	}
	return events.WaitForRunEvent(m.Events)
}

// StartStream installs the event channel, resets internal state, and
// returns the first WaitForRunEvent command. Callers (root model) must
// route the returned cmd back through bubbletea.
func (m RunViewModel) StartStream(ch <-chan services.RunEvent, dryRun bool) (RunViewModel, tea.Cmd) {
	m.Events = ch
	m.DryRun = dryRun
	m.rows = map[string]*runRow{}
	m.order = nil
	m.done = false
	m.endedAt = time.Time{}
	m.startAt = time.Now()
	if ch == nil {
		return m, nil
	}
	return m, events.WaitForRunEvent(ch)
}

// Update consumes RunEventMsg values and re-arms WaitForRunEvent until
// it receives a terminal RunEventRunDone.
func (m RunViewModel) Update(msg tea.Msg) (RunViewModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "down", "j":
			if m.cursor+1 < len(m.order) {
				m.cursor++
			}
			return m, nil
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "enter":
			if m.cursor >= 0 && m.cursor < len(m.order) {
				jobID := m.order[m.cursor]
				return m, func() tea.Msg {
					return RunJobSelectedMsg{JobID: jobID, ExecID: m.ExecID}
				}
			}
			return m, nil
		}
	case services.RunEventMsg:
		ev := msg.Event
		switch ev.Kind {
		case services.RunEventRunDone:
			m.done = true
			m.endedAt = time.Now()
			// Do not re-arm: stream is finished.
			return m, nil

		case services.RunEventJobStarted,
			services.RunEventJobCompleted,
			services.RunEventJobFailed:
			row, ok := m.rows[ev.JobID]
			if !ok {
				row = &runRow{JobID: ev.JobID, StartedAt: time.Now()}
				m.rows[ev.JobID] = row
				m.order = append(m.order, ev.JobID)
			}
			if ev.Component != "" {
				row.Component = ev.Component
			}
			if ev.Env != "" {
				row.Env = ev.Env
			}
			switch ev.Kind {
			case services.RunEventJobStarted:
				row.Status = "running"
			case services.RunEventJobCompleted:
				row.Status = "completed"
				row.EndedAt = time.Now()
			case services.RunEventJobFailed:
				row.Status = "failed"
				row.Err = ev.Error
				row.EndedAt = time.Now()
			}
		}
		// Re-arm to keep draining until the channel sends RunEventRunDone
		// (or closes, which WaitForRunEvent converts into RunEventRunDone).
		if m.done || m.Events == nil {
			return m, nil
		}
		return m, events.WaitForRunEvent(m.Events)
	}
	return m, nil
}

// View renders the timeline. Layout is intentionally minimal: header,
// grouped rows by environment, then a footer summarizing terminal state.
func (m RunViewModel) View() string {
	var b strings.Builder
	header := theme.StyleSectionTitle.Render("Activity · live")
	if m.DryRun {
		header += "  " + theme.StyleChipAccent.Render("(dry-run)")
	}
	b.WriteString(header + "\n\n")

	if len(m.rows) == 0 {
		if m.Events == nil {
			b.WriteString(theme.StyleDim.Render("open a component (1) and press d for a dry-run.\n"))
		} else if m.done {
			b.WriteString(theme.StyleDim.Render("run completed with no jobs reported.\n"))
		} else {
			b.WriteString(theme.StyleAccent.Render("◐ starting run…\n"))
		}
		return b.String()
	}

	// Group rows by env for readability; preserve event arrival order
	// within each group so the user sees jobs as they start.
	byEnv := map[string][]string{}
	for _, id := range m.order {
		row := m.rows[id]
		byEnv[row.Env] = append(byEnv[row.Env], id)
	}
	envs := make([]string, 0, len(byEnv))
	for env := range byEnv {
		envs = append(envs, env)
	}
	sort.Strings(envs)

	var done, failed, running int
	for _, env := range envs {
		label := env
		if label == "" {
			label = "(no-env)"
		}
		fmt.Fprintf(&b, "\n%s\n", theme.StyleChipAccent.Render("env "+label))
		for _, id := range byEnv[env] {
			row := m.rows[id]
			icon := styledStatusIcon(row.Status)
			comp := row.Component
			if comp == "" {
				comp = "-"
			}
			elapsed := ""
			if !row.StartedAt.IsZero() {
				end := row.EndedAt
				if end.IsZero() {
					end = time.Now()
				}
				d := end.Sub(row.StartedAt)
				if d > 0 {
					elapsed = theme.StyleDim.Render(humanShortDur(d))
				}
			}
			cursor := "  "
			if id == m.SelectedJobID() {
				cursor = theme.StyleCursorBar.Render("▌") + " "
			}
			fmt.Fprintf(&b, "%s%s %s %s %s %s\n",
				cursor,
				icon,
				theme.StyleValue.Render(fmt.Sprintf("%-32s", row.JobID)),
				theme.StyleChipDim.Render(comp),
				renderRunStatus(row.Status),
				elapsed,
			)
			if row.Status == "failed" && row.Err != "" {
				fmt.Fprintf(&b, "      %s %s\n",
					theme.StylePillError.Render("err:"), truncateErr(row.Err))
			}
			switch row.Status {
			case "completed":
				done++
			case "failed":
				failed++
			case "running":
				running++
			}
		}
	}

	b.WriteString("\n")
	if m.done {
		fmt.Fprintf(&b, "%s  %s  %s  %s\n",
			theme.StylePillSuccess.Render("✓ done"),
			theme.StylePillSuccess.Render(fmt.Sprintf("done %d", done)),
			pillFor(failed),
			theme.StyleDim.Render(fmt.Sprintf("total %d", len(m.rows))),
		)
	} else {
		fmt.Fprintf(&b, "%s  %s  %s  %s  %s\n",
			theme.StylePillRunning.Render("◐ in flight"),
			theme.StylePillRunning.Render(fmt.Sprintf("running %d", running)),
			theme.StylePillSuccess.Render(fmt.Sprintf("done %d", done)),
			pillFor(failed),
			theme.StyleDim.Render(fmt.Sprintf("total %d", len(m.rows))),
		)
	}
	return b.String()
}

func pillFor(failed int) string {
	if failed > 0 {
		return theme.StylePillError.Render(fmt.Sprintf("failed %d", failed))
	}
	return theme.StyleDim.Render("failed 0")
}

func styledStatusIcon(status string) string {
	switch status {
	case "running":
		return theme.StylePillRunning.Render("◐")
	case "completed":
		return theme.StylePillSuccess.Render("✓")
	case "failed":
		return theme.StylePillError.Render("✗")
	default:
		return theme.StyleDim.Render("·")
	}
}

func renderRunStatus(status string) string {
	switch status {
	case "running":
		return theme.StylePillRunning.Render("● running")
	case "completed":
		return theme.StylePillSuccess.Render("● done")
	case "failed":
		return theme.StylePillError.Render("● failed")
	default:
		return theme.StyleDim.Render("· " + status)
	}
}

func humanShortDur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

// Done reports whether the streaming run has emitted RunEventRunDone.
// Exposed for tests asserting no-re-arm semantics.
func (m RunViewModel) Done() bool { return m.done }

// Rows returns the per-job status rows in arrival order. Test-only seam.
func (m RunViewModel) Rows() []runRow {
	out := make([]runRow, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, *m.rows[id])
	}
	return out
}

func statusIcon(status string) string {
	switch status {
	case "running":
		return "…"
	case "completed":
		return "✓"
	case "failed":
		return "✗"
	default:
		return "·"
	}
}

func truncateErr(s string) string {
	const max = 160
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// RunJobSelectedMsg is emitted when the user presses `enter` on a job
// row. The root model uses it to open the Log Explorer attached to a
// TailLogs stream for that job.
type RunJobSelectedMsg struct {
	JobID  string
	ExecID string
	StepID string
}

// SelectedJobID returns the job ID under the cursor, or "" if none.
func (m RunViewModel) SelectedJobID() string {
	if m.cursor < 0 || m.cursor >= len(m.order) {
		return ""
	}
	return m.order[m.cursor]
}

// SelectedRow returns the row under the cursor (or nil).
func (m RunViewModel) SelectedRow() *runRow {
	id := m.SelectedJobID()
	if id == "" {
		return nil
	}
	row := m.rows[id]
	if row == nil {
		return nil
	}
	cp := *row
	return &cp
}
