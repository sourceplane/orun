// Package workbridge pins the producer side of the orun-work delivery bridge
// (orun-work milestone W2): it projects an internal/affected.Result into the
// AffectedSet wire DTO the work plane's auto-linker consumes.
//
// # Why this exists
//
// Work tasks and their hot state live in Orun Cloud (Postgres); the change
// engine (internal/affected) and the component catalog (internal/objcatalog)
// live here, in Go. The auto-linker (orun-cloud `@saas/db/work` autolink) needs
// the affected component set for a PR's diff to decide which tasks to link and
// transition. This package is the seam that crosses: it converts the engine's
// Result into the exact JSON the cloud consumes.
//
// # Parity contract
//
// AffectedSet.Components is, by construction, identical to `orun catalog
// affected`'s reported blast radius (Result.Affected = DirectlyChanged ∪
// Dependents). The cloud auto-linker therefore matches the engine for the same
// diff without a second implementation of the closure — the W2 "blast radius
// matches `orun catalog affected`" requirement holds because both read the same
// field. Dependents is carried alongside for reviewer/owner attribution.
//
// This package depends only on internal/affected; it does not import
// internal/work (the work model is import-isolated and lives cloud-side as the
// system of record).
package workbridge
