package views

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/theme"
)

// AgentModel is the cockpit's Agent surface (orun-agents AG3): the workspace's
// git-authored agent types on the left, the selected type's detail plus a live
// session transcript on the right. Fully local — types come from the same
// content-addressed catalog every other surface reads; a transcript streams
// from a running session over the Events channel (nil until a run starts).
type AgentModel struct {
	Types  []services.AgentTypeRow
	Cursor int
	Filter string
	Width  int
	Height int

	// Live transcript.
	transcript []string
	running    bool
	Events     <-chan AgentTranscriptEvent
}

// AgentTranscriptEvent is one line of a streaming session transcript delivered
// to the Agent surface. Done marks end-of-stream.
type AgentTranscriptEvent struct {
	Line string
	Done bool
}

// AgentTranscriptMsg wraps a streamed transcript event for the tea loop.
type AgentTranscriptMsg struct{ Event AgentTranscriptEvent }

// WaitForAgentEvent blocks until the next transcript line arrives, mirroring
// events.WaitForRunEvent. A closed channel yields a Done sentinel.
func WaitForAgentEvent(ch <-chan AgentTranscriptEvent) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-ch
		if !ok {
			return AgentTranscriptMsg{Event: AgentTranscriptEvent{Done: true}}
		}
		return AgentTranscriptMsg{Event: e}
	}
}

// NewAgentModel builds the Agent surface from the loaded agent types.
func NewAgentModel(types []services.AgentTypeRow) AgentModel {
	return AgentModel{Types: types}
}

func (m AgentModel) Init() tea.Cmd { return nil }

// SetFilter narrows the type list by name/harness/owner substring.
func (m AgentModel) SetFilter(f string) AgentModel {
	m.Filter = f
	m.Cursor = 0
	return m
}

// StartStream attaches a transcript channel and returns the first wait command.
func (m AgentModel) StartStream(ch <-chan AgentTranscriptEvent) (AgentModel, tea.Cmd) {
	m.Events = ch
	m.running = true
	m.transcript = nil
	return m, WaitForAgentEvent(ch)
}

// Selected returns the highlighted agent type, or nil.
func (m AgentModel) Selected() *services.AgentTypeRow {
	rows := m.filtered()
	if len(rows) == 0 || m.Cursor < 0 || m.Cursor >= len(rows) {
		return nil
	}
	return &rows[m.Cursor]
}

func (m AgentModel) filtered() []services.AgentTypeRow {
	if m.Filter == "" {
		return m.Types
	}
	f := strings.ToLower(m.Filter)
	var out []services.AgentTypeRow
	for _, t := range m.Types {
		if strings.Contains(strings.ToLower(t.Name), f) ||
			strings.Contains(strings.ToLower(t.Harness), f) ||
			strings.Contains(strings.ToLower(t.Owner), f) {
			out = append(out, t)
		}
	}
	return out
}

func (m AgentModel) Update(msg tea.Msg) (AgentModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.Cursor > 0 {
				m.Cursor--
			}
		case "down", "j":
			if m.Cursor < len(m.filtered())-1 {
				m.Cursor++
			}
		}
	case AgentTranscriptMsg:
		if msg.Event.Done {
			m.running = false
			return m, nil
		}
		m.transcript = append(m.transcript, msg.Event.Line)
		if m.Events != nil {
			return m, WaitForAgentEvent(m.Events)
		}
	}
	return m, nil
}

func (m AgentModel) View() string {
	rows := m.filtered()
	if len(rows) == 0 {
		return theme.StyleMuted.Render("No agent types. Author one under agents/<name>.md, then `orun agent import`.")
	}

	var left strings.Builder
	left.WriteString(theme.StyleSectionTitle.Render("Agent types"))
	left.WriteByte('\n')
	for i, t := range rows {
		marker := "  "
		name := t.Name
		if i == m.Cursor {
			marker = theme.StyleAccent.Render("▸ ")
			name = theme.StyleAccent.Render(name)
		}
		fmt.Fprintf(&left, "%s%s\n", marker, name)
		left.WriteString(theme.StyleMuted.Render("    "+t.Harness+" · "+t.Model) + "\n")
	}

	var right strings.Builder
	if sel := m.Selected(); sel != nil {
		right.WriteString(theme.StyleSectionTitle.Render(sel.Name))
		right.WriteByte('\n')
		fmt.Fprintf(&right, "harness   %s\n", sel.Harness)
		fmt.Fprintf(&right, "model     %s\n", sel.Model)
		fmt.Fprintf(&right, "owner     %s\n", orDashView(sel.Owner))
		if sel.Autonomy != "" {
			fmt.Fprintf(&right, "autonomy  %s\n", sel.Autonomy)
		}
		if len(sel.MayAffect) > 0 {
			fmt.Fprintf(&right, "affects   %s\n", strings.Join(sel.MayAffect, ", "))
		}
		if p := strings.TrimSpace(sel.Persona); p != "" {
			right.WriteByte('\n')
			right.WriteString(theme.StyleMuted.Render(firstLines(p, 6)))
			right.WriteByte('\n')
		}
	}
	if m.running || len(m.transcript) > 0 {
		right.WriteByte('\n')
		title := "Transcript"
		if m.running {
			title += " (live)"
		}
		right.WriteString(theme.StyleSectionTitle.Render(title))
		right.WriteByte('\n')
		for _, line := range m.transcript {
			right.WriteString("  " + line + "\n")
		}
	}

	return joinColumns(left.String(), right.String(), m.Width)
}

func orDashView(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = append(lines[:n], "…")
	}
	return strings.Join(lines, "\n")
}

// joinColumns lays two text blocks side by side with a gutter, falling back to
// stacked output on narrow terminals.
func joinColumns(left, right string, width int) string {
	if width < 80 || strings.TrimSpace(right) == "" {
		if strings.TrimSpace(right) == "" {
			return left
		}
		return left + "\n" + right
	}
	leftLines := strings.Split(left, "\n")
	rightLines := strings.Split(right, "\n")
	colW := 34
	n := len(leftLines)
	if len(rightLines) > n {
		n = len(rightLines)
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		l, r := "", ""
		if i < len(leftLines) {
			l = leftLines[i]
		}
		if i < len(rightLines) {
			r = rightLines[i]
		}
		pad := colW - lipglossWidth(l)
		if pad < 1 {
			pad = 1
		}
		b.WriteString(l)
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(r)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// lipglossWidth is the visible width of a possibly-styled string. Styling adds
// ANSI escapes; strip them for width. Kept minimal to avoid a lipgloss import
// churn here — the theme styles are simple foreground colors.
func lipglossWidth(s string) int {
	w, inEsc := 0, false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEsc = true
		case inEsc && r == 'm':
			inEsc = false
		case inEsc:
			// skip
		default:
			w++
		}
	}
	return w
}
