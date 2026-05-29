package tui

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
	if msg := cmd(); msg == nil {
		t.Fatal("reload cmd returned nil msg")
	}
	select {
	case <-called:
	default:
		t.Fatal("LoadWorkspace was not invoked by reload cmd")
	}
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
