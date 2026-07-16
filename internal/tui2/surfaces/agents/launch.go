package agents

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui2/design"
	"github.com/sourceplane/orun/internal/tui2/frame"
)

// LaunchSpec is the resolved New Session request.
type LaunchSpec struct {
	Type   string
	Task   string
	Driver string
}

var drivers = []string{"claude-code", "stub"}

// LaunchOverlay is the New Session dialog: agent type, task, driver.
// tab cycles fields, enter launches, esc (handled by the shell) cancels.
type LaunchOverlay struct {
	typeIn textinput.Model
	taskIn textinput.Model
	driver int
	focus  int // 0 type, 1 task, 2 driver
	submit func(LaunchSpec) tea.Cmd
	rev    int
}

// NewLaunchOverlay builds the dialog; submit runs on enter.
func NewLaunchOverlay(submit func(LaunchSpec) tea.Cmd) *LaunchOverlay {
	typeIn := textinput.New()
	typeIn.Placeholder = "agent type (blank = ad-hoc)"
	typeIn.Prompt = ""
	typeIn.Focus()
	taskIn := textinput.New()
	taskIn.Placeholder = "task (blank = interactive)"
	taskIn.Prompt = ""
	return &LaunchOverlay{typeIn: typeIn, taskIn: taskIn, submit: submit}
}

// Rev implements shell.Overlay.
func (o *LaunchOverlay) Rev() string { return "launch/" + strconv.Itoa(o.rev) }

// HandleKey implements shell.Overlay.
func (o *LaunchOverlay) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	o.rev++
	switch msg.String() {
	case "enter":
		return o.submit(LaunchSpec{
			Type:   strings.TrimSpace(o.typeIn.Value()),
			Task:   strings.TrimSpace(o.taskIn.Value()),
			Driver: drivers[o.driver],
		}), true
	case "tab", "down":
		o.setFocus((o.focus + 1) % 3)
		return nil, false
	case "shift+tab", "up":
		o.setFocus((o.focus + 2) % 3)
		return nil, false
	case "left", "right":
		if o.focus == 2 {
			o.driver = (o.driver + 1) % len(drivers)
			return nil, false
		}
	}
	var cmd tea.Cmd
	switch o.focus {
	case 0:
		o.typeIn, cmd = o.typeIn.Update(msg)
	case 1:
		o.taskIn, cmd = o.taskIn.Update(msg)
	case 2:
		if msg.String() == " " {
			o.driver = (o.driver + 1) % len(drivers)
		}
	}
	return cmd, false
}

func (o *LaunchOverlay) setFocus(f int) {
	o.focus = f
	o.typeIn.Blur()
	o.taskIn.Blur()
	switch f {
	case 0:
		o.typeIn.Focus()
	case 1:
		o.taskIn.Focus()
	}
}

// View implements shell.Overlay.
func (o *LaunchOverlay) View(max frame.Size) string {
	w := min(56, max.Width-8)
	label := func(i int, name string) string {
		if o.focus == i {
			return design.Selected.Render(name)
		}
		return design.Dim.Render(name)
	}
	driver := drivers[o.driver]
	if o.focus == 2 {
		driver = design.Selected.Render("‹ " + driver + " ›")
	} else {
		driver = design.Text.Render(driver)
	}
	lines := []string{
		label(0, "type") + "    " + frame.FitLine(o.typeIn.View(), w-10),
		label(1, "task") + "    " + frame.FitLine(o.taskIn.View(), w-10),
		label(2, "driver") + "  " + driver,
		"",
		design.ToneInfo.Style().Render("enter") + " " + design.Text.Render("Launch session") + design.Dim.Render("   ·   esc cancel"),
	}
	return design.Box("New session", strings.Join(lines, "\n"), max)
}
