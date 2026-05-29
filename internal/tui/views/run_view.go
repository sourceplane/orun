package views

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/events"
	"github.com/sourceplane/orun/internal/tui/services"
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

	rows    map[string]*runRow
	order   []string
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
	header := "Run Dashboard"
	if m.DryRun {
		header += " (dry-run)"
	}
	fmt.Fprintln(&b, header)

	if len(m.rows) == 0 {
		if m.Events == nil {
			b.WriteString("press `d` in Plan Studio to start a dry-run.\n")
		} else if m.done {
			b.WriteString("run completed with no jobs reported.\n")
		} else {
			b.WriteString("starting run…\n")
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
		fmt.Fprintf(&b, "\nenv=%s\n", label)
		for _, id := range byEnv[env] {
			row := m.rows[id]
			icon := statusIcon(row.Status)
			comp := row.Component
			if comp == "" {
				comp = "-"
			}
			fmt.Fprintf(&b, "  %s %-32s [%s] %s\n", icon, row.JobID, comp, row.Status)
			if row.Status == "failed" && row.Err != "" {
				fmt.Fprintf(&b, "      err: %s\n", truncateErr(row.Err))
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
		fmt.Fprintf(&b, "✓ run done — completed=%d failed=%d (jobs total=%d)\n",
			done, failed, len(m.rows))
	} else {
		fmt.Fprintf(&b, "… in flight — running=%d completed=%d failed=%d (total=%d)\n",
			running, done, failed, len(m.rows))
	}
	return b.String()
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
