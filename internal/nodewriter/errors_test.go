package nodewriter

import (
	"context"
	"errors"
	"testing"

	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// stubRefs is a programmable RefStore for exercising moveRef branches.
type stubRefs struct {
	m        map[string]string
	readErr  error
	onUpdate func(name, old, new string) error // nil = default map CAS
}

func (s *stubRefs) Read(_ context.Context, name string) (refstore.Ref, error) {
	if s.readErr != nil {
		return refstore.Ref{}, s.readErr
	}
	t, ok := s.m[name]
	if !ok {
		return refstore.Ref{}, refstore.ErrNotFound
	}
	return refstore.Ref{Kind: "Ref", Target: t}, nil
}

func (s *stubRefs) Update(_ context.Context, name, old, new string) error {
	if s.onUpdate != nil {
		return s.onUpdate(name, old, new)
	}
	if s.m[name] != old {
		return refstore.ErrConflict
	}
	s.m[name] = new
	return nil
}

func (s *stubRefs) List(context.Context, string) ([]string, error) { return nil, nil }
func (s *stubRefs) Delete(_ context.Context, name string) error    { delete(s.m, name); return nil }

func writerWithRefs(refs refstore.RefStore) *Writer {
	return New(objectstore.NewMemStore(""), refs, WithClock(clock.Fixed{}))
}

const target = objectstore.ObjectID("sha256:1111111111111111111111111111111111111111111111111111111111111111")

func TestMoveRefIdempotentNoop(t *testing.T) {
	t.Parallel()
	called := false
	s := &stubRefs{
		m:        map[string]string{"r": string(target)},
		onUpdate: func(string, string, string) error { called = true; return nil },
	}
	if err := writerWithRefs(s).moveRef(context.Background(), "r", target); err != nil {
		t.Fatalf("moveRef: %v", err)
	}
	if called {
		t.Fatalf("moveRef should not Update when ref already at target")
	}
}

func TestMoveRefReadError(t *testing.T) {
	t.Parallel()
	s := &stubRefs{readErr: errBoom}
	if err := writerWithRefs(s).moveRef(context.Background(), "r", target); !errors.Is(err, errBoom) {
		t.Fatalf("moveRef read error = %v, want errBoom", err)
	}
}

func TestMoveRefConflictThenSuccess(t *testing.T) {
	t.Parallel()
	n := 0
	s := &stubRefs{m: map[string]string{}, onUpdate: func(name, _, new string) error {
		n++
		if n == 1 {
			return refstore.ErrConflict
		}
		s2 := name // ensure closure captures
		_ = s2
		return nil
	}}
	if err := writerWithRefs(s).moveRef(context.Background(), "r", target); err != nil {
		t.Fatalf("moveRef should recover from a single conflict: %v", err)
	}
	if n < 2 {
		t.Fatalf("expected a retry, got %d Update calls", n)
	}
}

func TestMoveRefTooManyConflicts(t *testing.T) {
	t.Parallel()
	s := &stubRefs{m: map[string]string{}, onUpdate: func(string, string, string) error { return refstore.ErrConflict }}
	if err := writerWithRefs(s).moveRef(context.Background(), "r", target); err == nil {
		t.Fatalf("expected too-many-conflicts error")
	}
}

func TestMoveRefUpdateError(t *testing.T) {
	t.Parallel()
	s := &stubRefs{m: map[string]string{}, onUpdate: func(string, string, string) error { return errBoom }}
	if err := writerWithRefs(s).moveRef(context.Background(), "r", target); !errors.Is(err, errBoom) {
		t.Fatalf("moveRef update error = %v, want errBoom", err)
	}
}

// failingStore wraps a MemStore and can fail Has or PutBlob.
type failingStore struct {
	*objectstore.MemStore
	hasErr error
	putErr error
}

func (f *failingStore) Has(ctx context.Context, id objectstore.ObjectID) (bool, error) {
	if f.hasErr != nil {
		return false, f.hasErr
	}
	return f.MemStore.Has(ctx, id)
}

func (f *failingStore) PutBlob(ctx context.Context, data []byte) (objectstore.ObjectID, error) {
	if f.putErr != nil {
		return "", f.putErr
	}
	return f.MemStore.PutBlob(ctx, data)
}

func TestWriteRevisionStoreErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rev := nodes.PlanRevision{Scope: nodes.RevisionScope{Mode: "full"}}
	plan := []byte(`{"p":1}`)
	good := &stubRefs{m: map[string]string{}}

	hasFail := New(&failingStore{MemStore: objectstore.NewMemStore(""), hasErr: errBoom}, good)
	if _, _, err := hasFail.WriteRevision(ctx, rev, plan); !errors.Is(err, errBoom) {
		t.Fatalf("WriteRevision has-error = %v, want errBoom", err)
	}
	putFail := New(&failingStore{MemStore: objectstore.NewMemStore(""), putErr: errBoom}, good)
	if _, _, err := putFail.WriteRevision(ctx, rev, plan); !errors.Is(err, errBoom) {
		t.Fatalf("WriteRevision put-error = %v, want errBoom", err)
	}
}

func TestWriteSourceAndCatalogErrorPropagation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := New(&failingStore{MemStore: objectstore.NewMemStore(""), putErr: errBoom}, &stubRefs{m: map[string]string{}})
	if _, err := w.WriteSource(ctx, nodes.SourceSnapshot{Scope: nodes.ScopeMain}); !errors.Is(err, errBoom) {
		t.Fatalf("WriteSource = %v, want errBoom", err)
	}
	if _, err := w.WriteCatalog(ctx, nodes.CatalogSnapshot{SourceID: string(target)}, nil, nil); !errors.Is(err, errBoom) {
		t.Fatalf("WriteCatalog = %v, want errBoom", err)
	}
}

func TestPlanPropagatesRefError(t *testing.T) {
	t.Parallel()
	// A ref store that always errors on Read (non-NotFound) fails the first
	// moveRef inside WriteSource, so Plan surfaces it.
	w := New(objectstore.NewMemStore(""), &stubRefs{readErr: errBoom})
	_, err := w.Plan(context.Background(), samplePlan(true))
	if !errors.Is(err, errBoom) {
		t.Fatalf("Plan ref error = %v, want errBoom", err)
	}
}

func TestRecordTriggerValidationError(t *testing.T) {
	t.Parallel()
	// Empty TriggerName fails nodes validation inside AssembleTrigger.
	w := New(objectstore.NewMemStore(""), &stubRefs{m: map[string]string{}})
	if _, err := w.RecordTrigger(context.Background(), nodes.TriggerOccurrence{}, target); !errors.Is(err, nodes.ErrInvalid) {
		t.Fatalf("RecordTrigger bad = %v, want ErrInvalid", err)
	}
}
