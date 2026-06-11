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
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/theme"
	"github.com/sourceplane/orun/internal/tui/views"
)

// Mode identifies which primary view occupies the stage.
type Mode int

const (
	// ModeCatalog is the cockpit's home surface: the multi-kind entity
	// explorer that absorbed the former Browse/Component surfaces — its
	// Component entities carry the full work surface (change overlay, run,
	// compose, execution history).
	ModeCatalog Mode = iota
	ModePlanStudio
	ModeRunDashboard
	ModeLogExplorer
	ModeHistory
	ModeActivity
)

// ModeComponentStudio is an alias for the inline Compose surface that
// is reached via `g` on a component in the Catalog. The underlying
// PlanStudioModel is reused as the implementation engine.
const ModeComponentStudio = ModePlanStudio

func (m Mode) String() string {
	switch m {
	case ModeCatalog:
		return "catalog"
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
	case ModeCatalog, ModePlanStudio:
		return "catalog"
	}
	return "activity"
}

// searchable reports whether the mode's stage supports the `/` filter bar.
func (m Mode) searchable() bool {
	switch m {
	case ModeCatalog, ModeHistory, ModeLogExplorer:
		return true
	}
	return false
}

// autoInspector reports whether entering the mode opens the inspector on wide
// terminals so the selection's detail is immediately visible. A per-mode
// affordance, not a sticky preference — it is never persisted to prefs.
func (m Mode) autoInspector() bool {
	switch m {
	case ModeActivity, ModePlanStudio, ModeCatalog:
		return true
	}
	return false
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
	history    views.HistoryModel
	planStudio views.PlanStudioModel
	runView    views.RunViewModel
	logView    views.LogExplorerModel
	activity   views.ActivityModel
	catalog    views.CatalogModel
	inspector  views.InspectorModel

	// selectedEnv is the cockpit's currently selected environment
	// (environments.md §1) — used for the component-scoped run action and shown
	// in the header. Cycled with `e`; defaulted to the first env or the
	// last-used value from prefs. Empty when the workspace declares no envs.
	selectedEnv string

	// runAfterGenerate is set when a component-scoped run was requested: the
	// next successful plan generation pops the confirm modal instead of just
	// reporting "plan ready".
	runAfterGenerate bool

	// Plan cached on dispatch so Activity can render a DAG for the
	// currently in-flight run. Nil between runs.
	livePlan   *model.Plan
	liveExecID string

	// runDetails caches lazily-loaded plan + statuses per historical execution
	// (keyed by exec id) so the Activity drilldown can enumerate steps/logs.
	// Re-applied after every run-list rebuild so a background live run's refresh
	// doesn't wipe a historical run the user is currently inspecting.
	runDetails map[string]services.RunDetail

	// Component Studio context — set when the user presses `g` on a
	// component in the Catalog; cleared when they `esc` back out.
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
	// catalogStale is true when the object-model catalog was resolved against a
	// different tree than the workspace currently has — surfaced as a header
	// "⟳ stale" chip prompting ⌃r (Bucket 6C). Updated on the live-view ticker.
	catalogStale bool

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
		activeMode:       ModeCatalog,
		activePanel:      PanelMain,
		navigator:        views.NewNavigatorModel(),
		history:          views.NewHistoryModel(),
		planStudio:       views.NewPlanStudioModel(),
		runView:          views.NewRunViewModel(),
		logView:          views.NewLogExplorerModel(),
		activity:         views.NewActivityModel(),
		catalog:          views.NewCatalogModel(),
		inspector:        views.NewInspectorModel(),
		commandPalette:   views.NewCommandPaletteModel(),
		loading:          true,
		spinner:          sp,
		search:           ti,
		keys:             DefaultGlobalKeyMap(),
		prefs:            prefs,
		selectedEnv:      prefs.SelectedEnv,
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
	// Refresh the object-model catalog on open (force — even a dirty tree) so
	// the cockpit opens on a current catalog, then reloads when it lands.
	return tea.Batch(loadWorkspaceCmd(m.svc), m.spinner.Tick, catalogRefreshTickCmd(), refreshCatalogCmd(m.svc, true), checkCatalogStaleCmd(m.svc), loadCatalogCmd(m.svc))
}

