package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/theme"
)

// LaunchSpec is the resolved configuration for a new agent session, produced
// when the user confirms the New Session dialog. The root model turns it into
// `orun agent run` flags and spawns a detached body it then attaches to.
type LaunchSpec struct {
	Driver string // "claude-code" (delegate to Claude) or "stub" (deterministic)
	Type   string // agent type name, or "" for an ad-hoc session
	Task   string // work item key, or "" for an interactive chat session
}

// launchDrivers is the driver choice the dialog offers — the real driver first,
// so delegating to Claude is the default and the stub is the explicit fallback.
var launchDrivers = []string{"claude-code", "stub"}

type launchField int

const (
	lfType launchField = iota
	lfDriver
	lfTask
	lfLaunch
	launchFieldCount
)

// AgentLaunchModel is the New Session dialog — the "cloud-CLI" launch flow that
// was missing: pick an agent type, choose a driver (Claude or the deterministic
// stub), optionally bind a task, then launch. It owns no I/O; it resolves a
// LaunchSpec the root model executes as `orun agent run --detach …`.
type AgentLaunchModel struct {
	Types     []services.AgentTypeRow
	typeIdx   int // index into Types, or -1 for "none (ad-hoc)"
	driverIdx int
	task      textinput.Model
	field     launchField
	claudeOK  bool // claude CLI present on PATH — drives the default + the hint
	Width     int
}

// NewAgentLaunchModel builds the dialog from the loaded agent types. claudeOK
// (whether the `claude` CLI is on PATH) selects the default driver: Claude when
// present, the stub otherwise, so the dialog never defaults to a driver that
// cannot run.
func NewAgentLaunchModel(types []services.AgentTypeRow, claudeOK bool) AgentLaunchModel {
	ti := textinput.New()
	ti.Placeholder = "e.g. ORN-142   (blank = interactive chat)"
	ti.Prompt = "› "
	ti.CharLimit = 120
	ti.Width = 44
	driverIdx := 0 // claude-code
	if !claudeOK {
		driverIdx = 1 // fall back to the stub when Claude isn't installed
	}
	typeIdx := -1
	if len(types) > 0 {
		typeIdx = 0
	}
	m := AgentLaunchModel{Types: types, typeIdx: typeIdx, driverIdx: driverIdx, task: ti, claudeOK: claudeOK}
	return m.syncFocus()
}

// Spec resolves the current selection into a launch specification.
func (m AgentLaunchModel) Spec() LaunchSpec {
	typ := ""
	if m.typeIdx >= 0 && m.typeIdx < len(m.Types) {
		typ = m.Types[m.typeIdx].Name
	}
	return LaunchSpec{
		Driver: launchDrivers[m.driverIdx],
		Type:   typ,
		Task:   strings.TrimSpace(m.task.Value()),
	}
}

// Update handles one key while the dialog is open. It returns the next model and
// submit=true when the user confirmed (enter) and the resolved Spec should run.
func (m AgentLaunchModel) Update(msg tea.KeyMsg) (AgentLaunchModel, bool) {
	switch msg.String() {
	case "enter":
		return m, true
	case "up", "shift+tab":
		m.field = (m.field + launchField(launchFieldCount) - 1) % launchField(launchFieldCount)
		return m.syncFocus(), false
	case "down", "tab":
		m.field = (m.field + 1) % launchField(launchFieldCount)
		return m.syncFocus(), false
	case "left":
		if m.field == lfType || m.field == lfDriver {
			return m.cycle(-1), false
		}
	case "right":
		if m.field == lfType || m.field == lfDriver {
			return m.cycle(1), false
		}
	}
	// Any other key edits the task field when it holds focus.
	if m.field == lfTask {
		var cmd tea.Cmd
		m.task, cmd = m.task.Update(msg)
		_ = cmd
	}
	return m, false
}

// syncFocus keeps the task text-input focused exactly when the task field is
// active, so the caret and typed characters land only there.
func (m AgentLaunchModel) syncFocus() AgentLaunchModel {
	if m.field == lfTask {
		m.task.Focus()
	} else {
		m.task.Blur()
	}
	return m
}

// cycle moves the value of the active choice field. The type field wraps
// through an extra "none" slot (index -1) so an ad-hoc session is selectable.
func (m AgentLaunchModel) cycle(delta int) AgentLaunchModel {
	switch m.field {
	case lfType:
		n := len(m.Types)
		m.typeIdx += delta
		if m.typeIdx < -1 {
			m.typeIdx = n - 1
		} else if m.typeIdx >= n {
			m.typeIdx = -1
		}
	case lfDriver:
		n := len(launchDrivers)
		m.driverIdx = (m.driverIdx + delta + n) % n
	}
	return m
}

// View renders the dialog body; the root model wraps it in a modal card.
func (m AgentLaunchModel) View() string {
	var b strings.Builder
	b.WriteString(theme.StyleModalTitle.Render("New agent session"))
	b.WriteString("\n\n")

	// Agent type.
	typeVal := theme.StyleMuted.Render("none — ad-hoc session")
	if m.typeIdx >= 0 && m.typeIdx < len(m.Types) {
		t := m.Types[m.typeIdx]
		typeVal = theme.StyleValue.Render(t.Name) + theme.StyleDim.Render("  "+dash(t.Harness)+" · "+dash(t.Model))
	}
	b.WriteString(m.fieldRow("agent", typeVal, m.field == lfType, len(m.Types) > 0))

	// Driver.
	var driverVal string
	if launchDrivers[m.driverIdx] == "claude-code" {
		driverVal = theme.StyleValue.Render("claude-code") + theme.StyleDim.Render("  delegate to Claude")
	} else {
		driverVal = theme.StyleValue.Render("stub") + theme.StyleDim.Render("  deterministic · no Claude/network")
	}
	b.WriteString(m.fieldRow("driver", driverVal, m.field == lfDriver, true))

	// Task.
	b.WriteString(m.fieldRow("task", m.task.View(), m.field == lfTask, false))

	b.WriteByte('\n')
	if m.field == lfLaunch {
		b.WriteString("  " + theme.StylePillSuccess.Render(" Launch ") + theme.StyleDim.Render("  enter"))
	} else {
		b.WriteString("  " + theme.StyleChipAccent.Render(" Launch ") + theme.StyleDim.Render("  enter to launch"))
	}
	b.WriteByte('\n')

	if !m.claudeOK && launchDrivers[m.driverIdx] == "claude-code" {
		b.WriteString("\n  " + theme.StylePillWarn.Render(" ! ") +
			theme.StyleMuted.Render(" claude not on PATH — run `orun agent doctor` (session will fail to start)"))
	}
	b.WriteString("\n  " + theme.StyleDim.Render("↑↓ field · ←→ change · enter launch · esc cancel"))
	return b.String()
}

func (m AgentLaunchModel) fieldRow(label, value string, active, cyclable bool) string {
	marker := "  "
	lbl := fmt.Sprintf("%-8s", label)
	if active {
		marker = theme.StyleAccent.Render("▸ ")
		lbl = theme.StyleAccent.Render(lbl)
	} else {
		lbl = theme.StyleLabel.Render(lbl)
	}
	arrows := ""
	if active && cyclable {
		arrows = theme.StyleDim.Render("  ‹ ›")
	}
	return marker + lbl + value + arrows + "\n"
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
