package objectstore

import (
	"context"
	"sync"
)

// MemStore is an in-memory ObjectStore for tests. It is safe for concurrent use
// and exercises the same serialization/identity/validation paths as LocalStore
// without touching the filesystem.
type MemStore struct {
	algo Algo
	mu   sync.RWMutex
	objs map[ObjectID]memObject
}

type memObject struct {
	kind Kind
	body []byte
}

// NewMemStore returns an empty in-memory store under algo (DefaultAlgo if "").
func NewMemStore(algo Algo) *MemStore {
	if algo == "" {
		algo = DefaultAlgo
	}
	return &MemStore{algo: algo, objs: make(map[ObjectID]memObject)}
}

// Root identifies the store for diagnostics.
func (m *MemStore) Root() string { return "mem://" }

// Algo returns the store's hash algorithm.
func (m *MemStore) Algo() Algo { return m.algo }

// PutBlob stores data as a blob (idempotent).
func (m *MemStore) PutBlob(_ context.Context, data []byte) (ObjectID, error) {
	_, id, err := computeBlobID(m.algo, data)
	if err != nil {
		return "", err
	}
	body := append([]byte(nil), data...)
	m.mu.Lock()
	m.objs[id] = memObject{kind: KindBlob, body: body}
	m.mu.Unlock()
	return id, nil
}

// PutTree validates+sorts entries and stores the tree (idempotent).
func (m *MemStore) PutTree(_ context.Context, entries []TreeEntry) (ObjectID, error) {
	sorted, _, id, err := computeTree(m.algo, entries)
	if err != nil {
		return "", err
	}
	body := encodeTreeBody(sorted)
	m.mu.Lock()
	m.objs[id] = memObject{kind: KindTree, body: body}
	m.mu.Unlock()
	return id, nil
}

// Get returns the kind and a copy of the body, verifying integrity.
func (m *MemStore) Get(_ context.Context, id ObjectID) (Kind, []byte, error) {
	if _, _, err := parseID(id); err != nil {
		return "", nil, err
	}
	m.mu.RLock()
	o, ok := m.objs[id]
	m.mu.RUnlock()
	if !ok {
		return "", nil, ErrNotFound
	}
	if err := verify(m.algo, frame(o.kind, o.body), id); err != nil {
		return "", nil, err
	}
	return o.kind, append([]byte(nil), o.body...), nil
}

// GetTree decodes a tree object.
func (m *MemStore) GetTree(ctx context.Context, id ObjectID) ([]TreeEntry, error) {
	kind, body, err := m.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if kind != KindTree {
		return nil, ErrInvalid
	}
	return decodeTreeBody(body, m.algo)
}

// Has reports presence.
func (m *MemStore) Has(_ context.Context, id ObjectID) (bool, error) {
	if _, _, err := parseID(id); err != nil {
		return false, err
	}
	m.mu.RLock()
	_, ok := m.objs[id]
	m.mu.RUnlock()
	return ok, nil
}

// Walk visits every object reachable from root depth-first.
func (m *MemStore) Walk(ctx context.Context, root ObjectID, fn func(ObjectID, Kind) error) error {
	return walkFrom(ctx, root, m.Get, m.algo, make(map[ObjectID]struct{}), fn)
}

// Iterate enumerates every present object id.
func (m *MemStore) Iterate(_ context.Context, fn func(ObjectID) error) error {
	m.mu.RLock()
	ids := make([]ObjectID, 0, len(m.objs))
	for id := range m.objs {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	for _, id := range ids {
		if err := fn(id); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes one object (GC only).
func (m *MemStore) Delete(_ context.Context, id ObjectID) error {
	m.mu.Lock()
	delete(m.objs, id)
	m.mu.Unlock()
	return nil
}

var _ ObjectStore = (*MemStore)(nil)
