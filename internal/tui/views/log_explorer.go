package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sourceplane/orun/internal/tui/events"
	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/theme"
)

// Severity captures a coarse log-line classification used for coloring and
// the "errors only" filter. Determined from explicit LogEvent.IsError plus
// a cheap substring scan over the line itself.
type Severity int

const (
	SevInfo Severity = iota
	SevDebug
	SevWarn
	SevError
)

// logLine is the retained form of a streamed LogEvent — kept structured
// rather than pre-formatted so we can re-group/re-filter on the fly.
type logLine struct {
	StepID string
	Line   string
	Sev    Severity
	TS     string
}

// maxLogLines bounds the retained scrollback. A long live run (a package
// install alone can emit tens of thousands of lines) would otherwise grow the
// buffer — and the cost of every viewport rebuild — without limit. The oldest
// lines are dropped; the footer reports how many were trimmed.
const maxLogLines = 4000

// LogExplorerModel is the streaming log viewer. It owns a viewport for
// scrollback plus a filter input, an errors summary card, severity-aware
// rendering, and a follow/pause control. Drop a TailLogs channel in via
// Attach() to start tailing.
type LogExplorerModel struct {
	Events <-chan services.LogEvent

	JobID  string
	StepID string
	Live   bool
	Width  int
	Height int

	// stream is the generation id of the attached tail. Batches stamped with
	// any other id belong to a superseded tail and are discarded, so a stale
	// pump (or a cancelled stream's close-sentinel) can never append foreign
	// lines or flip this view to "ended".
	stream int64

	viewport   viewport.Model
	filter     textinput.Model
	lines      []logLine
	trimmed    int // lines dropped from the head by the buffer cap
	ready      bool
	follow     bool // auto-scroll to bottom on new lines
	errorsOnly bool // filter view to errors+warnings only
	ended      bool // stream has closed
}

func NewLogExplorerModel() LogExplorerModel {
	vp := viewport.New(0, 0)
	ti := textinput.New()
	ti.Placeholder = "filter logs…  (/ focus · esc clear)"
	ti.Prompt = "/ "
	ti.CharLimit = 80
	return LogExplorerModel{viewport: vp, filter: ti, follow: true}
}

// Attach installs a TailLogs channel and resets the buffer. The fresh stream
// id supersedes any previous tail: its in-flight batches no longer match and
// are dropped on arrival.
func (m LogExplorerModel) Attach(ch <-chan services.LogEvent, jobID, stepID string, live bool) (LogExplorerModel, tea.Cmd) {
	m.Events = ch
	m.JobID = jobID
	m.StepID = stepID
	m.Live = live
	m.lines = nil
	m.trimmed = 0
	m.follow = true
	m.errorsOnly = false
	m.ended = false
	m.stream = events.NextLogStream()
	m.rebuildViewport()
	if ch == nil {
		return m, nil
	}
	return m, events.WaitForLogBatch(ch, m.stream)
}

// Detach clears the attached stream + buffer without resetting size info.
// Bumping the stream id orphans any in-flight pump on the old channel.
func (m LogExplorerModel) Detach() LogExplorerModel {
	m.Events = nil
	m.JobID = ""
	m.StepID = ""
	m.Live = false
	m.lines = nil
	m.trimmed = 0
	m.stream = events.NextLogStream()
	m.rebuildViewport()
	return m
}

// FocusFilter focuses the filter input (so `/` from the root model can
// route into us cleanly).
func (m LogExplorerModel) FocusFilter() LogExplorerModel {
	m.filter.Focus()
	return m
}

func (m LogExplorerModel) Init() tea.Cmd { return nil }

func (m LogExplorerModel) SetSize(w, h int) LogExplorerModel {
	m.Width = w
	m.Height = h
	m.viewport.Width = w - 2
	m.ready = true
	m.rebuildViewport()
	return m
}

// viewportHeight computes the scrollback height from the ACTUAL chrome around
// it (header + errors card + filter + footer), so the composed view is always
// exactly m.Height lines tall. A fixed reservation would make the frame's
// height oscillate as the errors card grows during a live run — vertical
// jitter that turns any renderer hiccup into persistent ghost rows.
func (m LogExplorerModel) viewportHeight() int {
	h := m.Height
	if h <= 0 {
		h = 20
	}
	chrome := 3 // header + filter + footer
	if card := m.renderErrorsCard(); card != "" {
		chrome += lipgloss.Height(card)
	}
	inner := h - chrome
	if inner < 3 {
		inner = 3
	}
	return inner
}

