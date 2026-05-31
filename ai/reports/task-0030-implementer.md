# Milestone C4 PR-1 — internal/catalogstore: paths, errors, Writer (sources + catalog snapshots)

Lands the foundational layer of `internal/catalogstore` per `specs/orun-component-catalog/catalog-store.md`.
All catalog persistence MUST flow through this package — no raw FS, no raw path concatenation outside `paths.go`.

Refs: `ai/tasks/task-0030.md`
Branch: `task-0030-catalogstore-c4-pr1`
Spec: `specs/orun-component-catalog/catalog-store.md`
Plan: `specs/orun-component-catalog/implementation-plan.md` (C4 PR-1, lines 149–178)

## Surface delivered (PR-1)

### `paths.go` — every helper from spec §2
Sources:
- `SourceDir`, `SourceDocPath`

Catalogs:
- `CatalogDir`, `CatalogDocPath`
- `CatalogGraphPath(srcKey, catKey, kind)` — `kind` validated against `CatalogGraphKinds()` (`dependencies`, `systems`, `apis`, `resources`, `owners`)
- `CatalogRevisionDir`, `CatalogRevisionPlanPath`, `CatalogExecutionDir`

Components:
- `ComponentDir`, `ComponentManifestPath`

Refs:
- `SourceRefPath` (latest/current/main), `SourceBranchRefPath`, `SourcePRRefPath`
- `CatalogRefPath`, `CatalogBranchRefPath`, `CatalogPRRefPath`

Local indexes (per (srcKey, catKey)):
- `ComponentLocalIndexPath`, `OwnerLocalIndexPath`, `SystemLocalIndexPath`, `DomainLocalIndexPath`, `TypeLocalIndexPath`

Global indexes:
- `ComponentGlobalIndexPath` — sanitises 3-segment componentKey via `catalogmodel.SanitizeComponentKey`
- `CatalogGlobalIndexPath`, `SourceGlobalIndexPath`

History events:
- `ComponentHistoryEventPath(srcKey, catKey, name, n, kind)` → `…/history/components/<name>/events/<009d>-<sanitized-kind>.json`

