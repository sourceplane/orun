// Package tui implements the Orun Cockpit terminal UI.
//
// Cockpit redesign (claude-code / linear aesthetic)
// =================================================
//
// LAYOUT
//
//	┌─────────────────────────────────────────────────────────────────┐
//	│ breadcrumb: intent · env · plan · run        (accent rule line) │
//	├──────────┬──────────────────────────────────────────┬───────────┤
//	│ sidebar  │ STAGE — one dominant view per mode       │ inspector │
//	│ (rail)   │   Browse · Plan · Run · Logs · History   │ (drawer)  │
//	│          │                                          │ optional  │
//	├──────────┴──────────────────────────────────────────┴───────────┤
//	│ key hints (context-aware)                                       │
//	│ status: spinner + last toast                                    │
//	└─────────────────────────────────────────────────────────────────┘
//
// The sidebar is collapsible (tab) and degrades to an icon-only rail
// below 100 cols. The inspector drawer is hidden by default and slides
// in on `i` or row selection; it disappears entirely below 100 cols.
// Below 70 cols only the stage renders.
//
// MODE SWITCHING — number keys 1..5 jump to Browse/Plan/Run/Logs/History;
// `:` opens a real fuzzy command palette; `/` opens a slash-search bar
// that filters the active view's rows; `?` opens the help modal; `q`
// quits. The dry-run dispatch path (PlanStudioDryRunRequestedMsg) is
// preserved verbatim so callers still get ModeRunDashboard on success
// and remain in ModePlanStudio on error.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/theme"
	"github.com/sourceplane/orun/internal/tui/views"
)

// Mode identifies which primary view occupies the stage.
type Mode int

const (
	ModeBrowse Mode = iota
	ModePlanStudio
	ModeRunDashboard
	ModeLogExplorer
	ModeHistory
	ModeActivity
)

// ModeComponentStudio is an alias for the inline Compose surface that
// is reached via `enter` on a component in Browse. The underlying
// PlanStudioModel is reused as the implementation engine.
const ModeComponentStudio = ModePlanStudio

func (m Mode) String() string {
	switch m {
	case ModeBrowse:
		return "browse"
	case ModePlanStudio:
		return "plan-studio"
	case ModeRunDashboard:
		return "run-dashboard"
	case ModeLogExplorer:
		return "log-explorer"
	case ModeHistory:
		return "history"
	case ModeActivity:
		return "activity"
	}
	return "unknown"
}

func (m Mode) sidebarKey() string {
	switch m {
	case ModeBrowse, ModePlanStudio:
		return "browse"
	}
	return "activity"
}

// Panel — legacy enum retained so the few remaining test references keep
// compiling; the cockpit's real focus model is mode + drawer visibility.
type Panel int

const (
	PanelNavigator Panel = iota
	PanelMain
	PanelInspector
)

// Model is the root Bubble Tea model for the cockpit.
type Model struct {
	width, height int

	activeMode  Mode
	activePanel Panel

	// Layout state
	sidebarCollapsed bool
	showInspector    bool
	showBottom       bool // optional bottom panel (Activity mode)
	// prefs tracks the persisted user preference for sidebar+inspector
	// visibility. Responsive-mode overrides (sub-100-cols) mutate the
	// runtime flags but leave prefs intact so the next resize restores.
	prefs Prefs

	// Children views
	navigator  views.NavigatorModel
	browse     views.BrowseModel
	history    views.HistoryModel
	planStudio views.PlanStudioModel
	runView    views.RunViewModel
	logView    views.LogExplorerModel
	activity   views.ActivityModel
	inspector  views.InspectorModel

	// Plan cached on dispatch so Activity can render a DAG for the
	// currently in-flight run. Nil between runs.
	livePlan   *model.Plan
	liveExecID string

	// Component Studio context — set when the user presses `enter` on a
	// component in Browse; cleared when they `esc` back out.
	studioComponent string

	// History stacks for back/forward navigation. We push the current mode
	// onto navBack whenever the user moves to a new mode; esc/backspace/ctrl+o
	// pops navBack and pushes the popped value onto navFwd so ctrl+i can
	// redo the navigation. Cleared on explicit jumps from the palette.
	navBack []Mode
	navFwd  []Mode

	// Active run execution ID, used to scope TailLogs.
	currentExecID string

	// logCancel cancels the in-flight follow-mode log tail. We keep at most
	// one live tail: starting a new one (or the run finishing) cancels the
	// previous so follow goroutines never leak across navigations.
	logCancel context.CancelFunc

	// Overlays
	commandPalette     views.CommandPaletteModel
	showHelp           bool
	showCommandPalette bool

	// Real-run confirm overlay
	showConfirm bool
	pendingRun  *PendingRun

	// Spinner state for run kickoff (drives the "Starting run…" card before
	// the first RunEvent arrives).
	runStarting bool

	// Slash search
	searchActive bool
	search       textinput.Model

	// Async / status
	svc       services.OrunService
	workspace *services.WorkspaceSnapshot
	loading   bool
	lastErr   error
	toast     string
	toastAt   time.Time
	spinner   spinner.Model

	keys GlobalKeyMap
}

// NewModel constructs the root model in its default starting state.
func NewModel(svc services.OrunService) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = theme.StyleAccent

	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "search…"
	ti.CharLimit = 64

	prefs := LoadPrefs()

	return Model{
		svc:              svc,
		activeMode:       ModeBrowse,
		activePanel:      PanelMain,
		navigator:        views.NewNavigatorModel(),
		browse:           views.NewBrowseModel(),
		history:          views.NewHistoryModel(),
		planStudio:       views.NewPlanStudioModel(),
		runView:          views.NewRunViewModel(),
		logView:          views.NewLogExplorerModel(),
		activity:         views.NewActivityModel(),
		inspector:        views.NewInspectorModel(),
		commandPalette:   views.NewCommandPaletteModel(),
		loading:          true,
		spinner:          sp,
		search:           ti,
		keys:             DefaultGlobalKeyMap(),
		prefs:            prefs,
		sidebarCollapsed: prefs.SidebarCollapsed,
		showInspector:    prefs.InspectorVisible,
		showBottom:       prefs.BottomPanelVisible,
	}
}

// --- Accessors (for tests) -------------------------------------------------

func (m Model) ActiveMode() Mode                       { return m.activeMode }
func (m Model) ActivePanel() Panel                     { return m.activePanel }
func (m Model) Workspace() *services.WorkspaceSnapshot { return m.workspace }
func (m Model) LastError() error                       { return m.lastErr }

