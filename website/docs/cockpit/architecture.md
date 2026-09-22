---
title: TUI cockpit architecture
description: Internal structure of both cockpit generations — the default v1 shell in internal/tui and the frame-stable v2 kernel in internal/tui2 — and the cockpit bridge they share.
---

The Orun Cockpit is a Bubble Tea application that surfaces the same plan/run/status/logs primitives as the CLI, but as a navigable, event-driven control plane. Two generations coexist in the binary:

| Generation | Command | Package | Status |
| --- | --- | --- | --- |
| Cockpit v1 | `orun tui` (and bare `orun`) | `internal/tui` | **Default** |
| Cockpit v2 | `orun tui-next`, `orun tui --next`, `ORUN_TUI=next`, `orun agent --next` | `internal/tui2` | Preview while it soaks; the default flips once the cloud lanes land and a release has soaked |

`cmd/orun/command_tui.go` decides at launch: `useNextTUI()` is true when `--next` is passed or `ORUN_TUI=next`, and then both `runTUI` and the bare-`orun agent` front door (`runAgentTUI`) hand off to `tui2.NewProgram`. Everything else on this page up to [Cockpit v2](#cockpit-v2-internaltui2) describes v1; the [cockpit bridge](#cockpit-bridge) is shared by both.

## Shell (v1)

The cockpit is a three-pane shell:

```text
┌─ header ───────────────────────────────────────────────┐
│ sidebar │ main                              │ inspector │
│         │                                   │           │
├─────────┴───────────────────────────────────┴───────────┤
│ bottom info band (optional)                             │
└─────────────────────────────────────────────────────────┘
```

The sidebar lists modes, the main pane hosts the active view, the inspector shows a field list for the current selection, and the bottom band carries level-aware overview content. Sidebar collapsed state, inspector visibility, and bottom panel visibility are persisted (see Preferences below).

## Stack

The cockpit is built on the Charm stack:

- **Bubble Tea** provides the Elm-style model/update/view loop. Orun's TUI is event-driven (plan generation, run events, status polling, log appends, resize) and Bubble Tea's `tea.Cmd` model handles those streams without an ad-hoc event loop.
- **Bubbles** supplies list, viewport, spinner, help, and text-input widgets.
- **Lip Gloss** handles styling and layout primitives so panes can be composed declaratively.

## View model

Each pane is its own Bubble Tea sub-model, owned by the root model:

| Sub-model | Pane |
| --- | --- |
| `CatalogModel` | Main, Catalog mode (multi-kind entity explorer + component work surface: changed/affected overlay, last-run, executions) |
| `PlanStudioModel` | Main, Plan Studio mode |
| `ActivityModel` | Main, Activity mode |
| `LogExplorerModel` | Main, Logs mode |
| `HistoryModel` | Main, History mode |
| `InspectorModel` | Right-hand inspector pane |

Sub-models receive only the messages and the slice of screen real estate the root model gives them. They don't reach into each other.

The Catalog's work-surface context reads through the catalog read seam (`internal/cockpit/catalogread` → `internal/cockpit/viewmodel`), which composes the object-model catalog with the `internal/affected` change-detection engine for the changed/affected overlay; the entity list itself is projected from `internal/objcatalog` by the service layer. An execution row (`⏎`) hands off to the Activity run → job → logs drilldown. A live-view ticker re-reads the catalog off the UI thread so external writes appear without a keystroke.

## Mode machine

The root model tracks the current pane through:

```go
activeMode Mode
navBack    []Mode
navFwd     []Mode
```

`ctrl+o` pops `navBack` (and pushes onto `navFwd`); `ctrl+i` is the inverse. Direct mode jumps (`1` catalog, `2` activity, palette commands) push the previous mode onto `navBack` and clear `navFwd`.

## Drilldown machine

Inside a mode, navigation is a stack of levels:

| Mode | Levels |
| --- | --- |
| Catalog | List → Entity → Entity → … (graph walk, unbounded) |
| Activity | Index → Run → Job → Step (4 levels) |
| Plan Studio | Jobs → Steps → Step (3 levels) |

Each view exposes an `AtRoot() bool` predicate. On `esc`, the root model asks the active view: if `AtRoot()` is true, `esc` pops the mode; otherwise the view itself handles `esc` and pops one drilldown level.

## Inspector binding

When selection changes in any mode, the root model calls `refreshInspectorSelection()`. That dispatches per active mode, asks the view for the current selection's resource description, and calls:

```go
inspector.SetDescription(*services.ResourceDescription)
```

The inspector renders the description as a field list, with each field's value capped to a one-line preview. Full bodies (large step `run` blocks, multi-line manifests) live in the main pane via drill-in, so the inspector never has to scroll.

## Bottom panel

`bottomPanelHeight()` gates whether the bottom band is rendered at all (driven by `showBottom` and terminal height). When visible, `renderBottomPanel()` dispatches to the active view's:

```go
BottomPanelContent(width int) string
```

Currently implemented by `ActivityModel` (OVERVIEW / RUN PROGRESS / JOB / STEP per level) and `PlanStudioModel` (jobs / steps / step per level). Other views return an empty string and the band collapses.

## Live updates

Service-layer streams reach the model as Bubble Tea messages:

- `StatusMsg` — periodic status snapshot
- `RunStartedMsg` — a new run was kicked off (from Plan Studio dry-run or real-run)
- `LogLineMsg` — a single log line appended

Each is produced by a `tea.Cmd` returned by the service layer. In addition, `spinner.TickMsg` drives a four-frame wall-clock pulse glyph used to mark live jobs — the spinner is stateless (frame derived from `time.Now()`), so multiple panes can pulse in sync without coordinating state.

## Cockpit bridge

The TUI shares its rendering layer with `orun status`, `orun get runs`, and `orun logs` through the `internal/cockpit/*` packages:

```text
.orun/  ──▶  cockpit/bridge  ──▶  cockpit/viewmodel  ──▶  cockpit/render
                  │                                              │
                  └──▶  cockpit/watch (live updates) ─────────────┤
                                                                  ▼
                                            cockpit/surface  →  stdout / TUI
```

- `internal/cockpit/style` is the design-token source of truth (palette,
  glyphs, separators). `internal/tui/theme` wraps it via
  `lipgloss.AdaptiveColor`; `internal/ui` consumes the same hex codes
  for ANSI output. One file changes a colour everywhere.
- `internal/cockpit/viewmodel` exposes `RunView`, `RunListView`, and
  `LogsView` — pure value objects built from `state.Store` or the
  remote `statebackend.Backend` via a single `bridge.Source` interface.
- `internal/cockpit/render` formats those view-models into surface-
  agnostic lines (brand wedge, status legend, progress bar, component
  tree, grouped log frames).
- `internal/cockpit/watch` ships a polling stream emitting
  `Update{View, Err, Terminal}`. Both `orun status --watch` and the
  TUI's `LiveOrunService.WatchRunView` subscribe to the same loop, so
  refresh cadence and terminal-state semantics are identical across
  surfaces.

The TUI is the CLI with navigation; the CLI is the TUI compressed into
one frame. Drift between them is now a compile error rather than a
visual regression.

## Layout sizing

`propagateSize()` is the single owner of geometry. On `tea.WindowSizeMsg` it:

1. Computes sidebar width (collapsed vs expanded).
2. Computes inspector width (0 if hidden or terminal too narrow).
3. Computes bottom panel height (0 if hidden).
4. Subtracts those from the total and calls `SetSize(w, h)` on each child sub-model with the remaining slice.

Children must respect `SetSize` and never read raw terminal dimensions. This keeps every pane bounded to its assigned rectangle, so nothing overflows when the inspector or bottom panel is toggled.

## Preferences persistence

Persisted state lives in `internal/tui/prefs.go`:

```text
~/.orun/cockpit.json
```

Fields include `SidebarCollapsed`, `InspectorVisible`, `BottomPanelVisible`, `AutoRefresh` (the catalog auto-refresh toggle, default off), and `PerComponent` (sticky env / trigger overrides keyed by component name). `LoadPrefs()` returns `DefaultPrefs()` on any read error; `SavePrefs()` swallows write errors — prefs are non-critical and must never break the cockpit.

## Cockpit v2 (`internal/tui2`)

Cockpit v2 (`specs/orun-tui-v2`) is a rebuild, not a refactor: a fresh kernel whose contract is that the old bug classes — ghost rows, frame oscillation, idle repaints — are structurally impossible. It reuses the seams that already exist (`cockpit/bridge` for reads, `internal/agent/attach` for sessions, `internal/remotestate` for the cloud) and adds nothing to v1, which stays untouched until the default flips.

### Package layout

| Package | Role |
| --- | --- |
| `tui2` | `NewProgram(Options)` — wires the data plane, the surfaces, and the design gallery into one program. An empty `OrunRoot` falls back to the seeded mock workspace (the demo). |
| `tui2/shell` | The kernel: root Bubble Tea model, surface router, overlay stack, focus rules, command registry (palette, help, confirm, settings). It owns navigation and chrome and nothing else. |
| `tui2/frame` | The rendering kernel: a region renders into a box of exactly the size it is given; memoization by (state revision, size); the animation scheduler; the per-frame profiler. |
| `tui2/store` | Shared state as revisioned slices. Renderers fold the revisions of the slices they read into their memo keys — that is the whole caching story. |
| `tui2/design` | **Northwind Mono**, the terminal projection of the console's Northwind design system: tokens from `internal/cockpit/style`, plus status line, pills, tables, drawers, dialogs, palette, and markdown-lite components. |
| `tui2/data` | The data plane: one `Source` interface with local, cloud, and mock implementations. Reads are snapshot + cursor; `Subscribe` delivers payloadless change notifications per topic. |
| `tui2/surfaces/{home,agents,activity,catalog,events}` | The five shipped surfaces. |
| `tui2/agentfold` | Folds an attach-v1 frame stream into a renderable conversation — the counterpart of the console's conversation fold, checked against the same golden fixtures. |
| `tui2/demo` | Deterministic stub surfaces kept for the benchmarks and property tests. |

### Surfaces and navigation

A surface is a tab is a route. Each surface owns its own drill stack (Activity: feed → run → job → step → logs); `enter` and `esc` push and pop; the global back/forward history spans surfaces. What the v1 modes became: Catalog → the Catalog surface; Plan Studio → the **Compose** flow, a drill under a component rather than a place; Run Dashboard, Log Explorer, History, and Activity → the Activity surface; the Agent mode → the Agents surface. Settings (scope, auth, prefs) is an overlay, not a surface.

### Streams instead of polls

Where v1 polls the disk for live step progress, v2 subscribes to the runner's step-level hooks (`RunnerHooks.OnStepStart` and `AfterStepTerminal` in `internal/runner`) and to fs-watch notifications on `.orun` refs. Being online is a status, not a mode: surfaces gain cloud lanes when a session is authenticated and degrade to local silently.

### Performance harness

Setting `ORUN_TUI_PROFILE=/path/to/file.ndjson` installs a profiler that records each Update + View cycle (message type, durations, rendered bytes); when unset the profiler is not installed at all. The same variable is honoured by v1. `make tui-bench` runs the `internal/tui2` benchmarks (`BenchmarkIdleTick`, `BenchmarkFullFrame` in `tui2/shell`); the budget assertions themselves run as ordinary tests.

## Related

- [Cockpit overview](./overview.md) — the glyph language and the surfaces as an operator sees them.
- [`orun tui`](../cli/orun-tui.md) — cockpit v1 reference.
- [`orun tui-next`](../cli/orun-tui-next.md) — cockpit v2 preview reference.
- [`orun agent`](../cli/orun-agent.md) — the agent runtime behind the Agents surface.
- [Internals](../architecture/internals.md) — the full package map.
