package design

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/sourceplane/orun/internal/tui2/frame"
)

// Box draws the one bordered container Northwind Mono allows on screen at a
// time: a muted rounded frame around content, sized to fit within max.
// Overlays (palette, help, dialogs) are Boxes; panes never are — panes
// separate by whitespace and rules (design §4a).
func Box(title string, content string, max frame.Size) string {
	innerW := 0
	lines := strings.Split(content, "\n")
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > innerW {
			innerW = w
		}
	}
	if tw := ansi.StringWidth(title) + 2; tw > innerW {
		innerW = tw
	}
	if innerW > max.Width-4 {
		innerW = max.Width - 4
	}
	if innerW < 1 {
		innerW = 1
	}
	maxLines := max.Height - 2
	if len(lines) > maxLines && maxLines > 0 {
		lines = lines[:maxLines]
	}

	var b strings.Builder
	if title != "" {
		pad := innerW - ansi.StringWidth(title) - 1
		if pad < 0 {
			pad = 0
		}
		b.WriteString(Muted.Render("╭─") + Title.Render(title) + Muted.Render(strings.Repeat("─", pad)+"─╮"))
	} else {
		b.WriteString(Muted.Render("╭" + strings.Repeat("─", innerW+2) + "╮"))
	}
	for _, l := range lines {
		b.WriteString("\n" + Muted.Render("│") + " " + frame.FitLine(l, innerW) + " " + Muted.Render("│"))
	}
	b.WriteString("\n" + Muted.Render("╰"+strings.Repeat("─", innerW+2)+"╯"))
	return b.String()
}

// Dialog renders the shared confirmation box. The primary action names its
// verb ("Run 4 jobs", never "OK" — design §4e); keys are enter / esc.
func Dialog(title string, body []string, primaryVerb string, max frame.Size) string {
	content := strings.Join(body, "\n") + "\n\n" +
		ToneInfo.Style().Render("enter") + " " + Text.Render(primaryVerb) +
		Dim.Render("   ·   ") + Dim.Render("esc cancel")
	return Box(title, content, max)
}

// Drawer renders the right-hand inspector panel at exactly size: a muted
// left rule, a title, then key/value pairs. It overlays the stage edge
// rather than reflowing it (design §4c).
func Drawer(size frame.Size, title string, kv [][2]string) string {
	if size.Empty() {
		return ""
	}
	keyW := 0
	for _, p := range kv {
		if len(p[0]) > keyW {
			keyW = len(p[0])
		}
	}
	var lines []string
	lines = append(lines, " "+Title.Render(title), "")
	for _, p := range kv {
		lines = append(lines, " "+Dim.Render(frame.FitLine(p[0], keyW))+"  "+Text.Render(p[1]))
	}
	body := strings.Split(frame.Fit(strings.Join(lines, "\n"), frame.Size{Width: size.Width - 2, Height: size.Height}), "\n")
	for i := range body {
		body[i] = Muted.Render("│") + " " + body[i]
	}
	return strings.Join(body, "\n")
}
