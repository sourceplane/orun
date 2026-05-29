// Package render turns cockpit view-models into painted lines for any
// surface. The functions here are the visual contract: change them and
// both `orun status` and the TUI's run pane update together.
package render

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/cockpit/style"
	"github.com/sourceplane/orun/internal/cockpit/surface"
	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
)

// Brand renders the cockpit wedge + name line, e.g. "▲ orun release".
func Brand(s surface.Surface, subtitle string) string {
	head := s.Style(style.TokenBrand, style.BrandWedge+" "+style.BrandName)
	if subtitle == "" {
		return head
	}
	return head + " " + s.Bold(subtitle)
}

// RunStatus paints the full status frame for a single execution. Returns
// the rendered lines (caller may flush to surface with WriteBlock or
// pre-process).
func RunStatus(s surface.Surface, v viewmodel.RunView) []string {
	if s.JSON() {
		return nil // caller uses surface.Emit on the raw view-model
	}

	var lines []string
	lines = append(lines, "")
	lines = append(lines, Brand(s, v.PlanName))

	sub := []string{}
	if v.PlanID != "" {
		sub = append(sub, "Plan: "+v.PlanID)
	}
	sub = append(sub, "Run: "+v.ExecID)
	sub = append(sub, "State: "+statusInline(s, v.Status))
	if dur := runDuration(v); dur != "" {
		sub = append(sub, "Duration: "+dur)
	}
	if v.DryRun {
		sub = append(sub, s.Style(style.TokenWarning, "dry-run"))
	}
	lines = append(lines, "  "+s.Dim(joinDim(s, sub)))

	scope := []string{}
	if n := len(v.Components); n > 0 {
		scope = append(scope, fmt.Sprintf("%d component%s", n, plural(n)))
	}
	if v.Counts.Total > 0 {
		scope = append(scope, fmt.Sprintf("%d job%s", v.Counts.Total, plural(v.Counts.Total)))
	}
	if len(scope) > 0 {
		lines = append(lines, "  "+s.Dim("Scope: "+strings.Join(scope, style.SepInline)))
	}
	lines = append(lines, "")

	if v.Counts.Total > 0 {
		lines = append(lines, "  "+s.Dim("Status:   ")+statusLegend(s, v.Counts))
		bar := progressBar(v.Counts.Percent(), 32)
		lines = append(lines, fmt.Sprintf("  %s%s %3d%%", s.Dim("Progress: "), bar, v.Counts.Percent()))
		lines = append(lines, "")
	}

	for _, link := range v.Links {
		label := link.Label
		if label == "" {
			label = "Link:"
		}
		lines = append(lines, fmt.Sprintf("  %s %s",
			s.Style(style.TokenSecondary, label),
			link.URL,
		))
	}
	if len(v.Links) > 0 {
		lines = append(lines, "")
	}

	if len(v.Groups) == 0 {
		lines = append(lines, fmt.Sprintf("  %s orun logs --exec-id %s",
			s.Dim("Logs:"), v.ExecID))
		return lines
	}

	lines = append(lines, renderGroups(s, v)...)
	return lines
}

func renderGroups(s surface.Surface, v viewmodel.RunView) []string {
	var out []string
	now := time.Now()
	for gi, g := range v.Groups {
		if gi > 0 {
			out = append(out, "  "+s.Dim(style.TreeVert))
		}
		out = append(out, fmt.Sprintf("  %s %s",
			s.Style(style.TokenBrand, style.GlyphBullet),
			s.Bold(g.Component),
		))
		for ji, j := range g.Jobs {
			connector := style.TreeBranch
			if ji == len(g.Jobs)-1 {
				connector = style.TreeLast
			}
			tok := style.StatusToken(j.Status)
			glyph := s.Style(tok, style.StatusGlyph(j.Status))
			label := j.Short
			if v.MultiEnv && j.Environment != "" {
				label = s.Dim(j.Environment+"/") + label
			}
			dur := ""
			if d := j.Duration(now); d > 0 {
				dur = "  " + s.Dim(shortDuration(d))
			}
			head := fmt.Sprintf("  %s  %s %s %s%s",
				s.Dim(style.TreeVert),
				s.Dim(connector),
				glyph,
				label,
				dur,
			)
			if strings.EqualFold(strings.TrimSpace(j.Status), "failed") && j.Error != "" {
				head += "  " + s.Style(style.TokenError, truncate(j.Error, 60))
			}
			out = append(out, head)
		}
	}
	return out
}

