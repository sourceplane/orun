// Package objectstore is the L0 content-addressed object store for the
// orun-object-model rewrite (specs/orun-object-model/object-store.md). It is the
// only path through which object bytes are written; every higher layer (nodes,
// nodewriter, indexes, working view, remote substitution) is built on it.
//
// There are two structural object kinds — blob and tree — addressed by the hash
// of their framed canonical serialization. Objects are immutable: there is no
// Update. All mutation lives in the ref store (a sibling package). Identical
// content yields one object and one id, which is what makes dedup and remote
// substitution correct by construction.
package objectstore

import "context"

// ObjectStore is the frozen L0 contract. Implementations MUST guarantee:
//   - Content integrity: Get returns bytes whose hash equals the requested id,
//     else ErrCorrupt.
//   - Idempotent puts: storing identical content twice yields one object and
//     the same id, and is safe under concurrency.
//   - Atomicity: a reader never observes a partially written object.
type ObjectStore interface {
	// Root returns a diagnostic identifier (local: absolute fs path; remote:
	// bucket+prefix). Not a logical address; for logging only.
	Root() string

	// Algo returns the hash algorithm this store addresses objects under.
	Algo() Algo

	// PutBlob stores data as a blob and returns its content id. Idempotent: a
	// repeat put of identical bytes returns the same id and is a no-op on disk.
	PutBlob(ctx context.Context, data []byte) (ObjectID, error)

	// PutTree stores a tree. Entries need not be pre-sorted; the store sorts and
	// validates them (alphabet, uniqueness, kind, id shape). Returns ErrInvalid
	// on a malformed entry. An empty tree is legal.
	PutTree(ctx context.Context, entries []TreeEntry) (ObjectID, error)

	// Get returns the object's kind and uncompressed body. Returns ErrNotFound
	// if absent and ErrCorrupt if the stored bytes do not hash to id.
	Get(ctx context.Context, id ObjectID) (Kind, []byte, error)

	// GetTree returns a tree object's entries. Returns ErrInvalid if id names a
	// blob.
	GetTree(ctx context.Context, id ObjectID) ([]TreeEntry, error)

	// Has reports presence without reading the body — the hot path for
	// Has-gated reuse during the write walk.
	Has(ctx context.Context, id ObjectID) (bool, error)

	// Walk visits every object reachable from root depth-first, invoking fn once
	// per distinct id (visited ids are de-duplicated). Used by GC marking and
	// fsck.
	Walk(ctx context.Context, root ObjectID, fn func(ObjectID, Kind) error) error

	// Iterate enumerates every object id present in the store, in unspecified
	// order. Used by GC sweep and fsck.
	Iterate(ctx context.Context, fn func(ObjectID) error) error

	// Delete removes a single object. Used only by GC, which MUST have proven
	// unreachability. No-op if absent. There is intentionally no Update.
	Delete(ctx context.Context, id ObjectID) error
}

// MissingFilter is an OPTIONAL ObjectStore capability: report, in a single
// operation, which of many ids are absent. Stores backed by a network plane
// implement it so a presence scan over a whole closure collapses into one (or a
// few chunked) round-trip(s) instead of one round-trip per object. Callers MUST
// fall back to per-id Has when a store does not implement it — a local store has
// no reason to (its Has is a cheap disk stat).
type MissingFilter interface {
	// MissingObjects returns the subset of ids the store does not have. The
	// result order is unspecified; each absent id appears at most once.
	MissingObjects(ctx context.Context, ids []ObjectID) ([]ObjectID, error)
}

// computeBlobID frames data as a blob and returns (serialized, id) under algo.
func computeBlobID(algo Algo, data []byte) ([]byte, ObjectID, error) {
	serialized := frame(KindBlob, data)
	id, err := algo.idFor(serialized)
	return serialized, id, err
}

// computeTree validates+sorts entries, encodes the tree body, frames it, and
// returns (sortedEntries, serialized, id) under algo.
func computeTree(algo Algo, entries []TreeEntry) ([]TreeEntry, []byte, ObjectID, error) {
	sorted, err := sortValidateEntries(entries, algo)
	if err != nil {
		return nil, nil, "", err
	}
	body := encodeTreeBody(sorted)
	serialized := frame(KindTree, body)
	id, err := algo.idFor(serialized)
	if err != nil {
		return nil, nil, "", err
	}
	return sorted, serialized, id, nil
}

// verify recomputes the id of framed bytes and compares it to want, returning
// ErrCorrupt on mismatch. Shared by every driver's Get.
func verify(algo Algo, serialized []byte, want ObjectID) error {
	got, err := algo.idFor(serialized)
	if err != nil {
		return err
	}
	if got != want {
		return ErrCorrupt
	}
	return nil
}

// walkFrom implements the shared DFS used by every driver's Walk: it dedups
// visited ids and recurses into tree children. get is the driver's own Get.
func walkFrom(
	ctx context.Context,
	root ObjectID,
	get func(context.Context, ObjectID) (Kind, []byte, error),
	algo Algo,
	visited map[ObjectID]struct{},
	fn func(ObjectID, Kind) error,
) error {
	if _, seen := visited[root]; seen {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	kind, body, err := get(ctx, root)
	if err != nil {
		return err
	}
	visited[root] = struct{}{}
	if err := fn(root, kind); err != nil {
		return err
	}
	if kind != KindTree {
		return nil
	}
	entries, err := decodeTreeBody(body, algo)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := walkFrom(ctx, e.ID, get, algo, visited, fn); err != nil {
			return err
		}
	}
	return nil
}
