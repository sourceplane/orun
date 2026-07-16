// Package home is the cockpit v2 Home surface (specs/orun-tui-v2 §5,
// TR7): the workspace landing — stat tiles, the needs-attention fold, and
// latest activity. Everything here is a fold over the other surfaces'
// slices; Home owns no data of its own and every row deep-links to the
// surface that does.
package home

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/agent/live"
	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
	"github.com/sourceplane/orun/internal/tui2/data"
	"github.com/sourceplane/orun/internal/tui2/shell"
)

// attention is one needs-you item.
type attention struct {
	label  string
	detail string
	// surface to jump to on enter.
	surface string
}

// Surface implements shell.Surface.
type Surface struct {
	src    data.Source
	ctx    context.Context
	cancel context.CancelFunc

	catalog  viewmodel.CatalogView
	runs     viewmodel.RunListView
	sessions []live.Entry

	sel    int // selected attention row
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
		catalog  viewmodel.CatalogView
		runs     viewmodel.RunListView
		sessions []live.Entry
	}
	subscribedMsg struct{ ch <-chan data.Delta }
	deltaMsg      struct{ ok bool }
)

func (s *Surface) loadCmd() tea.Cmd {
	src, ctx := s.src, s.ctx
	return func() tea.Msg {
		var msg snapshotMsg
		msg.catalog, _ = src.Catalog(ctx)
		msg.runs, _ = src.Runs(ctx)
		msg.sessions, _ = src.Sessions(ctx)
		return msg
	}
}

func (s *Surface) subscribeCmd() tea.Cmd {
	src, ctx := s.src, s.ctx
	return func() tea.Msg {
		ch, err := src.Subscribe(ctx)
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
func (s *Surface) ID() string { return "home" }

// Title implements shell.Surface.
func (s *Surface) Title() string { return "Home" }

// InputFocused implements shell.Surface.
func (s *Surface) InputFocused() bool { return false }

// Rev implements shell.Surface.
func (s *Surface) Rev() string { return strconv.Itoa(s.rev) }

// Pop implements shell.Surface.
func (s *Surface) Pop() bool { return false }

// Init implements shell.Surface.
func (s *Surface) Init() tea.Cmd { return tea.Batch(s.loadCmd(), s.subscribeCmd()) }

// Update implements shell.Surface.
func (s *Surface) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case snapshotMsg:
		s.catalog = msg.catalog
		s.runs = msg.runs
		s.sessions = msg.sessions
		if n := len(s.attentionItems()); s.sel >= n {
			s.sel = max(0, n-1)
		}
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
	items := s.attentionItems()
	switch msg.String() {
	case "up", "k":
		if s.sel > 0 {
			s.sel--
			s.rev++
		}
		return nil, true
	case "down", "j":
		if s.sel < len(items)-1 {
			s.sel++
			s.rev++
		}
		return nil, true
	case "enter":
		if s.sel < len(items) {
			target := items[s.sel].surface
			return func() tea.Msg { return shell.GotoMsg{ID: target} }, true
		}
		return nil, true
	}
	return nil, false
}

// attentionItems folds the needs-you queue from local state: sessions in
// non-running states and the most recent failed run. The cloud lane (TR8)
// merges the org-wide queue into the same fold.
func (s *Surface) attentionItems() []attention {
	var out []attention
	for _, e := range s.sessions {
		st := strings.ToLower(e.State)
		if st == "" || st == "running" {
			continue
		}
		label := e.AgentType
		if label == "" {
			label = e.SessionID
		}
		out = append(out, attention{
			label:   label + " · " + st,
			detail:  orDash(e.Task),
			surface: "agents",
		})
	}
	for _, r := range s.runs.Runs {
		if strings.EqualFold(r.Status, "failed") || strings.EqualFold(r.Status, "error") {
			out = append(out, attention{
				label:   orDash(r.PlanName) + " failed",
				detail:  r.ExecID,
				surface: "activity",
			})
			break // the newest failed run only; the rest live on Activity
		}
	}
	return out
}

// Stats the tiles fold.
func (s *Surface) stats() (components string, compHint string, sessions string, sessHint string, lastRun string, lastHint string) {
	changed := 0
	for _, c := range s.catalog.Components {
		if c.Changed() {
			changed++
		}
	}
	components = fmt.Sprintf("%d", len(s.catalog.Components))
	compHint = fmt.Sprintf("%d changed", changed)

	sessions = fmt.Sprintf("%d", len(s.sessions))
	if n := len(s.attentionItems()); n > 0 {
		sessHint = fmt.Sprintf("%d need you", n)
	} else {
		sessHint = "all quiet"
	}

	lastRun, lastHint = "—", "no runs yet"
	if len(s.runs.Runs) > 0 {
		r := s.runs.Runs[0]
		lastRun = strings.ToLower(r.Status)
		lastHint = orDash(r.PlanName)
	}
	return
}

func orDash(v string) string {
	if v == "" {
		return "—"
	}
	return v
}

// Close releases the subscription context.
func (s *Surface) Close() { s.cancel() }
