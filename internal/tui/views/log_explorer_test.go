package views

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/sourceplane/orun/internal/tui/services"
)

// TestLogExplorer_LinesNeverExceedWidth guards TUI frame integrity: the bubbles
// viewport does not clip horizontally, so a log line wider than the pane wraps
// in the terminal and corrupts the whole frame (doubled headers/footers). Every
// rendered row — viewport body AND the errors card — must stay within the width.
func TestLogExplorer_LinesNeverExceedWidth(t *testing.T) {
	const width, height = 80, 24
	m := NewLogExplorerModel()
	m = m.SetSize(width, height)

	// Long error lines (full filesystem paths) — the real-world trigger.
	long := "WARN Failed to create bin at /Users/irinelinson/sourceplane/multi-tenant-saas/.orun/runs/multi-tenant-saas-20260608-368372/cli/node_modules/.bin/whatever-very-long-binary-name"
	for i := 0; i < 30; i++ {
		var nm LogExplorerModel
		nm, _ = m.Update(services.LogEventMsg{Event: services.LogEvent{
			JobID: "cli.dev.verify", StepID: "install-workspace-dependencies",
			Line: long, Timestamp: time.Unix(0, 0),
		}})
		m = nm
	}

	out := m.View()
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("rendered line width %d exceeds pane width %d:\n%q", w, width, line)
		}
	}
	if h := lipgloss.Height(out); h > height {
		t.Fatalf("rendered height %d exceeds pane height %d", h, height)
	}
}