Validators (return `(string, error)`; none panic):
- `ValidateSegment` — workhorse alphabet check (`[a-z0-9._-]+`, no `..`/`/`/`\`/whitespace, ≤128 chars)
- `ValidateSourceKey`, `ValidateCatalogKey` — wrap `catalogmodel.Validate*Key` and re-type errors under `ErrInvalidPathInput`
- `ValidateRefName` (`latest`/`current`/`main`), `ValidateGraphKind`, `ValidateEventKind` (allows dots, mirrors event-kind sanitiser policy)

### `errors.go` — typed sentinels
- `ErrSourceMismatch` — wraps `statestore.ErrExists` when `WriteSourceSnapshot` finds a divergent body
- `ErrCatalogMismatch` — same, for catalog doc / graph divergence
- `ErrManifestMismatch` — same, for component manifest divergence
- `ErrInputsInconsistent` — pre-flight cross-link violation (src↔cat, manifest↔(src,cat))
- `ErrInvalidPathInput` — common parent for every `Validate*` failure
- `ErrNotImplemented` — placeholder return for PR-2/PR-3 methods

Mismatch sentinels intentionally **double-wrap** so `errors.Is(err, ErrSourceMismatch)` AND `errors.Is(err, statestore.ErrExists)` both succeed — callers who already key off the statestore sentinel keep working.

### `store.go` — public interfaces + constructor
- `Store` (composes `Writer + Resolver`), `Writer`, `Resolver`
- `New(state statestore.StateStore) Store`
- Compile-time interface assertions on `*store` so PR-2/PR-3 signature drift is a build break.
- `Resolver` methods + `WriteRefs`/`WriteGlobalIndexes`/`AppendComponentEvent` return `ErrNotImplemented` (scheduled for PR-2/PR-3).

### `writer.go` — step A & step B body writes

**Step A — `WriteSourceSnapshot`:**
- `CreateIfAbsent(SourceDocPath, PrettyEncode(src))`
- `ErrExists` + byte-identical body → success (idempotent)
- `ErrExists` + divergent body → `ErrSourceMismatch` wrapping `statestore.ErrExists`

**Step B — `WriteCatalogSnapshot`:**
- Pre-flight `preflightCatalogInputs`: validates `srcKey`/`catKey` shape, asserts `cat.SourceSnapshotKey == src.SourceSnapshotKey`, asserts every `manifest.Source.{SourceSnapshotKey, CatalogSnapshotKey}` matches the (src, cat) tuple. Failures emit `ErrInputsInconsistent` BEFORE any write.
- B.1 manifests (caller-supplied order; idempotent CreateIfAbsent + body reconcile → `ErrManifestMismatch`)
- B.2 graphs in fixed kind order (`dependencies` → `systems` → `apis` → `resources` → `owners`); body divergence → `ErrCatalogMismatch`
- B.3 catalog.json (CreateIfAbsent; divergence → `ErrCatalogMismatch`)
- B.4 local indexes via plain `Write` (overwrite-OK; rebuildable)

`createOrReconcile` is the shared idempotent-or-mismatch helper used across B.1–B.3.

## What's NOT in this PR (PR-2 / PR-3)

- `WriteRefs`, `WriteGlobalIndexes`, `AppendComponentEvent` — stubbed `ErrNotImplemented`
- `Resolver` (current source / catalog resolution + fallback chain `current → latest → main`)
- `ResolveComponentLatest` (global-index seek)

Naming for future types (`RefUpdate`, `GlobalIndexUpdate`, `RefSelector`, `ComponentLatest`) is reserved in `store.go` so PR-2 doesn't churn the public API.

## Tests

`internal/catalogstore/paths_test.go` (~290 LoC):
- Happy path for every helper (28 cases via table)
- `ValidateSegment` rejection matrix (empty, `.`, `..`, `/`, `\`, space, tab, uppercase, colon, oversize, `x..y`)
- `ValidateRefName` allow/deny
- `ValidateGraphKind` allow/deny across `CatalogGraphKinds()`
- `ValidateEventKind` allow (with dots) / deny
- `ValidateSourceKey`/`ValidateCatalogKey` rejection
- `ComponentGlobalIndexPath` sanitisation + rejection
- Panic safety (defer-recover wrapping every helper)
- Per-helper error-path drive (`store_test.go::TestPathHelpers_AllErrorPathsExercised`)

`internal/catalogstore/writer_test.go` (~580 LoC):
- In-file spy `statestore.StateStore` recording call order, with `preExisting` and `failCreate` injection.
- Step A: happy path, idempotent re-write, `ErrSourceMismatch` + `errors.Is(err, statestore.ErrExists)`, invalid-key abort, non-`ErrExists` create error doesn't classify as `ErrSourceMismatch`.
- Step B: full write trace asserting B.1 → B.2 → B.3 → B.4 ordering with fixed graph order; out-of-order graph input still yields canonical trace order; manifest mismatch aborts subsequent writes; pre-flight `ErrInputsInconsistent` for `cat.SourceSnapshotKey ≠ src.SourceSnapshotKey`, `manifest.Source.SourceSnapshotKey` mismatch, `manifest.Source.CatalogSnapshotKey` mismatch; idempotent re-run of full B; catalog doc mismatch; graph body mismatch; all five local-index axes (components/owners/systems/domains/types) written; invalid local-index key returns `ErrInvalidPathInput`; explicit `srcKey`/`catKey` validation in pre-flight.

`internal/catalogstore/store_test.go` (~140 LoC):
- Locks PR-2/PR-3 stubs to `ErrNotImplemented` (defence against accidental nil-return swaps).
- Batch error-path drive across every path helper.

## Gates run

| Gate | Result |
| --- | --- |
| `go vet ./...` | ✅ clean |
| `go build ./...` | ✅ clean |
| `go test ./...` | ✅ all packages pass |
| `go test ./internal/catalogstore/... -race` | ✅ pass |
| `go test ./internal/catalogstore/... -cover` | **✅ 90.7% of statements** (≥90 floor) |
| `make verify-generated` | ✅ generated artifacts up-to-date |
| Raw FS guard (`grep '"os"\|"io/ioutil"\|"path/filepath"' internal/catalogstore/`) | ✅ no raw FS imports |

The remaining unhit lines are the PR-2/PR-3 `ErrNotImplemented` returns (locked by `TestStubsReturnErrNotImplemented`) plus a handful of `if err != nil` shortcuts inside `pathJoin`/encoder error paths that would require injecting an encode failure for no real signal.

## Key design decisions

- **Mismatch sentinels wrap `statestore.ErrExists`** so callers keep their existing `errors.Is(err, statestore.ErrExists)` checks while gaining the ability to discriminate body conflicts via the typed sentinel.
- **Pre-flight cross-link validation** in `WriteCatalogSnapshot` returns `ErrInputsInconsistent` before any write so a caller that passes a mismatched (src, cat, manifest) tuple cannot leave partial state on disk.
- **Path helpers return `(string, error)`** rather than panicking. The public surface is large and any caller-controlled string should fail typed.
- **`pathSegmentMaxLen = 128`** caps caller-supplied segments well below NAME_MAX while staying above the sanitised-branch max (40).
- **`PrettyEncode`** for body writes (matches existing fixture pattern); `CanonicalEncode` reserved for hash inputs only (used by upstream layers).
- **Graph write order is fixed in code** (`CatalogGraphKinds()` drives the loop), not derived from the input map — guarantees a deterministic trace regardless of which order the caller populated `CatalogGraphs`.

## Files

```
internal/catalogstore/doc.go         (1.7 KB)
internal/catalogstore/errors.go      (2.3 KB)
internal/catalogstore/paths.go       (17.4 KB)
internal/catalogstore/paths_test.go  (9.6 KB)
internal/catalogstore/store.go       (6.9 KB)
internal/catalogstore/store_test.go  (5.2 KB)
internal/catalogstore/writer.go      (9.2 KB)
internal/catalogstore/writer_test.go (~17.5 KB)
```
