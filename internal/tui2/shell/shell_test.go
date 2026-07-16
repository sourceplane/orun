package shell_test

import (
	"math/rand"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/sourceplane/orun/internal/tui2/demo"
	"github.com/sourceplane/orun/internal/tui2/frame"
	"github.com/sourceplane/orun/internal/tui2/shell"
)

type fakeClock struct{ t time.Time }

func newClock() *fakeClock                   { return &fakeClock{t: time.Unix(1_700_000_000, 0)} }
func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

func newShell(w, h int) (*shell.Shell, *fakeClock) {
	clock := newClock()
	sh := shell.New(shell.Config{Surfaces: demo.New(), Now: clock.Now})
	sh.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return sh, clock
}

func runes(s string) tea.KeyMsg        { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func keyType(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func mustView(t *testing.T, sh *shell.Shell, w, h int) string {
	t.Helper()
	out := sh.View()
	if err := frame.Check(out, frame.Size{Width: w, Height: h}); err != nil {
		t.Fatalf("frame invariant violated: %v\n%s", err, out)
	}
	return out
}

// yieldsQuit executes a command tree and reports whether any leaf is QuitMsg.
func yieldsQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	switch m := cmd().(type) {
	case tea.QuitMsg:
		return true
	case tea.BatchMsg:
		for _, c := range m {
			if yieldsQuit(c) {
				return true
			}
		}
	}
	return false
}

// TestFrameInvariantProperty is design §12's "frame invariants as property
// tests": arbitrary key/resize/tick sequences at arbitrary sizes never
// destabilize the frame, esc never quits, and no state corrupts the view.
func TestFrameInvariantProperty(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	keys := []tea.KeyMsg{
		runes("a"), runes("j"), runes("k"), runes("r"), runes("?"), runes(":"),
		runes("1"), runes("2"), runes("3"), runes("9"),
		keyType(tea.KeyEsc), keyType(tea.KeyEnter), keyType(tea.KeyUp),
		keyType(tea.KeyDown), keyType(tea.KeyBackspace), keyType(tea.KeyCtrlK),
		keyType(tea.KeyCtrlC),
	}
	w, h := 100, 30
	sh, clock := newShell(w, h)
	for i := 0; i < 3000; i++ {
		switch r.Intn(10) {
		case 0:
			w, h = 24+r.Intn(200), 6+r.Intn(60)
			sh.Update(tea.WindowSizeMsg{Width: w, Height: h})
		case 1:
			sh.Update(frame.TickMsg{})
		default:
			// The clock leaps between presses so ctrl+c never lands twice
			// inside the quit-guard window.
			clock.Advance(3 * time.Second)
			_, cmd := sh.Update(keys[r.Intn(len(keys))])
			_ = cmd // never executed: ticks are injected synthetically above
		}
		mustView(t, sh, w, h)
	}
}

func TestDigitsSwitchSurfaces(t *testing.T) {
	sh, _ := newShell(100, 30)
	sh.Update(runes("3"))
	if got := sh.Router().Active().ID(); got != "activity" {
		t.Fatalf("active = %s, want activity", got)
	}
	sh.Update(runes("1"))
	if got := sh.Router().Active().ID(); got != "home" {
		t.Fatalf("active = %s, want home", got)
	}
	// esc walks surface history back.
	sh.Update(keyType(tea.KeyEsc))
	if got := sh.Router().Active().ID(); got != "activity" {
		t.Fatalf("after esc active = %s, want activity", got)
	}
}

func TestPaletteRunsCommands(t *testing.T) {
	sh, _ := newShell(100, 30)
	sh.Update(keyType(tea.KeyCtrlK))
	if sh.OverlayDepth() != 1 {
		t.Fatal("palette must open")
	}
	for _, r := range "agents" {
		sh.Update(runes(string(r)))
	}
	_, cmd := sh.Update(keyType(tea.KeyEnter))
	if sh.OverlayDepth() != 0 {
		t.Fatal("palette must close on enter")
	}
	if cmd == nil {
		t.Fatal("selected command must run")
	}
	sh.Update(cmd())
	if got := sh.Router().Active().ID(); got != "agents" {
		t.Fatalf("active = %s, want agents", got)
	}
}

func TestEscNeverQuits(t *testing.T) {
	sh, _ := newShell(100, 30)
	states := [][]tea.Msg{
		{},                                  // home root
		{runes("2"), keyType(tea.KeyEnter)}, // agents, attached (composer)
		{runes("?")},                        // help overlay
		{keyType(tea.KeyCtrlK)},             // palette
	}
	for i, setup := range states {
		for _, m := range setup {
			sh.Update(m)
		}
		for j := 0; j < 5; j++ {
			_, cmd := sh.Update(keyType(tea.KeyEsc))
			if yieldsQuit(cmd) {
				t.Fatalf("state %d: esc produced quit", i)
			}
		}
	}
}

func TestCtrlCQuitGuard(t *testing.T) {
	sh, clock := newShell(100, 30)
	_, cmd := sh.Update(keyType(tea.KeyCtrlC))
	if yieldsQuit(cmd) {
		t.Fatal("first ctrl+c must not quit")
	}
	if !strings.Contains(ansi.Strip(sh.View()), "again to quit") {
		t.Fatal("quit guard must announce itself on the status line")
	}
	clock.Advance(time.Second)
	_, cmd = sh.Update(keyType(tea.KeyCtrlC))
	if !yieldsQuit(cmd) {
		t.Fatal("second ctrl+c inside the window must quit")
	}

	// Expired window: the guard re-arms instead of quitting.
	sh2, clock2 := newShell(100, 30)
	sh2.Update(keyType(tea.KeyCtrlC))
	clock2.Advance(5 * time.Second)
	_, cmd = sh2.Update(keyType(tea.KeyCtrlC))
	if yieldsQuit(cmd) {
		t.Fatal("ctrl+c after the window must re-arm, not quit")
	}
}

// TestComposerOwnsKeyboard pins the focus rule: while a surface reports
// InputFocused, digits and globals reach the composer, not the router.
func TestComposerOwnsKeyboard(t *testing.T) {
	sh, _ := newShell(100, 30)
	sh.Update(runes("2"))
	sh.Update(keyType(tea.KeyEnter)) // attach → composer focused
	sh.Update(runes("1"))
	sh.Update(runes("?"))
	if got := sh.Router().Active().ID(); got != "agents" {
		t.Fatalf("typing must not navigate; active = %s", got)
	}
	if sh.OverlayDepth() != 0 {
		t.Fatal("typing '?' into a composer must not open help")
	}
	if !strings.Contains(ansi.Strip(sh.View()), "› 1?") {
		t.Fatalf("composer must receive the typed text:\n%s", ansi.Strip(sh.View()))
	}
	// ctrl+k stays global even while typing.
	sh.Update(keyType(tea.KeyCtrlK))
	if sh.OverlayDepth() != 1 {
		t.Fatal("ctrl+k must stay global while typing")
	}
}

// TestIdleHoldsZeroTickers is design §13.3: navigation alone never starts
// the animation chain.
func TestIdleHoldsZeroTickers(t *testing.T) {
	sh, _ := newShell(100, 30)
	for _, m := range []tea.Msg{
		runes("2"), keyType(tea.KeyEnter), keyType(tea.KeyEsc),
		runes("3"), runes("j"), runes("1"), keyType(tea.KeyCtrlK), keyType(tea.KeyEsc),
	} {
		sh.Update(m)
		sh.View()
	}
	if sh.Scheduler().Armed() || sh.Scheduler().Active() {
		t.Fatal("idle cockpit must hold zero tickers")
	}
}

// TestIdleTickIsRegionOnly is the §11 budget's mechanism: with only the
// status line animating, ticks re-render the status region and nothing else.
func TestIdleTickIsRegionOnly(t *testing.T) {
	sh, _ := newShell(100, 30)
	sh.View()
	sh.Update(shell.NoticeMsg{Text: "working"}) // arms the status animator
	sh.View()

	_, missesBefore := sh.MemoStats()
	const ticks = 5
	for i := 0; i < ticks; i++ {
		sh.Update(frame.TickMsg{})
		mustView(t, sh, 100, 30)
	}
	hits, misses := sh.MemoStats()
	if got := misses - missesBefore; got != ticks {
		t.Fatalf("ticks re-rendered %d regions, want %d (status only)", got, ticks)
	}
	if hits < 2*ticks {
		t.Fatalf("header+stage must be cache hits during idle ticks (hits=%d)", hits)
	}
}

// TestNoticeExpires: the notice clears itself and the tick chain dies with
// it — no residual animation after the work is done.
func TestNoticeExpires(t *testing.T) {
	sh, clock := newShell(100, 30)
	sh.Update(shell.NoticeMsg{Text: "hello"})
	if !strings.Contains(ansi.Strip(sh.View()), "hello") {
		t.Fatal("notice must show")
	}
	clock.Advance(3 * time.Second)
	sh.Update(frame.TickMsg{})
	if strings.Contains(ansi.Strip(sh.View()), "hello") {
		t.Fatal("notice must expire")
	}
	if sh.Scheduler().Active() || sh.Scheduler().Armed() {
		t.Fatal("expired notice must release the tick chain")
	}
}

// TestDemoRunAnimatesAndCompletes drives the fake run end to end: the
// animator registers, progress advances per tick, and completion releases
// the chain (motion means work — and stops when the work stops).
func TestDemoRunAnimatesAndCompletes(t *testing.T) {
	sh, _ := newShell(100, 30)
	sh.Update(runes("3"))
	_, cmd := sh.Update(runes("r"))
	if cmd == nil {
		t.Fatal("starting the run must arm the scheduler")
	}
	if !sh.Scheduler().Active() {
		t.Fatal("run must register an animator")
	}
	prev := mustView(t, sh, 100, 30)
	changed := 0
	for i := 0; i < 30; i++ {
		sh.Update(frame.TickMsg{})
		out := mustView(t, sh, 100, 30)
		if out != prev {
			changed++
		}
		prev = out
	}
	if changed < 20 {
		t.Fatalf("run must animate per tick (only %d frames changed)", changed)
	}
	if sh.Scheduler().Active() {
		t.Fatal("completed run must deregister its animator")
	}
	if !strings.Contains(ansi.Strip(prev), "demo run · succeeded") {
		t.Fatal("completed run must land in the feed")
	}
}

func TestHelpIsGenerated(t *testing.T) {
	sh, _ := newShell(120, 40)
	sh.Update(runes("?"))
	out := ansi.Strip(mustView(t, sh, 120, 40))
	for _, want := range []string{"Go to Agents", "Command palette", "Quit"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help must list %q:\n%s", want, out)
		}
	}
	sh.Update(runes("x"))
	if sh.OverlayDepth() != 0 {
		t.Fatal("any key must close help")
	}
}

func TestTooSmallDegradesStably(t *testing.T) {
	sh, _ := newShell(10, 3)
	mustView(t, sh, 10, 3)
}

// --- benches (make tui-bench) ----------------------------------------------

func BenchmarkIdleTick(b *testing.B) {
	sh, _ := newShell(220, 60)
	sh.Update(shell.NoticeMsg{Text: "working"})
	sh.View()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sh.Update(frame.TickMsg{})
		_ = sh.View()
	}
}

func BenchmarkFullFrame(b *testing.B) {
	sh, _ := newShell(220, 60)
	sh.Update(runes("2"))
	b.ReportAllocs()
	b.ResetTimer()
	keys := []tea.KeyMsg{runes("j"), runes("k")}
	for i := 0; i < b.N; i++ {
		sh.Update(keys[i%2])
		_ = sh.View()
	}
}
