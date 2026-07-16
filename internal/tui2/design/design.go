// Package design is Northwind Mono — the terminal projection of orun
// cloud's Northwind design system (specs/orun-tui-v2/design.md §4).
//
// Temperament: calm, editorial, precise. Whitespace over borders; hierarchy
// through weight and tone, never size; color is information, not decoration.
// The closed Tone set below maps 1:1 to the console's Tone vocabulary, and
// every color resolves through internal/cockpit/style.DefaultPalette — the
// single reskin point shared with the CLI and the v1 cockpit.
//
// This package is the only place cockpit v2 defines styles. Surfaces compose
// these components; they do not call lipgloss directly.
package design

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/sourceplane/orun/internal/cockpit/style"
)

// Tone is the closed semantic color channel (console parity: `Tone` in
// apps/web-console-next). Nothing outside this set carries meaning through
// color.
type Tone int

const (
	ToneNeutral Tone = iota
	ToneInfo
	ToneSuccess
	ToneWarning
	ToneError
	// ToneLive marks running work — the only tone that may animate.
	ToneLive
)

// String returns the canonical tone name.
func (t Tone) String() string {
	switch t {
	case ToneInfo:
		return "info"
	case ToneSuccess:
		return "success"
	case ToneWarning:
		return "warning"
	case ToneError:
		return "error"
	case ToneLive:
		return "live"
	}
	return "neutral"
}

func adaptive(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

var pal = style.DefaultPalette

// The foundational styles. Names follow role, not appearance.
var (
	// Text is default foreground.
	Text = lipgloss.NewStyle().Foreground(adaptive(pal.FG, pal.FGDark))
	// Dim is supporting metadata.
	Dim = lipgloss.NewStyle().Foreground(adaptive(pal.Dim, pal.DimDark))
	// Muted is barely-there scaffolding (rules, placeholders).
	Muted = lipgloss.NewStyle().Foreground(adaptive(pal.Muted, pal.MutedDark))
	// Accent is the brand violet: the wordmark and the active-surface
	// indicator. Two places, no more (design §4a).
	Accent = lipgloss.NewStyle().Foreground(adaptive(pal.Brand, pal.BrandDark))
	// Title is emphasized foreground for headings and active tabs.
	Title = lipgloss.NewStyle().Bold(true).Foreground(adaptive(pal.FG, pal.FGDark))
	// Ref renders identifiers — digests, run ids, entity keys.
	Ref = lipgloss.NewStyle().Foreground(adaptive(pal.Secondary, pal.SecondaryDark))
	// Selected marks the focused row.
	Selected = lipgloss.NewStyle().Bold(true).Foreground(adaptive(pal.Brand, pal.BrandDark))
)

// toneStyles resolves each tone to its style once.
var toneStyles = map[Tone]lipgloss.Style{
	ToneNeutral: Dim,
	ToneInfo:    lipgloss.NewStyle().Foreground(adaptive(pal.Secondary, pal.SecondaryDark)),
	ToneSuccess: lipgloss.NewStyle().Foreground(adaptive(pal.Success, pal.SuccessDark)),
	ToneWarning: lipgloss.NewStyle().Foreground(adaptive(pal.Warning, pal.WarningDark)),
	ToneError:   lipgloss.NewStyle().Foreground(adaptive(pal.Error, pal.ErrorDark)),
	ToneLive:    lipgloss.NewStyle().Foreground(adaptive(pal.Brand, pal.BrandDark)),
}

// Style returns the tone's style.
func (t Tone) Style() lipgloss.Style { return toneStyles[t] }

// StatusTone maps a lifecycle status string (internal/state, internal/runner
// vocabulary) onto the tone channel, mirroring style.StatusToken.
func StatusTone(status string) Tone {
	switch style.StatusToken(status) {
	case style.TokenSuccess:
		return ToneSuccess
	case style.TokenError:
		return ToneError
	case style.TokenRunning:
		return ToneLive
	case style.TokenWarning:
		return ToneWarning
	case style.TokenPending:
		return ToneNeutral
	}
	return ToneNeutral
}
