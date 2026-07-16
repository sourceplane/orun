package shell

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui2/frame"
)

// Overlay is a modal layer above the stage — palette, help, dialogs, launch
// flows. Overlays live on a LIFO stack: the top one gets keys first and esc
// always pops exactly one (the shell enforces that; overlays never see esc).
// The five independent overlay booleans of the v1 cockpit, each with its own
// precedence branch, are this stack now — precedence bugs are
// unrepresentable.
type Overlay interface {
	// HandleKey folds a key event. done=true closes the overlay after the
	// returned command is dispatched.
	HandleKey(msg tea.KeyMsg) (cmd tea.Cmd, done bool)
	// View renders the overlay's box at any size up to max; the shell
	// centers it over the stage.
	View(max frame.Size) string
	// Rev changes exactly when the overlay's next View would differ; it is
	// folded into the stage memo key.
	Rev() string
}
