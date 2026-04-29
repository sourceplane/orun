package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

// LiveRegion renders a transient block below the cursor while still allowing
// persistent lines to be printed above. The block can show a sticky "header"
// (status counts, progress) plus a windowed list of in-flight rows grouped by
// component. On non-TTY writers it degrades to plain append-only output:
// SetRow/RemoveRow become no-ops and Print just writes a line.
type LiveRegion struct {
	w            io.Writer
	tty          bool
	color        bool
	fd           int
	mu           sync.Mutex
	rows         []liveRow
	rowIndex     map[string]int
	frame        int
	headerFunc   func() []string
	windowMax    int
	stop         chan struct{}
	done         chan struct{}
	lastRendered int
}

type liveRow struct {
	key   string
	group string
	label string
	tail  string
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// NewLiveRegion constructs a live region.
func NewLiveRegion(w io.Writer, tty, color bool) *LiveRegion {
	fd := -1
	if f, ok := w.(*os.File); ok && tty {
		fd = int(f.Fd())
	}
	return &LiveRegion{
		w:         w,
		tty:       tty,
		color:     color,
		fd:        fd,
		rowIndex:  map[string]int{},
		windowMax: 14,
	}
}

// SetHeaderFunc registers a callback that returns sticky header lines drawn
// above the active rows on every frame. Pass nil to clear.
func (l *LiveRegion) SetHeaderFunc(fn func() []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.headerFunc = fn
}

// SetWindowMax caps the number of lines used by the active section.
func (l *LiveRegion) SetWindowMax(n int) {
	if n < 1 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.windowMax = n
}

// Start begins the spinner render goroutine. No-op for non-TTY.
func (l *LiveRegion) Start() {
	if !l.tty {
		return
	}
	l.stop = make(chan struct{})
	l.done = make(chan struct{})
	go l.loop()
}

// Stop ends the render goroutine and clears the live region.
func (l *LiveRegion) Stop() {
	if !l.tty || l.stop == nil {
		return
	}
	close(l.stop)
	<-l.done
	l.stop = nil
	l.done = nil
	l.mu.Lock()
	defer l.mu.Unlock()
	l.clearRegion()
	l.rows = nil
	l.rowIndex = map[string]int{}
	l.headerFunc = nil
}

func (l *LiveRegion) loop() {
	t := time.NewTicker(120 * time.Millisecond)
	defer t.Stop()
	defer close(l.done)
	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
			l.mu.Lock()
			l.frame++
			l.draw()
			l.mu.Unlock()
		}
	}
}

// SetRow inserts or updates a row by key (no group). For component grouping
// use SetRowDetail instead.
func (l *LiveRegion) SetRow(key, label string) {
	l.SetRowDetail(key, "", label, "")
}

// SetRowDetail upserts a row with optional group label and trailing
// breadcrumb. Empty group/tail leaves the prior values intact when updating.
func (l *LiveRegion) SetRowDetail(key, group, label, tail string) {
	if !l.tty {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if idx, ok := l.rowIndex[key]; ok {
		l.rows[idx].label = label
		if group != "" {
			l.rows[idx].group = group
		}
		l.rows[idx].tail = tail
		return
	}
	l.rowIndex[key] = len(l.rows)
	l.rows = append(l.rows, liveRow{key: key, group: group, label: label, tail: tail})
}

// RemoveRow drops a row from the live region.
func (l *LiveRegion) RemoveRow(key string) {
	if !l.tty {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	idx, ok := l.rowIndex[key]
	if !ok {
		return
	}
	l.rows = append(l.rows[:idx], l.rows[idx+1:]...)
	delete(l.rowIndex, key)
	for k, v := range l.rowIndex {
		if v > idx {
			l.rowIndex[k] = v - 1
		}
	}
}

// RowCount returns the current number of in-flight rows.
func (l *LiveRegion) RowCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.rows)
}

// Print writes a persistent line above the live region.
func (l *LiveRegion) Print(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.clearRegion()
	fmt.Fprintln(l.w, line)
	l.draw()
}

// PrintBlock writes multiple persistent lines above the live region.
func (l *LiveRegion) PrintBlock(lines []string) {
	if len(lines) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.clearRegion()
	for _, ln := range lines {
		fmt.Fprintln(l.w, ln)
	}
	l.draw()
}

