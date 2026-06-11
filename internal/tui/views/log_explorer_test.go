package views

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/sourceplane/orun/internal/tui/events"
	"github.com/sourceplane/orun/internal/tui/services"
)

func logEvents(n int, line string) []services.LogEvent {
	out := make([]services.LogEvent, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, services.LogEvent{
			JobID: "cli.dev.verify", StepID: "install-workspace-dependencies",
			Line: line, Timestamp: time.Unix(0, 0),
		})
	}
	return out
}

// TestLogExplorer_LinesNeverExceedWidth guards TUI frame integrity: the bubbles
// viewport does not clip horizontally, so a log line wider than the pane wraps
// in the terminal and corrupts the whole frame (doubled headers/footers). Every
// rendered row — viewport body AND the errors card — must stay within the width.
func TestLogExplorer_LinesNeverExceedWidth(t *testing.T) {
	const width, height = 80, 24
	m := NewLogExplorerModel()
	m = m.SetSize(width, height)

	// Long error lines (full filesystem paths) — the real-world trigger.
	long := "WARN Failed to create bin at /Users/irinelinson/sourceplane/multi-tenant-saas/.orun/runs/multi-tenant-saas-20260608-368372/cli/node_modules/.bin/whatever-very-long-binary-name"
	m, _ = m.Update(services.LogBatchMsg{Events: logEvents(30, long)})

	out := m.View()
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("rendered line width %d exceeds pane width %d:\n%q", w, width, line)
		}
	}
	if h := lipgloss.Height(out); h > height {
		t.Fatalf("rendered height %d exceeds pane height %d", h, height)
	}
}

// TestLogExplorer_FrameHeightStableWhileStreaming is the live-run jitter
// guard: as lines and errors stream in, the composed frame must stay EXACTLY
// the configured height — the errors card growing (0 → 5 → "+N more") must
// shrink the viewport, never reshape the frame. A frame whose height
// oscillates between renders turns any terminal hiccup into persistent ghost
// rows (doubled headers/footers).
func TestLogExplorer_FrameHeightStableWhileStreaming(t *testing.T) {
	const width, height = 100, 30
	m := NewLogExplorerModel()
	ch := make(chan services.LogEvent) // attached: streaming state
	m, _ = m.Attach(ch, "cli.dev.verify", "install", true)
	m = m.SetSize(width, height)

	assertHeight := func(stage string) {
		t.Helper()
		out := m.View()
		if h := lipgloss.Height(out); h != height {
			t.Fatalf("%s: frame height %d, want exactly %d\n%s", stage, h, height, out)
		}
	}

	assertHeight("no lines yet")
	m, _ = m.Update(services.LogBatchMsg{Stream: m.stream, Events: logEvents(3, "Progress: resolved 1028")})
	assertHeight("info lines only (no errors card)")
	m, _ = m.Update(services.LogBatchMsg{Stream: m.stream, Events: logEvents(2, "WARN Failed to create bin at /tmp/x")})
	assertHeight("2 errors (card without +more)")
	m, _ = m.Update(services.LogBatchMsg{Stream: m.stream, Events: logEvents(17, "WARN Failed to create bin at /tmp/y")})
	assertHeight("19 errors (card with +more)")
	m, _ = m.Update(services.LogBatchMsg{Stream: m.stream, Closed: true})
	assertHeight("stream ended")
}

// A batch from a superseded tail (stale pump or a cancelled stream's
// close-sentinel) must be dropped whole: no appended lines, no "ended" flip,
// no re-armed pump.
func TestLogExplorer_StaleStreamBatchIgnored(t *testing.T) {
	m := NewLogExplorerModel().SetSize(80, 24)
	ch := make(chan services.LogEvent)
	m, _ = m.Attach(ch, "job", "step", true)

	stale := services.LogBatchMsg{Stream: m.stream - 1, Events: logEvents(5, "old run line"), Closed: true}
	m2, cmd := m.Update(stale)
	if len(m2.lines) != 0 {
		t.Fatalf("stale batch appended %d lines", len(m2.lines))
	}
	if m2.ended || !m2.Live {
		t.Fatal("stale close-sentinel must not end the live stream")
	}
	if cmd != nil {
		t.Fatal("stale batch must not re-arm a pump")
	}

	// The current stream's batch still lands and re-arms.
	m3, cmd := m2.Update(services.LogBatchMsg{Stream: m2.stream, Events: logEvents(2, "current line")})
	if len(m3.lines) != 2 {
		t.Fatalf("current batch lines = %d, want 2", len(m3.lines))
	}
	if cmd == nil {
		t.Fatal("current batch must re-arm the pump")
	}
}

