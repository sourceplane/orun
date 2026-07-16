package design

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Sanitize strips terminal control bytes from remote-originated text.
// This is the trust boundary of design §10: work items, session text, and
// docs are data, and data does not get to talk to the terminal. ANSI
// sequences are removed wholesale; of the control range only \n and \t
// survive (\t as two spaces).
func Sanitize(s string) string {
	s = ansi.Strip(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteRune('\n')
		case r == '\t':
			b.WriteString("  ")
		case r < 0x20 || r == 0x7f:
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Markdown renders markdown-lite at width: headings emphasized, bullets
// normalized, inline `code` toned as refs, fenced code indented, quotes
// dimmed. Input is Sanitized first — every Markdown call site is by
// definition rendering foreign text.
func Markdown(src string, width int) string {
	src = Sanitize(src)
	var out []string
	inFence := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			inFence = !inFence
			continue
		case inFence:
			out = append(out, wrap("    "+Ref.Render(line), width)...)
		case strings.HasPrefix(trimmed, "### "), strings.HasPrefix(trimmed, "## "), strings.HasPrefix(trimmed, "# "):
			text := strings.TrimLeft(trimmed, "# ")
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, Title.Render(text))
		case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "):
			out = append(out, wrap("  "+Dim.Render("·")+" "+inline(trimmed[2:]), width)...)
		case strings.HasPrefix(trimmed, "> "):
			out = append(out, wrap("  "+Dim.Render(trimmed[2:]), width)...)
		case trimmed == "":
			out = append(out, "")
		default:
			out = append(out, wrap(inline(line), width)...)
		}
	}
	return strings.Join(out, "\n")
}

// inline tones `code` spans.
func inline(s string) string {
	parts := strings.Split(s, "`")
	if len(parts) < 3 {
		return s
	}
	var b strings.Builder
	for i, p := range parts {
		if i%2 == 1 && i < len(parts)-(len(parts)%2) {
			b.WriteString(Ref.Render(p))
		} else {
			b.WriteString(p)
		}
	}
	return b.String()
}

// wrap word-wraps a styled line to width, preserving a hanging indent of
// two cells for continuation lines.
func wrap(line string, width int) []string {
	if width < 8 || ansi.StringWidth(line) <= width {
		return []string{line}
	}
	words := strings.Fields(line)
	var out []string
	cur := ""
	for _, w := range words {
		cand := cur
		if cand != "" {
			cand += " "
		}
		cand += w
		if cur != "" && ansi.StringWidth(cand) > width {
			out = append(out, cur)
			cur = "  " + w
			continue
		}
		cur = cand
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
