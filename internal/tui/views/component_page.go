package views

// Component page — the middle level of the catalog→component→job→logs
// drill-down (specs/orun-catalog-state/consumers.md §3). It is a pure
// *consumer* of the read seam: it renders the component detail (from the
// resolved catalog, via ComponentSummary) plus a Jobs section sourced from the
// component→executions join (§5, scan+filter over the run history). It holds no
// action seam itself — the run action is emitted as a message the root model
// dispatches against the selected environment (environments.md §1).

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/theme"
)

// ComponentPageModel renders one component's detail page and its recent
// executions. The execution list is the component→executions join (consumers.md
// §5): the runs whose plan touched this component, newest first.
type ComponentPageModel struct {
	Component *services.ComponentSummary
	// Env is the cockpit's currently selected environment, shown in the header
	// and used to decide whether the run action is offered (active-in-env).
	Env string

	runs   []services.RunSummary // already filtered to this component, newest first
	cursor int                   // index into runs
	Width  int
	Height int
}

// NewComponentPageModel constructs an empty component page.
func NewComponentPageModel() ComponentPageModel { return ComponentPageModel{} }

// SetComponent points the page at a component and its execution history. runs is
// the full run list; it is filtered to the component here so the caller does not
// have to. Resets the cursor.
func (m ComponentPageModel) SetComponent(c *services.ComponentSummary, runs []services.RunSummary) ComponentPageModel {
	m.Component = c
	m.cursor = 0
	m.runs = nil
	if c != nil {
		m.runs = runsForComponent(c.Name, runs, 20)
	}
	return m
}

// SetSize stores the page dimensions.
func (m ComponentPageModel) SetSize(w, h int) ComponentPageModel {
	m.Width = w
	m.Height = h
	return m
}

// SelectedRun returns the execution under the cursor (or nil).
func (m ComponentPageModel) SelectedRun() *services.RunSummary {
	if m.cursor < 0 || m.cursor >= len(m.runs) {
		return nil
	}
	r := m.runs[m.cursor]
	return &r
}

// ActiveInSelectedEnv reports whether the component is active in the page's
// selected environment — the gate on whether a run may be launched from here
// (environments.md §1). With no env selected it returns false.
func (m ComponentPageModel) ActiveInSelectedEnv() bool {
	if m.Component == nil || m.Env == "" {
		return false
	}
	for _, e := range m.Component.Envs {
		if e == m.Env {
			return true
		}
	}
	return false
}

func (m ComponentPageModel) Init() tea.Cmd { return nil }

// Update handles navigation + the action keys. `enter` drills into the selected
// execution (job→logs handoff); `r` requests a run; `g` opens the composer.
func (m ComponentPageModel) Update(msg tea.Msg) (ComponentPageModel, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch km.String() {
	case "down", "j":
		if m.cursor+1 < len(m.runs) {
			m.cursor++
		}
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "home", "g0":
		m.cursor = 0
	case "end", "G":
		if len(m.runs) > 0 {
			m.cursor = len(m.runs) - 1
		}
	case "enter":
		if r := m.SelectedRun(); r != nil {
			exec := r.ExecID
			return m, func() tea.Msg { return ComponentJobOpenMsg{ExecID: exec} }
		}
	case "r":
		if m.Component != nil {
			name := m.Component.Name
			return m, func() tea.Msg { return ComponentRunRequestedMsg{Name: name} }
		}
	case "g":
		if m.Component != nil {
			name := m.Component.Name
			return m, func() tea.Msg { return ComponentEnterMsg{Name: name} }
		}
	}
	return m, nil
}

// ComponentJobOpenMsg signals the user drilled into an execution from the
// component page. The root model hands off to the Activity run→job→logs
// drilldown focused on this execution.
type ComponentJobOpenMsg struct {
	ExecID string
}

// ComponentRunRequestedMsg asks the root model to run the named component,
// scoped to the cockpit's selected environment (environments.md §1). The root
// model owns the env + the action seam, so it validates active-in-env and
// dispatches the run.
type ComponentRunRequestedMsg struct {
	Name string
}

