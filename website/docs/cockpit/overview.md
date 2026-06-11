---
title: Cockpit overview
description: The cockpit is the unified UX layer for orun. Same view-model, same glyphs, same palette — across the CLI and the TUI.
---

The **cockpit** is the unified UX layer for orun. Every surface that shows you what's
happening — `orun status`, `orun status --watch`, `orun get runs`, `orun logs`, and
`orun tui` — flows through the same view-model and the same design tokens.

It is the operator-facing half of orun: planning happens in [the compiler](/architecture/compiler-pipeline);
operation happens in the cockpit.

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
orun tui      # explicit
orun          # bare invocation opens the cockpit on an interactive terminal
```

A three-pane Bubble Tea shell: sidebar (modes), main pane (active view), inspector
(field list for the selection). Modes are `Browse`, `Component`, `Plan Studio`,
`Activity`, `Logs`, `History`, and `Catalog`. See
[cockpit architecture](/cockpit/architecture) for the full mode and drilldown
machine.

The cockpit is the **default command** — a bare `orun` opens it on an interactive
terminal, and falls back to printing help in non-interactive shells or when
`ORUN_NO_TUI` is set. From Plan Studio you can **dry-run** (`d`) or **real-run**
(`R`, behind a confirm) a plan; a real run executes through the same internal
runner as `orun run`, persists state and per-step logs to `.orun/`, and streams
those logs into the Activity and Logs surfaces live. See the
[TUI reference](/cli/orun-tui) for the run and live-log workflow.

## The component catalog view

Browse renders the workspace's component list from the **object-model catalog**
(the same one [`orun catalog`](/cli/orun-catalog) reads), and stays current on its
own:

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
- **Changed / affected overlay.** Each row is badged by the change-detection
  engine: a filled dot for a **directly changed** component, a hollow dot for one
  **affected** through a dependency. Press `c` to filter to only the changed and
  affected rows — the cockpit view of `orun catalog affected`.
- **Freshness gate.** When the catalog is fresh for a clean tree, the list is
  served straight from the catalog; a dirty tree falls back to the live intent
  loader so uncommitted edits show immediately. Either way the row data
  (`name/type/domain/path/envs/profile/dependsOn/watches`) is identical.

### Drill down: catalog → component → job → logs

Press `⏎` on a component to open its **Component page** — the resolved detail
(path, envs, profile, dependencies, `change.watches`, change badge) plus a
**Recent executions** section (the runs that touched this component). Drill into an
execution to reach the run → job → logs view. This mirrors the object graph: every
level reads from the object-model store (the catalog and the sealed executions
under `.orun/objectmodel/`).

## The Catalog surface — the multi-kind entity explorer

Press `3` (or `tab`-cycle, or the `goto.catalog` palette command) to open the
**Catalog** surface: the cockpit view of the
[service-catalog entity model](/cli/orun-catalog). Where Browse is the *work*
surface (components you can compose and run), Catalog is the *knowledge*
surface — every entity the resolver derived from your workspace:

- **Kind tabs with counts.** `[` / `]` (or `←`/`→`) cycle through the kinds
  present in the catalog — Component, API, Resource, System, Domain, Group,
  Composition, Environment, Deployment — each with its entity count. The `All`
  tab mixes kinds with a kind glyph per row.
- **Envelope columns per kind.** Components lead with OWNER (CODEOWNERS-derived
  ownership) and STAGE (lifecycle); Compositions show VERSION and lifecycle
  stage; derived kinds show member counts.
- **A walkable graph.** `⏎` opens an entity's detail page: identity, ownership,
  lifecycle, and a **Connections** list — its members and typed relation edges
  (`dependsOn`, `partOf`, `ownedBy`, `deployedTo`, `composedBy`, …, with `◂`
  marking incoming edges). Connections are navigable: `⏎` follows an edge to
  its other endpoint, `esc` walks back. The header breadcrumb tracks the path.
- **Same freshness model as Browse.** The surface reads the resolved catalog at
  `catalogs/current`; the header's `⟳ stale (⌃r)` badge tells you when a
  refresh would change it.
- **The work surface, on every component.** Component entities carry the
  changed/affected overlay (CHG column, `c` filters to changed-only), the
  last-run status (LAST column), and an **Executions** section on the detail
  page that drills straight into the Activity run view. `r` runs the selected
  component for the selected environment (same confirm-then-execute flow as
  the Component page), `g` composes it in Plan Studio, `o` opens its classic
  page. Knowledge surface and work surface are one screen.

### Environment selector and component-scoped run

The cockpit holds one **selected environment** (shown in the header), cycled with
`e` and remembered between sessions. From a component page, `r` launches a
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

## Next

- **[Status reference](/cli/orun-status)** — every flag, output format, and exit code.
- **[Logs reference](/cli/orun-logs)** — filtering and grouping.
- **[TUI reference](/cli/orun-tui)** — key bindings, modes, drilldown.
- **[Cockpit architecture](/cockpit/architecture)** — internal structure, view-model
  flow, preferences, sizing.
