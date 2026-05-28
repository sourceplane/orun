# Current Orchestration Context

Last updated: 2026-05-29 (Task 0144.1 scoped — verify PR #143 TUI foundation)

## Repo Reality

- Local branch observed during orchestration: `impl/task-0144-tui-foundation` at `19627c2` (`docs: add task-0144 implementer report (PR #143)`), tracking `origin/impl/task-0144-tui-foundation`.
- Open PRs:
  - **#143** (`impl/task-0144-tui-foundation` → `main`, title `Task 0144: Orun Cockpit TUI Phase 1 foundation`) — ready for verifier. `mergeStateStatus = CLEAN`; CI rollup shows `Orun Plan` SUCCESS and `Harness dry-run guard` SUCCESS, with downstream matrix jobs skipped as expected for the plan shape.
  - **#142** (`happy-patch-113`, title `chore: update happy-patch-113`) — previously verified FAIL and remains open/dirty. It is out of scope for Task 0144.1.
- Repo health: **yellow** until PR #143 is verified/merged or failed with blockers documented. PR #142 remains a separate unresolved open-risk item.

## Last Completed Task (0141.1)

Task 0141 verified PASS and merged via PR #141 at `1ebcb46`. `orun github runs --details` now downloads manifest-only data for each Orun shard and prints Level 2 detail: role, exec-id, status, job, component, environment. Default `orun github runs` remains Level 1 (no downloads). GitHub Artifacts Requirement 11 satisfied.

## User-Directed Roadmap Pivot

The user explicitly instructed the orchestrator to act from `agents/orchestrator.md` and to produce new tasks or verifiers as suited. The current repo state shows the implementer already followed a pivot from the earlier PR #142 cleanup path into Orun TUI cockpit work:

- Task 0144 prompt exists at `ai/tasks/task-0144.md`.
- Implementer report exists at `ai/reports/task-0144-implementer.md`.
- PR #143 is open and claims Task 0144 complete.

Given the implementer report and green check rollup, the correct next orchestration action is a **Verifier** task for PR #143, not a new implementation task.

## Current Task (0144.1 — Verifier)

Prompt: `ai/tasks/task-0144-verifier.md`

Objective: verify PR #143 against Task 0144, the TUI cockpit spec pack, and `agents/orchestrator.md` Verifier Standard. Merge only on PASS plus acceptable local validation and CI log inspection; otherwise leave PR #143 open with precise blockers.

### PR Boundary To Verify

In scope:

- `cmd/orun/command_tui.go` and `cmd/orun/commands_root.go` for `orun tui` registration.
- Charm stack dependencies in `go.mod` / `go.sum`.
- `internal/tui/**` Phase 1 foundation: app/model/keymap/theme, service boundary, mock/live service, read-only LoadWorkspace/ListRuns/TailLogs slice, minimal views/events, tests.
- `.kiro/specs/orun-tui-cockpit/**`, `agents/orchestrator.md`, `orun-tui-cockpit.md`, and `ai/tasks/task-0144.md` included by the pivot branch; verifier must decide whether bundling these with implementation is acceptable under the user-directed pivot or should be recorded as a scope concern.
- `ai/reports/task-0144-implementer.md`.

Out of scope:

- No PR #142 repair, closure, or merge.
- No full Browse filters, dependency tree, Plan Studio, Run Dashboard, follow-mode Log Explorer, History replay, command palette execution, remote cockpit polling, plan diff, failure workbench, or explain mode.
- No feature implementation by verifier beyond small verifier-only report/metadata fixes if needed.

### Acceptance Summary

- PR #143 maps exactly to Task 0144 and does not include PR #142 CLI repair files.
- `orun tui --help` is registered and visible.
- `orun tui --remote-state` fails before Bubble Tea launch if no backend URL is configured.
- TUI code compiles and focused tests pass.
- `internal/tui/services` uses Orun internals directly and does not shell out to the `orun` binary.
- Orun validation/plan/dry-run remains healthy for `intent.yaml`.
- CI logs for PR #143 are inspected and support the check rollup.
- Secret safety is verified.
- Spec drift around `github.com/flyingmutant/rapid` vs `pgregory.net/rapid` is documented and either accepted, proposed, or blocked.
- If PASS: PR #143 is merged, local `main` is synced, and repo is clean.

## Current Roadmap Position

Orun TUI cockpit roadmap is active for PR #143 verification. The TUI cockpit spec pack defines four MVP phases:

1. Phase 1 — read-only browse / foundation.
2. Phase 2 — Plan Studio.
3. Phase 3 — Execution Dashboard, Log Explorer, History/Replay.
4. Phase 4 — advanced features: plan diff, failure workbench, explain mode, remote cockpit.

Task 0144 covers a large but coherent Phase 1 foundation slice: command entry point, service seam, read-only workspace/run/log access, minimal Bubble Tea shell, and tests. This is appropriately PR-sized because the service boundary, command registration, dependencies, and root model must land together for later TUI phases to build on a stable seam.

## Next Task After 0144.1

If Task 0144.1 PASSes and PR #143 merges, the next orchestrator cycle should scope one of these, in priority order:

1. **Task 0145 Implementer — TUI Phase 1 read-only Browse completion**: implement full Navigator/Browse/Inspector interactions from `.kiro/specs/orun-tui-cockpit/tasks.md` tasks 7-9, including component table, filters/search, dependency tree, and resource descriptions.
2. **Task 0145 Implementer — Spec cleanup for rapid module path** if the verifier decides the `pgregory.net/rapid` mismatch should be corrected separately before more TUI implementation.
3. Revisit **PR #142 cleanup/closure** only if the user wants the old GitHub CLI UX PR resolved before continuing TUI cockpit work.

If Task 0144.1 FAILs, the next task should be a targeted implementer fix for PR #143 blockers, not new feature work.

## Deferred / Open Risks

- PR #142 remains open, dirty, and previously failed verification. It includes unrelated TUI/spec/process/history files plus a GitHub CLI UX fix. Keep it out of PR #143 verification.
- The implementer report notes a spec/module mismatch: `.kiro/specs/orun-tui-cockpit/tasks.md` names `github.com/flyingmutant/rapid`, but the Go module path used is `pgregory.net/rapid`. Verifier must decide whether this needs a formal proposal/spec edit.
- PR #143 is large because it includes the spec pack, orchestrator doc, architecture brief, and implementation. Verifier should judge whether this is acceptable under the explicit pivot and report rationale.