// View renders the component detail + the executions (Jobs) section.
func (m ComponentPageModel) View() string {
	width := m.Width
	if width <= 0 {
		width = 80
	}
	if m.Component == nil {
		return centerCard(width, m.Height, "no component selected")
	}
	c := m.Component

	var b strings.Builder

	// Title line: name + change badge.
	title := theme.StyleSectionTitle.Render(c.Name)
	switch c.ChangeKind {
	case "changed":
		title += "  " + theme.StyleChangedDot.Render("● changed")
	case "affected":
		title += "  " + theme.StyleChangedDot.Render("◌ affected")
	}
	b.WriteString(title + "\n")
	sub := zoa(c.Type)
	if c.Domain != "" {
		sub += " · " + c.Domain
	}
	b.WriteString(theme.StyleDim.Render(sub) + "\n\n")

	// Detail fields.
	detail := func(label, value string) {
		b.WriteString(theme.StyleLabel.Render(pad(label, 12)) + " " +
			theme.StyleValue.Render(zoa(value)) + "\n")
	}
	detail("path", c.Path)
	detail("envs", strings.Join(c.Envs, ", "))
	detail("profile", c.Profile)
	detail("depends-on", strings.Join(c.DependsOn, ", "))
	detail("watches", strings.Join(c.Watches, ", "))

	// Selected-env run affordance.
	b.WriteString("\n")
	if m.Env != "" {
		envLine := theme.StyleLabel.Render(pad("run env", 12)) + " " +
			theme.StyleChipAccent.Render(m.Env)
		if m.ActiveInSelectedEnv() {
			envLine += "  " + theme.StyleDim.Render("(r to run · e to change env)")
		} else {
			envLine += "  " + theme.StyleDim.Render("(not active in this env)")
		}
		b.WriteString(envLine + "\n")
	}

	// Executions (Jobs) section — the component→executions join.
	b.WriteString("\n")
	b.WriteString(theme.StyleSectionTitle.Render("Recent executions") +
		theme.StyleDim.Render(fmt.Sprintf("  ·  %d", len(m.runs))) + "\n\n")

	if len(m.runs) == 0 {
		b.WriteString(theme.StyleDim.Render("  no executions yet — press r to run this component"))
	} else {
		idW := clamp(width*16/100, 10, 14)
		statW := 12
		header := theme.StyleTableHeader.Render(fmt.Sprintf(" %s %s %s",
			pad("EXEC", idW), pad("STATUS", statW), "STARTED"))
		b.WriteString(" " + header + "\n")
		for i, r := range m.runs {
			exec := shortID(r.ExecID)
			line := fmt.Sprintf(" %s %s %s",
				pad(exec, idW), pad(runStatusPill(r.Status), statW+8), humanAgo(r.StartedAt))
			if i == m.cursor {
				b.WriteString(theme.StyleCursorBar.Render("▌") +
					theme.StyleTableRowSelected.Render(line))
			} else if i%2 == 1 {
				b.WriteString(" " + theme.StyleTableRowAlt.Render(line))
			} else {
				b.WriteString(" " + theme.StyleTableRow.Render(line))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(theme.StyleDim.Render(
		"↑↓ select · ⏎ open run · r run · g compose · esc back"))
	return b.String()
}

// runsForComponent returns up to `limit` runs that touched the named component,
// newest first. Mirrors the inspector's recentRunsForComponent: exact membership
// over RunSummary.Components when populated, else a PlanName substring fallback
// for legacy runs without per-run component metadata (consumers.md §5).
func runsForComponent(name string, runs []services.RunSummary, limit int) []services.RunSummary {
	if name == "" || limit <= 0 {
		return nil
	}
	out := make([]services.RunSummary, 0, limit)
	lname := strings.ToLower(name)
	for _, r := range runs {
		match := false
		if len(r.Components) > 0 {
			for _, comp := range r.Components {
				if comp == name {
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
