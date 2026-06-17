package refstore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRefTransport is an in-memory CAS ref plane: a name → target map enforcing
// compare-and-swap, exactly as the hosted KV must. It lets RemoteRefStore be
// exercised without any HTTP.
type fakeRefTransport struct {
	refs map[string]string
}

func newFakeRefTransport() *fakeRefTransport {
	return &fakeRefTransport{refs: map[string]string{}}
}

func (f *fakeRefTransport) ReadRef(_ context.Context, name string) (Ref, bool, error) {
	t, ok := f.refs[name]
	if !ok {
		return Ref{}, false, nil
	}
	return Ref{Kind: "Ref", Target: t, Writer: "saas"}, true, nil
}

func (f *fakeRefTransport) UpdateRef(_ context.Context, name, oldTarget, newTarget string) error {
	cur := f.refs[name]
	if cur != oldTarget {
		return ErrConflict
	}
	if newTarget == "" {
		delete(f.refs, name)
		return nil
	}
	f.refs[name] = newTarget
	return nil
}

func (f *fakeRefTransport) ListRefs(_ context.Context, prefix string) ([]string, error) {
	var out []string
	for name := range f.refs {
		if strings.HasPrefix(name, prefix) {
			out = append(out, name)
		}
	}
	return out, nil
}

func (f *fakeRefTransport) DeleteRef(_ context.Context, name string) error {
	delete(f.refs, name)
	return nil
}

const idA = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
const idB = "sha256:2222222222222222222222222222222222222222222222222222222222222222"

func TestRemoteRefStoreCAS(t *testing.T) {
	ctx := context.Background()
	r := NewRemoteRefStore(newFakeRefTransport())

	// Create from absent (oldTarget "").
	if err := r.Update(ctx, "catalogs/current", "", idA); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := r.Read(ctx, "catalogs/current")
	if err != nil || got.Target != idA {
		t.Fatalf("read = %+v, %v", got, err)
	}
	// CAS advance with the correct old value.
	if err := r.Update(ctx, "catalogs/current", idA, idB); err != nil {
		t.Fatalf("advance: %v", err)
	}
	// CAS with a stale old value conflicts.
	if err := r.Update(ctx, "catalogs/current", idA, idB); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update = %v, want ErrConflict", err)
	}
}

func TestRemoteRefStoreReadMissing(t *testing.T) {
	ctx := context.Background()
	r := NewRemoteRefStore(newFakeRefTransport())
	if _, err := r.Read(ctx, "executions/latest"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("read absent = %v, want ErrNotFound", err)
	}
}

func TestRemoteRefStoreListSorted(t *testing.T) {
	ctx := context.Background()
	r := NewRemoteRefStore(newFakeRefTransport())
	_ = r.Update(ctx, "sources/main", "", idA)
	_ = r.Update(ctx, "sources/branches/feature", "", idA)
	_ = r.Update(ctx, "catalogs/current", "", idB)

	names, err := r.List(ctx, "sources/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 2 || names[0] != "sources/branches/feature" || names[1] != "sources/main" {
		t.Fatalf("list = %v (want sorted, sources/ prefix only)", names)
	}
}

func TestRemoteRefStoreValidation(t *testing.T) {
	ctx := context.Background()
	r := NewRemoteRefStore(newFakeRefTransport())
	if _, err := r.Read(ctx, "/bad"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("read bad name = %v, want ErrInvalid", err)
	}
	if err := r.Update(ctx, "ok/name", "", "not-an-id"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("update bad target = %v, want ErrInvalid", err)
	}
}

func TestRemoteRefStoreDelete(t *testing.T) {
	ctx := context.Background()
	r := NewRemoteRefStore(newFakeRefTransport())
	_ = r.Update(ctx, "revisions/latest", "", idA)
	if err := r.Delete(ctx, "revisions/latest"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := r.Read(ctx, "revisions/latest"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("read after delete = %v, want ErrNotFound", err)
	}
}
