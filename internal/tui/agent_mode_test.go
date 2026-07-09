package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
)

// TestModel_AgentModeSwitchAndLoad drives the '3' key into the Agent surface,
// runs the loadAgentTypesCmd against a mock, and asserts the rows land and the
// view renders them — the AG3 mode integration end to end at the model level.
func TestModel_AgentModeSwitchAndLoad(t *testing.T) {
	svc := &services.MockOrunService{
		LoadAgentTypesFn: func(ctx context.Context) ([]services.AgentTypeRow, error) {
			return []services.AgentTypeRow{
				{Name: "implementer", Harness: "claude-code", Model: "claude-opus-4-8", Owner: "team/pay"},
			}, nil
		},
	}
	m := NewModel(svc)
	m.width = 120
	m.loading = false

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	m = next.(Model)
	if m.ActiveMode() != ModeAgent {
		t.Fatalf("ActiveMode = %v, want ModeAgent", m.ActiveMode())
	}
	if cmd == nil {
		t.Fatal("switching to agent mode did not return a load command")
	}
	// Execute the load command and feed its message back.
	next, _ = m.Update(cmd())
	m = next.(Model)
	if len(m.agent.Types) != 1 || m.agent.Types[0].Name != "implementer" {
		t.Fatalf("agent types not loaded: %+v", m.agent.Types)
	}
	if body := m.renderStage(); !contains(body, "implementer") || !contains(body, "Agent types") {
		t.Fatalf("agent stage not rendered:\n%s", body)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return len(sub) == 0
}
