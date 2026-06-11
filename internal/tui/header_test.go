package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/theme"
)

// The animated wordmark must be pure color animation: identical visible text
// and width at every phase, so the shimmer can never reshape the header line.
func TestBrandMark_StableWidthAcrossPhases(t *testing.T) {
	// Tests run without a TTY, where lipgloss downgrades to the no-color
	// profile and every phase would render identically; force colors so the
	// animation is observable.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	w0 := lipgloss.Width(theme.BrandMark(0))
	if w0 == 0 {
		t.Fatal("brand mark renders empty")
	}
	var distinct bool
	for phase := 1; phase < 16; phase++ {
		mark := theme.BrandMark(phase)
		if w := lipgloss.Width(mark); w != w0 {
			t.Fatalf("phase %d: width %d, want %d", phase, w, w0)
		}
		if mark != theme.BrandMark(0) {
			distinct = true
		}
	}
	if !distinct {
		t.Fatal("phases all render identically — the shimmer is not animating")
	}
}

// The brand ticker advances the phase and re-arms.
func TestModel_BrandTickAdvances(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	before := m.brandPhase
	next, cmd := m.Update(brandTickMsg{})
	m = next.(Model)
	if m.brandPhase != before+1 {
		t.Fatalf("brandPhase = %d, want %d", m.brandPhase, before+1)
	}
	if cmd == nil {
		t.Fatal("brand tick must re-arm")
	}
}

// The header's second line surfaces the workspace's top-level source context
// and the catalog identity; without either, the header stays one line.
func TestModel_HeaderDetailLine(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = next.(Model)

	if h := lipgloss.Height(m.renderHeader()); h != 1 {
		t.Fatalf("header with no workspace/catalog should be 1 line, got %d", h)
	}

	ws := &services.WorkspaceSnapshot{
		IntentName: "multi-tenant-saas",
		Source: services.SourceInfo{
			Repo: "sourceplane/multi-tenant-saas", Branch: "main", Head: "a1b2c3d", Dirty: false,
		},
	}
	next, _ = m.Update(services.WorkspaceLoadedMsg{Snapshot: ws})
	m = next.(Model)
	next, _ = m.Update(services.CatalogLoadedMsg{Snapshot: modeTestSnapshot()})
	m = next.(Model)

	header := m.renderHeader()
	if h := lipgloss.Height(header); h != 2 {
		t.Fatalf("header height = %d, want 2 (brand line + detail line)", h)
	}
	for _, want := range []string{"branch", "main", "head", "a1b2c3d", "clean", "catalog", "cat-test", "2 entities"} {
		if !strings.Contains(header, want) {
			t.Errorf("header missing %q:\n%s", want, header)
		}
	}

	// A dirty tree flips the pill.
	ws.Source.Dirty = true
	next, _ = m.Update(services.WorkspaceLoadedMsg{Snapshot: ws})
	m = next.(Model)
	if header := m.renderHeader(); !strings.Contains(header, "dirty") {
		t.Error("dirty tree should surface a dirty pill")
	}

	// The detail line never exceeds the terminal width.
	for _, line := range strings.Split(m.renderHeader(), "\n") {
		if lw := lipgloss.Width(line); lw > 160 {
			t.Errorf("header line width %d exceeds terminal: %q", lw, line)
		}
	}
}