// Init kicks off workspace loading, starts the spinner, and arms the live-view
// refresh ticker so the cockpit reflects external writes without a keystroke.
func (m Model) Init() tea.Cmd {
	return tea.Batch(loadWorkspaceCmd(m.svc), m.spinner.Tick, catalogRefreshTickCmd())
}

func loadWorkspaceCmd(svc services.OrunService) tea.Cmd {
	return func() tea.Msg {
		snap, err := svc.LoadWorkspace(context.Background(), services.WorkspaceRequest{})
		return services.WorkspaceLoadedMsg{Snapshot: snap, Err: err}
	}
}

func listRunsCmd(svc services.OrunService) tea.Cmd {
	return func() tea.Msg {
		runs, err := svc.ListRuns(context.Background(), services.ListRunsRequest{Limit: 50})
		return services.RunsListedMsg{Runs: runs, Err: err}
	}
}

// Update is the canonical bubbletea reducer.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.applyResponsive()
		m.propagateSize()
		return m, nil

	case spinner.TickMsg:
		var c tea.Cmd
		m.spinner, c = m.spinner.Update(msg)
		return m, c

	case services.WorkspaceLoadedMsg:
		m.loading = false
		if msg.Err != nil {
			m.lastErr = msg.Err
			return m, nil
		}
		m.lastErr = nil
		m.workspace = msg.Snapshot
		m.browse.Workspace = msg.Snapshot
		if msg.Snapshot != nil && msg.Snapshot.IntentFile != "" {
			m.planStudio = m.planStudio.SetRequest(services.PlanRequest{
				IntentFile: msg.Snapshot.IntentFile,
			})
		}
		if msg.Snapshot != nil {
			m.planStudio = m.planStudio.SetCustomization(
				msg.Snapshot.Environments,
				[]string{"manual", "ci", "schedule", "promote"},
			)
		}
		m.refreshInspectorSelection()
		return m, listRunsCmd(m.svc)

	case catalogRefreshTickMsg:
		// Live view (D-9): silently reload the workspace and re-arm the tick.
		// The reload runs off the UI thread; the result lands as
		// workspaceRefreshedMsg.
		return m, tea.Batch(backgroundReloadCmd(m.svc), catalogRefreshTickCmd())

	case workspaceRefreshedMsg:
		// Best-effort: a failed background refresh keeps the current snapshot
		// (the live view never replaces good data with a transient error).
		if msg.Err != nil || msg.Snapshot == nil {
			return m, nil
		}
		m.workspace = msg.Snapshot
		m.browse.Workspace = msg.Snapshot
		m.refreshInspectorSelection()
		return m, nil

	case services.RunsListedMsg:
		if msg.Err == nil {
			m.history.Runs = msg.Runs
			m.refreshActivityRuns()
		}
		return m, nil

	case services.PlanGeneratedMsg:
		m.planStudio, _ = m.planStudio.Update(msg)
		if msg.Err != nil {
			m.lastErr = msg.Err
			return m, m.setToast("plan error: " + msg.Err.Error())
		}
		m.lastErr = nil
		m.refreshInspectorSelection()
		return m, m.setToast(fmt.Sprintf("plan ready · %d jobs", msg.Result.JobCount))

	case views.PlanStudioSaveRequestedMsg:
		if m.planStudio.Result == nil {
			return m, nil
		}
		req := m.planStudio.Request
		req.NamedPlan = msg.Name
		m.planStudio = m.planStudio.MarkGenerating()
		return m, views.GeneratePlanCmd(m.svc, req)

	case views.ComponentEnterMsg:
		m.studioComponent = msg.Name
		// Seed request: workspace defaults + sticky per-component prefs.
		req := services.PlanRequest{
			Components: []string{msg.Name},
		}
		if m.workspace != nil {
			req.IntentFile = m.workspace.IntentFile
		}
		if cp, ok := m.prefs.PerComponent[msg.Name]; ok {
			req.Environment = cp.Env
			req.TriggerName = cp.Trigger
			req.ChangedOnly = cp.ChangedOnly
		}
		m.planStudio = m.planStudio.SetRequest(req)
		m.planStudio = m.planStudio.MarkGenerating()
		m = m.switchMode(ModePlanStudio)
		return m, views.GeneratePlanCmd(m.svc, req)

	case views.PlanStudioDryRunRequestedMsg:
		ch, err := m.svc.RunPlan(context.Background(), services.RunRequest{
			Plan:   msg.Plan,
			DryRun: true,
		})
		if err != nil {
			m.lastErr = err
			return m, nil
		}
		m.lastErr = nil
		// Stash plan context so the kickoff card can show checksum/jobs.
		jobs := 0
		checksum := ""
		if m.planStudio.Result != nil {
			jobs = m.planStudio.Result.JobCount
			checksum = m.planStudio.Result.Checksum
		}
		if jobs == 0 {
			jobs = len(msg.Plan.Jobs)
		}
		m.pendingRun = &PendingRun{
			Plan:     msg.Plan,
			Env:      m.planStudio.Request.Environment,
			Checksum: checksum,
			Jobs:     jobs,
		}
		m.runStarting = true
		m.livePlan = msg.Plan
		m.liveExecID = ""
		var cmd tea.Cmd
		m.runView, cmd = m.runView.StartStream(ch, true)
		m.refreshActivityRuns()
		m = m.switchMode(ModeActivity)
		return m, tea.Batch(cmd, m.spinner.Tick)

	case views.PlanStudioRealRunRequestedMsg:
		// Stash the pending run and pop a confirm modal — actual dispatch
		// happens on `y` in handleKey.
		if msg.Plan == nil {
			return m, nil
		}
		checksum := ""
		jobs := 0
		env := ""
		if m.planStudio.Result != nil {
			checksum = m.planStudio.Result.Checksum
			jobs = m.planStudio.Result.JobCount
		}
		if jobs == 0 {
			jobs = len(msg.Plan.Jobs)
		}
		env = m.planStudio.Request.Environment
		m.pendingRun = &PendingRun{
			Plan:     msg.Plan,
			Env:      env,
			Checksum: checksum,
			Jobs:     jobs,
		}
		m.showConfirm = true
		return m, nil

	case views.RunJobSelectedMsg:
		// Follow only while the run is still streaming; a finished run's logs
		// are read once and the channel closes.
		follow := !m.runView.Done()
		ctx := m.newLogContext(follow)
		ch, err := m.svc.TailLogs(ctx, services.LogRequest{
			ExecID: msg.ExecID,
			JobID:  msg.JobID,
			StepID: msg.StepID,
			Follow: follow,
		})
		if err != nil {
			m.lastErr = err
			return m, nil
		}
		m.lastErr = nil
		var cmd tea.Cmd
		m.logView, cmd = m.logView.Attach(ch, msg.JobID, msg.StepID, follow)
		m = m.switchMode(ModeLogExplorer)
		return m, cmd

	case views.ActivityTailLogsMsg:
		ctx := m.newLogContext(msg.Live)
		ch, err := m.svc.TailLogs(ctx, services.LogRequest{
			ExecID: msg.ExecID,
			JobID:  msg.JobID,
			StepID: msg.StepID,
			Follow: msg.Live,
		})
		if err != nil {
			m.lastErr = err
			return m, nil
		}
		m.lastErr = nil
		var cmd tea.Cmd
		m.activity, cmd = m.activity.AttachLogs(ch, msg.JobID, msg.StepID)
		return m, cmd

	case services.RunEventMsg:
		// First event of any kind clears the kickoff spinner state.
		if m.runStarting {
			m.runStarting = false
		}
		// Learn the execution ID from the stream so live log tailing and the
		// activity run row resolve to the real run rather than an empty ID.
		if msg.Event.ExecID != "" && m.liveExecID == "" {
			m.liveExecID = msg.Event.ExecID
		}
		var cmd tea.Cmd
		m.runView, cmd = m.runView.Update(msg)
		m.refreshActivityRuns()
		cmds := []tea.Cmd{cmd}
		if msg.Event.Kind == services.RunEventRunDone {
			// The run is finished and all step logs are on disk. Give the
			// follow poll one more interval to drain the final step, then
			// stop it so the tail goroutine exits cleanly.
			cmds = append(cmds, tea.Tick(600*time.Millisecond, func(time.Time) tea.Msg {
				return stopFollowMsg{}
			}))
		}
		return m, tea.Batch(cmds...)

	case stopFollowMsg:
		if m.logCancel != nil {
			m.logCancel()
			m.logCancel = nil
		}
		return m, nil

	case services.LogEventMsg:
		// Route to whichever surface is consuming logs.
		var cmd tea.Cmd
		m.logView, cmd = m.logView.Update(msg)
		var cmd2 tea.Cmd
		m.activity, cmd2 = m.activity.Update(msg)
		return m, tea.Batch(cmd, cmd2)

	case ToastTickMsg:
		if m.toast != "" && time.Since(m.toastAt) >= 10*time.Second {
			m.toast = ""
		}
		if m.toast != "" {
			return m, toastTickCmd()
		}
		return m, nil

	case views.PaletteCommandSelectedMsg:
		return m.applyPaletteCommand(msg.Command)

	case services.ErrMsg:
		m.lastErr = msg.Err
		if m.activeMode == ModePlanStudio {
			m.planStudio, _ = m.planStudio.Update(msg)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Overlay routing first.
	if m.showConfirm {
		switch msg.String() {
		case "y", "Y":
			return m.dispatchPendingRun()
		case "n", "N", "esc":
			m.showConfirm = false
			m.pendingRun = nil
			return m, nil
		}
		// Block all other input while modal is open.
		return m, nil
	}
	if m.showCommandPalette {
		if key.Matches(msg, m.keys.Cancel) {
			m.commandPalette = m.commandPalette.Close()
			m.showCommandPalette = false
			return m, nil
		}
		var cmd tea.Cmd
		m.commandPalette, cmd = m.commandPalette.Update(msg)
		return m, cmd
	}
	if m.showHelp {
		if key.Matches(msg, m.keys.Cancel) || key.Matches(msg, m.keys.Help) {
			m.showHelp = false
			return m, nil
		}
		if key.Matches(msg, m.keys.Palette) {
			m.showHelp = false
			m.commandPalette = m.commandPalette.SetWidth(m.width).Open()
			m.showCommandPalette = true
		}
		return m, nil
	}
	if m.searchActive {
		switch msg.String() {
		case "esc":
			m.searchActive = false
			m.search.Blur()
			m.search.SetValue("")
			m.applySearch("")
			return m, nil
		case "enter":
			m.searchActive = false
			m.search.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		m.applySearch(m.search.Value())
		return m, cmd
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Reload):
		m.loading = true
		m.lastErr = nil
		return m, loadWorkspaceCmd(m.svc)
	case key.Matches(msg, m.keys.ToggleMode):
		// Tab cycles between Components and Activity at the top level.
		if m.activeMode == ModeBrowse {
			mm := m.switchMode(ModeActivity)
			_, cmd := mm.activity.AutoAttachCmd()
			return mm, cmd
		}
		return m.switchMode(ModeBrowse), nil
	case key.Matches(msg, m.keys.ToggleSidebar):
		m.sidebarCollapsed = !m.sidebarCollapsed
		m.prefs.SidebarCollapsed = m.sidebarCollapsed
		SavePrefs(m.prefs)
		return m, nil
	case key.Matches(msg, m.keys.ToggleInspector):
		m.showInspector = !m.showInspector
		m.prefs.InspectorVisible = m.showInspector
		SavePrefs(m.prefs)
		if m.showInspector {
			m.refreshInspectorSelection()
		}
		return m, nil
	case key.Matches(msg, m.keys.Help):
		m.showHelp = true
		return m, nil
	case key.Matches(msg, m.keys.Palette):
		m.commandPalette = m.commandPalette.SetWidth(m.width).Open()
		m.showCommandPalette = true
		return m, nil
	case key.Matches(msg, m.keys.Search):
		if m.activeMode == ModeBrowse || m.activeMode == ModeHistory || m.activeMode == ModeLogExplorer {
			m.searchActive = true
			m.search.SetValue("")
			m.search.Focus()
		}
		return m, nil
	case key.Matches(msg, m.keys.NextPanel):
		m.activePanel = nextPanel(m.activePanel)
		return m, nil
	case key.Matches(msg, m.keys.PrevPanel):
		m.activePanel = prevPanel(m.activePanel)
		return m, nil
	case key.Matches(msg, m.keys.ToggleBottom):
		if m.activeMode == ModeActivity {
			m.showBottom = !m.showBottom
			m.prefs.BottomPanelVisible = m.showBottom
			SavePrefs(m.prefs)
		}
		return m, nil
	case key.Matches(msg, m.keys.Back):
		// Inside Activity, prefer popping a drilldown level before
		// falling back to global mode-back history.
		if m.activeMode == ModeActivity && !m.activity.AtRoot() {
			var cmd tea.Cmd
			m.activity, cmd = m.activity.Update(msg)
			return m, cmd
		}
		if m.activeMode == ModePlanStudio && !m.planStudio.AtRoot() {
			var cmd tea.Cmd
			m.planStudio, cmd = m.planStudio.Update(msg)
			m.refreshInspectorSelection()
			return m, cmd
		}
		return m.goBack(), nil
	case key.Matches(msg, m.keys.Forward):
		return m.goForward(), nil
	case key.Matches(msg, m.keys.Cancel):
		// Esc when no overlay is open: inside Activity, pop drilldown
		// level first; otherwise act as "back".
		if m.activeMode == ModeActivity && !m.activity.AtRoot() {
			var cmd tea.Cmd
			m.activity, cmd = m.activity.Update(msg)
			return m, cmd
		}
		if m.activeMode == ModePlanStudio && !m.planStudio.AtRoot() {
			var cmd tea.Cmd
			m.planStudio, cmd = m.planStudio.Update(msg)
			m.refreshInspectorSelection()
			return m, cmd
		}
		if len(m.navBack) > 0 {
			return m.goBack(), nil
		}
	case key.Matches(msg, m.keys.GoBrowse):
		return m.switchMode(ModeBrowse), nil
	case key.Matches(msg, m.keys.GoActivity):
		mm := m.switchMode(ModeActivity)
		_, cmd := mm.activity.AutoAttachCmd()
		return mm, cmd
	case key.Matches(msg, m.keys.GoPlan):
		return m.switchMode(ModePlanStudio), nil
	case key.Matches(msg, m.keys.GoRun):
		return m.switchMode(ModeRunDashboard), nil
	case key.Matches(msg, m.keys.GoLogs):
		return m.switchMode(ModeLogExplorer), nil
	case key.Matches(msg, m.keys.GoHistory):
		return m.switchMode(ModeHistory), nil
	}

	// Forward to active view.
	return m.forwardKey(msg)
}

