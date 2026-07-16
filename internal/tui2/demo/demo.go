// Package demo provides the TR0 stub surfaces: enough content to exercise
// every kernel seam — drilldown, focused input, animation, commands —
// without any data plane. TR2 replaces these with real surfaces; the demo
// stays behind the bench and the property tests, which need deterministic
// surfaces forever.
package demo

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui2/frame"
	"github.com/sourceplane/orun/internal/tui2/shell"
)

// New returns the demo surface set in tab order.
func New() []shell.Surface {
	return []shell.Surface{
		&homeSurface{},
		&agentsSurface{sessions: []string{"implementer · fix flaky catalog test", "reviewer · PR #482", "interactive · scratch"}},
		&activitySurface{runs: []string{"deploy checkout · succeeded", "plan payments · succeeded", "deploy web · failed"}, progress: -1},
	}
}

// --- Home -----------------------------------------------------------------

type homeSurface struct{}

func (h *homeSurface) ID() string                           { return "home" }
func (h *homeSurface) Title() string                        { return "Home" }
func (h *homeSurface) Init() tea.Cmd                        { return nil }
func (h *homeSurface) Update(tea.Msg) tea.Cmd               { return nil }
func (h *homeSurface) HandleKey(tea.KeyMsg) (tea.Cmd, bool) { return nil, false }
func (h *homeSurface) Pop() bool                            { return false }
func (h *homeSurface) InputFocused() bool                   { return false }
func (h *homeSurface) Rev() string                          { return "0" }

func (h *homeSurface) View(size frame.Size) string {
	content := strings.Join([]string{
		"",
		"  cockpit v2 kernel demo (TR0)",
		"",
		"  1–3 switch surfaces · : palette · ? help · esc back",
		"",
		"  Agents: enter attaches (composer takes the keyboard)",
		"  Activity: r starts a fake run (the only motion here)",
	}, "\n")
	return frame.Fit(content, size)
}

// --- Agents ---------------------------------------------------------------

// agentsSurface exercises drilldown and the focused-input rule: attached, a
// composer owns the keyboard and unchorded printables must reach it.
type agentsSurface struct {
	sessions []string
	sel      int
	attached bool
	composer string
	rev      int
}

func (a *agentsSurface) ID() string             { return "agents" }
func (a *agentsSurface) Title() string          { return "Agents" }
func (a *agentsSurface) Init() tea.Cmd          { return nil }
func (a *agentsSurface) Update(tea.Msg) tea.Cmd { return nil }
func (a *agentsSurface) InputFocused() bool     { return a.attached }
func (a *agentsSurface) Rev() string            { return strconv.Itoa(a.rev) }

func (a *agentsSurface) Pop() bool {
	if a.attached {
		a.attached = false
		a.rev++
		return true
	}
	return false
}

func (a *agentsSurface) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	a.rev++
	if a.attached {
		switch msg.String() {
		case "esc":
			a.attached = false
		case "backspace":
			if a.composer != "" {
				a.composer = a.composer[:len(a.composer)-1]
			}
		case "enter":
			a.composer = ""
		default:
			if msg.Type == tea.KeyRunes && !msg.Alt {
				a.composer += string(msg.Runes)
			}
		}
		return nil, true
	}
	switch msg.String() {
	case "up", "k":
		if a.sel > 0 {
			a.sel--
		}
		return nil, true
	case "down", "j":
		if a.sel < len(a.sessions)-1 {
			a.sel++
		}
		return nil, true
	case "enter":
		a.attached = true
		return nil, true
	}
	a.rev-- // unconsumed: view unchanged
	return nil, false
}

func (a *agentsSurface) View(size frame.Size) string {
	var b strings.Builder
	if a.attached {
		fmt.Fprintf(&b, "\n  session: %s\n\n", a.sessions[a.sel])
		b.WriteString("  (streaming transcript arrives in TR3)\n")
		for i := 0; i < size.Height-7; i++ {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "  › %s▏", a.composer)
	} else {
		b.WriteString("\n  live sessions\n\n")
		for i, s := range a.sessions {
			marker := "   "
			if i == a.sel {
				marker = " ▸ "
			}
			fmt.Fprintf(&b, "%s%s\n", marker, s)
		}
	}
	return frame.Fit(b.String(), size)
}

// --- Activity ---------------------------------------------------------------

// activitySurface exercises the animation scheduler: r starts a fake run
// that advances on scheduler ticks and deregisters itself when done.
type activitySurface struct {
	runs     []string
	sched    *frame.Scheduler
	progress int // -1 idle, 0..100 running
	rev      int
}

const runAnimID = "demo.activity.run"

func (a *activitySurface) ID() string         { return "activity" }
func (a *activitySurface) Title() string      { return "Activity" }
func (a *activitySurface) Init() tea.Cmd      { return nil }
func (a *activitySurface) Pop() bool          { return false }
func (a *activitySurface) InputFocused() bool { return false }
func (a *activitySurface) Rev() string        { return strconv.Itoa(a.rev) }

func (a *activitySurface) SetScheduler(s *frame.Scheduler) { a.sched = s }

func (a *activitySurface) Update(msg tea.Msg) tea.Cmd {
	if _, ok := msg.(frame.TickMsg); ok && a.progress >= 0 {
		a.progress += 4
		a.rev++
		if a.progress >= 100 {
			a.progress = -1
			a.runs = append([]string{"demo run · succeeded"}, a.runs...)
			if a.sched != nil {
				a.sched.Remove(runAnimID)
			}
		}
	}
	return nil
}

func (a *activitySurface) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if msg.String() == "r" && a.progress < 0 {
		a.progress = 0
		a.rev++
		if a.sched != nil {
			a.sched.Add(runAnimID)
			return a.sched.Arm(), true
		}
		return nil, true
	}
	return nil, false
}

func (a *activitySurface) View(size frame.Size) string {
	var b strings.Builder
	b.WriteString("\n  runs\n\n")
	if a.progress >= 0 {
		width := 30
		filled := a.progress * width / 100
		fmt.Fprintf(&b, "   ⣿ demo run  %s%s %d%%\n", strings.Repeat("█", filled), strings.Repeat("░", width-filled), a.progress)
	}
	for _, r := range a.runs {
		fmt.Fprintf(&b, "   %s\n", r)
	}
	b.WriteString("\n   r starts a fake run\n")
	return frame.Fit(b.String(), size)
}
