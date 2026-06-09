package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/views"
)

func TestNewModel_Defaults(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	if m.ActiveMode() != ModeBrowse {
		t.Errorf("ActiveMode = %v, want ModeBrowse", m.ActiveMode())
	}
	if m.ActivePanel() != PanelMain {
		t.Errorf("ActivePanel = %v, want PanelMain", m.ActivePanel())
	}
	if !m.loading {
		t.Errorf("expected loading=true at start")
	}
	if m.Workspace() != nil {
		t.Errorf("expected nil workspace at start")
	}
}

func TestModel_TabTogglesTopLevelMode(t *testing.T) {
	tab := tea.KeyMsg{Type: tea.KeyTab}
	m := NewModel(&services.MockOrunService{})
	if m.ActiveMode() != ModeBrowse {
		t.Fatalf("expected ModeBrowse at start, got %v", m.ActiveMode())
	}
	next, _ := m.Update(tab)
	m = next.(Model)
	if m.ActiveMode() != ModeActivity {
		t.Fatalf("after tab: ActiveMode = %v, want ModeActivity", m.ActiveMode())
	}
	next, _ = m.Update(tab)
	m = next.(Model)
	if m.ActiveMode() != ModeBrowse {
		t.Fatalf("after second tab: ActiveMode = %v, want ModeBrowse", m.ActiveMode())
	}
}

func TestModel_InspectorToggle(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	// Force inspector on regardless of any persisted user preference so the
	// test is independent of ~/.orun/cockpit.json on the developer machine.
	m.showInspector = true
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = next.(Model)
	if m.showInspector {
		t.Fatal("expected inspector hidden after first `i` toggle")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = next.(Model)
	if !m.showInspector {
		t.Fatal("expected inspector visible after second `i` toggle")
	}
}

func TestModel_WorkspaceLoadedMsg_StoresSnapshot(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	snap := &services.WorkspaceSnapshot{IntentName: "fixture"}
	next, _ := m.Update(services.WorkspaceLoadedMsg{Snapshot: snap})
	m = next.(Model)
	if m.loading {
		t.Errorf("expected loading=false after WorkspaceLoadedMsg")
	}
	if m.Workspace() != snap {
		t.Errorf("expected stored snapshot pointer")
	}
	if m.LastError() != nil {
		t.Errorf("unexpected error: %v", m.LastError())
	}
}

func TestModel_WorkspaceLoadedMsg_StoresError(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	errBoom := errors.New("boom")
	next, _ := m.Update(services.WorkspaceLoadedMsg{Err: errBoom})
	m = next.(Model)
	if m.loading {
		t.Errorf("expected loading=false after error")
	}
	if !errors.Is(m.LastError(), errBoom) {
		t.Errorf("LastError = %v, want %v", m.LastError(), errBoom)
	}
}

func TestModel_WindowSizeMsg_StoresDimensions(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	if m.width != 120 || m.height != 40 {
		t.Fatalf("size = %dx%d, want 120x40", m.width, m.height)
	}
}

func TestModel_QuitKey_ReturnsQuitCmd(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected tea.Quit cmd, got nil")
	}
	// Invoke the cmd; tea.Quit returns tea.QuitMsg.
	if msg := cmd(); msg == nil {
		t.Fatal("expected non-nil msg from quit cmd")
	}
}

func TestModel_HelpToggleAndCancel(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	help := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}
	next, _ := m.Update(help)
	m = next.(Model)
	if !m.showHelp {
		t.Fatal("expected showHelp=true after ?")
	}
	esc := tea.KeyMsg{Type: tea.KeyEsc}
	next, _ = m.Update(esc)
	m = next.(Model)
	if m.showHelp {
		t.Fatal("expected showHelp=false after esc")
	}
}

func TestModel_PaletteAndHelpAreMutuallyExclusive(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	m = next.(Model)
	if m.showHelp {
		t.Fatal("opening palette should close help")
	}
	if !m.showCommandPalette {
		t.Fatal("expected palette visible after :")
	}
}

