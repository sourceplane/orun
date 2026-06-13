// Package work holds the orun-work plane's system-of-record data model: the
// entity envelope (Initiative/Epic/Task), the task contract, the append-only
// WorkEvent log, principals, relation edges, and the projection reducer that
// folds events into the queryable status model.
//
// # Authority
//
// Authoritative spec source: specs/orun-work/data-model.md. Field tags on every
// struct match the data model byte-for-byte (lowerCamelCase JSON keys, RFC 3339
// / Z timestamps, ULID IDs with ini_/epc_/tsk_/prn_/wev_ prefixes). The JSON
// Schema in schema/work.schema.json is generated from these types via
// `go generate ./internal/work/...` and verified clean by `make
// verify-generated`.
//
// # The two stores
//
// This package owns the *system of record* (the event log) and the *projection*
// derived from it. The Orun Cloud backend Worker persists the same shapes in D1
// and mirrors the mutators here; this package is the conformance oracle for that
// implementation. Sealing into the content-addressed object graph (the *system
// of proof*, SpecSnapshot / WorkLedgerSegment) is W4 and lives elsewhere.
//
// # Invariants enforced here
//
//   - Every WorkEvent carries an actor; an event without one is rejected
//     (invariant 4, WD-2). Automated transitions never wear a human's name.
//   - Each mutator appends exactly one event and updates the projection in the
//     same step (invariant 3, WD-3) — there is one write path.
//   - The projection is derived: dropping it and replaying the event log
//     reproduces it byte-for-byte (invariant 2). Reduce is that replay.
//   - The event-kind set is closed per schema version; an unknown kind is a
//     write-time error, never a forward-compat dumping ground.
//
// # Boundary
//
// work is a pure data package. It MUST NOT import any other internal/* package
// (the same isolation rule internal/catalogmodel obeys); it depends only on the
// standard library and the shared ULID source. Callers inject the wall clock by
// passing the event timestamp, so the package is deterministic under test.
package work

// (schema generation lives in ./schema/gen — see entity.go's go:generate line)
