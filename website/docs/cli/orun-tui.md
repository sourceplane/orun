---
title: orun tui
---

`orun tui` opens the Orun Cockpit — an interactive terminal UI for browsing components, generating plans, running them, and watching logs stream live.

## Launch

```bash
orun tui     # explicit
orun         # bare invocation — opens the cockpit on an interactive terminal
```

The cockpit is the **default command**: running `orun` with no arguments and no
subcommand opens it, so `orun` and `orun tui` are equivalent in an interactive
terminal.

To keep scripts and CI predictable, a bare `orun` falls back to printing help
(it does **not** launch the TUI) when **any** of the following are true:

- standard input or output is not a TTY (pipes, redirects, CI logs), or
- `ORUN_NO_TUI` is set to a truthy value (`1`, `true`, `yes`).

```bash
ORUN_NO_TUI=1 orun        # print help even on an interactive terminal
orun | cat                # non-TTY → prints help, never launches the TUI
orun plan ...             # explicit subcommands are always unaffected
orun bogus                # unknown command → error (not swallowed by the TUI)
```

The command takes no positional arguments. It auto-discovers the nearest `intent.yaml` and `.orun/` directory the same way `orun plan` and `orun status` do — including when launched as a bare `orun` from a subdirectory.

## Layout

The cockpit is a three-pane shell with an optional bottom information band:

```text
┌─ Orun Cockpit ────────────────────────────────────────────────────────────┐
│ repo: sourceplane/orun   plan: latest a1b2c3d   run: running              │
├──────────────┬────────────────────────────────────────┬───────────────────┤
│ SIDEBAR      │ MAIN                                   │ INSPECTOR         │
│              │                                        │                   │
│ Catalog      │ mode-specific view                     │ selected item     │
│ Activity     │ (catalog / plan studio / activity /    │ field list        │
│              │  logs / history)                       │ one-line previews │
│              │                                        │                   │
│              │                                        │                   │
├──────────────┴────────────────────────────────────────┴───────────────────┤
│ BOTTOM PANEL (toggle with `b`) — level-aware overview                     │
└───────────────────────────────────────────────────────────────────────────┘
```

The inspector auto-opens when the terminal is at least 100 columns wide. Below that, toggle it manually with `i`.

## Modes

| Mode | Purpose |
| --- | --- |
| Catalog | The home surface: every catalog entity (Component, API, Resource, System, Domain, Group, Composition, Environment, Deployment) with kind tabs, ownership/lifecycle columns, walkable relation edges — and, on Component entities, the full work surface (change overlay, run, compose, execution history). |
| Plan Studio | Compose intent, generate plans, drill into jobs and steps, dry-run or real-run from the TUI. |
| Activity | Drilldown across runs, jobs, and steps with live status. |
| Logs | Streaming log explorer scoped to a run, job, or step. |
| History | Past runs and plans, sorted by recency. |

The former **Browse** and **Component** surfaces are absorbed into the Catalog:
component rows live on the Catalog's Component tab (the default tab), and the
entity detail page carries everything the Component page used to show.

## Global keys

| Key | Action |
| --- | --- |
| `tab` | Toggle the top-level surfaces (catalog ⇄ activity) |
| `1` / `2` | Jump to Catalog / Activity |
| `i` | Toggle inspector |
| `b` | Toggle bottom panel |
| `?` | Help |
| `:` | Command palette |
| `/` | Search |
| `ctrl+r` | Refresh the catalog now |
| `ctrl+o` | Navigate back (mode history) |
| `ctrl+i` | Navigate forward (mode history) |
| `esc` | Back / pop drilldown level |
| `q` | Quit |

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

## Running plans from the cockpit

The cockpit runs plans through the same internal packages as `orun run` — it
never shells out to the `orun` binary.

| Key | Action | Effect |
| --- | --- | --- |
| `d` | **Dry run** | Previews execution. Emits per-job lifecycle events but runs no commands and writes no logs. |
| `R` | **Real run** | Executes the plan locally. Pops a confirmation modal first (`y` to proceed, `n`/`esc` to cancel) because real runs invoke real commands. |

A real run:

