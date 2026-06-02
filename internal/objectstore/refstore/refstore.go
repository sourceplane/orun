// Package refstore is the L2 mutable-pointer layer of the orun-object-model
// store (object-store.md §6). Refs are the only mutable, authoritative surface
// over the immutable object graph: each ref is a name → ObjectID pointer,
// updated by compare-and-swap, and refs are the roots reachability GC marks
// from.
//
// A ref body is itself a small JSON record written atomically (temp + rename),
// so a ref move is an atomic publish point: higher layers write an object's
// full closure first, then move the ref last.
package refstore

import (
	"context"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/objectstore"
)

// Re-export the shared error taxonomy so callers route ref errors the same way
// they route object errors.
var (
	// ErrNotFound is returned by Read when no ref exists at the name.
	ErrNotFound = objectstore.ErrNotFound
	// ErrConflict is returned by Update when the current target does not equal
	// the expected oldTarget (the compare-and-swap lost).
	ErrConflict = objectstore.ErrConflict
	// ErrInvalid is returned for a malformed ref name or target.
	ErrInvalid = objectstore.ErrInvalid
)

// Ref is the persisted pointer record.
type Ref struct {
	Kind      string    `json:"kind"`      // always "Ref"
	Target    string    `json:"target"`    // an objectstore.ObjectID
	UpdatedAt time.Time `json:"updatedAt"` // RFC 3339 / Z
	Writer    string    `json:"writer"`    // "cli"|"runner"|"tui"|"saas"|"migrate"
}

// RefStore is the L2 contract.
type RefStore interface {
	// Read returns the ref at name, or ErrNotFound.
	Read(ctx context.Context, name string) (Ref, error)

	// Update writes newTarget at name only if the current target equals
	// oldTarget (use "" to require the ref be absent). Returns ErrConflict on
	// mismatch and ErrInvalid for a malformed name or newTarget.
	Update(ctx context.Context, name, oldTarget, newTarget string) error

	// List returns the logical names of every ref under prefix, sorted.
	List(ctx context.Context, prefix string) ([]string, error)

	// Delete removes the ref at name. No-op if absent.
	Delete(ctx context.Context, name string) error
}

// validRefName reports whether name is a legal ref path: non-empty, no leading
// or trailing slash, and every "/"-separated segment matches [A-Za-z0-9._-]
// and is not "." or "..".
func validRefName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return false
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
		for i := 0; i < len(seg); i++ {
			c := seg[i]
			switch {
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			case c == '.' || c == '_' || c == '-':
			default:
				return false
			}
		}
	}
	return true
}

// validTarget reports whether target is a well-formed object id, delegating to
// the object store's id validation.
func validTarget(target string) error {
	return objectstore.ValidateID(objectstore.ObjectID(target))
}