func TestModel_ReloadKey_TriggersLoadWorkspaceCmd(t *testing.T) {
	called := make(chan struct{}, 1)
	svc := &services.MockOrunService{
		LoadWorkspaceFn: func(ctx context.Context, req services.WorkspaceRequest) (*services.WorkspaceSnapshot, error) {
			called <- struct{}{}
			return &services.WorkspaceSnapshot{}, nil
		},
	}
	m := NewModel(svc)
	// First mark loading=false so we can observe the reset.
	next, _ := m.Update(services.WorkspaceLoadedMsg{Snapshot: &services.WorkspaceSnapshot{}})
	m = next.(Model)
	if m.loading {
		t.Fatal("expected loading=false after initial load")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = next.(Model)
	if !m.loading {
		t.Fatal("expected loading=true after reload key")
	}
	if cmd == nil {
		t.Fatal("expected reload cmd")
	}
	// ⌃r now batches the workspace reload with a catalog refresh; run the
	// batched sub-commands so the load fires.
	runCmd(cmd)
	select {
	case <-called:
	default:
		t.Fatal("LoadWorkspace was not invoked by reload cmd")
	}
}

// runCmd executes a tea.Cmd, recursing into tea.BatchMsg sub-commands so a test
// can drive a batched command's side effects.
func runCmd(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			runCmd(c)
		}
	}
}

func TestModel_ActivityLoadRunDetailMsg_DispatchesLoad(t *testing.T) {
	called := ""
	svc := &services.MockOrunService{
		GetRunDetailFn: func(_ context.Context, req services.RunDetailRequest) (services.RunDetail, error) {
			called = req.ExecID
			return services.RunDetail{ExecID: req.ExecID}, nil
		},
	}
	m := NewModel(svc)
	_, cmd := m.Update(views.ActivityLoadRunDetailMsg{ExecID: "exec-h"})
	if cmd == nil {
		t.Fatal("ActivityLoadRunDetailMsg should produce a load cmd")
	}
	cmd() // executes GetRunDetail
	if called != "exec-h" {
		t.Fatalf("GetRunDetail called with %q, want exec-h", called)
	}
}

func TestModel_RunDetailLoadedMsg_HydratesAndSurvivesRefresh(t *testing.T) {
	m := NewModel(&services.MockOrunService{})

	// Seed a plan-less historical run into the Activity pane.
	m.history.Runs = []services.RunSummary{{ExecID: "exec-h", Status: "completed"}}
	m.refreshActivityRuns()
	if r := activityRun(m, "exec-h"); r == nil || r.Plan != nil {
		t.Fatalf("expected a plan-less historical run before hydration")
	}

	plan := &model.Plan{Jobs: []model.PlanJob{
		{ID: "j1", Component: "cli", Steps: []model.PlanStep{{ID: "s1", Run: "echo"}}},
	}}
	out, _ := m.Update(services.RunDetailLoadedMsg{
		ExecID: "exec-h", Plan: plan, Statuses: map[string]string{"j1": "completed"},
	})
	m = out.(Model)

	r := activityRun(m, "exec-h")
	if r == nil || r.Plan == nil {
		t.Fatal("run should be hydrated with a plan after RunDetailLoadedMsg")
	}
	if r.Statuses["j1"] != "completed" {
		t.Fatalf("statuses not applied: %+v", r.Statuses)
	}

	// A subsequent rebuild (e.g. a background live run's per-event refresh)
	// must NOT wipe the hydrated detail.
	m.refreshActivityRuns()
	if r := activityRun(m, "exec-h"); r == nil || r.Plan == nil {
		t.Fatal("hydrated detail should survive a run-list rebuild")
	}
}

// activityRun returns the activity run with the given exec id (or nil).
func activityRun(m Model, execID string) *views.ActivityRun {
	for i := range m.activity.Runs {
		if m.activity.Runs[i].ExecID == execID {
			return &m.activity.Runs[i]
		}
	}
	return nil
}

func TestModel_View_EmptyWhenNoSize(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	if out := m.View(); out != "" {
		t.Fatalf("expected empty view, got %q", out)
	}
}

func TestModel_View_RendersWhenSized(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	if out := m.View(); out == "" {
		t.Fatal("expected non-empty view after WindowSizeMsg")
	}
}

// TestModel_ConfirmModal_RendersWhenArmed guards the run-with-r flow: while the
// confirm overlay is armed, handleKey blocks every key but y/n/esc, so the
// modal MUST be painted — otherwise the cockpit looks frozen and the run never
// visibly happens.
func TestModel_ConfirmModal_RendersWhenArmed(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)

	m.showConfirm = true
	m.pendingRun = &PendingRun{
		Plan: &model.Plan{Jobs: []model.PlanJob{{ID: "j1", Component: "api"}}},
		Env:  "prod", Checksum: "abc123", Jobs: 1,
	}

	out := m.View()
	for _, want := range []string{"Run plan?", "api", "prod", "cancel"} {
		if !strings.Contains(out, want) {
			t.Fatalf("confirm modal missing %q in view:\n%s", want, out)
		}
	}
}

