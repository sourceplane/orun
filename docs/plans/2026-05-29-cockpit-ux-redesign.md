# Orun Cockpit UX Redesign — Phased Plan

> Goal: a single, coherent design language across the TUI (`orun tui`) and
> every stdout command (`status`, `get`, `logs`, `run`, …), backed by a robust
> bridge between `.orun` on-disk state and whatever surface is rendering it.

## North star

Orun stops being "a CLI plus a TUI with similar-but-different output". Both
surfaces become two presentations of the **same Cockpit view-model**, drawn
with the **same design tokens**, and fed by the **same state bridge**. The CLI
becomes the TUI compressed into a single frame; the TUI becomes the CLI with
navigation and panes.

Reference languages we steal from:
- **k9s / lazygit** — k9s-style header strip + lazygit's column metaphors.
- **GitHub Actions / Vercel** — run summary cards with status pills and
  progress bars.
- **Linear / Claude Code** — soft accent palette, generous whitespace, dim
  metadata, a single bright focus colour (violet `#a78bfa` dark / `#7c3aed`
  light), unicode glyphs everywhere.
- **CNCF tooling** (flux, argo, kubectl) — verbs over flags, predictable
  resource grammar, machine-readable `-o json|yaml|wide` always available.

## Architecture

```
internal/cockpit/
  style/         Design tokens (colors, glyphs, brand, separators).
                 Single source of truth, consumed by:
                   - internal/ui (CLI ANSI rendering)
                   - internal/tui/theme (lipgloss styles)
  viewmodel/     Pure value objects built from .orun state.
                 RunView, RunListView, ComponentView, PlanView, LogView.
                 Side-effect-free; trivially testable.
  render/        Surface-agnostic formatters.
                 RenderRun(view, surface) -> []string for stdout,
                 also reused by tui/views as the canonical layout.
  surface/       Output surface abstraction.
                 ANSISurface (TTY), PlainSurface (non-TTY/CI),
                 JSONSurface (structured), GHASurface (GitHub Actions).
                 Knows width, color, interactivity.
  watch/         fsnotify-backed stream over .orun/executions/<id>/.
                 Single ChangeStream consumed by both
                 `orun status --watch` and TUI's LiveOrunService.
  bridge/        Glue: workspace discovery + state store + watcher
                 wrapped into one StateBridge that surfaces just need
                 to wire up.
```

### Why this shape

1. **One design system.** Both CLI and TUI import `cockpit/style`. Changing
   the accent colour or the success glyph happens in one place and ripples
   everywhere.
2. **One state model.** Both surfaces consume `viewmodel.RunView` (etc).
   Today the CLI builds its own `jobView` and the TUI builds another via
   `OrunService` — they drift. The cockpit viewmodel collapses them.
3. **One event stream.** `cockpit/watch` is fsnotify on `state.json` +
   `metadata.json`. Today `orun status --watch` re-polls every second and
   re-renders the whole frame; the TUI has its own polling tick. The bridge
   unifies them and adds proper live updates.
4. **Surface polymorphism.** The render package emits structured "lines"
   that an `ANSISurface` paints with colour, a `PlainSurface` strips, a
   `JSONSurface` rewrites as objects, and a `GHASurface` wraps in
   `::group::` markers. Each command picks a surface and writes once.

## Phases

### Phase 1 — Foundation (this PR)

Ship the cockpit packages and one end-to-end proof (`orun status`).

- [x] `internal/cockpit/style` — tokens (colors, glyphs, brand, separators).
- [x] `internal/cockpit/viewmodel` — `RunView`, `RunListView`, helpers.
- [x] `internal/cockpit/surface` — `Surface` interface + `ANSISurface`,
      `PlainSurface`, `JSONSurface`.
- [x] `internal/cockpit/render` — `RunStatus`, `RunList`, `Header`.
- [x] `internal/cockpit/watch` — fsnotify wrapper over `.orun/executions`.
- [x] `internal/cockpit/bridge` — `StateBridge` (store + watcher).
- [x] Port `cmd/orun/command_status.go` to use cockpit (non-remote path).
- [x] Wire `internal/ui` brand colour + glyph constants through cockpit.
- [x] Tests for viewmodel + render (table-driven; golden-style).

