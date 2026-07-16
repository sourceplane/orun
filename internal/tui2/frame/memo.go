package frame

// Memo caches one region's render, keyed by a revision string and the box
// size. Renderers declare what their output depends on by folding it into the
// key (store revisions, focus, selection); as long as the key and size hold,
// the cached string is returned and the render function never runs.
//
// This is what makes an idle animation tick cheap: regions whose keys did not
// change are string reuses, and only the region that animated re-renders.
// The v1 cockpit re-rendered the full ~18KB frame on every spinner tick;
// here that is a cache hit.
//
// Memo is not safe for concurrent use; like all render state it belongs to
// the Update/View goroutine.
type Memo struct {
	valid  bool
	key    string
	size   Size
	out    string
	hits   uint64
	misses uint64
}

// Render returns the cached output when key and size match the previous
// call, and re-renders (and re-caches) otherwise.
func (m *Memo) Render(key string, size Size, render func() string) string {
	if m.valid && m.key == key && m.size == size {
		m.hits++
		return m.out
	}
	m.misses++
	m.out = render()
	m.key = key
	m.size = size
	m.valid = true
	return m.out
}

// Invalidate drops the cache; the next Render always runs.
func (m *Memo) Invalidate() { m.valid = false }

// Stats reports cache hits and misses since creation — surfaced by the
// profiling harness so the bench can assert idle frames are region-only.
func (m *Memo) Stats() (hits, misses uint64) { return m.hits, m.misses }
