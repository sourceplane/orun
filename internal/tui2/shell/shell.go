package shell

import (
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sourceplane/orun/internal/tui2/frame"
)

// Chrome geometry: header, rule, stage, rule, status. The heights are
// constants — the v1 pattern of rendering the chrome to measure it is gone.
const (
	chromeLines   = 4
	minWidth      = 20
	minHeight     = 5
	noticeFor     = 2500 * time.Millisecond
	quitGuardFor  = 2 * time.Second
	noticeAnimID  = "shell.notice"
	spinnerFrames = "⣾⣽⣻⢿⡿⣟⣯⣷"
)

var (
	styleActive = lipgloss.NewStyle().Bold(true)
	styleDim    = lipgloss.NewStyle().Faint(true)
)

// Messages the shell folds. Commands request shell effects by yielding
// these rather than mutating the shell directly.
type (
	// GotoMsg activates a surface by ID.
	GotoMsg struct{ ID string }
	// OpenPaletteMsg opens the command palette.
	OpenPaletteMsg struct{}
	// OpenHelpMsg opens generated help.
	OpenHelpMsg struct{}
	// NoticeMsg puts a transient notice on the status line.
	NoticeMsg struct{ Text string }
)

// Config assembles a shell.
type Config struct {
	Surfaces []Surface
	// Scope is what the header's right edge shows ("local", "acme/platform").
	Scope string
	// Now overrides the clock (tests); nil means time.Now.
	Now func() time.Time
}

// Shell is the root Bubble Tea model.
type Shell struct {
	size  frame.Size
	ready bool

	router   *Router
	overlays []Overlay
	reg      *Registry
	sched    *frame.Scheduler

	scope       string
	notice      string
	noticeAt    time.Time
	quitArmedAt time.Time
	spin        int
	now         func() time.Time

	memoHeader frame.Memo
	memoStage  frame.Memo
	memoStatus frame.Memo
}

// New assembles the shell: router over the surfaces, the one scheduler,
// and the command registry seeded with navigation, help, palette, and quit.
func New(cfg Config) *Shell {
	s := &Shell{
		router: NewRouter(cfg.Surfaces),
		reg:    NewRegistry(),
		sched:  frame.NewScheduler(),
		scope:  cfg.Scope,
		now:    cfg.Now,
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.scope == "" {
		s.scope = "local"
	}
	for _, sf := range cfg.Surfaces {
		if ss, ok := sf.(ScheduledSurface); ok {
			ss.SetScheduler(s.sched)
		}
	}
	for i, sf := range cfg.Surfaces {
		id, title := sf.ID(), sf.Title()
		s.reg.Register(Command{
			ID:    "goto." + id,
			Title: "Go to " + title,
			Keys:  []string{strconv.Itoa(i + 1)},
			Run:   func() tea.Cmd { return func() tea.Msg { return GotoMsg{ID: id} } },
		})
	}
	s.reg.Register(Command{
		ID: "ui.palette", Title: "Command palette", Keys: []string{":", "ctrl+k"},
		Run: func() tea.Cmd { return func() tea.Msg { return OpenPaletteMsg{} } },
	})
	s.reg.Register(Command{
		ID: "ui.help", Title: "Help", Keys: []string{"?"},
		Run: func() tea.Cmd { return func() tea.Msg { return OpenHelpMsg{} } },
	})
	s.reg.Register(Command{
		ID: "app.quit", Title: "Quit", Keys: []string{"ctrl+c ctrl+c"},
		Run: func() tea.Cmd { return tea.Quit },
	})
	return s
}

// Registry exposes the command bus (surfaces register their commands here;
// tests drive it directly).
func (s *Shell) Registry() *Registry { return s.reg }

// Scheduler exposes the animation scheduler (the idle invariants are
// asserted against it).
func (s *Shell) Scheduler() *frame.Scheduler { return s.sched }

// Router exposes navigation state for tests.
func (s *Shell) Router() *Router { return s.router }

// OverlayDepth reports how many overlays are open.
func (s *Shell) OverlayDepth() int { return len(s.overlays) }

// MemoStats sums region-cache hits and misses — the bench's evidence that
// idle ticks are region-only.
func (s *Shell) MemoStats() (hits, misses uint64) {
	for _, m := range []*frame.Memo{&s.memoHeader, &s.memoStage, &s.memoStatus} {
		h, mi := m.Stats()
		hits += h
		misses += mi
	}
	return hits, misses
}

// Init implements tea.Model.
func (s *Shell) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, sf := range s.router.Surfaces() {
		cmds = append(cmds, sf.Init())
	}
	return tea.Batch(cmds...)
}

// Update implements tea.Model. It folds messages and never performs I/O —
// data arrives in messages produced by tea.Cmds (design §13.2).
func (s *Shell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.size = frame.Size{Width: msg.Width, Height: msg.Height}
		s.ready = true
		return s, nil

	case frame.TickMsg:
		s.spin++
		if s.notice != "" && s.now().Sub(s.noticeAt) > noticeFor {
			s.notice = ""
			s.sched.Remove(noticeAnimID)
		}
		cmds := []tea.Cmd{s.forwardAll(msg)}
		cmds = append(cmds, s.sched.OnTick())
		return s, tea.Batch(cmds...)

	case tea.KeyMsg:
		return s, s.handleKey(msg)

	case GotoMsg:
		s.router.ActivateID(msg.ID)
		return s, nil

	case OpenPaletteMsg:
		s.overlays = append(s.overlays, NewPalette(s.reg))
		return s, nil

	case OpenHelpMsg:
		s.overlays = append(s.overlays, NewHelp(s.reg))
		return s, nil

	case NoticeMsg:
		s.setNotice(msg.Text)
		return s, s.sched.Arm()
	}

	// Data messages reach every surface: a run finishing must land in
	// Activity even while the user is on Home.
	return s, tea.Batch(s.forwardAll(msg), s.sched.Arm())
}

