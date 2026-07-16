package work

import (
	"fmt"
	"strings"

	"github.com/sourceplane/orun/internal/tui2/design"
	"github.com/sourceplane/orun/internal/tui2/frame"
)

// View implements shell.Surface.
func (s *Surface) View(size frame.Size) string {
	var body string
	switch s.level {
	case levelList:
		body = s.viewList(size)
	case levelEpic:
		body = s.viewEpic(size)
	default:
		body = s.viewBrief(size)
	}
	return frame.Fit(body, size)
}

// sealedBanner is the honest state of the offline lane (Q2).
func sealedBanner(at string) string {
	label := "sealed snapshot"
	if at != "" {
		label += " · approved " + at
	}
	return design.Pill(design.ToneInfo, "◈ "+label) + design.Dim.Render("  — the live work plane lights up when signed in")
}

func (s *Surface) viewList(size frame.Size) string {
	var b strings.Builder
	b.WriteString("\n " + design.Title.Render("work") + "\n\n")
	if len(s.epics) == 0 {
		b.WriteString("  " + design.Dim.Render("no sealed epics — `orun epic pull <slug>` fetches one; sign in for the live plane") + "\n")
	}
	for i, e := range s.epics {
		snap := e.Snapshot
		secondary := fmt.Sprintf("%s · %d milestones · %d tasks", e.Slug, len(snap.Milestones), len(snap.Tasks))
		right := design.Pill(design.ToneInfo, "◈ sealed")
		if snap.Approval.By.ID != "" {
			right += design.Dim.Render("  by " + snap.Approval.By.ID)
		}
		b.WriteString(design.DataRow(size.Width, i == s.sel, design.Sanitize(snap.Spec.Title), design.Sanitize(secondary), right) + "\n")
	}
	b.WriteString("\n  " + design.KeyHint("enter", "open") + "\n")
	return b.String()
}

func (s *Surface) viewEpic(size frame.Size) string {
	e := s.current()
	if e == nil {
		return ""
	}
	snap := e.Snapshot
	var b strings.Builder
	b.WriteString("\n " + design.Ref.Render(snap.Spec.Key) + " " + design.Title.Render(design.Sanitize(snap.Spec.Title)) + "\n")
	b.WriteString(" " + sealedBanner(snap.Approval.At) + "\n\n")

	// The milestone ladder with its tasks, session links joined in.
	taskIdx := 0
	for _, m := range snap.Milestones {
		b.WriteString(" " + design.Title.Render(m.Key+" — "+design.Sanitize(m.Title)) + "\n")
		if m.Goal != "" {
			b.WriteString("   " + design.Dim.Render(design.Sanitize(m.Goal)) + "\n")
		}
		for _, dw := range m.DoneWhen {
			b.WriteString("   " + design.Dim.Render("✓? "+design.Sanitize(dw)) + "\n")
		}
		for _, task := range snap.Tasks {
			if task.Milestone != m.Key {
				continue
			}
			right := ""
			if sess := s.sessionFor(task.Key); sess != nil {
				right = design.Pill(design.ToneLive, "● "+sess.AgentType+" working") + design.Dim.Render("  2 opens Agents")
			}
			b.WriteString(design.DataRow(size.Width, s.taskAt(taskIdx, task.Key), design.Sanitize(task.Title), task.Key, right) + "\n")
			taskIdx++
		}
		b.WriteString("\n")
	}
	// Unfiled tasks (no milestone).
	for _, task := range snap.Tasks {
		if task.Milestone != "" {
			continue
		}
		b.WriteString(design.DataRow(size.Width, s.taskAt(taskIdx, task.Key), design.Sanitize(task.Title), task.Key, "") + "\n")
		taskIdx++
	}

	hints := "  " + design.KeyHint("esc", "back")
	if e.Brief != "" {
		hints = "  " + design.KeyHint("b", "brief") + "   " + hints
	}
	b.WriteString(hints + "\n")
	return b.String()
}

// taskAt reports whether the flattened task index is selected.
func (s *Surface) taskAt(idx int, _ string) bool { return idx == s.selTask }

func (s *Surface) viewBrief(size frame.Size) string {
	e := s.current()
	if e == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n " + design.Ref.Render(e.Snapshot.Spec.Key) + " " + design.Title.Render("brief") + "  " + sealedBanner(e.Snapshot.Approval.At) + "\n\n")
	lines := strings.Split(design.Markdown(e.Brief, size.Width-4), "\n")
	if s.scroll > len(lines)-1 {
		s.scroll = max(0, len(lines)-1)
	}
	viewH := size.Height - 4
	end := min(len(lines), s.scroll+viewH)
	for _, l := range lines[s.scroll:end] {
		b.WriteString(frame.FitLine("  "+l, size.Width) + "\n")
	}
	return b.String()
}
