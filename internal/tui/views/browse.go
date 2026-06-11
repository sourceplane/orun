package views

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

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
	// ChangedOnly is the Q2 show-only-changed filter (toggled with `c`): when
	// set, only components in the changed/affected overlay are listed.
	ChangedOnly bool
}

func NewBrowseModel() BrowseModel { return BrowseModel{} }

func (m BrowseModel) Init() tea.Cmd { return nil }

// SetFilter sets the case-insensitive substring filter for component rows.
func (m BrowseModel) SetFilter(f string) BrowseModel {
	m.Filter = f
	m.Cursor = 0
	return m
}

// ToggleChangedOnly flips the show-only-changed overlay filter, resetting the
// cursor so it never points past the (now shorter) list.
func (m BrowseModel) ToggleChangedOnly() BrowseModel {
	m.ChangedOnly = !m.ChangedOnly
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
	if m.Filter == "" && !m.ChangedOnly {
		return m.Workspace.Components
	}
	f := strings.ToLower(m.Filter)
	out := make([]services.ComponentSummary, 0, len(m.Workspace.Components))
	for _, c := range m.Workspace.Components {
		if m.ChangedOnly && !c.Changed {
			continue
		}
		if f != "" &&
			!strings.Contains(strings.ToLower(c.Name), f) &&
			!strings.Contains(strings.ToLower(c.Type), f) &&
			!strings.Contains(strings.ToLower(c.Domain), f) {
			continue
		}
		out = append(out, c)
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
		case "c":
			m = m.ToggleChangedOnly()
		case "enter":
			if sel := m.Selected(); sel != nil {
				name := sel.Name
				return m, func() tea.Msg { return ComponentOpenMsg{Name: name} }
			}
		case "g":
			if sel := m.Selected(); sel != nil {
				name := sel.Name
				return m, func() tea.Msg { return ComponentEnterMsg{Name: name} }
			}
		}
	}
	return m, nil
}

// ComponentOpenMsg signals the user pressed `enter` on a component row to drill
// into its detail page (the catalog→component→job→logs drill-down, consumers.md
// §3). The root model opens the Component page scoped to the named component.
type ComponentOpenMsg struct {
	Name string
}

// ComponentEnterMsg signals the user pressed `g` on a component row to compose
// a plan for it. The root model handles this by opening Component Studio scoped
// to the named component and kicking off an auto-generate.
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
	if m.ChangedOnly {
		headerL += "  " + theme.StyleChipAccent.Render("changed-only")
	}
	if m.Filter != "" {
		headerL += "  " + theme.StyleDim.Render(fmt.Sprintf("(filter: %s)", m.Filter))
	}

	var b strings.Builder
	b.WriteString(headerL)
	b.WriteString("\n\n")

	if len(rows) == 0 {
		hint := "No components yet — try `orun init` or generate a plan."
		switch {
		case m.ChangedOnly:
			hint = "No changed components — press c to show all."
		case m.Filter != "":
			hint = fmt.Sprintf("No components match %q.", m.Filter)
		}
		b.WriteString(centerCard(width, m.Height-4, hint))
		return b.String()
	}

	// Column widths. The OWNER column (envelope ownership, SC1b) only renders
	// when the catalog-served list actually carries owners and the stage is
	// wide enough to keep NAME readable.
	showOwner := false
	for _, c := range m.Workspace.Components {
		if c.Owner != "" {
			showOwner = true
			break
		}
	}
	if width < 64 {
		showOwner = false
	}
	nameW := clamp(width*45/100, 16, 56)
	typeW := clamp(width*20/100, 8, 24)
	ownerW := 0
	if showOwner {
		ownerW = clamp(width*22/100, 10, 26)
		nameW = clamp(width*32/100, 14, 40)
	}

	headerCols := fmt.Sprintf(" %s  %s  %s", pad("NAME", nameW), pad("TYPE", typeW), "CHG")
	if showOwner {
		headerCols = fmt.Sprintf(" %s  %s  %s  %s",
			pad("NAME", nameW), pad("OWNER", ownerW), pad("TYPE", typeW), "CHG")
	}
	header := theme.StyleTableHeader.Render(headerCols)
	b.WriteString(" " + header)
	b.WriteString("\n")

	// Viewport: clip rows to the available height and scroll with the cursor so
	// the list never overflows past the top of the stage.
	maxRows := m.Height - 6
	if maxRows < 3 {
		maxRows = 3
	}
	start, end := viewportWindow(m.Cursor, len(rows), maxRows)

	for i := start; i < end; i++ {
		c := rows[i]
		glyph := theme.StatusGlyph(c.LastRunStatus)
		changedMark := "   "
		switch c.ChangeKind {
		case "changed":
			changedMark = " " + theme.ChangedDot() + " "
		case "affected":
			changedMark = " " + theme.AffectedDot() + " "
		default:
			if c.Changed {
				changedMark = " " + theme.ChangedDot() + " "
			}
		}
		nameStyled := c.Name
		if i == m.Cursor {
			nameStyled = theme.StyleTitle.Render(c.Name)
		}
		var line string
		if showOwner {
			line = fmt.Sprintf(" %s %s  %s  %s  %s",
				glyph,
				padStyled(nameStyled, c.Name, nameW),
				pad(zoa(c.Owner), ownerW),
				pad(c.Type, typeW),
				changedMark,
			)
		} else {
			line = fmt.Sprintf(" %s %s  %s  %s",
				glyph,
				padStyled(nameStyled, c.Name, nameW),
				pad(c.Type, typeW),
				changedMark,
			)
		}
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
		"enter open · g compose · e env · c changed-only · / search · : commands"))
	return b.String()
}

