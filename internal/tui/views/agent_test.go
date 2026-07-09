package views

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
)

func sampleTypes() []services.AgentTypeRow {
	return []services.AgentTypeRow{
		{Name: "implementer", Harness: "claude-code", Model: "claude-opus-4-8", Owner: "team/pay", Autonomy: "assist", MayAffect: []string{"a/b/billing-worker"}, Persona: "# Implementer\n\nOne task to a PR."},
		{Name: "orchestrator", Harness: "claude-code", Model: "claude-opus-4-8", Owner: "team/plat"},
	}
}

func TestAgentViewRendersTypes(t *testing.T) {
	m := NewAgentModel(sampleTypes())
	m.Width = 120
	out := m.View()
	for _, want := range []string{"Agent types", "implementer", "orchestrator", "claude-opus-4-8", "team/pay", "billing-worker"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q:\n%s", want, out)
		}
	}
}

func TestAgentViewEmptyState(t *testing.T) {
	m := NewAgentModel(nil)
	if !strings.Contains(m.View(), "No agent types") {
		t.Fatalf("empty state missing:\n%s", m.View())
	}
}

func TestAgentCursorAndSelection(t *testing.T) {
	m := NewAgentModel(sampleTypes())
	if m.Selected().Name != "implementer" {
		t.Fatalf("initial selection = %s", m.Selected().Name)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.Selected().Name != "orchestrator" {
		t.Fatalf("after down = %s", m.Selected().Name)
	}
	// Can't move past the end.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.Cursor != 1 {
		t.Fatalf("cursor overran: %d", m.Cursor)
	}
}

func TestAgentFilter(t *testing.T) {
	m := NewAgentModel(sampleTypes()).SetFilter("orch")
	if len(m.filtered()) != 1 || m.filtered()[0].Name != "orchestrator" {
		t.Fatalf("filter = %v", m.filtered())
	}
}

func TestAgentLiveTranscriptStream(t *testing.T) {
	ch := make(chan AgentTranscriptEvent, 3)
	m := NewAgentModel(sampleTypes())
	m.Width = 120
	m, cmd := m.StartStream(ch)
	if cmd == nil {
		t.Fatal("StartStream returned no wait command")
	}
	// Feed two lines then close; the model folds each and re-arms.
	ch <- AgentTranscriptEvent{Line: "reading brief"}
	m, _ = m.Update(cmd())
	ch <- AgentTranscriptEvent{Line: "opened PR local://x"}
	m, _ = m.Update(WaitForAgentEvent(ch)())
	out := m.View()
	if !strings.Contains(out, "Transcript (live)") || !strings.Contains(out, "reading brief") || !strings.Contains(out, "opened PR") {
		t.Fatalf("transcript not rendered:\n%s", out)
	}
	close(ch)
	m, _ = m.Update(WaitForAgentEvent(ch)())
	if strings.Contains(m.View(), "(live)") {
		t.Fatalf("stream did not end:\n%s", m.View())
	}
}
