package catalogstore

import (
	"errors"

	"github.com/sourceplane/orun/internal/statestore"
)

// ErrSourceMismatch is returned by WriteSourceSnapshot when an existing
// source.json under the same srcKey has a body that differs from the
// caller-supplied source. Wraps statestore.ErrExists so callers can keep
// using errors.Is(err, statestore.ErrExists) for the underlying signal.
var ErrSourceMismatch = errors.New("catalogstore: source body conflict for same key")

// ErrCatalogMismatch is returned by WriteCatalogSnapshot when an existing
// catalog.json under the same (srcKey, catKey) has a body that differs
// from the caller-supplied catalog. Wraps statestore.ErrExists.
var ErrCatalogMismatch = errors.New("catalogstore: catalog body conflict for same key")

// ErrManifestMismatch is returned by WriteCatalogSnapshot when an
// existing component manifest has a body that differs from the
// caller-supplied manifest at the same path. Wraps statestore.ErrExists.
var ErrManifestMismatch = errors.New("catalogstore: manifest body conflict for same key")

// ErrInputsInconsistent is returned by WriteCatalogSnapshot before any
// write is issued when the source/catalog/manifest tuple supplied to the
// writer is internally inconsistent — i.e. when
// `cat.SourceSnapshotKey != src.SourceSnapshotKey`, or when any
// manifest's `Source.SourceSnapshotKey` / `Source.CatalogSnapshotKey`
// disagrees with `src.SourceSnapshotKey` / `cat.CatalogSnapshotKey`.
//
// This is the writer-side mirror of the in-memory invariant
// internal/catalogresolve.BuildCatalog guarantees. It catches
// programmer-mistake wiring (e.g. a caller passing the wrong source
// alongside a freshly built catalog) before any partial write reaches
// disk.
var ErrInputsInconsistent = errors.New("catalogstore: inputs inconsistent (src/cat/manifests linkage)")

// ErrRefStale is returned by WriteRefs, WriteGlobalIndexes (component
// global indexes), and AppendComponentEvent (seq.lock allocator) when
// the CompareAndSwap / CreateIfAbsent retry budget is exhausted without
// reaching a stable state. Wraps the last underlying statestore conflict
// (statestore.ErrConflict for CAS, statestore.ErrExists for the seq.lock
// allocator) via the fmt.Errorf("%w: %w", ...) double-wrap pattern, so
// callers may use errors.Is against either sentinel.
//
// PR-2 picks a single retry-exhausted sentinel for taxonomy minimalism
// (vs split ErrGlobalIndexStale / ErrEventSeqExhausted): every retry-
// exhausted path in PR-2 returns ErrRefStale, with the message text and
// the wrapped statestore sentinel naming the actual surface.
var ErrRefStale = errors.New("catalogstore: retry budget exhausted (ref / global index / seq.lock CAS)")

// ErrNotImplemented is returned by Resolver methods whose bodies are
// scheduled for C4 PR-3. PR-2 wires up every Writer method, so the only
// remaining stubs are on the Resolver surface.
//
// The error wraps errors.ErrUnsupported so callers can detect a
// not-yet-wired surface uniformly with errors.Is(err, errors.ErrUnsupported).
var ErrNotImplemented = errors.New("catalogstore: not implemented in this PR (filled in by C4 PR-3)")

// ErrCatalogNotFound is returned by Resolver methods when no catalog
// matches the supplied selector — neither via the typed `refs/catalogs/*`
// pointer nor via the spec §4 fallback walk under
// `sources/<srcKey>/catalogs/*/catalog.json`. Wraps statestore.ErrNotFound
// so callers may use errors.Is against either sentinel.
//
// Mirror of catalog-store.md §6.
var ErrCatalogNotFound = errors.New("catalogstore: no catalog for selector")

// ErrComponentNotFound is returned by ResolveComponent /
// ResolveComponentLatest when no manifest exists for the requested
// component name (or 3-segment componentKey) under the resolved catalog —
// neither via the global component index nor via the §4 fallback walk.
// Wraps statestore.ErrNotFound.
//
// Mirror of catalog-store.md §6.
var ErrComponentNotFound = errors.New("catalogstore: no manifest for component in catalog")

// errNotFoundChain wraps a Resolver-surface sentinel together with
// statestore.ErrNotFound via errors.Join so the returned error satisfies
// errors.Is against both sentinels. Used by resolver.go to keep the wrap
// pattern consistent across every read-side fall-through.
func errNotFoundChain(surface error) error {
	return errors.Join(surface, statestore.ErrNotFound)
}
