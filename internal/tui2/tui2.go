// Package tui2 is the cockpit v2 (specs/orun-tui-v2): the terminal head of
// orun cloud, rebuilt on a kernel that guarantees frame stability by
// construction. It is gated behind `orun tui --next` / ORUN_TUI=next until
// TR8 flips the default.
package tui2

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui2/data"
	"github.com/sourceplane/orun/internal/tui2/demo"
	"github.com/sourceplane/orun/internal/tui2/frame"
	"github.com/sourceplane/orun/internal/tui2/shell"
	"github.com/sourceplane/orun/internal/tui2/surfaces/activity"
	"github.com/sourceplane/orun/internal/tui2/surfaces/agents"
	"github.com/sourceplane/orun/internal/tui2/surfaces/catalog"
	"github.com/sourceplane/orun/internal/tui2/surfaces/events"
	"github.com/sourceplane/orun/internal/tui2/surfaces/home"
	"github.com/sourceplane/orun/internal/tui2/surfaces/work"
)

// Options locates the workspace the cockpit reads.
type Options struct {
	// OrunRoot is the absolute .orun directory; empty falls back to the
	// seeded mock workspace (the demo).
	OrunRoot string
	// WorkspaceRoot is the intent root (change-overlay base).
	WorkspaceRoot string
	// IntentFile and ConfigDir feed plan generation (the Compose flow).
	IntentFile string
	ConfigDir  string
	Version    string
	// InitialSurface opens the cockpit on a specific surface ("agents"
	// for the bare `orun agent` front door). Empty = Home.
	InitialSurface string
}

// NewProgram builds the cockpit v2 program: the real surfaces as they land
// (Home/Events TR7, Work TR6, Agents TR3, Activity TR4, Catalog TR5) plus
// the design-system gallery.
func NewProgram(opts Options) *tea.Program {
	var src data.Source
	var comp data.Composer
	if opts.OrunRoot == "" {
		src = data.SampleMock()
		comp = data.SampleComposer()
	} else {
		src = data.WithCloud(
			data.NewLocal(data.LocalConfig{OrunRoot: opts.OrunRoot, WorkspaceRoot: opts.WorkspaceRoot}),
			data.ResolveCloud(context.Background(), opts.Version),
		)
		comp = data.NewLocalComposer(data.LocalComposerConfig{
			IntentFile: opts.IntentFile,
			IntentRoot: opts.WorkspaceRoot,
			ConfigDir:  opts.ConfigDir,
			OrunRoot:   opts.OrunRoot,
			Version:    opts.Version,
		})
	}

	surfaces := []shell.Surface{
		home.New(src),
		work.New(src),
		agents.New(src),
		activity.New(src),
		catalog.New(src, comp),
		events.New(src),
		demo.NewGallery(),
	}
	sh := shell.New(shell.Config{Surfaces: surfaces, Scope: src.Scope(), Version: opts.Version})
	if opts.InitialSurface != "" {
		sh.Router().ActivateID(opts.InitialSurface)
	}
	return tea.NewProgram(frame.WithProfiling(sh), tea.WithAltScreen())
}
