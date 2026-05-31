package catalogstore

import "errors"

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

// ErrNotImplemented is returned by Writer/Resolver methods whose bodies
// are scheduled for later C4 PRs:
//
//	WriteRefs               — PR-2
//	WriteGlobalIndexes      — PR-2
//	AppendComponentEvent    — PR-2
//	Resolver methods        — PR-3
//
// The error wraps errors.ErrUnsupported so callers can detect a
// not-yet-wired surface uniformly with errors.Is(err, errors.ErrUnsupported).
var ErrNotImplemented = errors.New("catalogstore: not implemented in this PR (filled in by C4 PR-2 / PR-3)")
