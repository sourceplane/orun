package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/views"
)

// TestModel_ScrollWhileLiveStreamKeepsFrameStable reproduces the reported
// corruption scenario: a live job's step view receiving log batches (info +
// error lines, so the errors card grows) while the user scrolls. EVERY
// intermediate frame must be exactly terminal-height and within terminal
// width — the invariant that keeps bubbletea's line-diff renderer in sync
// with the terminal (a single over-tall or over-wide frame scrolls the alt
// screen one row and every later diff paints against stale rows: doubled
// headers, doubled footers, ghost sidebar items).
func TestModel_ScrollWhileLiveStreamKeepsFrameStable(t *testing.T) {
	const w, h = 204, 52 // the terminal size from the report
	m := NewModel(&services.MockOrunService{})
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = next.(Model)
	m.loading = false

	plan := &model.Plan{Jobs: []model.PlanJob{
		{ID: "admin-worker-tests.dev.verify", Component: "admin-worker-tests", Environment: "dev",
			Steps: []model.PlanStep{{ID: "install-workspace-dependencies", Name: "install-workspace-dependencies"}}},
	}}
	m.activity = m.activity.SetRuns(&views.ActivityRun{
		ExecID: "e1", PlanName: "multi-tenant-saas", Status: "running", Live: true,
		Plan: plan, Statuses: map[string]string{"admin-worker-tests.dev.verify": "running"},
	}, nil)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")}) // → Activity
	m = next.(Model)
	for i := 0; i < 3; i++ { // index → run → job → step
		n, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = n.(Model)
	}

	long := "WARN  Failed to create bin at /Users/irinelinson/sourceplane/multi-tenant-saas/.orun/runs/multi-tenant-saas-20260611-01a85f/admin/node_modules/.bin/x"
	assertFrame := func(stage string) {
		t.Helper()
		out := m.View()
		if gh := lipgloss.Height(out); gh != h {
			t.Fatalf("%s: frame height %d, want exactly %d", stage, gh, h)
		}
		for _, line := range strings.Split(out, "\n") {
			if lw := lipgloss.Width(line); lw > w {
				t.Fatalf("%s: line width %d exceeds terminal %d:\n%q", stage, lw, w, line)
			}
		}
	}

	// Interleave live batches with scroll keys, exactly the reported usage.
	keys := []string{"k", "k", "j", "g", "G", "f", "E", "E", "f"}
	for i := 0; i < 12; i++ {
		batch := services.LogBatchMsg{}
		for j := 0; j < 8; j++ {
			line := fmt.Sprintf("Progress: resolved 1028, downloaded %d, added %d", i*8+j, i*8+j)
			if j%3 == 0 {
				line = long // grows the errors card mid-stream
			}
			batch.Events = append(batch.Events, services.LogEvent{
				JobID: "admin-worker-tests.dev.verify", StepID: "install-workspace-dependencies",
				Line: line, Timestamp: time.Unix(0, 0),
			})
		}
		n, _ := m.Update(batch)
		m = n.(Model)
		assertFrame(fmt.Sprintf("after batch %d", i))

		key := keys[i%len(keys)]
		n, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m = n.(Model)
		assertFrame(fmt.Sprintf("after key %q (batch %d)", key, i))
	}
}
