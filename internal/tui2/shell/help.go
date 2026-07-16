package shell

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui2/frame"
)

// Help is the `?` overlay. It is generated from the command registry —
// there is deliberately no way to add a hand-written entry (design §13.4):
// if an action deserves help, it deserves to be a command.
type Help struct {
	reg *Registry
}

// NewHelp opens help over reg.
func NewHelp(reg *Registry) *Help { return &Help{reg: reg} }

// HandleKey implements Overlay: any key closes help.
func (h *Help) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) { return nil, true }

// Rev implements Overlay; help is static per registry.
func (h *Help) Rev() string { return "help" }

// View implements Overlay.
func (h *Help) View(max frame.Size) string {
	w := min(56, max.Width-4)
	if w < 20 {
		w = max.Width
	}

	keyWidth := 0
	for _, c := range h.reg.All() {
		if kw := len(strings.Join(c.Keys, " ")); kw > keyWidth {
			keyWidth = kw
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "┌%s┐\n", strings.Repeat("─", w-2))
	b.WriteString("│ keys\n")
	fmt.Fprintf(&b, "├%s┤\n", strings.Repeat("─", w-2))
	rows := 0
	for _, c := range h.reg.All() {
		if rows >= max.Height-5 {
			break
		}
		fmt.Fprintf(&b, "│ %-*s  %s\n", keyWidth, strings.Join(c.Keys, " "), c.Title)
		rows++
	}
	fmt.Fprintf(&b, "└%s┘", strings.Repeat("─", w-2))
	return frame.Fit(b.String(), frame.Size{Width: w, Height: rows + 4})
}