// loadCatalogCmd reads the multi-kind entity catalog for the Catalog surface.
func loadCatalogCmd(svc services.OrunService) tea.Cmd {
	return func() tea.Msg {
		snap, err := svc.LoadCatalog(context.Background())
		return services.CatalogLoadedMsg{Snapshot: snap, Err: err}
	}
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

func loadRunDetailCmd(svc services.OrunService, execID string) tea.Cmd {
	return func() tea.Msg {
		detail, err := svc.GetRunDetail(context.Background(), services.RunDetailRequest{ExecID: execID})
		return services.RunDetailLoadedMsg{
			ExecID:   execID,
			Plan:     detail.Plan,
			Statuses: detail.Statuses,
			Steps:    detail.Steps,
			Err:      err,
		}
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
		m.ensureSelectedEnv()
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
		m.syncCatalogContext()
		m.refreshInspectorSelection()
		return m, listRunsCmd(m.svc)

	case catalogRefreshTickMsg:
		// Live view (D-9): silently reload the workspace and re-arm the tick.
		// The reload runs off the UI thread; the result lands as
		// workspaceRefreshedMsg. When auto-refresh is enabled, also re-resolve
		// the catalog (staleness-gated, so a clean unchanged tree is a no-op).
		cmds := []tea.Cmd{backgroundReloadCmd(m.svc), catalogRefreshTickCmd(), checkCatalogStaleCmd(m.svc)}
		if m.prefs.AutoRefresh {
			cmds = append(cmds, refreshCatalogCmd(m.svc, false))
		}
		return m, tea.Batch(cmds...)

	case catalogStaleMsg:
		m.catalogStale = msg.stale
		return m, nil

	case catalogRefreshedMsg:
		// A refresh just ran → the catalog is current; clear the stale badge.
		// When it changed, re-read the workspace + entity catalog so the new
		// member set surfaces. Fresh/skipped/failed only clears the badge.
		m.catalogStale = false
		if msg.refreshed {
			return m, tea.Batch(backgroundReloadCmd(m.svc), loadCatalogCmd(m.svc))
		}
		return m, nil

	case services.CatalogLoadedMsg:
		// Best-effort: a failed read keeps the current snapshot (mirrors
		// workspaceRefreshedMsg) so a transient store error never blanks the
		// Catalog surface.
		if msg.Err != nil {
			return m, nil
		}
		m.catalog = m.catalog.SetSnapshot(msg.Snapshot)
		if m.activeMode == ModeCatalog {
			m.refreshInspectorSelection()
		}
		return m, nil

	case liveRefreshTickMsg:
		// While a run is in flight, re-read its per-job/step state so the
		// drilldown's status + duration columns advance between job events. The
		// tick self-stops once the run is no longer live.
		if m.liveExecID == "" || m.runView.Done() {
			return m, nil
		}
		return m, tea.Batch(loadRunDetailCmd(m.svc, m.liveExecID), liveRefreshTickCmd())

	case workspaceRefreshedMsg:
		// Best-effort: a failed background refresh keeps the current snapshot
		// (the live view never replaces good data with a transient error).
		if msg.Err != nil || msg.Snapshot == nil {
			return m, nil
		}
		m.workspace = msg.Snapshot
		m.ensureSelectedEnv()
		m.syncCatalogContext()
		m.refreshInspectorSelection()
		return m, nil

	case services.RunsListedMsg:
		if msg.Err == nil {
			m.history.Runs = msg.Runs
			m.refreshActivityRuns()
			m.syncCatalogContext()
		}
		return m, nil

	case services.PlanGeneratedMsg:
		m.planStudio, _ = m.planStudio.Update(msg)
		if msg.Err != nil {
			m.runAfterGenerate = false
			m.lastErr = msg.Err
			return m, m.setToast("plan error: " + msg.Err.Error())
		}
		m.lastErr = nil
		// Component-scoped run: a real run was requested, so confirm + dispatch
		// instead of just reporting the compiled plan.
		if m.runAfterGenerate {
			m.runAfterGenerate = false
			if msg.Result != nil && msg.Result.Plan != nil {
				m.pendingRun = &PendingRun{
					Plan:     msg.Result.Plan,
					Env:      m.selectedEnv,
					Checksum: msg.Result.Checksum,
					Jobs:     msg.Result.JobCount,
				}
				m.showConfirm = true
				return m, nil
			}
		}
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

	case views.ComponentJobOpenMsg:
		// Hand off to the Activity run→job→logs drilldown for this execution.
		m.refreshActivityRuns()
		act, ok := m.activity.FocusRun(msg.ExecID)
		if !ok {
			return m, m.setToast("run not found in history yet")
		}
		m.activity = act
		m = m.switchMode(ModeActivity)
		// FocusRun jumps straight to LevelRun without going through drillIn, so
		// kick off the historical-detail load here too.
		return m, m.activity.LoadDetailCmd()

	case views.ComponentRunRequestedMsg:
		// Component-scoped run for the selected env (environments.md §1). Only
		// offered for a component active in the env; generation is async, so we
		// flag runAfterGenerate and pop the confirm modal on PlanGeneratedMsg.
		comp := m.componentByName(msg.Name)
		if m.selectedEnv == "" {
			return m, m.setToast("no environment selected — press e to pick one")
		}
		if comp == nil || !envContains(comp.Envs, m.selectedEnv) {
			return m, m.setToast(fmt.Sprintf("%s is not active in %s", msg.Name, m.selectedEnv))
		}
		req := services.PlanRequest{
			Components:  []string{msg.Name},
			Environment: m.selectedEnv,
		}
		if m.workspace != nil {
			req.IntentFile = m.workspace.IntentFile
		}
		m.runAfterGenerate = true
		return m, tea.Batch(
			views.GeneratePlanCmd(m.svc, req),
			m.setToast(fmt.Sprintf("compiling %s · %s…", msg.Name, m.selectedEnv)),
		)

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

	case views.ActivityLoadRunDetailMsg:
		// A historical run was opened; load its plan + statuses off-thread so
		// the drilldown can enumerate steps and tail logs.
		if msg.ExecID == "" {
			return m, nil
		}
		return m, loadRunDetailCmd(m.svc, msg.ExecID)

	case services.RunDetailLoadedMsg:
		if msg.Err != nil {
			// Best-effort: leave the run plan-less; the drilldown shows its
			// "step list unavailable" hint rather than surfacing an error.
			return m, nil
		}
		if m.runDetails == nil {
			m.runDetails = map[string]services.RunDetail{}
		}
		m.runDetails[msg.ExecID] = services.RunDetail{
			ExecID:   msg.ExecID,
			Plan:     msg.Plan,
			Statuses: msg.Statuses,
			Steps:    msg.Steps,
		}
		m.activity = m.activity.SetRunDetail(msg.ExecID, msg.Plan, msg.Statuses, msg.Steps)
		m.refreshInspectorSelection()
		return m, nil

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
		// Refresh the live run's plan + per-job/step records from the working
		// tree as jobs transition, so the drilldown's step status/duration
		// columns track progress (the run only emits job-level events, so this
		// updates at job boundaries).
		if execID := msg.Event.ExecID; execID != "" {
			cmds = append(cmds, loadRunDetailCmd(m.svc, execID))
		}
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

	case services.LogBatchMsg:
		// Route to whichever surface is consuming logs. Each surface compares
		// the batch's stream id against its attached tail, so a batch only ever
		// lands in the view that owns the stream.
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
		// ⌃r re-resolves the catalog (force) and reloads the view.
		m.loading = true
		m.lastErr = nil
		return m, tea.Batch(loadWorkspaceCmd(m.svc), refreshCatalogCmd(m.svc, true), loadCatalogCmd(m.svc))
	case key.Matches(msg, m.keys.ToggleMode):
		// Tab toggles the two top-level surfaces: Catalog ⇄ Activity.
		if m.activeMode == ModeActivity {
			return m.switchMode(ModeCatalog), loadCatalogCmd(m.svc)
		}
		mm := m.switchMode(ModeActivity)
		_, cmd := mm.activity.AutoAttachCmd()
		return mm, cmd
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
		if m.activeMode.searchable() {
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
		// Back (⌫ / ⌃o) pops the active mode's drilldown level first, then
		// falls back to global mode-back history.
		if mm, cmd, ok := m.popDrill(); ok {
			return mm, cmd
		}
		return m.goBack(), nil
	case key.Matches(msg, m.keys.Forward):
		return m.goForward(), nil
	case key.Matches(msg, m.keys.Cancel):
		// Esc when no overlay is open: pop the active mode's drilldown level
		// first; otherwise act as "back".
		if mm, cmd, ok := m.popDrill(); ok {
			return mm, cmd
		}
		if len(m.navBack) > 0 {
			return m.goBack(), nil
		}
	case key.Matches(msg, m.keys.GoCatalog):
		return m.switchMode(ModeCatalog), loadCatalogCmd(m.svc)
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
	case msg.String() == "e" && m.activeMode == ModeCatalog:
		// Cycle the selected environment (environments.md §1). Scoped to the
		// catalog surface; Plan Studio keeps its own `e` via forwardKey.
		m.cycleEnv()
		return m, nil
	}

	// Forward to active view.
	return m.forwardKey(msg)
}

func (m Model) forwardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.activeMode {
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
		beforeLevel := m.activity.Level
		var c tea.Cmd
		m.activity, c = m.activity.Update(msg)
		m.refreshInspectorSelection()
		// Drilling between levels swaps the entire stage body; force a full
		// repaint on the transition so bubbletea's line-diff can't leave an
		// orphaned row from the previous level on screen.
		if m.activity.Level != beforeLevel {
			return m, tea.Batch(c, tea.ClearScreen)
		}
		return m, c
	case ModeCatalog:
		wasRoot := m.catalog.AtRoot()
		var c tea.Cmd
		m.catalog, c = m.catalog.Update(msg)
		m.refreshInspectorSelection()
		// Drilling between list and detail swaps the whole stage body; force a
		// repaint so the line-diff renderer can't leave residue (same rationale
		// as the Activity level transitions).
		if m.catalog.AtRoot() != wasRoot {
			return m, tea.Batch(c, tea.ClearScreen)
		}
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
	if target.autoInspector() && m.width >= 100 {
		m.showInspector = true
	}
	m.refreshInspectorSelection()
	return m
}

// popDrill pops one drilldown level of the active mode's stage, if it has a
// drill stack and is below its root. It forwards a synthetic esc so ⌫/⌃o and
// esc behave identically (the drilldown views pop on esc). ok=false means the
// mode is at its root (or has no drill stack) and the caller should fall back
// to global mode-back history. This is the single place a mode's drill stack
// participates in back/esc dispatch — a new drillable mode adds one case here.
func (m Model) popDrill() (Model, tea.Cmd, bool) {
	esc := tea.KeyMsg{Type: tea.KeyEsc}
	switch m.activeMode {
	case ModeActivity:
		if !m.activity.AtRoot() {
			var cmd tea.Cmd
			m.activity, cmd = m.activity.Update(esc)
			m.refreshInspectorSelection()
			return m, cmd, true
		}
	case ModePlanStudio:
		if !m.planStudio.AtRoot() {
			var cmd tea.Cmd
			m.planStudio, cmd = m.planStudio.Update(esc)
			m.refreshInspectorSelection()
			return m, cmd, true
		}
	case ModeCatalog:
		if !m.catalog.AtRoot() {
			m.catalog, _ = m.catalog.Update(esc)
			m.refreshInspectorSelection()
			return m, nil, true
		}
	}
	return m, nil, false
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
	case ModeHistory:
		m.history = m.history.SetFilter(q)
	case ModeCatalog:
		m.catalog = m.catalog.SetFilter(q)
	}
}

// ensureSelectedEnv settles the selected environment after a workspace load:
// it keeps a still-valid current/pref value, otherwise falls back to the first
// (sorted) environment. A workspace with no environments leaves it empty.
func (m *Model) ensureSelectedEnv() {
	if m.workspace == nil {
		return
	}
	envs := append([]string(nil), m.workspace.Environments...)
	sort.Strings(envs)
	if envContains(envs, m.selectedEnv) {
		return
	}
	if envContains(envs, m.prefs.SelectedEnv) {
		m.selectedEnv = m.prefs.SelectedEnv
		return
	}
	if len(envs) > 0 {
		m.selectedEnv = envs[0]
	} else {
		m.selectedEnv = ""
	}
}

// cycleEnv advances the selected environment to the next one (sorted, wrapping)
// and persists the choice. No-op when the workspace declares fewer than two.
func (m *Model) cycleEnv() {
	if m.workspace == nil {
		return
	}
	envs := append([]string(nil), m.workspace.Environments...)
	sort.Strings(envs)
	if len(envs) < 2 {
		return
	}
	idx := 0
	for i, e := range envs {
		if e == m.selectedEnv {
			idx = i
			break
		}
	}
	m.selectedEnv = envs[(idx+1)%len(envs)]
	m.prefs.SelectedEnv = m.selectedEnv
	SavePrefs(m.prefs)
}

// componentByName returns the workspace component summary with the given name,
// or nil. Used to validate component-scoped run requests.
func (m *Model) componentByName(name string) *services.ComponentSummary {
	if m.workspace == nil {
		return nil
	}
	for i := range m.workspace.Components {
		if m.workspace.Components[i].Name == name {
			c := m.workspace.Components[i]
			return &c
		}
	}
	return nil
}

// syncCatalogContext refreshes the Catalog surface's Component work-surface
// context (change overlay, last-run status, recent executions) from the
// latest workspace + run history, so catalog component rows stay live without
// re-reading the object store.
func (m *Model) syncCatalogContext() {
	if m.workspace == nil {
		return
	}
	runsBy := make(map[string][]services.RunSummary, len(m.workspace.Components))
	for _, c := range m.workspace.Components {
		if recent := recentRunsForComponent(c.Name, m.history.Runs, 10); len(recent) > 0 {
			runsBy[c.Name] = recent
		}
	}
	m.catalog = m.catalog.SetComponentContext(m.workspace.Components, runsBy)
}

// envContains reports whether envs holds the (non-empty) name.
func envContains(envs []string, name string) bool {
	if name == "" {
		return false
	}
	for _, e := range envs {
		if e == name {
			return true
		}
	}
	return false
}

func (m *Model) refreshInspectorSelection() {
	switch m.activeMode {
	case ModeHistory:
		if sel := m.history.Selected(); sel != nil {
			m.inspector = m.inspector.SetDescription(runDesc(sel))
		}
	case ModeActivity:
		if d := m.activity.InspectorDesc(); d != nil {
			m.inspector = m.inspector.SetDescription(d)
		}
	case ModeCatalog:
		if d := m.catalog.InspectorDesc(); d != nil {
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

// truncateOneLine collapses whitespace and trims the result to max cells
// (rune-aware — a byte slice could split a multi-byte rune).
func truncateOneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && lipgloss.Width(s) > max {
		return ansi.Truncate(s, max, "…")
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
	w, h := m.stageDimensions()
	// Every stage-hosted view renders inside StyleStage, whose RoundedBorder (2
	// cols) + Padding(0,1) (2 cols) shrink the usable content area to w-2 (lipgloss
	// Width(n) = content+padding, border outside). Passing the full w makes
	// lipgloss soft-wrap every over-long line, which inflates the box past its
	// height and breaks the frame — so hand all children the content width.
	cw := w - 2
	m.logView = m.logView.SetSize(cw, h)
	m.activity = m.activity.SetSize(cw, h)
	m.planStudio = m.planStudio.SetSize(cw, h)
	m.catalog = m.catalog.SetSize(cw, h)
	m.history.Width = cw
	m.history.Height = h
	// The inspector box (DoubleBorder + Padding) likewise reserves 2 cols.
	m.inspector.Width = m.inspectorWidth() - 2
	m.inspector.Height = h
	m.search.Width = w - 4
}

func (m Model) stageDimensions() (int, int) {
	w := m.width - m.sidebarWidth() - m.inspectorWidth() - 4
	if w < 20 {
		w = 20
	}
	// Size the stage from the MEASURED chrome (header/rule/hints/status/bottom
	// panel), not a fixed guess. The breadcrumb header and a transient toast can
	// change the chrome's line count mid-run; if the stage height didn't track
	// that, the frame would oscillate between renders and the alt-screen would
	// accumulate residue (ghosted rows, duplicated footers). +2 is the stage
	// box's top/bottom border, so chrome + box = exactly m.height.
	h := m.height - m.chromeHeight() - 2
	if h < 6 {
		h = 6
	}
	return w, h
}

// chromeHeight is the rendered line count of everything around the stage box.
func (m Model) chromeHeight() int {
	h := lipgloss.Height(m.renderHeader()) +
		lipgloss.Height(m.renderRule()) +
		lipgloss.Height(m.renderHints()) +
		lipgloss.Height(m.renderStatus())
	if bp := m.renderBottomPanel(); bp != "" {
		h += lipgloss.Height(bp)
	}
	return h
}

// fitToScreen forces s to be exactly m.height rows, each at most m.width columns
// (ANSI-aware). This is the final guarantee against alt-screen residue: every
// frame the renderer sees has identical dimensions, so rows are always
// overwritten cleanly regardless of any upstream sizing drift.
func (m Model) fitToScreen(s string) string {
	if m.height <= 0 {
		return s
	}
	// Height only, via plain string ops. Do NOT re-render through a lipgloss
	// style here: reprocessing the whole composed frame can rewrite ANSI runs in
	// ways that desync bubbletea's line-diff renderer and leave ghosted rows. The
	// per-box clipBox already bounds line widths.
	lines := strings.Split(s, "\n")
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	// Clip any over-wide line to the terminal width, per line (the chrome — the
	// breadcrumb especially — can outgrow the width at deep drill-downs, and a
	// line that wraps in the terminal desyncs the renderer). Only touch lines
	// that actually exceed the width so styled content is left untouched.
	if m.width > 0 {
		for i, ln := range lines {
			if lipgloss.Width(ln) > m.width {
				lines[i] = lipgloss.NewStyle().MaxWidth(m.width).Render(ln)
			}
		}
	}
	return strings.Join(lines, "\n")
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
	case "goto.plan":
		return m.switchMode(ModePlanStudio), nil
	case "goto.run", "goto.logs", "goto.history", "goto.activity":
		return m.switchMode(ModeActivity), nil
	// goto.browse is the retired Components surface — tolerated as an alias so
	// stale palette muscle memory still lands somewhere sensible.
	case "goto.catalog", "goto.browse":
		return m.switchMode(ModeCatalog), loadCatalogCmd(m.svc)
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
	case "catalog.refresh":
		m.loading = true
		return m, tea.Batch(loadWorkspaceCmd(m.svc), refreshCatalogCmd(m.svc, true))
	case "catalog.autorefresh":
		m.prefs.AutoRefresh = !m.prefs.AutoRefresh
		SavePrefs(m.prefs)
		state := "off"
		if m.prefs.AutoRefresh {
			state = "on"
		}
		cmd := m.setToast("Catalog auto-refresh " + state)
		return m, cmd
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
	// Clip each box's content to the stage height before styling. lipgloss
	// Height() pads short content up but never truncates tall content, so an
	// over-long inner view (notably the inspector) would otherwise grow its box
	// past stageH, making the whole frame taller than the terminal — which
	// scrolls and leaves residue (duplicated footers/status lines). All three
	// boxes share the same content height so the row is exactly stageH+border.
	stageBox := theme.StyleStage.Width(stageW).Height(stageH).Render(clipBox(stage, stageW-2, stageH))

	rowParts := []string{}
	if m.sidebarWidth() > 0 {
		nav := m.navigator
		nav.Collapsed = m.sidebarCollapsed
		// Sidebar has Padding(1,1) and no border, so Height(stageH+2) → total
		// stageH+2, matching the bordered stage/inspector boxes.
		navBox := theme.StyleSidebar.Width(m.sidebarWidth()).Height(stageH + 2).Render(clipBox(nav.View(), m.sidebarWidth()-2, stageH))
		rowParts = append(rowParts, navBox)
	}
	rowParts = append(rowParts, stageBox)
	if m.inspectorWidth() > 0 {
		insBox := theme.StyleInspector.
			Width(m.inspectorWidth()).
			Height(stageH).
			Render(clipBox(m.inspector.View(), m.inspectorWidth()-2, stageH))
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

	// Overlays — rendered on top of the frame via lipgloss.Place. Precedence
	// mirrors handleKey: the run-confirm modal grabs input first, so it must
	// also paint first (otherwise it blocks every key while staying invisible,
	// which reads as a frozen cockpit after pressing r).
	if m.showConfirm {
		return m.fitToScreen(placeOver(frame, m.renderConfirmModal(), m.width, m.height))
	}
	if m.showCommandPalette {
		overlay := m.commandPalette.View()
		return m.fitToScreen(placeOver(frame, overlay, m.width, m.height))
	}
	if m.showHelp {
		return m.fitToScreen(placeOver(frame, m.renderHelpModal(), m.width, m.height))
	}
	return m.fitToScreen(frame)
}

// clipLines truncates s to at most n lines, preventing an over-long inner view
// from growing its box past the allotted height (lipgloss Height only pads up).
func clipLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n")
}

// clipBox clamps content to a box's inner area: at most h lines, each at most w
// visible columns. lipgloss MaxWidth is ANSI-aware (it won't split escapes) and
// applies per line. This guarantees the surrounding styled box can neither
// soft-wrap an over-long line nor pad past its height — the two ways the frame
// grew taller than the terminal and corrupted on redraw.
func clipBox(s string, w, h int) string {
	if w < 1 {
		w = 1
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(clipLines(s, h))
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
	if m.selectedEnv != "" {
		env = m.selectedEnv
	} else if m.planStudio.Request.Environment != "" {
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
	if m.activeMode == ModeCatalog {
		if bc := m.catalog.Breadcrumb(); len(bc) > 0 {
			crumbs = append(crumbs,
				theme.StyleDim.Render("›"),
				theme.StyleValue.Render(strings.Join(bc, " › ")),
			)
		}
	}
	if m.catalogStale {
		// The catalog was resolved against a different tree — prompt a refresh.
		crumbs = append(crumbs,
			theme.StyleDim.Render("·"),
			theme.StylePillWarn.Render("⟳ stale (⌃r)"),
		)
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
	case ModeCatalog:
		body = m.catalog.View()
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
	case ModeHistory:
		out = append(out, m.keys.Search)
	case ModeCatalog:
		out = append(out,
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "open")),
			key.NewBinding(key.WithKeys("[", "]"), key.WithHelp("[ ]", "kind")),
		)
		if sel := m.catalog.Selected(); sel != nil && sel.Kind == "Component" {
			out = append(out,
				key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "run")),
				key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "compose")),
				key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "env")),
			)
		}
		out = append(out,
			key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "changed-only")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			m.keys.Search,
		)
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
		{"Global", "tab catalog ⇄ activity", "i toggle inspector",
			"⌃b toggle sidebar", "⌃r reload", ": commands", "/ search", "? help", "q quit"},
		{"Navigation", "1 catalog", "2 activity", "esc back", "⌃o back", "⌃i forward"},
		{"Catalog", "[ ] cycle kind", "enter open entity / follow / open run",
			"r run (selected env)", "g compose", "c changed-only", "e cycle env", "esc back", "/ filter"},
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

