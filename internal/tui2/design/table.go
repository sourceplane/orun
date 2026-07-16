package design

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/sourceplane/orun/internal/tui2/frame"
)

// Column describes one table column. Width 0 marks the flex column that
// absorbs remaining space; at most one column should flex.
type Column struct {
	Title string
	Width int
}

// Table renders the Northwind data table at exactly width cells per line:
// a dim header, a muted rule, then rows with the selected one marked. Row
// cells are plain strings; the caller tones them (StatusText, Ref, …) —
// the table owns geometry, not meaning.
func Table(width int, cols []Column, rows [][]string, selected int) string {
	if width < 8 || len(cols) == 0 {
		return ""
	}
	const gutter = 2
	// Resolve the flex column.
	fixed := 0
	flex := -1
	for i, c := range cols {
		if c.Width == 0 {
			flex = i
			continue
		}
		fixed += c.Width
	}
	avail := width - 3 - gutter*(len(cols)-1) - fixed
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = c.Width
	}
	if flex >= 0 {
		if avail < 8 {
			avail = 8
		}
		widths[flex] = avail
	}

	var b strings.Builder
	// Header.
	cells := make([]string, len(cols))
	for i, c := range cols {
		cells[i] = frame.FitLine(Dim.Render(strings.ToLower(c.Title)), widths[i])
	}
	b.WriteString("   " + strings.Join(cells, strings.Repeat(" ", gutter)))
	b.WriteString("\n")
	b.WriteString(" " + Rule(width-2))
	// Rows.
	for r, row := range rows {
		b.WriteString("\n")
		marker := "   "
		if r == selected {
			marker = " " + Selected.Render("▸") + " "
		}
		for i := range cols {
			var cell string
			if i < len(row) {
				cell = row[i]
			}
			if r == selected && i == 0 && ansi.StringWidth(cell) > 0 {
				cell = Selected.Render(ansi.Strip(cell))
			}
			cells[i] = frame.FitLine(cell, widths[i])
		}
		b.WriteString(marker + strings.Join(cells, strings.Repeat(" ", gutter)))
	}
	return b.String()
}
