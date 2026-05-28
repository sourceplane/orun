# Current Orchestration Context

Last updated: 2026-05-29 (Task 0145 scoped)

## Repo Reality

- Local `main` synced with `origin/main` at `9ac35d3` (`chore(orchestrator): close task-0144.1 verifier loop (PR #143 merged)`). Working tree was clean when Task 0145 was scoped.
- Completed TUI checkpoint: Task 0144.1 verified PASS and merged PR #143. The Orun Cockpit TUI Phase 1 foundation is durable on `main`.
- Open PRs:
  - **#142** (`happy-patch-113`, title `chore: update happy-patch-113`) — OPEN and DIRTY. It previously failed Task 0142 verification and remains the active repo-health risk.
- Repo health: **yellow** until PR #142 is closed/superseded or narrowed and verified. Do not advance to TUI Phase 2 until this open-risk item is resolved.

## Last Completed Task (0144.1)

Task 0144.1 verified PASS and merged PR #143 at merge commit `17d3b58`; orchestration cleanup landed at `9ac35d3`. Verifier report: `ai/reports/task-0144-verifier.md`.

Durable outcomes now on `main`:

- `orun tui` Cobra subcommand registered (`cmd/orun/command_tui.go`, `cmd/orun/commands_root.go`).
- `--remote-state` fails closed (`✕ --remote-state requires --backend-url or ORUN_BACKEND_URL`) before `tea.NewProgram(...).Run()`, with focused command tests.
- `internal/tui` Phase 1 foundation: root Bubble Tea model, Mode/Panel enums, async workspace load, loading/error states, three-panel/status/key-hint rendering, quit/reload/focus/help bindings.
- `internal/tui/services` boundary calls Orun internals directly. No `exec.Command` and no `"orun"` literal under `internal/tui/`.
- Phase 2/3 surfaces (`GeneratePlan`, `RunPlan`, `Describe`, `TailLogs(Follow=true)`, `ListRuns(RemoteState=true)`) remain explicit stubs/errors.
- Charm deps pinned: `bubbletea v1.3.5`, `bubbles v0.21.0`, `lipgloss v1.1.0`. Actual property-test import path is `pgregory.net/rapid v1.1.0`, not the stale GitHub mirror named in the spec.

## Current Task (0145)

Objective: resolve PR #142 before continuing TUI Phase 2. Task 0145 must preserve the useful GitHub CLI UX behavior from PR #142 commit `ddbec4c` in a clean successor PR, then close or explicitly disposition PR #142.

PR boundary:

- In scope: `cmd/orun/command_github.go` changes for `--orun-dir` normalization and `github status` selector flags; focused tests; matching `website/docs/cli/orun-github.md`; optional direct UX note; `ai/reports/task-0145-implementer.md`; PR #142 closure/supersession.
- Out of scope: TUI Phase 2, `.kiro/specs/orun-tui-cockpit/**`, `orun-tui-cockpit.md`, `agents/orchestrator.md`, historical task prompts, stale `ai/waiting_for_input.md`, dummy component triggers, and broader GitHub Artifacts features.

Key constraints:

- Prefer a fresh successor branch from current `main`; PR #142 is DIRTY and contains stale/unrelated commits.
- Preserve `.orun` path compatibility: paths already ending in `.orun` must not have another `.orun` appended.
- Do not commit secrets, signed artifact URLs, or dummy CI trigger labels.
- Implementer must open a real PR and update the report with the real PR number before reporting complete.

Acceptance summary:

- Successor PR diff is narrow and excludes all PR #142 blocker files.
- Unit tests cover `--orun-dir` parent and existing-`.orun` cases plus status selector flag registration.
- Focused Go tests/build pass; Orun validation is N/A if no root `intent.yaml` exists.
- PR #142 is closed as superseded, or a documented permissions/ownership blocker explains why it remains open.

Prompt: `ai/tasks/task-0145.md`.

## Current Roadmap Position

1. ✅ GitHub Artifacts Level 1/2 CLI gaps through Task 0141.1.
2. ✅ Orun Cockpit TUI Phase 1 foundation through Task 0144.1.
3. 🔄 **Task 0145: resolve dirty PR #142 / GitHub CLI UX cleanup.**
4. ⏭️ Task 0145.1 verifier: verify and merge the successor PR if PASS.
5. ⏭️ Task 0146: TUI Cockpit Phase 2 / Plan Studio wiring through `internal/planner` once repo health is green.

## Next Task After 0145

Task 0145.1 (Verifier): verify the Task 0145 successor PR, inspect PR #142 disposition, run focused CLI tests/build, inspect CI logs, and merge only if the PR is narrow and green.

After Task 0145.1 PASS, likely Task 0146 should scope TUI Cockpit Phase 2: implement `GeneratePlan` through `internal/planner`, Plan Studio form/review state, focused property/unit tests, and the one-line rapid import-path spec housekeeping if still needed.

## Open Risks

- PR #142 remains open and dirty until Task 0145 resolves it.
- The old Task 0143 repair prompt is superseded by Task 0145; do not run both in parallel.
- Repo health remains yellow while PR #142 is open/dirty.
