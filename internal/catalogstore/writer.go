package catalogstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/statestore"
)

// WriteSourceSnapshot implements step A of catalog-store.md §3.
//
//	A. CreateIfAbsent(SourceDocPath(srcKey), canonicalJSON(src))
//	   - On ErrExists with byte-identical body: success.
//	   - On ErrExists with different body: ErrSourceMismatch (wraps statestore.ErrExists).
func (s *store) WriteSourceSnapshot(ctx context.Context, src catalogmodel.SourceSnapshot) error {
	if err := ValidateSourceKey(src.SourceSnapshotKey); err != nil {
		return fmt.Errorf("WriteSourceSnapshot: invalid SourceSnapshotKey: %w", err)
	}
	docPath, err := SourceDocPath(src.SourceSnapshotKey)
	if err != nil {
		return fmt.Errorf("WriteSourceSnapshot: %w", err)
	}
	body, err := catalogmodel.PrettyEncode(src)
	if err != nil {
		return fmt.Errorf("WriteSourceSnapshot: encode: %w", err)
	}
	if _, err := s.state.CreateIfAbsent(ctx, docPath, body); err != nil {
		if errors.Is(err, statestore.ErrExists) {
			return s.reconcileSourceBody(ctx, docPath, body, err)
		}
		return fmt.Errorf("WriteSourceSnapshot: CreateIfAbsent %s: %w", docPath, err)
	}
	return nil
}

// reconcileSourceBody is the ErrExists branch of step A — read the
// existing body and either succeed (byte-identical) or surface
// ErrSourceMismatch.
func (s *store) reconcileSourceBody(ctx context.Context, docPath string, want []byte, origErr error) error {
	got, _, readErr := s.state.Read(ctx, docPath)
	if readErr != nil {
		// Couldn't read what we just collided with — surface the
		// original ErrExists so callers still see the statestore
		// sentinel via errors.Is.
		return fmt.Errorf("WriteSourceSnapshot: ErrExists at %s, follow-up Read failed: %v: %w", docPath, readErr, origErr)
	}
	if bytes.Equal(got, want) {
		return nil
	}
	// Wrap BOTH ErrSourceMismatch and the original statestore.ErrExists
	// so errors.Is succeeds against either sentinel.
	return fmt.Errorf("%w: %w", ErrSourceMismatch, origErr)
}

// WriteCatalogSnapshot implements steps B.1 → B.4 of
// catalog-store.md §3, in order.
func (s *store) WriteCatalogSnapshot(
	ctx context.Context,
	src catalogmodel.SourceSnapshot,
	cat catalogmodel.CatalogSnapshot,
	manifests []catalogmodel.ComponentManifest,
	graphs CatalogGraphs,
	localIndexes CatalogLocalIndexes,
) error {
	// Pre-flight: validate keys + linkage. NO writes are issued before
	// every input is proven self-consistent.
	if err := s.preflightCatalogInputs(src, cat, manifests); err != nil {
		return err
	}

	srcKey := src.SourceSnapshotKey
	catKey := cat.CatalogSnapshotKey

	// B.1 — manifests in stable order. We honour the order the caller
	// passes (catalogresolve already sorts by componentKey); we do not
	// re-sort here so a deterministic upstream produces a deterministic
	// write trace.
	for i := range manifests {
		m := manifests[i]
		manifestPath, err := ComponentManifestPath(srcKey, catKey, m.Identity.Name)
		if err != nil {
			return fmt.Errorf("WriteCatalogSnapshot: manifest path for %q: %w", m.Identity.Name, err)
		}
		body, err := catalogmodel.PrettyEncode(m)
		if err != nil {
			return fmt.Errorf("WriteCatalogSnapshot: encode manifest %q: %w", m.Identity.Name, err)
		}
		if err := s.createOrReconcile(ctx, manifestPath, body, ErrManifestMismatch); err != nil {
			return fmt.Errorf("WriteCatalogSnapshot: manifest %q: %w", m.Identity.Name, err)
		}
	}

	// B.2 — graphs in fixed order.
	graphByKind := map[string]*catalogmodel.CatalogGraph{
		"dependencies": graphs.Dependencies,
		"systems":      graphs.Systems,
		"apis":         graphs.APIs,
		"resources":    graphs.Resources,
		"owners":       graphs.Owners,
	}
	for _, kind := range CatalogGraphKinds() {
		g := graphByKind[kind]
		if g == nil {
			continue
		}
		graphPath, err := CatalogGraphPath(srcKey, catKey, kind)
		if err != nil {
			return fmt.Errorf("WriteCatalogSnapshot: graph path %q: %w", kind, err)
		}
		body, err := catalogmodel.PrettyEncode(g)
		if err != nil {
			return fmt.Errorf("WriteCatalogSnapshot: encode graph %q: %w", kind, err)
		}
		// Graphs use the same idempotent CreateIfAbsent pattern as
		// manifests but reuse ErrCatalogMismatch for body divergence —
		// graphs are part of the catalog snapshot and a divergent graph
		// body for the same (srcKey,catKey) is a catalog-level
		// invariant violation, not a manifest-level one.
		if err := s.createOrReconcile(ctx, graphPath, body, ErrCatalogMismatch); err != nil {
			return fmt.Errorf("WriteCatalogSnapshot: graph %q: %w", kind, err)
		}
	}

	// B.3 — catalog doc.
	catalogPath, err := CatalogDocPath(srcKey, catKey)
	if err != nil {
		return fmt.Errorf("WriteCatalogSnapshot: catalog doc path: %w", err)
	}
	catBody, err := catalogmodel.PrettyEncode(cat)
	if err != nil {
		return fmt.Errorf("WriteCatalogSnapshot: encode catalog: %w", err)
	}
	if err := s.createOrReconcile(ctx, catalogPath, catBody, ErrCatalogMismatch); err != nil {
		return fmt.Errorf("WriteCatalogSnapshot: catalog doc: %w", err)
	}

	// B.4 — catalog-local indexes via plain Write (overwrite is fine;
	// rebuildable). Iteration order across the five axes is fixed; per-
	// axis order is the caller-supplied map order, but tests that need
	// determinism iterate over a single axis at a time, and the writer
	// guarantees only the inter-axis order documented in the spec.
	if err := s.writeLocalIndexes(ctx, srcKey, catKey, localIndexes); err != nil {
		return err
	}

	return nil
}

