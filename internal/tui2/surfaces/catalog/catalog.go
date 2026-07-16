// Package catalog is the cockpit v2 Catalog surface (specs/orun-tui-v2 §5,
// TR5): the component explorer with the change overlay, component detail,
// and the Compose flow — plan preview → dry-run → run — which absorbs the
// v1 Plan Studio as a drill under a component instead of a place of its
// own.
package catalog

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
	"github.com/sourceplane/orun/internal/tui2/data"
	"github.com/sourceplane/orun/internal/tui2/frame"
	"github.com/sourceplane/orun/internal/tui2/shell"
)

type level int

const (
	levelList level = iota
	levelDetail
	levelCompose
)

// Surface implements shell.Surface.
type Surface struct {
	src  data.Source
	comp data.Composer

	ctx    context.Context
	cancel context.CancelFunc

	view        viewmodel.CatalogView
	detail      viewmodel.ComponentView
	level       level
	sel         int
	changedOnly bool

	// Compose state.
	composeKey  string
	envIdx      int
	envs        []string
	preview     data.PlanPreview
	planErr     string
	generating  bool
	dispatching bool
	dryRun      bool
	progress    []string // rolling event lines from the dispatch
	execID      string

	sched  *frame.Scheduler
	deltas <-chan data.Delta
	status string
	rev    int
}

// New builds the surface. comp may be nil (read-only catalog; compose
// affordances hide — Caps discipline for sources that cannot execute).
func New(src data.Source, comp data.Composer) *Surface {
	ctx, cancel := context.WithCancel(context.Background())
	return &Surface{src: src, comp: comp, ctx: ctx, cancel: cancel}
}

// SetScheduler implements shell.ScheduledSurface: dispatch progress is the
// one motion this surface owns.
func (s *Surface) SetScheduler(sc *frame.Scheduler) { s.sched = sc }

const dispatchAnimID = "catalog.dispatch"

// --- messages / commands ------------------------------------------------

type (
	catalogMsg struct {
		v   viewmodel.CatalogView
		err error
	}
	componentMsg struct {
		key string
		v   viewmodel.ComponentView
		ok  bool
		err error
	}
	planMsg struct {
		key string
		p   data.PlanPreview
		err error
	}
	runEventMsg struct {
		ev data.RunEvent
		ok bool
	}
	subscribedMsg struct{ ch <-chan data.Delta }
	deltaMsg      struct{ ok bool }
)

func (s *Surface) loadCatalogCmd() tea.Cmd {
	src, ctx := s.src, s.ctx
	return func() tea.Msg {
		v, err := src.Catalog(ctx)
		return catalogMsg{v: v, err: err}
	}
}

func (s *Surface) loadComponentCmd(key string) tea.Cmd {
	src, ctx := s.src, s.ctx
	return func() tea.Msg {
		v, ok, err := src.Component(ctx, key)
		return componentMsg{key: key, v: v, ok: ok, err: err}
	}
}

func (s *Surface) generateCmd() tea.Cmd {
	comp, ctx := s.comp, s.ctx
	spec := data.PlanSpec{Components: []string{s.composeKey}, ChangedOnly: s.changedOnly}
	if s.envIdx < len(s.envs) {
		spec.Environment = s.envs[s.envIdx]
	}
	key := s.composeKey
	return func() tea.Msg {
		p, err := comp.GeneratePlan(ctx, spec)
		return planMsg{key: key, p: p, err: err}
	}
}

func (s *Surface) dispatchCmd(dry bool) tea.Cmd {
	comp, ctx, plan := s.comp, s.ctx, s.preview.Plan
	return func() tea.Msg {
		ch, err := comp.RunPlan(ctx, plan, dry)
		if err != nil {
			return runEventMsg{ev: data.RunEvent{Kind: "run_done", Status: "failed", Error: err.Error()}, ok: true}
		}
		ev, ok := <-ch
		if !ok {
			return runEventMsg{ok: false}
		}
		return runEventPump{ch: ch, first: ev}
	}
}

