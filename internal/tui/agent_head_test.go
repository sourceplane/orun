package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/agent"
	"github.com/sourceplane/orun/internal/agent/attach"
	"github.com/sourceplane/orun/internal/agent/driver"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/views"
)

// hostInteractiveSession spins a real interactive stub body with an attach
// server on a socket, returning the socket path and a stop func — the TUI head
// attaches to it exactly like `orun agent attach`.
func hostInteractiveSession(t *testing.T) (socket string, stop func()) {
	t.Helper()
	store := objectstore.NewMemStore(objectstore.AlgoSHA256)
	brief, err := agent.AssembleBrief(t.Context(), store, agent.BriefInput{RunKind: nodes.RunKindInteractive})
	if err != nil {
		t.Fatal(err)
	}
	q := agent.NewInputQueue()
	srv := attach.NewServer(attach.SessionInfo{SessionID: "as_tui1", RunKind: "interactive", Harness: "stub"}, q)
	sock := t.TempDir() + "/s.sock"
	ln, err := attach.ServeSocket(srv, sock)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = agent.Run(t.Context(), store, agent.RunOptions{
			SessionID: "as_tui1", Driver: &driver.Stub{Interactive: true}, Brief: brief,
			Inputs: q, Observe: srv.Observe, ObserveDelta: srv.ObserveDelta,
		})
		srv.Close("terminal")
	}()
	return sock, func() {
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	}
}

// drainInto pumps head frames into the model until pred(model) holds or the
// deadline passes. It reads the head feed with a bounded wait (never the
// blocking WaitForFrame) so a satisfied predicate returns promptly and a
// stalled body fails the test instead of hanging.
func drainInto(t *testing.T, m Model, pred func(Model) bool) Model {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if pred(m) {
			return m
		}
		select {
		case f, ok := <-m.agent.FramesForTest():
			var msg tea.Msg = views.AgentFrameMsg{Frame: f}
			if !ok {
				msg = views.AgentFrameMsg{Closed: true}
			}
			next, _ := m.Update(msg)
			m = next.(Model)
		case <-deadline:
			t.Fatalf("condition not met; view:\n%s", m.agent.View())
		}
	}
}

// TestModelAttachSteerApproveEndToEnd drives the whole AL3 head against a real
// hosted body: attach → replay/live → steer → approval card → approve → the
// resolution renders. The transport is a real socket, so this is the
// interchangeability property exercised at the model level.
func TestModelAttachSteerApproveEndToEnd(t *testing.T) {
	socket, stop := hostInteractiveSession(t)
	defer stop()

	m := NewModel(&services.MockOrunService{}).StartInAgentMode()
	m.width = 120

	// Attach through the same command the sidebar enter triggers.
	next, cmd := m.Update(agentAttachedMsgFor(t, socket))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("attach produced no stream command")
	}
	// Fold frames until live.
	m = drainInto(t, m, func(m Model) bool { return m.agent.LiveForTest() })

	// Steer through the composer.
	m.agent = m.agent.SetComposerForTest("hello from the tui")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	m = drainInto(t, m, func(m Model) bool {
		return containsStr(m.agent.View(), "echo: hello from the tui")
	})

	// Ask for a gated tool, then approve via ctrl+y.
	m.agent = m.agent.SetComposerForTest("/ask contract_propose")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	m = drainInto(t, m, func(m Model) bool { return m.agent.PendingCountForTest() == 1 })
	if !containsStr(m.agent.View(), "ctrl+y") {
		t.Fatalf("approval card not sticky:\n%s", m.agent.View())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	m = next.(Model)
	m = drainInto(t, m, func(m Model) bool { return m.agent.PendingCountForTest() == 0 })

	// End the session.
	m.agent = m.agent.SetComposerForTest("/done")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	m = drainInto(t, m, func(m Model) bool { return !m.agent.Attached() })
}

// agentAttachedMsgFor dials the socket and wraps the head like attachSessionCmd
// does, but synchronously (the test drives frames itself).
func agentAttachedMsgFor(t *testing.T, socket string) tea.Msg {
	t.Helper()
	c, err := attach.DialSocket(socket, -1, "tui")
	if err != nil {
		t.Fatal(err)
	}
	return agentAttachedMsg{head: c}
}

func containsStr(s, sub string) bool { return contains(s, sub) }
