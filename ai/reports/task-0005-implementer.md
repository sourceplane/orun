# Task 0005 — Implementer Report (M2 PR-C)

**PR Number:** #156
**Branch:** `impl/task-0005-m2-statestore-prc`
**Milestone:** M2 — `internal/statestore`, PR-C (typed refs + indexes)
**Spec:** `specs/orun-state-redesign/` (data-model.md §6/§7, state-store.md §1/§2.1/§3.3/§6)

## Summary

- Added `internal/statestore/refs.go` covering all four ref shapes from `data-model.md` §6 (`LatestRevisionRef`, `LatestExecutionRef`, `TriggerRef`, `NamedRef`) with typed `Read* / Write* / CAS*` helpers over the frozen `StateStore` primitives. Deterministic JSON (2-space indent, no HTML escaping, trailing newline). CAS helpers take a caller-supplied `prev ObjectMeta` and never re-read internally — loser-retries live in the caller per `state-store.md` §6.
- Added `internal/statestore/indexes.go` with typed `RevisionIndexEntry` / `ExecutionIndexEntry` and `WriteRevisionIndex` / `WriteExecutionIndex` writers (both go through `CreateIfAbsent` so duplicate writes surface `ErrExists`). `RebuildIndexes(ctx, store)` is a stub that returns `fmt.Errorf("%w: RebuildIndexes is deferred to M3+", ErrInvalid)` so M3 can wire against the final symbol now.
- Tests: `refs_test.go` and `indexes_test.go` cover round-trip equality, `ErrNotFound` on missing, `ErrExists` on duplicate index write, `ErrConflict` on stale CAS, **exactly-one-winner CAS race** on `LatestRevisionRef` (16 goroutines), invalid-component → `ErrInvalid` on every entry point, decoder rejects unknown fields, and JSON byte-stability via `internal/testfx/statefs.AssertJSONFile`. A source-guard test scans `refs.go`/`indexes.go` and fails if either file contains a literal `"refs/"` or `"indexes/"` path string.
- Coverage on `internal/statestore`: **96.1 %** (was 95.4 %; gate ≥ 95 %, stretch target met).
- Leaf-clean confirmed: `go list -deps ./internal/statestore | grep "/orun/internal/"` returns only the package itself.

## Files Changed

**Production:**
- `internal/statestore/refs.go` — new (286 LOC)
- `internal/statestore/indexes.go` — new (105 LOC)

**Tests:**
- `internal/statestore/refs_test.go` — new (~430 LOC)
- `internal/statestore/indexes_test.go` — new (~190 LOC)

**No-op / untouched:** `cmd/orun/*`, `internal/state/*`, `internal/runner/*`, `internal/runbundle/*`, all spec docs, `internal/statestore/local.go`, `paths.go`, `errors.go`, `store.go`, the existing rapid suite from PR-B.

## Checks Run