func (m Model) forwardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.activeMode {
	case ModeBrowse:
		var c tea.Cmd
		m.browse, c = m.browse.Update(msg)
		m.refreshInspectorSelection()
		return m, c
	case ModePlanStudio:
		var cmd tea.Cmd
		m.planStudio, cmd = m.planStudio.Update(msg)
		if cmd == nil && key.Matches(msg, m.planStudio.KeyMap().Generate) {
			cmd = views.GeneratePlanCmd(m.svc, m.planStudio.Request)
		}
		// Persist sticky per-component prefs whenever the user cycled env,
		// trigger, or toggled changed-only via e / t / c.
		if m.studioComponent != "" {
			switch msg.String() {
			case "e", "t", "c":
				if m.prefs.PerComponent == nil {
					m.prefs.PerComponent = map[string]ComponentPrefs{}
				}
				m.prefs.PerComponent[m.studioComponent] = ComponentPrefs{
					Env:         m.planStudio.Request.Environment,
					Trigger:     m.planStudio.Request.TriggerName,
					ChangedOnly: m.planStudio.Request.ChangedOnly,
				}
				SavePrefs(m.prefs)
			}
		}
		m.refreshInspectorSelection()
		return m, cmd
	case ModeHistory:
		var c tea.Cmd
		m.history, c = m.history.Update(msg)
		m.refreshInspectorSelection()
		return m, c
	case ModeRunDashboard:
		var c tea.Cmd
		m.runView, c = m.runView.Update(msg)
		return m, c
	case ModeLogExplorer:
		var c tea.Cmd
		m.logView, c = m.logView.Update(msg)
		return m, c
	case ModeActivity:
		var c tea.Cmd
		m.activity, c = m.activity.Update(msg)
		m.refreshInspectorSelection()
		return m, c
	}
	return m, nil
}

