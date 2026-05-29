package style

import "testing"

func TestStatusGlyph(t *testing.T) {
	cases := map[string]Glyph{
		"completed":   GlyphSuccess,
		"Completed":   GlyphSuccess,
		"success":     GlyphSuccess,
		"failed":      GlyphFailure,
		"running":     GlyphRunning,
		"in_progress": GlyphRunning,
		"pending":     GlyphPending,
		"queued":      GlyphPending,
		"skipped":     GlyphSkipped,
		"":            GlyphPending,
		"weird":       GlyphPending,
	}
	for in, want := range cases {
		if got := StatusGlyph(in); got != want {
			t.Errorf("StatusGlyph(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStatusToken(t *testing.T) {
	cases := map[string]Token{
		"completed": TokenSuccess,
		"FAILED":    TokenError,
		"running":   TokenRunning,
		"pending":   TokenPending,
		"skipped":   TokenWarning,
		"":          TokenMuted,
	}
	for in, want := range cases {
		if got := StatusToken(in); got != want {
			t.Errorf("StatusToken(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestStatusLabel(t *testing.T) {
	if got := StatusLabel("  RUNNING  "); got != "running" {
		t.Errorf("StatusLabel trim/lower failed: %q", got)
	}
	if got := StatusLabel(""); got != "unknown" {
		t.Errorf("StatusLabel empty: %q", got)
	}
}