// RunList paints `orun get runs` / `orun status --all`.
func RunList(s surface.Surface, list viewmodel.RunListView) []string {
	if s.JSON() {
		return nil
	}
	if len(list.Runs) == 0 {
		return []string{
			s.Dim("No runs yet."),
			"",
			"  Start one with: " + s.Bold("orun run"),
		}
	}
	var out []string
	out = append(out, "")
	out = append(out, Brand(s, "runs"))
	out = append(out, "")

	// Header
	out = append(out, "  "+s.Dim(fmt.Sprintf("%-38s  %-10s  %-22s  %-20s  %s",
		"RUN", "STATE", "PLAN", "RESULT", "AGE")))
	for _, r := range list.Runs {
		tok := style.StatusToken(r.Status)
		glyph := s.Style(tok, style.StatusGlyph(r.Status))
		state := s.Style(tok, style.StatusLabel(r.Status))
		result := formatResult(s, r.Counts)
		out = append(out, fmt.Sprintf("  %s %-37s  %-10s  %-22s  %-20s  %s",
			glyph,
			truncate(r.ExecID, 37),
			padPlain(state, 10),
			truncate(r.PlanName, 22),
			padPlain(result, 20),
			s.Dim(formatAge(r.StartedAt)),
		))
	}
	return out
}

// --- helpers ---------------------------------------------------------

func statusInline(s surface.Surface, status string) string {
	tok := style.StatusToken(status)
	return s.Style(tok, style.StatusLabel(status))
}

func statusLegend(s surface.Surface, c viewmodel.Counts) string {
	parts := []string{
		fmt.Sprintf("%s %d succeeded", s.Style(style.TokenSuccess, style.GlyphSuccess), c.Completed),
		fmt.Sprintf("%s %d running", s.Style(style.TokenRunning, style.GlyphRunning), c.Running),
		fmt.Sprintf("%s %d queued", s.Style(style.TokenPending, style.GlyphPending), c.Pending),
	}
	if c.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%s %d failed",
			s.Style(style.TokenError, style.GlyphFailure), c.Failed))
	}
	return strings.Join(parts, style.SepInline)
}

func progressBar(pct, width int) string {
	if width < 2 {
		width = 24
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := pct * width / 100
	if filled > width {
		filled = width
	}
	return strings.Repeat("▓", filled) + strings.Repeat("░", width-filled)
}

func formatResult(s surface.Surface, c viewmodel.Counts) string {
	if c.Total == 0 {
		return s.Dim("—")
	}
	if c.Failed > 0 {
		return fmt.Sprintf("%d/%d  %s %d failed",
			c.Completed, c.Total,
			s.Style(style.TokenError, style.GlyphFailure), c.Failed)
	}
	return fmt.Sprintf("%d/%d", c.Completed, c.Total)
}

func joinDim(s surface.Surface, parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, s.Dim(style.SepInline))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}

// padPlain pads visible-width-aware-ish: when ANSI escapes are present we
// can't naively use %-Ns, so this helper does a best-effort visible pad.
func padPlain(s string, n int) string {
	visible := visibleLen(s)
	if visible >= n {
		return s
	}
	return s + strings.Repeat(" ", n-visible)
}

func visibleLen(s string) int {
	n := 0
	in := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\x1b' {
			in = true
			continue
		}
		if in {
			if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
				in = false
			}
			continue
		}
		n++
	}
	return n
}

func runDuration(v viewmodel.RunView) string {
	if v.StartedAt.IsZero() {
		return ""
	}
	end := v.FinishedAt
	if end.IsZero() {
		end = time.Now()
	}
	return shortDuration(end.Sub(v.StartedAt))
}

func shortDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) - m*60
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) - h*60
	return fmt.Sprintf("%dh%02dm", h, m)
}

func formatAge(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// SortedComponentNames returns component names sorted alphabetically.
// Exported because some callers (TUI) need it independently.
func SortedComponentNames(jobs []viewmodel.Job) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, j := range jobs {
		if j.Component == "" {
			continue
		}
		if _, ok := seen[j.Component]; ok {
			continue
		}
		seen[j.Component] = struct{}{}
		out = append(out, j.Component)
	}
	sort.Strings(out)
	return out
}