func (m Model) switchMode(target Mode) Model {
	if target != m.activeMode {
		m.navBack = append(m.navBack, m.activeMode)
		// New forward navigation invalidates the redo stack.
		m.navFwd = nil
	}
	m.activeMode = target
	m.navigator = m.navigator.SetActiveMode(target.sidebarKey())
	// When entering Activity, force the inspector on so the user sees job
	// details immediately. We don't persist this to prefs — it's a per-mode
	// affordance, not a sticky preference.
	if target == ModeActivity && m.width >= 100 {
		m.showInspector = true
	}
	if target == ModePlanStudio && m.width >= 100 {
		m.showInspector = true
	}
	m.refreshInspectorSelection()
	return m
}

// goBack pops the back-stack into the active mode, pushing the current
// mode onto the forward stack so ctrl+i can redo. No-op if the stack
// is empty.
func (m Model) goBack() Model {
	if len(m.navBack) == 0 {
		return m
	}
	prev := m.navBack[len(m.navBack)-1]
	m.navBack = m.navBack[:len(m.navBack)-1]
	m.navFwd = append(m.navFwd, m.activeMode)
	m.activeMode = prev
	m.navigator = m.navigator.SetActiveMode(prev.sidebarKey())
	return m
}

// goForward is the inverse of goBack.
func (m Model) goForward() Model {
	if len(m.navFwd) == 0 {
		return m
	}
	next := m.navFwd[len(m.navFwd)-1]
	m.navFwd = m.navFwd[:len(m.navFwd)-1]
	m.navBack = append(m.navBack, m.activeMode)
	m.activeMode = next
	m.navigator = m.navigator.SetActiveMode(next.sidebarKey())
	return m
}

func (m *Model) applySearch(q string) {
	switch m.activeMode {
	case ModeBrowse:
		m.browse = m.browse.SetFilter(q)
	case ModeHistory:
		m.history = m.history.SetFilter(q)
	}
}

func (m *Model) refreshInspectorSelection() {
	switch m.activeMode {
	case ModeBrowse:
		if sel := m.browse.Selected(); sel != nil {
			m.inspector = m.inspector.SetDescription(componentDesc(sel, m.history.Runs))
		}
	case ModeHistory:
		if sel := m.history.Selected(); sel != nil {
			m.inspector = m.inspector.SetDescription(runDesc(sel))
		}
	case ModeActivity:
		if d := m.activity.InspectorDesc(); d != nil {
			m.inspector = m.inspector.SetDescription(d)
		}
	case ModePlanStudio:
		if step := m.planStudio.SelectedStep(); step != nil {
			if job := m.planStudio.SelectedJob(); job != nil {
				m.inspector = m.inspector.SetDescription(
					planStepDesc(job, step, m.planStudio.StepCursor()))
			}
		} else if sel := m.planStudio.SelectedJob(); sel != nil {
			m.inspector = m.inspector.SetDescription(planJobDesc(sel))
		}
	}
}

