package views

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
)

func launchTypes() []services.AgentTypeRow {
	return []services.AgentTypeRow{
		{Name: "implementer", Harness: "claude-code", Model: "claude-opus-4-8", Owner: "team/pay"},
		{Name: "reviewer", Harness: "claude-code", Model: "claude-opus-4-8", Owner: "team/pay"},
	}
}

func lkey(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// The default driver follows whether the claude CLI is on PATH: present ⇒
// claude-code (delegate to Claude), absent ⇒ the stub (so the dialog never
// defaults to a driver that cannot run).
func TestAgentLaunch_DriverDefault(t *testing.T) {
	if got := NewAgentLaunchModel(launchTypes(), true).Spec().Driver; got != "claude-code" {
		t.Fatalf("claudeOK=true default driver = %q, want claude-code", got)
	}
	if got := NewAgentLaunchModel(launchTypes(), false).Spec().Driver; got != "stub" {
		t.Fatalf("claudeOK=false default driver = %q, want stub", got)
	}
}

// The first agent type is preselected; cycling the type field wraps through an
// extra "none" (ad-hoc) slot.
func TestAgentLaunch_TypeCycleWrapsThroughNone(t *testing.T) {
	m := NewAgentLaunchModel(launchTypes(), true)
	if got := m.Spec().Type; got != "implementer" {
		t.Fatalf("initial type = %q, want implementer", got)
	}
	// Field 0 is the type field. right → reviewer, right → none, right → implementer.
	m, _ = m.Update(lkey("right"))
	if got := m.Spec().Type; got != "reviewer" {
		t.Fatalf("after 1 right, type = %q, want reviewer", got)
	}
	m, _ = m.Update(lkey("right"))
	if got := m.Spec().Type; got != "" {
		t.Fatalf("after 2 right, type = %q, want none (empty)", got)
	}
	m, _ = m.Update(lkey("right"))
	if got := m.Spec().Type; got != "implementer" {
		t.Fatalf("after 3 right, type = %q, want implementer (wrapped)", got)
	}
}

// down moves focus to the driver field; left/right toggles the driver.
func TestAgentLaunch_DriverToggle(t *testing.T) {
	m := NewAgentLaunchModel(launchTypes(), true) // default claude-code
	m, _ = m.Update(lkey("down"))                 // → driver field
	m, _ = m.Update(lkey("right"))                // toggle
	if got := m.Spec().Driver; got != "stub" {
		t.Fatalf("after toggle, driver = %q, want stub", got)
	}
	m, _ = m.Update(lkey("right")) // toggle back
	if got := m.Spec().Driver; got != "claude-code" {
		t.Fatalf("after toggle back, driver = %q, want claude-code", got)
	}
}

// Typing on the task field feeds the text input; the spec carries the task.
func TestAgentLaunch_TaskEntryAndSubmit(t *testing.T) {
	m := NewAgentLaunchModel(launchTypes(), true)
	m, _ = m.Update(lkey("down")) // driver
	m, _ = m.Update(lkey("down")) // task
	for _, r := range "ORN-142" {
		m, _ = m.Update(lkey(string(r)))
	}
	m, submit := m.Update(lkey("enter"))
	if !submit {
		t.Fatal("enter should submit")
	}
	spec := m.Spec()
	if spec.Task != "ORN-142" {
		t.Fatalf("task = %q, want ORN-142", spec.Task)
	}
	if spec.Type != "implementer" || spec.Driver != "claude-code" {
		t.Fatalf("spec = %+v, want implementer/claude-code", spec)
	}
}

// The dialog renders its title, both driver options' semantics, and the Launch
// affordance — the surface a user reads.
func TestAgentLaunch_ViewSurface(t *testing.T) {
	v := NewAgentLaunchModel(launchTypes(), true).View()
	for _, want := range []string{"New agent session", "claude-code", "Launch", "implementer"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
	// When claude is absent and the driver is claude-code, the view warns.
	warn := NewAgentLaunchModel(launchTypes(), false)
	warn, _ = warn.Update(lkey("down"))  // driver field
	warn, _ = warn.Update(lkey("right")) // stub → claude-code
	if !strings.Contains(warn.View(), "claude not on PATH") {
		t.Errorf("expected a claude-not-found warning:\n%s", warn.View())
	}
}
