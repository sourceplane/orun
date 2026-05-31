package catalogstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/statestore"
)

// refsRetryBudget is the maximum number of CompareAndSwap attempts the
// ref writer will issue per ref before surfacing ErrRefStale. Matches
// the seq.lock allocator budget for taxonomy consistency.
const refsRetryBudget = 16

// WriteRefs implements step D of catalog-store.md §3:
//
//	D.1 refs/sources/current.json   (CAS, retry on conflict)
//	D.2 refs/catalogs/current.json  (CAS, retry on conflict)
//	D.3 if Authoritative: refs/{sources,catalogs}/main.json
//	D.4 if Branch != "": refs/{sources,catalogs}/branches/<branch>.json
//	D.5 if PR != "":     refs/{sources,catalogs}/prs/<pr>.json
//	D.6 refs/sources/latest.json, refs/catalogs/latest.json (always last)
//
// Each ref is written via a CreateIfAbsent → CompareAndSwap retry loop:
// initial write attempts CreateIfAbsent (CAS with empty oldRev is
// undefined per statestore semantics); on ErrExists with a body that
// already matches what we want to write, success; on ErrExists with a
// divergent body or any non-Exists error from CreateIfAbsent we fall
// through to the CAS retry loop.
//
// When refs.Source == nil every D.* targeting a source ref is skipped;
// likewise for refs.Catalog. Both nil → no-op success.
func (s *store) WriteRefs(ctx context.Context, refs RefUpdate) error {
	if refs.Source == nil && refs.Catalog == nil {
		return nil
	}

	// Build the deterministic ordered work list per spec §3.D.
	type refWrite struct {
		path string
		body []byte
	}
	var writes []refWrite

	// Helper to encode + path-build per side.
	addSource := func(pathFn func() (string, error)) error {
		if refs.Source == nil {
			return nil
		}
		p, err := pathFn()
		if err != nil {
			return fmt.Errorf("WriteRefs: source ref path: %w", err)
		}
		body, err := catalogmodel.PrettyEncode(*refs.Source)
		if err != nil {
			return fmt.Errorf("WriteRefs: encode source ref: %w", err)
		}
		writes = append(writes, refWrite{p, body})
		return nil
	}
	addCatalog := func(pathFn func() (string, error)) error {
		if refs.Catalog == nil {
			return nil
		}
		p, err := pathFn()
		if err != nil {
			return fmt.Errorf("WriteRefs: catalog ref path: %w", err)
		}
		body, err := catalogmodel.PrettyEncode(*refs.Catalog)
		if err != nil {
			return fmt.Errorf("WriteRefs: encode catalog ref: %w", err)
		}
		writes = append(writes, refWrite{p, body})
		return nil
	}

	// D.1 sources/current
	if err := addSource(func() (string, error) { return SourceRefPath(catalogmodel.RefNameCurrent) }); err != nil {
		return err
	}
	// D.2 catalogs/current
	if err := addCatalog(func() (string, error) { return CatalogRefPath(catalogmodel.RefNameCurrent) }); err != nil {
		return err
	}
	// D.3 main (per-side authoritative flag)
	if refs.Source != nil && refs.Source.Authoritative {
		if err := addSource(func() (string, error) { return SourceRefPath(catalogmodel.RefNameMain) }); err != nil {
			return err
		}
	}
	if refs.Catalog != nil && refs.Catalog.Authoritative {
		if err := addCatalog(func() (string, error) { return CatalogRefPath(catalogmodel.RefNameMain) }); err != nil {
			return err
		}
	}
	// D.4 branch (per RefUpdate.Branch — the SourceRef/CatalogRef body
	// itself doesn't carry a branch field; scope label is on the
	// RefUpdate envelope).
	if refs.Branch != "" {
		branchSeg := catalogmodel.SanitizeBranch(refs.Branch)
		if branchSeg == "" {
			return fmt.Errorf("%w: branch %q sanitized to empty", ErrInvalidPathInput, refs.Branch)
		}
		if err := addSource(func() (string, error) { return SourceBranchRefPath(branchSeg) }); err != nil {
			return err
		}
		if err := addCatalog(func() (string, error) { return CatalogBranchRefPath(branchSeg) }); err != nil {
			return err
		}
	}
	// D.5 PR
	if refs.PR != "" {
		if err := addSource(func() (string, error) { return SourcePRRefPath(refs.PR) }); err != nil {
			return err
		}
		if err := addCatalog(func() (string, error) { return CatalogPRRefPath(refs.PR) }); err != nil {
			return err
		}
	}
	// D.6 latest (always last)
	if err := addSource(func() (string, error) { return SourceRefPath(catalogmodel.RefNameLatest) }); err != nil {
		return err
	}
	if err := addCatalog(func() (string, error) { return CatalogRefPath(catalogmodel.RefNameLatest) }); err != nil {
		return err
	}

	for _, w := range writes {
		if err := s.writeRefCAS(ctx, w.path, w.body); err != nil {
			return err
		}
	}
	return nil
}

// writeRefCAS is the per-ref CreateIfAbsent → CompareAndSwap retry loop.
// Returns ErrRefStale wrapping the last statestore.ErrConflict on
// retry-budget exhaustion.
func (s *store) writeRefCAS(ctx context.Context, p string, want []byte) error {
	// Initial attempt: CreateIfAbsent. CAS with empty oldRev is
	// undefined per statestore semantics, so we MUST attempt
	// CreateIfAbsent first when the object may not exist yet.
	_, err := s.state.CreateIfAbsent(ctx, p, want)
	if err == nil {
		return nil
	}
	if !errors.Is(err, statestore.ErrExists) {
		return fmt.Errorf("WriteRefs: CreateIfAbsent %s: %w", p, err)
	}
	// Object exists. Check current body — if byte-identical we're done.
	got, meta, readErr := s.state.Read(ctx, p)
	if readErr != nil {
		return fmt.Errorf("WriteRefs: post-Exists Read %s: %w", p, readErr)
	}
	if bytes.Equal(got, want) {
		return nil
	}

	// Divergent body — enter the CAS retry loop.
	oldRev := meta.Revision
	var lastConflict error
	for attempt := 0; attempt < refsRetryBudget; attempt++ {
		_, casErr := s.state.CompareAndSwap(ctx, p, oldRev, want)
		if casErr == nil {
			return nil
		}
		if !errors.Is(casErr, statestore.ErrConflict) {
			return fmt.Errorf("WriteRefs: CompareAndSwap %s: %w", p, casErr)
		}
		lastConflict = casErr
		// Re-read for fresh oldRev. If body now matches, success.
		got, meta, readErr = s.state.Read(ctx, p)
		if readErr != nil {
			return fmt.Errorf("WriteRefs: re-Read %s after conflict: %w", p, readErr)
		}
		if bytes.Equal(got, want) {
			return nil
		}
		oldRev = meta.Revision
	}
	// Retry budget exhausted — wrap the last conflict so callers can
	// errors.Is against both ErrRefStale and statestore.ErrConflict.
	return fmt.Errorf("%w: %w", ErrRefStale, lastConflict)
}
