package views

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/agent/attach"
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
	if !strings.Contains(m.View(), "author agents/<name>.md") {
		t.Fatalf("empty state missing:\n%s", m.View())
	}
}

func TestAgentCursorNavigatesSessionsThenTypes(t *testing.T) {
	m := NewAgentModel(sampleTypes())
	m.Sessions = []services.LiveSessionRow{{SessionID: "as_1", State: "running", Task: "ORN-1"}}
	// Cursor 0 = the session; the type selection starts one past it.
	if m.SelectedSession() == nil || m.SelectedSession().SessionID != "as_1" {
		t.Fatalf("cursor 0 should select the session")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.SelectedSession() != nil || m.Selected() == nil || m.Selected().Name != "implementer" {
		t.Fatalf("cursor 1 should select the first type, got session=%v type=%v", m.SelectedSession(), m.Selected())
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.Selected().Name != "orchestrator" {
		t.Fatalf("cursor 2 = %s", m.Selected().Name)
	}
	// Can't overrun (1 session + 2 types = 3 rows, max cursor 2).
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.Cursor != 2 {
		t.Fatalf("cursor overran: %d", m.Cursor)
	}
}

func TestAgentFilter(t *testing.T) {
	m := NewAgentModel(sampleTypes()).SetFilter("orch")
	if len(m.filteredTypes()) != 1 || m.filteredTypes()[0].Name != "orchestrator" {
		t.Fatalf("filter = %v", m.filteredTypes())
	}
}

// fakeHead is an in-memory AgentHead: it records inputs and lets the test push
// frames, exercising the conversation without a socket (design §2 — the head
// is transport-agnostic by construction).
type fakeHead struct {
	ch       chan attach.Frame
	steers   []string
	verdicts []string
	detached bool
	verdictErr error
}

func newFakeHead() *fakeHead { return &fakeHead{ch: make(chan attach.Frame, 64)} }

func (h *fakeHead) Frames() <-chan attach.Frame { return h.ch }
func (h *fakeHead) Steer(text string) error     { h.steers = append(h.steers, text); return nil }
func (h *fakeHead) Verdict(id string, ok bool, reason string) error {
	h.verdicts = append(h.verdicts, id)
	return h.verdictErr
}
func (h *fakeHead) Interrupt() error { return nil }
func (h *fakeHead) End() error       { return nil }
func (h *fakeHead) Detach()          { h.detached = true }
func (h *fakeHead) push(f attach.Frame) { h.ch <- f }

// feed applies the next frame from the head to the model.
func feed(m AgentModel) AgentModel {
	m, _ = m.Update(WaitForFrame(m.frames)())
	return m
}

func TestAgentConversationStreamsAndSteers(t *testing.T) {
	h := newFakeHead()
	m := NewAgentModel(sampleTypes())
	m.Width = 120
	m, cmd := m.Attach(h)
	if cmd == nil {
		t.Fatal("Attach returned no wait command")
	}
	if !m.Attached() || !m.ComposerFocused() {
		t.Fatal("attach should focus the composer")
	}

	h.push(attach.HelloFrame(attach.SessionInfo{SessionID: "as_x", RunKind: "interactive"}, "running", 0))
	m = feed(m)
	h.push(attach.LiveFrame(0))
	m = feed(m)
	if !m.live {
		t.Fatal("live marker not folded")
	}

	// A delta then the final message: the activity line clears, the turn lands.
	h.push(attach.DeltaFrame(1, "imple"))
	m = feed(m)
	if !strings.Contains(m.View(), "imple") {
		t.Fatalf("delta not shown as activity:\n%s", m.View())
	}
	h.push(attach.EventFrame(1, "message_agent", "", map[string]any{"text": "implementing ORN-142"}, ""))
	m = feed(m)
	out := m.View()
	if !strings.Contains(out, "implementing ORN-142") {
		t.Fatalf("agent turn not rendered:\n%s", out)
	}

	// Type a steer: enter sends it through the head, composer clears.
	m.composer.SetValue("also update the changelog")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(h.steers) != 1 || h.steers[0] != "also update the changelog" {
		t.Fatalf("steer not sent: %v", h.steers)
	}
	if m.composer.Value() != "" {
		t.Fatal("composer not cleared after send")
	}
}

func TestAgentApprovalCardIsStickyAndAnswerable(t *testing.T) {
	h := newFakeHead()
	m := NewAgentModel(nil)
	m.Width = 120
	m, _ = m.Attach(h)
	h.push(attach.EventFrame(3, "approval_requested", "", map[string]any{"requestId": "req-1", "tool": "contract_propose"}, ""))
	m = feed(m)
	out := m.View()
	if !strings.Contains(out, "approval") || !strings.Contains(out, "contract_propose") || !strings.Contains(out, "ctrl+y") {
		t.Fatalf("approval card not rendered:\n%s", out)
	}
	// ctrl+y answers the oldest pending approval through the head.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	if len(h.verdicts) != 1 || h.verdicts[0] != "req-1" {
		t.Fatalf("verdict not sent: %v", h.verdicts)
	}
	// The resolution frame clears the sticky card.
	h.push(attach.EventFrame(4, "approval_resolved", "", map[string]any{"requestId": "req-1", "approved": true, "principal": "usr_cli"}, ""))
	m = feed(m)
	if len(m.pending) != 0 {
		t.Fatalf("approval still pending after resolution: %v", m.pending)
	}
	if strings.Contains(m.View(), "ctrl+y approve") {
		t.Fatalf("resolved approval card still sticky:\n%s", m.View())
	}
}

func TestAgentDetachKeepsSessionAndBlursComposer(t *testing.T) {
	h := newFakeHead()
	m := NewAgentModel(nil)
	m, _ = m.Attach(h)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if !h.detached {
		t.Fatal("ctrl+d should detach the head")
	}
	if m.Attached() || m.ComposerFocused() {
		t.Fatal("detach should drop the head and blur the composer")
	}
}

func TestAgentFeedCloseEndsLive(t *testing.T) {
	h := newFakeHead()
	m := NewAgentModel(nil)
	m, _ = m.Attach(h)
	h.push(attach.LiveFrame(0))
	m = feed(m)
	close(h.ch)
	m, _ = m.Update(WaitForFrame(m.frames)())
	if m.live || m.Attached() {
		t.Fatalf("closed feed should end live and drop the head")
	}
}
