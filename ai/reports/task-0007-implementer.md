# Task 0007 — Implementer Report

## Status
Implemented on branch `impl/task-0007-m3-revision-pra`. Local quality gates green.

## Summary
Opened `internal/revision` package shipping the M3 PR-A surface:

- `internal/revision/model.go` — `PlanRevision`, `RevSummary`, `StateStoreVersion`
  with JSON tag layout matching `data-model.md` §1 + §3 byte-for-byte.
- `internal/revision/keys.go` — `PlanShortHash`, `RevisionKey`,
  `ValidateRevisionKey`, `ResolveCollision`. The collision resolver enforces the
  `^rev-[a-z0-9-]+-p[a-f0-9]{8}(-x\d+)?$` regex and walks `-x1` … `-x99` via
  `statestore.CreateIfAbsent` until it wins a slot; cap exhaustion surfaces
  `ErrConflict`. The reservation body is a tiny `{"reserved":true}` placeholder
  the writer overwrites in step 6 with the real `RevisionIndexEntry`.
- `internal/revision/writer.go` — `Config`, `WriteRevision`,
  `EnsureStateStoreVersion`, plus the `// TODO(m5)` legacy-mirror stub. The
  writer drives the seven-step ordered write list described in `cli-surface.md`
  §1.2 and `design.md` §5.1.
- `internal/revision/version.go` — small canonical-JSON helper +
  `stateStoreVersionPath()` (`"version.json"`); kept inside the revision
  package per the constraint not to grow `internal/statestore` for
  revision-specific paths.
- Tests at `keys_test.go`, `writer_test.go`, `coverage_test.go`.
- `Makefile` `test-state-redesign`: added a ≥ 90 % coverage gate on
  `./internal/revision/...`.

## Step-Order Deviation From `cli-surface.md` §1.2 (claim-first)
`cli-surface.md` §1.2 lists the indexes update as the **last** step. The
implementer chose **claim-first** ordering instead — the index slot is
reserved (`statestore.CreateIfAbsent`) **before** any body file is written;
the slot is then **overwritten with the real `RevisionIndexEntry`** as step
6 (the original "step 7 → indexes" position).

Reasoning:

- `statestore.Write` is last-write-wins on a path
  (`state-store.md` §3 — "atomic, replace-on-conflict"). Two concurrent
  writers landing the same `(TriggerKey, planHash)` would therefore
  silently clobber each other's revision body if we wrote bodies first.
- `CreateIfAbsent` is exclusive (state-store.md §3, exercised by Task
  0004's 100-goroutine atomicity property test). Routing collision
  resolution through it guarantees each writer obtains a **distinct**
  revision key — the second writer rolls forward to `-x1`, the third to
  `-x2`, etc., per `data-model.md` §3.1.
- Refs land **after** the body files, preserving the
  state-store.md §6 invariant that a crash mid-write leaves a complete
  revision body without a ref pointing at it (recoverable by a later
  resolver scan in M3 PR-B).

The verifier should confirm the deviation is documented and that the
correctness invariants of design.md §9 (trigger totality, revision
uniqueness, ref eventual freshness, atomicity) still hold.

## Coverage
`make test-state-redesign` measures **93.3 %** on
`./internal/revision/...` (gate ≥ 90 %). Per-symbol breakdown:

```
PlanShortHash             100.0 %
scopePart                  88.9 %
RevisionKey                90.0 %
ValidateRevisionKey       100.0 %
ResolveCollision           93.3 %
marshalCanonicalJSON       85.7 %  (panic arm unreachable for typed values)
stateStoreVersionPath     100.0 %
resolveDefaults           100.0 %
WithCompatibilityWrites   100.0 %
validateTrigger           100.0 %
summaryFromScope          100.0 %
WriteRevision              84.2 %  (uncovered: late-stage error paths
                                    after step 6 — exercised indirectly
                                    via injected stores)
updateLatestRevisionRef   100.0 %
updateTriggerRefs          85.7 %
EnsureStateStoreVersion   100.0 %
writeCompatibilityMirror  100.0 %
TOTAL                      93.3 %
```

Other state-redesign packages remain green:
`internal/testfx/statefs` ok, `internal/triggerctx` ok,
`internal/statestore` 96.1 % (gate ≥ 95 %).

## Quality Gates Run
- `go build ./...` — green
- `go vet ./...` — green (no issues in `internal/revision`)
- `go test -race -count=1 ./internal/revision/... ./internal/statestore/... ./internal/triggerctx/...` — green
- `make test-state-redesign` — green (statestore 96.1 %, revision 93.3 %)
- `grep '"refs/\|"indexes/' internal/revision/*.go` — empty (all paths
  routed through `statestore` helpers)
- `go list -deps ./internal/revision/...` confirms package depends only on
  `internal/statestore`, `internal/triggerctx` (transitively pulling
  `internal/model`, `internal/trigger` via triggerctx) plus stdlib +
  `oklog/ulid/v2`. Leaf-clean.

## Out of Scope (per Task 0007 PR boundary)
- Manifest writes / `WriteManifest` / `UpdateLatestExecutionSummary` — PR-B (Task 0008)
- `ResolveRevision` seven-branch resolver — PR-B
- Legacy `.orun/plans/<checksum>.json` mirror body — M5 (the seam is wired
  via `Config.CompatibilityWrites` + `writeCompatibilityMirror` stub)
- Production-caller wiring in `cmd/orun`, `internal/state`,
  `internal/runner`, `internal/runbundle` — M5

## Files Changed
- `internal/revision/model.go` (new)
- `internal/revision/keys.go` (new)
- `internal/revision/writer.go` (new)
- `internal/revision/version.go` (new)
- `internal/revision/keys_test.go` (new)
- `internal/revision/writer_test.go` (new)
- `internal/revision/coverage_test.go` (new)
- `Makefile` — added `internal/revision/...` coverage gate to `test-state-redesign`

## Open Items For Verifier
- Confirm the claim-first deviation from `cli-surface.md` §1.2 step-7 order
  is acceptable, or request a spec-amendment proposal.
- Confirm the `// TODO(m5)` compatibility-mirror stub is positioned where
  M5 expects (single seam at `writeCompatibilityMirror`).
- Confirm `EnsureStateStoreVersion` writing to logical path
  `"version.json"` (i.e. `.orun/version.json` on disk) matches
  `data-model.md` §1 — the helper lives in the revision package because
  M2 PR-C deliberately did not add a path helper for it; if the reviewer
  prefers a `statestore.StateStoreVersionPath()` helper that's a small
  follow-up.

## PR Number
#157
