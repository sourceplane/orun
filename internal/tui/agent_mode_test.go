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
	// GoAgent batches loadAgentTypes + loadLiveSessions; execute each and feed
	// its message back.
	for _, msg := range execBatch(cmd) {
		next, _ = m.Update(msg)
		m = next.(Model)
	}
	if len(m.agent.Types) != 1 || m.agent.Types[0].Name != "implementer" {
		t.Fatalf("agent types not loaded: %+v", m.agent.Types)
	}
	if body := m.renderStage(); !contains(body, "implementer") || !contains(body, "Agent types") {
		t.Fatalf("agent stage not rendered:\n%s", body)
	}
}

// execBatch runs a tea.Cmd that may be a batch, flattening to the concrete
// messages. A nil cmd or nil message is skipped.
func execBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, execBatch(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return len(sub) == 0
}
