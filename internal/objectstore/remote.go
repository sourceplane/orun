package objectstore

import (
	"context"
	"fmt"
)

// ObjectTransport is the minimal remote object-plane a RemoteStore drives: a
// content-addressed blob store keyed by digest ("<algo>:<hex>"), where the
// stored bytes are the object's framed canonical serialization (the same bytes
// LocalStore writes to disk). Because every id is the hash of those framed
// bytes, "the cloud has it" and "the CLI has it" name the same object — dedup is
// global and a tampered object is rejected by construction.
//
// The concrete HTTP transport lives in internal/remotestate (it imports this
// package, never the reverse), so the framing/identity logic stays in one place
// and the driver is transport-agnostic and unit-testable with an in-memory fake.
type ObjectTransport interface {
	// HasObject reports whether digest is present without transferring bytes.
	HasObject(ctx context.Context, digest string) (bool, error)
	// GetObject returns the stored framed bytes, or ok=false when absent.
	GetObject(ctx context.Context, digest string) (framed []byte, ok bool, err error)
	// PutObject stores framed bytes under digest. kind is the structural object
	// kind ("blob"|"tree"), carried for the server's index; the call is
	// idempotent — a repeat put of a present digest is a no-op.
	PutObject(ctx context.Context, digest, kind string, framed []byte) error
}

// BatchMissingTransport is an OPTIONAL ObjectTransport capability: answer a
// digest-negotiation for many digests in a single call, returning the absent
// subset. A RemoteStore uses it when present to satisfy MissingObjects in one
// round-trip (chunked by the transport); otherwise it falls back to a per-digest
// HasObject scan. The hosted object plane implements it over the batch
// /objects/missing endpoint.
type BatchMissingTransport interface {
	MissingObjects(ctx context.Context, digests []string) (missing []string, err error)
}

// RemoteStore is an ObjectStore backed by a remote object plane (R2/S3/Orun
// Cloud) over an ObjectTransport. It is the SAME interface as LocalStore plus a
// transport; the ModelReader read seam (objmodel) and the sync engine
// (objremote) drive it identically to a local store.
type RemoteStore struct {
	t    ObjectTransport
	algo Algo
	root string
}

// NewRemoteStore wraps an ObjectTransport as an ObjectStore under algo
// (DefaultAlgo when empty). root is a diagnostic label only (e.g. a bucket+prefix
// or base URL).
func NewRemoteStore(t ObjectTransport, algo Algo, root string) *RemoteStore {
	if algo == "" {
		algo = DefaultAlgo
	}
	if root == "" {
		root = "remote://"
	}
	return &RemoteStore{t: t, algo: algo, root: root}
}

// Root identifies the store for diagnostics.
func (r *RemoteStore) Root() string { return r.root }

// Algo returns the store's hash algorithm.
func (r *RemoteStore) Algo() Algo { return r.algo }

// PutBlob frames data as a blob and uploads it under its content id. Idempotent.
func (r *RemoteStore) PutBlob(ctx context.Context, data []byte) (ObjectID, error) {
	serialized, id, err := computeBlobID(r.algo, data)
	if err != nil {
		return "", err
	}
	if err := r.t.PutObject(ctx, string(id), string(KindBlob), serialized); err != nil {
		return "", fmt.Errorf("objectstore: remote put blob %s: %w", id, err)
	}
	return id, nil
}

// PutTree validates+sorts entries, frames the tree, and uploads it. Idempotent.
func (r *RemoteStore) PutTree(ctx context.Context, entries []TreeEntry) (ObjectID, error) {
	_, serialized, id, err := computeTree(r.algo, entries)
	if err != nil {
		return "", err
	}
	if err := r.t.PutObject(ctx, string(id), string(KindTree), serialized); err != nil {
		return "", fmt.Errorf("objectstore: remote put tree %s: %w", id, err)
	}
	return id, nil
}

