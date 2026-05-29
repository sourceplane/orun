// Package surface abstracts "where the cockpit is being painted".
//
// The same view-model + render code drives:
//   - ANSISurface: interactive terminal with colour + unicode.
//   - PlainSurface: non-TTY (pipes, files) — no ANSI, glyphs preserved.
//   - JSONSurface: machine-readable; renderers append marshalled objects.
//
// A surface is responsible for:
//   - resolving cockpit style tokens into concrete output (colour or none),
//   - knowing the terminal width (so renderers can size columns / bars),
//   - writing painted lines.
package surface

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"golang.org/x/term"

	"github.com/sourceplane/orun/internal/cockpit/style"
	"github.com/sourceplane/orun/internal/ui"
)

// Surface is the cockpit's output target.
type Surface interface {
	// Write paints a single line and writes it (line is appended with \n
	// by the surface, never by callers).
	Write(line string)
	// WriteBlock atomically paints and writes a sequence of lines.
	WriteBlock(lines []string)
	// Style returns the text wrapped in the colour / weight implied by the
	// token; for plain/JSON surfaces this is a no-op pass-through.
	Style(token style.Token, text string) string
	// Bold wraps text in the surface's bold rendering.
	Bold(text string) string
	// Dim wraps text in the surface's dim rendering.
	Dim(text string) string
	// Width returns the terminal width or a sensible default (80) when
	// the surface is not a TTY.
	Width() int
	// JSON marks the surface as machine-readable; renderers should switch
	// to JSON output and skip headers/progress bars.
	JSON() bool
}

// --- ANSI surface (interactive TTY) ----------------------------------

// ANSI returns an interactive surface that emits ANSI escapes when w is a
// TTY (delegates to internal/ui colour rules).
func ANSI(w io.Writer) Surface {
	return &ansiSurface{w: w, color: ui.ColorEnabledForWriter(w)}
}

// Plain returns a non-coloured surface that still preserves glyphs. Use
// this for CI logs that are not detected as TTY but that you still want
// to be readable.
func Plain(w io.Writer) Surface {
	return &ansiSurface{w: w, color: false}
}

// JSONOut returns a JSON-emitting surface. Style/Bold/Dim become identity;
// Write expects callers to push pre-marshalled JSON strings or to use the
// JSONSurface.Emit helper.
func JSONOut(w io.Writer) Surface {
	return &jsonSurface{w: w}
}

// Auto picks ANSI when stdout is a TTY, Plain otherwise.
func Auto(w io.Writer) Surface {
	if ui.IsInteractiveWriter(w) {
		return ANSI(w)
	}
	return Plain(w)
}

type ansiSurface struct {
	w     io.Writer
	color bool
}

func (s *ansiSurface) Write(line string)         { fmt.Fprintln(s.w, line) }
func (s *ansiSurface) WriteBlock(lines []string) {
	for _, l := range lines {
		fmt.Fprintln(s.w, l)
	}
}

func (s *ansiSurface) Style(t style.Token, text string) string {
	if !s.color || text == "" {
		return text
	}
	switch t {
	case style.TokenBrand:
		return ui.BoldCyan(true, text)
	case style.TokenBrandSoft:
		return ui.Magenta(true, text)
	case style.TokenSecondary, style.TokenRunning:
		return ui.Cyan(true, text)
	case style.TokenSuccess:
		return ui.Green(true, text)
	case style.TokenWarning:
		return ui.Yellow(true, text)
	case style.TokenError:
		return ui.Red(true, text)
	case style.TokenDim, style.TokenPending, style.TokenMuted:
		return ui.Dim(true, text)
	}
	return text
}

func (s *ansiSurface) Bold(text string) string { return ui.Bold(s.color, text) }
func (s *ansiSurface) Dim(text string) string  { return ui.Dim(s.color, text) }

func (s *ansiSurface) Width() int {
	f, ok := s.w.(*os.File)
	if !ok {
		return 80
	}
	if !ui.IsInteractiveWriter(s.w) {
		return 0
	}
	w, _, err := term.GetSize(int(f.Fd()))
	if err != nil || w <= 0 {
		return 80
	}
	return w
}

func (s *ansiSurface) JSON() bool { return false }

// --- JSON surface ----------------------------------------------------

type jsonSurface struct{ w io.Writer }

func (s *jsonSurface) Write(line string)                       { fmt.Fprintln(s.w, line) }
func (s *jsonSurface) WriteBlock(lines []string)               {
	for _, l := range lines {
		fmt.Fprintln(s.w, l)
	}
}
func (s *jsonSurface) Style(_ style.Token, text string) string { return text }
func (s *jsonSurface) Bold(text string) string                 { return text }
func (s *jsonSurface) Dim(text string) string                  { return text }
func (s *jsonSurface) Width() int                              { return 0 }
func (s *jsonSurface) JSON() bool                              { return true }

// Emit marshals v as indented JSON and writes it to the JSON surface. If
// s is not a JSON surface this is a no-op (renderers should always check
// s.JSON() and select the appropriate path).
func Emit(s Surface, v any) error {
	js, ok := s.(*jsonSurface)
	if !ok {
		return fmt.Errorf("surface is not JSON")
	}
	enc := json.NewEncoder(js.w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
