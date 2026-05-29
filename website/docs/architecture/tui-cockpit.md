---
title: TUI cockpit architecture
---

The Orun Cockpit (`orun tui`) is a Bubble Tea application that surfaces the same plan/run/status/logs primitives as the CLI, but as a navigable, event-driven control plane. This document describes its internal structure.

## Shell

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
| `BrowseModel` | Main, Browse mode |
| `PlanStudioModel` | Main, Plan Studio mode |
| `ActivityModel` | Main, Activity mode |
| `LogExplorerModel` | Main, Logs mode |
| `HistoryModel` | Main, History mode |
| `InspectorModel` | Right-hand inspector pane |

Sub-models receive only the messages and the slice of screen real estate the root model gives them. They don't reach into each other.

## Mode machine

The root model tracks the current pane through:

```go
activeMode Mode
navBack    []Mode
navFwd     []Mode
```

`ctrl+o` pops `navBack` (and pushes onto `navFwd`); `ctrl+i` is the inverse. Direct mode jumps (`g a`, `g p`, `g r`, `g l`, `g h`, `g c`) push the previous mode onto `navBack` and clear `navFwd`.

## Drilldown machine

Inside a mode, navigation is a stack of levels:

| Mode | Levels |
| --- | --- |
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

Fields include `SidebarCollapsed`, `InspectorVisible`, `BottomPanelVisible`, and `PerComponent` (sticky env / trigger overrides keyed by component name). `LoadPrefs()` returns `DefaultPrefs()` on any read error; `SavePrefs()` swallows write errors — prefs are non-critical and must never break the cockpit.
