package shell

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui2/design"
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
	keyWidth := 0
	for _, c := range h.reg.All() {
		if kw := len(strings.Join(c.Keys, " ")); kw > keyWidth {
			keyWidth = kw
		}
	}

	var lines []string
	for _, c := range h.reg.All() {
		if len(lines) >= max.Height-4 {
			break
		}
		keys := frame.FitLine(strings.Join(c.Keys, " "), keyWidth)
		lines = append(lines, design.Text.Render(keys)+"  "+design.Dim.Render(c.Title))
	}
	return design.Box("keys", strings.Join(lines, "\n"), max)
}
