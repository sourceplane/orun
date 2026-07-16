package activity

import (
	"fmt"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/tui2/design"
	"github.com/sourceplane/orun/internal/tui2/frame"
)

// View implements shell.Surface.
func (s *Surface) View(size frame.Size) string {
	var body string
	switch s.level {
	case levelFeed:
		body = s.viewFeed(size)
	case levelRun:
		body = s.viewRun(size)
	case levelJob:
		body = s.viewJob(size)
	default:
		body = s.viewLog(size)
	}
	return frame.Fit(body, size)
}

func (s *Surface) viewFeed(size frame.Size) string {
	var b strings.Builder
	b.WriteString("\n " + design.Title.Render("activity") + "   " + design.Chips(s.facet, facets...) + "\n\n")
	rows := s.filteredRuns()
	if len(rows) == 0 {
		b.WriteString("  " + design.Dim.Render("no runs yet") + "\n")
	}
	max := size.Height - 7
	for i, r := range rows {
		if i >= max {
			b.WriteString("  " + design.Dim.Render(fmt.Sprintf("… %d more", len(rows)-max)) + "\n")
			break
		}
		name := r.PlanName
		if name == "" {
			name = r.ExecID
		}
		secondary := design.Ref.Render(shortID(r.ExecID))
		if !r.StartedAt.IsZero() {
			secondary += " · " + ago(r.StartedAt)
		}
		right := design.StatusText(r.Status)
		if d := r.Duration(); d > 0 {
			right += design.Dim.Render("  " + d.Truncate(time.Second).String())
		}
		b.WriteString(design.DataRow(size.Width, i == s.selRun, name, secondary, right) + "\n")
	}
	b.WriteString("\n  " + design.KeyHint("enter", "open") + "   " + design.KeyHint("f", "filter") + "\n")
	return b.String()
}

func (s *Surface) runHeader(width int) string {
	name := s.run.PlanName
	if name == "" {
		name = s.pinned
	}
	left := " " + design.Title.Render(name) + "  " + design.Ref.Render(shortID(s.pinned))
	right := design.StatusText(s.run.Status)
	if s.run.Counts.Total > 0 {
		right += design.Dim.Render(fmt.Sprintf("  %d/%d jobs · %d%%",
			s.run.Counts.Completed, s.run.Counts.Total, s.run.Counts.Percent()))
	}
	gap := width - visWidth(left) - visWidth(right) - 1
	if gap < 1 {
		return frame.FitLine(left, width)
	}
	return left + strings.Repeat(" ", gap) + right + " "
}

func (s *Surface) viewRun(size frame.Size) string {
	var b strings.Builder
	b.WriteString("\n" + s.runHeader(size.Width) + "\n\n")
	if len(s.run.Jobs) == 0 {
		b.WriteString("  " + design.Dim.Render("loading run…") + "\n")
		return b.String()
	}
	for i, j := range s.run.Jobs {
		secondary := j.Environment
		if n := len(j.Steps); n > 0 {
			done := 0
			for _, st := range j.Steps {
				if strings.EqualFold(st.Status, "completed") {
					done++
				}
			}
			secondary += fmt.Sprintf(" · %d/%d steps", done, n)
		}
		right := design.StatusText(j.Status)
		if d := j.Duration(time.Now()); d > 0 {
			right += design.Dim.Render("  " + d.Truncate(time.Second).String())
		}
		b.WriteString(design.DataRow(size.Width, i == s.selJob, j.Component+" · "+j.Short, secondary, right) + "\n")
		if j.Error != "" && i == s.selJob {
			b.WriteString("     " + design.ToneError.Style().Render(design.Sanitize(firstLine(j.Error))) + "\n")
		}
	}
	b.WriteString("\n  " + design.KeyHint("enter", "steps") + "   " + design.KeyHint("esc", "back") + "\n")
	return b.String()
}

func (s *Surface) viewJob(size frame.Size) string {
	job := s.currentJob()
	var b strings.Builder
	b.WriteString("\n" + s.runHeader(size.Width) + "\n")
	if job == nil {
		return b.String()
	}
	b.WriteString(" " + design.Dim.Render("job ") + design.Text.Render(job.ID) + "\n\n")
	for i, st := range job.Steps {
		b.WriteString(design.DataRow(size.Width, i == s.selStep, st.ID, "", design.StatusText(st.Status)) + "\n")
	}
	b.WriteString("\n  " + design.KeyHint("enter", "log") + "   " + design.KeyHint("esc", "back") + "\n")
	return b.String()
}

func (s *Surface) viewLog(size frame.Size) string {
	var b strings.Builder
	title := " " + design.Dim.Render("log ") + design.Ref.Render(s.logKey)
	if s.errsOnly {
		title += design.ToneWarning.Style().Render("  errors only")
	}
	if s.currentStepRunning() {
		title += "  " + design.Pill(design.ToneLive, "● following")
	}
	b.WriteString("\n" + frame.FitLine(title, size.Width) + "\n\n")

	lines := s.logLines()
	viewH := size.Height - 5
	if viewH < 1 {
		viewH = 1
	}
	end := len(lines) - s.logScroll
	if end > len(lines) {
		end = len(lines)
	}
	if end < 0 {
		end = 0
	}
	start := end - viewH
	if start < 0 {
		start = 0
	}
	if s.logText == "" {
		b.WriteString("  " + design.Dim.Render("no output captured") + "\n")
	}
	for _, l := range lines[start:end] {
		b.WriteString(frame.FitLine("  "+styleLogLine(l), size.Width) + "\n")
	}
	return b.String()
}

// logLines sanitizes (trust boundary: step output is remote-ish data) and
// filters the log body.
func (s *Surface) logLines() []string {
	raw := strings.Split(design.Sanitize(s.logText), "\n")
	if !s.errsOnly {
		return raw
	}
	var out []string
	for _, l := range raw {
		low := strings.ToLower(l)
		if strings.Contains(low, "error") || strings.Contains(low, "fail") || strings.Contains(low, "fatal") {
			out = append(out, l)
		}
	}
	return out
}

func styleLogLine(l string) string {
	low := strings.ToLower(l)
	switch {
	case strings.Contains(low, "error") || strings.Contains(low, "fatal"):
		return design.ToneError.Style().Render(l)
	case strings.Contains(low, "warn") || strings.Contains(low, "retry"):
		return design.ToneWarning.Style().Render(l)
	default:
		return design.Dim.Render(l)
	}
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func visWidth(s string) int { return frame.LineWidth(s) }
