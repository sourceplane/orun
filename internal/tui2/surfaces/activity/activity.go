// Package activity is the cockpit v2 Activity surface (specs/orun-tui-v2
// §5, TR4): the runs feed and its drill hierarchy — feed → run → job →
// step log — replacing the v1 Run Dashboard, Log Explorer, and History
// surfaces with one stream-driven place.
//
// Everything renders from Source snapshots; TopicRuns fs-watch deltas
// drive refresh (live runs move their refs on every state projection).
// The run a user is inspecting is pinned by exec id: a background list
// refresh can never clobber the drilldown (the v1 runDetails re-apply
// hack, retired by construction).
package activity

import (
	"context"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
	"github.com/sourceplane/orun/internal/tui2/data"
	"github.com/sourceplane/orun/internal/tui2/shell"
)

type level int

const (
	levelFeed level = iota
	levelRun
	levelJob
	levelLog
)

// facets are the status filter chips, console Activities parity.
var facets = []string{"all", "running", "failed", "completed"}

// Surface implements shell.Surface.
type Surface struct {
	src    data.Source
	ctx    context.Context
	cancel context.CancelFunc

	runs   viewmodel.RunListView
	run    viewmodel.RunView
	pinned string // exec id the drilldown owns

	level   level
	facet   int
	selRun  int
	selJob  int
	selStep int

	logKey    string
	logText   string
	logScroll int
	errsOnly  bool

	deltas <-chan data.Delta
	status string
	rev    int
}

// New builds the surface over src.
func New(src data.Source) *Surface {
	ctx, cancel := context.WithCancel(context.Background())
	return &Surface{src: src, ctx: ctx, cancel: cancel}
}

// --- messages / commands ------------------------------------------------

type (
	runsMsg struct {
		v   viewmodel.RunListView
		err error
	}
	runMsg struct {
		id  string
		v   viewmodel.RunView
		err error
	}
	logMsg struct {
		key  string
		text string
		err  error
	}
	subscribedMsg struct{ ch <-chan data.Delta }
	deltaMsg      struct{ ok bool }
)

func (s *Surface) loadRunsCmd() tea.Cmd {
	src, ctx := s.src, s.ctx
	return func() tea.Msg {
		v, err := src.Runs(ctx)
		return runsMsg{v: v, err: err}
	}
}

func (s *Surface) loadRunCmd(id string) tea.Cmd {
	src, ctx := s.src, s.ctx
	return func() tea.Msg {
		v, err := src.Run(ctx, id)
		return runMsg{id: id, v: v, err: err}
	}
}

func (s *Surface) loadLogCmd(execID, jobID, stepID string) tea.Cmd {
	src, ctx := s.src, s.ctx
	key := execID + "/" + jobID + "/" + stepID
	return func() tea.Msg {
		b, err := src.StepLog(ctx, execID, jobID, stepID)
		return logMsg{key: key, text: string(b), err: err}
	}
}

func (s *Surface) subscribeCmd() tea.Cmd {
	src, ctx := s.src, s.ctx
	return func() tea.Msg {
		ch, err := src.Subscribe(ctx, data.TopicRuns)
		if err != nil {
			return runsMsg{err: err}
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
func (s *Surface) ID() string { return "activity" }

// Title implements shell.Surface.
func (s *Surface) Title() string { return "Activity" }

// InputFocused implements shell.Surface.
func (s *Surface) InputFocused() bool { return false }

// Rev implements shell.Surface.
func (s *Surface) Rev() string { return strconv.Itoa(s.rev) }

// Init implements shell.Surface.
func (s *Surface) Init() tea.Cmd {
	return tea.Batch(s.loadRunsCmd(), s.subscribeCmd())
}

// Pop implements shell.Surface: one drill level per esc.
func (s *Surface) Pop() bool {
	if s.level == levelFeed {
		return false
	}
	s.level--
	if s.level == levelFeed {
		s.pinned = ""
	}
	s.rev++
	return true
}

// Update implements shell.Surface.
func (s *Surface) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case runsMsg:
		if msg.err != nil {
			return nil // keep prior render (design §8: errors keep snapshots)
		}
		s.runs = msg.v
		s.clampSel()
		s.rev++
		return nil

	case runMsg:
		if msg.err != nil {
			return nil
		}
		// Pin discipline: only the drilled run may land in s.run.
		if s.pinned == "" || msg.id != s.pinned {
			return nil
		}
		s.run = msg.v
		s.rev++
		return nil

	case logMsg:
		if msg.err != nil || msg.key != s.logKey {
			return nil
		}
		s.logText = msg.text
		s.rev++
		return nil

	case subscribedMsg:
		s.deltas = msg.ch
		return waitDelta(msg.ch)

	case deltaMsg:
		if !msg.ok || s.deltas == nil {
			return nil
		}
		cmds := []tea.Cmd{s.loadRunsCmd(), waitDelta(s.deltas)}
		if s.pinned != "" {
			cmds = append(cmds, s.loadRunCmd(s.pinned))
		}
		if s.level == levelLog && s.logKey != "" && s.currentStepRunning() {
			parts := strings.SplitN(s.logKey, "/", 3)
			if len(parts) == 3 {
				cmds = append(cmds, s.loadLogCmd(parts[0], parts[1], parts[2]))
			}
		}
		return tea.Batch(cmds...)
	}
	return nil
}

// HandleKey implements shell.Surface.
func (s *Surface) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch s.level {
	case levelFeed:
		return s.keyFeed(msg)
	case levelRun:
		return s.keyRun(msg)
	case levelJob:
		return s.keyJob(msg)
	default:
		return s.keyLog(msg)
	}
}