// preflightCatalogInputs is the pre-flight ErrInputsInconsistent guard.
// All three input shapes must agree on the (sourceSnapshotKey,
// catalogSnapshotKey) pair before any write hits disk.
func (s *store) preflightCatalogInputs(
	src catalogmodel.SourceSnapshot,
	cat catalogmodel.CatalogSnapshot,
	manifests []catalogmodel.ComponentManifest,
) error {
	if err := ValidateSourceKey(src.SourceSnapshotKey); err != nil {
		return fmt.Errorf("WriteCatalogSnapshot: invalid SourceSnapshotKey: %w", err)
	}
	if err := ValidateCatalogKey(cat.CatalogSnapshotKey); err != nil {
		return fmt.Errorf("WriteCatalogSnapshot: invalid CatalogSnapshotKey: %w", err)
	}
	if cat.SourceSnapshotKey != src.SourceSnapshotKey {
		return fmt.Errorf(
			"%w: cat.SourceSnapshotKey=%q does not match src.SourceSnapshotKey=%q",
			ErrInputsInconsistent, cat.SourceSnapshotKey, src.SourceSnapshotKey,
		)
	}
	for i := range manifests {
		m := manifests[i]
		if m.Source.SourceSnapshotKey != src.SourceSnapshotKey {
			return fmt.Errorf(
				"%w: manifests[%d](%s).Source.SourceSnapshotKey=%q does not match src.SourceSnapshotKey=%q",
				ErrInputsInconsistent, i, m.Identity.Name, m.Source.SourceSnapshotKey, src.SourceSnapshotKey,
			)
		}
		if m.Source.CatalogSnapshotKey != cat.CatalogSnapshotKey {
			return fmt.Errorf(
				"%w: manifests[%d](%s).Source.CatalogSnapshotKey=%q does not match cat.CatalogSnapshotKey=%q",
				ErrInputsInconsistent, i, m.Identity.Name, m.Source.CatalogSnapshotKey, cat.CatalogSnapshotKey,
			)
		}
	}
	return nil
}

// createOrReconcile is the shared idempotent-or-mismatch branch used by
// every CreateIfAbsent path in step B. mismatchErr is the typed sentinel
// (ErrManifestMismatch / ErrCatalogMismatch) the caller wants surfaced
// when the existing body differs from `body`.
func (s *store) createOrReconcile(ctx context.Context, p string, body []byte, mismatchErr error) error {
	if _, err := s.state.CreateIfAbsent(ctx, p, body); err != nil {
		if errors.Is(err, statestore.ErrExists) {
			got, _, readErr := s.state.Read(ctx, p)
			if readErr != nil {
				return fmt.Errorf("ErrExists at %s, follow-up Read failed: %v: %w", p, readErr, err)
			}
			if bytes.Equal(got, body) {
				return nil
			}
			// Double-wrap so errors.Is matches mismatchErr AND the
			// underlying statestore.ErrExists.
			return fmt.Errorf("%w: %w", mismatchErr, err)
		}
		return fmt.Errorf("CreateIfAbsent %s: %w", p, err)
	}
	return nil
}

// writeLocalIndexes is the B.4 fan-out. Plain Write per the spec
// (overwrite is fine; local indexes are rebuildable).
func (s *store) writeLocalIndexes(
	ctx context.Context,
	srcKey, catKey string,
	idx CatalogLocalIndexes,
) error {
	type axis struct {
		name   string
		entries map[string]any
		path   func(srcKey, catKey, key string) (string, error)
	}
	axes := []axis{
		{"components", idx.Components, ComponentLocalIndexPath},
		{"owners", idx.Owners, OwnerLocalIndexPath},
		{"systems", idx.Systems, SystemLocalIndexPath},
		{"domains", idx.Domains, DomainLocalIndexPath},
		{"types", idx.Types, TypeLocalIndexPath},
	}
	for _, a := range axes {
		for key, body := range a.entries {
			p, err := a.path(srcKey, catKey, key)
			if err != nil {
				return fmt.Errorf("WriteCatalogSnapshot: local index %s/%s: %w", a.name, key, err)
			}
			encoded, err := catalogmodel.PrettyEncode(body)
			if err != nil {
				return fmt.Errorf("WriteCatalogSnapshot: encode local index %s/%s: %w", a.name, key, err)
			}
			if _, err := s.state.Write(ctx, p, encoded, statestore.WriteOptions{}); err != nil {
				return fmt.Errorf("WriteCatalogSnapshot: Write local index %s: %w", p, err)
			}
		}
	}
	return nil
}