// renderConfirmModal draws the real-run confirmation overlay shown after the
// user asks to run a component/plan (r). handleKey routes y/n/esc while
// showConfirm is set and blocks every other key, so this must be painted
// whenever showConfirm is true.
func (m Model) renderConfirmModal() string {
	pr := m.pendingRun
	var b strings.Builder
	b.WriteString(theme.StyleModalTitle.Render("Run plan?"))
	b.WriteString("\n\n")
	if pr != nil {
		field := func(label, value string) {
			if value == "" {
				return
			}
			b.WriteString("  " + theme.StyleLabel.Render(fmt.Sprintf("%-11s", label)) +
				theme.StyleValue.Render(value) + "\n")
		}
		field("components", strings.Join(planComponentNames(pr.Plan), ", "))
		field("env", pr.Env)
		field("jobs", fmt.Sprintf("%d", pr.Jobs))
		field("plan", pr.Checksum)
		b.WriteString("\n")
	}
	b.WriteString("  " + theme.StylePillSuccess.Render(" y ") +
		theme.StyleDim.Render(" run · ") +
		theme.StyleValue.Render("n") + theme.StyleDim.Render("/esc cancel"))
	cardW := clamp(m.width*50/100, 36, 70)
	return theme.StyleModalCard.Width(cardW).Render(b.String())
}