// runEventPump carries the live channel through the first event so Update
// can keep pulling without re-dispatching.
type runEventPump struct {
	ch    <-chan data.RunEvent
	first data.RunEvent
}

func waitRunEvent(ch <-chan data.RunEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return runEventMsg{ok: false}
		}
		return runEventPump{ch: ch, first: ev}
	}
}

func (s *Surface) subscribeCmd() tea.Cmd {
	src, ctx := s.src, s.ctx
	return func() tea.Msg {
		ch, err := src.Subscribe(ctx, data.TopicCatalog)
		if err != nil {
			return catalogMsg{err: err}
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
func (s *Surface) ID() string { return "catalog" }

// Title implements shell.Surface.
func (s *Surface) Title() string { return "Catalog" }

// InputFocused implements shell.Surface.
func (s *Surface) InputFocused() bool { return false }

// Rev implements shell.Surface.
func (s *Surface) Rev() string { return strconv.Itoa(s.rev) }

// Init implements shell.Surface.
func (s *Surface) Init() tea.Cmd {
	return tea.Batch(s.loadCatalogCmd(), s.subscribeCmd())
}

// Pop implements shell.Surface.
func (s *Surface) Pop() bool {
	if s.level == levelList {
		return false
	}
	s.level--
	s.rev++
	return true
}

// Update implements shell.Surface.
func (s *Surface) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case catalogMsg:
		if msg.err != nil {
			return nil
		}
		s.view = msg.v
		if n := len(s.rows()); s.sel >= n {
			s.sel = max(0, n-1)
		}
		s.rev++
		return nil

	case componentMsg:
		if msg.err != nil || !msg.ok {
			return nil
		}
		if s.level == levelDetail || s.level == levelCompose {
			s.detail = msg.v
			s.rev++
		}
		return nil

	case planMsg:
		if msg.key != s.composeKey {
			return nil // stale generation for a different component
		}
		s.generating = false
		if msg.err != nil {
			s.planErr = msg.err.Error()
			s.preview = data.PlanPreview{}
		} else {
			s.planErr = ""
			s.preview = msg.p
		}
		s.rev++
		return nil

	case runEventPump:
		s.foldRunEvent(msg.first)
		return waitRunEvent(msg.ch)

	case runEventMsg:
		if !msg.ok {
			s.endDispatch()
			return nil
		}
		s.foldRunEvent(msg.ev)
		return nil

	case subscribedMsg:
		s.deltas = msg.ch
		return waitDelta(msg.ch)

	case deltaMsg:
		if !msg.ok || s.deltas == nil {
			return nil
		}
		cmds := []tea.Cmd{s.loadCatalogCmd(), waitDelta(s.deltas)}
		if s.level != levelList && s.detail.Key != "" {
			cmds = append(cmds, s.loadComponentCmd(s.detail.Key))
		}
		return tea.Batch(cmds...)
	}
	return nil
}

func (s *Surface) foldRunEvent(ev data.RunEvent) {
	s.rev++
	if ev.ExecID != "" {
		s.execID = ev.ExecID
	}
	line := ev.Kind
	switch ev.Kind {
	case "job_started":
		line = ev.JobID + " started"
	case "job_completed":
		line = ev.JobID + " completed"
	case "job_failed":
		line = ev.JobID + " failed: " + ev.Error
	case "step_started":
		line = ev.JobID + " · " + ev.StepID + " running"
	case "step_completed":
		line = ev.JobID + " · " + ev.StepID + " " + ev.Status
	case "run_done":
		s.endDispatch()
		if ev.Error != "" {
			line = "run finished: " + ev.Status + " — " + ev.Error
		} else {
			line = "run finished: " + ev.Status
		}
	}
	s.progress = append(s.progress, line)
	if len(s.progress) > 12 {
		s.progress = s.progress[len(s.progress)-12:]
	}
}

func (s *Surface) endDispatch() {
	if s.dispatching {
		s.dispatching = false
		if s.sched != nil {
			s.sched.Remove(dispatchAnimID)
		}
	}
}

// HandleKey implements shell.Surface.
func (s *Surface) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch s.level {
	case levelList:
		return s.keyList(msg)
	case levelDetail:
		return s.keyDetail(msg)
	default:
		return s.keyCompose(msg)
	}
}