| Command | Exit | Notes |
|---|---|---|
| `go build ./...` | 0 | clean |
| `go vet ./...` | 0 | clean |
| `go test -race -count=1 ./internal/statestore/...` | 0 | ok 14.4 s |
| `make test-state-redesign` | 0 | coverage gate prints `measured: 96.1%` |
| `go list -deps ./internal/statestore \| grep "/orun/internal/"` | — | only `github.com/sourceplane/orun/internal/statestore` itself; leaf-clean |
| `git diff origin/main...HEAD -- cmd/orun internal/state internal/runner internal/runbundle` | empty | no production-caller wiring |
| `kiox exec -- orun validate` (in `examples/`) | 0 | `✓ Intent is valid` |
| `kiox exec -- orun plan --changed --intent intent.yaml --output /tmp/plan.json` | non-zero | reproduces the documented composition-cache resolution quirk (Constraint #8 — local env, CI authoritative) |

## Assumptions (durable)

1. **CAS shape — caller supplies `prev ObjectMeta`, helper never re-reads.** The CAS helpers take a `prev ObjectMeta` from the caller's prior `Read` and forward `prev.Revision` directly to `StateStore.CompareAndSwap`. This matches the M3 writer-order in `design.md` §5.1 (`WriteRevision` → `WriteRevisionIndex` → `CASLatestRevisionRef`) and keeps loser-retries at the caller per `state-store.md` §6.
2. **TriggerRef — single shape, scope picked at the call site.** Rather than two parallel helper sets for `latest.json` and `<scope>.json`, both routes go through one `TriggerRefScope{Name, Latest, Scope}` value. The `Latest=true` form ignores `Scope`; the `Latest=false` form requires a non-empty `Scope`. This keeps the helper count manageable while still exposing both file locations.
3. **JSON canonicalization — `encoding/json` with `SetIndent("", "  ")`, `SetEscapeHTML(false)`, trailing newline (the `Encoder.Encode`-emitted `\n`).** All ref/index struct types use only named fields, so the standard encoder emits keys in declaration order — the output is byte-stable across Go versions without a custom canonical encoder.
4. **`marshalCanonicalJSON` panics on encode failure.** The typed values we marshal cannot fail to encode (no channels, funcs, cyclic structures), so an encode error would be a programmer mistake (e.g. someone added an unsupported field type to a ref struct). Surfacing it as a panic keeps the writer signatures simple and avoids a sentinel branch that would otherwise be unreachable.
5. **Index entries are immutable once written.** `WriteRevisionIndex` / `WriteExecutionIndex` use `CreateIfAbsent`; duplicate writes return `ErrExists`. M3 will dedupe before write. A true rebuild path is the dedicated `RebuildIndexes()` helper (stubbed this PR).

## Spec Proposals

None. The data model in `data-model.md` §6/§7 was sufficient for every helper; no path was missing from `paths.go`; no new error sentinel was needed.

## Remaining Gaps

- **`RebuildIndexes()` is a stub.** Returns `ErrInvalid` with a deferred-to-M3+ message. Real implementation lands once revisions actually populate.
- **CAS loser-retries live in callers.** This is by design (per spec); M3's `WriteRevision` will own the retry loop.
- **No production-caller wiring.** `cmd/orun`, `internal/state`, `internal/runner`, `internal/runbundle` remain untouched per the explicit non-goals; M3 is the consumer that will exercise refs/indexes in production code.
- **Local `orun plan --changed` quirk** (Constraint #8) reproduced; CI is authoritative.

## Next Task Dependencies

**M3 — `internal/revision`** is unblocked. The PR scope is per `implementation-plan.md` Milestone M3 and consumes:

- `WriteRevision` ordered writes: revision body → `WriteRevisionIndex` → `CASLatestRevisionRef` (matches the design's writer-order and the CAS shape shipped here).
- M4 (`internal/executionstate`) will consume `WriteExecutionIndex` and `CASLatestExecutionRef` similarly.
- `RebuildIndexes()` is reserved as the symbol M3+ will fill in.

## Verification Handoff

The Verifier should:

1. Code-path inspect each ref/index helper against `data-model.md` §6/§7 byte-for-byte and `state-store.md` §1/§2.1/§3.3.
2. Confirm zero string-concatenation for paths in `refs.go`/`indexes.go` (the `TestRefs_NoStringConcatenationInPaths` test enforces this; verifier should run it and inspect the source).
3. Confirm CAS helpers do not re-read inside the helper.
4. Run all local quality gates (build, vet, race, cover, `make test-state-redesign`, `kiox -- orun validate / plan --changed / run --dry-run`). The plan-changed step is expected to hit the documented composition-cache quirk locally; CI is authoritative.
5. Inspect both required CI runs (`CI / Orun Plan` and `orun remote-state conformance / Harness dry-run guard`) at log level.
6. Confirm leaf-clean and no production-caller wiring.
7. On PASS: squash-merge PR #156, fast-forward main, delete branch.
