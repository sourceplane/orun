package views

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sourceplane/orun/internal/tui/theme"
)

// CommandPaletteCommand is one selectable action in the floating palette.
type CommandPaletteCommand struct {
	ID      string // stable identifier handed to the root model
	Title   string // display label
	Hint    string // dim subtitle (right-aligned)
	Keyword string // extra fuzzy-match text
}

// DefaultPaletteCommands returns the canonical palette command set.
func DefaultPaletteCommands() []CommandPaletteCommand {
	return []CommandPaletteCommand{
		{ID: "goto.browse", Title: "Go to Components", Hint: "1", Keyword: "browse"},
		{ID: "goto.plan", Title: "Compose component (last opened)", Hint: ""},
		{ID: "goto.run", Title: "Go to Activity", Hint: "2", Keyword: "runs history"},
		{ID: "goto.catalog", Title: "Go to Catalog (entities)", Hint: "3", Keyword: "entity api resource system domain group composition environment deployment"},
		{ID: "goto.logs", Title: "Go to Log Explorer", Keyword: "logs"},
		{ID: "goto.history", Title: "Go to History", Keyword: "past runs"},
		{ID: "plan.generate", Title: "Generate plan", Hint: "g", Keyword: "compile build"},
		{ID: "plan.save", Title: "Save current plan", Hint: "s"},
		{ID: "plan.dryrun", Title: "Dry-run current plan", Hint: "d"},
		{ID: "workspace.reload", Title: "Reload workspace", Hint: "⌃r"},
		{ID: "catalog.refresh", Title: "Refresh catalog (re-resolve workspace)", Keyword: "resolve sync stale"},
		{ID: "catalog.autorefresh", Title: "Toggle catalog auto-refresh", Keyword: "watch live stale"},
		{ID: "ui.toggle.inspector", Title: "Toggle inspector drawer", Hint: "i"},
		{ID: "ui.toggle.sidebar", Title: "Toggle sidebar", Hint: "tab"},
		{ID: "app.quit", Title: "Quit Orun cockpit", Hint: "q"},
	}
}

// CommandPaletteModel implements a hand-rolled fuzzy palette to keep the
// look consistent with the rest of the cockpit.
type CommandPaletteModel struct {
	Visible  bool
	commands []CommandPaletteCommand
	input    textinput.Model
	cursor   int
	width    int
}

// PaletteCommandSelectedMsg is dispatched when the user activates a row.
type PaletteCommandSelectedMsg struct {
	Command CommandPaletteCommand
}

func NewCommandPaletteModel() CommandPaletteModel {
	ti := textinput.New()
	ti.Placeholder = "type to search commands…"
	ti.Prompt = "› "
	ti.CharLimit = 64
	return CommandPaletteModel{
		commands: DefaultPaletteCommands(),
		input:    ti,
	}
}

// Open shows the palette and focuses the input.
func (m CommandPaletteModel) Open() CommandPaletteModel {
	m.Visible = true
	m.cursor = 0
	m.input.SetValue("")
	m.input.Focus()
	return m
}

// Close hides the palette and blurs input.
func (m CommandPaletteModel) Close() CommandPaletteModel {
	m.Visible = false
	m.input.Blur()
	return m
}

// SetWidth lets the root model control sizing.
func (m CommandPaletteModel) SetWidth(w int) CommandPaletteModel {
	m.width = w
	m.input.Width = clamp(w-6, 10, 80)
	return m
}

// Update consumes key input when visible; emits PaletteCommandSelectedMsg
// when the user hits enter on a row.
func (m CommandPaletteModel) Update(msg tea.Msg) (CommandPaletteModel, tea.Cmd) {
	if !m.Visible {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "down", "ctrl+n":
			matches := m.Matches()
			if m.cursor+1 < len(matches) {
				m.cursor++
			}
			return m, nil
		case "up", "ctrl+p":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "enter":
			matches := m.Matches()
			if len(matches) == 0 {
				return m, nil
			}
			if m.cursor >= len(matches) {
				m.cursor = len(matches) - 1
			}
			cmd := matches[m.cursor]
			return m, func() tea.Msg { return PaletteCommandSelectedMsg{Command: cmd} }
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.cursor = 0
	return m, cmd
}

// Matches returns the filtered, ranked command list for the current query.
func (m CommandPaletteModel) Matches() []CommandPaletteCommand {
	q := strings.ToLower(strings.TrimSpace(m.input.Value()))
	if q == "" {
		out := make([]CommandPaletteCommand, len(m.commands))
		copy(out, m.commands)
		return out
	}
	type ranked struct {
		score int
		cmd   CommandPaletteCommand
	}
	var ranks []ranked
	for _, c := range m.commands {
		hay := strings.ToLower(c.Title + " " + c.Keyword + " " + c.ID)
		score := fuzzyScore(hay, q)
		if score > 0 {
			ranks = append(ranks, ranked{score, c})
		}
	}
	sort.SliceStable(ranks, func(i, j int) bool { return ranks[i].score > ranks[j].score })
	out := make([]CommandPaletteCommand, len(ranks))
	for i, r := range ranks {
		out[i] = r.cmd
	}
	return out
}

func fuzzyScore(hay, q string) int {
	if strings.Contains(hay, q) {
		return 100 + (len(q) * 2)
	}
	// Subsequence match — every rune of q appears in hay in order.
	hi := 0
	matched := 0
	for _, r := range q {
		idx := strings.IndexRune(hay[hi:], r)
		if idx < 0 {
			return 0
		}
		hi += idx + 1
		matched++
	}
	if matched == len(q) {
		return 30 + matched
	}
	return 0
}

func (m CommandPaletteModel) View() string {
	if !m.Visible {
		return ""
	}
	w := m.width
	if w <= 0 {
		w = 60
	}
	cardW := clamp(w*60/100, 40, 90)

	matches := m.Matches()
	var rows []string
	maxRows := 8
	for i, c := range matches {
		if i >= maxRows {
			break
		}
		left := c.Title
		right := theme.StyleDim.Render(c.Hint)
		spacing := cardW - 6 - lipgloss.Width(left) - lipgloss.Width(c.Hint)
		if spacing < 1 {
			spacing = 1
		}
		line := left + strings.Repeat(" ", spacing) + right
		if i == m.cursor {
			rows = append(rows, theme.StyleCursorBar.Render("▌")+theme.StyleTableRowSelected.Render(line))
		} else {
			rows = append(rows, " "+theme.StyleTableRow.Render(line))
		}
	}
	if len(rows) == 0 {
		rows = append(rows, theme.StyleDim.Render("  no matches"))
	}

	body := strings.Join([]string{
		theme.StyleModalTitle.Render("⌘ Command Palette"),
		m.input.View(),
		theme.StyleMuted.Render(strings.Repeat("─", cardW-6)),
		strings.Join(rows, "\n"),
		theme.StyleDim.Render("↑↓ navigate · enter run · esc close"),
	}, "\n")

	return theme.StyleModalCard.Width(cardW).Render(body)
}
