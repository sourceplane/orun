# Implementation Plan

This is a milestone-based plan, not a rigid wave/task list. Each milestone
states its **goal**, its **dependencies**, its **suggested PR scope**, and
its **"done when" criteria**. Implementer agents have latitude to merge or
split milestones across PRs as long as:

- Every PR stays reviewable (one coherent change, scoped tests).
- Dependencies are respected (don't ship Milestone D before Milestone B).
- Each PR's report cites the milestone IDs it satisfies and the design sections
  it implements.

The Orchestrator's job is to pick the next ready milestone, write the task
prompt, and update `/ai/state.json`'s `active_milestone` field.

---

## Milestone M0 — Foundation

**Goal:** unblock every later milestone without touching production code.

- Add `github.com/oklog/ulid/v2` as a pinned direct dependency (`go mod tidy`).
- Add `internal/testfx/statefs` helper package:
  - `NewWorkspace(t *testing.T) string` — isolated temp `.orun/` root.
  - `AssertJSONFile(t, path string, expected any)` — schema-tolerant diff.
  - `ReadJSON[T any](t, path string) T`.
- Add a coverage gate to `Makefile`: `make test-state-redesign` running the
  packages targeted by later milestones (initially a no-op until those packages
  land).

**Dependencies:** none.

**Suggested PR scope:** one PR for `go.mod` + harness + Makefile.

**Done when:**
- `go build ./...` and `go test ./...` are green.
- `internal/testfx/statefs` has unit tests for all three helpers.
- `internal/testfx/statefs` imports no other `internal/` package.

**Design sections:** `design.md` §13.

---

## Milestone M1 — `internal/triggerctx`

**Goal:** every plan can be associated with a `TriggerOccurrence`. No
persistence yet — model and resolver only.

- `context.go` — `TriggerOccurrence`, `TriggerSource`, `PlanScope` (per
  `data-model.md` §2).
- `ids.go` — monotonic ULID generator with `trg_` prefix, `TriggerKey()`,
  `normalizeScope()`, `shortSHA()`, `worktreeMarker()`.
- `system.go` — `NewSystemManual()`, `NewSystemManualChanged()`,
  `NewSystemReplay()`, `NewSystemAPI()`, `NewSystemMigrated()`.
- `declared.go` — wraps `internal/trigger`. `FromDeclaredTrigger()` plus
  `ResolveProviderEvent()` for `--from-ci`.
- `resolve.go` — top-level `ResolveTriggerContext(opts, intent, git)`
  dispatcher.

**Dependencies:** M0.

**Suggested PR scope:** 1–2 PRs (one for types + ids + system constructors,
one for declared wrap + dispatcher + tests). An implementer may also ship it as
one PR if the diff stays under ~500 LOC.

**Done when:**
- ≥ 90 % statement coverage on `internal/triggerctx`.
- Property test: `TriggerKey` stability + format.
- Table-driven tests for all four `ResolveTriggerContext` branches.
- `--from-ci` no-match returns the typed error described in `design.md` §11.

**Design sections:** `design.md` §5.1, `data-model.md` §2.

---

## Milestone M2 — `internal/statestore` (local driver)

**Goal:** the only path through which new layout files will be written.
Critical foundation — its contract is frozen by the end of this milestone.

- `store.go` — `StateStore` interface, `ObjectMeta`, `ObjectInfo`,
  `WriteOptions`.
- `errors.go` — `ErrNotFound`, `ErrExists`, `ErrConflict`, `ErrInvalid` plus
  wrap/unwrap helpers.
- `paths.go` — every helper listed in `state-store.md` §2.1.
- `local.go` — `LocalStore`, `NewLocalStore(LocalConfig)`, full
  implementation of `Read`, `Write` (atomic temp+rename), `CreateIfAbsent`,
  `CompareAndSwap`, `List`, `Delete`. Orphan-tempfile cleanup on construction.
- `refs.go` — typed ref reader/writer helpers (`WriteLatestRevisionRef`,
  `WriteTriggerRef`, …) wrapping `Write` with deterministic JSON marshal.
- `indexes.go` — typed index writer + `RebuildIndexes()` stub.

**Dependencies:** M0.

**Suggested PR scope:** 2–3 PRs. Natural seams:
- PR A: interface + errors + paths + `LocalStore` for `Read/Write/Create/Delete`.
- PR B: `CompareAndSwap` + `List` + atomicity property tests.
- PR C: refs + indexes typed helpers.

**Done when:**
- ≥ 95 % statement coverage on `internal/statestore`.
- All atomicity tests in `test-plan.md` §2 pass.
- Concurrent `CreateIfAbsent` test passes with N=100 goroutines.
- Public docs on every exported symbol.

**Design sections:** `state-store.md` (entire), `design.md` §7.

---

## Milestone M3 — `internal/revision`

**Goal:** every plan can be persisted as a `PlanRevision` with refs and
indexes updated.

- `model.go` — `PlanRevision`, `RevSummary`.
- `keys.go` — `RevisionKey(trig, planHash)`, regex validator, collision
  suffix logic.
- `writer.go` — `WriteRevision(ctx, store, trig, planBytes, planHash)`
  executing the ordered writes from `design.md` §5.1 / writer-order list.
  Compatibility writes gated on `stateCompatibilityWrites` (default true).
- `manifest.go` — `WriteManifest`, `UpdateLatestExecutionSummary`.
- `resolver.go` — `ResolveRevision(ctx, store, arg)` per
  `compatibility-and-migration.md` §3.
- Writes `.orun/version.json` on first creation.

**Dependencies:** M1, M2.

**Suggested PR scope:** 1–2 PRs (model + keys + writer in one; manifest +
resolver in another is fine, or all together).

**Done when:**
- ≥ 90 % statement coverage on `internal/revision`.
- Property test: revision key uniqueness and collision-suffix correctness.
- Resolver test matrix covers all 7 resolution branches.
- Compat-writes flag tests both true and false paths.

**Design sections:** `design.md` §5, §6, `data-model.md` §3–§4 + §6–§7.

---

## Milestone M4 — `internal/executionstate` + runner bridge

**Goal:** every execution is filed under its revision and the runner's
existing state mirrors into the new layout via the bridge.

- `model.go` — `ExecutionRun`, `RunnerProfile`, `ExecSummary`.
- `writer.go` — `NextExecutionKey`, `SanitizeExecID`, `CreateExecution`,
  `UpdateSnapshot`, `MarkTerminal`. Emits `execution-created` event.
- `bridge.go` — `Bridge{Store, LegacyRoot, MirrorMode}`,
  `MirrorRunnerOutput(ctx, execKey, revKey, legacyExecID)`. Hardlink with copy
  fallback. Failures emit `bridge-mirror-failed` event and return nil.
- `resolver.go` — `ResolveExecution(ctx, store, arg, revHint)`. Legacy
  fallback to `.orun/executions/` scan.

**Dependencies:** M2, M3.

**Suggested PR scope:** 1–2 PRs. The bridge is naturally separable from the
plain writer; ship as two if the diff is large.

**Done when:**
- Property test: `NextExecutionKey` monotonicity under N concurrent
  `CreateExecution` calls (N=100).
- Bridge unit tests cover hardlink success and cross-device → copy fallback
  (use a temp dir on a single FS plus a forced-`EXDEV` injection).
- Resolver legacy-fallback test against a synthesized old `.orun/executions/`.

**Design sections:** `design.md` §5.1 (ExecState row), `data-model.md` §5,
§9.

---

## Milestone M5 — CLI rewire

**Goal:** users see the new layout from the command line. Compatibility holds.

Slice freely across PRs by command. Recommended boundaries:

- M5.a `orun plan` — always resolve trigger, always write revision-first
  layout, preserve `-o`, write compat aliases, emit new summary block.
- M5.b `orun run` — resolution chain, bridge wiring, `--revision` flag.
- M5.c `orun status` + `orun logs` + `orun describe` + `orun get plans` —
  resolver swap with legacy fallback. New flags and aliases.
- M5.d `orun state migrate` (hidden) per `compatibility-and-migration.md` §5.

**Dependencies:** M3, M4.

**Suggested PR scope:** 3–4 PRs (one per slice above is the easy default).

**Done when:**
- Every preserved workflow in `compatibility-and-migration.md` §1 has a
  command-level integration test.
- `orun plan && orun run && orun status && orun logs` against a fresh
  workspace shows the §Acceptance Demo output.
- `orun state migrate --dry-run` is idempotent (two consecutive runs produce
  identical output).
- Existing tests in `cmd/orun/*_test.go` are green.

**Design sections:** `cli-surface.md` (entire).

---

## Milestone M6 — End-to-end + property gates

**Goal:** lock in the correctness properties from `design.md` §9 as
regression tests.

- `cmd/orun/state_e2e_test.go` — full `plan → run → status → logs → describe
  revision latest` walk against a temp workspace.
- Property-based tests collected under `internal/revision/keys_property_test.go`
  and `internal/executionstate/writer_property_test.go`.
- `internal/statestore/local_property_test.go` for atomicity.
- Coverage gate in `Makefile` fails on regressions in the four state packages.

**Dependencies:** M5.

**Suggested PR scope:** 1 PR.

**Done when:**
- All property tests run in `go test -race ./...`.
- `make test-state-redesign` is wired and green.
- `go test ./... -race` is green on a clean checkout.

**Design sections:** `design.md` §9, `test-plan.md` (entire).

---

## Cross-cutting requirements (apply to every milestone)

- All new exported types/functions carry doc comments.
- No `panic()` in production paths.
- No `time.Now()` directly — use a `clock.Clock` interface (Phase 1 wires the
  default `clock.RealClock` everywhere; tests inject fakes). If a `clock`
  helper does not yet exist, M0 may add one in a separate sub-PR rather than
  inlining `time.Now` and refactoring later.
- No log lines containing secrets, tokens, or user emails.
- New JSON output uses stable key order and trailing newline.
- Errors flow through Go's `errors.Is`/`errors.As` — string sniffing is banned.

---

## Out of scope across all milestones

- Replacing `internal/runner` state-writing code path.
- R2 / S3 / Cloud `StateStore` driver.
- Supabase / DO coordination.
- Distributed locking.
- TUI surface changes.
- Deleting legacy state.
