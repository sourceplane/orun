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
orun tui
```

A three-pane Bubble Tea shell: sidebar (modes), main pane (active view), inspector
(field list for the selection). Modes are `Browse`, `Plan Studio`, `Activity`, `Logs`,
`History`. See [cockpit architecture](/cockpit/architecture) for the full mode and
drilldown machine.

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
- **Not a state editor.** The cockpit reads `.orun/`. It does not mutate state. Even
  `orun run --resume` writes new state; it doesn't rewrite the old.

## Next

- **[Status reference](/cli/orun-status)** — every flag, output format, and exit code.
- **[Logs reference](/cli/orun-logs)** — filtering and grouping.
- **[TUI reference](/cli/orun-tui)** — key bindings, modes, drilldown.
- **[Cockpit architecture](/cockpit/architecture)** — internal structure, view-model
  flow, preferences, sizing.
