// Package tui2 is the cockpit v2 (specs/orun-tui-v2): the terminal head of
// orun cloud, rebuilt on a kernel that guarantees frame stability by
// construction. It is gated behind `orun tui --next` / ORUN_TUI=next until
// TR8 flips the default.
package tui2

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui2/data"
	"github.com/sourceplane/orun/internal/tui2/demo"
	"github.com/sourceplane/orun/internal/tui2/frame"
	"github.com/sourceplane/orun/internal/tui2/shell"
	"github.com/sourceplane/orun/internal/tui2/surfaces/agents"
)

// Options locates the workspace the cockpit reads.
type Options struct {
	// OrunRoot is the absolute .orun directory; empty falls back to the
	// seeded mock workspace (the demo).
	OrunRoot string
	// WorkspaceRoot is the intent root (change-overlay base).
	WorkspaceRoot string
}

// NewProgram builds the cockpit v2 program: the real surfaces as they land
// (Agents since TR3), demo placeholders for the rest.
func NewProgram(opts Options) *tea.Program {
	var src data.Source
	if opts.OrunRoot == "" {
		src = data.SampleMock()
	} else {
		src = data.NewLocal(data.LocalConfig{OrunRoot: opts.OrunRoot, WorkspaceRoot: opts.WorkspaceRoot})
	}

	surfaces := []shell.Surface{
		demo.NewHome(),
		agents.New(src),
		demo.NewActivity(),
		demo.NewGallery(),
	}
	sh := shell.New(shell.Config{Surfaces: surfaces, Scope: src.Scope()})
	return tea.NewProgram(frame.WithProfiling(sh), tea.WithAltScreen())
}
