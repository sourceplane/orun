package catalog

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/sourceplane/orun/internal/tui2/data"
	"github.com/sourceplane/orun/internal/tui2/frame"
	"github.com/sourceplane/orun/internal/tui2/shell"
)

func runes(s string) tea.KeyMsg        { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func keyType(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func loaded(t *testing.T) (*Surface, *data.MockSource, *data.MockComposer) {
	t.Helper()
	m := data.SampleMock()
	c := data.SampleComposer()
	s := New(m, c)
	v, _ := m.Catalog(nil)
	s.Update(catalogMsg{v: v})
	return s, m, c
}

func view(t *testing.T, s *Surface, w, h int) string {
	t.Helper()
	out := s.View(frame.Size{Width: w, Height: h})
	if err := frame.Check(out, frame.Size{Width: w, Height: h}); err != nil {
		t.Fatalf("frame invariant: %v", err)
	}
	return ansi.Strip(out)
}

func TestListRendersWithOverlayBadges(t *testing.T) {
	s, _, _ := loaded(t)
	out := view(t, s, 100, 30)
	for _, want := range []string{"checkout", "payments", "web", "changed", "affected"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list missing %q:\n%s", want, out)
		}
	}
}

func TestChangedOnlyFilter(t *testing.T) {
	s, _, _ := loaded(t)
	s.HandleKey(runes("c"))
	out := view(t, s, 100, 30)
	if strings.Contains(out, "web") {
		t.Fatalf("changed-only must hide unchanged rows:\n%s", out)
	}
	for _, want := range []string{"checkout", "payments"} {
		if !strings.Contains(out, want) {
			t.Fatalf("changed-only lost %q", want)
		}
	}
}

func TestDetailDrill(t *testing.T) {
	s, _, _ := loaded(t)
	cmd, _ := s.HandleKey(keyType(tea.KeyEnter)) // checkout
	if cmd == nil {
		t.Fatal("detail must load")
	}
	s.Update(cmd().(componentMsg))
	out := view(t, s, 100, 30)
	for _, want := range []string{"services/checkout", "production", "payments-db"} {
		if !strings.Contains(out, want) {
			t.Fatalf("detail missing %q:\n%s", want, out)
		}
	}
	if !s.Pop() || s.Pop() {
		t.Fatal("detail pops exactly once")
	}
}

// TestComposeFlow drives g → preview → R → confirm → dispatch → events →
// run_done, the whole Plan Studio replacement.
func TestComposeFlow(t *testing.T) {
	s, _, c := loaded(t)

	cmd, _ := s.HandleKey(runes("g"))
	if cmd == nil {
		t.Fatal("compose must generate")
	}
	s.Update(cmd().(planMsg))
	out := view(t, s, 100, 30)
	for _, want := range []string{"compose", "checkout", "a1b2c3d4", "2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("preview missing %q:\n%s", want, out)
		}
	}

	// R opens the confirm; its verb names the action.
	cmd, _ = s.HandleKey(runes("R"))
	if cmd == nil {
		t.Fatal("R must open the confirm dialog")
	}
	ovMsg, ok := cmd().(shell.OpenOverlayMsg)
	if !ok {
		t.Fatalf("expected overlay msg, got %T", cmd())
	}
	dialog := ansi.Strip(ovMsg.Overlay.View(frame.Size{Width: 80, Height: 20}))
	if !strings.Contains(dialog, "Run 2 jobs") {
		t.Fatalf("confirm must name its verb:\n%s", dialog)
	}

	// Enter confirms → dispatch begins, events stream.
	runCmd, done := ovMsg.Overlay.HandleKey(keyType(tea.KeyEnter))
	if !done || runCmd == nil {
		t.Fatal("enter must dispatch")
	}
	msg := runCmd()
	if batch, isBatch := msg.(tea.BatchMsg); isBatch {
		for _, bc := range batch {
			if bc == nil {
				continue
			}
			m := bc()
			if _, isPump := m.(runEventPump); isPump {
				msg = m
			}
		}
	}
	// Pump all events through Update.
	for {
		pump, isPump := msg.(runEventPump)
		if !isPump {
			break
		}
		next := s.Update(pump)
		if next == nil {
			break
		}
		msg = next()
		if ev, isEv := msg.(runEventMsg); isEv {
			s.Update(ev)
			break
		}
	}

	if len(c.DispatchedDry) != 1 || c.DispatchedDry[0] {
		t.Fatalf("dispatch must be a real run: %v", c.DispatchedDry)
	}
	out = view(t, s, 100, 30)
	for _, want := range []string{"checkout@deploy · build", "run finished: completed", "exec_mock"} {
		if !strings.Contains(out, want) {
			t.Fatalf("dispatch progress missing %q:\n%s", want, out)
		}
	}
	if s.dispatching {
		t.Fatal("run_done must end the dispatch")
	}
}

func TestDryRunUsesDryVerb(t *testing.T) {
	s, _, c := loaded(t)
	cmd, _ := s.HandleKey(runes("g"))
	s.Update(cmd().(planMsg))
	cmd, _ = s.HandleKey(runes("d"))
	ovMsg := cmd().(shell.OpenOverlayMsg)
	dialog := ansi.Strip(ovMsg.Overlay.View(frame.Size{Width: 80, Height: 20}))
	if !strings.Contains(dialog, "Dry-run 2 jobs") {
		t.Fatalf("dry verb missing:\n%s", dialog)
	}
	runCmd, _ := ovMsg.Overlay.HandleKey(keyType(tea.KeyEnter))
	_ = runCmd()
	if len(c.DispatchedDry) != 1 || !c.DispatchedDry[0] {
		t.Fatalf("d must dispatch dry: %v", c.DispatchedDry)
	}
}

// TestStalePlanIgnored: a generation result for a different component (the
// user moved on) cannot land.
func TestStalePlanIgnored(t *testing.T) {
	s, _, _ := loaded(t)
	cmd, _ := s.HandleKey(runes("g")) // compose checkout
	stale := planMsg{key: "web", p: data.PlanPreview{Checksum: "stale"}}
	s.Update(stale)
	if s.preview.Checksum == "stale" {
		t.Fatal("stale plan landed")
	}
	s.Update(cmd().(planMsg))
	if s.preview.Checksum != "a1b2c3d4" {
		t.Fatalf("fresh plan lost: %q", s.preview.Checksum)
	}
}

func TestNilComposerHidesCompose(t *testing.T) {
	m := data.SampleMock()
	s := New(m, nil)
	v, _ := m.Catalog(nil)
	s.Update(catalogMsg{v: v})
	cmd, _ := s.HandleKey(runes("g"))
	if cmd != nil || s.level != levelList {
		t.Fatal("g must be inert without a composer")
	}
	if strings.Contains(view(t, s, 100, 30), "compose") {
		t.Fatal("compose hint must hide without a composer")
	}
}

func TestViewStableAcrossSizes(t *testing.T) {
	s, _, _ := loaded(t)
	sizes := []frame.Size{{Width: 40, Height: 8}, {Width: 100, Height: 30}, {Width: 220, Height: 60}}
	for _, sz := range sizes {
		if err := frame.Check(s.View(sz), sz); err != nil {
			t.Fatalf("list %+v: %v", sz, err)
		}
	}
	cmd, _ := s.HandleKey(runes("g"))
	s.Update(cmd().(planMsg))
	for _, sz := range sizes {
		if err := frame.Check(s.View(sz), sz); err != nil {
			t.Fatalf("compose %+v: %v", sz, err)
		}
	}
}