// Get downloads an object's framed bytes, verifies hash(bytes)==id, and returns
// its kind and body. Integrity is checked end-to-end: a transport that returns
// the wrong bytes is caught here, not trusted.
func (r *RemoteStore) Get(ctx context.Context, id ObjectID) (Kind, []byte, error) {
	if _, _, err := parseID(id); err != nil {
		return "", nil, err
	}
	framed, ok, err := r.t.GetObject(ctx, string(id))
	if err != nil {
		return "", nil, fmt.Errorf("objectstore: remote get %s: %w", id, err)
	}
	if !ok {
		return "", nil, ErrNotFound
	}
	if err := verify(r.algo, framed, id); err != nil {
		return "", nil, err
	}
	kind, body, err := parseFrame(framed)
	if err != nil {
		return "", nil, err
	}
	return kind, append([]byte(nil), body...), nil
}

// GetTree downloads and decodes a tree object.
func (r *RemoteStore) GetTree(ctx context.Context, id ObjectID) ([]TreeEntry, error) {
	kind, body, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if kind != KindTree {
		return nil, ErrInvalid
	}
	return decodeTreeBody(body, r.algo)
}

// Has reports presence via the transport's cheap existence check.
func (r *RemoteStore) Has(ctx context.Context, id ObjectID) (bool, error) {
	if _, _, err := parseID(id); err != nil {
		return false, err
	}
	return r.t.HasObject(ctx, string(id))
}

// MissingObjects reports which ids are absent remotely. When the transport
// supports batch digest negotiation (BatchMissingTransport) it answers the whole
// set in one round-trip — the seam objremote.Sync relies on to avoid an
// O(closure) burst of presence requests on push. Otherwise it falls back to a
// per-id HasObject scan, matching Has. Satisfies objectstore.MissingFilter.
func (r *RemoteStore) MissingObjects(ctx context.Context, ids []ObjectID) ([]ObjectID, error) {
	for _, id := range ids {
		if _, _, err := parseID(id); err != nil {
			return nil, err
		}
	}
	if bt, ok := r.t.(BatchMissingTransport); ok {
		digests := make([]string, len(ids))
		for i, id := range ids {
			digests[i] = string(id)
		}
		missing, err := bt.MissingObjects(ctx, digests)
		if err != nil {
			return nil, fmt.Errorf("objectstore: remote missing scan: %w", err)
		}
		out := make([]ObjectID, 0, len(missing))
		for _, d := range missing {
			out = append(out, ObjectID(d))
		}
		return out, nil
	}
	var out []ObjectID
	for _, id := range ids {
		has, err := r.t.HasObject(ctx, string(id))
		if err != nil {
			return nil, fmt.Errorf("objectstore: remote has %s: %w", id, err)
		}
		if !has {
			out = append(out, id)
		}
	}
	return out, nil
}

// Walk visits every object reachable from root depth-first, fetching each tree
// to recurse. It drives the same shared DFS as the local stores; for the read
// path (objmodel) this is rarely needed, but it lets objremote.Pull mirror a
// remote closure into a local store.
func (r *RemoteStore) Walk(ctx context.Context, root ObjectID, fn func(ObjectID, Kind) error) error {
	return walkFrom(ctx, root, r.Get, r.algo, make(map[ObjectID]struct{}), fn)
}

// Iterate is unsupported on a remote store: whole-store enumeration is a GC/fsck
// concern that runs against the authoritative store, not over the read
// transport. Returns ErrUnsupported.
func (r *RemoteStore) Iterate(_ context.Context, _ func(ObjectID) error) error {
	return fmt.Errorf("%w: Iterate", ErrUnsupported)
}

// Delete is unsupported on a remote store: object deletion is server-side GC,
// never a client operation. Returns ErrUnsupported.
func (r *RemoteStore) Delete(_ context.Context, _ ObjectID) error {
	return fmt.Errorf("%w: Delete", ErrUnsupported)
}

var (
	_ ObjectStore   = (*RemoteStore)(nil)
	_ MissingFilter = (*RemoteStore)(nil)
)