// planComponentNames returns the unique component names referenced by a plan's
// jobs, in plan order — used to summarize what a confirmed run will touch.
func planComponentNames(p *model.Plan) []string {
	if p == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(p.Jobs))
	out := make([]string, 0, len(p.Jobs))
	for _, j := range p.Jobs {
		if j.Component == "" {
			continue
		}
		if _, ok := seen[j.Component]; ok {
			continue
		}
		seen[j.Component] = struct{}{}
		out = append(out, j.Component)
	}
	return out
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
	// Re-apply any lazily-loaded historical detail so a rebuild (e.g. a
	// background live run's per-event refresh) doesn't wipe the plan/statuses
	// of a historical run the user is currently drilled into.
	for execID, d := range m.runDetails {
		m.activity = m.activity.SetRunDetail(execID, d.Plan, d.Statuses, d.Steps)
	}
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
	// Kick off the live per-step refresh poll alongside the event stream so the
	// drilldown's status/duration columns advance between job events.
	return m, tea.Batch(cmd, m.spinner.Tick, liveRefreshTickCmd())
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

// liveStepRefreshInterval is how often the cockpit re-reads an in-flight run's
// per-job/step state from the working tree so the drilldown's status + duration
// columns advance in near real time. The run itself only emits job-level events,
// so this poll fills the gap between job boundaries.
const liveStepRefreshInterval = 600 * time.Millisecond

