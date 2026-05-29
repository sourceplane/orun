# Task 0003 — Implementer Report

Agent: Implementer
PR: #154 (https://github.com/sourceplane/orun/pull/154)
Branch: `impl/task-0003-m2-statestore-pra`
Base: `main` @ `db342dd` (Task 0002 / M1)

## Summary

- Lands Milestone M2 **PR A**: the frozen `StateStore` interface, the four-error
  taxonomy, the path-helper module, and the local-driver implementation of the
  non-CAS subset (`Root`, `Read`, `Write`, `CreateIfAbsent`, `Delete`).
- `CompareAndSwap` and `List` are present on `*LocalStore` so the interface is
  satisfied, but return `%w: not implemented in PR A` wrapping `ErrInvalid`.
  They land with PR B alongside the 100-goroutine atomicity property tests.
- Path helpers from `state-store.md` §2.1 are complete and exercised; typed
  refs/indexes marshallers (PR C) are deliberately deferred.
- **Contract freeze**: the `StateStore` signatures shipped here match
  `state-store.md` §1 byte-for-byte. Any future divergence requires a
  spec-update proposal.
- No production callers wired. `cmd/orun`, `internal/state`, `internal/runner`,
  `internal/runbundle` are untouched.

## Files Changed

`internal/statestore/` (new package):
- `store.go` — `StateStore` interface, `ObjectMeta`, `ObjectInfo`,
  `WriteOptions`. Doc comments on every exported symbol.
- `errors.go` — `ErrNotFound`, `ErrExists`, `ErrConflict`, `ErrInvalid`
  sentinels.
- `paths.go` — every helper in `state-store.md` §2.1 (`RevisionDir`,
  `PlanPath`, `TriggerPath`, `RevisionDocPath`, `ManifestPath`, `ExecutionDir`,
  `ExecutionDocPath`, `SnapshotPath`, `EventPath`, `LatestRevisionRefPath`,
  `LatestExecutionRefPath`, `TriggerLatestRefPath`, `TriggerScopeRefPath`,
  `NamedRefPath`, `RevisionIndexPath`, `ExecutionIndexPath`) plus exported
  `ValidatePath` / `ValidateComponent` for the alphabet policy
  (`[a-zA-Z0-9._-]`, no `..`, no leading `/`, no Windows separators, no empty
  segments).
- `local.go` — `LocalConfig` (with optional `Clock func() time.Time` defaulting
  to `time.Now`), `LocalStore`, `NewLocalStore`. Atomic `Write` via temp +
  fsync + rename with cross-device copy fallback on `EXDEV`. `CreateIfAbsent`
  via `O_EXCL`. `Delete` no-ops on absent files and refuses non-empty
  directories. Best-effort orphan-tempfile sweep at construction.
- `local_test.go`, `paths_test.go`, `export_test.go` — full unit and
  table-driven coverage on top of `internal/testfx/statefs`.

`Makefile`:
- `test-state-redesign` now runs `internal/statestore` with a `≥ 95 %`
  coverage gate (parses `go test -cover` output and fails the target on
  threshold breach).

`ai/tasks/task-0003.md` — orchestrator scope doc, committed alongside the
deliverable so the report and PR remain self-describing.

`go.mod` / `go.sum` — `go mod tidy` clean; no new direct dependencies.

## Checks Run

| Command | Exit | Notes |
|---|---|---|
| `go build ./...` | 0 | clean |
| `go vet ./...` | 0 | clean |
| `go test -race -count=1 ./internal/statestore/...` | 0 | all unit + atomicity tests green |
| `go test -race -count=1 ./...` | 0 | no regressions across the repo |
| `go test -cover ./internal/statestore/...` | 0 | **95.4 % statement coverage** |
| `make test-state-redesign` | 0 | coverage gate (≥ 95 %) enforced |
| `go list -deps ./internal/statestore \| grep sourceplane/orun/internal \| grep -v statestore` | 1 (no matches) | **zero `internal/*` deps** — leaf package |
| `kiox exec -- orun validate --intent intent.yaml` (in `examples/`) | 0 | `✓ Intent is valid`, `✓ All validation passed` |
| `kiox exec -- orun plan --changed --intent intent.yaml --output plan.json` (in `examples/`) | non-zero | **pre-existing local-only failure** (composition-cache `c41fc08…` `stack.yaml` has no `spec.compositions`) carried from Task 0001/0002 verification — CI is authoritative, this is not a regression |
| `kiox exec -- orun run --plan plan.json --dry-run --runner github-actions` | n/a | not exercised — `plan.json` was not produced because of the local quirk above |

