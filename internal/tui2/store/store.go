// Package store holds the cockpit's shared state as revisioned slices.
//
// Every piece of state a renderer reads lives in a named slice with a
// monotonic revision. Renderers fold the revisions of the slices they read
// into their memo keys (internal/tui2/frame), which is the whole caching
// story: data changed ⇒ revision bumped ⇒ memo key changed ⇒ region
// re-renders. Nothing else invalidates anything.
//
// Mutation happens only on the Bubble Tea update goroutine — reducers fold
// messages carrying data that tea.Cmds fetched elsewhere. The store is
// therefore deliberately unsynchronized: handing it to another goroutine is
// a bug, and the data plane (TR2) delivers deltas as messages instead.
package store

import "strconv"

// Rev is a slice revision. Revisions are globally monotonic across the
// store, so a composite memo key of several revisions can never collide
// with a different history.
type Rev uint64

// Store is the cockpit's state root.
type Store struct {
	vals    map[string]any
	revs    map[string]Rev
	counter Rev
}

// New returns an empty store.
func New() *Store {
	return &Store{vals: make(map[string]any), revs: make(map[string]Rev)}
}

// Slice is a typed handle on one named piece of state.
type Slice[T any] struct {
	s    *Store
	name string
}

// Define names a slice in s. Defining the same name twice with different
// types is a programming error and will panic on first Get.
func Define[T any](s *Store, name string) Slice[T] {
	return Slice[T]{s: s, name: name}
}

// Get returns the current value (zero value if never set).
func (sl Slice[T]) Get() T {
	v, ok := sl.s.vals[sl.name]
	if !ok {
		var zero T
		return zero
	}
	return v.(T)
}

// Rev returns the slice's revision — 0 until first Set.
func (sl Slice[T]) Rev() Rev { return sl.s.revs[sl.name] }

// Key returns the revision rendered for use in a memo key.
func (sl Slice[T]) Key() string { return strconv.FormatUint(uint64(sl.s.revs[sl.name]), 36) }

// Set replaces the value and bumps the revision.
func (sl Slice[T]) Set(v T) {
	sl.s.counter++
	sl.s.vals[sl.name] = v
	sl.s.revs[sl.name] = sl.s.counter
}

// Update applies fn to the current value and stores the result.
func (sl Slice[T]) Update(fn func(T) T) { sl.Set(fn(sl.Get())) }
