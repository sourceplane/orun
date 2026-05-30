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

## Historical Notes

- 2026-05-30: roadmap pivoted from TUI cockpit (Phase 3) to orun-state-redesign
  Phase 1. `agents/orchestrator.md` and `specs/orun-state-redesign/` set the
  new authoritative spec pack. `ai/` directory rebuilt by orchestrator under
  the new lineage.
