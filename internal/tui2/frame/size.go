// Package frame is the rendering kernel of the cockpit v2 (specs/orun-tui-v2).
//
// Its contract, stated once and enforced everywhere: a region renders into a
// box of exactly the size it is given — never more, never less. Dimensional
// stability is guaranteed at the leaf, by construction, so the composed frame
// can never grow past the terminal and there is nothing for a post-hoc
// clipping pass to do. The three clipping layers and forced ClearScreens the
// v1 cockpit needed do not exist here, on purpose.
package frame

// Size is a box's dimensions in terminal cells.
type Size struct {
	Width  int
	Height int
}

// Empty reports whether the box has no drawable area.
func (s Size) Empty() bool { return s.Width <= 0 || s.Height <= 0 }
