package views

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sourceplane/orun/internal/tui/services"

	"github.com/sourceplane/orun/internal/tui/theme"
)

// InspectorModel renders the structured detail drawer on the right.
type InspectorModel struct {
	Desc    *services.ResourceDescription
	Focused bool
	Width   int
	Height  int
}

func NewInspectorModel() InspectorModel { return InspectorModel{} }

// SetDescription installs a new description (used by the root model when
// selection changes).
func (m InspectorModel) SetDescription(d *services.ResourceDescription) InspectorModel {
	m.Desc = d
	return m
}

func (m InspectorModel) Init() tea.Cmd                              { return nil }
func (m InspectorModel) Update(_ tea.Msg) (InspectorModel, tea.Cmd) { return m, nil }

func (m InspectorModel) View() string {
	width := m.Width
	if width <= 0 {
		width = 36
	}
	contentW := width - 4
	if contentW < 8 {
		contentW = 8
	}

	if m.Desc == nil {
		var b strings.Builder
		b.WriteString(theme.StyleSectionTitle.Render("INSPECTOR"))
		b.WriteString("\n\n")
		b.WriteString(theme.StyleDim.Render("Select a row to view details."))
		b.WriteString("\n\n")
		b.WriteString(theme.StyleDim.Render("·  ↑↓ navigate"))
		b.WriteString("\n")
		b.WriteString(theme.StyleDim.Render("·  i hide drawer"))
		return lipgloss.NewStyle().Width(contentW).Render(b.String())
	}

	var b strings.Builder

	// --- Header: kind chip + name -------------------------------------
	kind := strings.ToUpper(m.Desc.Kind)
	if kind == "" {
		kind = "DETAIL"
	}
	b.WriteString(theme.StyleSectionTitle.Render(kind))
	b.WriteString("\n")
	if name := strings.TrimSpace(m.Desc.Name); name != "" {
		b.WriteString(theme.StyleTitle.Render(truncate(name, contentW)))
		b.WriteString("\n")
	}

	// Optional status pill (when a field labelled "status" is present).
	if pill := inspectorStatusPill(m.Desc); pill != "" {
		b.WriteString(pill)
		b.WriteString("\n")
	}

	if s := strings.TrimSpace(m.Desc.Summary); s != "" {
		b.WriteString(theme.StyleDim.Render(truncate(s, contentW)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(theme.StyleMuted.Render(strings.Repeat("─", contentW)))
	b.WriteString("\n\n")

	// --- Fields -------------------------------------------------------
	for _, f := range m.Desc.Fields {
		if strings.EqualFold(f.Label, "status") {
			// Already rendered as the top pill — skip.
			continue
		}
		val := f.Value
		if val == "" {
			val = "—"
		}
		label := theme.StyleLabel.Render(strings.ToUpper(f.Label))
		b.WriteString(label)
		b.WriteString("\n")

		switch {
		case strings.Contains(val, "\n"):
			// Multi-line (steps, recent runs, etc.) — bullet each line.
			for _, ln := range strings.Split(val, "\n") {
				ln = strings.TrimSpace(ln)
				if ln == "" {
					continue
				}
				bullet := theme.StyleDim.Render("·") + " " +
					theme.StyleValue.Render(truncate(ln, contentW-3))
				b.WriteString("  " + bullet + "\n")
			}
		case strings.Contains(val, ","):
			parts := strings.Split(val, ",")
			chips := make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				chips = append(chips, theme.StyleChipAccent.Render(p))
			}
			b.WriteString("  " + wrapChips(chips, contentW-2) + "\n")
		default:
			rendered := theme.StyleValue.Render(truncate(val, contentW-2))
			b.WriteString("  " + rendered + "\n")
		}
		b.WriteString("\n")
	}

	// --- Actions footer ----------------------------------------------
	if len(m.Desc.Actions) > 0 {
		b.WriteString(theme.StyleMuted.Render(strings.Repeat("─", contentW)))
		b.WriteString("\n")
		b.WriteString(theme.StyleSectionTitle.Render("ACTIONS"))
		b.WriteString("\n")
		for _, a := range m.Desc.Actions {
			b.WriteString("  " + theme.StyleDim.Render("·") + " " +
				theme.StyleValue.Render(a) + "\n")
		}
	}

	return lipgloss.NewStyle().Width(contentW).Render(b.String())
}

// inspectorStatusPill picks the colored pill style for the description's
// status field. Returns "" when no status field exists.
func inspectorStatusPill(d *services.ResourceDescription) string {
	for _, f := range d.Fields {
		if !strings.EqualFold(f.Label, "status") {
			continue
		}
		val := strings.ToLower(strings.TrimSpace(f.Value))
		if val == "" {
			return ""
		}
		switch val {
		case "live", "running":
			return theme.StylePillRunning.Render("● " + strings.ToUpper(val))
		case "completed", "done", "success":
			return theme.StylePillSuccess.Render("✓ " + strings.ToUpper(val))
		case "failed", "error":
			return theme.StylePillError.Render("✗ " + strings.ToUpper(val))
		case "waiting", "pending", "queued":
			return theme.StylePillWarn.Render("◷ " + strings.ToUpper(val))
		default:
			return theme.StyleChipDim.Render(strings.ToUpper(val))
		}
	}
	return ""
}

// wrapChips word-wraps a list of pre-styled chips so they fit within `width`.
// Lipgloss style codes mean we can't measure rendered width precisely from
// the raw strings — we approximate with lipgloss.Width.
func wrapChips(chips []string, width int) string {
	if width < 4 {
		return strings.Join(chips, " ")
	}
	var lines []string
	var cur []string
	curW := 0
	for _, c := range chips {
		w := lipgloss.Width(c)
		if curW > 0 && curW+w+1 > width {
			lines = append(lines, strings.Join(cur, " "))
			cur = []string{c}
			curW = w
			continue
		}
		cur = append(cur, c)
		if curW > 0 {
			curW++
		}
		curW += w
	}
	if len(cur) > 0 {
		lines = append(lines, strings.Join(cur, " "))
	}
	return strings.Join(lines, "\n  ")
}

// ensure fmt stays imported even when unused elsewhere.
var _ = fmt.Sprint
