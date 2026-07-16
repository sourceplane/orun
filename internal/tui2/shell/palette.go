package shell

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui2/frame"
)

// Palette is the command palette overlay: type to filter the registry,
// enter to run. It renders plainly in TR0; Northwind Mono restyles it in
// TR1 without touching its behavior.
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
	w := min(64, max.Width-4)
	if w < 20 {
		w = max.Width
	}
	rows := min(10, max.Height-4)
	if rows < 1 {
		rows = 1
	}

	var b strings.Builder
	fmt.Fprintf(&b, "┌%s┐\n", strings.Repeat("─", w-2))
	fmt.Fprintf(&b, "│ › %s\n", p.query)
	fmt.Fprintf(&b, "├%s┤\n", strings.Repeat("─", w-2))
	m := p.matches()
	shown := 0
	for i, c := range m {
		if shown >= rows {
			break
		}
		marker := "  "
		if i == p.sel {
			marker = "▸ "
		}
		keys := ""
		if len(c.Keys) > 0 {
			keys = "  " + strings.Join(c.Keys, " ")
		}
		fmt.Fprintf(&b, "│ %s%s%s\n", marker, c.Title, keys)
		shown++
	}
	if len(m) == 0 {
		b.WriteString("│   no matching commands\n")
		shown = 1
	}
	fmt.Fprintf(&b, "└%s┘", strings.Repeat("─", w-2))

	return frame.Fit(b.String(), frame.Size{Width: w, Height: shown + 4})
}
