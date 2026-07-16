package events

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/sourceplane/orun/internal/tui2/data"
	"github.com/sourceplane/orun/internal/tui2/frame"
)

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func loaded(t *testing.T) *Surface {
	t.Helper()
	m := data.SampleMock()
	s := New(m)
	s.Update(s.loadCmd()().(snapshotMsg))
	return s
}

func view(t *testing.T, s *Surface, w, h int) string {
	t.Helper()
	out := s.View(frame.Size{Width: w, Height: h})
	if err := frame.Check(out, frame.Size{Width: w, Height: h}); err != nil {
		t.Fatalf("frame invariant: %v", err)
	}
	return ansi.Strip(out)
}

func TestFeedFoldsRunsAndSessions(t *testing.T) {
	s := loaded(t)
	out := view(t, s, 120, 40)
	for _, want := range []string{"run · deploy checkout", "session · implementer", "session · reviewer"} {
		if !strings.Contains(out, want) {
			t.Fatalf("feed missing %q:\n%s", want, out)
		}
	}
}

func TestFacetFilters(t *testing.T) {
	s := loaded(t)
	s.HandleKey(runes("f")) // runs only
	out := view(t, s, 120, 40)
	if strings.Contains(out, "session ·") {
		t.Fatalf("runs facet must hide sessions:\n%s", out)
	}
	s.HandleKey(runes("f")) // sessions only
	out = view(t, s, 120, 40)
	if strings.Contains(out, "run ·") {
		t.Fatalf("sessions facet must hide runs:\n%s", out)
	}
}

func TestEmptyStateHonest(t *testing.T) {
	m := data.NewMock()
	s := New(m)
	s.Update(s.loadCmd()().(snapshotMsg))
	out := view(t, s, 100, 30)
	if !strings.Contains(out, "org event bus arrives when signed in") {
		t.Fatalf("empty state must explain the cloud lane:\n%s", out)
	}
}

func TestViewStableAcrossSizes(t *testing.T) {
	s := loaded(t)
	for _, sz := range []frame.Size{{Width: 40, Height: 8}, {Width: 100, Height: 30}, {Width: 220, Height: 60}} {
		if err := frame.Check(s.View(sz), sz); err != nil {
			t.Fatalf("%+v: %v", sz, err)
		}
	}
}
