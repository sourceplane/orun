# Current Roadmap Position

## Active Spec
`specs/orun-state-redesign/` (Phase 1, local-only) — trigger-first revision-first
local state model. See `specs/orun-state-redesign/README.md` for the index and
read order.

## Active Milestone
**M5 — CLI rewire.** M4 fully closed: PR-A (`#159` → `ed48633`, model + writer
+ resolver) and PR-B (`#160` → `d51e828`, bridge + EXDEV fallback) both
verified PASS and merged. **Next: Task 0016 = M5.a implementer** (`orun plan`
rewire — always resolve trigger, always write revision-first layout, preserve
`-o`, write compat aliases, emit new summary block).

## Last Completed Implementer/Verifier (0014 / 0015 — M4 PR-B)
- Implementer Task 0014 → PR **#160** on `impl/task-0014-m4-executionstate-prb`
  @ `8ad4c5c` (parent `29087f0` "feat(executionstate): land M4 PR-B — bridge.go
  + EXDEV fallback", child `8ad4c5c` "docs(ai): file implementer report …").
- Verifier Task 0015 → PR #160 squash-merged to `main` as `d51e828`
  "Task 0014: M4 PR-B — internal/executionstate bridge + EXDEV fallback (#160)".
- Required CI both SUCCESS at log level on final head SHA (after verifier-side
  commit `f94730e`):
  - `CI / Orun Plan` — run `26677835038`.
  - `orun remote-state conformance / Harness dry-run guard` — run `26677835039`.
- Diff stat (5 files, +1370):
  - new: `internal/executionstate/bridge.go` (423 lines), `bridge_test.go` (767 lines, 19 test functions).
  - additive: `internal/statestore/paths.go` (`LegacyExecutionFilePath`, `ExecutionFilePath` — 18 lines), `paths_test.go` (16 lines).
  - artifact: `ai/reports/task-0014-implementer.md`.
- Coverage: `internal/executionstate` **90.0 %** (exact floor held);
  `internal/statestore` **95.7 %** (lifted from 95.4 %); `internal/revision`
  90.4 % (unchanged).
- Implementer report: `ai/reports/task-0014-implementer.md`. Verifier report:
  `ai/reports/task-0015-verifier.md`.

## Past Completed (0012 / 0013 — M4 PR-A)
PR #159 → `ed48633` "feat(executionstate): land M4 PR-A — internal/executionstate model + writer + resolver". Verified PASS by Task 0013 on 2026-05-30.

## Current Task (0016 — M5.a `orun plan` rewire Implementer, EMITTED 2026-05-30)
- Prompt: `ai/tasks/task-0016.md` (emitted).
- Branch (to be created from `main` @ `d51e828`):
  `impl/task-0016-m5a-orun-plan-rewire`.
- Objective: rewire `orun plan` to always resolve trigger, always write the
  revision-first layout, preserve `-o`, write compat aliases, and emit the new
  summary block per `implementation-plan.md` §M5.
- Scope boundary: `cmd/orun/plan.go` and friends + integration with
  `internal/triggerctx`, `internal/revision`, `internal/executionstate.Bridge`.
  EXCLUDES `orun run` rewire (M5.b), `orun status/logs/describe` (M5.c),
  hidden `orun state migrate` (M5.d).
- Acceptance: `orun plan` writes revisions-first layout end-to-end against
  `examples/intent.yaml`; compat alias paths populated; trigger resolution
  goes through `internal/triggerctx` not raw env reads; new summary block
  emitted; integration tests cover the rewire; coverage gates preserved.

## Repo Checkpoint

| Attribute | Value |
|---|---|
| Branch (local checkout) | `main` (clean post-Task-0015 merge) |
| `main` tip | `d51e828` — Task 0014: M4 PR-B — internal/executionstate bridge + EXDEV fallback (#160) |
| Open PRs (state-redesign lineage) | none (PR #160 merged) |
| Repo health | 🟢 Green — M4 fully closed; M5 awaiting Task 0016 emission |
| Last verified | 2026-05-30 (Task 0015, PR #160) |
| Active milestone | M5 (CLI rewire) — awaiting Task 0016 (M5.a `orun plan`) implementer |
| Tasks completed | 0001, 0002, 0003, 0004, 0005, 0007, 0008, 0009, 0010, 0011, 0012, 0013, 0014, 0015 (14 total) |
| Current task | **0016** (M5.a implementer — emitted 2026-05-30) |

## Roadmap (M0 → M6)
1. ✅ **M0 Foundation** — landed on main at `4ea1980` (PR #152).
2. ✅ **M1 `internal/triggerctx`** — landed on main at `db342dd` (PR #153).
3. ✅ **M2 `internal/statestore`** — closed at PR #156 (`cd8b3e8`, 2026-05-30).
4. ✅ **M3 `internal/revision`** — closed at PR #158 (`bfc2ae6`, 2026-05-30).
5. ✅ **M4 `internal/executionstate` + runner bridge** — closed.
   - ✅ PR-A — model + writer + resolver (PR #159 → `ed48633`).
   - ✅ PR-B — bridge + EXDEV fallback (PR #160 → `d51e828`).
6. **M5 CLI rewire** ← current. Sub-tasks: M5.a `orun plan` (Task 0016), M5.b `orun run` + bridge wiring, M5.c `orun status/logs/describe/get plans`, M5.d hidden `orun state migrate`.
7. M6 End-to-end + property gates

## Next Task After 0016 (proposed)
**Task 0017 — M5.a verifier.** Verifies the `orun plan` rewire against the
M5 "Done when" checklist (revision-first writes, compat aliases populated,
trigger resolution routed through `triggerctx`, new summary block, integration
coverage). On PASS + merge, Task 0018 = M5.b (`orun run` rewire + bridge
wiring) implementer becomes the next emission.

## Known Spec Drift / Open Questions
- **`bridge-mirror-failed` payload schema not pinned in `data-model.md` §9**
  (Task 0015 carry-forward). PR-B fixed the schema in code:
  `{executionKey, revisionKey, legacyExecId, artifact, stage, mode, error}`
  with `stage ∈ {read-source, read-dest, translate-dest, mkdir-dest,
  remove-dest, link, copy}`. Schema is well-formed and additive-friendly.
  Pin in §9 during M5.b runner wiring before any second consumer (metrics,
  `orun status`) lands.
- **`MirrorMode` trinary surface** (Task 0015 adjudicated, accepted with Risk
  Note). `MirrorModeAuto` / `MirrorModeHardlink` / `MirrorModeCopy`. Auto is
  zero value matching §M4 verbatim; Hardlink supports drift detection;
  Copy pre-positions remote drivers. Renaming is non-breaking source-level.
  Reconsider when M5/M6 remote-driver Phase 2 wiring picks the right name.
- **`MirrorRunnerOutput` has no production callers until M5.b.** Resolver
  legacy-fallback (PR-A) carries convergence burden in the meantime.
- **`MirrorModeHardlink` is currently a test/drift-detection mode.** If no
  production caller emerges by M6, fold into a debug flag.
- **`emitFailure` is best-effort** — events-dir-unwritable failures are
  silently dropped. M5+ should add stderr/metric fallback.
- **Event-sequence retry budget of 32** is acceptable for single-writer
  Phase 1; re-evaluate when remote drivers come online.
- **Manifest required for `UpdateLatestExecutionSummary`** (Task 0013
  carry-forward). Pin normatively in `data-model.md` §4 via proposal when
  M5 needs the option to skip the manifest step.
- **Legacy-execution literal defaults** (Task 0013 carry-forward).
  Pin literals in compat §4 when migration command (compat §5) lands.
- **`internal/executionstate` coverage at 90.0 % exact floor.** Carry-
  forward risk: small refactors deleting covered branches could trip the
  gate.
- **Half-shipped delivery anti-pattern.** Task 0007 first observed; the
  explicit `gh pr list --head` check has shipped every prompt since
  Task 0010 — clean record on Tasks 0010/0012/0014.
