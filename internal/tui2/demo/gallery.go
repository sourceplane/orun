package demo

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui2/design"
	"github.com/sourceplane/orun/internal/tui2/frame"
	"github.com/sourceplane/orun/internal/tui2/shell"
)

// gallerySurface renders the Northwind Mono kit with fixture data — the
// terminal counterpart of the console's /demo page. j/k scroll.
type gallerySurface struct {
	offset int
	rev    int
}

// NewGallery returns the design-system gallery surface.
func NewGallery() shell.Surface { return &gallerySurface{} }

func (g *gallerySurface) ID() string             { return "gallery" }
func (g *gallerySurface) Title() string          { return "Gallery" }
func (g *gallerySurface) Init() tea.Cmd          { return nil }
func (g *gallerySurface) Update(tea.Msg) tea.Cmd { return nil }
func (g *gallerySurface) Pop() bool {
	if g.offset > 0 {
		g.offset = 0
		g.rev++
		return true
	}
	return false
}
func (g *gallerySurface) InputFocused() bool { return false }
func (g *gallerySurface) Rev() string        { return strconv.Itoa(g.rev) }

func (g *gallerySurface) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "down", "j":
		g.offset++
		g.rev++
		return nil, true
	case "up", "k":
		if g.offset > 0 {
			g.offset--
			g.rev++
		}
		return nil, true
	}
	return nil, false
}

func (g *gallerySurface) View(size frame.Size) string {
	lines := strings.Split(design.Gallery(size.Width), "\n")
	if g.offset > len(lines)-size.Height {
		g.offset = max(0, len(lines)-size.Height)
	}
	if g.offset < len(lines) {
		lines = lines[g.offset:]
	}
	return frame.Fit(strings.Join(lines, "\n"), size)
}
