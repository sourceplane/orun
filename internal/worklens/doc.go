// Package worklens is the pure data model of the orun work plane (v2):
// the two append-only logs — coordination (what people intend) and
// observation (what the world did) — and the fold that derives every read
// surface from them. Lifecycle is a derived query, never a stored column
// (specs/orun-work/design.md WP-3); this package is the conformance oracle
// the orun-cloud TypeScript fold replays fixture-for-fixture.
//
// The package is import-isolated like internal/catalogmodel: it may not
// import any other internal/* package. The authoritative schema reference
// is specs/orun-work/data-model.md.
package worklens