## Coverage

- Package: `internal/statestore`
- Statement coverage: **95.4 %** (`go test -cover ./internal/statestore/...`).
- Measurement covers the entire shipped surface, including the stubbed
  `CompareAndSwap` / `List` (each is a single returning line, so they do not
  drag the threshold below 95 %).
- The Makefile target `test-state-redesign` parses the same `go test -cover`
  output and fails the target if coverage drops below 95.0 %.

## Assumptions

- **Orphan-sweep boundary**: the 1-hour threshold is wired through
  `LocalConfig.Clock` so tests can drive both sides of the boundary
  deterministically (file 1 h + 1 s old → removed; file 59 m old → preserved).
  No repo-wide `clock.Clock` interface was introduced — the task's non-goal
  is honored and the work is left as an M0 follow-up.
- **`Clock` default**: when `LocalConfig.Clock` is nil, the driver falls back
  to `time.Now`; this is the only `time.Now()` call in production code.
- **Sweep failures**: errors during the orphan sweep are non-fatal —
  `NewLocalStore` succeeds even if a stat or remove fails on a stale tempfile,
  matching the task's "best-effort" requirement. Failures are not currently
  surfaced to the caller (no logger is injected at the package layer); a
  structured-error return is a low-risk addition once the broader logging
  story lands.
- **CAS / List stubs**: returning `%w: not implemented in PR A`
  (wrapping `ErrInvalid`) means callers checking `errors.Is(err, ErrInvalid)`
  will treat the methods as path-policy failures. This is acceptable because
  no caller is wired in PR A; PR B will replace the stubs before any consumer
  appears.
- **Cross-device fallback**: the `EXDEV` path performs a copy + fsync into a
  tempdir on the *target* filesystem, then atomically renames into place,
  matching the spec.
- **Scope decision**: shipped PR A alone. PR B (CAS + List + 100-goroutine
  atomicity property suite) and PR C (typed refs/indexes marshallers) are the
  next implementer tasks. The diff in PR A is already non-trivial; bundling
  PR B / PR C would have pushed total LOC well past the ~700 production-LOC
  guideline.

## Spec Proposals

None. The spec was followed faithfully; no `/ai/proposals/task-0003-*.md`
file was needed.

## Remaining Gaps

Within Milestone M2:
- **PR B** — `CompareAndSwap` real implementation, `List` directory walk,
  CAS-conflict test, 100-goroutine `CreateIfAbsent` exclusivity test, rapid
  property test for arbitrary path components.
- **PR C** — typed marshallers for refs and indexes (e.g.
  `WriteLatestRevisionRef`, `ReadTriggerLatestRef`, indexes round-trip).
  The path helpers themselves ship now, so PR C is purely the typed surface.

Non-blocking observations:
- The local `kiox exec -- orun plan --changed` failure is the same
  composition-cache quirk recorded in Task 0001 and Task 0002 verifications.
  CI passes the same invocation; no investigation undertaken in this PR
  per the task's "do not treat as a regression" guidance.

## Next Task Dependencies

Next implementer task should be **Milestone M2 PR B** — `CompareAndSwap` +
`List` + the full atomicity / exclusivity / property test suite from
`test-plan.md` §2 / §3. PR B unblocks M3 (`internal/revision`) writing refs
through CAS.

After PR B, **PR C** (typed refs/indexes marshallers) can land in parallel
with the start of M3 since it's additive on top of the already-frozen
interface.

## PR Number

154
