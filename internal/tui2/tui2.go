// Package tui2 is the cockpit v2 (specs/orun-tui-v2): the terminal head of
// orun cloud, rebuilt on a kernel that guarantees frame stability by
// construction. It is gated behind `orun tui --next` / ORUN_TUI=next until
// TR8 flips the default.
package tui2

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui2/demo"
	"github.com/sourceplane/orun/internal/tui2/frame"
	"github.com/sourceplane/orun/internal/tui2/shell"
)

// NewProgram builds the cockpit v2 program. Through TR1 it hosts the demo
// surfaces; TR2 wires the data plane and the real surfaces land in TR3–TR7.
func NewProgram() *tea.Program {
	sh := shell.New(shell.Config{
		Surfaces: demo.New(),
		Scope:    "local",
	})
	return tea.NewProgram(frame.WithProfiling(sh), tea.WithAltScreen())
}