### Phase 2 — Port the rest of stdout

- [ ] `orun get runs|plans|jobs|components` → cockpit list renderer.
- [ ] `orun logs` → cockpit log viewer (compact / raw / failed).
- [ ] `orun run` live progress → cockpit surface (replaces
      `internal/ui.LiveRegion` direct use; LiveRegion becomes the
      ANSISurface's spinner backend).
- [ ] Remote state path of `orun status` ported.
- [ ] GHA renderer migrated to a `cockpit/surface.GHASurface`.
- [ ] `--output=json|yaml|wide` consistent across all `get` verbs.

### Phase 3 — TUI consumes the same view-model

- [ ] `internal/tui/services` returns `cockpit/viewmodel.*` types instead
      of its own `RunSummary`, `ComponentSummary`, etc.
- [ ] `internal/tui/theme` becomes a thin lipgloss wrapper around
      `cockpit/style` tokens — no duplicate colour definitions.
- [ ] `internal/tui/views/run_view.go` reuses `cockpit/render.RunStatus`
      to populate the main pane (rendered via lipgloss instead of ANSI).
- [ ] TUI's `LiveOrunService` subscribes to `cockpit/watch.ChangeStream`
      for true fsnotify-driven updates, replacing the ticker loop.

### Phase 4 — Cockpit-grade affordances

- [ ] `orun status --watch` becomes an inline cockpit: header strip, status
      pills, sticky progress bar, fsnotify-driven, no full-screen redraw.
- [ ] `orun logs --follow` cockpit: per-step group headers, jump-to-error
      hotkey hint when interactive, structured JSON when piped.
- [ ] `orun explain <ref>` — same explain feature the TUI hopes to add,
      first shipped on the CLI (e.g. `orun explain job api@stage.deploy`).
- [ ] Plan diff (`orun plan diff <a> <b>`) using cockpit's diff renderer.
- [ ] Failure workbench formatter shared by CLI failure footers and TUI
      inspector pane.

## Design tokens (frozen for Phase 1)

| Token            | Light          | Dark           | Glyph |
|------------------|----------------|----------------|-------|
| `Brand`          | `#7c3aed`      | `#a78bfa`      | `▲`   |
| `BrandSoft`      | `#c4b5fd`      | `#6d28d9`      |       |
| `Secondary`      | `#0891b2`      | `#22d3ee`      |       |
| `Success`        | `#059669`      | `#34d399`      | `✓` / `●` |
| `Warning`        | `#b45309`      | `#fbbf24`      | `↷` / `◌` |
| `Error`          | `#dc2626`      | `#f87171`      | `✗`   |
| `Running`        | `#0891b2`      | `#22d3ee`      | `◐` (spinner: braille) |
| `Pending`        | `#64748b`      | `#64748b`      | `○`   |
| `FG` / `Dim`     | `#1f2933` / `#64748b` | `#e2e8f0` / `#64748b` |       |

Separators: `·` (inline) · `  ─  ` (rule) · `│ ├─ └─` (tree).
Brand line: `▲ orun <name>` with the triangle in `Brand`.

## Compatibility

- `ORUN_NO_COLOR` / `NO_COLOR` / `CLICOLOR_FORCE` honoured by `cockpit/style`
  (delegates to `internal/ui.ColorEnabledForWriter`).
- `--output=json` works on every cockpit renderer (Phase 2).
- GHA detection (`GITHUB_ACTIONS=true`) auto-selects `GHASurface` (Phase 2).
- Existing `internal/ui.LiveRegion` remains in place during Phase 1; it
  becomes the spinner backend for `ANSISurface` in Phase 2.

## Out of scope (for now)

- Full Kubernetes-style `orun watch`/`-w` on arbitrary resources.
- Remote backend WebSocket streaming (today is poll-based; the cockpit
  watcher abstraction is fsnotify-only in Phase 1).
- Theming user preferences (`~/.config/orun/theme.yaml`).
