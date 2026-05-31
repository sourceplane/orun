package catalogstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/statestore"
)

func makeEvent(name, kind string) catalogmodel.ComponentHistoryEvent {
	return catalogmodel.ComponentHistoryEvent{
		APIVersion:         "orun.io/v1alpha1",
		Kind:               "ComponentHistoryEvent",
		EventType:          kind,
		ComponentKey:       "sourceplane/orun/" + name,
		SourceSnapshotKey:  testSrcKey,
		CatalogSnapshotKey: testCatKey,
		At:                 "2026-05-31T00:00:00Z",
	}
}

// TestAppendComponentEvent_AllocatesSeq1ThenSeq2 verifies the
// CreateIfAbsent → CompareAndSwap allocator path: first call
// initializes seq.lock with next=2 and emits event #1; second call
// reads the lock, allocates 2, CASes next=3 and emits event #2.
func TestAppendComponentEvent_AllocatesSeq1ThenSeq2(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)

	if err := st.AppendComponentEvent(context.Background(), makeEvent("aaa", catalogmodel.EventCatalogResolved)); err != nil {
		t.Fatalf("first event: %v", err)
	}
	if err := st.AppendComponentEvent(context.Background(), makeEvent("aaa", catalogmodel.EventManifestChanged)); err != nil {
		t.Fatalf("second event: %v", err)
	}

	// Bodies should land at seq 000000001 and 000000002.
	want1, err := catalogstore.ComponentHistoryEventPath(testSrcKey, testCatKey, "aaa", 1, catalogmodel.EventCatalogResolved)
	if err != nil {
		t.Fatalf("path1: %v", err)
	}
	want2, err := catalogstore.ComponentHistoryEventPath(testSrcKey, testCatKey, "aaa", 2, catalogmodel.EventManifestChanged)
	if err != nil {
		t.Fatalf("path2: %v", err)
	}
	if _, ok := spy.objects[want1]; !ok {
		t.Errorf("missing event1 at %s; have %v", want1, keys(spy.objects))
	}
	if _, ok := spy.objects[want2]; !ok {
		t.Errorf("missing event2 at %s; have %v", want2, keys(spy.objects))
	}
	// Verify final seq.lock body has next=3.
	lockPath := strings.Replace(want2, "/000000002-manifest-changed.json", "/seq.lock", 1)
	body, ok := spy.objects[lockPath]
	if !ok {
		t.Fatalf("missing seq.lock at %s; have %v", lockPath, keys(spy.objects))
	}
	var env struct {
		Next uint64 `json:"next"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode seq.lock: %v", err)
	}
	if env.Next != 3 {
		t.Errorf("seq.lock.next=%d, want 3", env.Next)
	}
}

// TestAppendComponentEvent_SeqLockBudgetExhausted — every CAS attempt
// against seq.lock conflicts → ErrRefStale.
func TestAppendComponentEvent_SeqLockBudgetExhausted(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)

	// Bootstrap seq.lock to existing state so we go straight into CAS.
	if err := st.AppendComponentEvent(context.Background(), makeEvent("aaa", catalogmodel.EventCatalogResolved)); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	// Find the lock path.
	var lockPath string
	for k := range spy.objects {
		if strings.HasSuffix(k, "/events/seq.lock") {
			lockPath = k
			break
		}
	}
	if lockPath == "" {
		t.Fatalf("no seq.lock present after bootstrap; have %v", keys(spy.objects))
	}
	// Force more CAS conflicts than the budget allows.
	spy.casConflicts[lockPath] = 100

	err := st.AppendComponentEvent(context.Background(), makeEvent("aaa", catalogmodel.EventManifestChanged))
	if err == nil {
		t.Fatalf("expected ErrRefStale")
	}
	if !errors.Is(err, catalogstore.ErrRefStale) {
		t.Errorf("err not ErrRefStale chain: %v", err)
	}
	if !errors.Is(err, statestore.ErrConflict) {
		t.Errorf("err must wrap statestore.ErrConflict: %v", err)
	}
}

// TestAppendComponentEvent_InvalidEventKind.
func TestAppendComponentEvent_InvalidEventKind(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ev := makeEvent("aaa", "BAD KIND")
	err := st.AppendComponentEvent(context.Background(), ev)
	if err == nil {
		t.Fatalf("expected error")
	}
	// May be wrapped as ErrInvalidPathInput via paths.go ValidateEventKind,
	// or surface as a generic validation error — either way, no writes.
	if len(spy.trace) != 0 {
		t.Errorf("no writes should have been issued, got %v", spy.trace)
	}
}

// TestAppendComponentEvent_InvalidComponentKey.
func TestAppendComponentEvent_InvalidComponentKey(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ev := makeEvent("aaa", catalogmodel.EventCatalogResolved)
	ev.ComponentKey = "" // missing
	err := st.AppendComponentEvent(context.Background(), ev)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, catalogstore.ErrInvalidPathInput) {
		t.Errorf("err not ErrInvalidPathInput chain: %v", err)
	}
}

// TestAppendComponentEvent_MissingRequiredFields.
func TestAppendComponentEvent_MissingRequiredFields(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	cases := map[string]func(*catalogmodel.ComponentHistoryEvent){
		"no-source":  func(e *catalogmodel.ComponentHistoryEvent) { e.SourceSnapshotKey = "" },
		"no-catalog": func(e *catalogmodel.ComponentHistoryEvent) { e.CatalogSnapshotKey = "" },
		"no-kind":    func(e *catalogmodel.ComponentHistoryEvent) { e.EventType = "" },
	}
	for name, mut := range cases {
		ev := makeEvent("aaa", catalogmodel.EventCatalogResolved)
		mut(&ev)
		if err := st.AppendComponentEvent(context.Background(), ev); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

// TestAppendComponentEvent_ConcurrentAllocatorPath — when seq.lock
// already exists (concurrent writer initialised it), the second writer
// uses the CAS path to allocate.
func TestAppendComponentEvent_ConcurrentAllocatorPath(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)

	// Pre-populate seq.lock with next=5 to simulate a peer that already
	// allocated 1..4.
	preBody, _ := json.Marshal(map[string]uint64{"next": 5})
	dir, _ := catalogstore.CatalogDir(testSrcKey, testCatKey)
	lockPath := dir + "/history/components/aaa/events/seq.lock"
	spy.preExisting[lockPath] = preBody

	if err := st.AppendComponentEvent(context.Background(), makeEvent("aaa", catalogmodel.EventCatalogResolved)); err != nil {
		t.Fatalf("AppendComponentEvent: %v", err)
	}
	want, _ := catalogstore.ComponentHistoryEventPath(testSrcKey, testCatKey, "aaa", 5, catalogmodel.EventCatalogResolved)
	if _, ok := spy.objects[want]; !ok {
		t.Errorf("expected event at seq=5 (%s); have %v", want, keys(spy.objects))
	}
	// Lock should now have next=6.
	body := spy.objects[lockPath]
	var env struct {
		Next uint64 `json:"next"`
	}
	_ = json.Unmarshal(body, &env)
	if env.Next != 6 {
		t.Errorf("seq.lock.next=%d, want 6", env.Next)
	}
}
