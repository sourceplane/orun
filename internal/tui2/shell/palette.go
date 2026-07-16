package shell

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui2/design"
	"github.com/sourceplane/orun/internal/tui2/frame"
)

// Palette is the command palette overlay: type to filter the registry,
// enter to run.
type Palette struct {
	reg   *Registry
	query string
	sel   int
	rev   int
}

// NewPalette opens a palette over reg.
func NewPalette(reg *Registry) *Palette { return &Palette{reg: reg} }

func (p *Palette) matches() []Command { return p.reg.Match(p.query) }

// HandleKey implements Overlay.
func (p *Palette) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	p.rev++
	switch msg.String() {
	case "enter":
		m := p.matches()
		if len(m) == 0 {
			return nil, true
		}
		if p.sel >= len(m) {
			p.sel = len(m) - 1
		}
		return m[p.sel].Run(), true
	case "up", "ctrl+p":
		if p.sel > 0 {
			p.sel--
		}
	case "down", "ctrl+n":
		if p.sel < len(p.matches())-1 {
			p.sel++
		}
	case "backspace":
		if p.query != "" {
			p.query = p.query[:len(p.query)-1]
			p.sel = 0
		}
	default:
		if msg.Type == tea.KeyRunes && !msg.Alt {
			p.query += string(msg.Runes)
			p.sel = 0
		}
	}
	return nil, false
}

// Rev implements Overlay.
func (p *Palette) Rev() string { return "palette/" + strconv.Itoa(p.rev) }

// View implements Overlay.
func (p *Palette) View(max frame.Size) string {
	w := min(60, max.Width-8)
	if w < 16 {
		w = max.Width - 4
	}
	rows := min(10, max.Height-6)
	if rows < 1 {
		rows = 1
	}

	lines := []string{
		frame.FitLine(design.Accent.Render("›")+" "+p.query+design.Muted.Render("▏"), w),
		design.Rule(w),
	}
	m := p.matches()
	for i, c := range m {
		if i >= rows {
			break
		}
		marker, title := "  ", design.Text.Render(c.Title)
		if i == p.sel {
			marker, title = design.Selected.Render("▸ "), design.Selected.Render(c.Title)
		}
		keys := ""
		if len(c.Keys) > 0 {
			keys = "  " + design.Dim.Render(strings.Join(c.Keys, " "))
		}
		lines = append(lines, marker+title+keys)
	}
	if len(m) == 0 {
		lines = append(lines, design.Dim.Render("no matching commands"))
	}
	return design.Box("", strings.Join(lines, "\n"), max)
}