// clearRegion clears any previously rendered live rows. Caller holds mu.
func (l *LiveRegion) clearRegion() {
	if !l.tty || l.lastRendered <= 0 {
		return
	}
	fmt.Fprintf(l.w, "\x1b[%dA\x1b[J", l.lastRendered)
	l.lastRendered = 0
}

// draw renders the current frame: sticky header, then rows grouped by
// component within an elastic window. Caller holds mu.
func (l *LiveRegion) draw() {
	if !l.tty {
		return
	}
	l.clearRegion()

	width := l.termWidth()
	frame := spinnerFrames[l.frame%len(spinnerFrames)]

	var lines []string

	if l.headerFunc != nil {
		for _, h := range l.headerFunc() {
			lines = append(lines, h)
		}
	}

	if len(l.rows) == 0 {
		l.flushLines(lines, width)
		return
	}

	if l.headerFunc != nil {
		lines = append(lines, "")
	}
	lines = append(lines, "  "+Dim(l.color, "Active"))
	lines = append(lines, "  "+Dim(l.color, "│"))

	type rowGroup struct {
		key  string
		rows []liveRow
	}
	var groups []*rowGroup
	groupIdx := map[string]int{}
	for _, r := range l.rows {
		if i, ok := groupIdx[r.group]; ok {
			groups[i].rows = append(groups[i].rows, r)
		} else {
			groupIdx[r.group] = len(groups)
			groups = append(groups, &rowGroup{key: r.group, rows: []liveRow{r}})
		}
	}

	used := 0
	hiddenJobs := 0
	for gi, g := range groups {
		if used+2 > l.windowMax {
			for _, r := range g.rows {
				_ = r
				hiddenJobs++
			}
			continue
		}
		title := g.key
		if title == "" {
			title = g.rows[0].key
		}
		lines = append(lines, fmt.Sprintf("  %s %s", Cyan(l.color, "●"), Bold(l.color, title)))
		used++
		for ri, r := range g.rows {
			if used >= l.windowMax {
				hiddenJobs++
				continue
			}
			connector := "├─"
			if ri == len(g.rows)-1 && r.tail == "" {
				connector = "└─"
			}
			lines = append(lines, fmt.Sprintf("  %s  %s %s %s",
				Dim(l.color, "│"), Dim(l.color, connector), Cyan(l.color, frame), r.label))
			used++
			if r.tail != "" && used < l.windowMax {
				tailConnector := "│ "
				if ri == len(g.rows)-1 {
					tailConnector = "  "
				}
				lines = append(lines, fmt.Sprintf("  %s  %s   %s %s",
					Dim(l.color, "│"),
					Dim(l.color, tailConnector),
					Dim(l.color, "└─ ❯"),
					Dim(l.color, r.tail)))
				used++
			}
		}
		if gi < len(groups)-1 && used < l.windowMax {
			lines = append(lines, "  "+Dim(l.color, "│"))
			used++
		}
	}
	if hiddenJobs > 0 {
		lines = append(lines, fmt.Sprintf("  %s %s + %d more active task%s",
			Dim(l.color, "│"), Dim(l.color, ""), hiddenJobs, pluralS(hiddenJobs)))
	}

	l.flushLines(lines, width)
}

func (l *LiveRegion) flushLines(lines []string, width int) {
	for _, ln := range lines {
		if width > 0 {
			ln = truncateVisible(ln, width-1)
		}
		fmt.Fprintln(l.w, ln)
	}
	l.lastRendered = len(lines)
}

func (l *LiveRegion) termWidth() int {
	if l.fd < 0 {
		return 0
	}
	w, _, err := term.GetSize(l.fd)
	if err != nil || w <= 0 {
		return 80
	}
	return w
}

func truncateVisible(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	visible := 0
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !isAnsiTerminator(s[j]) {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		visible++
		if visible > maxWidth {
			return s[:i] + "\x1b[0m"
		}
		i += size
	}
	return s
}

func isAnsiTerminator(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// RenderProgressBar draws a fixed-width unicode progress bar.
func RenderProgressBar(pct, width int) string {
	if width <= 2 {
		width = 24
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	inner := width
	filled := pct * inner / 100
	if filled > inner {
		filled = inner
	}
	return strings.Repeat("▓", filled) + strings.Repeat("░", inner-filled)
}
