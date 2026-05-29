package views

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sourceplane/orun/internal/tui/services"

	"github.com/sourceplane/orun/internal/tui/theme"
)

// BrowseModel renders the component list — the cockpit's primary surface.
//
// Columns: status icon · name · type · change indicator. Domain, envs, path,
// deps, and last-run history are surfaced in the inspector drawer instead
// of crowding the row.
type BrowseModel struct {
	Workspace *services.WorkspaceSnapshot
	Cursor    int
	Width     int
	Height    int
	Filter    string
}

func NewBrowseModel() BrowseModel { return BrowseModel{} }

func (m BrowseModel) Init() tea.Cmd { return nil }

// SetFilter sets the case-insensitive substring filter for component rows.
func (m BrowseModel) SetFilter(f string) BrowseModel {
	m.Filter = f
	m.Cursor = 0
	return m
}

// Selected returns the currently highlighted component (or nil).
func (m BrowseModel) Selected() *services.ComponentSummary {
	rows := m.filtered()
	if len(rows) == 0 {
		return nil
	}
	if m.Cursor < 0 || m.Cursor >= len(rows) {
		return nil
	}
	c := rows[m.Cursor]
	return &c
}

func (m BrowseModel) filtered() []services.ComponentSummary {
	if m.Workspace == nil {
		return nil
	}
	if m.Filter == "" {
		return m.Workspace.Components
	}
	f := strings.ToLower(m.Filter)
	out := make([]services.ComponentSummary, 0, len(m.Workspace.Components))
	for _, c := range m.Workspace.Components {
		if strings.Contains(strings.ToLower(c.Name), f) ||
			strings.Contains(strings.ToLower(c.Type), f) ||
			strings.Contains(strings.ToLower(c.Domain), f) {
			out = append(out, c)
		}
	}
	return out
}

func (m BrowseModel) Update(msg tea.Msg) (BrowseModel, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "down", "j":
			rows := m.filtered()
			if m.Cursor+1 < len(rows) {
				m.Cursor++
			}
		case "up", "k":
			if m.Cursor > 0 {
				m.Cursor--
			}
		case "home":
			m.Cursor = 0
		case "end", "G":
			rows := m.filtered()
			if len(rows) > 0 {
				m.Cursor = len(rows) - 1
			}
		case "enter":
			if sel := m.Selected(); sel != nil {
				name := sel.Name
				return m, func() tea.Msg { return ComponentEnterMsg{Name: name} }
			}
		}
	}
	return m, nil
}

// ComponentEnterMsg signals the user pressed `enter` on a component row in
// the Browse view. The root model handles this by opening Component Studio
// scoped to the named component and kicking off an auto-generate.
type ComponentEnterMsg struct {
	Name string
}

func (m BrowseModel) View() string {
	width := m.Width
	if width <= 0 {
		width = 80
	}
	if m.Workspace == nil {
		return centerCard(width, m.Height, "loading workspace…")
	}
	rows := m.filtered()

	// Header line.
	intent := m.Workspace.IntentName
	if intent == "" {
		intent = "workspace"
	}
	changed := 0
	for _, c := range m.Workspace.Components {
		if c.Changed {
			changed++
		}
	}
	headerL := theme.StyleSectionTitle.Render(intent) +
		theme.StyleDim.Render(fmt.Sprintf("  ·  %d components", len(m.Workspace.Components)))
	if changed > 0 {
		headerL += "  " + theme.StyleChangedDot.Render(fmt.Sprintf("● %d changed", changed))
	}
	if m.Filter != "" {
		headerL += "  " + theme.StyleDim.Render(fmt.Sprintf("(filter: %s)", m.Filter))
	}

	var b strings.Builder
	b.WriteString(headerL)
	b.WriteString("\n\n")

	if len(rows) == 0 {
		hint := "No components yet — try `orun init` or generate a plan."
		if m.Filter != "" {
			hint = fmt.Sprintf("No components match %q.", m.Filter)
		}
		b.WriteString(centerCard(width, m.Height-4, hint))
		return b.String()
	}

	// Column widths.
	nameW := clamp(width*45/100, 16, 56)
	typeW := clamp(width*20/100, 8, 24)

	header := theme.StyleTableHeader.Render(fmt.Sprintf(" %s  %s  %s",
		pad("NAME", nameW), pad("TYPE", typeW), "CHG"))
	b.WriteString(" " + header)
	b.WriteString("\n")

	for i, c := range rows {
		glyph := theme.StatusGlyph(c.LastRunStatus)
		changedMark := "   "
		if c.Changed {
			changedMark = " " + theme.ChangedDot() + " "
		}
		nameStyled := c.Name
		if i == m.Cursor {
			nameStyled = theme.StyleTitle.Render(c.Name)
		}
		line := fmt.Sprintf(" %s %s  %s  %s",
			glyph,
			padStyled(nameStyled, c.Name, nameW),
			pad(c.Type, typeW),
			changedMark,
		)
		if i == m.Cursor {
			bar := theme.StyleCursorBar.Render("▌")
			b.WriteString(bar + theme.StyleTableRowSelected.Render(line))
		} else if i%2 == 1 {
			b.WriteString(" " + theme.StyleTableRowAlt.Render(line))
		} else {
			b.WriteString(" " + theme.StyleTableRow.Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(theme.StyleDim.Render(
		"enter open · g generate plan · d dry-run · / search · : commands"))
	return b.String()
}

func pad(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) > w {
		if w <= 1 {
			return s[:w]
		}
		return s[:w-1] + "…"
	}
	return s + strings.Repeat(" ", w-lipgloss.Width(s))
}

// padStyled pads `styled` (which may contain ANSI) so its *visible* width
// equals w, using the unstyled `raw` to measure.
func padStyled(styled, raw string, w int) string {
	rawW := lipgloss.Width(raw)
	if rawW >= w {
		if w <= 1 {
			return raw[:w]
		}
		return raw[:w-1] + "…"
	}
	return styled + strings.Repeat(" ", w-rawW)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func zoa(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func statusPillFor(s string) string {
	switch s {
	case "success":
		return theme.StylePillSuccess.Render("● ok")
	case "failed":
		return theme.StylePillError.Render("● fail")
	case "running":
		return theme.StylePillRunning.Render("● run")
	default:
		return theme.StylePillIdle.Render("○ idle")
	}
}

// centerCard renders a centered message card with a hint.
func centerCard(width, height int, msg string) string {
	if width <= 0 {
		width = 40
	}
	// Clamp the message to fit comfortably inside the available width.
	maxMsg := width - 8
	if maxMsg < 12 {
		maxMsg = 12
	}
	if len(msg) > maxMsg {
		// soft-wrap by inserting line breaks at word boundaries
		words := strings.Fields(msg)
		var lines []string
		cur := ""
		for _, w := range words {
			if cur == "" {
				cur = w
				continue
			}
			if len(cur)+1+len(w) > maxMsg {
				lines = append(lines, cur)
				cur = w
			} else {
				cur += " " + w
			}
		}
		if cur != "" {
			lines = append(lines, cur)
		}
		msg = strings.Join(lines, "\n")
	}
	card := theme.StyleModalCard.Render(theme.StyleDim.Render(msg))
	if height <= 0 {
		return lipgloss.Place(width, 6, lipgloss.Center, lipgloss.Center, card)
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, card)
}