// Closed batches mark the stream ended and stop the pump.
func TestLogExplorer_ClosedBatchEndsStream(t *testing.T) {
	m := NewLogExplorerModel().SetSize(80, 24)
	ch := make(chan services.LogEvent)
	m, _ = m.Attach(ch, "job", "step", true)

	m2, cmd := m.Update(services.LogBatchMsg{Stream: m.stream, Events: logEvents(1, "tail line"), Closed: true})
	if !m2.ended || m2.Live {
		t.Fatalf("closed batch should end the stream: ended=%v live=%v", m2.ended, m2.Live)
	}
	if cmd != nil {
		t.Fatal("closed batch must not re-arm the pump")
	}
	if len(m2.lines) != 1 {
		t.Fatalf("trailing events of a closed batch must still land, got %d", len(m2.lines))
	}
}

// The scrollback is bounded: a long live run trims from the head and reports
// the trim in the footer instead of growing (and re-rendering) without limit.
func TestLogExplorer_BufferCap(t *testing.T) {
	m := NewLogExplorerModel().SetSize(80, 24)
	for i := 0; i < (maxLogLines+500)/250; i++ {
		batch := make([]services.LogEvent, 250)
		for j := range batch {
			batch[j] = services.LogEvent{Line: fmt.Sprintf("line %d-%d", i, j), Timestamp: time.Unix(0, 0)}
		}
		m, _ = m.Update(services.LogBatchMsg{Events: batch})
	}
	if len(m.lines) != maxLogLines {
		t.Fatalf("retained lines = %d, want cap %d", len(m.lines), maxLogLines)
	}
	if m.trimmed != 500 {
		t.Fatalf("trimmed = %d, want 500", m.trimmed)
	}
	if out := m.View(); !strings.Contains(out, "earlier trimmed") {
		t.Error("footer should report the trimmed scrollback")
	}
}

// The pills and gutter glyphs must stay off code points with an emoji
// presentation (⏸ U+23F8, ✖ U+2716): they render two cells on some terminals
// while measuring one, which wraps a full-width line, scrolls the alt screen
// one row, and desyncs the renderer (ghost rows).
func TestLogExplorer_NoEmojiPresentationGlyphs(t *testing.T) {
	m := NewLogExplorerModel().SetSize(100, 30)
	ch := make(chan services.LogEvent)
	m, _ = m.Attach(ch, "job", "step", true)
	m, _ = m.Update(services.LogBatchMsg{Stream: m.stream, Events: logEvents(3, "error: boom")})
	m.follow = false // live + scrolled = the PAUSED pill
	for _, frame := range []string{m.View()} {
		for _, banned := range []rune{'⏸', '✖'} {
			if strings.ContainsRune(frame, banned) {
				t.Errorf("frame contains emoji-presentation glyph %q", banned)
			}
		}
	}
}

// WaitForLogBatch coalesces everything immediately available into one message
// and reports channel close.
func TestWaitForLogBatch_CoalescesAndReportsClose(t *testing.T) {
	ch := make(chan services.LogEvent, 16)
	for i := 0; i < 10; i++ {
		ch <- services.LogEvent{Line: fmt.Sprintf("l%d", i)}
	}
	msg := events.WaitForLogBatch(ch, 7)()
	batch, ok := msg.(services.LogBatchMsg)
	if !ok {
		t.Fatalf("msg = %T, want LogBatchMsg", msg)
	}
	if batch.Stream != 7 || len(batch.Events) != 10 || batch.Closed {
		t.Fatalf("batch = stream %d, %d events, closed %v; want 7, 10, false", batch.Stream, len(batch.Events), batch.Closed)
	}

	close(ch)
	msg = events.WaitForLogBatch(ch, 7)()
	batch = msg.(services.LogBatchMsg)
	if !batch.Closed || len(batch.Events) != 0 {
		t.Fatalf("closed channel: batch = %+v, want Closed with no events", batch)
	}
}
