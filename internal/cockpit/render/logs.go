package render

import (
	"fmt"
	"strings"

	"github.com/sourceplane/orun/internal/cockpit/style"
	"github.com/sourceplane/orun/internal/cockpit/surface"
	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
)

// LogsOptions tunes log rendering. Raw=true preserves every line;
// otherwise entries with >MaxLines are truncated with a "… N more" hint.
type LogsOptions struct {
	Raw      bool
	MaxLines int // default 8 when Raw=false
}

// RunLogs renders a cockpit LogsView using the same brand/scope header as
// RunStatus, then prints grouped log blocks. JSON surfaces should consume
// the raw view-model via surface.Emit instead of calling this.
func RunLogs(s surface.Surface, v viewmodel.LogsView, opts LogsOptions) []string {
	if s.JSON() {
		return nil
	}
	if opts.MaxLines <= 0 {
		opts.MaxLines = 8
	}

	var out []string
	out = append(out, "")
	out = append(out, Brand(s, v.Run.PlanName))

	sub := []string{"Run: " + v.ExecID}
	if v.Run.Status != "" {
		sub = append(sub, "State: "+statusInline(s, v.Run.Status))
	}
	if dur := runDuration(v.Run); dur != "" {
		sub = append(sub, "Duration: "+dur)
	}
	out = append(out, "  "+s.Dim(joinDim(s, sub)))

	if v.Run.Counts.Total > 0 {
		out = append(out, "  "+s.Dim("Status:   ")+statusLegend(s, v.Run.Counts))
	}
	for _, link := range v.Run.Links {
		label := link.Label
		if label == "" {
			label = "Link:"
		}
		out = append(out, fmt.Sprintf("  %s %s",
			s.Style(style.TokenSecondary, label), link.URL))
	}
	out = append(out, "")

	if len(v.Entries) == 0 {
		out = append(out, "  "+s.Dim("No logs captured for this run."))
		return out
	}

	currentGroup := ""
	for _, e := range v.Entries {
		group := e.Component
		if e.Environment != "" {
			group = e.Component + "  " + s.Dim(style.SepDot+"  "+e.Environment)
		}
		if group != "" && group != currentGroup {
			if currentGroup != "" {
				out = append(out, "")
			}
			out = append(out, "  "+s.Style(style.TokenBrand, style.GlyphBullet)+" "+s.Bold(group))
			currentGroup = group
		}

		tok := style.StatusToken(e.Status)
		label := e.Short
		if label == "" {
			label = e.JobID
		}
		if e.StepID != "" {
			label += "  " + s.Dim(e.StepID)
		}
		out = append(out, fmt.Sprintf("    %s %s",
			s.Style(tok, style.StatusGlyph(e.Status)),
			s.Bold(label)))

		lines := e.Lines
		if !opts.Raw && len(lines) > opts.MaxLines {
			lines = lines[:opts.MaxLines]
		}
		for _, line := range lines {
			out = append(out, "       "+line)
		}
		if !opts.Raw {
			remaining := e.TotalLines - len(lines)
			if remaining > 0 {
				out = append(out, "       "+s.Dim(
					fmt.Sprintf("… %d more line%s", remaining, plural(remaining))))
			}
		}
		// Failure footer — surface the last few lines in error tone.
		if strings.EqualFold(strings.TrimSpace(e.Status), "failed") && len(e.Lines) > 0 {
			tail := e.Lines[len(e.Lines)-1]
			if !strings.Contains(strings.Join(lines, "\n"), tail) {
				out = append(out, "       "+s.Style(style.TokenError, tail))
			}
		}
	}
	return out
}
