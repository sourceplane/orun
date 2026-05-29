// Package statestore is the bytes-in/bytes-out persistence layer that backs
// the trigger-first revision-first state layout introduced in Milestone M2 of
// the orun-state-redesign spec (see specs/orun-state-redesign/state-store.md).
//
// The package exposes a single frozen interface, StateStore, plus a local
// filesystem driver, LocalStore, that implements the non-CAS subset of that
// interface in PR A: Root, Read, Write, CreateIfAbsent, Delete. CompareAndSwap
// and List land in PR B; helper-driven typed refs/indexes land in PR C.
//
// The interface is the only sanctioned write path for the new layout. Higher
// layers (internal/revision, internal/executionstate) code against the
// interface; remote drivers (R2/S3) will plug in here without changes to
// callers.
//
// Path policy is centralized in paths.go — callers MUST go through the helpers
// there rather than concatenating strings, so the validation alphabet and the
// trigger-first layout stay enforced at one site.
//
// All driver errors wrap one of the four sentinels in errors.go via fmt.Errorf
// %w, so callers use errors.Is / errors.As exclusively. String sniffing of
// error messages is unsupported.
//
// The package is a leaf in the dependency graph: it imports zero other
// internal/* packages, matching the constraint set out in the M2 plan.
package statestore

import (
	"context"
	"time"
)

// StateStore is the byte-level persistence contract for the new orun state
// layout. It is the only path through which Phase 1 callers write the new
// layout files. Implementations must honor the atomicity, exclusivity, and
// error-wrapping guarantees documented in state-store.md.
//
// All methods are safe for concurrent use unless otherwise noted on a specific
// implementation.
type StateStore interface {
	// Root returns the root identifier of the store. For the local driver
	// this is the absolute filesystem path of the .orun root; for a remote
	// driver it is the bucket+prefix. The value is intended for diagnostics
	// and logging only — callers MUST NOT parse it or use it to construct
	// further paths.
	Root() string

	// Read returns the bytes and metadata for the object at the given
	// logical path. Returns an error wrapping ErrNotFound if the object does
	// not exist, and an error wrapping ErrInvalid if path violates the path
	// policy (see paths.go). Implementations honor ctx cancellation.
	Read(ctx context.Context, path string) ([]byte, ObjectMeta, error)

	// Write atomically replaces the object at path with data. Concurrent
	// readers see either the previous bytes or the new bytes — never a
	// partial write. The returned ObjectMeta carries the content-derived
	// Revision (sha256 of data, lowercase hex). Returns an error wrapping
	// ErrInvalid if path violates the path policy.
	Write(ctx context.Context, path string, data []byte, opts WriteOptions) (ObjectMeta, error)

	// CreateIfAbsent writes data only if no object exists at path. Returns
	// an error wrapping ErrExists on collision; the loser of two concurrent
	// CreateIfAbsent calls is guaranteed to observe ErrExists rather than a
	// partial overwrite.
	CreateIfAbsent(ctx context.Context, path string, data []byte) (ObjectMeta, error)

	// CompareAndSwap writes data only if the current object's Revision
	// equals oldRev. Returns an error wrapping ErrConflict on revision
	// mismatch and ErrNotFound if the object does not exist. Phase 1's
	// local driver implements this with a Read-then-Write pair; a future
	// remote driver will use the object store's native conditional update.
	CompareAndSwap(ctx context.Context, path string, oldRev string, data []byte) (ObjectMeta, error)

	// List returns object info for every object whose logical path begins
	// with prefix. Order is unspecified. An empty prefix lists every object
	// under the store root.
	List(ctx context.Context, prefix string) ([]ObjectInfo, error)

	// Delete removes a single object. It is a no-op (returns nil) if the
	// object is already absent. Phase 1 does NOT support recursive
	// deletion: removing a non-empty directory returns an error wrapping
	// ErrInvalid.
	Delete(ctx context.Context, path string) error
}

// ObjectMeta describes a single object as observed by Read / Write /
// CreateIfAbsent / CompareAndSwap. Revision is content-derived: the lowercase
// hex sha256 of the object's bytes. Two writes of identical bytes produce
// identical Revision values; this is intentional and forms the basis for the
// CAS contract.
type ObjectMeta struct {
	// Path is the logical (forward-slash, root-relative, no leading slash)
	// path of the object.
	Path string
	// Size is the object's byte length.
	Size int64
	// Revision is the lowercase-hex sha256 of the object's bytes.
	Revision string
	// UpdatedAt is the wall-clock time the object was last written, taken
	// from the configured Clock at Write/CreateIfAbsent time.
	UpdatedAt time.Time
}

// ObjectInfo is the lightweight per-object record returned by List. It
// intentionally omits Revision so a List can avoid hashing every object.
type ObjectInfo struct {
	// Path is the logical path of the object.
	Path string
	// Size is the object's byte length.
	Size int64
	// UpdatedAt is the underlying storage's modification timestamp for the
	// object.
	UpdatedAt time.Time
}

// WriteOptions reserves space for future per-write knobs (content type, cache
// hints, etc.). It is intentionally empty in Phase 1; passing the zero value
// is correct.
type WriteOptions struct{}
