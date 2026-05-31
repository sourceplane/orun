// Package catalogstore is the persistence boundary for catalog objects
// (sources, catalogs, component manifests, graphs, local/global indexes,
// refs, history events). Every byte written under .orun/sources/... flows
// through this package; raw path concatenation is forbidden.
//
// Layering:
//
//   - This package is a thin layer over internal/statestore (Phase 1) — it
//     adds path conventions, write ordering, typed refs, and reader
//     fallback rules per specs/orun-component-catalog/catalog-store.md.
//
//   - The package depends on internal/statestore and
//     internal/catalogmodel. Both are leaves in the dependency graph; this
//     package introduces no other internal/* imports.
//
// Scope of this PR (Milestone C4 PR-1):
//
//   - paths.go ............... every helper named in catalog-store.md §2
//     plus matching Validate* siblings. Helpers return (string, error);
//     none panic.
//   - writer.go .............. WriteSourceSnapshot (step A) and
//     WriteCatalogSnapshot (steps B.1 → B.4). The remaining Writer
//     methods (WriteRefs, WriteGlobalIndexes, AppendComponentEvent) and
//     all Resolver methods return ErrNotImplemented; bodies arrive in
//     PR-2 / PR-3.
//   - errors.go .............. typed errors used by step-A/B writes.
//   - store.go ............... public surface (Writer/Resolver/Store
//     interfaces and the New constructor) plus stub implementations of
//     the not-yet-implemented methods.
//
// Canonical JSON contract: every body the writer hands to statestore is
// encoded via catalogmodel.PrettyEncode (which is canonical-key-sorted).
// encoding/json defaults are forbidden for any persisted body.
package catalogstore
