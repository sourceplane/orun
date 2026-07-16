package frame

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Compose splices box centered over base. Both inputs obey the exact-size
// contract (base is size; box is at most size) and the output is exactly
// size again — overlays cannot destabilize the frame.
//
// Style runs cut at the seams are terminated with a reset so the base's
// colors never bleed into the box and vice versa.
func Compose(base string, box string, size Size) string {
	if size.Empty() || box == "" {
		return base
	}
	boxLines := strings.Split(box, "\n")
	boxH := len(boxLines)
	boxW := 0
	for _, l := range boxLines {
		if w := ansi.StringWidth(l); w > boxW {
			boxW = w
		}
	}
	if boxW > size.Width || boxH > size.Height {
		// An overlay larger than the stage renders as the stage.
		return Fit(box, size)
	}

	x := (size.Width - boxW) / 2
	y := (size.Height - boxH) / 2

	baseLines := strings.Split(base, "\n")
	out := make([]string, len(baseLines))
	copy(out, baseLines)
	const reset = "\x1b[0m"
	for i, bl := range boxLines {
		row := y + i
		if row < 0 || row >= len(out) {
			continue
		}
		left := ansi.Cut(out[row], 0, x)
		right := ansi.Cut(out[row], x+boxW, size.Width)
		out[row] = left + reset + FitLine(bl, boxW) + reset + right
	}
	return strings.Join(out, "\n")
}
