// Package objgolden owns the cross-language object-model golden vectors: a
// language-neutral fixture set (authoritative framed bytes + the expected
// org-catalog projection) that pins BOTH readers — the Go object-model here and
// the TypeScript mirror in orun-cloud (apps/state-worker/src/object-model.ts +
// catalog-projection.ts) — to the same wire format and field semantics.
//
// The vectors are authored here (Go owns the canonical encoder): real frames are
// harvested from objectstore via a capturing transport, and the expected
// projection is computed from the source nodes structs with the documented
// projection rules. orun-cloud vendors the generated JSON and asserts its reader
// reproduces the same projection in CI, so the Go↔TS seam can no longer drift
// silently (a json-tag rename or a framing change regenerates the bytes and
// fails the TS test until the mirror is updated).
//
// Regenerate after an intentional change:  go test ./internal/objgolden/ -update
package objgolden
