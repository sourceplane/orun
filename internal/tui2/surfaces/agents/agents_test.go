package agents

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/sourceplane/orun/internal/agent/attach"
	"github.com/sourceplane/orun/internal/agent/live"
	"github.com/sourceplane/orun/internal/tui2/data"
	"github.com/sourceplane/orun/internal/tui2/frame"
)

// fakeHead records inputs and feeds frames.
type fakeHead struct {
	frames    chan attach.Frame
	steers    []string
	verdicts  []string
	interrupt int
	detached  bool
}

func newFakeHead() *fakeHead { return &fakeHead{frames: make(chan attach.Frame, 16)} }

func (f *fakeHead) Frames() <-chan attach.Frame { return f.frames }
func (f *fakeHead) Steer(t string) error        { f.steers = append(f.steers, t); return nil }
func (f *fakeHead) Verdict(id string, ok bool, _ string) error {
	v := id + ":denied"
	if ok {
		v = id + ":approved"
	}
	f.verdicts = append(f.verdicts, v)
	return nil
}
func (f *fakeHead) Interrupt() error { f.interrupt++; return nil }
func (f *fakeHead) Detach()          { f.detached = true }

func runes(s string) tea.KeyMsg        { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func keyType(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func testSurface() (*Surface, *data.MockSource) {
	m := data.SampleMock()
	s := New(m)
	return s, m
}

func view(t *testing.T, s *Surface, w, h int) string {
	t.Helper()
	out := s.View(frame.Size{Width: w, Height: h})
	if err := frame.Check(out, frame.Size{Width: w, Height: h}); err != nil {
		t.Fatalf("frame invariant: %v", err)
	}
	return ansi.Strip(out)
}

func attachFake(t *testing.T, s *Surface) *fakeHead {
	t.Helper()
	h := newFakeHead()
	cmd := s.Update(attachResultMsg{h: h, id: "as_test"})
	if cmd == nil {
		t.Fatal("attach must arm the frame pump")
	}
	if !s.InputFocused() {
		t.Fatal("attached surface must own the keyboard")
	}
	return h
}

// feedFrame delivers one frame through the pump path exactly as the tea
// loop would: send, run the pending wait, fold the message.
func feedFrame(s *Surface, h *fakeHead, f attach.Frame) {
	h.frames <- f
	msg := waitFrame(h.Frames())()
	s.Update(msg)
}

func TestListRendersSessions(t *testing.T) {
	s, m := testSurface()
	entries, _ := m.Sessions(nil)
	s.Update(sessionsMsg{entries: entries})
	out := view(t, s, 100, 30)
	if !strings.Contains(out, "implementer") || !strings.Contains(out, "fix flaky catalog test") {
		t.Fatalf("list missing sessions:\n%s", out)
	}
}

func TestDeltaPumpRefreshes(t *testing.T) {
	s, m := testSurface()
	// Subscribe as Init would.
	sub := s.subscribeCmd()().(subscribedMsg)
	cmd := s.Update(sub)
	if cmd == nil {
		t.Fatal("subscribe must arm the delta wait")
	}
	// A workspace change lands…
	m.Seed(func(ms *data.MockSource) {
		ms.LiveSessions = []live.Entry{{SessionID: "as_new", PID: 1, State: "running", AgentType: "fresh", StartedAt: time.Now()}}
	}, data.TopicSessions)
	// …the wait fires with a delta…
	dm, ok := cmd().(deltaMsg)
	if !ok || !dm.ok {
		t.Fatalf("wait must yield a live delta, got %#v", dm)
	}
	next := s.Update(dm)
	if next == nil {
		t.Fatal("delta must trigger refresh + re-arm")
	}
	entries, _ := m.Sessions(nil)
	s.Update(sessionsMsg{entries: entries})
	if !strings.Contains(view(t, s, 100, 30), "fresh") {
		t.Fatal("refresh did not land")
	}
}

func TestComposerSteerAndInterrupt(t *testing.T) {
	s, _ := testSurface()
	h := attachFake(t, s)

	for _, r := range "run the tests" {
		s.HandleKey(runes(string(r)))
	}
	s.HandleKey(keyType(tea.KeyEnter))
	if len(h.steers) != 1 || h.steers[0] != "run the tests" {
		t.Fatalf("steers = %v", h.steers)
	}
	s.HandleKey(keyType(tea.KeyEsc))
	if h.interrupt != 1 {
		t.Fatal("esc must interrupt")
	}
}

func TestApprovalCardsAndVerdicts(t *testing.T) {
	s, _ := testSurface()
	h := attachFake(t, s)

	feedFrame(s, h, attach.EventFrame(1, "approval_requested", "", map[string]any{"requestId": "req-9", "tool": "bash"}, ""))
	out := view(t, s, 100, 30)
	if !strings.Contains(out, "approval needed") || !strings.Contains(out, "bash") {
		t.Fatalf("approval card missing:\n%s", out)
	}
	s.HandleKey(keyType(tea.KeyCtrlY))
	if len(h.verdicts) != 1 || h.verdicts[0] != "req-9:approved" {
		t.Fatalf("verdicts = %v", h.verdicts)
	}
}

func TestStreamingRendersAndSeals(t *testing.T) {
	s, _ := testSurface()
	h := attachFake(t, s)

	feedFrame(s, h, attach.DeltaFrame(1, "thinking about "))
	feedFrame(s, h, attach.DeltaFrame(1, "the fix"))
	out := view(t, s, 100, 30)
	if !strings.Contains(out, "thinking about the fix") {
		t.Fatalf("streaming text missing:\n%s", out)
	}
	feedFrame(s, h, attach.EventFrame(2, "message_agent", "", map[string]any{"text": "here is the fix"}, ""))
	out = view(t, s, 100, 30)
	if !strings.Contains(out, "here is the fix") {
		t.Fatalf("sealed turn missing:\n%s", out)
	}
}

func TestDetachLeavesSessionRunning(t *testing.T) {
	s, _ := testSurface()
	h := attachFake(t, s)
	s.HandleKey(keyType(tea.KeyCtrlD))
	if !h.detached {
		t.Fatal("ctrl+d must send detach")
	}
	if s.InputFocused() {
		t.Fatal("detached surface must release the keyboard")
	}
	view(t, s, 100, 30) // back to the list, frame stable
}

func TestClosedFeedDetaches(t *testing.T) {
	s, _ := testSurface()
	attachFake(t, s)
	s.Update(frameMsg{closed: true})
	if s.InputFocused() {
		t.Fatal("closed feed must detach")
	}
	if !strings.Contains(view(t, s, 100, 30), "session ended") {
		t.Fatal("status must say the session ended")
	}
}

func TestAutoAttachAfterLaunch(t *testing.T) {
	s, m := testSurface()
	entries, _ := m.Sessions(nil)
	s.Update(sessionsMsg{entries: entries}) // seeds known ids

	s.launching = true
	withNew := append(entries, live.Entry{SessionID: "as_launched", Socket: "/tmp/x.sock", State: "running"})
	cmd := s.Update(sessionsMsg{entries: withNew})
	if cmd == nil {
		t.Fatal("unfamiliar session with a socket must auto-attach")
	}
	if s.launching {
		t.Fatal("launch completes on first sighting")
	}
}

func TestToolCardsToggle(t *testing.T) {
	s, _ := testSurface()
	h := attachFake(t, s)
	feedFrame(s, h, attach.EventFrame(1, "tool_call", "", map[string]any{"tool": "bash"}, ""))
	feedFrame(s, h, attach.EventFrame(2, "tool_result", "", map[string]any{"output": "SECRET-DETAIL-LINE"}, ""))

	if strings.Contains(view(t, s, 100, 30), "SECRET-DETAIL-LINE") {
		t.Fatal("tool cards must start collapsed")
	}
	s.HandleKey(keyType(tea.KeyCtrlO))
	if !strings.Contains(view(t, s, 100, 30), "SECRET-DETAIL-LINE") {
		t.Fatal("ctrl+o must expand tool cards")
	}
}

// TestHostileFramesRenderInert is the §10 trust boundary end to end: a
// malicious body cannot emit terminal control bytes through the head.
func TestHostileFramesRenderInert(t *testing.T) {
	s, _ := testSurface()
	h := attachFake(t, s)
	feedFrame(s, h, attach.EventFrame(1, "message_agent", "", map[string]any{"text": "evil\x1b]0;owned\x07text\x1b[2J"}, ""))
	raw := s.View(frame.Size{Width: 100, Height: 30})
	if strings.Contains(raw, "\x1b]0;") || strings.Contains(raw, "\x07") || strings.Contains(raw, "[2J") {
		t.Fatalf("control bytes leaked into the frame: %q", raw)
	}
}

func TestViewStableAcrossSizes(t *testing.T) {
	s, m := testSurface()
	entries, _ := m.Sessions(nil)
	s.Update(sessionsMsg{entries: entries})
	for _, sz := range []frame.Size{{Width: 40, Height: 8}, {Width: 80, Height: 24}, {Width: 220, Height: 60}} {
		out := s.View(sz)
		if err := frame.Check(out, sz); err != nil {
			t.Fatalf("size %+v: %v", sz, err)
		}
	}
	h := attachFake(t, s)
	feedFrame(s, h, attach.EventFrame(1, "message_agent", "", map[string]any{"text": strings.Repeat("long turn ", 60)}, ""))
	for _, sz := range []frame.Size{{Width: 40, Height: 8}, {Width: 80, Height: 24}, {Width: 220, Height: 60}} {
		out := s.View(sz)
		if err := frame.Check(out, sz); err != nil {
			t.Fatalf("attached size %+v: %v", sz, err)
		}
	}
}
