package catalog

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
	case levelDetail:
		body = s.viewDetail(size)
	default:
		body = s.viewCompose(size)
	}
	return frame.Fit(body, size)
}

func (s *Surface) viewList(size frame.Size) string {
	var b strings.Builder
	title := " " + design.Title.Render("catalog")
	if s.changedOnly {
		title += "   " + design.ToneWarning.Style().Render("changed only")
	}
	b.WriteString("\n" + title + "\n\n")
	rows := s.rows()
	if len(rows) == 0 {
		b.WriteString("  " + design.Dim.Render("no components") + "\n")
	}
	maxRows := size.Height - 7
	for i, r := range rows {
		if i >= maxRows {
			b.WriteString("  " + design.Dim.Render(fmt.Sprintf("… %d more", len(rows)-maxRows)) + "\n")
			break
		}
		secondary := r.Type
		if r.Domain != "" {
			secondary += " · " + r.Domain
		}
		if len(r.Envs) > 0 {
			secondary += " · " + strings.Join(r.Envs, " ")
		}
		right := ""
		switch r.Badge() {
		case "changed":
			right = design.ToneWarning.Style().Render("● changed")
		case "affected":
			right = design.ToneInfo.Style().Render("◐ affected")
		}
		b.WriteString(design.DataRow(size.Width, i == s.sel, r.Name, secondary, right) + "\n")
	}
	hints := "  " + design.KeyHint("enter", "detail") + "   " + design.KeyHint("c", "changed")
	if s.comp != nil {
		hints += "   " + design.KeyHint("g", "compose")
	}
	b.WriteString("\n" + hints + "\n")
	return b.String()
}

func (s *Surface) viewDetail(size frame.Size) string {
	d := s.detail
	var b strings.Builder
	b.WriteString("\n " + design.Dim.Render(design.Sanitize(d.Type)) + " " + design.Title.Render(design.Sanitize(d.Name)) + "\n\n")

	kv := [][2]string{
		{"key", d.Key},
		{"domain", orDash(d.Domain)},
		{"path", orDash(d.Path)},
		{"repo", orDash(d.Repo)},
	}
	for _, p := range kv {
		b.WriteString("   " + design.Dim.Render(frame.FitLine(p[0], 10)) + design.Text.Render(design.Sanitize(p[1])) + "\n")
	}

	if len(d.Envs) > 0 {
		b.WriteString("\n " + design.Title.Render("environments") + "\n")
		for _, e := range d.Envs {
			mark := design.Muted.Render("○")
			if e.Active {
				mark = design.ToneSuccess.Style().Render("●")
			}
			line := "   " + mark + " " + design.Text.Render(e.Name)
			if e.Profile != "" {
				line += design.Dim.Render("  " + e.Profile)
			}
			b.WriteString(line + "\n")
		}
	}
	if len(d.DependsOn) > 0 {
		b.WriteString("\n " + design.Title.Render("depends on") + "\n")
		for _, dep := range d.DependsOn {
			b.WriteString("   " + design.Dim.Render("→ ") + design.Ref.Render(dep) + "\n")
		}
	}
	if len(d.Watches) > 0 {
		b.WriteString("\n " + design.Title.Render("watches") + "\n")
		for _, w := range d.Watches {
			b.WriteString("   " + design.Dim.Render(design.Sanitize(w)) + "\n")
		}
	}
	if s.comp != nil {
		b.WriteString("\n  " + design.KeyHint("g", "compose") + "   " + design.KeyHint("esc", "back") + "\n")
	}
	return b.String()
}

func (s *Surface) viewCompose(size frame.Size) string {
	var b strings.Builder
	env := "all envs"
	if s.envIdx > 0 && s.envIdx < len(s.envs) {
		env = s.envs[s.envIdx]
	}
	head := " " + design.Title.Render("compose") + "  " + design.Text.Render(s.composeKey) +
		design.Dim.Render("  env:") + design.Ref.Render(env)
	if s.changedOnly {
		head += design.ToneWarning.Style().Render("  changed-only")
	}
	b.WriteString("\n" + frame.FitLine(head, size.Width) + "\n\n")

	switch {
	case s.generating:
		b.WriteString("  " + design.Dim.Render("generating plan…") + "\n")
	case s.planErr != "":
		b.WriteString("  " + design.ToneError.Style().Render(design.Sanitize(firstLine(s.planErr))) + "\n")
	case s.preview.Plan != nil:
		b.WriteString(" " + design.StatTile((size.Width-3)/3, "Jobs", fmt.Sprintf("%d", s.preview.JobCount), "plan "+s.preview.Checksum) + "\n")
		b.WriteString("\n " + design.Title.Render("components") + "\n")
		for _, c := range s.preview.Components {
			b.WriteString("   " + design.Dim.Render("◆ ") + design.Text.Render(c) + "\n")
		}
		for _, w := range s.preview.Warnings {
			b.WriteString("   " + design.ToneWarning.Style().Render("⚠ "+design.Sanitize(w)) + "\n")
		}
	}

	if len(s.progress) > 0 {
		b.WriteString("\n " + design.Title.Render("dispatch") + "\n")
		for _, l := range s.progress {
			b.WriteString("   " + design.Dim.Render(design.Sanitize(l)) + "\n")
		}
		if s.execID != "" {
			b.WriteString("   " + design.Dim.Render("run ") + design.Ref.Render(s.execID) + design.Dim.Render(" — 3 opens Activity") + "\n")
		}
	}

	hints := "  " + design.KeyHint("e", "env") + "   " + design.KeyHint("c", "changed") +
		"   " + design.KeyHint("d", "dry-run") + "   " + design.KeyHint("R", "run")
	if s.dispatching {
		hints = "  " + design.Pill(design.ToneLive, "● dispatching")
	}
	b.WriteString("\n" + hints + "\n")
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