func (m LogExplorerModel) Update(msg tea.Msg) (LogExplorerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case services.LogBatchMsg:
		// A batch from a superseded tail (older stream id) is dropped whole:
		// neither its lines nor its close-sentinel may touch the current view.
		if msg.Stream != m.stream {
			return m, nil
		}
		appended := false
		for _, ev := range msg.Events {
			if ev.Line == "" {
				continue
			}
			sev := classifySeverity(ev.Line)
			if ev.IsError && sev < SevError {
				sev = SevError
			}
			m.lines = append(m.lines, logLine{
				StepID: ev.StepID,
				Line:   ev.Line,
				Sev:    sev,
				TS:     ev.Timestamp.Format("15:04:05"),
			})
			appended = true
		}
		// Cap the scrollback (copy, so the dropped head can be collected).
		if over := len(m.lines) - maxLogLines; over > 0 {
			kept := make([]logLine, maxLogLines)
			copy(kept, m.lines[over:])
			m.lines = kept
			m.trimmed += over
		}
		if appended {
			m.rebuildViewport()
		}
		if msg.Closed {
			m.ended = true
			m.Live = false
			return m, nil
		}
		if m.Events != nil {
			return m, events.WaitForLogBatch(m.Events, m.stream)
		}
		return m, nil
	case tea.KeyMsg:
		var cmds []tea.Cmd
		if m.filter.Focused() {
			switch msg.String() {
			case "esc":
				// Clear AND blur — quick reset.
				m.filter.SetValue("")
				m.filter.Blur()
				m.rebuildViewport()
				return m, nil
			case "enter":
				m.filter.Blur()
				return m, nil
			}
			var c tea.Cmd
			m.filter, c = m.filter.Update(msg)
			cmds = append(cmds, c)
			m.rebuildViewport()
		} else {
			switch msg.String() {
			case "/":
				m.filter.Focus()
				return m, nil
			case "c":
				m.lines = nil
				m.rebuildViewport()
				return m, nil
			case "f":
				m.follow = !m.follow
				if m.follow {
					m.viewport.GotoBottom()
				}
				return m, nil
			case "E":
				m.errorsOnly = !m.errorsOnly
				m.rebuildViewport()
				return m, nil
			case "g":
				m.viewport.GotoTop()
				m.follow = false
				return m, nil
			case "G":
				m.viewport.GotoBottom()
				m.follow = true
				return m, nil
			case "]", "}":
				m.jumpStep(1)
				return m, nil
			case "[", "{":
				m.jumpStep(-1)
				return m, nil
			}
			// Any manual scroll (k/j/pgup/pgdn) disables follow.
			before := m.viewport.YOffset
			var c tea.Cmd
			m.viewport, c = m.viewport.Update(msg)
			if m.viewport.YOffset != before && !m.viewport.AtBottom() {
				m.follow = false
			}
			cmds = append(cmds, c)
		}
		return m, tea.Batch(cmds...)
	}
	var c tea.Cmd
	m.viewport, c = m.viewport.Update(msg)
	return m, c
}

// SeverityCounts returns (total, errors, warns) over the retained buffer.
// Exposed so the bottom panel can render a logs summary.
func (m LogExplorerModel) severityCounts() (total, errs, warns int) {
	for _, l := range m.lines {
		total++
		switch l.Sev {
		case SevError:
			errs++
		case SevWarn:
			warns++
		}
	}
	return
}

// classifySeverity does a cheap case-insensitive substring scan; ordering
// matters because "error" can appear inside a debug context, so we prefer
// the most severe match.
func classifySeverity(line string) Severity {
	l := strings.ToLower(line)
	switch {
	case strings.Contains(l, "panic:"),
		strings.Contains(l, "fatal"),
		strings.Contains(l, "error"),
		strings.Contains(l, "err:"),
		strings.Contains(l, "[error]"),
		strings.Contains(l, "fail"):
		return SevError
	case strings.Contains(l, "warn"), strings.Contains(l, "[warn]"):
		return SevWarn
	case strings.Contains(l, "debug"), strings.Contains(l, "[debug]"), strings.Contains(l, "trace"):
		return SevDebug
	}
	return SevInfo
}

