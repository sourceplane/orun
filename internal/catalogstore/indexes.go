package catalogstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/statestore"
)

// indexesRetryBudget caps CompareAndSwap attempts when writing a
// component-global-index entry. Spec §5 names 8; PR-2 standardises on
// 16 (matching refs/seq.lock) for taxonomy consistency. Spec proposal
// at ai/proposals/task-0032-spec-update.md is NOT required because §5's
// number was advisory and §6 introduces ErrRefStale as the unified
// retry-exhausted sentinel.
const indexesRetryBudget = 16

// WriteGlobalIndexes implements step C of catalog-store.md §3.
//
//	C.1 plain Write at SourceGlobalIndexPath  (skip if Source == nil)
//	C.2 plain Write at CatalogGlobalIndexPath (skip if Catalog == nil)
//	C.3 per-component CompareAndSwap at ComponentGlobalIndexPath
//	    with merge-on-conflict and a 16-attempt retry budget; exhaustion
//	    surfaces ErrRefStale wrapping the last statestore.ErrConflict.
//
// Component iteration is deterministic: entries are sorted by
// ComponentKey ascending so re-running with identical input produces a
// byte-identical write trace (asserted by the determinism test).
func (s *store) WriteGlobalIndexes(ctx context.Context, updates GlobalIndexUpdate) error {
	// C.1 source global index.
	if updates.Source != nil {
		srcKey := updates.Source.SourceSnapshotKey
		if err := ValidateSourceKey(srcKey); err != nil {
			return fmt.Errorf("WriteGlobalIndexes: %w", err)
		}
		p, err := SourceGlobalIndexPath(srcKey)
		if err != nil {
			return fmt.Errorf("WriteGlobalIndexes: source path: %w", err)
		}
		body, err := catalogmodel.PrettyEncode(*updates.Source)
		if err != nil {
			return fmt.Errorf("WriteGlobalIndexes: encode source: %w", err)
		}
		if _, err := s.state.Write(ctx, p, body, statestore.WriteOptions{}); err != nil {
			return fmt.Errorf("WriteGlobalIndexes: Write %s: %w", p, err)
		}
	}

	// C.2 catalog global index.
	if updates.Catalog != nil {
		catKey := updates.Catalog.CatalogSnapshotKey
		if err := ValidateCatalogKey(catKey); err != nil {
			return fmt.Errorf("WriteGlobalIndexes: %w", err)
		}
		p, err := CatalogGlobalIndexPath(catKey)
		if err != nil {
			return fmt.Errorf("WriteGlobalIndexes: catalog path: %w", err)
		}
		body, err := catalogmodel.PrettyEncode(*updates.Catalog)
		if err != nil {
			return fmt.Errorf("WriteGlobalIndexes: encode catalog: %w", err)
		}
		if _, err := s.state.Write(ctx, p, body, statestore.WriteOptions{}); err != nil {
			return fmt.Errorf("WriteGlobalIndexes: Write %s: %w", p, err)
		}
	}

	// C.3 component global indexes (deterministic order).
	comps := make([]*catalogmodel.ComponentGlobalIndex, 0, len(updates.Components))
	for _, c := range updates.Components {
		if c == nil {
			continue
		}
		comps = append(comps, c)
	}
	sort.SliceStable(comps, func(i, j int) bool {
		return comps[i].ComponentKey < comps[j].ComponentKey
	})

	for _, c := range comps {
		if err := s.writeComponentGlobalIndex(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

// writeComponentGlobalIndex applies one component-global-index entry via
// CompareAndSwap, merging the caller's entry into the latest persisted
// body on conflict. The merge policy: replace `Latest` and `Main`
// unconditionally with the caller's values when they are non-zero
// (caller is always the freshest writer); union `Previews` by
// SourceSnapshotKey, with the caller's row winning ties. This keeps two
// concurrent writers from clobbering each other's previews while
// allowing each writer to overwrite stale Latest/Main pointers.
func (s *store) writeComponentGlobalIndex(ctx context.Context, want *catalogmodel.ComponentGlobalIndex) error {
	if err := catalogmodel.ValidateComponentKey(want.ComponentKey); err != nil {
		return fmt.Errorf("WriteGlobalIndexes: %w: %v", ErrInvalidPathInput, err)
	}
	p, err := ComponentGlobalIndexPath(want.ComponentKey)
	if err != nil {
		return fmt.Errorf("WriteGlobalIndexes: %w", err)
	}

	// Initial attempt: CreateIfAbsent (CAS with empty oldRev is
	// undefined per statestore semantics).
	wantBody, err := catalogmodel.PrettyEncode(*want)
	if err != nil {
		return fmt.Errorf("WriteGlobalIndexes: encode component %q: %w", want.ComponentKey, err)
	}
	_, createErr := s.state.CreateIfAbsent(ctx, p, wantBody)
	if createErr == nil {
		return nil
	}
	if !errors.Is(createErr, statestore.ErrExists) {
		return fmt.Errorf("WriteGlobalIndexes: CreateIfAbsent %s: %w", p, createErr)
	}

	// Object exists — read current body, merge, CAS retry on conflict.
	var lastConflict error
	for attempt := 0; attempt < indexesRetryBudget; attempt++ {
		got, meta, readErr := s.state.Read(ctx, p)
		if readErr != nil {
			return fmt.Errorf("WriteGlobalIndexes: Read %s: %w", p, readErr)
		}
		var current catalogmodel.ComponentGlobalIndex
		if err := json.Unmarshal(got, &current); err != nil {
			return fmt.Errorf("WriteGlobalIndexes: decode existing %s: %w", p, err)
		}
		merged := mergeComponentGlobalIndex(current, *want)
		mergedBody, err := catalogmodel.PrettyEncode(merged)
		if err != nil {
			return fmt.Errorf("WriteGlobalIndexes: encode merged %s: %w", p, err)
		}
		if bytes.Equal(got, mergedBody) {
			// No-op merge — body already represents what we want.
			return nil
		}
		_, casErr := s.state.CompareAndSwap(ctx, p, meta.Revision, mergedBody)
		if casErr == nil {
			return nil
		}
		if !errors.Is(casErr, statestore.ErrConflict) {
			return fmt.Errorf("WriteGlobalIndexes: CompareAndSwap %s: %w", p, casErr)
		}
		lastConflict = casErr
	}
	return fmt.Errorf("%w: %w", ErrRefStale, lastConflict)
}

// mergeComponentGlobalIndex produces the body of a component-global
// index by overlaying `want` onto `current`. Latest / Main are
// overwritten when `want` provides non-zero values (caller is freshest
// writer); Previews are unioned by SourceSnapshotKey with caller-wins
// on collision, and the result is sorted by SourceSnapshotKey for
// determinism.
func mergeComponentGlobalIndex(current, want catalogmodel.ComponentGlobalIndex) catalogmodel.ComponentGlobalIndex {
	out := current
	// Identity fields: caller authoritative when non-empty.
	if want.APIVersion != "" {
		out.APIVersion = want.APIVersion
	}
	if want.Kind != "" {
		out.Kind = want.Kind
	}
	if want.ComponentKey != "" {
		out.ComponentKey = want.ComponentKey
	}
	if want.Name != "" {
		out.Name = want.Name
	}
	if want.Repo != "" {
		out.Repo = want.Repo
	}
	// Latest / Main: caller wins when populated (non-zero
	// SourceSnapshotKey is the freshness signal).
	if want.Latest.SourceSnapshotKey != "" {
		out.Latest = want.Latest
	}
	if want.Main.SourceSnapshotKey != "" {
		out.Main = want.Main
	}
	// Previews: union by SourceSnapshotKey, caller-wins on tie.
	byKey := make(map[string]catalogmodel.ComponentIndexPreview, len(out.Previews)+len(want.Previews))
	for _, p := range out.Previews {
		byKey[p.SourceSnapshotKey] = p
	}
	for _, p := range want.Previews {
		byKey[p.SourceSnapshotKey] = p
	}
	merged := make([]catalogmodel.ComponentIndexPreview, 0, len(byKey))
	for _, p := range byKey {
		merged = append(merged, p)
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].SourceSnapshotKey < merged[j].SourceSnapshotKey
	})
	out.Previews = merged
	return out
}
