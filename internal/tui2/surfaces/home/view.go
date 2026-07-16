package home

import (
	"fmt"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/tui2/design"
	"github.com/sourceplane/orun/internal/tui2/frame"
)

// View implements shell.Surface.
func (s *Surface) View(size frame.Size) string {
	var b strings.Builder

	// Stat tiles.
	comp, compHint, sess, sessHint, last, lastHint := s.stats()
	tileW := (size.Width - 4) / 3
	tiles := []string{
		design.StatTile(tileW, "Components", comp, compHint),
		design.StatTile(tileW, "Live sessions", sess, sessHint),
		design.StatTile(tileW, "Last run", last, lastHint),
	}
	rows := make([]string, 3)
	for _, tile := range tiles {
		for i, l := range strings.Split(tile, "\n") {
			rows[i] += " " + l
		}
	}
	b.WriteString("\n")
	for _, r := range rows {
		b.WriteString(frame.FitLine(r, size.Width) + "\n")
	}

	// Needs attention.
	items := s.attentionItems()
	b.WriteString("\n " + design.Title.Render("needs attention") + "\n")
	if len(items) == 0 {
		b.WriteString("  " + design.Dim.Render("nothing needs you") + "\n")
	}
	for i, it := range items {
		right := design.Dim.Render("enter opens " + it.surface)
		b.WriteString(design.DataRow(size.Width, i == s.sel, it.label, it.detail, right) + "\n")
	}

	// Latest activity.
	b.WriteString("\n " + design.Title.Render("latest activity") + "\n")
	if len(s.runs.Runs) == 0 {
		b.WriteString("  " + design.Dim.Render("no runs yet") + "\n")
	}
	shown := 0
	for _, r := range s.runs.Runs {
		if shown >= 5 || shown >= size.Height-14 {
			break
		}
		secondary := r.ExecID
		if !r.StartedAt.IsZero() {
			secondary += " · " + ago(r.StartedAt)
		}
		b.WriteString(design.DataRow(size.Width, false, orDash(r.PlanName), secondary, design.StatusText(r.Status)) + "\n")
		shown++
	}

	return frame.Fit(b.String(), size)
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
