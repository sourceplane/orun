# Current Roadmap Position

## Active Spec
`specs/orun-state-redesign/` (Phase 1, local-only) — trigger-first revision-first
local state model. See `specs/orun-state-redesign/README.md` for the index and
read order.

## Active Milestone
**M3 — `internal/revision`**. PR-A landed at PR #157 (`7f1e53d`, verified PASS
2026-05-30). M3 PR-B implementer (Task 0010) shipped on branch
`impl/task-0010-m3-revision-prb` as PR **#158** — OPEN, MERGEABLE, CLEAN, both
required CI checks SUCCESS at log level. **Next: Task 0011 = M3 PR-B verifier.**

## Last Completed Implementer Task (0010 — M3 PR-B)
- Branch: `impl/task-0010-m3-revision-prb` @ `ec74af1` (parent `e538b90` "feat(revision): land M3 PR-B …", child `ec74af1` "docs(ai): task-0010 implementer report")
- PR: **#158** — title `feat(revision): land M3 PR-B — manifest, resolver, compat-mirror, JobCount`
- State: OPEN, MERGEABLE, mergeStateStatus CLEAN, base `main`, head `ec74af1`.
- Required CI both SUCCESS at log level:
  - `CI / Orun Plan` — run `26674136935`, completed 2026-05-30T04:12:02Z.
  - `Harness dry-run guard` — run `26674136948`, completed 2026-05-30T04:11:21Z.
- Diff stat (12 files, +1966 / -22):
  - new: `internal/revision/{errors,legacy,manifest,resolver}.go` and tests `internal/revision/{manifest,resolver,writer_compat,coverage_extra}_test.go`
  - edits: `internal/revision/{model,writer}.go` (added `ManifestKind`; compat-mirror body, `Config.JobCount`, `summaryFromScope` plumbing)
  - artifacts: `ai/tasks/task-0010.md`, `ai/reports/task-0010-implementer.md`
- Coverage: `internal/revision` **90.4 %** (gate ≥ 90 %); `internal/statestore` **96.1 %** (M2 floor preserved).
- Implementer report: `ai/reports/task-0010-implementer.md`.

## Implementer Decisions to Adjudicate (Task 0011)
1. **`JobCount` Option A** — planner-supplied via `Config.JobCount`; `0` means "unknown" (writer does not parse `plan.json`).
2. **Resolver branch 3 fallthrough** — on `ErrNotFound`, regex-matching `rev-…` arg falls through to subsequent branches rather than bubbling up; only returns `ErrAmbiguousArg` if branches 4–7 also fail. Implementer rationale plausible but not in `compatibility-and-migration.md` §3 verbatim.
3. **`latest.json` last-write-wins** — `.orun/plans/latest.json` written via `statestore.Write`, not `CreateIfAbsent`. Implementer cites `compatibility-and-migration.md` §2.

Each must be adjudicated by Task 0011 either inline (Risk Notes) OR via a single `ai/proposals/task-0010-spec-update.md`.

## Repo Checkpoint

| Attribute | Value |
|---|---|
| Branch (local checkout) | `main` (clean post-Task-0009 merge) |
| `main` tip | `7f1e53d` — Task 0007: M3 PR-A — internal/revision model + keys + writer skeleton (#157) |
| Open PRs (state-redesign lineage) | **#158** (Task 0010, M3 PR-B; OPEN, MERGEABLE, CLEAN) |
| Repo health | 🟢 Green — M3 PR-B awaiting verification |
| Last verified | 2026-05-30 (Task 0009, PR #157) |
| Active milestone | M3 (`internal/revision`) — PR-A merged; PR-B awaiting verifier |
| Tasks completed | 0001, 0002, 0003, 0004, 0005, 0007, 0008, 0009, 0010 (9 total) |
| Current task | **0011** (M3 PR-B verifier — emitted) |

## Roadmap (M0 → M6)
1. ✅ **M0 Foundation** — landed on main at `4ea1980` (PR #152).
2. ✅ **M1 `internal/triggerctx`** — landed on main at `db342dd` (PR #153).
3. ✅ **M2 `internal/statestore`** — closed at PR #156 (`cd8b3e8`, 2026-05-30).
4. **M3 `internal/revision`** ← current
   - ✅ PR-A — model + keys + writer skeleton (PR #157 → `7f1e53d`, verified PASS via Task 0009 on 2026-05-30)
   - **PR-B — manifest + resolver + compat-mirror body + JobCount (PR #158, awaiting Task 0011 verifier)**
5. M4 `internal/executionstate` + runner bridge
6. M5 CLI rewire (`orun plan/run/status/logs/describe/get plans` + hidden `state migrate`)
7. M6 End-to-end + property gates

## Next Task After 0011 (proposed)
**Task 0012 — M4 implementer.** Opens `internal/executionstate` + the runner
bridge per `specs/orun-state-redesign/implementation-plan.md` §M4:

- `model.go` — `ExecutionRun`, `RunnerProfile`, `ExecSummary`.
- `writer.go` — `NextExecutionKey`, `SanitizeExecID`, `CreateExecution`,
  `UpdateSnapshot`, `MarkTerminal`. Emits `execution-created` event.
- `bridge.go` — `Bridge{Store, LegacyRoot, MirrorMode}`,
  `MirrorRunnerOutput(ctx, execKey, revKey, legacyExecID)`. Hardlink with copy
  fallback. Failures emit `bridge-mirror-failed` event and return nil.
- `resolver.go` — `ResolveExecution(ctx, store, arg, revHint)`. Legacy
  fallback to `.orun/executions/` scan.

Suggested branch: `impl/task-0012-m4-executionstate-pra`. Suggested PR scope:
1–2 PRs (the bridge is naturally separable from the writer/resolver).
Coverage gate ≥ 90 % on `internal/executionstate`.

If Task 0011 verifier rejects any of the three implementer decisions and files
`ai/proposals/task-0010-spec-update.md`, the orchestrator will accept, revise,
defer, or query the user about it before generating Task 0012 — and may
interpose a small spec-update task between 0011 and 0012.

## Known Spec Drift / Open Questions
- **Resolver branch 3 fallthrough vs `compatibility-and-migration.md` §3.**
  Implementer Task 0010 chose fallthrough on `ErrNotFound` rather than bubble-up.
  Adjudication owed by Task 0011 verifier (accept-and-document inline OR file
  `ai/proposals/task-0010-spec-update.md`).
- **`JobCount=0` ambiguity.** Option A treats `0` as "unknown". `data-model.md`
  §3 `RevSummary` does not normatively distinguish "no jobs" from "unknown".
  Adjudication owed by Task 0011 verifier.
- **`latest.json` `Write` vs `CreateIfAbsent`.** Implementer cites spec §2
  ("last-write-wins is acceptable for this alias"). Verifier confirms vs
  `compatibility-and-migration.md` §2.
- **`stateStoreVersionPath()` helper location** (carried from M3 PR-A). If M5
  migration tooling needs a statestore-side helper, that PR can lift the
  constant up. RESOLVED — defer.
- **Half-shipped delivery anti-pattern.** Task 0007 was the first observed case.
  Task 0010 prompt embedded the explicit `gh pr list --head <branch>` check;
  PR #158 was created on the first delivery cycle. Pattern guard remains in
  every implementer prompt going forward.
