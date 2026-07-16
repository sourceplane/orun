// Package agents is the cockpit v2 Agents surface (specs/orun-tui-v2 §9):
// the sessions list and the conversation head. The head is the same AL3
// head the v1 cockpit shipped — attach v1 over a local socket, steer/
// verdict/interrupt as attributed events — re-rendered under the kernel:
// pure fold (internal/tui2/agentfold), region-memoized paint, fs-watch
// session refresh, and a launch flow with no polling.
package agents

import (
	"context"
	"os"
	"os/exec"
	"strconv"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/agent/attach"
	"github.com/sourceplane/orun/internal/agent/live"
	"github.com/sourceplane/orun/internal/tui2/agentfold"
	"github.com/sourceplane/orun/internal/tui2/data"
	"github.com/sourceplane/orun/internal/tui2/shell"
)

// head is the slice of attach.SocketClient the surface drives; tests
// substitute a fake.
type head interface {
	Frames() <-chan attach.Frame
	Steer(text string) error
	Verdict(requestID string, approved bool, reason string) error
	Interrupt() error
	Detach()
}

// Surface implements shell.Surface.
type Surface struct {
	src    data.Source
	ctx    context.Context
	cancel context.CancelFunc

	sessions []live.Entry
	sel      int
	known    map[string]bool

	conv        *agentfold.Conversation
	client      head
	attachedID  string
	composer    textinput.Model
	expandTools bool
	scroll      int
	status      string

	launching bool
	deltas    <-chan data.Delta
	rev       int
}

// New builds the surface over src.
func New(src data.Source) *Surface {
	ctx, cancel := context.WithCancel(context.Background())
	ti := textinput.New()
	ti.Placeholder = "message the agent…  (enter steer · esc interrupt · ctrl+d detach)"
	ti.Prompt = "› "
	ti.CharLimit = 4000
	return &Surface{src: src, ctx: ctx, cancel: cancel, composer: ti, known: map[string]bool{}}
}

// --- messages ---------------------------------------------------------------

type (
	sessionsMsg struct {
		entries []live.Entry
		err     error
	}
	subscribedMsg struct{ ch <-chan data.Delta }
	deltaMsg      struct{ ok bool }
	frameMsg      struct {
		f      attach.Frame
		closed bool
	}
	attachResultMsg struct {
		h   head
		id  string
		err error
	}
	launchedMsg struct{ err error }
)

// --- commands ---------------------------------------------------------------

func (s *Surface) loadSessionsCmd() tea.Cmd {
	src, ctx := s.src, s.ctx
	return func() tea.Msg {
		entries, err := src.Sessions(ctx)
		return sessionsMsg{entries: entries, err: err}
	}
}

func (s *Surface) subscribeCmd() tea.Cmd {
	src, ctx := s.src, s.ctx
	return func() tea.Msg {
		ch, err := src.Subscribe(ctx, data.TopicSessions)
		if err != nil {
			return sessionsMsg{err: err}
		}
		return subscribedMsg{ch: ch}
	}
}

func waitDelta(ch <-chan data.Delta) tea.Cmd {
	return func() tea.Msg {
		_, ok := <-ch
		return deltaMsg{ok: ok}
	}
}

func waitFrame(ch <-chan attach.Frame) tea.Cmd {
	return func() tea.Msg {
		f, ok := <-ch
		if !ok {
			return frameMsg{closed: true}
		}
		return frameMsg{f: f}
	}
}

func attachCmd(socket, id string) tea.Cmd {
	return func() tea.Msg {
		c, err := attach.DialSocket(socket, -1, "tui")
		if err != nil {
			return attachResultMsg{err: err}
		}
		return attachResultMsg{h: c, id: id}
	}
}

// launchCmd spawns the detached body. Readiness needs no polling: the
// registry write lands as a TopicSessions delta, the list refreshes, and
// Update auto-attaches the unfamiliar session (design §9 — the 2s sleep
// loop dies here).
func launchCmd(spec LaunchSpec) tea.Cmd {
	return func() tea.Msg {
		exe, err := os.Executable()
		if err != nil {
			return launchedMsg{err: err}
		}
		args := []string{"agent", "run", "--detach"}
		if spec.Driver != "" {
			args = append(args, "--driver", spec.Driver)
		}
		if spec.Type != "" {
			args = append(args, "--type", spec.Type)
		}
		if spec.Task != "" {
			args = append(args, "--task", spec.Task)
		}
		if err := exec.Command(exe, args...).Run(); err != nil {
			return launchedMsg{err: err}
		}
		return launchedMsg{}
	}
}

// --- shell.Surface ----------------------------------------------------------

// ID implements shell.Surface.
func (s *Surface) ID() string { return "agents" }

// Title implements shell.Surface.
func (s *Surface) Title() string { return "Agents" }

// Rev implements shell.Surface.
func (s *Surface) Rev() string {
	r := strconv.Itoa(s.rev)
	if s.conv != nil {
		r += "/" + strconv.Itoa(s.conv.Rev())
	}
	return r
}

// InputFocused implements shell.Surface: attached, the composer owns the
// keyboard.
func (s *Surface) InputFocused() bool { return s.client != nil }

// Init implements shell.Surface.
func (s *Surface) Init() tea.Cmd {
	return tea.Batch(s.loadSessionsCmd(), s.subscribeCmd())
}

// Pop implements shell.Surface: esc-at-list has nothing to pop; detach is
// deliberate (ctrl+d), never a stray esc.
func (s *Surface) Pop() bool { return false }