func componentDesc(c *services.ComponentSummary, runs []services.RunSummary) *services.ResourceDescription {
	recent := recentRunsForComponent(c.Name, runs, 5)
	fields := []services.DescField{
		{Label: "type", Value: c.Type},
		{Label: "domain", Value: c.Domain},
		{Label: "path", Value: c.Path},
		{Label: "envs", Value: strings.Join(c.Envs, ",")},
		{Label: "profile", Value: c.Profile},
		{Label: "depends-on", Value: strings.Join(c.DependsOn, ",")},
		{Label: "watches", Value: strings.Join(c.Watches, ",")},
		{Label: "last run", Value: c.LastRunStatus},
	}
	if len(recent) > 0 {
		lines := make([]string, 0, len(recent))
		for _, r := range recent {
			id := r.ExecID
			if len(id) > 8 {
				id = id[:8]
			}
			lines = append(lines, fmt.Sprintf("%s %s", id, r.Status))
		}
		fields = append(fields, services.DescField{
			Label: "recent runs",
			Value: strings.Join(lines, "\n"),
		})
	}
	return &services.ResourceDescription{
		Kind:    "component",
		Name:    c.Name,
		Summary: c.Type + " · " + c.Domain,
		Fields:  fields,
	}
}

// recentRunsForComponent picks up to `limit` RunSummary entries that touched
// the given component. When RunSummary.Components is populated it does an
// exact membership check; otherwise (legacy runs without per-run component
// metadata) it falls back to a substring match on PlanName so the inspector
// still surfaces something useful.
func recentRunsForComponent(name string, runs []services.RunSummary, limit int) []services.RunSummary {
	if name == "" || limit <= 0 {
		return nil
	}
	out := make([]services.RunSummary, 0, limit)
	lname := strings.ToLower(name)
	for _, r := range runs {
		match := false
		if len(r.Components) > 0 {
			for _, c := range r.Components {
				if c == name {
					match = true
					break
				}
			}
		} else if r.PlanName != "" && strings.Contains(strings.ToLower(r.PlanName), lname) {
			match = true
		}
		if !match {
			continue
		}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func runDesc(r *services.RunSummary) *services.ResourceDescription {
	dr := "no"
	if r.DryRun {
		dr = "yes"
	}
	return &services.ResourceDescription{
		Kind:    "run",
		Name:    r.ExecID,
		Summary: r.PlanName + " · " + r.Status,
		Fields: []services.DescField{
			{Label: "status", Value: r.Status},
			{Label: "plan", Value: r.PlanName},
			{Label: "jobs done/failed",
				Value: fmt.Sprintf("%d/%d of %d", r.JobDone, r.JobFailed, r.JobTotal)},
			{Label: "duration", Value: r.Duration.String()},
			{Label: "trigger", Value: r.Trigger},
			{Label: "dry-run", Value: dr},
		},
	}
}

// planJobDesc renders the Plan Studio inspector pane for the job currently
// under the DAG cursor. It surfaces the static job metadata (component,
// env, deps, profile) plus a numbered list of the job's steps with their
// phase + a one-line preview of the `run` or `use` block — so selecting
// a DAG node immediately shows what work it will actually do.
// planStepDesc returns the inspector description for a single step drilled
// into from the steps list. Mirrors planJobDesc but step-scoped.
func planStepDesc(j *model.PlanJob, s *model.PlanStep, idx int) *services.ResourceDescription {
	if s == nil {
		return nil
	}
	name := s.Name
	if name == "" && s.ID != "" {
		name = s.ID
	}
	if name == "" {
		name = fmt.Sprintf("step-%d", idx+1)
	}
	fields := []services.DescField{
		{Label: "job", Value: j.ID},
		{Label: "order", Value: fmt.Sprintf("%d", idx+1)},
	}
	if s.Phase != "" {
		fields = append(fields, services.DescField{Label: "phase", Value: s.Phase})
	}
	if s.Use != "" {
		fields = append(fields, services.DescField{Label: "use", Value: s.Use})
	}
	if s.Shell != "" {
		fields = append(fields, services.DescField{Label: "shell", Value: s.Shell})
	}
	if s.WorkingDirectory != "" {
		fields = append(fields, services.DescField{Label: "workdir", Value: s.WorkingDirectory})
	}
	if s.Timeout != "" {
		fields = append(fields, services.DescField{Label: "timeout", Value: s.Timeout})
	}
	if s.Retry > 0 {
		fields = append(fields, services.DescField{Label: "retry", Value: fmt.Sprintf("%d", s.Retry)})
	}
	if s.OnFailure != "" {
		fields = append(fields, services.DescField{Label: "on-failure", Value: s.OnFailure})
	}
	if s.Run != "" {
		// Single-line preview; full body is visible in the step-detail
		// main pane to keep the inspector compact.
		fields = append(fields, services.DescField{Label: "run", Value: truncateOneLine(s.Run, 60)})
	}
	return &services.ResourceDescription{
		Kind:    "step",
		Name:    name,
		Summary: fmt.Sprintf("step %d of job %s", idx+1, j.ID),
		Fields:  fields,
	}
}

func planJobDesc(j *model.PlanJob) *services.ResourceDescription {
	if j == nil {
		return nil
	}
	summary := j.Component
	if j.Environment != "" {
		summary = j.Component + " · " + j.Environment
	}
	if summary == "" {
		summary = j.ID
	}
	fields := []services.DescField{
		{Label: "job id", Value: j.ID},
	}
	if j.Component != "" {
		fields = append(fields, services.DescField{Label: "component", Value: j.Component})
	}
	if j.Environment != "" {
		fields = append(fields, services.DescField{Label: "env", Value: j.Environment})
	}
	if j.Composition != "" {
		fields = append(fields, services.DescField{Label: "composition", Value: j.Composition})
	}
	if j.Profile != "" {
		fields = append(fields, services.DescField{Label: "profile", Value: j.Profile})
	}
	if j.RunsOn != "" {
		fields = append(fields, services.DescField{Label: "runs-on", Value: j.RunsOn})
	}
	if j.Path != "" {
		fields = append(fields, services.DescField{Label: "path", Value: j.Path})
	}
	if len(j.DependsOn) > 0 {
		fields = append(fields, services.DescField{
			Label: "depends-on",
			Value: strings.Join(j.DependsOn, ", "),
		})
	}
	if j.Timeout != "" {
		fields = append(fields, services.DescField{Label: "timeout", Value: j.Timeout})
	}
	if j.Retries > 0 {
		fields = append(fields, services.DescField{Label: "retries", Value: fmt.Sprintf("%d", j.Retries)})
	}
	if len(j.Steps) > 0 {
		fields = append(fields, services.DescField{
			Label: fmt.Sprintf("steps (%d)", len(j.Steps)),
			Value: planStepsBlock(j.Steps),
		})
	} else {
		fields = append(fields, services.DescField{
			Label: "steps",
			Value: "(none)",
		})
	}
	return &services.ResourceDescription{
		Kind:    "job",
		Name:    j.ID,
		Summary: summary,
		Fields:  fields,
	}
}

// planStepsBlock formats a job's steps as a compact, single-line-per-step
// list of step names. This is the inspector summary — full details
// (phase, run, use, with) live in the drilled-in StudioLevelStep view so
// the inspector never overflows for jobs with many or large steps.
//
//  1. build-image
//  2. push-image
//  3. deploy
func planStepsBlock(steps []model.PlanStep) string {
	var b strings.Builder
	for i, s := range steps {
		name := s.Name
		if name == "" && s.ID != "" {
			name = s.ID
		}
		if name == "" {
			name = fmt.Sprintf("step-%d", i+1)
		}
		// Hard cap each name so long inline IDs don't wrap the pane.
		fmt.Fprintf(&b, "%d. %s\n", i+1, truncateOneLine(name, 40))
	}
	return strings.TrimRight(b.String(), "\n")
}

// truncateOneLine collapses whitespace and trims the result to max runes.
func truncateOneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}

func (m *Model) applyResponsive() {
	if m.width < 100 {
		m.sidebarCollapsed = true
		m.showInspector = false
	}
}

func (m *Model) propagateSize() {
	// Pass sizing to the log explorer so its viewport scales.
	w, h := m.stageDimensions()
	m.logView = m.logView.SetSize(w, h)
	// Stage style adds Padding(0,1) which eats 2 horizontal cols of text
	// area — pass the inner content size to multi-pane children so they
	// don't overflow and force lipgloss to wrap every line.
	m.activity = m.activity.SetSize(w-2, h)
	m.planStudio = m.planStudio.SetSize(w-2, h)
	m.browse.Width = w
	m.browse.Height = h
	m.history.Width = w
	m.history.Height = h
	m.inspector.Width = m.inspectorWidth()
	m.inspector.Height = h
	m.search.Width = w - 4
}

func (m Model) stageDimensions() (int, int) {
	w := m.width - m.sidebarWidth() - m.inspectorWidth() - 4
	if w < 20 {
		w = 20
	}
	h := m.height - 5 - m.bottomPanelHeight()
	if h < 6 {
		h = 6
	}
	return w, h
}

// bottomPanelHeight returns the rendered height of the optional bottom
// info band (0 when hidden or not in a mode that supplies content).
func (m Model) bottomPanelHeight() int {
	if !m.showBottom {
		return 0
	}
	switch m.activeMode {
	case ModeActivity, ModePlanStudio:
		return 4
	}
	return 0
}

func (m Model) sidebarWidth() int {
	if m.width < 70 {
		return 0
	}
	if m.sidebarCollapsed {
		return 5
	}
	return 18
}

func (m Model) inspectorWidth() int {
	if !m.showInspector || m.width < 100 {
		return 0
	}
	return 38
}

func (m Model) applyPaletteCommand(c views.CommandPaletteCommand) (tea.Model, tea.Cmd) {
	m.commandPalette = m.commandPalette.Close()
	m.showCommandPalette = false
	switch c.ID {
	case "goto.browse":
		return m.switchMode(ModeBrowse), nil
	case "goto.plan":
		return m.switchMode(ModePlanStudio), nil
	case "goto.run", "goto.logs", "goto.history", "goto.activity":
		return m.switchMode(ModeActivity), nil
	case "plan.generate":
		m = m.switchMode(ModePlanStudio)
		m.planStudio = m.planStudio.MarkGenerating()
		return m, views.GeneratePlanCmd(m.svc, m.planStudio.Request)
	case "plan.save":
		if m.planStudio.Result != nil {
			req := m.planStudio.Request
			req.NamedPlan = "tui-draft"
			m.planStudio = m.planStudio.MarkGenerating()
			return m, views.GeneratePlanCmd(m.svc, req)
		}
		return m, nil
	case "plan.dryrun":
		if m.planStudio.Result != nil && m.planStudio.Result.Plan != nil {
			return m.Update(views.PlanStudioDryRunRequestedMsg{Plan: m.planStudio.Result.Plan})
		}
		return m, nil
	case "workspace.reload":
		m.loading = true
		return m, loadWorkspaceCmd(m.svc)
	case "ui.toggle.inspector":
		m.showInspector = !m.showInspector
		return m, nil
	case "ui.toggle.sidebar":
		m.sidebarCollapsed = !m.sidebarCollapsed
		return m, nil
	case "app.quit":
		return m, tea.Quit
	}
	return m, nil
}

// --- Rendering -------------------------------------------------------------

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	m.applyResponsive()
	m.propagateSize()

	header := m.renderHeader()
	rule := m.renderRule()

	stage := m.renderStage()
	stageW, stageH := m.stageDimensions()
	stageBox := theme.StyleStage.Width(stageW).Height(stageH).Render(stage)

	rowParts := []string{}
	if m.sidebarWidth() > 0 {
		nav := m.navigator
		nav.Collapsed = m.sidebarCollapsed
		navBox := theme.StyleSidebar.Width(m.sidebarWidth()).Height(stageH + 2).Render(nav.View())
		rowParts = append(rowParts, navBox)
	}
	rowParts = append(rowParts, stageBox)
	if m.inspectorWidth() > 0 {
		insBox := theme.StyleInspector.
			Width(m.inspectorWidth()).
			Height(stageH).
			Render(m.inspector.View())
		rowParts = append(rowParts, insBox)
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, rowParts...)

	hints := m.renderHints()
	status := m.renderStatus()

	parts := []string{header, rule, body}
	if bp := m.renderBottomPanel(); bp != "" {
		parts = append(parts, bp)
	}
	parts = append(parts, hints, status)
	frame := lipgloss.JoinVertical(lipgloss.Left, parts...)

	// Overlays — rendered on top of the frame via lipgloss.Place
	if m.showCommandPalette {
		overlay := m.commandPalette.View()
		return placeOver(frame, overlay, m.width, m.height)
	}
	if m.showHelp {
		return placeOver(frame, m.renderHelpModal(), m.width, m.height)
	}
	return frame
}