func (s *Surface) keyFeed(msg tea.KeyMsg) (tea.Cmd, bool) {
	rows := s.filteredRuns()
	switch msg.String() {
	case "up", "k":
		if s.selRun > 0 {
			s.selRun--
			s.rev++
		}
		return nil, true
	case "down", "j":
		if s.selRun < len(rows)-1 {
			s.selRun++
			s.rev++
		}
		return nil, true
	case "f":
		s.facet = (s.facet + 1) % len(facets)
		s.selRun = 0
		s.rev++
		return nil, true
	case "enter":
		if s.selRun < len(rows) {
			id := rows[s.selRun].ExecID
			if data.IsCloudRun(id) {
				// Cloud rows are feed-only until the console-detail lane
				// lands; drilling explains instead of faking a detail.
				return func() tea.Msg {
					return shell.NoticeMsg{Text: "cloud run — open it in orun cloud (detail lane is a TR8 follow-up)"}
				}, true
			}
			s.pinned = id
			s.run = viewmodel.RunView{}
			s.level = levelRun
			s.selJob = 0
			s.rev++
			return s.loadRunCmd(s.pinned), true
		}
		return nil, true
	}
	return nil, false
}

func (s *Surface) keyRun(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "up", "k":
		if s.selJob > 0 {
			s.selJob--
			s.rev++
		}
		return nil, true
	case "down", "j":
		if s.selJob < len(s.run.Jobs)-1 {
			s.selJob++
			s.rev++
		}
		return nil, true
	case "enter":
		if s.selJob < len(s.run.Jobs) {
			s.level = levelJob
			s.selStep = 0
			s.rev++
		}
		return nil, true
	}
	return nil, false
}

func (s *Surface) keyJob(msg tea.KeyMsg) (tea.Cmd, bool) {
	job := s.currentJob()
	switch msg.String() {
	case "up", "k":
		if s.selStep > 0 {
			s.selStep--
			s.rev++
		}
		return nil, true
	case "down", "j":
		if job != nil && s.selStep < len(job.Steps)-1 {
			s.selStep++
			s.rev++
		}
		return nil, true
	case "enter":
		if job != nil && s.selStep < len(job.Steps) {
			step := job.Steps[s.selStep]
			s.level = levelLog
			s.logKey = s.pinned + "/" + job.ID + "/" + step.ID
			s.logText = ""
			s.logScroll = 0
			s.rev++
			return s.loadLogCmd(s.pinned, job.ID, step.ID), true
		}
		return nil, true
	}
	return nil, false
}

func (s *Surface) keyLog(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "up", "k":
		s.logScroll++
		s.rev++
		return nil, true
	case "down", "j":
		if s.logScroll > 0 {
			s.logScroll--
			s.rev++
		}
		return nil, true
	case "pgup":
		s.logScroll += 20
		s.rev++
		return nil, true
	case "pgdown":
		s.logScroll = max(0, s.logScroll-20)
		s.rev++
		return nil, true
	case "e":
		s.errsOnly = !s.errsOnly
		s.logScroll = 0
		s.rev++
		return nil, true
	}
	return nil, false
}

// --- helpers ----------------------------------------------------------------

func (s *Surface) filteredRuns() []viewmodel.RunSummary {
	if facets[s.facet] == "all" {
		return s.runs.Runs
	}
	var out []viewmodel.RunSummary
	want := facets[s.facet]
	for _, r := range s.runs.Runs {
		st := strings.ToLower(r.Status)
		if st == want || (want == "completed" && (st == "success" || st == "ok")) ||
			(want == "failed" && st == "error") || (want == "running" && st == "in_progress") {
			out = append(out, r)
		}
	}
	return out
}

func (s *Surface) clampSel() {
	if n := len(s.filteredRuns()); s.selRun >= n {
		s.selRun = max(0, n-1)
	}
	if s.selJob >= len(s.run.Jobs) {
		s.selJob = max(0, len(s.run.Jobs)-1)
	}
}

func (s *Surface) currentJob() *viewmodel.Job {
	if s.selJob < len(s.run.Jobs) {
		return &s.run.Jobs[s.selJob]
	}
	return nil
}

func (s *Surface) currentStepRunning() bool {
	job := s.currentJob()
	if job == nil || s.selStep >= len(job.Steps) {
		return false
	}
	st := strings.ToLower(job.Steps[s.selStep].Status)
	return st == "running" || st == "in_progress"
}

// Close releases the subscription context.
func (s *Surface) Close() { s.cancel() }
