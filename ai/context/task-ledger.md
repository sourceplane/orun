# Task Ledger — orun-state-redesign lineage

Task numbering restarted at 0001 with the 2026-05-30 pivot from
`.kiro/specs/orun-tui-cockpit/` (paused at PR #146 merged) to
`specs/orun-state-redesign/` (Phase 1 local-only trigger-first revision-first
local state model).

Prior TUI cockpit tasks (0139–0147 with sub-tasks) are preserved in git
history; reports/prompts are no longer present in the working tree.

---

## Task 0001

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0001.md`
|- Status: **implemented and merged via PR #152** (2026-05-29T19:11:11Z, main commit `4ea1980e`)
|- Milestone: M0 — Foundation
|- Objective: pin `github.com/oklog/ulid/v2`, scaffold `internal/testfx/statefs`
   helper package with `NewWorkspace`, `AssertJSONFile`, and `ReadJSON[T]`,
   and add `make test-state-redesign` (initially a no-op).
|- Scope boundary: `go.mod`/`go.sum`, `internal/testfx/statefs/`, `Makefile`.
   No production-code or CLI-surface changes. Implementer may split into two
   PRs (dependency pin vs. harness) if cleaner; both must land before M1.
|- Acceptance: `go build ./...` and `go test ./...` green; unit tests for all
   three helpers; `internal/testfx/statefs` imports no other `internal/` package;
   `make test-state-redesign` target exists and runs.
|- Expected outcome: M1 (`internal/triggerctx`) and M2 (`internal/statestore`)
   are unblocked.
|- Implementer outcome (2026-05-29): PR **#152** opened on branch
   `impl/task-0001-m0-foundation` @ `628c212`. Status `OPEN`, `MERGEABLE`,
   `mergeStateStatus=CLEAN`, all required CI checks `SUCCESS`. Implementer
   report: `ai/reports/task-0001-implementer.md`. Awaiting verifier.

## Task 0001 (Verifier pass)

|- Agent: Verifier
|- Prompt: `ai/tasks/task-0001-verifier.md`
|- Status: **verified PASS and merged** (2026-05-29T19:11:11Z)
|- Verifying: PR #152 (`impl/task-0001-m0-foundation` @ `628c212`)
|- Implementation: PR **#152** squash-merged → main commit
   `4ea1980e8822d21908b09ab70316fe3953bb75ce`. Branch deleted.
|- PR CI: `CI / Orun Plan` SUCCESS (run `26656333958`, real plan output
   `0 components × 3 envs → 0 jobs`, plan artifact uploaded);
   `orun remote-state conformance / Harness dry-run guard` SUCCESS
   (run `26656333939`, all `[guard] PASS:` assertions ran). Matrix legs
   SKIPPED legitimately (empty matrix at M0).
|- Local checks: `go build ./...`, `go vet ./...`, `go test ./...`,
   `go test -count=1 ./internal/testfx/statefs/...`, `make test-state-redesign`,
   `go list -m github.com/oklog/ulid/v2` (→ `v2.1.1`),
   `go list -deps ./internal/testfx/statefs` (leaf-clean) all exit 0.
   `kiox -- orun validate` passes; `kiox -- orun plan --changed` fails locally
   on a pre-existing composition-cache env quirk that reproduces on main
   `d2ab48e` and is exercised successfully in CI.
|- Reports: implementer at `ai/reports/task-0001-implementer.md`; verifier at
   `ai/reports/task-0001-verifier.md`.
|- Objective: validate Task 0001 against the Verifier Standard in
   `agents/orchestrator.md` and the M0 "done when" criteria in
   `specs/orun-state-redesign/implementation-plan.md`; enforce non-goals;
   resolve the `flyingmutant/rapid` → `pgregory.net/rapid` spec drift.
|- Scope boundary: verification only (plus the verifier report committed to
   the PR branch as a verifier-only artifact); no production-code edits.
|- Durable outcome on main: `github.com/oklog/ulid/v2 v2.1.1` direct require,
   `internal/testfx/statefs` test harness with happy + fakeT-driven failure-
   path coverage, `make test-state-redesign` target, full
   `specs/orun-state-redesign/` spec pack, `agents/orchestrator.md` updates,
   rebuilt `ai/` lineage, TUI-era `task-014*.md` / `report task-014*.md`
   deleted. `flyingmutant/rapid` drift formally deferred to Task 0002 with
   rationale (forcing function: first real `rapid` import will fail to compile
   under the wrong path).

## Task 0002

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0002.md`
|- Status: **implemented and merged via PR #153** (2026-05-29T19:51:33Z, main commit `db342dd`)
|- Milestone: M1 — `internal/triggerctx`
|- Objective: build the trigger context model — `TriggerOccurrence`,
   `TriggerSource`, `PlanScope`, `trg_`-prefixed ULID generator, system trigger
   constructors, `FromDeclaredTrigger`, `ResolveProviderEvent`,
   `ResolveTriggerContext` — that every downstream milestone (statestore,
   revision, executionstate, CLI) consumes.
|- Scope boundary: `internal/triggerctx/` only (plus the M0→M1 hand-off chores:
   delete `internal/testfx/statefs/tools.go`, file the `pgregory.net/rapid`
   spec proposal). No production callers wired; no CLI surface changes.
|- Acceptance: ≥ 90 % coverage; rapid property test on `TriggerKey` stability
   + format; JSON stability via `internal/testfx/statefs.AssertJSONFile`;
   leaf-clean imports.
|- Implementer report: `ai/reports/task-0002.md` (and the implementer-side
   sub-report ladder under that name).
|- Verifier report: `ai/reports/task-0002-verifier.md`. Spec drift resolved
   via `ai/proposals/task-0002-spec-update.md`.
|- Durable outcome on main: `internal/triggerctx` exposing the documented
   surface, JSON-stable, copy-safe; M2 (statestore) and M3 (revision)
   unblocked.

## Task 0003

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0003.md`
|- Status: **implemented; PR #154 OPEN, MERGEABLE, CLEAN, all required CI checks SUCCESS** — awaiting verifier
|- Milestone: M2 — `internal/statestore` PR A (frozen interface + non-CAS local driver)
|- Branch: `impl/task-0003-m2-statestore-pra` @ `4afcd34`
|- Objective: introduce `internal/statestore` with the frozen `StateStore`
   interface, the four-error taxonomy (`ErrNotFound`, `ErrExists`,
   `ErrConflict`, `ErrInvalid`), the `paths.go` helper module covering
   `state-store.md` §2.1, and a local-driver implementation of `Root`,
   `Read`, `Write` (atomic), `CreateIfAbsent`, `Delete`. CAS and List are
   stubbed (`%w: not implemented in PR A` wrapping `ErrInvalid`) so the
   interface compiles; real implementations land in PR B.
|- Scope boundary: `internal/statestore/` (new package), `Makefile`
   (`test-state-redesign` coverage gate at ≥ 95 %), `go.mod` / `go.sum`
   (`go mod tidy`). No production callers wired (`cmd/orun`,
   `internal/state`, `internal/runner`, `internal/runbundle` untouched).
   No typed refs/indexes marshallers (PR C deferred).
|- Acceptance: `go build`, `go vet`, `go test -race`,
   `go test -cover ./internal/statestore/...` ≥ 95 %,
   `make test-state-redesign`, `kiox -- orun validate` all green; CI
   required checks SUCCESS; PR opened with real number; report committed
   to the PR branch.
|- Implementer report: `ai/reports/task-0003-implementer.md` — reported
   coverage 95.4 % on `internal/statestore`; PR Number: 154.
|- Expected outcome: contract freeze on `StateStore`; M3 (`internal/revision`)
   begins coding against this interface as soon as PR B (CAS + List) lands.

## Task 0003 (Verifier pass)

|- Agent: Verifier
|- Prompt: `ai/tasks/task-0003-verifier.md`
|- Status: **verified PASS and merged** (2026-05-29, PR #154 → main commit `9b0a39c`)
|- Verifying: PR **#154** (`impl/task-0003-m2-statestore-pra` @ `4afcd34`, then
   `3acb7ed` after the verifier-report commit)
|- Implementation: PR **#154** squash-merged → main commit `9b0a39c`. Branch
   `impl/task-0003-m2-statestore-pra` deleted.
|- PR CI: `CI / Orun Plan` SUCCESS (run **26665146437**, real `orun plan
   --from-ci github --event-file "$GITHUB_EVENT_PATH" --intent
   examples/intent.yaml --artifact github --github-output` invocation,
   `0 components × 3 envs → 0 jobs` (legitimate empty-matrix M2-PR-A shape),
   plan checksum `74af9580810d`, plan artifact uploaded). `orun remote-state
   conformance / Harness dry-run guard` SUCCESS (run **26665146435**, 30+
   `[guard] PASS:` assertions covering bash syntax, foundation@dev.smoke /
   api@dev.smoke command counts, duplicate-claim helper PASS+FAIL,
   status helper PASS+FAIL across 5 status states, env-export checks,
   signal-safe cleanup trap, jq/orun/repo-linkage preflight). Matrix legs
   SKIPPED legitimately. Post verifier-commit re-run: same checks SUCCESS,
   mergeStateStatus CLEAN.
|- Local checks: `go build ./...`, `go vet ./...`,
   `go test -race -count=1 ./...`,
   `go test -cover ./internal/statestore/...` (**95.4 %**),
   `make test-state-redesign`, `go list -deps ./internal/statestore`
   (leaf-clean), `git diff origin/main...HEAD -- cmd/orun internal/state
   internal/runner internal/runbundle` (empty), `kiox -- orun validate`
   all exit 0. `kiox -- orun plan --changed` reproduces the pre-existing
   composition-cache failure carried from Task 0001+; CI is authoritative
   and passes the same invocation.
|- Reports: implementer at `ai/reports/task-0003-implementer.md`; verifier at
   `ai/reports/task-0003-verifier.md`.
|- Objective: validate Task 0003 against the Verifier Standard in
   `agents/orchestrator.md` and the M2 PR-A "done when" criteria in
   `specs/orun-state-redesign/implementation-plan.md`. Confirm interface
   freeze (byte-for-byte vs `state-store.md` §1), path-helper coverage of
   §2.1, local-driver atomicity (§3) via code-path inspection, error
   taxonomy (§4) via `errors.Is` / `errors.As`, leaf-clean imports, and
   no production-caller wiring.
|- Scope boundary: verification only (verifier report committed to the PR
   branch as a verifier-only artifact); no production-code edits, no spec
   edits, no PR-B/C work.
|- Durable outcome on main: `internal/statestore` package shipping the
   frozen `StateStore` interface, four-error sentinel taxonomy
   (`ErrNotFound`, `ErrExists`, `ErrConflict`, `ErrInvalid` — all driver
   errors wrap a sentinel via `fmt.Errorf("%w: …", ErrX, …)` so
   `errors.Is`/`errors.As` are the only sanctioned detection path), full
   `paths.go` coverage of `state-store.md` §2.1 with centralized
   `[a-zA-Z0-9._-]` alphabet policy, and a `LocalStore` non-CAS subset
   (`Root`, `Read`, atomic `Write` via temp+fsync+rename with EXDEV
   cross-device fallback into the destination FS, `O_EXCL`-based
   `CreateIfAbsent`, no-op-on-absent / refuses-directory `Delete`,
   1-hour `LocalConfig.Clock`-driven orphan tempfile sweep at
   construction). `CompareAndSwap` and `List` stubbed returning
   `ErrInvalid`-wrapped "not implemented in PR A" — interface compiles,
   downstream MUST NOT call them until PR B. No production callers
   wired; package is leaf-clean (zero `internal/*` deps). Coverage gate
   measured 95.4 %.
|- Verifier non-blocking findings: empty-directory `Delete` returns
   `ErrInvalid` (state-store.md §3.4 only mandates non-empty); the
   `LocalConfig.Clock` field is an additive test-injection hook beyond
   §5. Both documented in the verifier report; neither blocked merge.
|- Expected next: Task 0004 = M2 PR B (CAS + List + 100-goroutine
   atomicity / exclusivity property suite per `test-plan.md` §2 / §3,
   plus `pgregory.net/rapid` round-trip on path-alphabet inputs).

## Task 0004

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0004.md`
|- Status: **scoped and ready to begin** (2026-05-30)
|- Milestone: M2 — `internal/statestore` (PR B)
|- Objective: replace the PR-A stubs for `*LocalStore.CompareAndSwap` and
   `*LocalStore.List` with real Phase-1 implementations per `state-store.md`
   §3.3 and §3.4, and add the atomicity / exclusivity / CAS / `pgregory.net/rapid`
   property suite per `test-plan.md` §2 and §3.
|- Scope boundary: `internal/statestore/local.go` (real `CompareAndSwap` +
   `List`) plus new test files in `internal/statestore/`. No `refs.go` /
   `indexes.go` (PR C). No production-caller wiring (`cmd/orun`,
   `internal/state`, `internal/runner`, `internal/runbundle` untouched).
   No spec changes. No new exported symbols beyond the frozen interface.
|- Acceptance: `make test-state-redesign` green with `internal/statestore`
   coverage ≥ 95 % (target ≥ 96 %); `go test -race -count=1
   ./internal/statestore/...` green; PR-A stub error strings ("not
   implemented in PR A") gone from `local.go`; package stays leaf-clean.
|- Expected outcome: M2 contract closed on the local driver; PR C
   (typed refs/indexes marshallers) and M3 (`internal/revision`) unblocked.

## Task 0004

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0004.md`
|- Status: **implemented; PR #155 OPEN, MERGEABLE, CLEAN, all required CI checks SUCCESS** — awaiting verifier
|- Milestone: M2 — `internal/statestore` (PR B)
|- Branch: `impl/task-0004-m2-statestore-prb` @ `4875025`
|- Objective: replace the PR-A stubs for `*LocalStore.CompareAndSwap` and
   `*LocalStore.List` with real Phase-1 implementations per `state-store.md`
   §3.3 and §3.4, and add the atomicity / exclusivity / CAS / `pgregory.net/rapid`
   property suite per `test-plan.md` §2 and §3.
|- Scope boundary: `internal/statestore/local.go` (real `CompareAndSwap` +
   `List`) plus new test files in `internal/statestore/`. No `refs.go` /
   `indexes.go` (PR C). No production-caller wiring (`cmd/orun`,
   `internal/state`, `internal/runner`, `internal/runbundle` untouched).
   No spec changes. No new exported symbols beyond the frozen interface.
|- Acceptance: `make test-state-redesign` green with `internal/statestore`
   coverage ≥ 95 % (target ≥ 96 %); `go test -race -count=1
   ./internal/statestore/...` green; PR-A stub error strings ("not
   implemented in PR A") gone from `local.go`; package stays leaf-clean.
|- Implementer report: `ai/reports/task-0004-implementer.md` — reported
   coverage 95.4 % on `internal/statestore`; PR Number: 155.
|- Expected outcome: M2 contract closed on the local driver; PR C
   (typed refs/indexes marshallers) and M3 (`internal/revision`) unblocked.

## Task 0004 (Verifier pass)

|- Agent: Verifier
|- Prompt: `ai/tasks/task-0004-verifier.md`
|- Status: **verified PASS and merged** (2026-05-30, PR #155 → main commit `0fa2111`)
|- Verifying: PR **#155** (`impl/task-0004-m2-statestore-prb` @ `4875025`)
|- Implementation: PR **#155** squash-merged → main commit `0fa2111`. Branch
   `impl/task-0004-m2-statestore-prb` deleted.
|- PR CI: `CI / Orun Plan` SUCCESS (run **26670829548**, real
   `orun plan --from-ci github …` invocation, `0 components × 3 envs → 0 jobs`
   legitimate empty-matrix M2-PR-B shape). `orun remote-state conformance /
   Harness dry-run guard` SUCCESS (run **26670829550**, full `[guard] PASS:`
   battery covering bash syntax, command-count thresholds, duplicate-claim
   helper PASS + FAIL, status helper PASS + FAIL across status states,
   exported env asserts for `ORUN_EXEC_ID` / `ORUN_REMOTE_STATE`).
|- Local checks: `go build ./...`, `go vet ./...`, `go test -race -count=1
   ./internal/statestore/...` (`14.069s`), `make test-state-redesign`
   (coverage **95.4 %**, gate ≥ 95 %), `kiox -- orun validate / plan
   --changed / run --dry-run` all green. The persistent composition-cache
   quirk DID NOT reproduce on this verifier run.
|- Reports: implementer at `ai/reports/task-0004-implementer.md`; verifier at
   `ai/reports/task-0004-verifier.md`.
|- Objective: validate Task 0004 against the Verifier Standard in
   `agents/orchestrator.md` and the M2 PR-B "done when" criteria in
   `specs/orun-state-redesign/implementation-plan.md`. Confirm
   `*LocalStore.CompareAndSwap` and `*LocalStore.List` semantics vs
   `state-store.md` §3.3 / §3.4, the four required atomicity / exclusivity
   / CAS / `pgregory.net/rapid` tests, leaf-clean imports, no
   production-caller wiring, and ≥ 95 % coverage.
|- Scope boundary: verification only (verifier report + previously missing
   implementer report committed to the PR branch as verifier-only
   housekeeping); no production-code edits, no spec edits, no PR-C work.
|- Durable outcome on main: real `*LocalStore.CompareAndSwap` (Read →
   revision compare → Write; `ErrConflict` on mismatch; per-path
   `sync.Mutex` narrows the in-process race, additive per §6 "best-effort
   on local") and real `*LocalStore.List` (`WalkDir` over translated
   prefix; symlinks skipped via `d.Type()&fs.ModeSymlink`; `.orun-tmp-*`
   filtered; logical paths via `filepath.ToSlash`; non-existent prefix →
   empty slice; `ErrInvalid` on alphabet/escape via `paths.go`). 17 new
   tests across CAS happy/error paths, List edges (symlinks, FIFO unix-only,
   tempfile filter, escape rejection, ctx cancel), 100-goroutine atomicity
   + exclusivity, CAS exactly-one-wins, `pgregory.net/rapid` path-alphabet
   round-trip with stable lowercase-hex sha256 `Revision`. Package stays
   leaf-clean; no production-caller wiring.
|- Verifier non-blocking findings: per-path `sync.Mutex` is in-process only
   (irrelevant to local Phase 1; remote driver Phase 2 supersedes);
   empty-directory `Delete` returns `ErrInvalid` (carried from Task 0003,
   §3.4 only mandates non-empty); coverage 95.4 % satisfies gate but sits
   below 96 % stretch target — PR-C should lift it.
|- Expected next: Task 0005 = M2 PR-C (typed refs.go + indexes.go
   marshallers + `RebuildIndexes()` stub) closes Milestone M2; M3
   (`internal/revision`) starts after PR-C verification.

## Task 0005

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0005.md`
|- Status: **scoped and ready to begin** (2026-05-30)
|- Milestone: M2 — `internal/statestore` (PR C — closes M2)
|- Suggested branch: `impl/task-0005-m2-statestore-prc`
|- Objective: add typed ref reader/writer + CAS helpers (`refs.go`) covering
   `data-model.md` §6.1–§6.4 (`LatestRevisionRef`, `LatestExecutionRef`,
   `TriggerRef`, `NamedRef`) and typed index writers (`indexes.go`) covering
   §7.1 / §7.2 (`RevisionIndexEntry`, `ExecutionIndexEntry`) plus a
   `RebuildIndexes()` stub returning `%w: rebuild deferred to M3+` wrapping
   `ErrInvalid`. Helpers wrap the frozen `StateStore` primitives with
   deterministic JSON marshal/unmarshal — no new persistence semantics, no
   new error sentinels, no new path helpers.
|- Scope boundary: `internal/statestore/refs.go`, `internal/statestore/indexes.go`,
   `internal/statestore/refs_test.go`, `internal/statestore/indexes_test.go`. NO
   production-caller wiring (`cmd/orun`, `internal/state`, `internal/runner`,
   `internal/runbundle` untouched). NO spec edits. NO new exported symbols
   beyond the typed surface for refs/indexes.
|- Constraints: zero string concatenation for paths (everything via `paths.go`);
   deterministic JSON (`MarshalIndent("", "  ")` + trailing `\n` + no HTML
   escaping); CAS helpers take `*ObjectMeta` from prior read (no re-read inside
   helper); index writers use `CreateIfAbsent` (re-write → `ErrExists`);
   package stays leaf-clean.
|- Acceptance: `make test-state-redesign` green with `internal/statestore`
   coverage ≥ 96 % (gate stays ≥ 95 %); `go test -race -count=1
   ./internal/statestore/...` green; round-trip / `ErrNotFound` /
   `ErrExists` / CAS-conflict / JSON byte-stability tests via
   `internal/testfx/statefs.AssertJSONFile`; package leaf-clean; PR opened
   with real number; implementer report committed to the PR branch.
|- Expected outcome: Milestone M2 closed (M2 "Done when" satisfied —
   coverage, atomicity tests, concurrent `CreateIfAbsent` N=100, public
   docs on every exported symbol). M3 (`internal/revision`) unblocked.

## Task 0005 (continued — implemented and merged)

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0005.md`
|- Status: **implemented and merged via PR #156** (2026-05-30, main commit `cd8b3e8`)
|- Branch: `impl/task-0005-m2-statestore-prc` @ `a8a580a`
|- Implementer report: `ai/reports/task-0005-implementer.md` — coverage 96.1 %
   on `internal/statestore` (gate ≥ 95 %, M2 stretch ≥ 96 % met); leaf-clean
   confirmed; no production-caller wiring; no spec proposals.
|- Files: `internal/statestore/refs.go` (286 LOC), `internal/statestore/indexes.go`
   (105 LOC), `refs_test.go` (~430 LOC), `indexes_test.go` (~190 LOC). Includes
   a source-guard test that fails if `refs.go` / `indexes.go` ever contain a
   literal `"refs/"` / `"indexes/"` path string (paths must route through
   `paths.go` only).
|- Durable assumptions: CAS helpers take `prev ObjectMeta` from caller — no
   re-read inside helper (loser-retries at the caller per `state-store.md` §6).
   `TriggerRef` is a single `TriggerRefScope{Name, Latest, Scope}` value;
   `Latest=true` ignores `Scope`, `Latest=false` requires non-empty `Scope`.
   `marshalCanonicalJSON` uses `encoding/json` with `SetIndent("", "  ")` +
   `SetEscapeHTML(false)`; trailing `\n` from `Encoder.Encode`.

## Task 0006 (Verifier pass for Task 0005 / PR #156)

|- Agent: Verifier
|- Prompt: `ai/tasks/task-0006-verifier.md`
|- Status: **verified PASS and merged** (2026-05-30, PR #156 → main commit `cd8b3e8`)
|- Verifying: PR **#156** (`impl/task-0005-m2-statestore-prc` @ `a8a580a`)
|- Implementation: PR **#156** squash-merged → main commit `cd8b3e8`. Branch
   `impl/task-0005-m2-statestore-prc` deleted.
|- PR CI: `CI / Orun Plan` SUCCESS (run **26671612378**). `Harness dry-run guard`
   SUCCESS (run **26671612360**). Other rollup checks SKIPPED legitimately
   (empty matrix `0 components × 3 envs → 0 jobs` for a code-only PR).
|- Verifier report: `ai/reports/task-0006-verifier.md`. Result: PASS. Diff
   audited as exactly the four files implied by Task 0005 scope plus the
   implementer report. `cmd/orun internal/state internal/runner internal/runbundle`
   diff EMPTY. PR-A / PR-B surface (`paths.go`, `errors.go`, `store.go`,
   `local.go`, rapid suite) byte-identical to PR-A / PR-B.
|- Spec conformance verified byte-for-byte vs `data-model.md` §6.1–§6.4 / §7.1–§7.2,
   `state-store.md` §1 / §2.1 / §3.3 / §6. Path-string source-guard test passes.
   `RebuildIndexes()` returns `fmt.Errorf("%w: RebuildIndexes is deferred to M3+", ErrInvalid)`.
|- Durable outcome on main: M2 closed. Typed `LatestRevisionRef` /
   `LatestExecutionRef` / `TriggerRef` / `NamedRef` reader/writer/CAS helpers
   live in `internal/statestore/refs.go`. Typed `RevisionIndexEntry` /
   `ExecutionIndexEntry` writers via `CreateIfAbsent` live in
   `internal/statestore/indexes.go`. `RebuildIndexes()` stub in place for M3+.
|- Expected next: Task 0007 = M3 PR-A (Implementer) opens `internal/revision`.

## Task 0007

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0007.md`
|- Status: **delivered via PR #157** by the corrective Task 0008 chore
   (2026-05-30). Branch `impl/task-0007-m3-revision-pra` head SHA `500218c`;
   commits `96621ed` (Task 0007 tree) + `500218c` (PR-number backfill into
   reports). Implementer report `ai/reports/task-0007-implementer.md` now
   carries `PR Number: #157`. PR is OPEN, MERGEABLE, mergeStateStatus CLEAN,
   both required CI checks SUCCESS at log level. Awaiting Task 0009 verifier.
|- Milestone: M3 — `internal/revision` (PR-A: model + keys + writer skeleton)
|- Branch: `impl/task-0007-m3-revision-pra` (local only)
|- Objective: open `internal/revision` package shipping the typed model
   (`PlanRevision`, `RevSummary`, `StateStoreVersion`), the revision-key
   generator (`RevisionKey`, `ValidateRevisionKey`, `ResolveCollision`,
   `PlanShortHash`) with collision suffix logic, and the writer skeleton
   (`Config`, `WriteRevision`, `EnsureStateStoreVersion`,
   `writeCompatibilityMirror` stub) executing the seven-step ordered write
   list from `cli-surface.md` §1.2 / `design.md` §5.1.
|- Scope boundary: `internal/revision/{model,keys,writer,version}.go`,
   `internal/revision/{keys,writer,coverage}_test.go`, `Makefile` (revision
   coverage gate ≥ 90 %). NO `manifest.go`, NO `resolver.go`, NO legacy mirror
   body, NO production-caller wiring (deferred to M3 PR-B = Task 0010 and
   M5 CLI rewire).
|- Implementer reported quality gates green: `go build`, `go vet`,
   `go test -race -count=1` on revision/statestore/triggerctx, and
   `make test-state-redesign` (revision pkg coverage **93.3 %**, gate ≥ 90 %;
   statestore stays at 96.1 %). Leaf-clean: revision pkg depends only on
   `internal/statestore` + `internal/triggerctx` (transitive `internal/model`
   via triggerctx) plus stdlib + `oklog/ulid/v2`.
|- Open verifier item: **claim-first ordering deviation from `cli-surface.md`
   §1.2 step-7.** Implementer reserves the index slot via `CreateIfAbsent`
   BEFORE any body file is written, then overwrites the slot with the real
   `RevisionIndexEntry` after refs land (originally listed as the last step).
   Rationale documented in `ai/reports/task-0007-implementer.md` § "Step-Order
   Deviation". Verifier (Task 0009) will accept-and-document or file
   `ai/proposals/task-0007-spec-update.md`.
|- Expected outcome: M3 PR-A delivered via the same PR shipped under Task 0008;
   verified under Task 0009.

## Task 0008

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0008.md`
|- Status: scoped and ready to begin (2026-05-30) — corrective delivery chore
|- Milestone: M3 — `internal/revision` (PR-A delivery)
|- Branch: `impl/task-0007-m3-revision-pra` (REUSED; same PR as Task 0007)
|- Objective: convert the locally-staged Task 0007 tree into a real reviewable
   PR. Commit the staged tree, push the branch, open the PR titled
   `Task 0007: M3 PR-A — internal/revision model + keys + writer skeleton`,
   backfill the real PR number into `ai/reports/task-0007-implementer.md`,
   and file `ai/reports/task-0008-implementer.md`.
|- Scope boundary: NO code changes. If a quality gate fails on the staged tree,
   the smallest targeted fix is allowed (committed as `fix: …` on the same
   branch); the M3 PR-A scope itself stays intact. NO spec edits, NO production-
   caller wiring, NO M3 PR-B work.
|- Acceptance: branch pushed; PR open and MERGEABLE; both implementer reports
   (0007 and 0008) carry `PR Number: #<PR>` (same number — single-PR delivery);
   local quality gates green on the final committed tree; required CI checks
   (`CI / Orun Plan`, `Harness dry-run guard`) at minimum queued/in-progress
   before report close.
|- Expected outcome: M3 PR-A real PR exists; next cycle emits Task 0009 = M3
   PR-A verifier; M3 PR-A merge unblocks Task 0010 = M3 PR-B (manifest writer +
   `ResolveRevision` seven-branch resolver + legacy `.orun/plans/**` mirror
   body behind `Config.CompatibilityWrites`).

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0008.md`
|- Status: **completed via PR #157** (2026-05-30). Commit `96621ed` shipped
   the staged Task 0007 tree; commit `500218c` backfilled `PR Number: #157`
   into both `ai/reports/task-0007-implementer.md` and the new
   `ai/reports/task-0008-implementer.md`. Branch `impl/task-0007-m3-revision-pra`
   pushed; PR opened (title: "Task 0007: M3 PR-A — internal/revision model +
   keys + writer skeleton"); both required CI checks SUCCESS
   (`CI / Orun Plan` run `26672937657`, `Harness dry-run guard`
   run `26672937641`). PR is OPEN+MERGEABLE+CLEAN, awaiting verifier (Task 0009).
|- Milestone: M3 — `internal/revision` (PR-A delivery)
|- Branch: `impl/task-0007-m3-revision-pra` (REUSED; same PR as Task 0007)
|- Objective: convert the locally-staged Task 0007 tree into a real reviewable
   PR. Commit the staged tree, push the branch, open the PR titled
   `Task 0007: M3 PR-A — internal/revision model + keys + writer skeleton`,
   backfill the real PR number into `ai/reports/task-0007-implementer.md`,
   and file `ai/reports/task-0008-implementer.md`.
|- Scope boundary: NO code changes. If a quality gate fails on the staged tree,
   the smallest targeted fix is allowed (committed as `fix: …` on the same
   branch); the M3 PR-A scope itself stays intact. NO spec edits, NO production-
   caller wiring, NO M3 PR-B work.
|- Acceptance: branch pushed; PR open and MERGEABLE; both implementer reports
   (0007 and 0008) carry `PR Number: #<PR>` (same number — single-PR delivery);
   local quality gates green on the final committed tree; required CI checks
   (`CI / Orun Plan`, `Harness dry-run guard`) at minimum queued/in-progress
   before report close.
|- Outcome: M3 PR-A real PR exists at #157 with both required CI green; next
   cycle emits Task 0009 = M3 PR-A verifier. Closed R-005 mitigation per this
   incident; pattern guard remains open for future implementer prompts.

## Task 0009

|- Agent: Verifier
|- Prompt: `ai/tasks/task-0009-verifier.md`
|- Status: **verified PASS and merged via PR #157** (2026-05-30T03:33:16Z, main commit `7f1e53d6`)
|- Verified: PR **#157** (`impl/task-0007-m3-revision-pra` @ `500218c` → `7f1e53d` after squash)
|- Milestone: M3 — `internal/revision` (PR-A verification)
|- Objective: validated Task 0007 against M3 "Done when" criteria; adjudicated the claim-first ordering deviation and the `version.json` helper-location decision; inspected both required CI runs at log level; merged per the Verifier Merge Protocol.
|- Scope boundary: verification only. Verifier committed only `ai/reports/task-0009-verifier.md` to the PR branch. No production-code edits, no spec edits, no proposal filed.
|- Verifier outcome (2026-05-30):
   - Result: **PASS**.
   - Coverage: `internal/revision` 93.3 % (gate ≥ 90 %); `internal/statestore` 96.1 % (gate ≥ 95 %, no regression).
   - Local quality gates: `go build ./...`, `go vet ./...`, `go test -race -count=1` on revision/statestore/triggerctx, `make test-state-redesign`, `go list -deps` leaf-clean audit — all green.
   - CI at log level: `CI / Orun Plan` (run `26672937657`, then `26673333973` on the verifier-side commit) executed real `orun plan --artifact github` against `examples/intent.yaml` with the legitimate `0 components × 3 envs → 0 jobs` empty matrix; plan artifact uploaded. `Harness dry-run guard` (run `26672937641`, then `26673333980`) emitted the full `[guard] PASS:` battery.
   - Diff audit: PR boundaries clean — no overreach into `cmd/orun`, `internal/state`, `internal/runner`, `internal/runbundle`; `internal/triggerctx`, `internal/statestore`, `internal/testfx/statefs` byte-identical to `origin/main`; no PR-B leakage (`manifest.go`, `resolver.go` absent).
   - Claim-first adjudication: ACCEPTED in-place. Rationale: `CreateIfAbsent` is the only exclusive primitive in `state-store.md` §3, claim-first is the only ordering producing distinct revision keys before any body write occurs under concurrent `(TriggerKey, planHash)` duplicates, refs still land after bodies preserving `state-store.md` §6 crash-recovery, and `cli-surface.md` §1.2 reads as a high-level descriptive flow not a normative atomicity proof. **No `ai/proposals/task-0007-spec-update.md` filed.**
   - `version.json` helper-location adjudication: DEFER. `stateStoreVersionPath()` stays in `internal/revision`; if M5 migration tooling needs a statestore-side helper, that PR can lift the constant up.
   - Risk Notes carried forward: `writeCompatibilityMirror` is a no-op stub gated by default-true `Config.CompatibilityWrites` (M5 fills the body); `RevSummary.JobCount` = 0 in PR-A (PR-B threads it via `WriteManifest`).
|- Reports: implementer `ai/reports/task-0007-implementer.md` + `ai/reports/task-0008-implementer.md`; verifier `ai/reports/task-0009-verifier.md`.
|- Durable outcome on `main`: `internal/revision` model (`PlanRevision`, `RevSummary`, `StateStoreVersion`) byte-matching `data-model.md` §3 / §1; revision-key generator + collision-suffix resolver with 100-iteration uniqueness/collision property test; `WriteRevision` seven-step ordered writer (claim-first index reservation → bodies → refs → index finalize → version doc → optional compat-mirror seam) wired against the frozen `internal/statestore` interface; coverage gate ≥ 90 % on `internal/revision`; leaf-clean imports (`statestore` + `triggerctx` only).
|- Next: Task 0010 = M3 PR-B implementer.

## Task 0010

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0010.md`
|- Status: **implemented; PR #158 OPEN, MERGEABLE, CLEAN, both required CI checks SUCCESS at log level** — awaiting Task 0011 verifier (2026-05-30)
|- Milestone: M3 — `internal/revision` (PR-B; closes M3)
|- Branch: `impl/task-0010-m3-revision-prb` @ `ec74af1` (parent `e538b90`)
|- Objective: close out M3 by adding the manifest writer (`WriteManifest`,
   `UpdateLatestExecutionSummary`), the seven-branch `ResolveRevision` resolver
   per `compatibility-and-migration.md` §3, the legacy `.orun/plans/<hex>.json`
   + `latest.json` mirror body promoting the `// TODO(m5)` stub to a real
   conditional write gated by `Config.CompatibilityWrites`, and `JobCount`
   plumbing — without touching production callers.
|- Scope boundary: `internal/revision/{errors,legacy,manifest,resolver}.go`
   (new), `internal/revision/{model,writer}.go` (edits — `ManifestKind`,
   compat-mirror body, `Config.JobCount`, `summaryFromScope`), tests
   `internal/revision/{manifest,resolver,writer_compat,coverage_extra}_test.go`.
   NO `cmd/orun`, NO `internal/state`, NO `internal/runner`, NO
   `internal/runbundle`, NO `internal/statestore` / `internal/triggerctx` /
   `internal/testfx/statefs` edits, NO `internal/executionstate` (M4), NO
   `--persist-revision` flag, NO `orun state migrate`.
|- Acceptance: ≥ 90 % coverage on `internal/revision`; all 7 resolver branches
   tested with explicit `ResolveSource` assertions; compat-writes flag exercises
   both true and false paths; idempotent re-run on same plan succeeds; CAS
   contention + budget-exhaustion + idempotent-short-circuit tested on
   `UpdateLatestExecutionSummary`; `<planHashHex>` strip helper has its own
   unit test; no new sentinels in `internal/statestore`.
|- Implementer report: `ai/reports/task-0010-implementer.md` — coverage
   `internal/revision` **90.4 %** (gate ≥ 90 %); `internal/statestore` **96.1 %**
   (M2 floor preserved). PR Number: **158**.
|- Implementer decisions (to be adjudicated by Task 0011 verifier): Option A
   for JobCount (planner-supplied, `0` means "unknown"); resolver branch 3
   fallthrough on `ErrNotFound`; `.orun/plans/latest.json` written via
   `statestore.Write` (last-write-wins per spec §2). Typed sentinels
   `ErrAmbiguousArg`, `ErrComponentRunUnchanged` in `internal/revision/errors.go`.
|- Expected outcome: M3 closes on Task 0011 PASS + PR #158 merge; M4
   (`internal/executionstate` + runner bridge) becomes the next milestone.

## Task 0011

|- Agent: Verifier
|- Prompt: `ai/tasks/task-0011-verifier.md`
|- Status: scoped and ready to begin (2026-05-30)
|- Verifying: PR **#158** (`impl/task-0010-m3-revision-prb` @ `ec74af1`)
|- Milestone: M3 — `internal/revision` (PR-B verification)
|- Objective: validate Task 0010 against the M3 "Done when" checklist (≥ 90 %
   coverage, all 7 resolver branches with explicit `ResolveSource` assertions,
   compat-writes flag exercises both true and false paths, key
   uniqueness/collision property carried from PR-A still green); adjudicate
   the three implementer decisions (JobCount Option A, branch-3 fallthrough,
   `latest.json` `Write`-not-`CreateIfAbsent`) inline OR via a single
   `ai/proposals/task-0010-spec-update.md`; inspect both required CI runs at
   log level; merge per the Verifier Merge Protocol on PASS.
|- Scope boundary: verification only. May commit `ai/reports/task-0011-verifier.md`
   and optional `ai/proposals/task-0010-spec-update.md` to the PR branch as
   verifier-only artifacts. NO production-code edits beyond a verifier-only
   typo/TODO fix strictly required for mergeability. NO M4 work.
|- Acceptance: PR #158 squash-merged to main on PASS (branch deleted, local
   `main` fast-forwarded, `git status --short` clean); OR left OPEN with
   explicit blockers on FAIL. Required CI both SUCCESS at log level on the
   final head SHA before merge.
|- Expected outcome: M3 closed; next implementer = Task 0012 = M4 PR-A
   (`internal/executionstate` model + writer; bridge optionally separable).

## Task 0012

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0012.md`
|- Status: **implemented and merged via PR #159** (2026-05-30T06:14:21Z, main commit `ed48633`)
|- Milestone: M4 — `internal/executionstate` (PR-A: model + writer + resolver)
|- Branch: `impl/task-0012-m4-executionstate-pra` @ `8a0c409` (parent `2c239d7`)
|- Objective: open M4 by shipping the leaf-clean `internal/executionstate`
   package — `model.go` (`ExecutionRun`/`RunnerProfile`/`ExecSummary` byte-stable
   per data-model §5), `writer.go` (`NextExecutionKey`, `SanitizeExecID`,
   `CreateExecution`, `UpdateSnapshot`, `MarkTerminal`, `finalizeExecution`,
   `updateRevisionSummary`), and `resolver.go` (seven-branch ladder with legacy
   `.orun/executions/` fallback). First real callers of
   `revision.UpdateLatestExecutionSummary` wired through the writer.
|- Scope boundary: `internal/executionstate/{model,writer,resolver,internal,
   *_test}.go` (new), additive helpers in `internal/statestore/paths.go`
   (`ExecutionsDir`, `ExecutionDocPath`, `ExecutionIndex*`, `LegacyExecution*`,
   `EventPath`, `SnapshotPath`), `Makefile` coverage gate. NO `bridge.go` (M4
   PR-B), NO `cmd/orun`, NO `internal/state`, NO `internal/runner`, NO
   `internal/runbundle`, NO `--persist-revision`, NO CLI rewire (M5 owns).
|- Acceptance: ≥90% on `internal/executionstate`; ≥90% on `internal/revision`
   preserved; ≥95% on `internal/statestore` preserved; `NextExecutionKey`
   monotonicity property test under N=100 concurrent `CreateExecution`;
   resolver branch-5 (legacy `.orun/executions/<arg>/execution.json` direct read)
   and branch-6 (legacy newest-mtime scan) both tested via synthesized legacy
   trees; `internal/testfx/statefs.AssertJSONFile` JSON-stability test on
   `ExecutionRun`; `revision.UpdateLatestExecutionSummary` first-caller wired
   through `finalizeExecution` + `MarkTerminal` with CAS retry + idempotent-
   short-circuit covered.
|- Implementer report: `ai/reports/task-0012-implementer.md` — coverage
   `internal/executionstate` **90.0 %** (exact floor), `internal/revision`
   **90.4 %**, `internal/statestore` **95.4 %**. PR Number: **159**.
|- Implementer assumptions (to be adjudicated by Task 0013 verifier):
   (1) M3 callers writing a revision but not a manifest is treated as an
   error — loud `ErrNotFound` surfacing on `UpdateLatestExecutionSummary`
   rather than silent no-op. (2) Legacy executions with no `revisionKey`
   carry `triggerKey="system.migrated"`, `Reason="migration"`,
   `Status="completed"` default per compat §4 projection.
|- Expected outcome: M4 PR-A closes on Task 0013 PASS + PR #159 merge; M4
   PR-B (`bridge.go` + `MirrorRunnerOutput`) becomes Task 0014.

## Task 0013

|- Agent: Verifier
|- Prompt: `ai/tasks/task-0013-verifier.md`
|- Status: **verified PASS and merged via PR #159** (2026-05-30T06:14:21Z, main commit `ed48633`)
|- Verified: PR **#159** (`impl/task-0012-m4-executionstate-pra` @ `8a0c409` →
   `ed48633` after squash)
|- Milestone: M4 — `internal/executionstate` (PR-A verification)
|- Objective: validate Task 0012 against the M4 "Done when" criteria (≥90%
   coverage on `internal/executionstate`, NextExecutionKey monotonicity property
   under N=100 concurrent calls, resolver legacy-fallback against synthesized
   `.orun/executions/`, leaf-clean invariant); adjudicate the two implementer
   assumptions inline OR via `ai/proposals/task-0012-spec-update.md`; inspect
   both required CI runs at log level; merge per the Verifier Merge Protocol.
|- Scope boundary: verification only. Verifier committed only
   `ai/reports/task-0013-verifier.md` to the PR branch. No production-code
   edits, no spec edits, no proposal filed.
|- Verifier outcome (2026-05-30):
   - Result: **PASS**.
   - Coverage: `internal/executionstate` 90.0 % (exact gate ≥90%);
     `internal/revision` 90.4 % (preserved); `internal/statestore` 95.4 %
     (preserved).
   - Local quality gates: `go build ./...`, `go vet ./...`,
     `go test -race -count=1 ./...` (incl. `internal/executionstate` 45.9 s
     no DATA RACE), `make test-state-redesign`,
     `TestNextExecutionKey_MonotonicityUnderConcurrency` (PASS in 30.47 s),
     resolver branch-5/6 legacy-fallback tests (both PASS),
     `go list -deps ./internal/executionstate` (leaf-clean confirmed: zero
     hits on `cmd/orun|internal/state$|internal/runner|internal/runbundle`),
     no production `.orun/` literal strings — all paths via
     `internal/statestore/paths.go`.
   - CI at log level: `CI / Orun Plan` run `26675724704` (job `78626951236`,
     completed 2026-05-30T05:30:17Z); `Harness dry-run guard` run
     `26675724720` (job `78626951221`, completed 2026-05-30T05:29:47Z). Five
     matrix legs SKIPPED legitimately (empty matrix at M4 PR-A — same shape
     as #152/#155–#158).
   - Diff audit: PR boundaries clean — no overreach into `cmd/orun`,
     `internal/state`, `internal/runner`, `internal/runbundle`. No
     `bridge.go` leakage. Public surface audit clean — no accidental
     over-export.
   - R-002 ExecID compat: `SanitizeExecID("gh-{run_id}-{attempt}-{sha}")`
     round-trips identity (alphabet-clean — no projection); compatible with
     `internal/runbundle` ExecIDs without further migration work. PR-B can
     accept GHA-shape `legacyExecID` directly.
   - Manifest-required-for-summary adjudication: ACCEPTED in-place. Conservative-
     on-unknowns posture matches `revision/writer.go` style; carry-forward
     to M5 to ensure `WriteRevision`+`WriteManifest` precede `CreateExecution`
     for the same `revKey`. **No spec proposal filed.**
   - Legacy-execution literal-defaults adjudication: ACCEPTED as documented
     fallback convention. Compat §4 prescribes `triggerType="system"`,
     `triggerName="system.migrated"`, `source.workingTree="unknown"` literally
     but does not normate the M4-projected `triggerKey` /`Reason` /`Status`
     defaults. **No spec proposal filed in PR-A; carry-forward to M5/Phase 2
     migration command landing.**
   - Risk Notes carried forward: (a) coverage at exact 90.0% floor — small
     refactors deleting covered branches could trip the gate; PR-B should
     organically lift it. (b) Race-mode property test ~30s under -race —
     acceptable now; flag for `-short` skip if it grows. (c) `kiox` local
     skip — no top-level `intent.yaml`; CI's `Orun Plan` and `Harness dry-run
     guard` cover the surface authoritatively.
|- Reports: implementer `ai/reports/task-0012-implementer.md`; verifier
   `ai/reports/task-0013-verifier.md`.
|- Durable outcome on `main`: `internal/executionstate` package shipped — typed
   model byte-matching `data-model.md` §5; writer with `NextExecutionKey` /
   `SanitizeExecID` / `CreateExecution` / `UpdateSnapshot` / `MarkTerminal`;
   resolver with seven-branch ladder + legacy fallback; first real callers of
   `revision.UpdateLatestExecutionSummary` wired with CAS + idempotent-short-
   circuit; coverage gate ≥90% on `internal/executionstate` enforced; leaf-
   clean imports preserved; additive `internal/statestore/paths.go` helpers.
|- Next: Task 0014 = M4 PR-B implementer (`bridge.go` + `MirrorRunnerOutput`).

## Task 0014

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0014.md`
|- Status: scoped and ready to begin (2026-05-30)
|- Milestone: M4 — `internal/executionstate` + runner bridge (PR-B; closes M4)
|- Branch (to be created): `impl/task-0014-m4-executionstate-prb` from `main` @ `ed48633`
|- Objective: ship `internal/executionstate/bridge.go` —
   `Bridge{Store, LegacyRoot, MirrorMode}` plus
   `MirrorRunnerOutput(ctx, execKey, revKey, legacyExecID)` with hardlink-with-
   copy-fallback, EXDEV-injection test seam, `bridge-mirror-failed` event
   emission per `data-model.md` §9, and idempotent re-mirror semantics. Wire
   exercised through tests only — no production runner integration in this PR.
|- Scope boundary: `internal/executionstate/bridge.go` (+ tests), `Makefile`
   if gate text changes, additive helpers in `internal/statestore/paths.go`
   only. NO `cmd/orun`, NO `internal/state`, NO `internal/runner`, NO
   `internal/runbundle`, NO production-runner wiring (M5 owns), NO CLI rewire,
   NO migration command (M5.d), NO `JobRun`/`JobAttempt`/`StepAttempt`
   directories (`design.md` §4 reserved-but-empty in Phase 1).
|- Acceptance: `internal/executionstate` ≥90% preserved (PR-B should lift the
   exact-90.0%-floor PR-A landed at); `internal/revision` ≥90% preserved;
   `internal/statestore` ≥95% preserved; hardlink success path test (single-FS
   temp dir); forced-EXDEV cross-device fallback test via injected `linker`
   seam (`os.Link` wrapper); `bridge-mirror-failed` event emission test with
   payload consistent with `data-model.md` §9; idempotent re-mirror test;
   leaf-clean invariant preserved (`go list -deps ./internal/executionstate`
   shows no reach into `cmd/orun`/`internal/state`/`internal/runner`/
   `internal/runbundle`); both required CI checks at minimum queued before
   report close. No new error sentinels — reuse `statestore.ErrInvalid` /
   `ErrNotFound` via `fmt.Errorf("%w: …", …)`.
|- Open spec questions (implementer authorized to choose, verifier adjudicates):
   `MirrorMode` enumeration (`MirrorModeHardlink`/`MirrorModeCopy`/
   `MirrorModeAuto`?) and `bridge-mirror-failed` event payload field set
   (data-model §9 names the event but does not enumerate every field).
|- Expected outcome: M4 closes when Task 0015 (PR-B verifier) PASSes + PR
   merges; Task 0016 = M5.a (`orun plan` rewire) implementer becomes the
   next emission.

## Task 0015

- Agent: Verifier
- Prompt: `ai/tasks/task-0015-verifier.md`
- Status: **verified PASS** on 2026-05-30
- Implementation: PR **#160** on branch `impl/task-0014-m4-executionstate-prb`,
  merged into `main` as squash commit `d51e828d0e55e6d5b6b369ad72575bcfe244a7e8`
  ("Task 0014: M4 PR-B — internal/executionstate bridge + EXDEV fallback (#160)")
  at 2026-05-30T07:18:02Z.
- PR CI on final head SHA (after verifier-side commit `f94730e`):
  - `CI / Orun Plan` — run `26677835038`, SUCCESS at log level (`orun plan`
    invoked against `examples/intent.yaml`; legitimate empty-matrix
    `0 components × 3 envs → 0 jobs`; plan artifact uploaded).
  - `orun remote-state conformance / Harness dry-run guard` — run `26677835039`,
    SUCCESS at log level (full `[guard] PASS:` battery: bash syntax,
    foundation@dev.smoke ≥2 commands, api@dev.smoke ≥1 command, required
    command/assertion markers, duplicate-claim helper PASS+FAIL,
    status helper PASS+FAIL, exported env asserts).
- Reports:
  - Implementer: `ai/reports/task-0014-implementer.md` (146 lines)
  - Verifier: `ai/reports/task-0015-verifier.md` (~140 lines)
- Objective: validated Task 0014 / PR #160 against the Verifier Standard +
  M4 "Done when" criteria; confirmed `bridge.go` correctness vs `design.md`
  §5.1 / §M4, EXDEV fallback property test, additive `paths.go` helpers,
  no overreach into `cmd/orun` / `internal/state` / `internal/runner` /
  `internal/runbundle`, byte-identical PR-A surface.
- Scope boundary: verification only (verifier report committed to PR branch
  before merge; no production-code edits).
- Coverage delta: `internal/executionstate` held at 90.0% (exact floor);
  `internal/statestore` lifted to 95.7% (was 95.4%); `internal/revision`
  unchanged at 90.4%. All gates green: `go build/vet`, `go test -race
  ./...`, `make test-state-redesign`. Leaf-clean confirmed.
- Adjudications (both accepted with Risk Notes; no spec proposal filed):
  1. **`MirrorMode` trinary surface** (`Auto`/`Hardlink`/`Copy`). Auto is
     zero value matching §M4 verbatim; Hardlink supports drift detection
     and the EXDEV emission test; Copy pre-positions remote drivers.
     Trinary surface is additive over the spec, not contradictory.
     Renaming is non-breaking source-level.
  2. **`bridge-mirror-failed` payload schema**
     `{executionKey, revisionKey, legacyExecId, artifact, stage, mode, error}`
     with `stage ∈ {read-source, read-dest, translate-dest, mkdir-dest,
     remove-dest, link, copy}`. data-model.md §9 left it open; PR-B fixed
     it in code. Schema is well-formed and additive-friendly. Risk Note:
     pin in §9 during M5.b runner wiring before any second consumer
     (metrics, `orun status`) lands.
- Durable outcome: M4 milestone fully closed. `internal/executionstate`
  feature-complete for Phase 1: model + writer (PR-A) + resolver-with-
  legacy-fallback (PR-A) + bridge with hardlink-with-copy-fallback (PR-B).
  Bridge ships with frozen `Bridge{Store, LegacyRoot, MirrorMode, Now}`
  surface, portable EXDEV detection, structured `bridge-mirror-failed`
  event emission, idempotent short-circuit on byte-equal destinations,
  and seq=2 reservation for `execution-created`. Production-caller wiring
  (runner → MirrorRunnerOutput, CLI rewire) is M5; resolver legacy-
  fallback carries convergence burden until M5.b.
- Expected next emission: **Task 0016 = M5.a (`orun plan` rewire)
  implementer.** Branch base: `main` @ `d51e828`.

## Task 0016

|- Agent: Implementer + Verifier (single-pass closure)
|- Prompt: `ai/tasks/task-0016.md`
|- Verifier report: `ai/reports/task-0016-verifier.md`
|- Status: **verified PASS, merged 2026-05-30T12:31:56Z**
|- Milestone: M5.a — `orun plan` rewire
|- Implementation: PR **#161** on `impl/task-0016-m5a-orun-plan-rewire`,
   squash-merged to `main` as `7a9c494` "Task 0016: M5.a — orun plan rewire
   to revision-first layout (#161)".
|- PR CI: `CI / Orun Plan` run `26683860043` (44s, success);
   `Harness dry-run guard` run `26683860052` (15s, success). Both required
   checks PASS at log level on final head SHA after verifier-side commit
   `01e75bd`.
|- Diff stat: 10 files changed, +505 / -65. Modified `cmd/orun/main.go`
   (+193 to add `computePlanHashForRevision` + `canonicalPlanJSON` helpers,
   replace `state.SavePlan` branch with full revision pipeline, emit §1.1
   summary block), `internal/model/plan.go` (+18 for `PlanRevisionMeta` +
   `PlanTrigger.Type`/`Name`), `internal/revision/legacy.go` (+30 for
   `WriteLegacyNamedPlan`). New tests: `cmd/orun/command_plan_revision_test.go`
   (4 tests covering plan-hash invariance under checksum/revision mutation +
   nil-plan paths) and `internal/revision/legacy_test.go` (4 tests covering
   byte-identical alias writes + reserved/bad-name + nil-store).
|- Coverage: `internal/statestore` 95.7 % (≥95 %); `internal/revision` 90.4 %
   (≥90 %); `internal/executionstate` 90.0 % (exact floor held — package not
   touched in M5.a).
|- Objective: rewire `orun plan` to the canonical revision-first flow per
   `cli-surface.md` §1 — always resolve `TriggerOccurrence` via
   `internal/triggerctx`, embed `metadata.trigger` + `metadata.revision`,
   persist via `internal/revision.WriteRevision` (canonical layout + refs +
   indexes), write byte-identical compat aliases at
   `.orun/plans/<checksum>.json` and `.orun/plans/latest.json`, preserve
   `-o/--output`, emit the new on-success summary block.
|- Scope boundary (in): `cmd/orun/main.go` (plan path) + `internal/model`
   `PlanRevisionMeta`/`PlanTrigger` additions + `internal/revision` legacy
   helper + tests. (out, held): `orun run` (M5.b), `orun status` / `logs` /
   `describe` / `get plans` (M5.c), hidden `orun state migrate` (M5.d),
   `internal/runner` / `internal/runbundle` / `internal/state` /
   `internal/executionstate`.
|- Durable outcome on main: `orun plan` produces canonical revision layout
   (`.orun/revisions/<key>/{plan,trigger,revision,manifest}.json`,
   `.orun/refs/latest-revision.json`, `.orun/refs/triggers/<type>/<name>/{latest,manual}.json`,
   `.orun/indexes/revisions/<key>.json`) plus byte-identical compat aliases
   (`.orun/plans/<sha256>.json` + `.orun/plans/latest.json` + optional
   `.orun/plans/<name>.json`) on every invocation. `metadata.trigger.Type`/
   `Name` and `metadata.revision.{Key,PlanHash}` always populated. Plan
   hash is canonical sha256 with self-referential metadata cleared per
   data-model.md §3.1 — invariant under checksum and revision mutation.
   `-o/--output` preserved as additive copy. New summary block (`✓ Plan
   revision created` / Revision / Trigger / Jobs / Path / Output) renders
   before legacy `components × envs → jobs` line so existing tooling that
   scans for the legacy line keeps working.
|- Verifier adjudications: scope discipline held (no overreach into runner /
   runbundle / state / executionstate); no spec proposals filed; no spec
   drift; risk notes from Task 0015 carried forward unchanged.
|- Unblocks: **Task 0018 = M5.b `orun run` rewire** (resolve PlanRevision via
   `internal/revision.ResolveRevision`, materialize in-memory system.manual
   revision when none exists, create executions via
   `internal/executionstate.CreateExecution`, hook runner snapshot stream
   into `Bridge.MirrorRunnerOutput`, add `--revision` flag, pin
   `bridge-mirror-failed` payload schema in `data-model.md` §9).

## Task 0018

|- Agent: Implementer + Verifier (single-pass closure)
|- Prompt: `ai/tasks/task-0018.md`
|- Implementer report: `ai/reports/task-0018-implementer.md`
|- Verifier report: `ai/reports/task-0018-verifier.md`
|- Status: **verified PASS, merged 2026-05-30T13:42:02Z**
|- Milestone: M5.b — `orun run` rewire onto revision-first execution path
|- Implementation: PR **#162** on `impl/task-0018-m5b-orun-run-rewire`,
   squash-merged to `main` as `59d06f3` "M5.b: rewire `orun run` onto the
   revision-first execution path (#162)". Head SHA at merge `e5dd580`.
|- PR CI: `CI / Orun Plan` PASS (45s); `Harness dry-run guard` PASS (12s).
   Both required checks PASS at log level on final head SHA. 5 matrix
   legs SKIPPED (empty matrix at M5.b — same shape as M5.a #161).
|- Diff stat: 5 files changed, +757 / -0. New `cmd/orun/command_run_revision.go`
   (365 LOC) houses `setupRevisionExecution` / `installRevisionHooks` /
   `finalizeRevisionExecution` / `synthesizeRevisionForRun` /
   `printRevisionRunSummary`. New `cmd/orun/command_run_revision_test.go`
   (298 LOC) covers synth round-trip, happy-path setup+finalize,
   failed-runner path, `--revision` short-circuit, `--exec-id` plumbing,
   nil-rx no-op, absolute store-root invariant, defensive nil-plan /
   empty-execID rejection. Modified `cmd/orun/command_run.go` (register
   `--revision` flag, wire setup/finalize around `r.Run(plan)`),
   `internal/runner/runner.go` (add `RunnerHooks.AfterStateUpdate` fired
   from `updateState` after `SaveState`),
   `specs/orun-state-redesign/data-model.md` (pin §9.1
   `bridge-mirror-failed` event payload schema).
|- Coverage: `internal/statestore` 95.7 % (≥95 %); `internal/revision`
   90.4 % (≥90 %); `internal/executionstate` 90.0 % (≥90 %, exact floor
   held — M5.b touched the package only via API consumption).
|- Objective: rewire `orun run` to the canonical revision-first flow per
   `cli-surface.md` §2 — resolve `PlanRevision` via the seven-branch
   `ResolveRevision` (latest / file / revision-key / named-ref /
   legacy-hash / component-name / ambiguous), synthesize-and-re-resolve
   when the resolver returns a benign miss so a fresh `orun run` (no
   preceding `orun plan`) still lands a real on-disk triplet, persist an
   `ExecutionRun` via `internal/executionstate.CreateExecution`, mirror
   runner `state.json`/`metadata.json` via `Bridge.MirrorRunnerOutput`
   (mode `Auto`: hardlink → copy fallback on EXDEV per design.md §11) on
   every runner tick, mark the execution terminal with summary counts on
   runner return, register `--revision <key>` flag (cli-surface.md §2.3)
   that short-circuits the resolution chain, and pin
   `bridge-mirror-failed` event payload schema in `data-model.md` §9.1.
|- Scope boundary (in): `cmd/orun/command_run.go` + new
   `cmd/orun/command_run_revision*.go` + minimal runner-snapshot bridge
   glue in `internal/runner/runner.go` (`AfterStateUpdate` hook only) +
   `data-model.md` §9.1 schema pin. (out, held): `orun status` / `logs`
   / `describe` / `get` (M5.c), hidden `orun state migrate` (M5.d),
   `--persist-revision` flag (Phase 1 reservation, synthesize-fallback
   covers the gap), Reason="rerun"/"retry"/"migration" (only
   "direct-run" emitted from this path).
|- Durable outcome on main: `orun run` produces canonical execution
   layout under `revisions/<key>/executions/<execKey>/`
   (`execution.json` with `status`, `reason="direct-run"`, `summary`
   counts, populated `revisionId`/`triggerId` ULIDs,
   `originalKey=<legacyExecID>`; `state.json` + `metadata.json` mirrored
   per tick from `.orun/executions/<legacyExecID>/`;
   `events/00000000000000000001-execution-created.json` written on
   creation), and updates `refs/latest-execution.json`. `--dry-run` and
   remote-state branches NOT wired (lifecycle owned upstream). Final
   summary block prints Revision / Execution / Path lines per
   cli-surface.md §1.1, matching the M5.a `orun plan` shape.
|- Verifier adjudications: scope discipline held (no overreach into
   `internal/state` / `internal/runbundle` / `internal/executor`; only
   `RunnerHooks.AfterStateUpdate` field added to runner package); no
   spec proposals filed; risk note "bridge-mirror-failed payload schema
   un-pinned" CLOSED in this task; new risk noted: bridge mirror runs
   synchronously inside `updateState` on the runner goroutine — may
   want async buffer in M5.c if real workloads regress.
|- Unblocks: **Task 0019 = M5.c `orun status / logs / describe / get`
   rewire** (read execution lifecycle from
   `revisions/<key>/executions/<execKey>/`, fall back to legacy
   `.orun/executions/<id>/` via resolver legacy-fallback path, surface
   `bridge-mirror-failed` events on stderr/metrics, expose new triplet
   shape in describe output).

## Task 0019 — M5.c `orun status / logs / describe / get plans` rewire

|- Agent: Implementer (closed) → Verifier (emitted)
|- Implementer prompt: `ai/tasks/task-0019.md`
|- Implementer report: `ai/reports/task-0019-implementer.md`
|- Verifier prompt: `ai/tasks/task-0019-verifier.md`
|- PR: **#163** on `impl/task-0019-m5c-orun-read-commands-rewire`, head `947773d` (+`fb364f1` housekeeping). mergeable=MERGEABLE / mergeStateStatus=CLEAN.
|- Required CI on final head SHA: `CI / Orun Plan` run 26686932774 PASS (56s); `Harness dry-run guard` run 26686932783 PASS (13s). 5 matrix legs SKIPPED.
|- Status: implementer COMPLETE 2026-05-30; verifier scoped and emitted.
|- Objective: rewire `orun status` / `orun logs` / `orun describe` / `orun get plans` onto canonical revision-first layout (`revisions/<revKey>/executions/<execKey>/{execution,snapshot.latest}.json`, `refs/latest-execution.json`, `indexes/executions/`, `indexes/triggers/`, `refs/named/`) with `.orun/executions/<id>/` as transparent fallback per `compatibility-and-migration.md` §3–§4. Surface `bridge-mirror-failed` events on stderr (best-effort, non-blocking). Expose triplet (revisionKey + executionKey + legacyExecID) in describe.
|- Scope boundary (in): `cmd/orun/read_resolve.go` (new, 98 LOC), `cmd/orun/bridge_mirror_warn.go` (new, 57 LOC), `command_status.go` (+10), `command_logs.go` (+25/-11), `command_describe.go` (+161), `command_get.go` (+166), `cmd/orun/command_read_revision_test.go` (new, 385 LOC), `ai/reports/task-0019-implementer.md`. Diff: 8 files, +959/-12. (out, held): writer/runner/executor/executionstate-writer/revision-writer/statestore edits, spec behavioral edits, new event kinds, `--persist-revision`, `orun state migrate` (M5.d), TUI cockpit, GHA artifact pipeline.
|- Acceptance (per implementer report, verifier-confirmed pending): all four read commands route through `executionstate.ResolveExecution` / `revision.ResolveRevision` with legacy fallback; `--revision` / `--exec-id` / `--all` flags wired on `status` / `logs` per cli-surface.md §3.2 / §4; `describe revision|trigger|execution` aliases functional; `get plans` revision-first table with legacy fallback + stable `--json`; `bridge-mirror-failed` one-line stderr per distinct execution + silent degradation on malformed events. Coverage held: statestore 95.7% / revision 90.4% / executionstate 90.0% (exact floor). go build/vet, go test -race ./..., make test-state-redesign all green per implementer.
|- Verifier scope: PR-boundary diff scan; quality-gate replay on PR head; fresh / legacy / mixed temp-workspace walks; new flag/alias exercise; bridge-mirror-failed surfacing + malformed-events silent-degradation check; `gh run view --log` on both required checks; coverage gates preserved. If PASS → squash-merge #163 + ff-pull main + emit Task 0020 (M5.d hidden `orun state migrate` implementer). If FAIL → leave PR open with explicit blockers.
|- Unblocks (on PASS+merge): Task 0020 (M5.d hidden `orun state migrate` implementer per `compatibility-and-migration.md` §5), which closes M5 and opens M6 (E2E + property gates).

## Task 0020 — M5.d hidden `orun state migrate` command

|- Agent: Implementer + Verifier (single-pass closure per the explicit "verify, merge, housekeeping" full-ship-cycle directive — see `orun-saas-verifier` skill convention)
|- Implementer prompt: `ai/tasks/task-0020.md` (filed proactively by the implementer because Task 0019's verifier merged #163 without emitting one)
|- Implementer report: `ai/reports/task-0020-implementer.md`
|- Verifier report: `ai/reports/task-0020-verifier.md` (no separate verifier prompt — single-pass)
|- PR: **#164** on `impl/task-0020-m5d-orun-state-migrate`. Final head SHA at merge: `c3cddb7` (verifier-report commit on top of feature commit `06b64d9`). mergeable=MERGEABLE.
|- Merge: squash-commit `17ef788` "Task 0020: M5.d — hidden orun state migrate command (#164)" on 2026-05-30T16:03:59Z.
|- Required CI on final head SHA: `Orun Plan` PASS (52s); `Harness dry-run guard` PASS (13s). 6 matrix legs SKIPPED (empty matrix, expected for state-redesign tasks).
|- Status: PASS + merged 2026-05-30. **M5 milestone CLOSED** — all four sub-tasks merged on main.
|- Objective: implement the hidden, idempotent, non-destructive `orun state migrate` (+ `--dry-run`) command per `specs/orun-state-redesign/compatibility-and-migration.md` §5. Two-phase walk: phase 1 scans `.orun/plans/<hex>.json` and synthesizes `system.migrated` revisions via the existing branch-5 `ResolveRevision` path; phase 2 walks `.orun/executions/<id>/`, attaches each execution to either an existing revision (matched by plan hash via dual-keyed lookup) or an `ensureUnknownRevision`-created placeholder, then promotes legacy state.json/metadata.json into `revisions/<revKey>/executions/<execKey>/` via `Bridge.MirrorRunnerOutput`. Plus Option A from `ai/proposals/task-0019-spec-update.md`: `describeRevision` / `describeTrigger` normalize literal `"latest"` to `""` at the CLI entrance.
|- Scope boundary (in): `cmd/orun/command_state_migrate.go` (new, ~430 LOC, command + algorithm), `cmd/orun/command_state_migrate_test.go` (new, ~260 LOC, 5 tests), `internal/revision/legacy_scan.go` (new, ~95 LOC, `ScanLegacyPlanHashes` helper), `cmd/orun/commands_root.go` (+1: `registerStateCommand` wired in init()), `cmd/orun/command_describe.go` (Option A normalization, ~14 LOC), task/report/state-file artifacts. Diff: 14 files, +1709 / -81. (out, held): `--persist-revision` flag (Phase 1 reservation, migrate persists unconditionally), `--prune-legacy` (spec §6 Phase 2, explicitly out of M5), Option B trigger-name resolver branch (deferred — larger surface change crossing internal/revision boundary), bridge-mirror surface extension (`bridgeMirroredFiles` not extended), runner / writer / executionstate API changes.
|- Acceptance (verifier-confirmed): spec §5.1 algorithm correspondence verbatim (plans → ResolveRevision branch 5 → WriteRevision; executions → CreateExecution(Reason=migration) + Bridge.MirrorRunnerOutput); spec §5.2 properties (idempotent via `TestStateMigrate_Idempotent`, non-destructive via `grep -n 'os.Remove' clean`, resumable via independent atomic phases); path policy (zero `.orun/...` concatenation in caller, only the existing `openLocalStateStore`-pattern store-root translation); no new sentinel errors (only the four existing statestore sentinels wrapped via `fmt.Errorf`); strict JSON decode via existing helpers; `WithCompatibilityWrites(false)` on migrate-side writes; Option A literal-`latest` normalization passes via `TestDescribeRevision_LiteralLatest_NormalizesToEmpty`. `go test ./... -count=1 -timeout 180s` all-green (cmd/orun 9.94s, revision 5.33s, executionstate 29.43s, statestore 20.16s); `go vet ./...` clean.
|- Risk Notes: (a) unknown-hash placeholder body for orphan executions — sentinel JSON, migrate is the only writer of unknown-hash revisions by construction (CreateIfAbsent + ErrExists); (b) `hashToRev` dual-keying (canonical `sha256:<hex>` AND bare-hex stem) depends on `state.PlanChecksumShort` continuing to emit bare-hex — covered by tests but worth flagging if that helper is ever changed. Both carried into M6 / current.md.
|- Spec proposals filed: none new. `ai/proposals/task-0019-spec-update.md` Option A is now implemented in this PR; Option B retained as queued source of truth for whoever picks up trigger-name resolver branch (foldable into M6 or standalone polish).
|- Unblocks: **M6 — E2E + property gates** (Task 0021, to be emitted). Branch base: `main` at `17ef788`.

## Task 0021 — M6 E2E + property gates

- Agent: Implementer + Verifier (single-pass closure)
- Prompt: `ai/tasks/task-0021.md`
- Status: **PASS + merged** (2026-05-30T19:17:23Z). **M6 milestone CLOSED — Phase 1 of orun-state-redesign STRUCTURALLY COMPLETE**.
- Base: `main` at `32d026f`. Branch: `impl/task-0021-m6-e2e-and-property-gates`.
- Implementation: PR **#165** squash-merged to main as `ad3656e` "Task 0021 / M6: end-to-end + property gates for state redesign (#165)" on 2026-05-30T19:17:23Z. Branch deleted.
- Required CI on PR head: `Orun Plan` PASS (53 s); `Harness dry-run guard` PASS (15 s). Matrix legs SKIPPED legitimately (empty matrix — test-only change).
- Implementer report: `ai/tasks/task-0021-report.md`.
- Verifier report: `ai/reports/task-0021-verifier.md` (single-pass closure — no separate verifier prompt).
- Objective: lock in Phase-1 correctness properties from `design.md` §9 as permanent regression gates, and ship the end-to-end CLI walk that exercises the full revision-first path through real fixtures.
- Scope landed (test-only / coverage-gate, +829 / -5):
  - new `cmd/orun/state_e2e_test.go` (~340 LOC, TestStateE2E with 14 sub-tests walking test-plan.md §4 from plan synthesis → revision documents → latest refs → legacy plans/ mirror → execution setup+finalize → execution.json terminal status → indexes/executions → read-side resolver → describe revision latest → get plans → state migrate dry-run + sha256 byte-equal idempotence). Each spec step gets its own `t.Run` for unambiguous failure attribution. Drives the same package-level subroutines (`synthesizeRevisionForRun`, `setupRevisionExecution`, `finalizeRevisionExecution`, `describeRevision`, `loadRevisionPlanRows`, `runStateMigrate`) the Cobra RunE handlers call.
  - new `internal/revision/keys_property_test.go` (~190 LOC, `pgregory.net/rapid`-driven properties): determinism, distinctness (rules out scope-truncation / short-hash regression), and ResolveCollision suffix contiguity (-x1..-xN under N forced collisions with isolated per-iteration LocalStore).
  - new `internal/revision/m6_coverage_test.go` (~160 LOC, targeted unit coverage): `ScanLegacyPlanHashes` (was 0%) — empty workspace, nil store, filter+sort path; `WriteLegacyNamedPlan` — nil store, reserved "latest", invalid component, happy path; `RevisionKey` input validation — zero trigger, short plan hash.
  - modified `Makefile` (`test-state-redesign` propagates `-race` to every step, adds `TestStateE2E` invocation, keeps the three coverage gates).
- Acceptance (verifier-confirmed on main post-merge): `go test ./... -race -count=1 -timeout 600s` all-green; `make test-state-redesign` all four gates green (statestore 95.7 %, revision 90.3 %, executionstate 90.0 %, triggerctx passes); `go vet ./...` clean. The `internal/revision` floor had been silently breached at 84.9 % on main tip pre-M6 (gate ran without `-race` under prior Makefile); M6 restores the documented ≥ 90 % floor by adding the M6-coverage unit tests **without lowering any threshold**.
- Risk Notes: `step05_legacy_plan_mirror_present` strips the canonical `sha256:` prefix when constructing the expected mirror path because `writer.writeCompatibilityMirror` normalises through `normalizeLegacyChecksum` before persisting — codified in the test comment to prevent future confusion.
- Spec proposals filed: none new.
- Carry-forward (deferred per task-0021 §non-goals): MirrorModeHardlink debug-fold decision; RunnerHooks.AfterStateUpdate async-mirror evaluation; `--persist-revision` flag wiring (Phase 2); Option B trigger-name resolver branch (`ai/proposals/task-0019-spec-update.md`); `--prune-legacy` (Phase 2 §6).
- Unblocks: Phase 2 work and the deferred housekeeping list above. Next session re-scopes from the candidates documented in `ai/context/current.md`.

---

# Phase 2 — orun-component-catalog

Task numbering continues. Phase 2 ships the SourceSnapshot/CatalogSnapshot
content-addressed component-catalog model wrapping the Phase 1 trigger /
revision / execution lineage. Authoritative spec:
`specs/orun-component-catalog/`. Local-only (no HTTP, no SaaS, no DB
schema). Milestones C0–C9 per `implementation-plan.md`.

## Task 0022

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0022.md`
|- Status: implementer in progress (2026-05-31) — branch `impl/task-0022-phase2-rollover`
|- Milestone: C0 (spec-landing half; the C0 code half ships in Task 0023)
|- Objective: land the Phase 2 rollover as a single docs-and-bookkeeping
   PR — commit `specs/orun-component-catalog/` (12 docs), the rewritten
   `agents/orchestrator.md`, and the rotated `ai/state.json`; refresh
   `ai/context/current.md` and this ledger; dispose of the root-level
   `orun-catalog-full-design.md` monolith (delete or archive under
   `_archive/` with a README); reset `ai/waiting_for_input.md`.
|- Scope boundary: docs and state files only. **No Go code, no CLI
   changes, no `internal/catalogmodel`, no JSON-Schema generator** —
   those are Task 0023 / C0 code half.
|- Acceptance: 12 spec docs present on branch; `agents/orchestrator.md`
   cites the new spec pack and milestones C0–C9; `ai/state.json` has
   `active_spec=specs/orun-component-catalog`, `active_milestone=C0`,
   and the `phase_history.phase_1_orun_state_redesign` block;
   monolith removed from repo root; `go build ./...` and `go test ./...`
   green (sanity); required CI checks PASS on PR head.
|- Expected outcome: Phase 2 is on `main` with an authoritative spec
   pack, an orchestrator prompt that points at it, and clean state
   files. Task 0023 (C0 code half — `internal/catalogmodel` +
   `internal/sourcectx` skeleton + JSON-Schema generator) becomes the
   next implementer task.

## Task 0022 (Verifier pass)

|- Agent: Verifier
|- Prompt: `ai/tasks/task-0022-verifier.md`
|- Status: **PASS** + merged (2026-05-31). PR #167 squash commit on
   `main`: `d435d8f`. PR-CI on rebased head `b58e76f`:
   `CI / Orun Plan` SUCCESS (run `26703688956`); matrix leg
   `${{ matrix.component }}/${{ matrix.env }}` SKIPPED (legitimate
   empty-matrix path for a docs-only diff). Original implementer head
   `1016a2b` PR-CI was also SUCCESS (run `26703506317`).
|- Implementation: PR `https://github.com/sourceplane/orun/pull/167`,
   branch `impl/task-0022-phase2-rollover` (deleted post-merge).
|- Reports: implementer at `ai/reports/task-0022-implementer.md`,
   verifier at `ai/reports/task-0022-verifier.md`.
|- Scope (verified): zero Go in diff; 12 spec docs +
   `_archive/{full-design-monolith,README}.md` present;
   `agents/orchestrator.md` carries Phase 2 rewrite (active spec,
   C0–C9, Phase 1 demoted, Deferred Decision Protocol present);
   `ai/state.json` shape correct with `phase_history` block recording
   coverage floors verbatim; Phase 1 ledger byte-identical (0
   deletion lines); `ai/waiting_for_input.md` is a clean "no input"
   note.
|- Local gates (on PR head): `go build ./...`, `go vet ./...`,
   `go test ./... -count=1 -timeout 600s`, `make test-state-redesign`
   all green. Coverage floors held verbatim — `internal/statestore`
   95.7 %, `internal/revision` 90.3 %, `internal/executionstate`
   90.0 %, `internal/triggerctx` passes.
|- Secret sweep: zero hits.
|- Spec proposals: none required.
|- Durable outcome: Phase 2 (`specs/orun-component-catalog/`) is the
   active authoritative spec on `main`. Phase 1 invariants and
   coverage floors preserved. `ai/state.json.current_task` rolled
   forward to `0023`; `task_agent` flipped to the verifier report
   path. Repo is clean and ready for the C0 code half.

## Task 0023

|- Agent: Implementer + Verifier (single-pass closure)
|- Implementer prompt: `ai/tasks/task-0023.md`
|- Verifier prompt: `ai/tasks/task-0023-verifier.md`
|- Status: **PASS** + merged (2026-05-31). PR #168 squash commit on
   `main`: `7f3f2bf`. PR-CI on verifier-fix head `3bd728c`:
   `state-redesign-tests / test` SUCCESS (run `26704421826`),
   `CI / Orun Plan` SUCCESS (run `26704421856`),
   `Harness dry-run guard` SUCCESS. Matrix legs SKIPPED legitimately.
|- Implementation: PR `https://github.com/sourceplane/orun/pull/168`,
   branch `impl/task-0023-c0-catalogmodel` (deleted post-merge).
|- Reports: implementer at `ai/reports/task-0023-implementer.md`,
   verifier at `ai/reports/task-0023-verifier.md`.
|- Milestone: C0 (code half) — pure data models, canonical encoder,
   JSON Schema generator + drift gate, golden roundtrip fixtures,
   property tests T-IDK-1 / T-IDK-3 / T-IDK-5, internal/sourcectx
   skeleton (types only — resolver lands in C1).
|- Scope (verified): new `internal/catalogmodel/` (15 Go files +
   schema generator + 9 golden fixtures + roundtrip / property /
   sanitize tests + verifier-added `coverage_test.go`), new
   `internal/sourcectx/` skeleton (model/keys/hash + tests),
   `Makefile` extended with package-level coverage gates and
   `verify-generated`, `.github/workflows/state-redesign-tests.yml`
   gates both targets. Zero Phase 1 file edits; zero spec edits.
|- Verifier-attached fix on PR #168: implementer shipped at 81.7 %
   catalogmodel coverage (under the spec-mandated ≥ 90 % floor)
   because the Makefile only gated `Sanitize*` at 100 %. Verifier
   added `internal/catalogmodel/coverage_test.go` (8 tests covering
   `CanonicalEncodeString` / `CanonicalEqual` / `CatalogInputHash` +
   edge paths) and hardened the Makefile with package-level ≥ 90 %
   gates on both `internal/catalogmodel` and `internal/sourcectx`.
|- Local gates: `go build`, `go vet`, `go test ./... -race`,
   `make test-state-redesign`, `make verify-generated` all green.
   Final coverage on main: catalogmodel **90.2 %**, sourcectx
   **91.3 %**, Sanitize* **100 %**, statestore 95.7 %, revision
   90.3 %, executionstate 90.0 %.
|- Leaf-clean: `go list -deps ./internal/catalogmodel/...` shows no
   `internal/*` imports outside the package itself + its `schema/gen`
   subpackage. Phase 1 invariants preserved byte-for-byte.
|- Secret sweep: zero hits in fixtures or logs.
|- Spec proposals: none.
|- Durable outcome: Phase 2 C0 ✅ COMPLETE on `main`. The pure-data
   foundation (`internal/catalogmodel`) and resolver-injection seams
   (`internal/sourcectx` types) are stable. `ai/state.json` rolled
   forward to `current_task=0024 / active_milestone=C1`.

## Task 0024

|- Agent: Implementer + Verifier (closed inline cycle)
|- Prompt (impl): `ai/tasks/task-0024.md`
|- Prompt (verifier): `ai/tasks/task-0024-verifier.md`
|- Reports: `ai/reports/task-0024-implementer.md`,
   `ai/reports/task-0024-verifier.md`
|- Status: ✅ verified PASS and merged via PR #169 (squash `b50d799`) on
   2026-05-31.
|- Milestone: C1 — `internal/sourcectx` resolver.
|- Scope (verified): `ResolveSourceSnapshot(ctx, opts) (WorkspaceState, error)`
   with injected `Git` / `Clock` / `Filesystem` adapters. `WorkspaceState`
   populated from adapters: `headRevision`, `treeHash`, `dirtyHash`,
   `catalogInputHash`. Scope detection (branch / PR / tag / local-dirty
   / local-nogit / ci-event). Default Git adapter ships. Clean / dirty /
   no-git / PR-injected / branch / tag fixtures all produce the
   spec-documented `SourceSnapshotKey` shapes. T-IDK-3 + T-IDK-4
   property tests included. Leaf-clean — no `internal/sourcectx`
   import outside its own tree.
|- Verifier-attached fix on PR #169: pre-existing C0 catalogmodel
   coverage flake at the 90 % gate (rapid-driven property tests
   sometimes failed to draw `\b`/`\f` strings, dropping
   `writeQuotedString` coverage from 100 % to 93.5 % and tipping the
   package below 90 %). Verifier added
   `internal/catalogmodel/coverage_test.go::TestCanonicalEncodeStringEscapeBranches`
   (16 deterministic cases covering every escape branch + U+2028 /
   U+2029 / multibyte UTF-8). Post-fix 20-run spread: 91.1 % × 19,
   90.6 % × 1. Sanitize* stays at 100.0 %.
|- Local gates on main post-merge: `go build`, `go vet`,
   `go test ./... -race`, `make test-state-redesign` × 3,
   `make verify-generated`,
   `kiox -- orun validate --intent intent.yaml` all green. Coverage
   floors held: catalogmodel ≥ 90 % deterministic, sourcectx 91.1 %,
   Sanitize* 100 %, statestore 95.7 %, revision 90.3 %, executionstate
   90.0 %.
|- Spec proposals: none.
|- Durable outcome: Phase 2 C1 ✅ COMPLETE on `main`. The
   `internal/sourcectx` resolver is the deterministic input layer for
   `internal/catalogresolve` (C2). `dirtyHash` and `catalogInputHash`
   flow correctly; the C0 catalogmodel gate is now deterministic.

## Task 0025

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0025.md`
|- Status: scoped and ready to begin (2026-05-31).
|- Milestone: C2 first PR — `internal/catalogresolve` (discover + load
   + inherit only).
|- Objective: stand up `internal/catalogresolve` with stages 2 / 3 of
   the resolution pipeline — workspace discovery of `component.yaml`,
   schema-validated load via the embedded
   `internal/catalogmodel/schema/component-yaml.schema.json`, and
   authored-only inheritance from `intent.yaml` `catalog.*` defaults
   (per `resolution-pipeline.md` §3 map-vs-list semantics). Output:
   `DiscoverAndLoad(ctx, opts) (DiscoveryResult, error)` returning a
   deterministic in-memory `[]AuthoredManifest` consumable by Task 0026.
|- Scope boundary: only `internal/catalogresolve/` package source
   (`discover.go`, `load.go`, `inherit.go`, types, `testdata/` fixtures,
   tests) plus the Makefile coverage gate for the new package. Zero
   edits to `internal/catalogmodel/`, `internal/sourcectx/`, or any
   Phase 1 internal package. Leaf-clean: may import only
   `internal/catalogmodel` + stdlib + already-pulled deps.
|- Acceptance: `go build` + `go vet` + `go test ./... -race` green;
   `make test-state-redesign` and `make verify-generated` green;
   `internal/catalogresolve` ≥ 90 % coverage; two consecutive
   `DiscoverAndLoad` calls byte-identical on the canonical fixture;
   typed errors for mixed `.yaml`/`.yml` collisions and schema-invalid
   manifests (with file + JSON pointer); inheritance fixture proves
   authored-precedence + map-key merge + list-wholesale + empty-list-as-set;
   Phase 1 + C0 + C1 floors held byte-for-byte; PR opened with real
   number backfilled into the implementer report.
|- Expected outcome: in-memory `[]AuthoredManifest` from
   `DiscoverAndLoad`, ready for Task 0026 (C2 PR-2: infer + deps +
   validate + `manifestHash`) to consume.
|- Non-goals: no inference, no deps resolution, no validation matrix,
   no `manifestHash`, no graph build, no `CatalogSnapshot`, no
   `catalogHash`, no `internal/catalogstore` writes, no CLI changes.
|- Implementation outcome (2026-05-31): branch
   `impl/task-0025-c2-discover-load-inherit` shipped to PR #170 —
   OPEN/MERGEABLE/CLEAN, all required CI green
   (`state-redesign-tests/test`, `CI/Orun Plan`, `Harness dry-run guard`).
   Diff +1 413 / −0 across 26 files: net new `internal/catalogresolve/`
   (10 source/test + 13 testdata fixtures), one additive file
   `internal/catalogmodel/schema_embed.go` (8 lines, `//go:embed`-only),
   `Makefile` +7 lines (catalogresolve gate ≥ 90 %). Phase 1 + C0 + C1
   floors held; new `internal/catalogresolve` measured 90.0 % exact
   (zero headroom — verifier should reproduce 3× and add deterministic
   backstop on any drift). Two implementer call-outs flagged for
   verifier: (1) additive `schema_embed.go` in `catalogmodel/` with
   proposed PR-Boundary wording tightening; (2) zero coverage headroom.
   Implementer report: `ai/reports/task-0025-implementer.md`.

## Task 0025 — Verifier

|- Agent: Verifier
|- Prompt: `ai/tasks/task-0025-verifier.md`
|- Status: ✅ verified PASS and merged via PR #170 (squash `723be32`)
   on 2026-05-31T07:06:29Z.
|- Target PR: #170 on branch `impl/task-0025-c2-discover-load-inherit`.
|- Objective: verify PR #170 against the Verifier Standard in
   `agents/orchestrator.md` and the Task 0025 acceptance criteria.
   Adjudicate the implementer's two call-outs (additive `schema_embed.go`
   in `catalogmodel/`; 90.0 % exact coverage on `catalogresolve`). On
   PASS, squash-merge with `--admin --delete-branch`, fast-forward local
   `main`, leave the working tree clean.
|- Permitted edits: `ai/reports/task-0025-verifier.md` and a minimal
   deterministic coverage backstop in `internal/catalogresolve/*_test.go`
   if reproducible flake at the 90 % gate is observed. No backstop
   needed — coverage was deterministic.
|- Acceptance (all met): PR Boundary fidelity confirmed (only
   `catalogresolve/`, `catalogmodel/schema_embed.go`, `Makefile`,
   reports); 3× `make test-state-redesign` stable at **90.0 %** on
   `catalogresolve`; Phase 1 / C0 / C1 coverage floors held
   byte-for-byte; determinism stress (`go test -count=10
   ./internal/catalogresolve/...`) green with 0 failures; PR #170 CI
   rollup CLEAN on rebased head SHA `835262e` (post verifier-report
   commit); verifier report filed at
   `ai/reports/task-0025-verifier.md`; squash commit `723be32` +
   merged-at `2026-05-31T07:06:29Z` recorded.
|- Adjudication outcomes:
   1. **Schema-embed call-out (ACCEPT).** Additive
      `internal/catalogmodel/schema_embed.go` (18 lines,
      `//go:embed`-only) is the narrowest possible reading of the
      "no edits to `catalogmodel/`" constraint given that `//go:embed`
      cannot escape its package and vendoring is forbidden by spec.
      Convention adopted: *"one additive file per cross-package contract
      surface in `internal/catalogmodel/`, no edits to existing source
      files, embed-only or read-only typed view, no logic"*. Spec
      proposal at `ai/proposals/task-0025-spec-update.md` tightens the
      C2 PR-Boundary wording for Tasks 0026 onward.
   2. **Coverage-headroom call-out (ACCEPT WITH RISK NOTE).**
      `internal/catalogresolve` measured 90.0 % exact across 3
      consecutive local `make test-state-redesign` runs and CI run
      26705772895 — deterministic, no `pgregory.net/rapid` variance,
      `go test -count=10 -race` zero failures. No backstop required;
      Task 0026 PR-2 will naturally raise the floor as more resolver
      machinery lands.
|- Spec proposals: `ai/proposals/task-0025-spec-update.md` (PR-Boundary
   wording tightening for `catalogmodel/` + `sourcectx/` to permit
   additive sibling files for cross-package contract surfaces).
|- Durable outcome: Phase 2 C2 PR-1 ✅ COMPLETE on `main`. The
   `internal/catalogresolve` package now provides
   `DiscoverAndLoad(ctx, Options) (DiscoveryResult, error)` —
   workspace walk + schema-validated load + intent-defaults inheritance
   producing a deterministic sorted `[]AuthoredManifest` with RFC 6901
   provenance. Mini-T-RES-1 asserted in-package. Ready for Task 0026
   (C2 PR-2: infer + deps + validate + `manifestHash`).

## Task 0026

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0026.md`
|- Status: **verified PASS and merged via PR #171** (2026-05-31T08:36:04Z, main commit `74b88e0`)
|- Milestone: C2 — `internal/catalogresolve` (PR-2; closes C2)
|- Branch: `task-0026-catalogresolve-c2-pr2` @ `9c65e7c` (squashed and deleted)
|- Implementation: PR **#171** squash-merged → main commit `74b88e0c3c607e63143acb9e6fddd3016bf71ee1`. Branch deleted.
|- PR CI on head `9c65e7c`: `Orun Plan` SUCCESS (run `26706854744`, 43s),
   `Harness dry-run guard` SUCCESS (run `26706854741`, 18s, full `[guard] PASS`
   battery), `test` SUCCESS (run `26706854770`, 1m26s). Matrix legs SKIPPED
   legitimately (PR-time plan-only profile). mergeable=MERGEABLE,
   mergeStateStatus=CLEAN at merge time.
|- Reports: implementer at `ai/reports/task-0026-implementer.md`; verifier at
   `ai/reports/task-0027-verifier.md`.
|- Objective: close C2 by adding the second `internal/catalogresolve` PR — top-level
   `Resolve(ctx, opts) (*ResolvedCatalog, []ValidationIssue, error)` covering
   resolution-pipeline stages 4 (infer), 5/6 (validate), 7 (assemble), 8 (deps),
   9 (validate post-deps), 10 (`manifestHash`). T-RES-1 (resolver determinism),
   T-RES-2 (provenance completeness), and `ErrDependencyMissing` land here.
|- Scope boundary: `internal/catalogresolve/` (new files: `assemble.go`, `clock.go`,
   `dependencies.go`, `errors.go`, `hash.go`, `infer.go`, `resolve_full.go`,
   `validate.go`, `resolve_full_test.go`, `testdata/resolve_e2e/`,
   `testdata/resolve_cycle/`); additive edits to `intent.go` (+intentInference
   pointer-mirror) and `types.go` (+ResolvedCatalog; +Options.{Strict,Repo,
   Namespace,Clock}). NO edits outside `internal/catalogresolve/`. No CLI
   wiring. No catalogstore/graph work (deferred to C3+).
|- Durable outcome on main:
   1. `manifestHash` computed via `catalogmodel.CanonicalEncode` over
      `{identity, metadata, spec, runtime}` masks provenance fields automatically
      per `identity-and-keys.md` §10; provenance-only edits do NOT perturb the
      hash; spec edits DO.
   2. Cross-repo dep refs resolve only when namespace+repo+name triple matches
      workspace; mismatch ⇒ `ErrDependencyMissing` carrying both endpoints.
   3. Inference is `recover()`-safe — failures emit warn-severity
      `ErrInferenceFailed` and skip rather than panic; gated by
      `intent.catalog.inference.{packageJson,dockerfile,terraform,helm,readme}`.
   4. `deploy-after` cycle = error always; `calls` cycle = warn (default) /
      error (strict).
   5. Errors-typed surface (`ErrDuplicateComponent`, `ErrDependencyMissing`,
      `ErrCycle`, `ErrInferenceFailed`) with `errors.Is`/`errors.As` compat.
|- Coverage on main: `internal/catalogresolve` **90.2%** (+0.2pp headroom over
   PR-1's exact 90.0%); Phase 2 floors held byte-for-byte (catalogmodel 91.1%,
   sourcectx 91.1%, Sanitize* 100%); Phase 1 floors held (statestore 95.7%,
   revision 90.3%, executionstate 90.0%). Determinism stress
   `go test -count=10 -race ./internal/catalogresolve/...` zero failures.

## Task 0027 (Verifier pass for PR #171)

|- Agent: Verifier
|- Prompt: `ai/tasks/task-0027-verifier.md`
|- Status: **verified PASS and merged** (2026-05-31T08:36:04Z)
|- Verifying: PR #171 (`task-0026-catalogresolve-c2-pr2` @ `9c65e7c`)
|- Implementation: PR **#171** squash-merged → main commit `74b88e0`. Branch deleted.
|- PR CI: `Orun Plan` SUCCESS (run `26706854744`), `Harness dry-run guard`
   SUCCESS (run `26706854741`), `test` SUCCESS (run `26706854770`). All
   required checks green; mergeable MERGEABLE / mergeStateStatus CLEAN at
   merge time.
|- Local checks (all exit 0): `go build ./...`, `go vet ./...`,
   `make verify-generated`, `go test -race -count=1 ./...`,
   `go test -race -count=10 ./internal/catalogresolve/...` (zero flake),
   `make test-state-redesign`, `kiox -- orun validate / plan --changed /
   run --dry-run`. Coverage measurements match implementer report
   byte-for-byte.
|- Reports: verifier at `ai/reports/task-0027-verifier.md`.
|- Objective: validate PR #171 against the Verifier Standard in
   `agents/orchestrator.md` and the Milestone C2 "done when" criteria in
   `specs/orun-component-catalog/implementation-plan.md` §C2; on PASS merge
   per the Verifier Merge Protocol and close Milestone C2.
|- Scope boundary: verification only; no production-code edits.
|- Durable outcome on main: Milestone C2 ✅ closed. C3 (`CatalogSnapshot` +
   graph builder + `catalogHash`) opens as Task 0028. PR #171 squash-merged
   at `74b88e0`; branch deleted; main fast-forwarded.

## Task 0028 (C3 implementer — `CatalogSnapshot` + graph builder + `catalogHash`)

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0028.md`
|- Status: implemented; PR #172 opened on branch `task-0028-catalog-c3-snapshot-graph` (head `ffb5ee9`); implementer report `ai/reports/task-0028-implementer.md`. **Awaiting Task 0029 verifier.** Coverage claim: `internal/catalogresolve` 90.2 → 90.9.
|- Milestone: **C3** (per `specs/orun-component-catalog/implementation-plan.md` §C3) — single PR
|- Active spec sections: `resolution-pipeline.md` §1 stages 11–13 + §7 (determinism); `identity-and-keys.md` §9 (catalogHash inputs) + §3 (CatalogSnapshotKey shape); `data-model.md` §2 (CatalogSnapshot) + §4 (CatalogGraph); `test-plan.md` §1 + T-IDK-1
|- Objective: Add post-resolution stages (graph build / catalogHash / snapshot
   assembly) on top of the existing `Resolve` pipeline. Export
   `BuildCatalog(ctx, opts, ResolverInputs) (*CatalogView,
   []ValidationIssue, error)` returning a deterministic
   `(*CatalogSnapshot, []*CatalogGraph)` view with `summary.*` counts from
   sorted distinct collections, byte-stable graph files, and `catalogHash`
   per `identity-and-keys.md` §9.
|- Scope boundary:
   1. New files only in `internal/catalogresolve/`: `graph.go`,
      `catalog_hash.go`, `catalog_snapshot.go` (+ test files).
   2. `ResolverInputs` struct caller-supplied (no invented values for
      `authoritative`, `preview`, `sourceSnapshotKey`, `catalogInputHash`,
      `headRevision`, `treeHash`, `workingTree`, `createdAt`).
   3. Five `CatalogGraph` siblings — `dependencies`, `systems`, `apis`,
      `resources`, `owners`. Sorted nodes (by `key`) and edges (by
      `(from, to, type, optional)`).
   4. `catalogHash` inputs: `(catalogInputHash, sorted (componentKey,
      manifestHash) pairs, canonical encoding of each CatalogGraph in
      fixed order, resolver.resolverVersion)` per §9 verbatim.
|- Non-goals: any FS writes; `internal/catalogstore`; CLI surface;
   `orun plan` / `orun run` integration; `ComponentHistoryEvent`;
   `internal/catalogdiff`; `internal/catalogsync`; edits to Phase 1
   packages; edits to existing files in `internal/catalogresolve/`
   beyond strict wiring (prefer additive sibling files — convention
   adopted in C2 PR-1).
|- Acceptance:
   1. T-IDK-1 (1000 random orderings of input bundle ⇒ identical
      `catalogHash`) green via `pgregory.net/rapid`.
   2. `metadata.owner` edit changes `catalogHash` and `manifestHash`.
   3. Provenance-only (`resolution.inheritedFrom`) edit does NOT change
      `manifestHash` AND does NOT change `catalogHash` (T-IDK-2 carries
      forward into C3 layer).
   4. Two consecutive `BuildCatalog` calls produce byte-identical encoded
      `(*CatalogSnapshot, []*CatalogGraph)`.
   5. `summary.*` counts equal sorted-distinct enumeration of resolved
      manifest fields (`components`, `systems`, `apis`, `resources`,
      `owners`, `domains`).
   6. `catalogSnapshotKey` regex-matches `^cat-[a-f0-9]{6,16}$` and
      passes `catalogmodel.ValidateCatalogSnapshotKey`.
   7. Coverage: `internal/catalogresolve` ≥ 90.2 % (no regression);
      Phase 2 floors held; Phase 1 floors held byte-for-byte.
   8. `make verify-generated` green; full `go test ./... -race` green.
|- Expected outcome: single squash-merged PR closing Milestone C3.
   `BuildCatalog` becomes the deterministic data-only entry point that
   C4 (`internal/catalogstore` Writer) consumes verbatim.

## Task 0029 (C3 verifier — PR #172)

|- Agent: Verifier
|- Prompt: `ai/tasks/task-0029-verifier.md`
|- Status: ✅ verified PASS on 2026-05-31. Squash-merged PR #172 at `75082ca`; branch `task-0028-catalog-c3-snapshot-graph` deleted; `main` fast-forwarded.
|- Implementation: PR #172 (squash `75082ca`), branch `task-0028-catalog-c3-snapshot-graph`, head `ffb5ee9` pre-merge.
|- PR CI: `Orun Plan` SUCCESS (run 26708921448), `Harness dry-run guard` SUCCESS (run 26708921463), `state-redesign-tests / test` SUCCESS (run 26708921479). 3/3 required, branch CLEAN/MERGEABLE pre-merge.
|- Reports: implementer at `ai/reports/task-0028-implementer.md`; verifier at `ai/reports/task-0029-verifier.md`.
|- Objective: validate PR #172 against C3 acceptance, `resolution-pipeline.md` §1 stages 11–13, `identity-and-keys.md` §3 + §9, and `data-model.md` §2 + §4; PASS-and-merge or FAIL-and-block per Verifier Merge Protocol.
|- Scope boundary: 7 files changed (+1273 / -0); three new sibling files in `internal/catalogresolve/` (`graph.go`, `catalog_hash.go`, `catalog_snapshot.go`) + their test siblings + the implementer report. **Zero edits to existing source files.** Bounded as scoped.
|- Durable outcome on main: Milestone C3 ✅ closed. `BuildCatalog(ctx, opts, ResolverInputs) (*CatalogView, []ValidationIssue, error)` is the deterministic data-only entry point that C4 (`internal/catalogstore` Writer) will consume verbatim. T-IDK-1 (rapid 1000) green; deterministic backstops for owner-edit propagation, provenance-only stability (`manifestHash` AND `catalogHash` both invariant under `resolution.inheritedFrom` changes), two-call byte-identical determinism, summary sorted-distinct counts, `^cat-[a-f0-9]{6,16}$` snapshot key shape, and `ErrResolverInputsIncomplete` (extractable via `IsResolverInputsIncomplete`) all green. Source-block back-fill happens AFTER `manifestHash` is finalised at C2 stage 10 — `hash.go` excludes the entire Source block from the hashed payload, so back-fill is invariant-safe by construction. Coverage on main: catalogresolve **90.9 %** (90.2 → 90.9, +0.7 pp); catalogmodel 91.1, sourcectx 91.1, Sanitize* 100; Phase 1 floors held byte-for-byte (statestore 95.7, revision 90.3, executionstate 90.0).
|- Risk note: C3 trusts the caller to compute `Authoritative`/`Preview` correctly (no zero-value sentinel for booleans). C4 writer is the next guardrail (`authoritative=true` ⇒ `workingTree=clean` per data-model §2). Minor non-blocking spec wording mismatch noted in verifier report (data-model §4 / resolution-pipeline §7 describe node ordering as `(kind, key, type)`; implementer + prompt sort by `key` alone — functionally identical because nodes sharing a key always share a kind, but worth a future editorial pass).

## Task 0030 (C4 PR-1 implementer — `internal/catalogstore` paths + body writer)

|- Agent: Implementer
|- Prompt: `ai/tasks/task-0030.md`
|- Status: ⏳ scoped 2026-05-31 (cycle 4). Awaiting implementer pass.
|- Milestone: C4 PR-1 of an expected 2–3 PR split per `implementation-plan.md` §C4.
|- Branch: `task-0030-catalogstore-c4-pr1` (to be created).
|- Objective: introduce `internal/catalogstore` with `paths.go` (every helper from `catalog-store.md` §2 plus `Validate*` siblings) and a partial `writer.go` covering write-order steps A and B (Source body, manifests in §2 path order, graphs in fixed `dependencies, systems, apis, resources, owners` order, catalog doc, local indexes via `Write`). Public `Writer` / `Resolver` / `Store` interface decls and `New(state statestore.StateStore) Store` ship in this PR exactly as `catalog-store.md` §1 spells them; not-yet-implemented methods (`WriteRefs`, `WriteGlobalIndexes`, `AppendComponentEvent`, all `Resolver` methods) return a typed `ErrNotImplemented` so PR-2 / PR-3 fill bodies without widening the surface. Errors declared this PR: `ErrSourceMismatch`, `ErrCatalogMismatch`, `ErrManifestMismatch`, `ErrInputsInconsistent` (pre-flight cross-reference guard rejecting mismatched `sourceSnapshotKey`/`catalogSnapshotKey` linkage between `src`, `cat`, and `manifests` BEFORE issuing any write).
|- Files allowed: `internal/catalogstore/{paths,writer,errors,store,doc}.go` and `_test.go` siblings only.
|- Files forbidden: `internal/catalogstore/{refs,indexes,resolver}.go` (PR-2 / PR-3 territory) and any change to Phase 1 packages, `internal/catalogresolve`, `internal/catalogmodel`, `internal/sourcectx`, `cmd/orun/`, `go.mod`/`go.sum`.
|- Acceptance:
   1. Every path helper from §2 exists with `Validate*` sibling; raw `path.Join` of caller-supplied keys forbidden outside helpers.
   2. `WriteSourceSnapshot` idempotent on byte-identical re-write; mismatch → `ErrSourceMismatch` (preserves `errors.Is(statestore.ErrExists)`).
   3. `WriteCatalogSnapshot` calls in observable order B.1 manifests → B.2 graphs (fixed) → B.3 catalog doc → B.4 local indexes; pre-flight `ErrInputsInconsistent` issues NO writes when `src`/`cat`/manifest keys disagree.
   4. Stub methods return `ErrNotImplemented` (or panic per documented policy); covered by smoke tests.
   5. Canonical-JSON encoder used for every body handed to `statestore`; `encoding/json` defaults forbidden for hashed/persisted payloads.
   6. Coverage: `internal/catalogstore` ≥ 90 %; Phase 1 floors held byte-for-byte (statestore 95.7, revision 90.3, executionstate 90.0); Phase 2 floors held (catalogmodel 91.1, sourcectx 91.1, catalogresolve 90.9).
   7. `make verify-generated` clean; `go test ./... -race` green.
   8. PR titled `catalog: C4 PR-1 — internal/catalogstore paths + body writer (Task 0030)` opened on `task-0030-catalogstore-c4-pr1`; PR body lists every file added, the `internal/catalogstore` cover output, and held floors.
|- Non-goals: `WriteRefs`, `WriteGlobalIndexes`, `AppendComponentEvent`, `Resolver.*`, `RebuildIndexes`, `-x<n>` collision-suffix logic, `stateCompatibilityWrites` mirror writes, CLI changes, plan/run integration.
|- Expected outcome: single PR landing the path layer plus the body-write half of `Writer`, with the public surface of `Writer`/`Resolver`/`Store` finalised so PR-2 (refs + indexes) and PR-3 (resolver + fallback chain) only fill bodies.

## Task 0030 — UPDATE (cycle 5)

|- Status: 🟢 implementer-pass on 2026-05-31. PR #173 opened on `task-0030-catalogstore-c4-pr1` (head `7fec059`); CI 4/4 SUCCESS, MERGEABLE/CLEAN. Implementer self-report shipped on the branch at `reports/task-0030-catalogstore-c4-pr1.md` (note: canonical path was `ai/reports/task-0030-implementer.md` per task spec — verifier to either move or accept).
|- Surface delivered: `internal/catalogstore/{paths.go, paths_test.go, writer.go, writer_test.go, errors.go, store.go, store_test.go, doc.go}`. Path layer covers every helper from `catalog-store.md` §2 (sources, catalogs, components, refs, local indexes, global indexes, history events) plus `Validate*` siblings (return `(string, error)`, no panics). Mismatch sentinels `Err{Source,Catalog,Manifest}Mismatch` double-wrap `statestore.ErrExists` so `errors.Is` succeeds against both. `ErrInputsInconsistent` pre-flight rejects mismatched (src, cat, manifests) tuples BEFORE any write. Writer ships steps A and B.1→B.2→B.3→B.4 in fixed order; graph order driven by `CatalogGraphKinds()` (not input map iteration); B.4 local indexes via plain `Write` (overwrite-OK). `Writer`/`Resolver`/`Store` interfaces frozen with compile-time assertions on `*store`; deferred methods return `ErrNotImplemented` (test-pinned). Encoder choice: `PrettyEncode` for body writes, `CanonicalEncode` reserved for hashing.
|- Coverage (claimed): `internal/catalogstore` 90.7 %; Phase 1 + Phase 2 floors held. Verifier to re-measure.
|- Verifier task: `ai/tasks/task-0031-verifier.md`.

## Task 0031 — Verifier on PR #173 (C4 PR-1)

|- Agent: Verifier
|- Prompt: `ai/tasks/task-0031-verifier.md`
|- Status: ⏳ scoped 2026-05-31 (cycle 5). Awaiting verifier pass.
|- Target PR: #173 on branch `task-0030-catalogstore-c4-pr1`, head `7fec059`. CI green at scope time; MERGEABLE/CLEAN.
|- Verification scope:
   1. PR-boundary audit (only the eight `internal/catalogstore/` files; no edits outside that dir; no `refs.go`/`indexes.go`/`resolver.go`).
   2. Write-order audit (B.1→B.2→B.3→B.4 in code AND spy test; fixed graph order `dependencies, systems, apis, resources, owners`).
   3. Pre-flight `ErrInputsInconsistent` for cat↔src, manifest↔src, manifest↔cat mismatch shapes BEFORE any write (read the test, not the report).
   4. Idempotence + double-wrap `errors.Is` chains to both typed sentinel and `statestore.ErrExists`.
   5. Stub policy pinned by test for `WriteRefs`/`WriteGlobalIndexes`/`AppendComponentEvent` + every `Resolver` method.
   6. Step B.4 uses plain `Write`, NOT CAS (rebuildable per spec).
   7. No raw FS imports under `internal/catalogstore/`.
   8. Coverage: `internal/catalogstore` ≥ 90 %; Phase 1 floors held byte-for-byte; Phase 2 floors held.
   9. Implementer report location decision (move to canonical path or document deviation — verifier's call).
   10. CI green at merge time; `mergeStateStatus: CLEAN`.
|- Verifier-only fixes allowed: report relocation, tiny doc polish required to PASS. Anything more becomes Task 0031.x.
|- On PASS: merge via Verifier Merge Protocol, sync `main`, scope Task 0032 (C4 PR-2 implementer — `refs.go` + `indexes.go` + `AppendComponentEvent` with `seq.lock` retry-up-to-16). On FAIL: leave PR open with blockers; remediation stays in same PR.
|- Expected outcome: PR #173 squash-merged with `internal/catalogstore` paths + body writer landed on `main`, public `Writer`/`Resolver`/`Store` surface frozen for PR-2/PR-3, all coverage floors held.

## Task 0031 — UPDATE (cycle 6)
- ✅ PASS on 2026-05-31. PR #173 squash-merged at `c2d7b9d` and branch
  deleted. All 12 Required Outcomes met under `-race -count=1`. Coverage
  on `internal/catalogstore` re-measured at exactly 90.7 % (matches
  implementer claim). Phase 1 floors held byte-for-byte
  (statestore 95.7, revision 90.3, executionstate 90.0); Phase 2 floors
  held (catalogmodel 91.1, sourcectx 91.1, catalogresolve 90.9).
- Verifier-only fix applied: `git mv reports/task-0030-catalogstore-c4-pr1.md
  ai/reports/task-0030-implementer.md` post-merge to honour the canonical
  report path; legacy `reports/` directory removed.
- Verifier report: `ai/reports/task-0031-verifier.md`.
- Spec proposals: _none_. PR followed `catalog-store.md` §1-§6 exactly.

## Task 0032 — C4 PR-2 implementer (refs + global indexes + events)
- **Agent:** Implementer
- **Prompt:** `ai/tasks/task-0032.md`
- **Scope:** Steps C and D from `catalog-store.md` §3:
  - `WriteRefs` (CompareAndSwap with 16-retry budget per ref;
    `ErrRefStale` on exhaust). Six sub-steps D.1-D.6 (current,
    main if authoritative, branch, pr, latest).
  - `WriteGlobalIndexes` (sources/catalogs plain Write; component
    indexes via CompareAndSwap with merge-retry).
  - `AppendComponentEvent` (seq.lock allocator with 16-retry budget;
    events written via CreateIfAbsent — events immutable per spec).
  - `ErrRefStale` added to errors.go; double-wrap pattern
    (`fmt.Errorf("%w: %w", ErrRefStale, statestore.ErrConflict)`)
    to chain `errors.Is` to both targets.
- **New files:** `internal/catalogstore/{refs.go, refs_test.go,
  indexes.go, indexes_test.go, events.go, events_test.go}`.
- **Edits permitted:** `errors.go` (add sentinel), `store.go`
  (replace 3 stubs), `store_test.go` (drop 3 from
  `TestStubsReturnErrNotImplemented`), `writer_test.go` (extend
  spy `CompareAndSwap` to record `cas:<path>:<oldRev>`, support
  conflict-injection, maintain rev counters).
- **Forbidden:** `resolver.go`, fallback chain, `RebuildIndexes`
  (PR-3); raw FS imports; `go.mod`/`go.sum` churn beyond main.
- **Coverage target:** `internal/catalogstore` ≥ 91 % (floor 90;
  +1 % buffer for PR-3 headroom). Phase 1 + Phase 2 floors held
  byte-for-byte.
- **Done when:** PR opened, CI all-green, MERGEABLE/CLEAN,
  determinism test (same input → byte-identical spy trace), retry
  budget tests inject 16 conflicts and assert `ErrRefStale` chain.

## Historical Notes

- 2026-05-30: roadmap pivoted from TUI cockpit (Phase 3) to orun-state-redesign
  Phase 1. `agents/orchestrator.md` and `specs/orun-state-redesign/` set the
  new authoritative spec pack. `ai/` directory rebuilt by orchestrator under
  the new lineage.

## Task 0032 — UPDATE (cycle 7, implementer pass)
- ✅ Implementer pass complete on 2026-05-31. PR #174 opened on branch
  `task-0032-catalogstore-c4-pr2`. CI 3/3 SUCCESS, MERGEABLE/CLEAN.
  Diff +1602 / -52 across 14 files (all `internal/catalogstore/`).
  Implementer report: `ai/reports/task-0032-implementer.md`.
- Surface delivered: `refs.go` (D.1–D.6), `indexes.go` (C.1–C.3 with
  `mergeComponentGlobalIndex`), `events.go` (`AppendComponentEvent`
  + seq.lock allocator), `errors.go` (`ErrRefStale` sentinel).
- Retry budgets unified at 16 across refs, indexes C.3, and seq.lock
  (spec §5 advises 8 — verifier to adjudicate spec proposal need).
- Stub-pin updated: `TestStubsReturnErrNotImplemented` narrowed to 5
  Resolver methods; new `TestPR2WritersImplemented` pins 3 writers.
- ⚠️ Coverage flag: implementer self-reports `internal/catalogstore`
  at 85.3 % vs Task 0032 acceptance ≥ 91 % with floor 90 % (PR-1 was
  90.7 %). Three-branch adjudication owned by Task 0033 verifier.

## Task 0033 — C4 PR-2 verifier (PR #174)
- **Agent:** Verifier
- **Prompt:** `ai/tasks/task-0033-verifier.md`
- **Status:** scoped and ready to begin (2026-05-31)
- **Objective:** Verify PR #174 against Task 0032's acceptance criteria.
  PASS = merge via Verifier Merge Protocol; FAIL = leave PR open with
  blockers (likely coverage-axis remediation).
- **PR boundary:** PR #174 only. Verifier-only commits allowed for
  Outcome 11 path (a) coverage top-up tests, doc polish required to
  PASS, and an optional retry-budget spec proposal.
- **15 Required Outcomes:**
  1. PR-boundary audit (only `internal/catalogstore/` + implementer
     report touched; no `resolver.go`).
  2. `WriteRefs` D.1→D.6 ordering, conditional sub-steps (Source/
     Catalog nil-skip, Branch/PR conditional emission), spy test.
  3. Branch sanitisation via `catalogmodel.SanitizeBranch` (accept
     + reject inputs tested).
  4. `WriteGlobalIndexes` C.1/C.2 plain `Write`, C.3 deterministic
     ascending CAS — read code, not just tests.
  5. `mergeComponentGlobalIndex` per-clause tests (identity-fields,
     Latest/Main freshness, Previews union+sort).
  6. CAS retry budget = 16 at all three sites; exhaustion returns
     `ErrRefStale` chained to `statestore.ErrConflict`; both
     `errors.Is` succeed.
  7. `AppendComponentEvent` seq.lock allocator: `CreateIfAbsent next=2`
     → CAS+1 retry; immutable event body; concurrency test for
     gap-free strictly-increasing indices.
  8. `seq.lock` path location matches PR-1 hypothesis or is documented.
  9. `ErrRefStale` taxonomy: package-level sentinel, multi-target
     `errors.Is` pattern.
  10. Stub-pin tests updated (Resolver-only) AND new
      `TestPR2WritersImplemented` confirms 3 writers do not return
      `ErrNotImplemented`.
  11. **Coverage adjudication (FLAGGED):** ≥91 PASS / 90–91
      PASS-with-note / <90 HARD FAIL → verifier-attached fix
      (preferred) or Task 0033.1 remediation.
  12. Phase 1 (statestore 95.7, revision 90.3, executionstate 90.0)
      and Phase 2 (catalogmodel 91.1, sourcectx 91.1, catalogresolve
      90.9) coverage floors held byte-for-byte.
  13. Static guards: `go vet`, `go build`, `go test ./... -race`,
      `make verify-generated`, no raw FS imports.
  14. CI green at merge time.
  15. kiox / orun guards (no-op acceptable).
- **Spec drift to flag:** retry-budget 8→16. Inline justification +
  advisory wording → one-sentence note typically suffices; only
  escalate to a spec proposal if spec is read as prescriptive.
- **On PASS:** `gh pr merge 174 --squash --delete-branch`,
  fast-forward `main`, write report, scope Task 0034 (C4 PR-3
  implementer — `resolver.go` + fallback chain + `RebuildIndexes`).
- **On FAIL:** leave PR open with blockers; most likely path is a
  coverage-recovery fix (verifier-attached) or remediation task
  Task 0033.1.
- **Expected outcome:** PR #174 merged with `internal/catalogstore`
  Writer surface complete (Steps A/B/C/D), `ErrRefStale` in error
  taxonomy, `internal/catalogstore` coverage ≥ 90 % (≥ 91 % preferred),
  all sibling floors held. C4 PR-3 (resolver) becomes next slot.

## Task 0033 — UPDATE (cycle 7 verifier close)
- ✅ Verifier PASS (path-(a) verifier-attached coverage fix). PR #174
  squash-merged at `73c6e8e1` on 2026-05-31T11:30:39Z. Implementer
  baseline coverage 85.3 % → verifier added 28 focused tests
  (`writer_test.go` extension + new `verifier_coverage_test.go`) →
  final 90.1 % (484/537 statements, +4.8 pp). Cleared 90 % floor in
  the PASS-with-note band (90–91 %).
- Adjacent floors held byte-for-byte: statestore 95.7, revision 90.3,
  executionstate 90.0, catalogmodel 91.1, sourcectx 91.1,
  catalogresolve 90.9.
- Retry-budget 8→16 spec drift documented but not escalated to a
  proposal (advisory spec wording; harmless under single-writer
  Phase 2). Re-evaluate at C9 / Phase 3.
- Verifier report: `ai/reports/task-0033-verifier.md`.

## Task 0034 — C4 PR-3 implementer (Resolver + fallback chain + RebuildIndexes)
- **Agent:** Implementer
- **Prompt:** `ai/tasks/task-0034.md`
- **Status:** scoped and ready to begin (2026-05-31)
- **Objective:** Implement the read side of `internal/catalogstore`
  per `catalog-store.md` §4 (reader fallback) and §8 (index rebuild).
  Wire all five `Resolver` methods on `*store` with the documented
  fallback ladder and add `RebuildIndexes` (one-line interface
  extension) for byte-identical T-STORE-3 rebuild. Closes Milestone C4
  and unblocks C5 (catalog CLI).
- **Scope boundary:** `internal/catalogstore/` only — new
  `resolver.go`, `resolver_test.go`, `rebuild.go`, `rebuild_test.go`;
  one-line `Resolver` interface extension in `store.go`; stub-pin
  update in `store_test.go`. No CLI, no `internal/*` outside
  catalogstore, no spec edits, no new error sentinels beyond §6.
- **Required outcomes:** five Resolver methods + `RebuildIndexes`
  implemented; T-STORE-3 byte-identical rebuild proven by
  scrub-then-rebuild test; reader fallback ladder per §4 preserved
  exactly; no raw FS imports; `errors.Is` chain through
  `ErrCatalogNotFound` / `ErrComponentNotFound` /
  `statestore.ErrNotFound` preserved; ctx cancellation honoured in
  walks; `internal/catalogstore` ≥ 91 % coverage (floor 90 %);
  adjacent floors held byte-for-byte; CI green / MERGEABLE / CLEAN
  before reporting complete; `TestStubsReturnErrNotImplemented`
  reduced to zero entries (or removed).
- **Acceptance:** PR opened, all green CI, MERGEABLE / CLEAN, no
  `[PR]` / TBD placeholders left in report or context files,
  coverage table in implementer report.
- **Expected outcome:** PR opened on
  `task-0034-catalogstore-c4-pr3-resolver`, ready for Task 0035
  verifier. After merge: Milestone C4 closes, Milestone C5 (CLI
  surface) becomes the active milestone.
- **Outcome (2026-05-31):** PR **#175** opened. CI all-green /
  mergeStateStatus CLEAN. NOTE: `test` job flapped red once on an
  `executionstate` coverage measurement (89.6 % vs 90.0 % floor —
  a Phase-1 package PR-3 does not touch); rerun → 90.0 % green.
  **Coverage shortfall:** implementer landed `internal/catalogstore`
  at **88.9 %** — below the 90 % floor / 91 % target (low funcs are
  the new `rebuild.go` bodies, 72–79 %). catalogstore has no CI gate,
  so green CI did not catch it. Adjudication handed to Task 0035
  verifier. Awaiting verification.

## Task 0035 — C4 PR-3 verifier (PR #175)
- **Agent:** Verifier
- **Prompt:** `ai/tasks/task-0035-verifier.md`
- **Status:** scoped and ready to begin (2026-05-31)
- **Objective:** Verify PR #175 (Task 0034 C4 PR-3 read-side Resolver
  + RebuildIndexes) against acceptance criteria + the Verifier
  Standard. Primary focus: adjudicate the `internal/catalogstore`
  88.9 % coverage shortfall via the three-branch policy (mirror Task
  0033 PR-2), inspect the §4 reader fallback ladder, and validate the
  T-STORE-3 byte-identical rebuild proof. Execute the merge protocol.
- **Scope boundary:** verify PR #175 only — no scope expansion. May
  attach focused coverage tests to the PR branch (path (a)) to lift
  `internal/catalogstore` ≥ 90 % before merge. Must NOT touch
  executionstate or any package outside catalogstore.
- **Required outcomes:** coverage re-measured + adjudication branch
  chosen (a/b/c) with final number stated; §4 fallback ladder code
  path verified (refs→walk→typed sentinel; `ErrComponentNotFound` vs
  `ErrCatalogNotFound`; `errors.Is` chain; ctx cancellation; zero
  live `ErrNotImplemented` surfaces); T-STORE-3 byte-identity proven
  across all three index kinds; no-raw-FS guard empty; implementer's
  3 open questions adjudicated; secret review clean; carry-forward
  executionstate floor-flap recorded as R-008.
- **Acceptance:** `Result: PASS` requires PR #175 MERGED, C4 CLOSED
  (zero `ErrNotImplemented` Resolver surfaces, T-STORE-3 green on
  main), adjacent floors held, verifier report filed.
- **Expected outcome:** PASS → PR #175 squash-merged, C4 closes, C5
  (`orun catalog *` CLI) becomes the active milestone; or
  PASS-with-attached-fix → coverage top-up committed to #175 then
  merged; or FAIL → PR stays open, Task 0035.1 implementer-fix scoped.
- **Verifier outcome (2026-05-31):**
  - **Result: PASS (with verifier-attached coverage fix, path a).**
    PR **#175** squash-merged → `main` commit
    `42eace5f7a475bbf48d82b9f1847031e25068c1f` at 2026-05-31T12:51:14Z;
    branch deleted. **Milestone C4 CLOSED.**
  - **Coverage adjudication:** implementer landed
    `internal/catalogstore` at **88.9 %** (below 90 % floor; package
    has no CI gate). Gap concentrated in NEW `rebuild.go` bodies
    (reachable walk/merge/write logic, not defensive returns) →
    path (a). Verifier attached two test files
    (`rebuild_coverage_test.go` 321 L + `resolver_coverage_test.go`
    193 L, 514 insertions, `package catalogstore_test`, `writeRaw`
    helper) on commit `cb563be`, lifting to **90.3 %** (progression
    88.9 → 89.5 → 89.8 → 90.2 → 90.3). Healthy margin above floor to
    avoid the executionstate "flap problem". No production code changed.
  - **§4 fallback ladder:** verified — refs-first → walk-fallback →
    typed-sentinel; `current → latest → main`; `ErrComponentNotFound`
    vs `ErrCatalogNotFound` with intact `errors.Is` chains; ctx
    cancellation honoured; non-`NotFound` read errors surfaced not
    swallowed. Zero live `ErrNotImplemented` Resolver surfaces.
  - **T-STORE-3:** scrub-then-rebuild byte-identity test present and
    green across all three index kinds.
  - **No-raw-FS guard:** `grep` for `os`/`io/ioutil`/`path/filepath`
    in `internal/catalogstore/` empty. Secret review clean.
  - **Adjudications:** RefSelector.Snapshot accept-and-document
    (spec reserves for C8; implementer's `readSourceByKey` wiring is a
    permitted superset). Implementer's 3 open questions resolved
    inline (no proposals): CGI test-helper duplication = harness
    hygiene; stale-index-scrub → C8; corrupt-manifest silent skip =
    acceptable + now covered.
  - **Carry-forward:** executionstate zero-margin floor flapped once
    (89.6 % vs 90.0 %) on the PR #175 CI `test` job — recorded as
    R-008 in `ai/context/open-risks.md`; not fixed in-PR (PR-boundary).
  - **Quality gates on merged tree:** `go vet ./...`, `go build ./...`,
    `go test -race -count=1 ./...`, `make verify-generated` all green;
    adjacent floors held; post-merge main CI (CI, state-redesign-tests,
    conformance) all PASS.
  - **Reports:** implementer `ai/reports/task-0034-implementer.md`;
    verifier `ai/reports/task-0035-verifier.md`.
  - **Next:** Task 0036 = C5 PR-1 implementer (`orun catalog
    refresh|list|describe|refs` per `cli-surface.md`).

## Task 0036 — C5 PR-1 implementer (catalog CLI foundation + refresh + refs)
- **Agent:** Implementer
- **Prompt:** `ai/tasks/task-0036.md`
- **Status:** scoped and ready to begin (2026-05-31)
- **Milestone:** C5 — Catalog CLI surface (first of the suggested 2 C5
  PRs). C4 closed via PR #175; the `internal/catalogstore` read+write
  engine + `catalogresolve.BuildCatalog` + `sourcectx.
  ResolveSourceSnapshot` are all live.
- **Objective:** Ship `cmd/orun/catalog.go` (root + dispatch) and the
  first two subcommands forming one coherent write→read slice:
  `orun catalog refresh` (resolve→build→persist the
  `(SourceSnapshot, CatalogSnapshot)` pair through the live engine;
  idempotent reuse; dirty banner; `--source`/`--catalog-source`/
  `--catalog-snapshot`/`--strict`/`--no-infer`/`--json`/`--sync`; §2
  exit codes) and `orun catalog refs` (enumerate refs + `authoritative`;
  `--json`). Plus shared plumbing reused by every later C5 command:
  `RefSelector` parser, §11 JSON envelope writer (`kind` ∈
  `{CatalogRefreshResult, CatalogRefsResult}`), help fixtures.
- **Two new seams (the leverage):** (1) persistence-bundle assembly
  (`CatalogView` → `CatalogGraphs`/`CatalogLocalIndexes`/
  `GlobalIndexUpdate`/`RefUpdate`) — no helper exists; recommended
  `internal/catalogresolve`, pure + unit-tested. (2) ref enumeration —
  `Resolver` resolves per-selector but cannot LIST; add a typed
  `ListRefs`-style reader over `statestore` dir reads with **no raw FS
  imports in `internal/catalogstore/`** (no-raw-FS lint stays green).
- **Scope boundary:** one PR, additive only. NO `list`/`describe`/`tree`/
  `history`/`validate` (later C5 PRs), no `diff` (C8), no `plan`/`run`
  integration (C6/C7), no `AppendComponentEvent` (C7), no networking.
- **Acceptance:** `go build`/`go vet`/`go test -race -count=1 ./...`/
  `make verify-generated`/`make test-state-redesign` green; floors held
  (catalogstore/catalogresolve/sourcectx ≥ 90 %, any new pkg ≥ 90 %,
  Phase-1 floors byte-for-byte); two-run refresh idempotent + ref
  appears; `--json` envelopes correct; help fixtures gated; PR opened.
- **Expected outcome:** implementer-pass → Task 0037 = C5 PR-1 verifier;
  on merge the C5 PR-2 read surface (`list`/`describe`/...) unlocks.

## Task 0036 — UPDATE (cycle 11, implementer pass — single-pass closure)
- **Status:** ✅ implemented; shipped via PR #176, merged at `96e3bbd`.
- **Implementation:** branch `feat/c5-pr1-catalog-cli`. `cmd/orun/`:
  `catalog.go` (root + subcommand index; §11 envelope writer;
  `parseCatalogSelector` → `catalogstore.RefSelector` shared bridge;
  pure input-builders incl. `shortRepoName`/`computeCatalogAuthoritative`),
  `catalog_refresh.go` (resolve→build→assemble→commit; idempotent reuse;
  dirty banner; authoritative vs preview; §2 exit codes; `--sync` no-op),
  `catalog_refs.go` (enumeration via `ListRefs`), `main.go` (exit-code
  unwrap via `errors.As`). `internal/catalogstore/`: `bundle.go`
  (`AssembleBundle` — pure, deterministic) + `listrefs.go` (`ListRefs`).
- **Seam-home decision (differs from scope hint):** persistence-bundle
  assembly landed in `internal/catalogstore` (NOT catalogresolve) — it must
  return catalogstore types and catalogresolve may not import catalogstore.
  Architecture rule (catalogstore → catalogresolve, never reverse) held.
- **Tests:** `cmd/orun/catalog_test.go` (selector forms + malformed exit-1,
  envelope shape, authoritative matrix, refresh→reused E2E, refs E2E, empty
  store, sync no-op); `internal/catalogstore/seams_test.go` (AssembleBundle
  purity/determinism/scope-carry; ListRefs empty/join/sorted).
- **Validation:** build/vet clean; `-race -count=1` (4 pkgs) PASS;
  `make verify-generated` up-to-date; no-raw-FS grep clean; catalogstore
  coverage 90.2% (floor); 7-scenario manual smoke green.
- **Reports:** `ai/reports/task-0036-implementer.md`.

## Task 0037 — C5 PR-1 verifier (PR #176)
- **Agent:** Verifier (single-pass closure — same session as implementer,
  full-ship-cycle directive)
- **Prompt:** `ai/tasks/task-0037-verifier.md`
- **Objective:** Verify PR #176 (Task 0036 C5 PR-1: catalog refresh/refs +
  shared CLI foundation + two pure seams) against acceptance criteria.
- **Scope boundary:** verify PR #176 only — no expansion to C5 PR-2.
- **Verifier outcome (2026-05-31):**
  - **Result: PASS, no fixes required.** PR **#176** squash-merged → `main`
    commit `96e3bbd` at 2026-05-31T15:17:22Z; branch deleted. **C5 PR-1
    CLOSED.**
  - **Scope:** PR maps to exactly one task; no unrelated churn; no spec
    drift (no `ai/proposals/` entry needed).
  - **Code inspection:** dependency direction clean (catalogstore does NOT
    import catalogresolve — matches are comments only); no raw FS in
    catalogstore non-test files except `paths.go`; `AssembleBundle` pure +
    deterministic; exit-code plumbing correct; no secrets.
  - **Coverage:** `internal/catalogstore` held at **90.2 %** (floor).
    `cmd/orun` 24.1 % (long-standing baseline, no enforced floor) — new
    seams + refresh→refs E2E covered; uncovered arms are text-render +
    io-error sentinels (defensive).
  - **Static guards:** `go build ./...`, `go vet`, `go test -race -count=1`
    (4 pkgs), `make verify-generated`, no-raw-FS grep all green.
  - **CI:** PR CI green on HEAD `88e104d` (run 26716370631 — test, Orun
    Plan, Harness dry-run guard).
  - **Carry-forward to C5 PR-2:** `CatalogLocalIndexes` owner/system/domain/
    type axes still empty (data-model §9 under-specifies; history is C7).
  - **Reports:** implementer `ai/reports/task-0036-implementer.md`;
    verifier `ai/reports/task-0037-verifier.md`.
  - **Next:** Task 0038 = C5 PR-2 implementer (`orun catalog list|describe|
    tree|history|validate`; `diff` stubbed for C8).
