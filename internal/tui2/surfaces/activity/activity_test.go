package activity

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
	"github.com/sourceplane/orun/internal/tui2/data"
	"github.com/sourceplane/orun/internal/tui2/frame"
)

func runes(s string) tea.KeyMsg        { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func keyType(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func loaded(t *testing.T) (*Surface, *data.MockSource) {
	t.Helper()
	m := data.SampleMock()
	s := New(m)
	v, _ := m.Runs(nil)
	s.Update(runsMsg{v: v})
	return s, m
}

func view(t *testing.T, s *Surface, w, h int) string {
	t.Helper()
	out := s.View(frame.Size{Width: w, Height: h})
	if err := frame.Check(out, frame.Size{Width: w, Height: h}); err != nil {
		t.Fatalf("frame invariant: %v", err)
	}
	return ansi.Strip(out)
}

// drill descends feed → run → job → log against the sample fixtures.
func drill(t *testing.T, s *Surface, m *data.MockSource) {
	t.Helper()
	cmd, _ := s.HandleKey(keyType(tea.KeyEnter)) // exec_01J8Z3 is row 0
	if cmd == nil {
		t.Fatal("drill must load the run")
	}
	s.Update(cmd().(runMsg))
	s.HandleKey(runes("j")) // payments@deploy (running)
	s.HandleKey(keyType(tea.KeyEnter))
	s.HandleKey(runes("j")) // migrate step
	cmd, _ = s.HandleKey(keyType(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("step must load its log")
	}
	s.Update(cmd().(logMsg))
}

func TestFeedRendersAndFilters(t *testing.T) {
	s, _ := loaded(t)
	out := view(t, s, 100, 30)
	for _, want := range []string{"deploy checkout", "plan payments", "deploy web", "running", "failed"} {
		if !strings.Contains(out, want) {
			t.Fatalf("feed missing %q:\n%s", want, out)
		}
	}
	// f cycles to running-only.
	s.HandleKey(runes("f"))
	out = view(t, s, 100, 30)
	if strings.Contains(out, "deploy web") {
		t.Fatalf("running facet must hide the failed run:\n%s", out)
	}
	if !strings.Contains(out, "deploy checkout") {
		t.Fatal("running facet must keep the running run")
	}
}

func TestDrillToLog(t *testing.T) {
	s, m := loaded(t)
	drill(t, s, m)
	out := view(t, s, 100, 30)
	if !strings.Contains(out, "applying migration 0042_add_ledger") {
		t.Fatalf("log leaf missing content:\n%s", out)
	}
	if !strings.Contains(out, "following") {
		t.Fatalf("running step must show follow marker:\n%s", out)
	}
	// esc walks back up one level at a time; the surface pops until feed.
	for i := 0; i < 3; i++ {
		if !s.Pop() {
			t.Fatalf("pop %d must succeed", i)
		}
	}
	if s.Pop() {
		t.Fatal("feed level must not pop")
	}
}

func TestErrorsOnlyFilter(t *testing.T) {
	s, m := loaded(t)
	drill(t, s, m)
	s.HandleKey(runes("e"))
	out := view(t, s, 100, 30)
	if !strings.Contains(out, "ERROR: retrying lock acquisition") {
		t.Fatalf("errors-only must keep error lines:\n%s", out)
	}
	if strings.Contains(out, "lock acquired") {
		t.Fatalf("errors-only must drop info lines:\n%s", out)
	}
}

// TestPinnedRunSurvivesBackgroundRefresh is the design §12 clobber
// property: while drilled into a run, list refreshes and stray run loads
// must not replace the pinned detail.
func TestPinnedRunSurvivesBackgroundRefresh(t *testing.T) {
	s, m := loaded(t)
	drill(t, s, m)

	// A background list refresh lands (different ordering, new rows).
	v, _ := m.Runs(nil)
	s.Update(runsMsg{v: v})
	// A stray detail for a DIFFERENT run arrives (stale command result).
	s.Update(runMsg{id: "exec_01J8Z1", v: viewmodel.RunView{ExecID: "exec_01J8Z1", Status: "failed"}})

	if s.run.ExecID != "exec_01J8Z3" {
		t.Fatalf("pinned run clobbered: now %s", s.run.ExecID)
	}
	out := view(t, s, 100, 30)
	if !strings.Contains(out, "applying migration") {
		t.Fatal("drilldown lost after background refresh")
	}
}

// TestDeltaRefreshesPinnedAndLog: a runs delta reloads the list, the
// pinned run, and the live step's log — no polling anywhere.
func TestDeltaRefreshesPinnedAndLog(t *testing.T) {
	s, m := loaded(t)
	drill(t, s, m)

	sub := s.subscribeCmd()().(subscribedMsg)
	s.Update(sub)
	m.Emit(data.TopicRuns, false)

	cmd := s.Update(deltaMsg{ok: true})
	if cmd == nil {
		t.Fatal("delta must fan out reloads")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected batch of reloads")
	}
	// Execute the batch parts; count what they produce.
	var sawRuns, sawRun, sawLog bool
	for _, c := range batch {
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case runsMsg:
			sawRuns = true
		case runMsg:
			sawRun = msg.id == "exec_01J8Z3"
		case logMsg:
			sawLog = strings.Contains(msg.key, "migrate")
		case deltaMsg:
			// the re-armed wait would block; it only fires on the next
			// emit — skip executing it here.
		}
	}
	_ = sawRuns
	if !sawRun || !sawLog {
		t.Fatalf("delta must reload pinned run and live log (run=%v log=%v)", sawRun, sawLog)
	}
}

func TestViewStableAcrossSizesAndLevels(t *testing.T) {
	s, m := loaded(t)
	sizes := []frame.Size{{Width: 40, Height: 8}, {Width: 100, Height: 30}, {Width: 220, Height: 60}}
	for _, sz := range sizes {
		if err := frame.Check(s.View(sz), sz); err != nil {
			t.Fatalf("feed %+v: %v", sz, err)
		}
	}
	drill(t, s, m)
	for _, sz := range sizes {
		if err := frame.Check(s.View(sz), sz); err != nil {
			t.Fatalf("log %+v: %v", sz, err)
		}
	}
}

// TestHostileLogRendersInert: step output is data, not terminal input.
func TestHostileLogRendersInert(t *testing.T) {
	s, m := loaded(t)
	m.StepLogs["exec_01J8Z3/payments@deploy/migrate"] = "ok\x1b]0;owned\x07\x1b[2Jboom"
	drill(t, s, m)
	raw := s.View(frame.Size{Width: 100, Height: 30})
	if strings.Contains(raw, "\x07") || strings.Contains(raw, "]0;") || strings.Contains(raw, "[2J") {
		t.Fatalf("control bytes leaked: %q", raw)
	}
}
