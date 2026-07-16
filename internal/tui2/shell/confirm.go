package shell

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui2/design"
	"github.com/sourceplane/orun/internal/tui2/frame"
)

// Confirm is the one confirmation dialog in the cockpit: a titled box
// whose primary action names its verb (design §4e — "Run 4 jobs", never
// "OK"). enter runs, esc (shell-handled) cancels.
type Confirm struct {
	title string
	body  []string
	verb  string
	run   func() tea.Cmd
}

// NewConfirm builds a confirmation overlay; run fires on enter.
func NewConfirm(title string, body []string, verb string, run func() tea.Cmd) *Confirm {
	return &Confirm{title: title, body: body, verb: verb, run: run}
}

// HandleKey implements Overlay.
func (c *Confirm) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if msg.String() == "enter" {
		return c.run(), true
	}
	return nil, false
}

// Rev implements Overlay; a confirm never changes once open.
func (c *Confirm) Rev() string { return "confirm" }

// View implements Overlay.
func (c *Confirm) View(max frame.Size) string {
	return design.Dialog(c.title, c.body, c.verb, max)
}