func placeOver(frame, overlay string, w, h int) string {
	centered := lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, overlay)
	_ = frame
	return centered
}

func (m Model) renderHeader() string {
	intent := "(no intent)"
	if m.workspace != nil && m.workspace.IntentName != "" {
		intent = m.workspace.IntentName
	}
	plan := "—"
	if m.planStudio.Result != nil {
		plan = m.planStudio.Result.Checksum
		if len(plan) > 10 {
			plan = plan[:10]
		}
	}
	env := "all"
	if m.planStudio.Request.Environment != "" {
		env = m.planStudio.Request.Environment
	}
	runStat := "idle"
	if m.runView.Done() {
		runStat = "done"
	} else if m.runView.Events != nil {
		runStat = "running"
	}

	crumbs := []string{
		theme.StyleHeaderAccent.Render("◆ orun"),
		theme.StyleDim.Render("│"),
		theme.StyleLabel.Render("intent ") + theme.StyleValue.Render(intent),
		theme.StyleDim.Render("·"),
		theme.StyleLabel.Render("env ") + theme.StyleValue.Render(env),
		theme.StyleDim.Render("·"),
		theme.StyleLabel.Render("plan ") + theme.StyleValue.Render(plan),
		theme.StyleDim.Render("·"),
		theme.StyleLabel.Render("run ") + theme.StyleValue.Render(runStat),
		theme.StyleDim.Render("·"),
		theme.StyleChipAccent.Render(m.activeMode.String()),
	}
	if m.activeMode == ModeActivity {
		bc := m.activity.Breadcrumb()
		if len(bc) > 1 {
			crumbs = append(crumbs,
				theme.StyleDim.Render("›"),
				theme.StyleValue.Render(strings.Join(bc[1:], " › ")),
			)
		}
	}
	return theme.StyleHeader.Render(strings.Join(crumbs, " "))
}

