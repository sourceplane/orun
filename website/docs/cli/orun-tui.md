---
title: orun tui
---

`orun tui` opens the Orun Cockpit — an interactive terminal UI for browsing components, generating plans, and watching runs.

## Launch

```bash
orun tui
```

The command takes no positional arguments. It auto-discovers the nearest `intent.yaml` and `.orun/` directory the same way `orun plan` and `orun status` do.

## Layout

The cockpit is a three-pane shell with an optional bottom information band:

```text
┌─ Orun Cockpit ────────────────────────────────────────────────────────────┐
│ repo: sourceplane/orun   plan: latest a1b2c3d   run: running              │
├──────────────┬────────────────────────────────────────┬───────────────────┤
│ SIDEBAR      │ MAIN                                   │ INSPECTOR         │
│              │                                        │                   │
│ Components   │ mode-specific view                     │ selected item     │
│ Environments │ (browse / plan studio / activity /     │ field list        │
│ Plans        │  logs / history)                       │ one-line previews │
│ Runs         │                                        │                   │
│ Logs         │                                        │                   │
├──────────────┴────────────────────────────────────────┴───────────────────┤
│ BOTTOM PANEL (toggle with `b`) — level-aware overview                     │
└───────────────────────────────────────────────────────────────────────────┘
```

The inspector auto-opens when the terminal is at least 100 columns wide. Below that, toggle it manually with `i`.

## Modes

| Mode | Purpose |
| --- | --- |
| Browse | Lists components, environments, compositions, and per-component metadata. |
| Plan Studio | Compose intent, generate plans, drill into jobs and steps, dry-run or real-run from the TUI. |
| Activity | Drilldown across runs, jobs, and steps with live status. |
| Logs | Streaming log explorer scoped to a run, job, or step. |
| History | Past runs and plans, sorted by recency. |

## Global keys

| Key | Action |
| --- | --- |
| `tab` | Switch between components and activity |
| `i` | Toggle inspector |
| `b` | Toggle bottom panel |
| `?` | Help |
| `:` | Command palette |
| `/` | Search |
| `ctrl+o` | Navigate back (mode history) |
| `ctrl+i` | Navigate forward (mode history) |
| `esc` | Back / pop drilldown level |
| `q` | Quit |
| `g a` | Go to Activity |
| `g p` | Go to Plan Studio |
| `g r` | Go to Run dashboard |
| `g l` | Go to Logs |
| `g h` | Go to History |
| `g c` | Go to Components |

## Plan Studio (Compose)

Plan Studio is a three-level drilldown:

1. **Jobs list** — every job in the current plan, listed by full GitHub-style job ID. The inspector shows job metadata (deps, env, profile, path) and a flat list of step names.
2. **Steps list** — every step in the selected job. The inspector shows one-line previews per step.
3. **Step detail** — the full step body: phase, use, shell, workdir, timeout, retry, and the `run` block.

Press `⏎` (Enter) to drill in, `esc` to pop a level.

| Key | Action |
| --- | --- |
| `g` | Generate plan |
| `R` | Real run |
| `d` | Dry run |
| `s` | Save current draft |
| `c` | Clear draft |
| `e` | Cycle environment |
| `t` | Cycle trigger |
| `↑` / `↓` | Move selection |
| `⏎` | Drill in / open |
| `esc` | Pop one level (or pop mode at root) |

The bottom panel (toggle `b`) is level-aware:

| Level | Bottom panel shows |
| --- | --- |
| Jobs | `N jobs`, `N envs`, `N components` + plan checksum |
| Steps | `N steps`, `use` / `run` counts, phase breakdown |
| Step | capability, phase, timeout, retry, shell |

Job step bodies live in the drill-in view rather than the inspector — the inspector only shows a flat list of step names, so jobs with large bodies don't overflow.

## Activity

Activity is a four-level drilldown:

1. **Index** — all recent runs.
2. **Run** — jobs in the selected run.
3. **Job** — steps in the selected job.
4. **Step** — full step detail.

The bottom panel changes per level:

| Level | Bottom panel shows |
| --- | --- |
| Index | OVERVIEW (`✓` / `✗` / live counts + recent runs sparkline) |
| Run | RUN PROGRESS (per-job status bar) |
| Job | JOB status (steps, timing, exit) |
| Step | STEP detail (phase, capability, exit, duration) |

Live jobs pulse via a four-frame wall-clock spinner. Step rows show jump-back chips so deep drilldowns can be popped quickly:

```text
◀ esc · back to job
◀◀ esc esc · back to run
```

## Preferences

The TUI persists inspector visibility, bottom panel visibility, sidebar collapsed state, and sticky per-component overrides (env / trigger) to:

```text
~/.orun/cockpit.json
```

Writes are best-effort and silent — a missing or corrupt prefs file falls back to defaults.

## See also

- [orun plan](./orun-plan.md)
- [orun run](./orun-run.md)
- [orun status](./orun-status.md)
- [orun logs](./orun-logs.md)
