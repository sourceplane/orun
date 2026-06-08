package views

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/sourceplane/orun/internal/tui/services"
)

func historyWith(n int) HistoryModel {
	runs := make([]services.RunSummary, 0, n)
	for i := 0; i < n; i++ {
		runs = append(runs, services.RunSummary{ExecID: fmt.Sprintf("exec-%02d", i)})
	}
	return HistoryModel{Runs: runs, Width: 120, Height: 20}
}

// TestHistory_ViewClipsToHeight guards against the history list overflowing past
// the top of the stage: with more runs than fit, the rendered View must stay
// within the height budget instead of pushing rows off-screen.
func TestHistory_ViewClipsToHeight(t *testing.T) {
	m := historyWith(50)

	out := m.View()
	if h := lipgloss.Height(out); h > m.Height {
		t.Fatalf("View rendered %d lines, exceeds height budget %d", h, m.Height)
	}
}

// TestHistory_ViewScrollsToCursor verifies the viewport follows the cursor so
// the selected row stays visible even when it sits far past the first screenful.
func TestHistory_ViewScrollsToCursor(t *testing.T) {
	m := historyWith(50)
	m.Cursor = 49 // last row

	out := m.View()
	if !strings.Contains(out, "exec-49") {
		t.Fatal("View did not scroll to show the cursor row exec-49")
	}
	if lipgloss.Height(out) > m.Height {
		t.Fatalf("View exceeds height budget %d while scrolled", m.Height)
	}
}
