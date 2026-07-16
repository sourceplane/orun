// Package events is the cockpit v2 Events surface (specs/orun-tui-v2 §5,
// TR7). The local lane folds a lifecycle feed from what the workspace
// records — run and session activity — newest first (spec Q5: the local
// lane is thin by nature; the org-wide event bus is the cloud lane and
// merges into the same feed at TR8).
package events

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/agent/live"
	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
	"github.com/sourceplane/orun/internal/tui2/data"
	"github.com/sourceplane/orun/internal/tui2/design"
	"github.com/sourceplane/orun/internal/tui2/frame"
)

var facets = []string{"all", "runs", "sessions"}

// event is one feed row.
type event struct {
	at     time.Time
	kind   string // "run" | "session"
	label  string
	detail string
	status string
}

// Surface implements shell.Surface.
type Surface struct {
	src    data.Source
	ctx    context.Context
	cancel context.CancelFunc

	runs     viewmodel.RunListView
	sessions []live.Entry
	facet    int
	scroll   int

	deltas <-chan data.Delta
	rev    int
}

// New builds the surface over src.
func New(src data.Source) *Surface {
	ctx, cancel := context.WithCancel(context.Background())
	return &Surface{src: src, ctx: ctx, cancel: cancel}
}

type (
	snapshotMsg struct {
		runs     viewmodel.RunListView
		sessions []live.Entry
	}
	subscribedMsg struct{ ch <-chan data.Delta }
	deltaMsg      struct{ ok bool }
)

func (s *Surface) loadCmd() tea.Cmd {
	src, ctx := s.src, s.ctx
	return func() tea.Msg {
		var m snapshotMsg
		m.runs, _ = src.Runs(ctx)
		m.sessions, _ = src.Sessions(ctx)
		return m
	}
}

func (s *Surface) subscribeCmd() tea.Cmd {
	src, ctx := s.src, s.ctx
	return func() tea.Msg {
		ch, err := src.Subscribe(ctx, data.TopicRuns, data.TopicSessions)
		if err != nil {
			return snapshotMsg{}
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

// ID implements shell.Surface.
func (s *Surface) ID() string { return "events" }

// Title implements shell.Surface.
func (s *Surface) Title() string { return "Events" }

// InputFocused implements shell.Surface.
func (s *Surface) InputFocused() bool { return false }

// Rev implements shell.Surface.
func (s *Surface) Rev() string { return strconv.Itoa(s.rev) }

// Pop implements shell.Surface.
func (s *Surface) Pop() bool {
	if s.scroll > 0 {
		s.scroll = 0
		s.rev++
		return true
	}
	return false
}

// Init implements shell.Surface.
func (s *Surface) Init() tea.Cmd { return tea.Batch(s.loadCmd(), s.subscribeCmd()) }

// Update implements shell.Surface.
func (s *Surface) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case snapshotMsg:
		s.runs = msg.runs
		s.sessions = msg.sessions
		s.rev++
		return nil
	case subscribedMsg:
		s.deltas = msg.ch
		return waitDelta(msg.ch)
	case deltaMsg:
		if !msg.ok || s.deltas == nil {
			return nil
		}
		return tea.Batch(s.loadCmd(), waitDelta(s.deltas))
	}
	return nil
}

// HandleKey implements shell.Surface.
func (s *Surface) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "f":
		s.facet = (s.facet + 1) % len(facets)
		s.scroll = 0
		s.rev++
		return nil, true
	case "up", "k":
		s.scroll++
		s.rev++
		return nil, true
	case "down", "j":
		if s.scroll > 0 {
			s.scroll--
			s.rev++
		}
		return nil, true
	}
	return nil, false
}

// feed folds the local lifecycle events, newest first.
func (s *Surface) feed() []event {
	var out []event
	want := facets[s.facet]
	if want != "sessions" {
		for _, r := range s.runs.Runs {
			label := r.PlanName
			if label == "" {
				label = r.ExecID
			}
			if !r.FinishedAt.IsZero() {
				out = append(out, event{at: r.FinishedAt, kind: "run", label: label, detail: r.ExecID, status: r.Status})
			}
			if !r.StartedAt.IsZero() {
				out = append(out, event{at: r.StartedAt, kind: "run", label: label, detail: r.ExecID, status: "running"})
			}
		}
	}
	if want != "runs" {
		for _, e := range s.sessions {
			label := e.AgentType
			if label == "" {
				label = e.SessionID
			}
			out = append(out, event{at: e.StartedAt, kind: "session", label: label, detail: orDash(e.Task), status: e.State})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].at.After(out[j].at) })
	return out
}

// View implements shell.Surface.
func (s *Surface) View(size frame.Size) string {
	var b strings.Builder
	b.WriteString("\n " + design.Title.Render("events") + "   " + design.Chips(s.facet, facets...) + "\n\n")
	rows := s.feed()
	if len(rows) == 0 {
		b.WriteString("  " + design.Dim.Render("no local events yet — the org event bus arrives when signed in") + "\n")
	}
	maxRows := size.Height - 7
	if s.scroll > len(rows)-1 {
		s.scroll = max(0, len(rows)-1)
	}
	for i := s.scroll; i < len(rows) && i-s.scroll < maxRows; i++ {
		ev := rows[i]
		primary := ev.kind + " · " + ev.label
		secondary := ev.detail
		if !ev.at.IsZero() {
			secondary += " · " + ago(ev.at)
		}
		b.WriteString(design.DataRow(size.Width, false, design.Sanitize(primary), design.Sanitize(secondary), design.StatusText(ev.status)) + "\n")
	}
	b.WriteString("\n  " + design.KeyHint("f", "filter") + "\n")
	return frame.Fit(b.String(), size)
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func orDash(v string) string {
	if v == "" {
		return "—"
	}
	return v
}

// Close releases the subscription context.
func (s *Surface) Close() { s.cancel() }
