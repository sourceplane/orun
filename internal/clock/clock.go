// Package clock provides a tiny injectable wall-clock abstraction shared by the
// object-model packages. Production code in those packages must not call
// time.Now() directly (the object-model lint gate enforces this); it takes a
// Clock instead, which tests replace with a Fixed clock for determinism. The
// real implementation lives here, outside the gated package set, so the single
// time.Now() call site is intentional and reviewed.
package clock

import "time"

// Clock yields the current time. Now MUST return UTC.
type Clock interface {
	Now() time.Time
}

// Real is the production Clock — wall time in UTC, no monotonic guarantees.
type Real struct{}

// Now returns the current UTC time.
func (Real) Now() time.Time { return time.Now().UTC() }

// New returns the production wall clock.
func New() Clock { return Real{} }

// Fixed is a deterministic Clock for tests.
type Fixed struct{ T time.Time }

// Now returns the fixed timestamp.
func (f Fixed) Now() time.Time { return f.T }