func (m Model) renderRule() string {
	if m.width <= 0 {
		return ""
	}
	return theme.StyleRule.Render(strings.Repeat("─", m.width))
}

func (m Model) renderStage() string {
	if m.loading {
		return centerLoading(m.spinner.View(), m.width, m.height)
	}
	var body string
	switch m.activeMode {
	case ModeBrowse:
		body = m.browse.View()
	case ModePlanStudio:
		body = m.planStudio.View()
	case ModeRunDashboard:
		body = m.runView.View()
	case ModeLogExplorer:
		body = m.logView.View()
	case ModeHistory:
		body = m.history.View()
	case ModeActivity:
		body = m.activity.View()
	}
	if m.searchActive {
		body = m.search.View() + "\n\n" + body
	}
	return body
}

func centerLoading(spin string, w, h int) string {
	if w <= 0 {
		w = 60
	}
	if h <= 0 {
		h = 10
	}
	card := theme.StyleModalCard.Render(
		theme.StyleAccent.Render(spin) + "  " +
			theme.StyleDim.Render("loading workspace…"))
	return lipgloss.Place(w-4, h-4, lipgloss.Center, lipgloss.Center, card)
}

func (m Model) renderHints() string {
	bindings := m.contextualKeys()
	parts := []string{}
	for _, b := range bindings {
		h := b.Help()
		parts = append(parts,
			theme.StyleKeyDim.Render(h.Key)+" "+theme.StyleKeyBold.Render(h.Desc))
	}
	sep := theme.StyleKeySep.Render(" · ")
	line := strings.Join(parts, sep)
	if lipgloss.Width(line) > m.width-2 {
		line = lipgloss.NewStyle().MaxWidth(m.width - 2).Render(line)
	}
	return theme.StyleHints.Render(line)
}

func (m Model) contextualKeys() []key.Binding {
	out := []key.Binding{}
	switch m.activeMode {
	case ModePlanStudio:
		k := m.planStudio.KeyMap()
		out = append(out, k.Generate, k.DryRun, k.Save, k.Clear)
	case ModeActivity:
		out = append(out,
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "drill in")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "list ⇄ graph")),
			m.keys.ToggleBottom,
		)
	case ModeBrowse, ModeHistory:
		out = append(out, m.keys.Search)
	}
	out = append(out, m.keys.Palette, m.keys.ToggleInspector, m.keys.ToggleSidebar, m.keys.Help, m.keys.Quit)
	return out
}

