// Package shell is the cockpit v2 kernel: the root Bubble Tea model, the
// surface router, the overlay stack, the focus rules, and the command
// registry. It owns navigation and chrome and nothing else — surfaces own
// their content, the frame package owns rendering discipline, the store owns
// state (specs/orun-tui-v2/design.md §6).
package shell

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui2/frame"
)

// Surface is one of the cockpit's top-level screens (Home, Work, Agents, …).
// Surfaces are registered as pointers and mutate themselves — the v1 pattern
// of copying child models by value is gone.
type Surface interface {
	// ID is the stable surface identifier ("agents"). Command IDs and deep
	// links are built from it.
	ID() string
	// Title is the header-tab label ("Agents").
	Title() string
	// Init returns the surface's startup command, if any.
	Init() tea.Cmd
	// Update folds a non-key message. Key events arrive via HandleKey.
	Update(msg tea.Msg) tea.Cmd
	// HandleKey folds a key event, reporting whether it was consumed.
	// Unconsumed keys fall through to nothing — the shell has already taken
	// its globals before offering the key here.
	HandleKey(msg tea.KeyMsg) (tea.Cmd, bool)
	// Pop leaves one drill level, reporting whether there was one to leave.
	// The shell routes esc here first; a false return sends esc to surface
	// history instead.
	Pop() bool
	// InputFocused reports whether a text input owns the keyboard. While
	// true the shell forwards unchorded printables to the surface untouched
	// (design §4e: no single-letter global can ever eat typed text).
	InputFocused() bool
	// Rev is the surface's render revision: any string that changes exactly
	// when the surface's next View would differ. It is folded into the
	// stage memo key.
	Rev() string
	// View renders the surface at exactly size (frame.Fit is the usual last
	// step).
	View(size frame.Size) string
}

// ScheduledSurface is implemented by surfaces that animate. The shell hands
// them the one scheduler at construction; they register animator ids while
// live and remove them when done.
type ScheduledSurface interface {
	Surface
	SetScheduler(*frame.Scheduler)
}
