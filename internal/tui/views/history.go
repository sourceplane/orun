package views

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"

	"github.com/sourceplane/orun/internal/tui/theme"
)

// HistoryModel renders the recent-runs list with a hand-built filter so we
// keep the styling consistent with the rest of the cockpit.
type HistoryModel struct {
	Runs   []services.RunSummary
	Cursor int
	Width  int
	Height int
	Filter string
}

func NewHistoryModel() HistoryModel { return HistoryModel{} }

func (m HistoryModel) Init() tea.Cmd { return nil }

func (m HistoryModel) SetFilter(f string) HistoryModel {
	m.Filter = f
	m.Cursor = 0
	return m
}

// Selected returns the currently highlighted RunSummary (or nil).
func (m HistoryModel) Selected() *services.RunSummary {
	rows := m.filtered()
	if len(rows) == 0 || m.Cursor < 0 || m.Cursor >= len(rows) {
		return nil
	}
	r := rows[m.Cursor]
	return &r
}

func (m HistoryModel) filtered() []services.RunSummary {
	if m.Filter == "" {
		return m.Runs
	}
	f := strings.ToLower(m.Filter)
	out := make([]services.RunSummary, 0, len(m.Runs))
	for _, r := range m.Runs {
		if strings.Contains(strings.ToLower(r.ExecID), f) ||
			strings.Contains(strings.ToLower(r.PlanName), f) ||
			strings.Contains(strings.ToLower(r.Status), f) {
			out = append(out, r)
		}
	}
	return out
}

func (m HistoryModel) Update(msg tea.Msg) (HistoryModel, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "down", "j":
			if m.Cursor+1 < len(m.filtered()) {
				m.Cursor++
			}
		case "up", "k":
			if m.Cursor > 0 {
				m.Cursor--
			}
		}
	}
	return m, nil
}

func (m HistoryModel) View() string {
	width := m.Width
	if width <= 0 {
		width = 80
	}
	rows := m.filtered()
	if len(rows) == 0 {
		hint := "No runs yet — open a component and press d to dry-run, R to execute."
		if m.Filter != "" {
			hint = fmt.Sprintf("No runs match %q.", m.Filter)
		}
		return centerCard(width, m.Height, hint)
	}

	idW := 14
	statW := 10
	planW := clamp(width*30/100, 12, 36)
	agoW := 16
	durW := 12

	var b strings.Builder
	title := fmt.Sprintf("History · %d runs", len(rows))
	if m.Filter != "" {
		title += "  " + theme.StyleDim.Render(fmt.Sprintf("(filter: %s)", m.Filter))
	}
	b.WriteString(theme.StyleSectionTitle.Render(title))
	b.WriteString("\n\n")
	b.WriteString(theme.StyleTableHeader.Render(fmt.Sprintf(" %s %s %s %s %s",
		pad("EXEC", idW), pad("STATUS", statW),
		pad("PLAN", planW), pad("STARTED", agoW), pad("DURATION", durW))))
	b.WriteString("\n")

	// Viewport: clip rows to the available height and scroll with the cursor so
	// the list never overflows past the top of the stage. Mirrors browse.go;
	// History has 3 lines of chrome (title, blank, table header) and no footer.
	maxRows := m.Height - 4
	if maxRows < 3 {
		maxRows = 3
	}
	start, end := viewportWindow(m.Cursor, len(rows), maxRows)

	for i := start; i < end; i++ {
		r := rows[i]
		execShort := r.ExecID
		if len(execShort) > idW-1 {
			execShort = execShort[:idW-1]
		}
		status := runStatusPill(r.Status)
		ago := humanAgo(r.StartedAt)
		dur := humanDur(r.Duration)
		line := fmt.Sprintf(" %s %s %s %s %s",
			pad(execShort, idW), pad(status, statW+8),
			pad(zoa(r.PlanName), planW), pad(ago, agoW), pad(dur, durW))
		if i == m.Cursor {
			b.WriteString(theme.StyleCursorBar.Render("▌") + theme.StyleTableRowSelected.Render(line))
		} else if i%2 == 1 {
			b.WriteString(" " + theme.StyleTableRowAlt.Render(line))
		} else {
			b.WriteString(" " + theme.StyleTableRow.Render(line))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func runStatusPill(s string) string {
	switch strings.ToLower(s) {
	case "success", "completed", "ok":
		return theme.StylePillSuccess.Render("● " + s)
	case "failed", "error":
		return theme.StylePillError.Render("● " + s)
	case "running", "in_progress":
		return theme.StylePillRunning.Render("● " + s)
	case "":
		return theme.StylePillIdle.Render("○ —")
	default:
		return theme.StylePillIdle.Render("○ " + s)
	}
}

func humanAgo(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func humanDur(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}