func (s *Shell) forwardAll(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for _, sf := range s.router.Surfaces() {
		cmds = append(cmds, sf.Update(msg))
	}
	return tea.Batch(cmds...)
}

func (s *Shell) setNotice(text string) {
	s.notice = text
	s.noticeAt = s.now()
	s.sched.Add(noticeAnimID)
}

// handleKey routes one key event: quit guard → overlay stack → focused
// input → globals → surface (design §6, "the focus model").
func (s *Shell) handleKey(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()

	// The quit guard outranks everything: ctrl+c twice within the window
	// quits from any state; esc never does (design §13.5).
	if k == "ctrl+c" {
		if s.now().Sub(s.quitArmedAt) < quitGuardFor {
			return tea.Quit
		}
		s.quitArmedAt = s.now()
		s.setNotice("press ctrl+c again to quit")
		return s.sched.Arm()
	}

	if len(s.overlays) > 0 {
		if k == "esc" {
			s.overlays = s.overlays[:len(s.overlays)-1]
			return nil
		}
		top := s.overlays[len(s.overlays)-1]
		cmd, done := top.HandleKey(msg)
		if done {
			s.overlays = s.overlays[:len(s.overlays)-1]
		}
		return cmd
	}

	sf := s.router.Active()

	// A focused input owns the keyboard: only chords stay global, so no
	// global shortcut can ever eat typed text.
	if sf.InputFocused() {
		if k == "ctrl+k" {
			s.overlays = append(s.overlays, NewPalette(s.reg))
			return nil
		}
		cmd, _ := sf.HandleKey(msg)
		return tea.Batch(cmd, s.sched.Arm())
	}

	switch k {
	case ":", "ctrl+k":
		s.overlays = append(s.overlays, NewPalette(s.reg))
		return nil
	case "?":
		s.overlays = append(s.overlays, NewHelp(s.reg))
		return nil
	case "esc":
		if !sf.Pop() {
			s.router.Back()
		}
		return nil
	}
	if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
		if s.router.Activate(int(k[0] - '1')) {
			return nil
		}
	}
	cmd, _ := sf.HandleKey(msg)
	return tea.Batch(cmd, s.sched.Arm())
}

// View implements tea.Model: header, rule, stage, rule, status — five
// memoized bands whose heights are facts, joined and never clipped.
func (s *Shell) View() string {
	if !s.ready {
		return ""
	}
	if s.size.Width < minWidth || s.size.Height < minHeight {
		return frame.Fit("terminal too small", s.size)
	}

	lineW := frame.Size{Width: s.size.Width, Height: 1}
	stageSize := frame.Size{Width: s.size.Width, Height: s.size.Height - chromeLines}

	header := s.memoHeader.Render(s.headerKey(), lineW, func() string {
		return s.renderHeader(s.size.Width)
	})

	sf := s.router.Active()
	stage := s.memoStage.Render(sf.ID()+"/"+sf.Rev(), stageSize, func() string {
		return frame.Fit(sf.View(stageSize), stageSize)
	})
	if n := len(s.overlays); n > 0 {
		top := s.overlays[n-1]
		stage = frame.Compose(stage, top.View(stageSize), stageSize)
	}

	status := s.memoStatus.Render(s.statusKey(), lineW, func() string {
		return s.renderStatus(s.size.Width)
	})

	rule := styleDim.Render(strings.Repeat("─", s.size.Width))
	return header + "\n" + rule + "\n" + stage + "\n" + rule + "\n" + status
}

func (s *Shell) headerKey() string {
	return strconv.Itoa(s.router.ActiveIndex()) + "/" + s.scope
}

func (s *Shell) renderHeader(width int) string {
	var b strings.Builder
	b.WriteString(" ")
	b.WriteString(styleActive.Render("orun"))
	b.WriteString("  ")
	for i, sf := range s.router.Surfaces() {
		label := sf.Title()
		if i == s.router.ActiveIndex() {
			b.WriteString(styleActive.Render(label))
		} else {
			b.WriteString(styleDim.Render(label))
		}
		b.WriteString("  ")
	}
	left := b.String()
	right := styleDim.Render(s.scope) + " "
	return joinEnds(left, right, width)
}

func (s *Shell) statusKey() string {
	key := s.notice + "/" + s.scope
	if s.sched.Active() {
		key += "/" + strconv.Itoa(s.spin)
	}
	return key
}

func (s *Shell) renderStatus(width int) string {
	var left string
	if s.sched.Active() {
		left = " " + string([]rune(spinnerFrames)[s.spin%len([]rune(spinnerFrames))]) + " "
	} else {
		left = "   "
	}
	if s.notice != "" {
		left += s.notice
	}
	right := styleDim.Render("⏺ "+s.scope+"   :cmd ?help") + " "
	return joinEnds(left, right, width)
}

// joinEnds lays left and right at the edges of one width-wide line.
func joinEnds(left, right string, width int) string {
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	gap := width - lw - rw
	if gap < 1 {
		return frame.FitLine(left, width)
	}
	return left + strings.Repeat(" ", gap) + right
}
