package work

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/sourceplane/orun/internal/tui2/data"
	"github.com/sourceplane/orun/internal/tui2/frame"
	"github.com/sourceplane/orun/internal/worklens"
)

func runes(s string) tea.KeyMsg        { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func keyType(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func loaded(t *testing.T) *Surface {
	t.Helper()
	m := data.SampleMock()
	s := New(m)
	epics, _ := m.Work(nil)
	s.Update(workMsg{epics: epics})
	sess, _ := m.Sessions(nil)
	s.Update(sessionsMsg{entries: sess})
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

func TestListShowsSealedEpics(t *testing.T) {
	s := loaded(t)
	out := view(t, s, 100, 30)
	for _, want := range []string{"Checkout rework", "sealed", "2 milestones", "3 tasks"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list missing %q:\n%s", want, out)
		}
	}
}

func TestEpicDetailLadderAndSessionLink(t *testing.T) {
	s := loaded(t)
	s.HandleKey(keyType(tea.KeyEnter))
	out := view(t, s, 110, 40)
	for _, want := range []string{
		"SPEC-12", "M0 — Extract payment intent", "M1 — Cutover",
		"intent API behind flag", "ORN-142", "Cutover checklist",
		"sealed snapshot", "live work plane lights up when signed in",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("epic detail missing %q:\n%s", want, out)
		}
	}
	// The session working "fix flaky catalog test" contains no task key;
	// links join on the task key appearing in the session task string.
	if strings.Contains(out, "working") {
		t.Fatal("no session mentions a task key; no link should render")
	}
}

func TestSessionLinkJoinsOnTaskKey(t *testing.T) {
	m := data.SampleMock()
	s := New(m)
	epics, _ := m.Work(nil)
	s.Update(workMsg{epics: epics})
	sess, _ := m.Sessions(nil)
	sess[0].Task = "ORN-142: fix flaky catalog test"
	s.Update(sessionsMsg{entries: sess})
	s.HandleKey(keyType(tea.KeyEnter))
	out := view(t, s, 110, 40)
	if !strings.Contains(out, "implementer working") {
		t.Fatalf("session link missing:\n%s", out)
	}
}

func TestBriefRendersMarkdown(t *testing.T) {
	s := loaded(t)
	s.HandleKey(keyType(tea.KeyEnter))
	s.HandleKey(runes("b"))
	out := view(t, s, 100, 30)
	if !strings.Contains(out, "Checkout rework") || !strings.Contains(out, "Sealed brief body") {
		t.Fatalf("brief missing:\n%s", out)
	}
	// esc: brief → epic → list.
	if !s.Pop() || !s.Pop() || s.Pop() {
		t.Fatal("pop chain must be brief→epic→list, then stop")
	}
}

// TestLocalSourceReadsSealedSnapshots exercises the real read path over a
// synthesized .orun/epics tree.
func TestLocalSourceReadsSealedSnapshots(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "epics", "demo-epic")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	snap := worklens.EpicSnapshot{
		Kind: "EpicSnapshot",
		Spec: worklens.Spec{Key: "SPEC-1", Title: "Demo"},
		Tasks: []worklens.Task{
			{Key: "T-1", Title: "do the thing"},
		},
	}
	raw, _ := json.Marshal(snap)
	if err := os.WriteFile(filepath.Join(dir, "snapshot.json"), raw, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "BRIEF.md"), []byte("# Demo\nbody"), 0o444); err != nil {
		t.Fatal(err)
	}
	// A malformed sibling must be skipped, not fatal.
	bad := filepath.Join(root, "epics", "broken")
	_ = os.MkdirAll(bad, 0o755)
	_ = os.WriteFile(filepath.Join(bad, "snapshot.json"), []byte("{nope"), 0o644)

	src := data.NewLocal(data.LocalConfig{OrunRoot: root, WorkspaceRoot: root})
	defer src.Close()
	epics, err := src.Work(context.Background())
	if err != nil {
		t.Fatalf("work: %v", err)
	}
	if len(epics) != 1 || epics[0].Snapshot.Spec.Key != "SPEC-1" || epics[0].Brief == "" {
		t.Fatalf("epics = %+v", epics)
	}
}

func TestViewStableAcrossSizes(t *testing.T) {
	s := loaded(t)
	sizes := []frame.Size{{Width: 40, Height: 8}, {Width: 100, Height: 30}, {Width: 220, Height: 60}}
	for _, sz := range sizes {
		if err := frame.Check(s.View(sz), sz); err != nil {
			t.Fatalf("list %+v: %v", sz, err)
		}
	}
	s.HandleKey(keyType(tea.KeyEnter))
	for _, sz := range sizes {
		if err := frame.Check(s.View(sz), sz); err != nil {
			t.Fatalf("epic %+v: %v", sz, err)
		}
	}
}

// TestHostileSnapshotRendersInert: sealed snapshots are remote-originated
// content (pulled from the cloud) — control bytes must die at render.
func TestHostileSnapshotRendersInert(t *testing.T) {
	m := data.SampleMock()
	m.Epics[0].Snapshot.Spec.Title = "evil\x1b]0;owned\x07title\x1b[2J"
	s := New(m)
	epics, _ := m.Work(nil)
	s.Update(workMsg{epics: epics})
	raw := s.View(frame.Size{Width: 100, Height: 30})
	if strings.Contains(raw, "\x07") || strings.Contains(raw, "]0;") || strings.Contains(raw, "[2J") {
		t.Fatalf("control bytes leaked: %q", raw)
	}
}
