package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/views"
)

// Mode identifies which primary view is active in the Main panel.
type Mode int

const (
	ModeBrowse Mode = iota
	ModePlanStudio
	ModeRunDashboard
	ModeLogExplorer
	ModeHistory
)

// String returns the human-readable mode name (used in tests and the
// status bar).
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
	default:
		return "unknown"
	}
}

// Panel identifies which of the three panels has keyboard focus.
type Panel int

const (
	PanelNavigator Panel = iota
	PanelMain
	PanelInspector
)

// Model is the root Bubble Tea model.
type Model struct {
	width, height int

	activeMode  Mode
	activePanel Panel

	navigator  views.NavigatorModel
	browse     views.BrowseModel
	history    views.HistoryModel
	planStudio views.PlanStudioModel
	runView    views.RunViewModel
	logView    views.LogExplorerModel
	inspector  views.InspectorModel

	commandPalette views.CommandPaletteModel
	helpModel      help.Model

	showHelp           bool
	showCommandPalette bool

	svc       services.OrunService
	workspace *services.WorkspaceSnapshot
	loading   bool
	lastErr   error

	keys GlobalKeyMap
}

// NewModel constructs the root model with default state.
func NewModel(svc services.OrunService) Model {
	return Model{
		svc:            svc,
		activeMode:     ModeBrowse,
		activePanel:    PanelMain,
		navigator:      views.NewNavigatorModel(),
		browse:         views.NewBrowseModel(),
		history:        views.NewHistoryModel(),
		planStudio:     views.NewPlanStudioModel(),
		runView:        views.NewRunViewModel(),
		logView:        views.NewLogExplorerModel(),
		inspector:      views.NewInspectorModel(),
		commandPalette: views.NewCommandPaletteModel(),
		helpModel:      help.New(),
		loading:        true,
		keys:           DefaultGlobalKeyMap(),
	}
}

// ActiveMode returns the currently active primary view (used by tests).
func (m Model) ActiveMode() Mode { return m.activeMode }

// ActivePanel returns the currently focused panel (used by tests).
func (m Model) ActivePanel() Panel { return m.activePanel }

// Workspace returns the most recently loaded workspace snapshot, or nil
// if none has loaded yet. Used by tests asserting workspace immutability.
func (m Model) Workspace() *services.WorkspaceSnapshot { return m.workspace }

// LastError returns the most recently surfaced error (or nil).
func (m Model) LastError() error { return m.lastErr }

// Init asynchronously loads the workspace snapshot.
func (m Model) Init() tea.Cmd {
	return loadWorkspaceCmd(m.svc)
}

// loadWorkspaceCmd returns a tea.Cmd that calls OrunService.LoadWorkspace.
func loadWorkspaceCmd(svc services.OrunService) tea.Cmd {
	return func() tea.Msg {
		snap, err := svc.LoadWorkspace(context.Background(), services.WorkspaceRequest{})
		return services.WorkspaceLoadedMsg{Snapshot: snap, Err: err}
	}
}

// Update dispatches all messages. Global keys are handled first; the
// remainder is forwarded to the focused panel's update.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case services.WorkspaceLoadedMsg:
		m.loading = false
		if msg.Err != nil {
			m.lastErr = msg.Err
			return m, nil
		}
		m.lastErr = nil
		m.workspace = msg.Snapshot
		m.browse.Workspace = msg.Snapshot
		return m, nil

	case services.ErrMsg:
		m.lastErr = msg.Err
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Reload):
			m.loading = true
			m.lastErr = nil
			return m, loadWorkspaceCmd(m.svc)
		case key.Matches(msg, m.keys.NextPanel):
			m.activePanel = nextPanel(m.activePanel)
			return m, nil
		case key.Matches(msg, m.keys.PrevPanel):
			m.activePanel = prevPanel(m.activePanel)
			return m, nil
		case key.Matches(msg, m.keys.Help):
			m.showHelp = !m.showHelp
			if m.showHelp {
				m.showCommandPalette = false
			}
			return m, nil
		case key.Matches(msg, m.keys.Palette):
			m.showCommandPalette = !m.showCommandPalette
			if m.showCommandPalette {
				m.showHelp = false
			}
			return m, nil
		case key.Matches(msg, m.keys.Cancel):
			if m.showHelp || m.showCommandPalette {
				m.showHelp = false
				m.showCommandPalette = false
			}
			return m, nil
		}
	}
	return m, nil
}

