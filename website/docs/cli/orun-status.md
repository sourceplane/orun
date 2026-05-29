---
title: orun status
---

`orun status` shows the execution status of the latest run or a specific execution.

## Usage

```bash
orun status
```

## Common examples

Show the latest execution:

```bash
orun status
```

Show all executions:

```bash
orun status --all
```

Show step-level detail for the latest execution:

```bash
orun status --detailed
```

Show a specific execution:

```bash
orun status --exec-id my-plan-20240601-a1b2c3
```

## Output

`orun status` renders through the shared **cockpit** layer (see
[TUI cockpit architecture](../architecture/tui-cockpit.md) and
the [Cockpit UX](#cockpit-ux) section below). The output is the TUI's
run pane compressed into a single frame — same palette, same glyphs,
same view-model, no drift.

Default view — single execution:

```
▲ orun my-plan
  Plan: sha256-ad6ce · Run: my-plan-20240601-a1b2c3 · State: running · Duration: 2m
  Scope: 7 components · 38 jobs
  Status:   ✓ 22 succeeded · ◐ 4 running · ○ 12 queued
  Progress: ▓▓▓▓▓▓▓▓░░░░░░░░ 58%
  ● api-edge-worker
  │  └─ ◐ deploy           12.0s
  ● platform-shared
  │  └─ ✓ build             8.0s
  ○ web-console
     └─ ○ deploy
```

`--all` view — all executions:

```
▲ orun
  3 runs · 1 running · 1 succeeded · 1 failed

  ◐ my-plan-20240601-a1b2c3   my-plan          4/38     2m       now
  ✓ my-plan-20240531-d4e5f6   my-plan         38/38    4m12s     1d
  ✗ my-plan-20240530-7a8b9c   my-plan         12/38    1m05s     2d
```

`--watch` re-renders this exact frame on a poll loop driven by
`internal/cockpit/watch` — the same loop the TUI subscribes to — and
exits cleanly on a terminal status (`completed` / `failed`).

## Status icons

| Icon | Meaning |
| --- | --- |
| `●` | Component group with at least one running job |
| `◐` | Running job (pulse) |
| `✓` | Completed |
| `✗` | Failed |
| `○` | Pending / queued |
| `↷` | Skipped |
| `▲` | Brand wedge — anchors every cockpit frame |

Glyphs are stable across `NO_COLOR`, CI logs, and the TUI. Colour can
be stripped; the iconography stays.

## Flags

| Flag | Meaning |
| --- | --- |
| `--exec-id` | Show a specific execution by ID |
| `--all` | List all stored executions |
| `--detailed` | Include step-level status in the output |
| `--json` | Output in JSON format |
| `--watch`, `-w` | Continuously refresh until the run reaches a terminal state |
| `--interval` | Refresh interval for `--watch` (default `1s`) |
| `--remote-state` | Fetch status from orun-backend instead of local state |
| `--backend-url` | orun-backend URL for remote state (or set `ORUN_BACKEND_URL`) |

## Remote status

When `--remote-state` is set, `orun status` fetches run and job state from the backend rather than the local `.orun/` store.

```bash
orun status \
  --remote-state \
  --backend-url https://orun-backend.example.com \
  --exec-id gh-12345678-1-a1b2c3
```

The same rendering is used for local and remote state.  `--watch` polls the backend until the run reaches a terminal state (completed or failed).  `--json` returns machine-readable output.

Outside GitHub Actions, remote status uses the local Orun CLI session from `orun auth login` and the backend URL from `--backend-url`, `ORUN_BACKEND_URL`, `intent.yaml`, or `~/.orun/config.yaml`.

Use `orun describe run <id>` for a fuller breakdown including metadata, timing, and job-level errors.

## Cockpit UX

`orun status`, `orun get runs`, `orun logs`, `orun status --watch`, and
`orun tui` all render through the same `internal/cockpit/*` packages:

- `cockpit/style` — palette (violet `#7c3aed` light / `#a78bfa` dark),
  glyphs, separators. CLI ANSI and TUI lipgloss both consume it.
- `cockpit/viewmodel` — pure value objects (`RunView`, `RunListView`,
  `LogsView`) built from `.orun` state.
- `cockpit/render` — surface-agnostic formatters (brand wedge, status
  legend, progress bar, component tree, log groups).
- `cockpit/bridge` — one `Source` interface over local `state.Store`
  *or* the remote `statebackend.Backend`, so `--remote-state` lands the
  same frame as the local path.
- `cockpit/watch` — poll loop emitting `Update{View, Err, Terminal}`.
  Shared by `--watch` and the TUI.

One place to reskin Orun. See
[TUI cockpit architecture](../architecture/tui-cockpit.md) for the
TUI-side wiring.