// Update implements shell.Surface.
func (s *Surface) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case sessionsMsg:
		if msg.err != nil {
			// Keep the previous list; empty-workspace errors are normal.
			return nil
		}
		s.rev++
		s.sessions = msg.entries
		if s.sel >= len(s.sessions) {
			s.sel = max(0, len(s.sessions)-1)
		}
		var cmd tea.Cmd
		if s.launching {
			for _, e := range msg.entries {
				if !s.known[e.SessionID] && e.Socket != "" {
					s.launching = false
					s.status = "attaching " + e.SessionID
					cmd = attachCmd(e.Socket, e.SessionID)
					break
				}
			}
		}
		if !s.launching {
			for _, e := range msg.entries {
				s.known[e.SessionID] = true
			}
		}
		return cmd

	case subscribedMsg:
		// The delta pump: wait → refresh → wait again. No ticker exists;
		// the chain re-arms itself for as long as the channel lives.
		s.deltas = msg.ch
		return waitDelta(msg.ch)

	case deltaMsg:
		if !msg.ok || s.deltas == nil {
			return nil // subscription ended (shutdown)
		}
		return tea.Batch(s.loadSessionsCmd(), waitDelta(s.deltas))

	case attachResultMsg:
		s.rev++
		if msg.err != nil {
			s.status = "attach failed: " + msg.err.Error()
			return nil
		}
		s.client = msg.h
		s.attachedID = msg.id
		s.conv = agentfold.New()
		s.composer.Focus()
		s.scroll = 0
		s.status = ""
		return waitFrame(msg.h.Frames())

	case frameMsg:
		if s.client == nil {
			return nil
		}
		if msg.closed {
			s.rev++
			s.status = "session ended"
			s.detach()
			return nil
		}
		s.conv.Fold(msg.f)
		return waitFrame(s.client.Frames())

	case launchedMsg:
		s.rev++
		if msg.err != nil {
			s.launching = false
			s.status = "launch failed: " + msg.err.Error()
		}
		return nil
	}
	return nil
}

// detach releases the head; the session keeps running (heads are
// ephemeral, design §13.7).
func (s *Surface) detach() {
	if s.client != nil {
		s.client.Detach()
	}
	s.client = nil
	s.conv = nil
	s.attachedID = ""
	s.composer.Blur()
	s.composer.Reset()
	s.rev++
}

// HandleKey implements shell.Surface.
func (s *Surface) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if s.client != nil {
		return s.handleAttachedKey(msg)
	}
	switch msg.String() {
	case "up", "k":
		if s.sel > 0 {
			s.sel--
			s.rev++
		}
		return nil, true
	case "down", "j":
		if s.sel < len(s.sessions)-1 {
			s.sel++
			s.rev++
		}
		return nil, true
	case "enter":
		if s.sel < len(s.sessions) {
			e := s.sessions[s.sel]
			if e.Socket == "" {
				s.status = "session has no socket"
				s.rev++
				return nil, true
			}
			s.status = "attaching " + e.SessionID
			s.rev++
			return attachCmd(e.Socket, e.SessionID), true
		}
		return nil, true
	case "n":
		ov := NewLaunchOverlay(func(spec LaunchSpec) tea.Cmd {
			s.launching = true
			s.status = "launching…"
			s.rev++
			return launchCmd(spec)
		})
		return func() tea.Msg { return shell.OpenOverlayMsg{Overlay: ov} }, true
	}
	return nil, false
}

// handleAttachedKey mirrors the v1 head bindings exactly: enter steers,
// esc interrupts, ctrl+d detaches, ctrl+y / ctrl+n answer the oldest
// approval, ctrl+o toggles tool cards, pgup/pgdn scroll. Everything else
// is composer text.
func (s *Surface) handleAttachedKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "enter":
		text := s.composer.Value()
		if text == "" {
			return nil, true
		}
		s.composer.Reset()
		s.rev++
		if err := s.client.Steer(text); err != nil {
			s.status = err.Error()
		} else {
			s.status = "queued"
		}
		return nil, true
	case "esc":
		_ = s.client.Interrupt()
		s.status = "interrupted"
		s.rev++
		return nil, true
	case "ctrl+d":
		s.detach()
		return nil, true
	case "ctrl+y":
		s.answerTopApproval(true)
		return nil, true
	case "ctrl+n":
		s.answerTopApproval(false)
		return nil, true
	case "ctrl+o":
		s.expandTools = !s.expandTools
		s.rev++
		return nil, true
	case "pgup":
		s.scroll += 10
		s.rev++
		return nil, true
	case "pgdown":
		s.scroll = max(0, s.scroll-10)
		s.rev++
		return nil, true
	}
	var cmd tea.Cmd
	s.composer, cmd = s.composer.Update(msg)
	s.rev++
	return cmd, true
}

func (s *Surface) answerTopApproval(approved bool) {
	if s.conv == nil || len(s.conv.Pending) == 0 {
		return
	}
	top := s.conv.Pending[0]
	s.rev++
	if err := s.client.Verdict(top.RequestID, approved, ""); err != nil {
		s.status = err.Error()
		return
	}
	if approved {
		s.status = "approved " + top.Tool
	} else {
		s.status = "denied " + top.Tool
	}
}

// Close releases the subscription context (used by tests).
func (s *Surface) Close() {
	s.detach()
	s.cancel()
}