func (s *Surface) keyList(msg tea.KeyMsg) (tea.Cmd, bool) {
	rows := s.rows()
	switch msg.String() {
	case "up", "k":
		if s.sel > 0 {
			s.sel--
			s.rev++
		}
		return nil, true
	case "down", "j":
		if s.sel < len(rows)-1 {
			s.sel++
			s.rev++
		}
		return nil, true
	case "c":
		s.changedOnly = !s.changedOnly
		s.sel = 0
		s.rev++
		return nil, true
	case "enter":
		if s.sel < len(rows) {
			s.level = levelDetail
			s.detail = viewmodel.ComponentView{Key: rows[s.sel].Key, Name: rows[s.sel].Name}
			s.rev++
			return s.loadComponentCmd(rows[s.sel].Key), true
		}
		return nil, true
	case "g":
		if s.sel < len(rows) && s.comp != nil {
			return s.enterCompose(rows[s.sel].Key, rows[s.sel].Envs), true
		}
		return nil, true
	}
	return nil, false
}

func (s *Surface) keyDetail(msg tea.KeyMsg) (tea.Cmd, bool) {
	if msg.String() == "g" && s.comp != nil {
		var envs []string
		for _, e := range s.detail.Envs {
			if e.Active {
				envs = append(envs, e.Name)
			}
		}
		return s.enterCompose(s.detail.Key, envs), true
	}
	return nil, false
}

func (s *Surface) enterCompose(key string, envs []string) tea.Cmd {
	s.level = levelCompose
	s.composeKey = key
	s.envs = append([]string{""}, envs...) // "" = all active envs
	s.envIdx = 0
	s.preview = data.PlanPreview{}
	s.planErr = ""
	s.generating = true
	s.progress = nil
	s.execID = ""
	s.rev++
	return s.generateCmd()
}

func (s *Surface) keyCompose(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "e":
		if len(s.envs) > 1 && !s.dispatching {
			s.envIdx = (s.envIdx + 1) % len(s.envs)
			s.generating = true
			s.rev++
			return s.generateCmd(), true
		}
		return nil, true
	case "c":
		if !s.dispatching {
			s.changedOnly = !s.changedOnly
			s.generating = true
			s.rev++
			return s.generateCmd(), true
		}
		return nil, true
	case "d", "R":
		if s.preview.Plan == nil || s.dispatching || s.generating {
			return nil, true
		}
		dry := msg.String() == "d"
		verb := fmt.Sprintf("Run %d jobs", s.preview.JobCount)
		if dry {
			verb = fmt.Sprintf("Dry-run %d jobs", s.preview.JobCount)
		}
		body := []string{
			strings.Join(s.preview.Components, ", "),
			"plan " + s.preview.Checksum,
		}
		ov := shell.NewConfirm("Dispatch plan", body, verb, func() tea.Cmd {
			s.dispatching = true
			s.dryRun = dry
			s.progress = nil
			s.rev++
			var arm tea.Cmd
			if s.sched != nil {
				s.sched.Add(dispatchAnimID)
				arm = s.sched.Arm()
			}
			return tea.Batch(s.dispatchCmd(dry), arm)
		})
		return func() tea.Msg { return shell.OpenOverlayMsg{Overlay: ov} }, true
	}
	return nil, false
}

func (s *Surface) rows() []viewmodel.ComponentRow {
	if !s.changedOnly {
		return s.view.Components
	}
	var out []viewmodel.ComponentRow
	for _, r := range s.view.Components {
		if r.Changed() {
			out = append(out, r)
		}
	}
	return out
}

// Close releases the subscription context.
func (s *Surface) Close() { s.cancel() }