// liveRefreshTickMsg drives the live per-step refresh while a run is in flight.
type liveRefreshTickMsg struct{}

func liveRefreshTickCmd() tea.Cmd {
	return tea.Tick(liveStepRefreshInterval, func(time.Time) tea.Msg { return liveRefreshTickMsg{} })
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

// catalogRefreshedMsg reports a cockpit-side catalog resolve. Refreshed is true
// only when the catalog actually changed, so the handler reloads the workspace
// to surface the new components; a fresh/skipped/failed refresh is a no-op.
type catalogRefreshedMsg struct{ refreshed bool }

// refreshCatalogCmd re-resolves the object-model catalog (staleness-gated unless
// force) off the UI thread. Best-effort: any error is swallowed and reported as
// "not refreshed" so a hostile workspace never blocks the cockpit.
func refreshCatalogCmd(svc services.OrunService, force bool) tea.Cmd {
	return func() tea.Msg {
		res, err := svc.RefreshCatalog(context.Background(), force)
		return catalogRefreshedMsg{refreshed: err == nil && res.Refreshed}
	}
}

// catalogStaleMsg reports the read-only staleness probe used to drive the
// header "⟳ stale" badge.
type catalogStaleMsg struct{ stale bool }

// checkCatalogStaleCmd probes whether the catalog is stale for the current tree
// (read-only, no resolve). Best-effort: an error reports not-stale.
func checkCatalogStaleCmd(svc services.OrunService) tea.Cmd {
	return func() tea.Msg {
		stale, err := svc.CatalogStale(context.Background())
		return catalogStaleMsg{stale: err == nil && stale}
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
