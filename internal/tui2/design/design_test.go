package design

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/sourceplane/orun/internal/tui2/frame"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestMain(m *testing.M) {
	// Pin the render environment: goldens must not depend on the terminal
	// the tests happen to run in.
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
	os.Exit(m.Run())
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s — run: go test ./internal/tui2/design -update", path)
	}
	if string(want) != got {
		t.Errorf("golden %s drifted.\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

// TestGalleryGoldens pins the entire kit at the three reference widths in
// dark and light (design §12).
func TestGalleryGoldens(t *testing.T) {
	for _, dark := range []bool{true, false} {
		lipgloss.SetHasDarkBackground(dark)
		mode := "light"
		if dark {
			mode = "dark"
		}
		for _, w := range []int{80, 120, 220} {
			golden(t, fmt.Sprintf("gallery-%s-%d", mode, w), Gallery(w))
		}
	}
	lipgloss.SetHasDarkBackground(true)
}

func TestDataRowExactWidth(t *testing.T) {
	for _, w := range []int{40, 80, 120} {
		row := DataRow(w, true, "checkout-service", "payments · deploy", StatusText("running"))
		if got := ansi.StringWidth(row); got != w {
			t.Fatalf("width %d: got %d", w, got)
		}
	}
	// Degenerate width still fits.
	if got := ansi.StringWidth(DataRow(12, false, "very-long-component-name", "meta", "right")); got != 12 {
		t.Fatalf("narrow row width = %d", got)
	}
}

func TestTableLinesExactWidth(t *testing.T) {
	out := Table(100,
		[]Column{{Title: "Run", Width: 0}, {Title: "Env", Width: 10}, {Title: "Status", Width: 12}},
		[][]string{{"deploy", "prod", StatusText("running")}, {"plan", "stage", StatusText("completed")}}, 1)
	for i, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w > 100 {
			t.Fatalf("line %d exceeds width: %d", i, w)
		}
	}
}

func TestSanitizeStripsControlBytes(t *testing.T) {
	hostile := "safe\x1b]0;owned\x07 text\x1b[2Jmore\x00\x08 end\ttab"
	got := Sanitize(hostile)
	for _, r := range got {
		if r < 0x20 && r != '\n' {
			t.Fatalf("control byte %q survived: %q", r, got)
		}
	}
	if !strings.Contains(got, "safe") || !strings.Contains(got, "end") {
		t.Fatalf("content lost: %q", got)
	}
}

func TestMarkdownNeutralizesHostileInput(t *testing.T) {
	out := Markdown("# title\x1b[2J\nbody with `code`\x07", 60)
	stripped := ansi.Strip(out)
	if strings.ContainsRune(stripped, 0x07) || strings.Contains(stripped, "[2J") {
		t.Fatalf("hostile bytes survived markdown: %q", out)
	}
}

func TestMarkdownWraps(t *testing.T) {
	long := strings.Repeat("word ", 40)
	for i, l := range strings.Split(Markdown(long, 40), "\n") {
		if w := ansi.StringWidth(l); w > 40 {
			t.Fatalf("line %d too wide: %d", i, w)
		}
	}
}

func TestBoxFitsWithinMax(t *testing.T) {
	max := frame.Size{Width: 40, Height: 8}
	out := Box("Title", strings.Repeat("content line that is quite long indeed\n", 10), max)
	lines := strings.Split(out, "\n")
	if len(lines) > max.Height {
		t.Fatalf("box height %d exceeds max %d", len(lines), max.Height)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > max.Width {
			t.Fatalf("box line %d width %d exceeds max %d", i, w, max.Width)
		}
	}
}

func TestDrawerExactSize(t *testing.T) {
	size := frame.Size{Width: 30, Height: 10}
	out := Drawer(size, "run 01J8Z3", [][2]string{{"status", "running"}, {"env", "production"}, {"jobs", "4"}})
	if err := frame.Check(out, size); err != nil {
		t.Fatalf("drawer unstable: %v", err)
	}
}

func TestStatusToneMapping(t *testing.T) {
	cases := map[string]Tone{
		"completed": ToneSuccess, "failed": ToneError, "running": ToneLive,
		"pending": ToneNeutral, "skipped": ToneWarning, "mystery": ToneNeutral,
	}
	for status, want := range cases {
		if got := StatusTone(status); got != want {
			t.Errorf("StatusTone(%q) = %v, want %v", status, got, want)
		}
	}
}
