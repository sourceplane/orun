# Task 0144 — Implementer Report

**Agent:** Implementer
**PR:** [#143](https://github.com/sourceplane/orun/pull/143) — *Task 0144: Orun Cockpit TUI Phase 1 foundation*
**Branch:** `impl/task-0144-tui-foundation` → `main`
**Status:** Ready for verifier

---

## Summary

Implemented the Phase 1 foundation of the Orun Cockpit TUI per `.kiro/specs/orun-tui-cockpit/`. The TUI is registered as `orun tui` and is a component-native control plane: it calls Orun internal packages directly through a typed `OrunService` boundary and never shells out to the `orun` binary. The PR delivers the cobra command entry point, the `internal/tui` package scaffold (root Bubble Tea model, keymap, theme, app), the `internal/tui/services` boundary with mock + live implementations, a read-only service slice (LoadWorkspace, ListRuns, single-shot TailLogs), event bridges, view stubs for every mode named in the spec, and focused tests at every layer. Charm dependencies are pinned. Build is clean and the entire `go test ./...` matrix passes.

The minimal Bubble Tea shell loads the workspace asynchronously on init, shows loading/error states, renders a three-panel shell with a status bar, supports global quit/reload/focus-cycle/help, and resizes correctly. Plan Studio, live Run Dashboard, follow-mode logs, command palette execution, and remote-state polling are deferred to Phase 2/3 per the PR boundary in `ai/tasks/task-0144.md`.

---

## Files Changed

**New (TUI implementation)**
- `cmd/orun/command_tui.go` — `tuiCmd` cobra subcommand + `resolveTUIBackend` helper
- `cmd/orun/command_tui_test.go` — registration + remote-state validation tests
- `internal/tui/app.go` — `NewProgram` (AltScreen + MouseCellMotion)
- `internal/tui/model.go` — root Bubble Tea model (Mode/Panel enums, Init/Update/View)
- `internal/tui/model_test.go` — root model lifecycle tests
- `internal/tui/keymap.go` — `GlobalKeyMap` + `DefaultGlobalKeyMap`
- `internal/tui/theme.go` — lipgloss style/color tokens
- `internal/tui/services/orun_service.go` — `OrunService` interface + typed types + `tea.Msg` wrappers
- `internal/tui/services/mock_service.go` — `MockOrunService` with function-field hooks
- `internal/tui/services/live_service.go` — `LiveOrunService` + `LiveServiceConfig` + Phase 2/3 stubs
- `internal/tui/services/workspace_service.go` — `LoadWorkspace`
- `internal/tui/services/history_service.go` — `ListRuns`
- `internal/tui/services/log_service.go` — single-shot `TailLogs`
- `internal/tui/services/orun_service_test.go` — mock service contract
- `internal/tui/services/live_service_test.go` — workspace loading w/ intent fixture
- `internal/tui/views/navigator.go`
- `internal/tui/views/inspector.go`
- `internal/tui/views/browse.go`
- `internal/tui/views/plan_studio.go`
- `internal/tui/views/run_view.go`
- `internal/tui/views/log_explorer.go`
- `internal/tui/views/history.go`
- `internal/tui/views/command_palette.go`
- `internal/tui/events/run_events.go`
- `internal/tui/events/log_tail.go`

**Modified**
- `cmd/orun/commands_root.go` — added `registerTuiCommand(rootCmd)` in `init()`
- `go.mod`, `go.sum` — Charm + `pgregory.net/rapid` deps

**Spec & orchestration sources (also committed in this PR per branch state)**
- `.kiro/specs/orun-tui-cockpit/{requirements,design,tasks}.md`
- `agents/orchestrator.md`
- `ai/tasks/task-0144.md`
- `orun-tui-cockpit.md`

---

## Checks Run

| Command | Result |
| --- | --- |
| `go build ./...` | exit 0 — clean |
| `go test ./internal/tui/... -count=1` | PASS (`internal/tui`, `internal/tui/services`) |
| `go test ./cmd/orun/... -count=1` | PASS |
| `go test ./... -count=1` | PASS (all packages, including `artifactstore/github`, `state`, `statebackend`, etc.) |
| `go run ./cmd/orun --help \| grep -iE 'tui\|cockpit'` | `tui   Open the Orun Cockpit TUI` |
| `go run ./cmd/orun tui --help` | shows command help + flags |
| `env -u ORUN_BACKEND_URL go run ./cmd/orun tui --remote-state` | exit 1, `✕ --remote-state requires --backend-url or ORUN_BACKEND_URL` |
| `grep -RE 'exec\.Command' internal/tui` | no matches |
| `grep -RE '"orun"' internal/tui/services` | no matches |
| `kiox exec -- orun validate --intent intent.yaml` | `✓ Intent is valid` / `✓ All validation passed` |
| `kiox exec -- orun plan --changed --intent intent.yaml --output /tmp/task0144-plan.json` | 2 components × 5 envs → 3 jobs; plan `20141c8dfaa6` |
| `kiox exec -- orun run --plan /tmp/task0144-plan.json --dry-run --runner github-actions` | all 3 jobs simulated ✓ |

---

## TUI Service Boundary Notes

- `internal/tui/services/` reads workspace state through internal packages only:
  - `internal/discovery.FindIntentFile` to locate `intent.yaml`
  - `internal/loader` + `internal/normalize` to parse and normalize the intent
  - `internal/state.Store.ListPlans` for plan inventory
  - `internal/state.Store.ListExecutions` for run history
- `LiveOrunService.TailLogs` reads local `.orun/executions/{execID}/logs/{jobID}/` files. `Follow=true` returns an explicit error in Phase 1 (no fsnotify watcher yet) rather than silently degrading. `RemoteState=true` also returns an explicit error — Phase 3 will add the remote backend path.
- `LiveOrunService.ListRuns` returns an explicit error when `RemoteState=true`. We do not fake remote behavior.
- `GeneratePlan` / `RunPlan` / `Describe` are explicit `errNotImplemented` stubs — Phase 2 / Phase 3 work.
- No `exec.Command` and no `"orun"` string literal anywhere under `internal/tui/`. Verified with grep (see checks table).

---

## Remote-State Validation Evidence

```
$ env -u ORUN_BACKEND_URL go run ./cmd/orun tui --remote-state
✕ --remote-state requires --backend-url or ORUN_BACKEND_URL
exit status 1
```

Validation runs inside `resolveTUIBackend` (called from `tuiCmd.RunE`) *before* `tea.NewProgram(...).Run()`, so an invalid remote-state config never reaches Bubble Tea.

---

## Assumptions

- The Phase 1 PR may show stub Plan Studio / Run / Log Explorer / Command Palette views that render placeholder text. The spec phases these for later PRs (`tasks.md` Phase 2/3).
- The minimal Bubble Tea shell uses tab focus cycling and ctrl+r reload as the only navigation/mutation surface in Phase 1; Browse filters, dependency tree, and component actions are intentionally deferred.
- Charm versions used: `bubbletea v1.3.5`, `bubbles v0.21.0`, `lipgloss v1.1.0`. These are current stable releases and compile cleanly under the repo's Go toolchain (1.25.6 darwin/arm64).

---

## Spec Proposals

**`rapid` module path correction** — `design.md` Testing Strategy lists `github.com/flyingmutant/rapid`. That is the legacy GitHub mirror; the canonical Go module path is `pgregory.net/rapid` (same project, same author Grigory Petrov). `go get github.com/flyingmutant/rapid` fails with a module-path mismatch and cannot be used. This PR uses `pgregory.net/rapid v1.1.0`. Recommend updating the spec to reflect the canonical import path. No property-based tests are introduced in this PR (per spec phasing); the dep is added so Phase 2/3 tests can import it without a follow-up `go.mod` change.

A formal `ai/proposals/task-0144-spec-update.md` can be authored if the verifier wants the change recorded outside this report.

---

## Risks / Follow-ups

- **Phase 2 GeneratePlan implementation** must wire `internal/planner` through `LiveOrunService.GeneratePlan` and add Plan Studio interactivity. The current stub returns `errNotImplemented` so any accidental call surface in the UI fails loudly rather than silently.
- **Phase 3 follow-mode log tailing** needs an fsnotify or polling watcher and a cancellable `tea.Cmd` loop. The current `TailLogs` is single-shot only and rejects `Follow=true` explicitly.
- **Remote backend integration** (`ListRuns`, eventual `TailLogs`, plan/run streaming) requires the `--remote-state` path to instantiate `statebackend.Backend` and wire it into `LiveServiceConfig.Backend`. The flag plumbing and validation are already in place; the backend client construction is the Phase 3 work.
- **Property-based tests** for normalize/plan determinism are now possible via `pgregory.net/rapid`; not added in this PR.
- **PR #142** remains open and un-merged per the orchestrator's explicit direction. This PR does not touch it.
