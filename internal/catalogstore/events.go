package catalogstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/statestore"
)

// eventSeqRetryBudget caps CreateIfAbsent attempts on the seq.lock
// allocator before surfacing ErrRefStale.
const eventSeqRetryBudget = 16

// AppendComponentEvent implements catalog-store.md §3.D's history-event
// paragraph and §5's seq.lock allocator semantics.
//
// Allocation:
//
//   - <eventsDir>/seq.lock holds a JSON envelope `{"next":<uint64>}`
//     with the NEXT sequence number to issue. Initial CreateIfAbsent
//     stores `{"next": 2}` (we just allocated 1).
//   - On ErrExists: read the current value, allocate it, write back
//     value+1 via CompareAndSwap. On conflict: retry with re-read.
//   - Retry budget: 16 attempts. Exhaustion → ErrRefStale wrapping the
//     last statestore.ErrConflict (or statestore.ErrExists for the
//     initial-create race).
//
// Body write:
//
//   - Once <seq> is allocated, the event body lands at
//     ComponentHistoryEventPath(srcKey, catKey, name, seq, kind) via
//     CreateIfAbsent (events are immutable per spec). PrettyEncode for
//     consistency with PR-1 body writes.
func (s *store) AppendComponentEvent(ctx context.Context, ev catalogmodel.ComponentHistoryEvent) error {
	if ev.SourceSnapshotKey == "" {
		return fmt.Errorf("AppendComponentEvent: SourceSnapshotKey is empty")
	}
	if ev.CatalogSnapshotKey == "" {
		return fmt.Errorf("AppendComponentEvent: CatalogSnapshotKey is empty")
	}
	if ev.EventType == "" {
		return fmt.Errorf("AppendComponentEvent: EventType is empty")
	}
	// ComponentKey is required so we can derive the component name
	// segment from its tail (`<ns>/<repo>/<name>`).
	if err := catalogmodel.ValidateComponentKey(ev.ComponentKey); err != nil {
		return fmt.Errorf("AppendComponentEvent: %w: %v", ErrInvalidPathInput, err)
	}
	name := componentKeyTail(ev.ComponentKey)
	if name == "" {
		return fmt.Errorf("%w: empty component name segment in %q", ErrInvalidPathInput, ev.ComponentKey)
	}
	if err := ValidateSourceKey(ev.SourceSnapshotKey); err != nil {
		return fmt.Errorf("AppendComponentEvent: %w", err)
	}
	if err := ValidateCatalogKey(ev.CatalogSnapshotKey); err != nil {
		return fmt.Errorf("AppendComponentEvent: %w", err)
	}
	if err := ValidateEventKind(ev.EventType); err != nil {
		return fmt.Errorf("AppendComponentEvent: %w", err)
	}

	seq, err := s.allocateEventSeq(ctx, ev.SourceSnapshotKey, ev.CatalogSnapshotKey, name)
	if err != nil {
		return err
	}

	bodyPath, err := ComponentHistoryEventPath(ev.SourceSnapshotKey, ev.CatalogSnapshotKey, name, seq, ev.EventType)
	if err != nil {
		return fmt.Errorf("AppendComponentEvent: body path: %w", err)
	}
	body, err := catalogmodel.PrettyEncode(ev)
	if err != nil {
		return fmt.Errorf("AppendComponentEvent: encode: %w", err)
	}
	if _, err := s.state.CreateIfAbsent(ctx, bodyPath, body); err != nil {
		// Events are immutable; ErrExists at the body path means a
		// concurrent writer beat us to <seq>+kind, which our allocator
		// should have prevented. Surface as-is.
		return fmt.Errorf("AppendComponentEvent: CreateIfAbsent body %s: %w", bodyPath, err)
	}
	return nil
}

// componentKeyTail returns the trailing `<name>` segment of a 3-segment
// componentKey (`<ns>/<repo>/<name>`). Returns "" for malformed keys
// (callers should ValidateComponentKey first).
func componentKeyTail(componentKey string) string {
	for i := len(componentKey) - 1; i >= 0; i-- {
		if componentKey[i] == '/' {
			return componentKey[i+1:]
		}
	}
	return componentKey
}

// seqLockEnvelope is the JSON shape stored at <eventsDir>/seq.lock.
// The single field `Next` is the NEXT sequence number to issue (the
// allocator allocates `Next` and bumps to `Next+1`).
type seqLockEnvelope struct {
	Next uint64 `json:"next"`
}

// allocateEventSeq returns a unique <seq> for the next event under the
// component's history directory. Initial allocation: CreateIfAbsent on
// seq.lock with `{"next": 2}` (we just allocated 1). Otherwise:
// CompareAndSwap loop with merge-on-conflict.
func (s *store) allocateEventSeq(ctx context.Context, srcKey, catKey, name string) (uint64, error) {
	// Path: <eventsDir>/seq.lock — same parent as the events
	// themselves, per integration note in task-0032.md.
	dir, err := CatalogDir(srcKey, catKey)
	if err != nil {
		return 0, fmt.Errorf("AppendComponentEvent: %w", err)
	}
	if err := ValidateComponentName(name); err != nil {
		return 0, fmt.Errorf("AppendComponentEvent: %w", err)
	}
	lockPath := dir + "/history/components/" + name + "/events/seq.lock"

	// Initial: try CreateIfAbsent with next=2 (we allocate 1).
	initialBody, err := json.Marshal(seqLockEnvelope{Next: 2})
	if err != nil {
		return 0, fmt.Errorf("AppendComponentEvent: encode initial seq.lock: %w", err)
	}
	_, createErr := s.state.CreateIfAbsent(ctx, lockPath, initialBody)
	if createErr == nil {
		return 1, nil
	}
	if !errors.Is(createErr, statestore.ErrExists) {
		return 0, fmt.Errorf("AppendComponentEvent: CreateIfAbsent %s: %w", lockPath, createErr)
	}

	// Lock file exists. CAS loop.
	var lastErr error = createErr
	for attempt := 0; attempt < eventSeqRetryBudget; attempt++ {
		got, meta, readErr := s.state.Read(ctx, lockPath)
		if readErr != nil {
			return 0, fmt.Errorf("AppendComponentEvent: Read %s: %w", lockPath, readErr)
		}
		var env seqLockEnvelope
		if err := json.Unmarshal(got, &env); err != nil {
			return 0, fmt.Errorf("AppendComponentEvent: decode %s: %w", lockPath, err)
		}
		if env.Next == 0 {
			return 0, fmt.Errorf("AppendComponentEvent: invalid seq.lock at %s (next=0)", lockPath)
		}
		allocated := env.Next
		newBody, err := json.Marshal(seqLockEnvelope{Next: allocated + 1})
		if err != nil {
			return 0, fmt.Errorf("AppendComponentEvent: encode seq.lock: %w", err)
		}
		if bytes.Equal(got, newBody) {
			// Defensive: should be impossible since Next changes.
			return allocated, nil
		}
		_, casErr := s.state.CompareAndSwap(ctx, lockPath, meta.Revision, newBody)
		if casErr == nil {
			return allocated, nil
		}
		if !errors.Is(casErr, statestore.ErrConflict) {
			return 0, fmt.Errorf("AppendComponentEvent: CompareAndSwap %s: %w", lockPath, casErr)
		}
		lastErr = casErr
	}
	return 0, fmt.Errorf("%w: %w", ErrRefStale, lastErr)
}
