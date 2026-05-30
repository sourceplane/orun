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

## Historical Notes

- 2026-05-30: roadmap pivoted from TUI cockpit (Phase 3) to orun-state-redesign
  Phase 1. `agents/orchestrator.md` and `specs/orun-state-redesign/` set the
  new authoritative spec pack. `ai/` directory rebuilt by orchestrator under
  the new lineage.
