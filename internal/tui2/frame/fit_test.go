package frame

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// fragments deliberately mix plain ASCII, ANSI-styled runs, double-width
// CJK, and emoji — the inputs that historically broke width math.
var fragments = []string{
	"",
	"plain text",
	"\x1b[31mred\x1b[0m",
	"\x1b[1;38;5;208mbold orange no reset",
	"日本語テキスト",
	"mixed 日本 and \x1b[32mgreen\x1b[0m",
	"🚀 emoji ⣾ braille",
	strings.Repeat("wide 幅 ", 40),
}

func randContent(r *rand.Rand) string {
	n := r.Intn(12)
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fragments[r.Intn(len(fragments))]
	}
	return strings.Join(lines, "\n")
}

// TestFitExactDims is the exact-size contract as a property: any content at
// any size renders to exactly that size.
func TestFitExactDims(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 2000; i++ {
		size := Size{Width: r.Intn(60), Height: r.Intn(12)}
		out := Fit(randContent(r), size)
		if size.Empty() {
			if out != "" {
				t.Fatalf("iter %d: non-empty output for empty size %+v", i, size)
			}
			continue
		}
		if err := Check(out, size); err != nil {
			t.Fatalf("iter %d: size %+v: %v", i, size, err)
		}
	}
}

func TestFitLineWideRuneBoundary(t *testing.T) {
	// Truncating "日本" at width 3 cuts mid-rune; the result must still be
	// exactly 3 cells.
	got := FitLine("日本", 3)
	if w := ansi.StringWidth(got); w != 3 {
		t.Fatalf("width = %d, want 3 (%q)", w, got)
	}
}

func TestCheckRejectsBadFrames(t *testing.T) {
	if err := Check("a\nb", Size{Width: 1, Height: 3}); err == nil {
		t.Fatal("want height error")
	}
	if err := Check("ab\ncd", Size{Width: 1, Height: 2}); err == nil {
		t.Fatal("want width error")
	}
}

func TestComposeKeepsFrameStable(t *testing.T) {
	size := Size{Width: 24, Height: 8}
	base := Fit(strings.Repeat("base content here\n", 8), size)
	box := Fit("┌────┐\n│ hi │\n└────┘", Size{Width: 6, Height: 3})

	out := Compose(base, box, size)
	if err := Check(out, size); err != nil {
		t.Fatalf("composed frame unstable: %v", err)
	}
	if !strings.Contains(ansi.Strip(out), "│ hi │") {
		t.Fatalf("overlay content missing:\n%s", ansi.Strip(out))
	}
}

func TestComposeOversizedBoxClampsToStage(t *testing.T) {
	size := Size{Width: 10, Height: 3}
	base := Fit("x", size)
	box := Fit(strings.Repeat("very long overlay line\n", 6), Size{Width: 40, Height: 6})
	out := Compose(base, box, size)
	if err := Check(out, size); err != nil {
		t.Fatalf("oversized overlay destabilized frame: %v", err)
	}
}

func TestComposeStyledBaseDoesNotBleed(t *testing.T) {
	size := Size{Width: 20, Height: 3}
	base := Fit("\x1b[7mreverse video everywhere\x1b[0m\n\x1b[7mreverse\x1b[0m\n\x1b[7mreverse\x1b[0m", size)
	box := "plain"
	out := Compose(base, Fit(box, Size{Width: 5, Height: 1}), size)
	if err := Check(out, size); err != nil {
		t.Fatalf("styled compose unstable: %v", err)
	}
}
