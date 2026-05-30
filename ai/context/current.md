# Current Roadmap Position

## Active Spec
`specs/orun-state-redesign/` (Phase 1, local-only) — trigger-first revision-first
local state model. See `specs/orun-state-redesign/README.md` for the index and
read order.

## Active Milestone
**M5 — CLI rewire.** M4 fully closed; M5.a closed (PR #161 → `7a9c494`); M5.b
closed (PR #162 → `59d06f3`); **M5.c closed** (PR #163 → `73108ee`).
**Next: Task 0020 = M5.d implementer** — hidden `orun state migrate` command
per `compatibility-and-migration.md` §5. Closes M5 and unlocks M6.

## Last Completed Implementer/Verifier (0018 — M5.b)
- Implementer + verifier both Task 0018 (single-pass) → PR **#162** on
  `impl/task-0018-m5b-orun-run-rewire`.
- Squash-merged to `main` as `59d06f3` "M5.b: rewire `orun run` onto the
  revision-first execution path (#162)" on 2026-05-30T13:42:02Z.
  Head SHA at merge: `e5dd580`.
- Required CI both PASS at log level on final head SHA: `CI / Orun Plan` (45 s);
  `Harness dry-run guard` (12 s). 5 matrix legs SKIPPED (empty matrix at M5.b —
  same shape as M5.a #161).
- Diff stat (5 files changed, +757 / -0):
  - new: `cmd/orun/command_run_revision.go` (365 LOC, houses
    `setupRevisionExecution` / `installRevisionHooks` /
    `finalizeRevisionExecution` / `synthesizeRevisionForRun` /
    `printRevisionRunSummary`).
  - new tests: `cmd/orun/command_run_revision_test.go` (298 LOC, 8 tests).
  - modified: `cmd/orun/command_run.go` (register `--revision` flag,
    wire setup/finalize around `r.Run(plan)`),
    `internal/runner/runner.go` (add `RunnerHooks.AfterStateUpdate` fired
    from `updateState` after `SaveState`),
    `specs/orun-state-redesign/data-model.md` (pin §9.1
    `bridge-mirror-failed` event payload schema).
  - artifacts: `ai/reports/task-0018-implementer.md`,
    `ai/reports/task-0018-verifier.md`, plus ai/state.json + ai/context/*
    updates.
- Coverage: `internal/statestore` **95.7 %** (≥95 %); `internal/revision`
  **90.4 %** (≥90 %); `internal/executionstate` **90.0 %** (exact floor held —
  M5.b touched the package only via API consumption).
- Verifier report: `ai/reports/task-0018-verifier.md`.
- Phase 1 reservations honoured (NOT wired): `--persist-revision` flag
  (synthesize-fallback covers the gap), `Reason="rerun"/"retry"/"migration"`
  (only `"direct-run"` emitted from this path).

## Past Completed (0016 — M5.a)
PR #161 → `7a9c494`. `orun plan` rewire onto canonical revision-first layout.
Verified PASS (single-pass closure).

## Past Completed (0014 / 0015 — M4 PR-B)
PR #160 → `d51e828`. Bridge + EXDEV fallback. Verified PASS by Task 0015.

## Past Completed (0012 / 0013 — M4 PR-A)
PR #159 → `ed48633`. Verified PASS by Task 0013.

## Last Completed Implementer/Verifier (0019 — M5.c)
- Task 0019 read-side rewire (`orun status / logs / describe / get plans`) verified PASS and squash-merged to `main` as `73108ee` on 2026-05-30T15:16:16Z via PR **#163**.
- Verifier head SHA: `cb33a8e` (verifier report + spec proposal commit on top of feature `947773d` + housekeeping `fb364f1`).
- Required CI on final head SHA: both PASS at log level. `CI / Orun Plan` SUCCESS — real `orun plan --from-ci github` step rendered Revision `rev-github-pull-request-fb364f1-pbfd83933`, plan artifact uploaded. `Harness dry-run guard` SUCCESS — 30+ `[guard] PASS:` assertions, final `DRY-RUN GUARD PASSED`.
- Coverage gates preserved (zero changes to those packages): statestore 95.7%, revision 90.4%, executionstate 90.0%.
- Verifier walks confirmed: fresh revision-first happy path; legacy-only fallback transparent across all four commands; bridge-mirror-failed single-line stderr warning per execution (dedup on multi-event, silent on malformed body / no events, exit code unchanged); new `--revision` / `--exec-id` / `--all` flags wired on `status` and `logs`; `describe revision|trigger|execution` aliases functional; describe execution prints triplet (revisionKey/executionKey/legacyExecID); `get plans -o json` stable key order with `[]` legacy fallback.
- One non-blocking spec proposal filed: `ai/proposals/task-0019-spec-update.md`. `cli-surface.md` §5.1 documents `describe revision latest` / `describe trigger latest` / `describe trigger <triggerName>` as canonical, but the resolver lacks a `latest`-literal branch and a trigger-name lookup branch (workarounds exist via empty-arg / explicit revision key). Recommendation: fold Option A normalization (`ref==\"latest\" → ref=\"\"`) and Option B trigger-name lookup into M5.d scope.
- Reports: `ai/reports/task-0019-implementer.md`, `ai/reports/task-0019-verifier.md`.

## Current Task (0020 — M5.d hidden `orun state migrate` implementer, NOT YET EMITTED)
- Spec ref: `specs/orun-state-redesign/compatibility-and-migration.md` §5 (legacy `.orun/executions/<id>/` → `revisions/<key>/executions/<execKey>/` backfill).
- Task base branch: `main` at the post-#163-merge tip (`73108ee`).
- Recommended scope nudge from Task 0019 verifier: roll the `describe revision/trigger latest` literal-arg fix + trigger-name lookup branch from `ai/proposals/task-0019-spec-update.md` into this task. Two-line CLI normalization (`ref==\"latest\" → ref=\"\"`) for the literal; resolver-side branch reading `refs/triggers/<name>/latest.json` for trigger-name lookup.
- Closes M5 once merged. Unlocks M6 (E2E + property gates).

## Repo Checkpoint

| Attribute | Value |
|---|---|
| Branch (local checkout) | `main` (clean post-#163 merge) |
| `main` tip | `73108ee` — Task 0019: M5.c — orun read-side commands revision-first rewire (#163) |
| Open PRs (state-redesign lineage) | none — #163 merged on 2026-05-30 |
| Repo health | 🟢 Green — Task 0019 PASS+merged; awaiting Task 0020 (M5.d) implementer emission |
| Last verified | 2026-05-30 (Task 0019, PR #163, merge `73108ee`) |
| Active milestone | M5 (CLI rewire) — M5.a/b/c closed, M5.d pending |
| Tasks completed | 0001, 0002, 0003, 0004, 0005, 0007, 0008, 0009, 0010, 0011, 0012, 0013, 0014, 0015, 0016, 0018, 0019 (17 total) |
| Current task | **0020** (M5.d implementer — to be emitted) |

## Roadmap (M0 → M6)
1. ✅ **M0 Foundation** — landed on main at `4ea1980` (PR #152).
2. ✅ **M1 `internal/triggerctx`** — landed on main at `db342dd` (PR #153).
3. ✅ **M2 `internal/statestore`** — closed at PR #156 (`cd8b3e8`, 2026-05-30).
4. ✅ **M3 `internal/revision`** — closed at PR #158 (`bfc2ae6`, 2026-05-30).
5. ✅ **M4 `internal/executionstate` + runner bridge** — closed.
   - ✅ PR-A — model + writer + resolver (PR #159 → `ed48633`).
   - ✅ PR-B — bridge + EXDEV fallback (PR #160 → `d51e828`).
6. **M5 CLI rewire** ← current. Sub-tasks: ✅ M5.a `orun plan` (Task 0016, PR #161 → `7a9c494`), ✅ M5.b `orun run` + bridge wiring (Task 0018, PR #162 → `59d06f3`), ✅ M5.c `orun status / logs / describe / get` (Task 0019, PR #163 → `73108ee`), M5.d hidden `orun state migrate` (Task 0020, pending).
7. M6 End-to-end + property gates

## Next Task After 0019 (proposed)
**Task 0020 = M5.d implementer** — hidden `orun state migrate` command per
`compatibility-and-migration.md` §5 for legacy `.orun/executions/<id>/` →
`revisions/<key>/executions/<execKey>/` backfill. After M5.d PASS+merge,
M5 closes and M6 (E2E + property gates) opens. Branch base: `main` at
the post-#163-merge tip. Pin literal legacy-execution defaults
(carry-forward from Task 0013) in compat §4 as part of that task.

## Known Spec Drift / Open Questions
- ~~**`bridge-mirror-failed` payload schema not pinned in `data-model.md` §9**~~
  CLOSED in M5.b (§9.1 added; field table matches
  `internal/executionstate.bridgeMirrorFailedPayload` exactly).
- **`MirrorMode` trinary surface** (Task 0015 adjudicated, accepted with Risk
  Note). `MirrorModeAuto` / `MirrorModeHardlink` / `MirrorModeCopy`. Auto is
  zero value matching §M4 verbatim; Hardlink supports drift detection;
  Copy pre-positions remote drivers. Renaming is non-breaking source-level.
  Reconsider when M5/M6 remote-driver Phase 2 wiring picks the right name.
- ~~**`MirrorRunnerOutput` has no production callers until M5.b.**~~ CLOSED in
  M5.b — `cmd/orun/command_run_revision.go::installRevisionHooks` is now the
  first production caller via `RunnerHooks.AfterStateUpdate`. Resolver
  legacy-fallback (PR-A) remains the convergence path for legacy on-disk state.
- **`MirrorModeHardlink` is currently a test/drift-detection mode.** If no
  production caller emerges by M6, fold into a debug flag.
- ~~**`emitFailure` is best-effort** — events-dir-unwritable failures are
  silently dropped.~~ ADDRESSED in M5.c — `cmd/orun/bridge_mirror_warn.go`
  now surfaces dropped events as one-line stderr warnings to the read-command
  audience (filename-based detection, payload-shape-agnostic, dedup per
  execution). Future work: parse `data-model.md` §9.1 fields for richer
  diagnostics.
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
- **NEW (Task 0018 carry-forward): `RunnerHooks.AfterStateUpdate` fires
  bridge mirror synchronously on the runner goroutine.** On slow filesystems
  this could measurably extend per-tick wall time. M5.c may want to move the
  mirror to a buffered channel + dedicated goroutine if real workloads
  regress.
- **Half-shipped delivery anti-pattern.** Task 0007 first observed; the
  explicit `gh pr list --head` check has shipped every prompt since
  Task 0010 — clean record on Tasks 0010/0012/0014/0016/0018.
