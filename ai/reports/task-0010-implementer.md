# Task 0010 — Implementer Report

- **Task**: M3 PR-B `internal/revision` (manifest + resolver + compat-mirror + JobCount)
- **Branch**: `impl/task-0010-m3-revision-prb`
- **PR**: https://github.com/sourceplane/orun/pull/158
- **Spec sources**: `specs/orun-state-redesign/{data-model.md,compatibility-and-migration.md,state-store.md,test-plan.md}`

## Summary

Lands the revision-writer surface that the M5 CLI rewire (`orun plan` / `orun run`) will consume:

1. `manifest.go` — `WriteManifest` produces the denormalized
   `revisions/<key>/manifest.json` (data-model.md §4); `UpdateLatestExecutionSummary`
   performs Read-modify-CAS with an idempotent short-circuit and a retry budget so
   the executionstate writer can update `summary.latestExecutionKey` /
   `summary.latestExecutionStatus` without owning a separate ref.
2. `resolver.go` — seven-branch `ResolveRevision` per compatibility-and-migration.md §3.
   Branches 2 (plan file) and 5 (legacy hex hash) synthesize revisions in-memory;
   the `--persist-revision` flag is reserved for a Phase 2 task.
3. `legacy.go` — `plans/<hex>.json` and `plans/latest.json` path helpers plus a
   `sha256:`-tolerant checksum normalizer. The bare-hex form is the canonical
   on-disk filename (data-model.md §10 / compat §2).
4. `writer.go` — `writeCompatibilityMirror` writes the plan aliases byte-identically
   to canonical `plan.json` when `Config.CompatibilityWrites` is true. `Config.JobCount`
   is threaded into `RevSummary.JobCount`.
5. `errors.go` — `ErrAmbiguousArg`, `ErrComponentRunUnchanged` resolver sentinels
   (`errors.Is`-routable; resolver-shaped, with no analogue at the byte-level
   persistence layer, so they live in the revision package, not `statestore`).

## Option A: JobCount

Selected and implemented Option A from the task spec — `JobCount` is planner-supplied
via `Config.JobCount`, persisted exactly (0 when unknown). The writer does **not**
parse `plan.json` to derive a count.

Rationale:

- Keeps the writer free of plan-document structural assumptions; the planner already
  knows the job count and is the single source of truth.
- Zero is a faithful "unknown" marker rather than a parser-derived false zero. Future
  callers that genuinely have zero jobs (a no-op plan) and callers that have an
  unknown count both encode as 0 — but the planner contract makes the distinction
  explicit at the call site.
- Avoids coupling the revision package to a specific plan schema version, which is
  important for the migration path (legacy `.orun/plans/*.json` may not match the
  current Plan kind).

## Compatibility-mirror posture

The mirror writes through the same `StateStore` driver as canonical writes so atomic
semantics, the path-component alphabet, and future remote-driver semantics all carry.
The mirror is gated on `Config.CompatibilityWrites`, which defaults to `true` in
Phase 1 (the default value is preserved through `WithCompatibilityWrites(false)` via
the internal `compatibilityWritesSet` flag).

## Resolver branch ordering

Branch ordering matches compat §3 exactly. Two ordering nuances worth recording:

- **Branch 3 (revision-key) on `ErrNotFound` falls through, not bubbles up.** A user
  can have an arg that matches the revision-key regex, doesn't exist on disk, but
  is also a known named ref or legacy hash. The resolver continues to subsequent
  branches and only returns `ErrAmbiguousArg` if branches 4-7 also fail.
- **Branch 5 length floor.** `isHexLower` accepts arbitrary-length hex but the
  resolver requires `len(arg) >= planShortHashLen` (8) before entering branch 5,
  matching `normalizeLegacyChecksum`'s minimum. Shorter hex falls through to
  branch 7. Tested.

## Verification

| Gate | Result |
| --- | --- |
| `go build ./...` | ok |
| `go vet ./...` | ok |
| `make test-state-redesign` | ok (statestore 96.1%, revision 90.4%) |
| `./internal/revision/...` coverage gate (≥ 90%) | **90.4%** |
| `go test ./...` | ok |
| `go mod tidy` | clean (no go.mod/go.sum changes) |

## Files changed

- A `internal/revision/errors.go`
- A `internal/revision/legacy.go`
- A `internal/revision/manifest.go`
- A `internal/revision/manifest_test.go`
- A `internal/revision/resolver.go`
- A `internal/revision/resolver_test.go`
- A `internal/revision/writer_compat_test.go`
- A `internal/revision/coverage_extra_test.go`
- M `internal/revision/model.go` (added `ManifestKind`)
- M `internal/revision/writer.go` (compat-mirror body, `Config.JobCount`, `summaryFromScope` plumbing)

## Open follow-ups

None blocking. Phase-2 follow-ups not in scope:

- `--persist-revision` flag for branches 2 and 5.
- Migration command (`orun state migrate`) per compat §5.
- Reader fallback for `executionstate.ResolveExecution` per compat §4.

## Awaiting

Verifier review on PR #158.
