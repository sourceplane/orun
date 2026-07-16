package frame

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ProfileEnvVar names the file the cockpit writes per-frame timings to.
// When it is unset the profiler is not installed at all and the TUI runs
// exactly as before — zero allocations, zero indirection.
//
//	ORUN_TUI_PROFILE=/tmp/tui.ndjson orun tui --next
const ProfileEnvVar = "ORUN_TUI_PROFILE"

// frameRecord is one Update+View cycle. Bubbletea drives the model with a
// single message at a time and re-renders after each one, so a record here is
// the cockpit's unit of work per frame: the cost of folding a message into
// state (Update) plus the cost of turning that state into a screenful of
// bytes (View).
type frameRecord struct {
	Seq       int64   `json:"seq"`
	AtMS      float64 `json:"at_ms"`     // ms since profiler start
	Msg       string  `json:"msg"`       // concrete tea.Msg type
	UpdateUS  float64 `json:"update_us"` // Update() duration, microseconds
	ViewUS    float64 `json:"view_us"`   // View() duration, microseconds
	ViewBytes int     `json:"view_bytes"`
}

// profiler decorates a tea.Model, timing each Update and View. It is a
// tea.Model itself, so bubbletea cannot tell the difference.
//
// The timed render is cached and handed back to bubbletea's own View() call.
// Rendering inside Update and then letting bubbletea render a second time
// would double the cockpit's real render cost and corrupt the measurement, so
// the profiled path performs exactly one inner.View() per message — the same
// count as the unprofiled path.
type profiler struct {
	inner  tea.Model
	sink   *frameSink
	cached string
	fresh  bool
}

// frameSink serializes records to disk off the hot path. Encoding happens
// after both timers have stopped, so it never contaminates a measurement —
// though it does add a small constant to real wall-clock frame cost. Expect
// single-digit microseconds per frame of profiler overhead.
type frameSink struct {
	mu     sync.Mutex
	w      *bufio.Writer
	f      *os.File
	enc    *json.Encoder
	start  time.Time
	seq    int64
	closed bool
}

func newFrameSink(path string) (*frameSink, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	w := bufio.NewWriterSize(f, 1<<20)
	s := &frameSink{w: w, f: f, enc: json.NewEncoder(w), start: time.Now()}

	// Guard against the session being killed rather than quit cleanly: flush
	// on a slow ticker so a SIGKILL costs at most a couple of seconds of data.
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for range t.C {
			s.mu.Lock()
			if s.closed {
				s.mu.Unlock()
				return
			}
			_ = s.w.Flush()
			s.mu.Unlock()
		}
	}()
	return s, nil
}

func (s *frameSink) record(msg string, update, view time.Duration, viewBytes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.seq++
	_ = s.enc.Encode(frameRecord{
		Seq:       s.seq,
		AtMS:      float64(time.Since(s.start).Microseconds()) / 1000,
		Msg:       msg,
		UpdateUS:  float64(update.Nanoseconds()) / 1000,
		ViewUS:    float64(view.Nanoseconds()) / 1000,
		ViewBytes: viewBytes,
	})
}

func (s *frameSink) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	_ = s.w.Flush()
	_ = s.f.Close()
}

func (p profiler) Init() tea.Cmd { return p.inner.Init() }

func (p profiler) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	start := time.Now()
	next, cmd := p.inner.Update(msg)
	update := time.Since(start)

	p.inner = next

	// View is timed here rather than in View() so the render cost is
	// attributed to the message that caused it. The result is cached for the
	// View() call bubbletea is about to make.
	viewStart := time.Now()
	out := p.inner.View()
	view := time.Since(viewStart)

	p.cached = out
	p.fresh = true

	p.sink.record(fmt.Sprintf("%T", msg), update, view, len(out))

	if _, quitting := msg.(tea.QuitMsg); quitting {
		p.sink.close()
	}
	return p, cmd
}

// View serves the render already performed (and timed) in Update. Only the
// very first paint, which bubbletea requests before any message arrives, falls
// through to the inner model.
func (p profiler) View() string {
	if p.fresh {
		return p.cached
	}
	return p.inner.View()
}

// WithProfiling wraps m when ORUN_TUI_PROFILE is set, and otherwise returns m
// untouched. A profiler that cannot open its output file is a hard error: a
// silently-unprofiled run that looks profiled is worse than no run at all.
func WithProfiling(m tea.Model) tea.Model {
	path := os.Getenv(ProfileEnvVar)
	if path == "" {
		return m
	}
	sink, err := newFrameSink(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "orun: %s=%s: cannot open profile output: %v\n", ProfileEnvVar, path, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "orun: TUI frame profile → %s\n", path)
	return profiler{inner: m, sink: sink}
}
