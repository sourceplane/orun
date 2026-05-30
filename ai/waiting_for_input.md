No human input currently requested. Milestone M3 (`internal/revision`) is the
active milestone. M3 PR-A landed on `main` at `7f1e53d` (PR #157, verified
PASS via Task 0009 on 2026-05-30). M3 PR-B implementer (Task 0010) shipped on
branch `impl/task-0010-m3-revision-prb` as PR **#158** — OPEN, MERGEABLE,
mergeStateStatus CLEAN, head SHA `ec74af1`. Both required CI checks SUCCESS
at log level (`CI / Orun Plan` run `26674136935`, `Harness dry-run guard`
run `26674136948`).

Current orchestrator decision (2026-05-30): emit **Task 0011 = M3 PR-B
verifier** at `ai/tasks/task-0011-verifier.md`. Verifier validates Task 0010
against `specs/orun-state-redesign/implementation-plan.md` Milestone M3 "Done
when" criteria (≥ 90 % coverage, all 7 resolver branches with explicit
`ResolveSource` assertions, compat-writes flag exercises both true and false
paths), adjudicates three implementer decisions (Option A for `JobCount` =
planner-supplied, `0` means "unknown"; resolver branch 3 fallthrough on
`ErrNotFound` rather than bubble-up; `.orun/plans/latest.json` written via
`statestore.Write` last-write-wins) — each accepted-with-Risk-Note OR via a
single `ai/proposals/task-0010-spec-update.md`. Verifier inspects both
required CI runs at log level and merges per the Verifier Merge Protocol on
PASS.

Next implementer after Task 0011 (assuming PASS) = **Task 0012 = M4 PR-A
implementer** scoping `internal/executionstate` (`ExecutionRun`,
`RunnerProfile`, `ExecSummary`, `NextExecutionKey`, `SanitizeExecID`,
`CreateExecution`, `UpdateSnapshot`, `MarkTerminal`) and the runner bridge
(`Bridge{Store,LegacyRoot,MirrorMode}`, `MirrorRunnerOutput` hardlink with
copy fallback, `bridge-mirror-failed` event), plus `ResolveExecution` legacy
fallback to `.orun/executions/` scan, per
`specs/orun-state-redesign/implementation-plan.md` §M4. Suggested PR scope
1–2 PRs; the bridge is naturally separable.