// TestModel_ConfirmModal_YDispatchesRun verifies that, with the overlay armed,
// pressing y kicks off the real run and clears the modal.
func TestModel_ConfirmModal_YDispatchesRun(t *testing.T) {
	ran := false
	svc := &services.MockOrunService{
		RunPlanFn: func(_ context.Context, req services.RunRequest) (<-chan services.RunEvent, error) {
			if req.DryRun {
				t.Errorf("confirm should dispatch a real run, got DryRun=true")
			}
			ran = true
			ch := make(chan services.RunEvent)
			close(ch)
			return ch, nil
		},
	}
	m := NewModel(svc)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	m.showConfirm = true
	m.pendingRun = &PendingRun{Plan: &model.Plan{Jobs: []model.PlanJob{{ID: "j1", Component: "api"}}}, Env: "prod", Jobs: 1}

	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = out.(Model)
	if !ran {
		t.Fatal("pressing y should dispatch the real run")
	}
	if m.showConfirm || m.pendingRun != nil {
		t.Fatal("confirm modal should clear after y")
	}
}

// TestModel_FrameFitsTerminal guards TUI frame integrity: the rendered frame
// must never exceed the terminal's rows/cols, or it scrolls and leaves residue
// (duplicated footers/status lines), most visibly under the live log view's
// frequent re-renders.
func TestModel_FrameFitsTerminal(t *testing.T) {
	sizes := [][2]int{{204, 50}, {120, 40}, {100, 30}, {80, 24}}
	// Check each top-level surface — the log/step view (Activity) is where the
	// overflow visibly corrupted the frame.
	modes := []struct {
		name string
		key  string
	}{
		{"browse", ""},
		{"activity", "2"},
	}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		for _, md := range modes {
			m := NewModel(&services.MockOrunService{})
			next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			m = next.(Model)
			m.loading = false // render real views, not the startup spinner
			if md.key != "" {
				n2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(md.key)})
				m = n2.(Model)
			}
			out := m.View()
			// Must be EXACTLY the terminal height: a short frame leaves stale
			// rows below it on the next render just as a tall one overflows.
			if gh := lipgloss.Height(out); gh != h {
				t.Errorf("%s at %dx%d: frame height %d, want exactly %d", md.name, w, h, gh, h)
			}
			if gw := lipgloss.Width(out); gw > w {
				t.Errorf("%s at %dx%d: frame width %d exceeds terminal width %d", md.name, w, h, gw, w)
			}
		}
	}
}

// TestModel_StepLogViewFitsTerminal drives the exact surface that was breaking
// the frame — the Activity step-log view with long log lines — and asserts the
// rendered frame stays within the terminal at full size.
func TestModel_StepLogViewFitsTerminal(t *testing.T) {
	const w, h = 204, 50
	m := NewModel(&services.MockOrunService{})
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = next.(Model)
	m.loading = false

	// A completed run with one job + step, hosted in Activity.
	plan := &model.Plan{Jobs: []model.PlanJob{
		{ID: "cli.dev.verify", Component: "cli", Environment: "dev",
			Steps: []model.PlanStep{{ID: "setup-node", Name: "setup-node", Use: "actions/setup-node@v6"}}},
	}}
	m.activity = m.activity.SetRuns(&views.ActivityRun{
		ExecID: "e1", PlanName: "multi-tenant-saas", Status: "completed",
		Plan: plan, Statuses: map[string]string{"cli.dev.verify": "completed"},
	}, nil)
	n2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")}) // → Activity
	m = n2.(Model)
	for i := 0; i < 3; i++ { // index → run → job → step
		n, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = n.(Model)
	}

	// Feed long log lines (the real overflow trigger).
	long := "WARN Failed to create bin at /Users/irinelinson/sourceplane/multi-tenant-saas/.orun/runs/multi-tenant-saas-20260608-368372/cli/node_modules/.bin/some-really-long-binary"
	for i := 0; i < 40; i++ {
		n, _ := m.Update(services.LogEventMsg{Event: services.LogEvent{
			JobID: "cli.dev.verify", StepID: "setup-node", Line: long, Timestamp: time.Unix(0, 0),
		}})
		m = n.(Model)
	}

	out := m.View()
	if gh := lipgloss.Height(out); gh != h {
		t.Fatalf("step-log frame height %d, want exactly %d", gh, h)
	}
	if gw := lipgloss.Width(out); gw > w {
		t.Fatalf("step-log frame width %d exceeds terminal %d", gw, w)
	}
}