// jumpStep moves the viewport to the previous/next step boundary in the
// current buffer order. Disables follow so the user can browse freely.
func (m *LogExplorerModel) jumpStep(direction int) {
	if len(m.lines) == 0 {
		return
	}
	// Collect step boundaries (line index of first occurrence of each new step).
	type boundary struct {
		line int
		step string
	}
	var bounds []boundary
	var last string
	for i, l := range m.lines {
		if l.StepID == "" {
			continue
		}
		if l.StepID != last {
			bounds = append(bounds, boundary{line: i, step: l.StepID})
			last = l.StepID
		}
	}
	if len(bounds) == 0 {
		return
	}
	// Find the boundary closest to current scroll position.
	curLine := m.viewport.YOffset
	idx := 0
	for i, b := range bounds {
		if b.line <= curLine {
			idx = i
		} else {
			break
		}
	}
	next := idx + direction
	if next < 0 {
		next = 0
	}
	if next >= len(bounds) {
		next = len(bounds) - 1
	}
	m.viewport.SetYOffset(bounds[next].line)
	m.follow = false
}

func (m LogExplorerModel) View() string {
	width := m.Width
	if width <= 0 {
		width = 80
	}
	height := m.Height
	if height <= 0 {
		height = 20
	}

	if len(m.lines) == 0 && m.Events == nil {
		return centerCard(width, height,
			"open Activity (tab / 2) and press enter on a job to tail its logs")
	}

	// ── Header ──────────────────────────────────────────────────────────
	job := m.JobID
	if job == "" {
		job = "(no job)"
	}
	step := m.StepID
	if step == "" {
		step = "all steps"
	}
	// Pill glyphs stay on the cockpit's vetted single-width set. U+23F8 "⏸"
	// (and friends with an emoji presentation) render two cells wide on some
	// terminals while measuring one — a line that then lands on the terminal's
	// last column wraps, scrolls the alt screen one row, and desyncs the
	// renderer (ghost rows). ▌ is already proven single-width in this UI.
	livePill := theme.StylePillIdle.Render("○ idle")
	switch {
	case m.ended:
		livePill = theme.StylePillIdle.Render("○ ended")
	case m.Live && m.follow:
		livePill = theme.StylePillRunning.Render("● LIVE")
	case m.Live && !m.follow:
		livePill = theme.StylePillWarn.Render("▌▌ PAUSED")
	}
	modePills := ""
	if m.errorsOnly {
		modePills = "  " + theme.StylePillError.Render("errors only")
	}
	header := clipWidth(fmt.Sprintf("%s  %s  %s   %s%s",
		theme.StyleSectionTitle.Render("Logs"),
		theme.StyleChipAccent.Render(job),
		theme.StyleDim.Render(step),
		livePill,
		modePills,
	), width)

	errors := m.renderErrorsCard()
	body := m.viewport.View()

	// ── Footer status line ──────────────────────────────────────────────
	footer := m.renderStatusFooter()

	pieces := []string{header}
	if errors != "" {
		pieces = append(pieces, errors)
	}
	pieces = append(pieces, m.filter.View(), body, footer)
	return strings.Join(pieces, "\n")
}

func (m LogExplorerModel) renderStatusFooter() string {
	var total, errs, warns int
	for _, l := range m.lines {
		total++
		switch l.Sev {
		case SevError:
			errs++
		case SevWarn:
			warns++
		}
	}
	lineCount := fmt.Sprintf("%d lines", total)
	if m.trimmed > 0 {
		lineCount = fmt.Sprintf("%d lines (%d earlier trimmed)", total, m.trimmed)
	}
	parts := []string{
		theme.StyleDim.Render(lineCount),
	}
	if errs > 0 {
		parts = append(parts, theme.StylePillError.Render(fmt.Sprintf("%d err", errs)))
	}
	if warns > 0 {
		parts = append(parts, theme.StylePillWarn.Render(fmt.Sprintf("%d warn", warns)))
	}
	hints := theme.StyleDim.Render("  f follow · E errors-only · [ ] step · g/G top/bottom · c clear · / filter")
	width := m.Width
	if width <= 0 {
		width = 80
	}
	return clipWidth(strings.Join(parts, "  ")+hints, width)
}

// clipWidth truncates a (possibly ANSI-styled) single line to w visible columns
// without splitting escape sequences — lipgloss MaxWidth is escape-aware, unlike
// the byte-slicing truncate used for plain text.
func clipWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