- executes each job's steps with the local executor,
- persists run state and **per-step logs** to `.orun/` (exactly like `orun run`), and
- streams lifecycle events into the Activity surface as they happen.

Both run types kick over to **Activity** with the in-flight run pinned to the
top of the run list, pulsing live until it reaches a terminal state.

### Live logs while running

Logs stream into the cockpit **as a real run executes** — you don't have to
wait for it to finish:

- **Activity → Step level.** Drill into a running job's step (`⏎`) and the log
  pane attaches to that run and follows it, surfacing each step's output as the
  step completes. The tail stops automatically when the run finishes.
- **Run dashboard.** Press `⏎` on a job row to open the Log Explorer attached to
  that job; while the run is live the explorer follows new output, and for a
  finished run it replays the stored logs once.

Follow-mode tailing is scoped to the active run's execution ID and is cancelled
automatically when the run completes, when you attach a different job/step, or
when you leave the logs surface — so background tails never accumulate.

Dry runs intentionally produce no logs (they execute nothing); use a **real
run** (`R`) when you want to watch live output.

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

## Catalog (entity explorer)

The Catalog surface (`1`, the home surface) renders the resolved service
catalog as a browsable graph. The list level shows kind tabs with per-kind
counts and opens on the **Component** tab; the detail level shows one entity's
envelope (identity, ownership, lifecycle) plus its **Connections** — members
and typed relation edges, each navigable.

Component entities carry the full work surface: the Component tab adds
**LAST** (last-run status) and **CHG** (changed/affected overlay) columns, and
the detail page shows the source detail (path, profile, watches), the live
change state, and an **Executions** section.

| Key | Action |
| --- | --- |
| `[` / `]` (or `←` / `→`) | Cycle kind tab (All → Component → API → …) |
| `↑` / `↓` | Move selection |
| `⏎` | Open entity / follow a connection / open an execution |
| `r` | Run the selected component for the selected environment (confirms first) |
| `g` | Compose the selected component in Plan Studio |
| `e` | Cycle the selected environment |
| `c` | Toggle changed-only (changed/affected components) |
| `esc` | Pop back one entity (or pop mode at the list) |
| `/` | Filter by name, kind, key, or owner |

Connections marked `◂` are incoming edges (e.g. `◂ deployedTo` on an
Environment lists the components deployed to it). A `⤴` suffix marks an
endpoint outside the loaded catalog (e.g. a cross-repo dependency) — shown but
not followable. On a Component's detail page, `⏎` on an **Executions** row
jumps straight into that run's Activity drilldown (run → jobs → steps → logs).

## Catalog freshness

The Catalog surface reads the [object-model catalog](../concepts/state-model.md#the-component-catalog).
The cockpit keeps it current for you rather than relying on an external
`orun plan`/`run`/`catalog refresh` having run:

- **Refresh on open.** Launching the cockpit resolves and persists a current
  catalog (even for a dirty tree), so you start on an up-to-date view.
- **Manual refresh.** `ctrl+r` — or the `catalog.refresh` command-palette command
  (`:`) — forces an immediate refresh at any time.
- **Auto-refresh toggle.** The `catalog.autorefresh` command-palette command turns
  on periodic refresh (on the live-view tick), refreshing only when the source has
  changed. It is **off by default** so a dirty tree does not re-resolve on every
  edit; the choice persists across sessions (see Preferences).
- **Stale badge.** When the loaded catalog no longer matches the working tree, the
  header shows a `⟳ stale (⌃r)` pill prompting a refresh.

The cockpit and the CLI share one resolve engine, so both produce the **same**
content-addressed catalog id, and a non-blocking lock keeps a concurrent
`orun catalog refresh` and the cockpit from both running the expensive resolve.

## Preferences

The TUI persists inspector visibility, bottom panel visibility, sidebar collapsed state, the catalog **auto-refresh** toggle (`autoRefresh`, default off), and sticky per-component overrides (env / trigger) to:

```text
~/.orun/cockpit.json
```

Writes are best-effort and silent — a missing or corrupt prefs file falls back to defaults.

## See also

- [orun plan](./orun-plan.md)
- [orun run](./orun-run.md)
- [orun status](./orun-status.md)
- [orun logs](./orun-logs.md)