// TestModel_LiveRenderHeightStable simulates a live run's rapid re-renders
// (spinner ticks, detail reloads, log events, a transient toast) and asserts
// the frame height never changes — the invariant that prevents alt-screen
// residue (ghosted spinner rows, stacked status lines).
func TestModel_LiveRenderHeightStable(t *testing.T) {
	const w, h = 204, 50
	m := NewModel(&services.MockOrunService{})
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = next.(Model)
	m.loading = false

	plan := &model.Plan{Jobs: []model.PlanJob{
		{ID: "cli.dev.verify", Component: "cli", Environment: "dev",
			Steps: []model.PlanStep{{ID: "a", Name: "a", Run: "x"}, {ID: "b", Name: "b", Run: "y"}}},
	}}
	m.activity = m.activity.SetRuns(&views.ActivityRun{
		ExecID: "e1", Status: "running", Live: true, Plan: plan,
		Statuses: map[string]string{"cli.dev.verify": "running"},
		StepInfo: map[string]services.StepInfo{
			services.StepDetailKey("cli.dev.verify", "a"): {Status: "running"},
			services.StepDetailKey("cli.dev.verify", "b"): {Status: "pending"},
		},
	}, nil)
	n2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m = n2.(Model)
	for i := 0; i < 2; i++ { // → job step list
		n, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = n.(Model)
	}

	if got := lipgloss.Height(m.View()); got != h {
		t.Fatalf("live job view height %d, want %d", got, h)
	}
	// A toast must not change the frame height (stage absorbs the chrome delta).
	withToast := m
	withToast.toast = "compiling cli · dev…"
	if got := lipgloss.Height(withToast.View()); got != h {
		t.Fatalf("frame height with toast = %d, want %d (toast must not destabilize height)", got, h)
	}
}

func TestPanelHelpers(t *testing.T) {
	if nextPanel(PanelNavigator) != PanelMain {
		t.Error("next nav -> main")
	}
	if nextPanel(PanelInspector) != PanelNavigator {
		t.Error("next inspector -> navigator (wrap)")
	}
	if prevPanel(PanelNavigator) != PanelInspector {
		t.Error("prev nav -> inspector (wrap)")
	}
	if prevPanel(PanelMain) != PanelNavigator {
		t.Error("prev main -> navigator")
	}
}

func TestModel_DryRunRequested_DispatchesRunPlanAndSwitchesMode(t *testing.T) {
	plan := &model.Plan{Jobs: []model.PlanJob{{ID: "j1"}}}
	var got services.RunRequest
	called := make(chan struct{}, 1)
	svc := &services.MockOrunService{
		RunPlanFn: func(ctx context.Context, req services.RunRequest) (<-chan services.RunEvent, error) {
			got = req
			ch := make(chan services.RunEvent)
			close(ch)
			called <- struct{}{}
			return ch, nil
		},
	}
	m := NewModel(svc)
	next, _ := m.Update(views.PlanStudioDryRunRequestedMsg{Plan: plan})
	m = next.(Model)

	select {
	case <-called:
	default:
		t.Fatal("svc.RunPlan was not invoked")
	}
	if got.Plan != plan {
		t.Error("plan pointer not forwarded")
	}
	if !got.DryRun {
		t.Error("DryRun=true should be set")
	}
	if m.ActiveMode() != ModeActivity {
		t.Errorf("ActiveMode = %v, want ModeActivity", m.ActiveMode())
	}
	if m.LastError() != nil {
		t.Errorf("unexpected error: %v", m.LastError())
	}
}

func TestModel_DryRunRequested_RunPlanErrorStaysInPlanStudio(t *testing.T) {
	boom := errors.New("validation failure")
	svc := &services.MockOrunService{
		RunPlanFn: func(ctx context.Context, req services.RunRequest) (<-chan services.RunEvent, error) {
			return nil, boom
		},
	}
	m := NewModel(svc)
	m.activeMode = ModePlanStudio
	next, _ := m.Update(views.PlanStudioDryRunRequestedMsg{Plan: &model.Plan{}})
	m = next.(Model)
	if m.ActiveMode() != ModePlanStudio {
		t.Errorf("ActiveMode = %v, want ModePlanStudio (no transition on error)", m.ActiveMode())
	}
	if !errors.Is(m.LastError(), boom) {
		t.Errorf("LastError = %v, want %v", m.LastError(), boom)
	}
}

func TestMode_String(t *testing.T) {
	cases := map[Mode]string{
		ModeBrowse:       "browse",
		ModePlanStudio:   "plan-studio",
		ModeRunDashboard: "run-dashboard",
		ModeLogExplorer:  "log-explorer",
		ModeHistory:      "history",
	}
	for m, want := range cases {
		if got := m.String(); got != want {
			t.Errorf("Mode(%d).String() = %q, want %q", m, got, want)
		}
	}
}
