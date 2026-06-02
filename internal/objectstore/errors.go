package objectstore

import "errors"

// Sentinel errors for the object store. All store methods wrap these so callers
// route on them via errors.Is / errors.As; string-sniffing is banned
// (claude-goals.md §3, object-store.md §8).
var (
	// ErrNotFound is returned by Get/GetTree when no object exists at the id.
	ErrNotFound = errors.New("objectstore: object not found")

	// ErrExists is reserved for create-if-absent style operations that have no
	// idempotent semantics. Plain PutBlob/PutTree are idempotent and never
	// return it.
	ErrExists = errors.New("objectstore: object already exists")

	// ErrConflict is returned by the ref store on a compare-and-swap mismatch.
	// It lives here so the object and ref layers share one taxonomy.
	ErrConflict = errors.New("objectstore: compare-and-swap conflict")

	// ErrInvalid is returned for malformed ids, illegal tree entries (bad name,
	// duplicate name, unsorted on decode), or unknown object kinds.
	ErrInvalid = errors.New("objectstore: invalid argument")

	// ErrCorrupt is returned by Get when a stored object's bytes do not hash to
	// the id under which they were requested — on-disk corruption or tampering.
	ErrCorrupt = errors.New("objectstore: object corrupt (hash mismatch)")
)