func (m Model) renderStatus() string {
	left := ""
	if m.loading {
		left = m.spinner.View() + " working…"
	} else if m.lastErr != nil {
		left = theme.StylePillError.Render("error: " + m.lastErr.Error())
	} else if m.toast != "" && time.Since(m.toastAt) < 10*time.Second {
		left = theme.StyleToast.Render(m.toast)
	} else {
		left = theme.StyleDim.Render("ready")
	}
	right := theme.StyleDim.Render(fmt.Sprintf("%dx%d", m.width, m.height))
	if m.activeMode == ModeActivity {
		focus := m.activity.FocusLabel()
		if focus != "" {
			right = theme.StyleChipAccent.Render("focus · "+focus) + "  " + right
		}
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return theme.StyleStatusLine.Render(left + strings.Repeat(" ", gap) + right)
}

// renderBottomPanel renders the optional info band that spans main +
// inspector. Only active in Activity mode when the user has toggled it
// on with `b`. Per-level content is sourced from ActivityModel.
func (m Model) renderBottomPanel() string {
	if m.bottomPanelHeight() == 0 {
		return ""
	}
	innerW := m.width - m.sidebarWidth() - 2
	if innerW < 20 {
		innerW = 20
	}
	var body string
	switch m.activeMode {
	case ModeActivity:
		body = m.activity.BottomPanelContent(innerW)
	case ModePlanStudio:
		body = m.planStudio.BottomPanelContent(innerW)
	}
	if body == "" {
		body = theme.StyleDim.Render("(no overview)")
	}
	// Left-pad to the sidebar's width so the band visually starts at
	// the main pane and runs through the inspector.
	pad := strings.Repeat(" ", m.sidebarWidth())
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return theme.StyleBottomPanel.Render(strings.Join(lines, "\n"))
}

func (m Model) renderHelpModal() string {
	groups := [][]string{
		{"Global", "tab components ⇄ activity", "i toggle inspector",
			"⌃b toggle sidebar", "⌃r reload", ": commands", "/ search", "? help", "q quit"},
		{"Navigation", "1 components", "2 activity", "esc back", "⌃o back", "⌃i forward"},
		{"Components", "enter compose · component", "/ filter", "i inspector"},
		{"Compose", "g generate", "d dry-run", "R real run", "s save", "e/t/C cycle"},
		{"Activity", "tab cycle pane", "↑/↓ move", "enter tail logs", "r runs · l logs"},
	}
	var b strings.Builder
	b.WriteString(theme.StyleModalTitle.Render("Cockpit · Help"))
	b.WriteString("\n\n")
	for _, g := range groups {
		b.WriteString(theme.StyleSectionTitle.Render(g[0]))
		b.WriteString("\n")
		for _, item := range g[1:] {
			b.WriteString("  " + theme.StyleDim.Render(item) + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(theme.StyleDim.Render("esc to close"))
	cardW := clamp(m.width*60/100, 40, 90)
	return theme.StyleModalCard.Width(cardW).Render(b.String())
}

// refreshActivityRuns rebuilds the Activity pane's run list from the
// current history slice plus, if a live run is in flight, a synthesized
// in-flight ActivityRun.
func (m *Model) refreshActivityRuns() {
	var live *views.ActivityRun
	statuses := map[string]string{}
	for _, row := range m.runView.Rows() {
		statuses[row.JobID] = row.Status
	}
	if m.livePlan != nil && (!m.runView.Done() || len(statuses) > 0) {
		planName := ""
		if m.planStudio.Result != nil {
			planName = m.planStudio.Result.Checksum
		}
		if planName == "" {
			planName = "(live run)"
		}
		comps := []string{}
		if len(m.planStudio.Request.Components) > 0 {
			comps = append(comps, m.planStudio.Request.Components...)
		}
		live = &views.ActivityRun{
			ExecID:     m.liveExecID,
			PlanName:   planName,
			Status:     "running",
			Live:       !m.runView.Done(),
			Plan:       m.livePlan,
			Statuses:   statuses,
			Components: comps,
			StartedAt:  "now",
			Duration:   "—",
		}
		if m.runView.Done() {
			live.Status = "completed"
			for _, s := range statuses {
				if s == "failed" {
					live.Status = "failed"
					break
				}
			}
		}
	}
	m.activity = m.activity.SetRuns(live, m.history.Runs)
}

// --- Helpers for tests / legacy --------------------------------------------

// PendingRun stashes the metadata needed to render the confirm modal and
// to dispatch the actual non-dry RunPlan once the user presses `y`.
type PendingRun struct {
	Plan     *model.Plan
	Env      string
	Checksum string
	Jobs     int
}

// dispatchPendingRun performs the real (non-dry) RunPlan using the stashed
// plan, mirrors the dry-run kickoff flow (StartStream + spinner tick), and
// clears the confirm modal state. Called from handleKey on `y`.
func (m Model) dispatchPendingRun() (tea.Model, tea.Cmd) {
	if m.pendingRun == nil || m.pendingRun.Plan == nil {
		m.showConfirm = false
		m.pendingRun = nil
		return m, nil
	}
	ch, err := m.svc.RunPlan(context.Background(), services.RunRequest{
		Plan:   m.pendingRun.Plan,
		DryRun: false,
	})
	if err != nil {
		m.lastErr = err
		m.showConfirm = false
		m.pendingRun = nil
		return m, nil
	}
	m.lastErr = nil
	m.runStarting = true
	m.livePlan = m.pendingRun.Plan
	m.liveExecID = ""
	var cmd tea.Cmd
	m.runView, cmd = m.runView.StartStream(ch, false)
	m.refreshActivityRuns()
	m = m.switchMode(ModeActivity)
	m.showConfirm = false
	m.pendingRun = nil
	return m, tea.Batch(cmd, m.spinner.Tick)
}

// stopFollowMsg is dispatched a short interval after a run completes so the
// follow-mode log tail drains its final step and then shuts down.
type stopFollowMsg struct{}

// newLogContext returns a context for a log tail. For a follow tail it
// cancels any previous follow and stores the new cancel func so the tail's
// lifetime is bounded (cancelled on the next tail, on run completion, or at
// program exit). A non-follow (one-shot) tail self-terminates when the log
// files are exhausted, so it needs no stored cancel.
func (m *Model) newLogContext(follow bool) context.Context {
	if m.logCancel != nil {
		m.logCancel()
		m.logCancel = nil
	}
	if !follow {
		return context.Background()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.logCancel = cancel
	return ctx
}

// ToastTickMsg drives the 1-second tick that auto-dismisses toasts after
// 10s without requiring an external event.
type ToastTickMsg struct{}

func toastTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return ToastTickMsg{} })
}

// refreshInterval is the cockpit's live-view poll cadence (design.md §3.4
// trigger 2 / cli-surface.md §1, D-9). The catalog ticker re-runs the workspace
// load every interval so local edits and other processes' writes (an external
// `orun plan`/`run`, the universal refresh hook) appear without a keystroke.
const refreshInterval = 3 * time.Second

// catalogRefreshTickMsg fires on the live-view interval.
type catalogRefreshTickMsg struct{}

func catalogRefreshTickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return catalogRefreshTickMsg{} })
}

// workspaceRefreshedMsg carries the result of a silent live-view reload (the
// interval ticker), distinct from WorkspaceLoadedMsg so it never toggles the
// loading spinner and is applied best-effort: a failed background refresh keeps
// the current snapshot rather than surfacing a blocking error.
type workspaceRefreshedMsg struct {
	Snapshot *services.WorkspaceSnapshot
	Err      error
}

// backgroundReloadCmd reloads the workspace silently for the live ticker.
func backgroundReloadCmd(svc services.OrunService) tea.Cmd {
	return func() tea.Msg {
		snap, err := svc.LoadWorkspace(context.Background(), services.WorkspaceRequest{})
		return workspaceRefreshedMsg{Snapshot: snap, Err: err}
	}
}

// setToast records a toast message + timestamp and returns a tick cmd that
// will repaint and clear the toast roughly 10s later. Safe to call from
// any Update branch — re-arms the tick on every fresh toast.
func (m *Model) setToast(text string) tea.Cmd {
	m.toast = text
	m.toastAt = time.Now()
	return toastTickCmd()
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func nextPanel(p Panel) Panel { return Panel((int(p) + 1) % 3) }
func prevPanel(p Panel) Panel { return Panel((int(p) + 2) % 3) }
