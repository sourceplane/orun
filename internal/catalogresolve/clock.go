package catalogresolve

import "time"

// Clock is the time seam used by the resolver. The resolver itself
// MUST NOT call time.Now() — every clock read goes through this
// adapter so tests can pin timestamps and the resolution pipeline
// stays deterministic per resolution-pipeline.md §7.
//
// The interface mirrors the C1 internal/sourcectx Clock by design;
// keeping the type local avoids a cross-package import cycle while
// preserving the conceptual contract.
type Clock interface {
	Now() time.Time
}

// systemClock is the wall-clock implementation. UTC by convention so
// any persisted timestamp is timezone-stable.
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// defaultClock returns the package's wall-clock implementation. Used
// when Options.Clock is nil.
func defaultClock() Clock { return systemClock{} }

// FixedClock is a small test helper. Exported for callers that want to
// pin the resolver's view of time without rolling their own type.
type FixedClock struct{ T time.Time }

// Now implements Clock.
func (f FixedClock) Now() time.Time { return f.T }