// View renders the full frame: error banner (if any), three-panel layout,
// status bar, and key-hint bar.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	header := ""
	if m.lastErr != nil {
		header = StyleErrorBanner.Render("Error: " + m.lastErr.Error())
	}

	navWidth := 24
	insWidth := 32
	if m.width < 80 {
		navWidth = m.width / 4
		insWidth = m.width / 4
	}
	mainWidth := m.width - navWidth - insWidth - 6
	if mainWidth < 10 {
		mainWidth = 10
	}

	panelHeight := m.height - 4
	if header != "" {
		panelHeight--
	}
	if panelHeight < 3 {
		panelHeight = 3
	}

	navContent := m.navigator.View()
	mainContent := m.renderMain()
	insContent := m.inspector.View()

	navBox := m.panelStyle(PanelNavigator).Width(navWidth).Height(panelHeight).Render(navContent)
	mainBox := m.panelStyle(PanelMain).Width(mainWidth).Height(panelHeight).Render(mainContent)
	insBox := m.panelStyle(PanelInspector).Width(insWidth).Height(panelHeight).Render(insContent)

	row := lipgloss.JoinHorizontal(lipgloss.Top, navBox, mainBox, insBox)

	status := m.renderStatusBar()
	hints := m.renderKeyHints()

	parts := []string{}
	if header != "" {
		parts = append(parts, header)
	}
	parts = append(parts, row, status, hints)

	if m.showHelp {
		parts = append(parts, StyleKeyHint.Render(m.helpModel.FullHelpView(m.keys.FullHelp())))
	}
	if m.showCommandPalette {
		parts = append(parts, StyleAccent.Render(m.commandPalette.View()))
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m Model) renderMain() string {
	if m.loading {
		return StyleLoading.Render("Loading workspace…")
	}
	switch m.activeMode {
	case ModeBrowse:
		return m.browse.View()
	case ModePlanStudio:
		return m.planStudio.View()
	case ModeRunDashboard:
		return m.runView.View()
	case ModeLogExplorer:
		return m.logView.View()
	case ModeHistory:
		return m.history.View()
	}
	return ""
}

func (m Model) renderStatusBar() string {
	intent := "(no intent)"
	if m.workspace != nil && m.workspace.IntentName != "" {
		intent = m.workspace.IntentName
	}
	mode := m.activeMode.String()
	panel := panelName(m.activePanel)
	return StyleStatusBar.Render(fmt.Sprintf("intent=%s  mode=%s  panel=%s", intent, mode, panel))
}

func (m Model) renderKeyHints() string {
	parts := []string{}
	for _, k := range m.keys.ShortHelp() {
		parts = append(parts, k.Help().Key+" "+k.Help().Desc)
	}
	return StyleKeyHint.Render(strings.Join(parts, "  •  "))
}

func (m Model) panelStyle(p Panel) lipgloss.Style {
	if p == m.activePanel {
		return StylePanelFocused
	}
	return StylePanel
}

func nextPanel(p Panel) Panel {
	return Panel((int(p) + 1) % 3)
}

func prevPanel(p Panel) Panel {
	return Panel((int(p) + 2) % 3)
}

func panelName(p Panel) string {
	switch p {
	case PanelNavigator:
		return "navigator"
	case PanelMain:
		return "main"
	case PanelInspector:
		return "inspector"
	}
	return "?"
}
