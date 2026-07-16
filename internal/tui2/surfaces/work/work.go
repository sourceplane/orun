// Package work is the cockpit v2 Work surface (specs/orun-tui-v2 §5, TR6).
//
// Offline it renders the local lane honestly: approval-sealed epic
// snapshots pulled via `orun epic pull`, each with a "sealed at" banner
// (risks-and-open-questions Q2 — the banner IS the honest state). The
// live work plane (rungs, triage, verdicts) is the cloud lane and lights
// up when signed in (TR8); this surface's structure — list → epic detail
// with milestone ladder, tasks, brief, and session links — is the same in
// both lanes.
package work

import (
	"context"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/agent/live"
	"github.com/sourceplane/orun/internal/tui2/data"
)

type level int

const (
	levelList level = iota
	levelEpic
	levelBrief
)

// Surface implements shell.Surface.
type Surface struct {
	src    data.Source
	ctx    context.Context
	cancel context.CancelFunc

	epics    []data.EpicView
	sessions []live.Entry
	level    level
	sel      int
	selTask  int
	scroll   int

	deltas <-chan data.Delta
	rev    int
}

// New builds the surface over src.
func New(src data.Source) *Surface {
	ctx, cancel := context.WithCancel(context.Background())
	return &Surface{src: src, ctx: ctx, cancel: cancel}
}

// --- messages / commands ------------------------------------------------

type (
	workMsg struct {
		epics []data.EpicView
		err   error
	}
	sessionsMsg struct {
		entries []live.Entry
		err     error
	}
	subscribedMsg struct{ ch <-chan data.Delta }
	deltaMsg      struct{ ok bool }
)

func (s *Surface) loadCmd() tea.Cmd {
	src, ctx := s.src, s.ctx
	return func() tea.Msg {
		epics, err := src.Work(ctx)
		return workMsg{epics: epics, err: err}
	}
}

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
		ch, err := src.Subscribe(ctx, data.TopicWork, data.TopicSessions)
		if err != nil {
			return workMsg{err: err}
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

// --- shell.Surface --------------------------------------------------------

// ID implements shell.Surface.
func (s *Surface) ID() string { return "work" }

// Title implements shell.Surface.
func (s *Surface) Title() string { return "Work" }

// InputFocused implements shell.Surface.
func (s *Surface) InputFocused() bool { return false }

// Rev implements shell.Surface.
func (s *Surface) Rev() string { return strconv.Itoa(s.rev) }

// Init implements shell.Surface.
func (s *Surface) Init() tea.Cmd {
	return tea.Batch(s.loadCmd(), s.loadSessionsCmd(), s.subscribeCmd())
}

// Pop implements shell.Surface.
func (s *Surface) Pop() bool {
	if s.level == levelList {
		return false
	}
	s.level--
	s.scroll = 0
	s.rev++
	return true
}

// Update implements shell.Surface.
func (s *Surface) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case workMsg:
		if msg.err != nil {
			return nil
		}
		s.epics = msg.epics
		if s.sel >= len(s.epics) {
			s.sel = max(0, len(s.epics)-1)
		}
		s.rev++
		return nil
	case sessionsMsg:
		if msg.err != nil {
			return nil
		}
		s.sessions = msg.entries
		s.rev++
		return nil
	case subscribedMsg:
		s.deltas = msg.ch
		return waitDelta(msg.ch)
	case deltaMsg:
		if !msg.ok || s.deltas == nil {
			return nil
		}
		return tea.Batch(s.loadCmd(), s.loadSessionsCmd(), waitDelta(s.deltas))
	}
	return nil
}

// HandleKey implements shell.Surface.
func (s *Surface) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch s.level {
	case levelList:
		switch msg.String() {
		case "up", "k":
			if s.sel > 0 {
				s.sel--
				s.rev++
			}
			return nil, true
		case "down", "j":
			if s.sel < len(s.epics)-1 {
				s.sel++
				s.rev++
			}
			return nil, true
		case "enter":
			if s.sel < len(s.epics) {
				s.level = levelEpic
				s.selTask = 0
				s.rev++
			}
			return nil, true
		}
	case levelEpic:
		switch msg.String() {
		case "up", "k":
			if s.selTask > 0 {
				s.selTask--
				s.rev++
			}
			return nil, true
		case "down", "j":
			if e := s.current(); e != nil && s.selTask < len(e.Snapshot.Tasks)-1 {
				s.selTask++
				s.rev++
			}
			return nil, true
		case "b":
			if e := s.current(); e != nil && e.Brief != "" {
				s.level = levelBrief
				s.scroll = 0
				s.rev++
			}
			return nil, true
		}
	case levelBrief:
		switch msg.String() {
		case "up", "k":
			if s.scroll > 0 {
				s.scroll--
				s.rev++
			}
			return nil, true
		case "down", "j":
			s.scroll++
			s.rev++
			return nil, true
		}
	}
	return nil, false
}

func (s *Surface) current() *data.EpicView {
	if s.sel < len(s.epics) {
		return &s.epics[s.sel]
	}
	return nil
}

// sessionFor joins a task key to a live session working it — the
// session↔work link both product surfaces render (a session is
// infrastructure; what it achieves lives on Work).
func (s *Surface) sessionFor(taskKey string) *live.Entry {
	for i := range s.sessions {
		if strings.Contains(s.sessions[i].Task, taskKey) {
			return &s.sessions[i]
		}
	}
	return nil
}

// Close releases the subscription context.
func (s *Surface) Close() { s.cancel() }
