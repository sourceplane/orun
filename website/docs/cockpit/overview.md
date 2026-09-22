---
title: Cockpit overview
description: The cockpit is the unified UX layer for orun. Same view-model, same glyphs, same palette — across the CLI and the TUI.
---

The **cockpit** is the unified UX layer for orun. Every surface that shows you what's
happening — `orun status`, `orun status --watch`, `orun get runs`, `orun logs`, and
`orun tui` — flows through the same view-model and the same design tokens.

It is the operator-facing half of orun: planning happens in [the compiler](/architecture/compiler-pipeline);
operation happens in the cockpit.

Two generations of the full-screen cockpit ship in the binary today. **Cockpit v1**
(`orun tui`, `internal/tui`) is the default. **Cockpit v2** (`orun tui-next`,
`internal/tui2`) is a preview that rebuilds the same surfaces on a frame-stable kernel;
see [Cockpit v2](#cockpit-v2--orun-tui-next-preview) below. Both read the same state
and the same design tokens.

<div className="cockpitFrame">
  <div className="cf-chrome">
    <span className="cf-dots"><span/><span/><span/></span>
    <span>orun status · live</span>
  </div>
<pre>
{`▲ orun multi-environment-platform
  Plan: sha256-ad6ce · Run: gh-26563885741 · State: running · Duration: 38.2s
  Scope: 3 components · 6 jobs · 2 environments

  Status:   ✓ 3 succeeded · ◐ 2 running · ○ 1 queued · ✗ 0 failed
  Progress: ▓▓▓▓▓▓▓▓▓▓░░░░░ 66%

  ● api-edge-worker
  │  ├─ ✓ build              4.1s
  │  ├─ ✓ test               12.3s
  │  └─ ◐ verify-deploy      running 19.0s
  ● database
  │  └─ ◐ apply              running 38.2s
  ○ web-app
     └─ ○ deploy             queued`}
</pre>
</div>

## What "unified" means

Three things are shared across every cockpit surface:

<div className="signalGrid">
  <article className="signalCard">
    <strong>view-model</strong>
    <h3>One read path</h3>
    <p>
      <code>internal/cockpit/bridge.Source</code> reads from either a local <code>.orun/</code>
      directory or a remote <code>statebackend.Backend</code>. The CLI and TUI both consume
      <code>RunView</code>, <code>RunListView</code>, and <code>LogsView</code> — pure value
      objects with no rendering logic.
    </p>
  </article>
  <article className="signalCard">
    <strong>design tokens</strong>
    <h3>One palette, one glyph set</h3>
    <p>
      <code>internal/cockpit/style</code> owns the violet brand
      (<span className="g g-brand">▲</span> <code>#7c3aed</code> / <code>#a78bfa</code>), the
      lifecycle glyphs, and the tree connectors. Both the ANSI layer and the lipgloss theme
      wrap these constants. Reskinning is one file.
    </p>
  </article>
  <article className="signalCard">
    <strong>live updates</strong>
    <h3>One polling stream</h3>
    <p>
      <code>internal/cockpit/watch</code> emits <code>Update&#123;View, Err, Terminal&#125;</code>
      on a 500ms cadence (100ms floor). <code>orun status --watch</code> subscribes directly;
      the TUI subscribes via <code>LiveOrunService.WatchRunView</code>. Refresh and
      terminal-state semantics are identical.
    </p>
  </article>
</div>

## The glyph language

The cockpit speaks a small, deliberate vocabulary. Every glyph maps to a status token,
which maps to a palette color. Once you know the alphabet, you read every surface fluently.

| Glyph | Token | Meaning |
|---|---|---|
| <span className="g g-ok">✓</span> | `Success` | Step or job completed successfully |
| <span className="g g-fail">✗</span> | `Error` | Step or job failed |
| <span className="g g-run">◐</span> | `Running` | In progress (paired with wall-clock spinner in TUI) |
| <span className="g g-pending">○</span> | `Pending` | Queued, not started |
| <span className="g g-skip">↷</span> | `Warning` | Skipped (condition gate, dependency failure) |
| <span className="g g-brand">●</span> | `Brand` | Active or changed scope marker |
| <span className="g g-brand">▲</span> | `Brand` | The orun wedge — opens every cockpit header |

Tree connectors `├─`, `└─`, `│` group jobs under components, and steps under jobs.
`→` marks transitions (promotions, redirects). `↻` marks retries.

`NO_COLOR=1` strips palette but **never** strips glyphs — the alphabet survives.

## Three surfaces, same frame

### `orun status` — the one-shot cockpit

```bash
orun status                # default frame for the current run
orun status --all          # list of recent runs
orun status --watch        # live updates until terminal state
orun status --run <id>     # specific run by ID
```

The CLI is the TUI **compressed to one frame**. No navigation, no panes — just the
single most relevant view, rendered through the same view-model and design tokens.

### `orun logs` — grouped log view

```bash
orun logs                  # all logs from the current run
orun logs --failed         # failed steps only
orun logs --job <id>       # one job's logs
orun logs --watch          # tail live
```

Logs are grouped by job and step, with `… N more lines` truncation in dense views.
The grouping is built by `internal/cockpit/render` and is identical in the TUI's
Log Explorer pane.

### `orun tui` — the full cockpit

```bash
orun tui      # explicit (cockpit v1, the default)
orun          # bare invocation opens the cockpit on an interactive terminal
orun tui-next # cockpit v2 (preview); also: orun tui --next, or ORUN_TUI=next
```

Cockpit v1 is a three-pane Bubble Tea shell: sidebar (surfaces), main pane
(active view), inspector (field list for the selection). The two top-level
surfaces are `Catalog` (the home surface) and `Activity`; `Plan Studio`,
`Logs`, and `History` are reached from within them. See
[cockpit architecture](/cockpit/architecture) for the full mode and drilldown
machine.

The cockpit is the **default command** — a bare `orun` opens it on an interactive
terminal, and falls back to printing help in non-interactive shells or when
`ORUN_NO_TUI` is set. From Plan Studio you can **dry-run** (`d`) or **real-run**
(`R`, behind a confirm) a plan; a real run executes through the same internal
runner as `orun run`, persists state and per-step logs to `.orun/`, and streams
those logs into the Activity and Logs surfaces live. See the
[TUI reference](/cli/orun-tui) for the run and live-log workflow.

## Cockpit v2 — `orun tui-next` (preview)

Cockpit v2 is the terminal head of Orunbase: the same entities the hosted console
renders, at terminal density. It lives in `internal/tui2` and is opt-in while it
soaks — `orun tui-next`, `orun tui --next`, or `ORUN_TUI=next` all open it, and
`orun agent --next` opens it straight on the Agents surface. The v1 cockpit stays
the default until the remaining cloud lanes land and a release has soaked.

| Surface | What it shows |
|---|---|
| **Home** | Stat tiles (components, live sessions, last run), a needs-attention list, latest activity, current scope |
| **Agents** | Live local agent sessions, agent types, the launch flow, and the conversation head over the attach protocol |
| **Activity** | Runs feed → run → jobs → steps → logs, stream-driven from step-level runner events; the log explorer is the leaf |
| **Catalog** | Entity explorer by kind, entity detail with relations, and the component work surface — Plan Studio becomes the **Compose** flow under a component |
| **Events** | Local execution and agent session events |

What is different from v1, by construction:

- **Frame stability.** Every region renders into a box of exactly the size it is
  given (`tui2/frame`); regions are memoized by state revision and size, and one
  animation scheduler ticks only while something is genuinely live. An idle cockpit
  renders nothing.
- **Streams over polls.** Step progress arrives through the runner's
  `OnStepStart` / `AfterStepTerminal` hooks and catalog refresh through fs-watch
  on `.orun` refs, instead of the disk polling v1 relies on.
- **Northwind Mono.** The design system (`tui2/design`) is the terminal projection
  of the console's Northwind system, built on the same tokens in
  `internal/cockpit/style` — so v1, v2, and the CLI still share one palette and
  one glyph alphabet.
- **Claude Code grammar.** One header line, one status line, `esc` always
  dismisses, and a command palette is the escape hatch for everything.

`ORUN_TUI_PROFILE=/path/to/file.ndjson` writes per-frame timings for either
cockpit generation; `make tui-bench` reports the v2 render budgets.

## The Catalog surface — knowledge and work in one screen

The **Catalog** is the cockpit's home surface (`1`, or the `goto.catalog`
palette command): the cockpit view of the
[service-catalog entity model](/cli/orun-catalog). It replaced the former
Browse and Component surfaces — every entity the resolver derived from your
workspace is here, and Component entities carry the full work surface:

- **Kind tabs with counts.** `[` / `]` (or `←`/`→`) cycle through the kinds
  present in the catalog — Component, API, Resource, System, Domain, Group,
  Composition, Environment, Deployment — each with its entity count. The
  surface opens on the **Component** tab; the `All` tab mixes kinds with a
  kind glyph per row.
- **Envelope columns per kind.** Components lead with OWNER (CODEOWNERS-derived
  ownership) and STAGE (lifecycle); Compositions show VERSION and lifecycle
  stage; derived kinds show member counts.
- **Changed / affected overlay.** Component rows are badged by the
  change-detection engine: a filled dot for a **directly changed** component,
  a hollow dot for one **affected** through a dependency. Press `c` to filter
  to only the changed and affected components — the cockpit view of
  `orun catalog affected`.
- **A walkable graph.** `⏎` opens an entity's detail page: identity, ownership,
  lifecycle, and a **Connections** list — its members and typed relation edges
  (`dependsOn`, `partOf`, `ownedBy`, `deployedTo`, `composedBy`, …, with `◂`
  marking incoming edges). Connections are navigable: `⏎` follows an edge to
  its other endpoint, `esc` walks back. The header breadcrumb tracks the path.
- **Execution history, in place.** A Component's detail page shows its source
  detail (path, profile, watches), live change state, last-run status, and an
  **Executions** section; `⏎` on an execution drills straight into the
  Activity run → job → logs view, reading the sealed executions under
  `.orun/objectmodel/`.
- **Run from the graph.** `r` runs the selected component for the selected
  environment (confirm-then-execute through the same internal runner as
  `orun run`); `g` composes it in Plan Studio.

### Catalog freshness

The surface stays current on its own:

- **Live, keystroke-free refresh.** The cockpit re-reads the catalog on a short
  interval, so a component you edit on disk — or a catalog written by an external
  `orun plan`/`run` or the universal refresh hook — appears within a few seconds
  without a reload.
- **Keeps its own catalog fresh.** The cockpit resolves a current catalog when it
  opens (even for a dirty tree). In-session, `ctrl+r` (or the `catalog.refresh`
  palette command) forces an immediate refresh, and the `catalog.autorefresh`
  palette command enables periodic re-resolve on change (off by default, persisted
  in `~/.orun/cockpit.json`). When the loaded catalog drifts from the working tree,
  a `⟳ stale (⌃r)` badge appears in the header. See the
  [TUI reference](/cli/orun-tui#catalog-freshness) for the full workflow.
- **Freshness gate.** When the catalog is fresh for a clean tree, the component
  work-surface context is served straight from the catalog; a dirty tree falls
  back to the live intent loader so uncommitted edits show immediately.

### Environment selector and component-scoped run

The cockpit holds one **selected environment** (shown in the header), cycled with
`e` and remembered between sessions. On a component, `r` launches a
**component-scoped run for the selected environment** — only when the component is
active in it — through the same `orun run` path (it confirms, then executes and
persists state + logs). `g` opens Plan Studio to compose instead. This uses the
existing environment model; it does not change how `orun plan`/`run` resolve
environments elsewhere.

## State, on disk

The cockpit reads from `.orun/`, written by `orun run`:

```text
.orun/
├── runs/
│   └── <run-id>/
│       ├── metadata.json    ExecMetadata — plan ref, start time, trigger
│       ├── state.json       ExecState — job/step status, durations, exit codes
│       └── logs/
│           └── <job>.log
└── current                  symlink to the most recent run
```

This is the only place runtime state lives. Anything you can see in the cockpit, you can
see by reading `.orun/` directly. Remote state backends (`statebackend.Backend`) expose
the same shape over the wire — `bridge.FromBackend` normalises them into the same
`bridge.Source` interface.

## What the cockpit deliberately is not

- **Not a dashboard.** It is operator-facing, not stakeholder-facing. There is no
  aggregated cross-run metrics view, no SLO panel, no graphs of deployment frequency.
  Those belong upstream, in your observability stack.
- **Not a CI UI.** GitHub Actions, Buildkite, and friends remain the systems of record
  for who triggered what. The cockpit shows you the **plan** and the **execution** of
  one run; the CI shows you the context.
- **Not a state editor.** The cockpit reads `.orun/`, and a real run from Plan Studio
  *appends* new run state and logs through the same runner as `orun run`. It never
  rewrites or hand-edits existing state — even `orun run --resume` writes new state
  rather than mutating the old.

## Related

- [Status reference](../cli/orun-status.md) — every flag, output format, and exit code.
- [Logs reference](../cli/orun-logs.md) — filtering and grouping.
- [TUI reference](../cli/orun-tui.md) — cockpit v1 key bindings, modes, drilldown.
- [`orun tui-next`](../cli/orun-tui-next.md) — the cockpit v2 preview.
- [`orun agent`](../cli/orun-agent.md) — the Agents surface and the agent runtime.
- [Cockpit architecture](./architecture.md) — internal structure of both generations,
  view-model flow, preferences, sizing.
