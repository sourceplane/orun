// Package catalogmodel holds every persisted Phase 2 (component-catalog) data
// type, plus the canonical-JSON encoder, sanitizers, ID/key construction
// helpers, and the embedded JSON Schema artifact for `component.yaml`.
//
// # Authority
//
// Authoritative spec source: specs/archive/orun-component-catalog/data-model.md and
// specs/archive/orun-component-catalog/identity-and-keys.md. Field tags on every
// struct in this package match data-model.md byte-for-byte (lowerCamelCase
// JSON keys, RFC 3339 / Z timestamps, ULID IDs with `src_` / `cat_` / `cmp_`
// prefixes).
//
// # Determinism contract
//
// All hashed inputs MUST go through CanonicalEncode (sorted keys, no
// whitespace). All persisted human-readable artifacts go through
// PrettyEncode (sorted keys, 2-space indent, trailing newline elided). No
// caller anywhere in the codebase may pass a hash input through bare
// encoding/json — the lack of guaranteed map-key ordering for nested values
// breaks `catalogHash` / `manifestHash` / `dirtyHash` reproducibility (the
// property under test in test-plan.md T-IDK-1 / T-IDK-3).
//
// # Boundary
//
// catalogmodel is a pure data package. It MUST NOT import any other
// `internal/*` package — verified in CI by `go list -deps`. The resolver
// (Milestone C2) and writer (C3) live in sibling packages and depend on
// catalogmodel; catalogmodel never depends on them.
package catalogmodel

// (schema generation lives in ./schema/gen — see component_yaml.go)
