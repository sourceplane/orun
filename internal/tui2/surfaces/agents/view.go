package agents

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/sourceplane/orun/internal/agent/live"
	"github.com/sourceplane/orun/internal/tui2/agentfold"
	"github.com/sourceplane/orun/internal/tui2/design"
	"github.com/sourceplane/orun/internal/tui2/frame"
)

// View implements shell.Surface.
func (s *Surface) View(size frame.Size) string {
	if s.client != nil && s.conv != nil {
		return frame.Fit(s.viewConversation(size), size)
	}
	return frame.Fit(s.viewList(size), size)
}

// viewList renders the sessions list — the local lane of the console's
// Agents workbench.
func (s *Surface) viewList(size frame.Size) string {
	var b strings.Builder
	b.WriteString("\n " + design.Title.Render("live sessions") + "\n\n")
	if len(s.sessions) == 0 {
		b.WriteString("  " + design.Dim.Render("no live sessions — n starts one") + "\n")
	}
	for i, e := range s.sessions {
		b.WriteString(sessionRow(size.Width, i == s.sel, e) + "\n")
	}
	b.WriteString("\n  " + design.KeyHint("enter", "attach") + "   " + design.KeyHint("n", "new session") + "\n")
	if s.status != "" {
		b.WriteString("\n  " + design.Dim.Render(s.status) + "\n")
	}
	return b.String()
}

func sessionRow(width int, selected bool, e live.Entry) string {
	primary := e.SessionID
	if e.AgentType != "" {
		primary = e.AgentType
	}
	secondary := e.Task
	if secondary == "" {
		secondary = "interactive"
	}
	if e.Driver != "" {
		secondary += " · " + e.Driver
	}
	right := design.StatusText(e.State)
	if !e.StartedAt.IsZero() {
		right += design.Dim.Render("  " + since(e.StartedAt))
	}
	return design.DataRow(width, selected, primary, secondary, right)
}

// viewConversation renders the head: header, transcript (bottom-anchored),
// streaming line, sticky approval cards, composer, hints.
func (s *Surface) viewConversation(size frame.Size) string {
	width := size.Width
	c := s.conv

	header := " " + design.Title.Render(s.attachedID)
	if e := s.entryFor(s.attachedID); e != nil && e.AgentType != "" {
		header += design.Dim.Render("  "+e.AgentType) + design.Dim.Render(" · "+orDash(e.Task))
	}
	state := c.State
	if state == "" {
		state = "running"
	}
	headerRight := design.StatusText(state)
	if c.Tokens != "" {
		headerRight += design.Dim.Render("  " + c.Tokens)
	}
	head := joinEnds(header, headerRight+" ", width)

	// Fixed-height bottom chrome: approvals + composer + hints.
	var bottom []string
	for _, a := range c.Pending {
		card := design.ToneWarning.Style().Render("● approval needed  ") +
			design.Text.Render(a.Tool) +
			design.Dim.Render("   ctrl+y approve · ctrl+n deny")
		bottom = append(bottom, frame.FitLine(" "+card, width))
	}
	composer := " " + s.composer.View()
	bottom = append(bottom, frame.FitLine(composer, width))
	hint := design.Dim.Render(" enter steer · esc interrupt · ctrl+d detach · ctrl+o tools")
	if s.status != "" {
		hint = design.Dim.Render(" " + s.status)
	}
	bottom = append(bottom, frame.FitLine(hint, width))

	// Transcript fills the rest, bottom-anchored with scroll offset.
	bodyH := size.Height - 2 - len(bottom) // header + blank
	if bodyH < 1 {
		bodyH = 1
	}
	lines := s.transcriptLines(width)
	total := len(lines)
	end := total - s.scroll
	if end > total {
		end = total
	}
	if end < 0 {
		end = 0
	}
	start := end - bodyH
	if start < 0 {
		start = 0
	}
	body := lines[start:end]

	var b strings.Builder
	b.WriteString(head + "\n\n")
	for i := 0; i < bodyH-len(body); i++ {
		b.WriteString("\n")
	}
	for _, l := range body {
		b.WriteString(frame.FitLine(l, width) + "\n")
	}
	b.WriteString(strings.Join(bottom, "\n"))
	return b.String()
}

// transcriptLines renders folded items as wrapped, styled lines.
func (s *Surface) transcriptLines(width int) []string {
	c := s.conv
	inner := width - 4
	if inner < 16 {
		inner = width
	}
	var out []string
	for _, it := range c.Items {
		switch it.Kind {
		case agentfold.ItemAgent:
			out = append(out, "")
			out = append(out, prefixWrap("  ", design.Markdown(it.Text, inner), width)...)
		case agentfold.ItemUser:
			out = append(out, "")
			who := it.Principal
			if who == "" {
				who = "you"
			}
			out = append(out, frame.FitLine("  "+design.Selected.Render("› ")+design.Text.Render(design.Sanitize(it.Text))+design.Dim.Render("  — "+who), width))
		case agentfold.ItemTool:
			marker := "▸"
			if s.expandTools {
				marker = "▾"
			}
			out = append(out, frame.FitLine("  "+design.Dim.Render(marker+" tool ")+design.Ref.Render(design.Sanitize(it.Text)), width))
			if s.expandTools && it.Detail != "" {
				for _, dl := range strings.Split(design.Sanitize(it.Detail), "\n") {
					out = append(out, frame.FitLine("      "+design.Dim.Render(dl), width))
				}
			}
		case agentfold.ItemNote:
			out = append(out, frame.FitLine("  "+design.Dim.Render("· "+design.Sanitize(it.Text)), width))
		}
	}
	if c.Streaming != "" {
		out = append(out, "")
		out = append(out, prefixWrap("  ", design.Text.Render(design.Sanitize(c.Streaming))+design.Muted.Render("▏"), width)...)
	}
	return out
}

func prefixWrap(prefix, block string, width int) []string {
	var out []string
	for _, l := range strings.Split(block, "\n") {
		out = append(out, frame.FitLine(prefix+l, width))
	}
	return out
}

func (s *Surface) entryFor(id string) *live.Entry {
	for i := range s.sessions {
		if s.sessions[i].SessionID == id {
			return &s.sessions[i]
		}
	}
	return nil
}

func joinEnds(left, right string, width int) string {
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return frame.FitLine(left, width)
	}
	return left + strings.Repeat(" ", gap) + right
}

func since(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
