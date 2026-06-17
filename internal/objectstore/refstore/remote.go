package refstore

import (
	"context"
	"fmt"
	"sort"
)

// RefTransport is the minimal remote ref-plane a RemoteRefStore drives: a
// compare-and-swap pointer store keyed by ref name. The concrete HTTP transport
// lives in internal/remotestate (it imports this package, never the reverse), so
// name/target validation and the CAS contract stay in one place and the driver
// is unit-testable with an in-memory fake.
type RefTransport interface {
	// ReadRef returns the ref at name, or ok=false when absent.
	ReadRef(ctx context.Context, name string) (ref Ref, ok bool, err error)
	// UpdateRef writes newTarget at name only if the current target equals
	// oldTarget ("" requires the ref be absent). It MUST return ErrConflict on a
	// compare-and-swap mismatch so callers (objremote.moveRef) can retry.
	UpdateRef(ctx context.Context, name, oldTarget, newTarget string) error
	// ListRefs returns the logical names of every ref under prefix.
	ListRefs(ctx context.Context, prefix string) ([]string, error)
	// DeleteRef removes the ref at name (no-op if absent).
	DeleteRef(ctx context.Context, name string) error
}

// RemoteRefStore is a RefStore backed by a remote ref plane (a CAS-capable KV in
// Orun Cloud) over a RefTransport. It is the SAME interface as LocalRefStore plus
// a transport; objremote and the ModelReader seam drive it identically.
type RemoteRefStore struct {
	t RefTransport
}

// NewRemoteRefStore wraps a RefTransport as a RefStore.
func NewRemoteRefStore(t RefTransport) *RemoteRefStore {
	return &RemoteRefStore{t: t}
}

// Read returns the ref at name, or ErrNotFound.
func (r *RemoteRefStore) Read(ctx context.Context, name string) (Ref, error) {
	if !validRefName(name) {
		return Ref{}, fmt.Errorf("%w: ref name %q", ErrInvalid, name)
	}
	ref, ok, err := r.t.ReadRef(ctx, name)
	if err != nil {
		return Ref{}, fmt.Errorf("refstore: remote read %q: %w", name, err)
	}
	if !ok {
		return Ref{}, ErrNotFound
	}
	return ref, nil
}

// Update writes newTarget at name under compare-and-swap. The transport maps a
// server CAS rejection to ErrConflict.
func (r *RemoteRefStore) Update(ctx context.Context, name, oldTarget, newTarget string) error {
	if !validRefName(name) {
		return fmt.Errorf("%w: ref name %q", ErrInvalid, name)
	}
	if newTarget != "" {
		if err := validTarget(newTarget); err != nil {
			return err
		}
	}
	return r.t.UpdateRef(ctx, name, oldTarget, newTarget)
}

// List returns the logical names of every ref under prefix, sorted.
func (r *RemoteRefStore) List(ctx context.Context, prefix string) ([]string, error) {
	names, err := r.t.ListRefs(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("refstore: remote list %q: %w", prefix, err)
	}
	sort.Strings(names)
	return names, nil
}

// Delete removes the ref at name (no-op if absent).
func (r *RemoteRefStore) Delete(ctx context.Context, name string) error {
	if !validRefName(name) {
		return fmt.Errorf("%w: ref name %q", ErrInvalid, name)
	}
	return r.t.DeleteRef(ctx, name)
}

var _ RefStore = (*RemoteRefStore)(nil)
