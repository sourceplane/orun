package sourcectx

import "time"

// systemClock is the default Clock — wall time, no monotonic guarantees.
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// DefaultClock returns the package's wall-clock implementation. Tests
// inject a fixed-time fake instead.
func DefaultClock() Clock { return systemClock{} }

// FixedClock is a tiny test helper — not exported as a constructor with
// `New*` to avoid implying a real adapter, but useful enough that it lives
// next to the production Clock for callers/tests.
type FixedClock struct{ T time.Time }

// Now returns the fixed timestamp.
func (f FixedClock) Now() time.Time { return f.T }