func (m LogExplorerModel) renderErrorsCard() string {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	var errs []logLine
	for _, l := range m.lines {
		if l.Sev != SevError {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(l.Line), q) {
			continue
		}
		errs = append(errs, l)
	}
	if len(errs) == 0 {
		return ""
	}
	// last 5 most recent
	const max = 5
	start := 0
	if len(errs) > max {
		start = len(errs) - max
	}
	tail := errs[start:]
	width := m.Width
	if width <= 0 {
		width = 80
	}
	var b strings.Builder
	b.WriteString(theme.StyleSectionTitle.Render("Errors"))
	b.WriteString(theme.StyleDim.Render(fmt.Sprintf("  (%d)", len(errs))))
	for _, e := range tail {
		step := e.StepID
		if step == "" {
			step = "—"
		}
		// Truncate to the card width (indent + step chip + space) so a long
		// error line can't wrap and break the frame.
		avail := width - 4 - lipgloss.Width(step) - 1
		line := truncate(e.Line, avail)
		b.WriteString("\n  " + theme.StyleChipDim.Render(step) + " " +
			theme.StylePillError.Render(line))
	}
	if len(errs) > max {
		b.WriteString("\n" + theme.StyleDim.Render(fmt.Sprintf("  +%d more", len(errs)-max)))
	}
	// No trailing newline: the caller stacks pieces with strings.Join("\n"),
	// and a trailing newline here would inject a phantom blank line whose
	// presence depends on whether errors exist — frame-height jitter.
	return b.String()
}

// severityGlyph returns a colored 1-char prefix for the gutter. Glyphs come
// from the cockpit's vetted single-width set — U+2716 "✖" has an emoji
// presentation that renders two cells on some terminals, which widens the
// line past its measured width and wraps/scrolls the alt screen.
func severityGlyph(s Severity) string {
	switch s {
	case SevError:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171")).Render("✗")
	case SevWarn:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#fbbf24")).Render("▲")
	case SevDebug:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#64748b")).Render("·")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#22d3ee")).Render("›")
}

// severityStyle returns a lipgloss style for the line body.
func severityStyle(s Severity) lipgloss.Style {
	switch s {
	case SevError:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#fca5a5"))
	case SevWarn:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#fde68a"))
	case SevDebug:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#64748b"))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#e2e8f0"))
}

func (m *LogExplorerModel) rebuildViewport() {
	if !m.ready && m.viewport.Width == 0 {
		return
	}
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	// Hard cap every rendered row at the viewport width. The bubbles viewport
	// does not clip horizontally, so an un-truncated long line (e.g. a full
	// filesystem path in a log message) soft-wraps in the terminal, throwing off
	// the renderer's line accounting and corrupting the whole TUI frame.
	width := m.viewport.Width
	if width <= 0 {
		width = 80
	}
	var b strings.Builder
	var lastStep string
	for _, l := range m.lines {
		if q != "" && !strings.Contains(strings.ToLower(l.Line), q) {
			continue
		}
		if m.errorsOnly && l.Sev != SevError && l.Sev != SevWarn {
			continue
		}
		if l.StepID != lastStep && l.StepID != "" {
			b.WriteString(theme.StyleDim.Render(truncate("── step: "+l.StepID+" ──", width)) + "\n")
			lastStep = l.StepID
		}
		head := severityGlyph(l.Sev) + " "
		headW := 2 // glyph + space (glyph is single-width)
		if l.TS != "" {
			head += theme.StyleDim.Render(l.TS) + " "
			headW += lipgloss.Width(l.TS) + 1
		}
		body := truncate(l.Line, width-headW)
		b.WriteString(head + severityStyle(l.Sev).Render(body) + "\n")
	}
	// Track the chrome's current height so the composed frame stays exactly
	// m.Height tall (the errors card can grow as a live run streams errors).
	m.viewport.Height = m.viewportHeight()
	content := strings.TrimRight(b.String(), "\n")
	if content == "" {
		// Keep the viewport in place (it pads to its height) so the frame
		// doesn't reshape when the first line lands.
		if len(m.lines) == 0 {
			content = theme.StyleDim.Render("  waiting for log lines…")
		} else {
			content = theme.StyleDim.Render("  no lines match the filter")
		}
	}
	m.viewport.SetContent(content)
	if m.follow {
		m.viewport.GotoBottom()
	}
}
