// Package style is the single source of truth for Orun's design tokens.
//
// Both the CLI surfaces (internal/ui, cmd/orun) and the TUI (internal/tui)
// consume these constants so the cockpit looks identical whether you run
// `orun status` or `orun tui`.
//
// The package intentionally has no Lipgloss or Bubble Tea dependency — TUI
// code wraps these tokens into lipgloss.Style values in
// internal/tui/theme, and CLI code maps them onto ANSI escapes via
// internal/ui. Both wrappers must stay faithful to the constants here.
package style

// Glyph is a single visible character used as a status or scope marker.
// Glyphs are intentionally unicode-only; the cockpit assumes a modern
// terminal font. NO_COLOR strips colour but never strips glyphs.
type Glyph = string

// Status / lifecycle glyphs. Kept short so they align in dense tables.
const (
	GlyphSuccess  Glyph = "✓"
	GlyphFailure  Glyph = "✗"
	GlyphRunning  Glyph = "◐"
	GlyphPending  Glyph = "○"
	GlyphSkipped  Glyph = "↷"
	GlyphDryRun   Glyph = "◌"
	GlyphResumed  Glyph = "⚡"
	GlyphChanged  Glyph = "●"
	GlyphBullet   Glyph = "●"
	GlyphRetry    Glyph = "↻"
	GlyphArrowR   Glyph = "→"
)

// Brand marks: the wedge that opens every cockpit header line.
const (
	BrandWedge Glyph = "▲"
	BrandName        = "orun"
)

// Separators kept as data so layouts share spacing rules.
const (
	SepInline = " · "  // inline: "Plan a1b2 · Run xyz · State running"
	SepDot    = "·"    // tight inline separator
	SepRule   = "  ─  "
)

// Tree connectors for grouped output (Active region, finished components,
// failure footers). Match the existing internal/ui live region.
const (
	TreeBranch = "├─"
	TreeLast   = "└─"
	TreeVert   = "│"
)

// Token is the abstract palette role. Concrete colour resolution lives in
// internal/ui (ANSI codes) and internal/tui/theme (lipgloss.AdaptiveColor)
// — both packages keep these mappings in sync.
type Token int

const (
	TokenFG Token = iota
	TokenDim
	TokenMuted
	TokenBrand
	TokenBrandSoft
	TokenSecondary
	TokenSuccess
	TokenWarning
	TokenError
	TokenRunning
	TokenPending
)

// String returns the canonical token name for debugging.
func (t Token) String() string {
	switch t {
	case TokenFG:
		return "fg"
	case TokenDim:
		return "dim"
	case TokenMuted:
		return "muted"
	case TokenBrand:
		return "brand"
	case TokenBrandSoft:
		return "brand-soft"
	case TokenSecondary:
		return "secondary"
	case TokenSuccess:
		return "success"
	case TokenWarning:
		return "warning"
	case TokenError:
		return "error"
	case TokenRunning:
		return "running"
	case TokenPending:
		return "pending"
	}
	return "unknown"
}

// StatusToken maps a lifecycle status string to its palette token.
// Statuses are matched case-insensitively against the canonical set used by
// internal/state and internal/runner.
func StatusToken(status string) Token {
	switch normalizeStatus(status) {
	case "completed", "success", "ok":
		return TokenSuccess
	case "failed", "error":
		return TokenError
	case "running", "in_progress":
		return TokenRunning
	case "pending", "queued", "waiting":
		return TokenPending
	case "skipped":
		return TokenWarning
	}
	return TokenMuted
}

// StatusGlyph returns the canonical glyph for a status string. Returns the
// pending glyph for empty / unknown statuses so renderers never have to
// branch on emptiness.
func StatusGlyph(status string) Glyph {
	switch normalizeStatus(status) {
	case "completed", "success", "ok":
		return GlyphSuccess
	case "failed", "error":
		return GlyphFailure
	case "running", "in_progress":
		return GlyphRunning
	case "skipped":
		return GlyphSkipped
	case "pending", "queued", "waiting":
		return GlyphPending
	}
	return GlyphPending
}

// StatusLabel normalises status text for headline display ("running",
// "completed", "failed", "pending"). Unknown statuses pass through trimmed
// and lowercased so callers never surface raw backend strings.
func StatusLabel(status string) string {
	s := normalizeStatus(status)
	if s == "" {
		return "unknown"
	}
	return s
}

// Palette is the canonical hex colour table for Orun's design tokens.
//
// Both the TUI (internal/tui/theme via lipgloss.AdaptiveColor) and any
// future themed CLI surface read from this struct so the cockpit can be
// reskinned in one place. Light/Dark pairs are required even for tokens
// the TUI currently renders identically — terminals may flip.
type Palette struct {
	FG, FGDark             string
	Dim, DimDark           string
	Muted, MutedDark       string
	Brand, BrandDark       string // accent (violet)
	BrandSoft, BrandSoftDark string
	Secondary, SecondaryDark string
	Success, SuccessDark   string
	Warning, WarningDark   string
	Error, ErrorDark       string
	Border, BorderDark     string
	RowAlt, RowAltDark     string
	RowSel, RowSelDark     string
}

// DefaultPalette is the soft modern accent system inspired by Claude
// Code / Linear. Mutating fields at runtime is unsupported — define a
// fresh Palette instead.
var DefaultPalette = Palette{
	FG: "#1f2933", FGDark: "#e2e8f0",
	Dim: "#64748b", DimDark: "#64748b",
	Muted: "#94a3b8", MutedDark: "#475569",
	Brand: "#7c3aed", BrandDark: "#a78bfa",
	BrandSoft: "#c4b5fd", BrandSoftDark: "#6d28d9",
	Secondary: "#0891b2", SecondaryDark: "#22d3ee",
	Success: "#059669", SuccessDark: "#34d399",
	Warning: "#b45309", WarningDark: "#fbbf24",
	Error: "#dc2626", ErrorDark: "#f87171",
	Border: "#cbd5e1", BorderDark: "#334155",
	RowAlt: "#f1f5f9", RowAltDark: "#1e293b",
	RowSel: "#ede9fe", RowSelDark: "#312e81",
}

func normalizeStatus(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			continue
		case c >= 'A' && c <= 'Z':
			out = append(out, c+('a'-'A'))
		default:
			out = append(out, c)
		}
	}
	return string(out)
}
