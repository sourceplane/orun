package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/views"
)

// The surface tab bar names all three top-level surfaces, so Agents is
// discoverable in the UI instead of only via an undocumented number key.
func TestModel_TabBarShowsSurfaces(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	tabs := m.renderTabs()
	for _, want := range []string{"Catalog", "Activity", "Agents"} {
		if !contains(tabs, want) {
			t.Errorf("tab bar missing %q: %q", want, tabs)
		}
	}
}

// Pressing `n` in the Agent surface opens the New Session dialog (rather than
// silently spawning a bare stub), and esc dismisses it.
func TestModel_NewSessionDialogOpensAndCancels(t *testing.T) {
	svc := &services.MockOrunService{
		LoadAgentTypesFn: func(ctx context.Context) ([]services.AgentTypeRow, error) {
			return []services.AgentTypeRow{
				{Name: "implementer", Harness: "claude-code", Model: "claude-opus-4-8"},
			}, nil
		},
	}
	m := NewModel(svc)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	m.loading = false

	// Enter the Agent surface and load its types.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	m = next.(Model)
	for _, msg := range execBatch(cmd) {
		next, _ = m.Update(msg)
		m = next.(Model)
	}

	// `n` opens the dialog.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = next.(Model)
	if !m.showAgentLaunch {
		t.Fatal("`n` did not open the New Session dialog")
	}
	if body := m.View(); !contains(body, "New agent session") {
		t.Fatalf("launch dialog not painted:\n%s", body)
	}

	// esc dismisses it.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.showAgentLaunch {
		t.Fatal("esc did not close the New Session dialog")
	}
}

// The LaunchSpec → `orun agent run` flag mapping: detached always, and
// driver/type/task only when set.
func TestAgentRunArgs(t *testing.T) {
	cases := []struct {
		name string
		spec views.LaunchSpec
		want []string
	}{
		{
			name: "full",
			spec: views.LaunchSpec{Driver: "claude-code", Type: "implementer", Task: "ORN-142"},
			want: []string{"agent", "run", "--detach", "--driver", "claude-code", "--type", "implementer", "--task", "ORN-142"},
		},
		{
			name: "interactive claude, no task",
			spec: views.LaunchSpec{Driver: "claude-code", Type: "implementer"},
			want: []string{"agent", "run", "--detach", "--driver", "claude-code", "--type", "implementer"},
		},
		{
			name: "ad-hoc stub",
			spec: views.LaunchSpec{Driver: "stub"},
			want: []string{"agent", "run", "--detach", "--driver", "stub"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := agentRunArgs(tc.spec)
			if len(got) != len(tc.want) {
				t.Fatalf("args = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("args = %v, want %v", got, tc.want)
				}
			}
		})
	}
}
