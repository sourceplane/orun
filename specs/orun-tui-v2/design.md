# Design: orun-tui-v2 — one product, two densities

## 1. The problem, with evidence

The current cockpit (`internal/tui`, 17,465 LOC) has four compounding failure
classes. None of them are cosmetic; each traces to an architectural choice
that a patch cannot fix.

**1a. Rendering fights the framework.** Bubble Tea line-diffs the frame; the
cockpit's frame is not dimensionally stable, so the diff leaves residue. The
codebase answers with three stacked mitigations — `clipLines`/`clipBox`
(`model.go:1855`), `fitToScreen` (`model.go:1662`, "the final guarantee
against alt-screen residue"), and batched `tea.ClearScreen` on drilldown
transitions (`model.go:1150`, `model.go:1162`) — plus `propagateSize` handing
children `w-2` to preempt lipgloss soft-wrap inflation. Git history reads
"stabilize the live step-log view — ghost rows", "frame stability". These are
symptoms of layout that does not guarantee its own dimensions.

**1b. The idle cockpit burns CPU repainting nothing.** Profiling
(`ORUN_TUI_PROFILE`, captured in `tui-profile.ndjson`) shows `View()` costs
2.5–8.4 ms per frame while `Update()` costs 5–210 µs — a ~100× skew — and the
costly frames are *idle animation ticks*: `spinner.TickMsg` (~10/s) and
`brandTickMsg` (2.5/s) each rebuild and re-clip the full ~18 KB screen to
advance one glyph and a gradient phase. `View()` also re-runs
`applyResponsive` + `propagateSize` every render, and `chromeHeight()`
*renders the chrome* just to measure it. Nothing is memoized.

**1c. The data plane polls what it could stream.** The workspace reloads
wholesale every 3 s (`refreshInterval`); live step progress is re-read from
disk every 600 ms because the runner only emits job-level events
(`model.go:2403`); every `RunEventMsg` additionally triggers a full run-detail
disk read. The `runDetails` map must be defensively re-applied after every
list rebuild to stop a background refresh clobbering what the user is viewing
(`model.go:2307`) — a hand-rolled guard around a known race.

**1d. The product drifted from the product.** Seven internal modes under
three tabs, `ModeComponentStudio` aliased to `ModePlanStudio`, inert legacy
keybindings retained so tests compile, five independent overlay booleans each
with their own routing branch. Meanwhile the console shipped Work, attention,
presence, an org-wide activity feed, and a named design language — over the
same state store the TUI already reads. The terminal user gets none of it,
and what they do get uses different names for the same things.

## 2. Goals / non-goals

**Goals**

1. A terminal head with **feature and vocabulary parity** with orun cloud
   wherever the entity exists locally, and cloud lanes for the rest when
   online.
2. The **interaction grammar of Claude Code**: calm chrome, composer-first
   conversations, esc-dismisses-everything, a palette as the universal escape
   hatch, motion only when something is live.
3. **Structural elimination** of the four failure classes above — not
   mitigation: fixed-dimension frames by construction, memoized rendering,
   stream-driven data, one navigation model.
4. Measured performance budgets enforced in CI (§11).
5. A staged, reversible cutover: the old cockpit remains shippable until the
   new one is demonstrably better on every axis.

**Non-goals**

- Remote plan **execution** from the TUI (reads and live tails yes; dispatch
  is a follow-on — §14).
- Any orun-cloud change. This epic consumes shipped cloud contracts only.
- Porting console code. Parity is contractual (fixtures, vocabulary, tones),
  not literal.
- Mouse-first interaction. Mouse works where free (scroll, click-to-focus),
  but every capability is keyboard-reachable.
- A general-purpose TUI framework. `tui2/frame` is built for this program.

## 3. Product thesis: one store, two heads

Both heads fold the same records: the content-addressed object graph
(`internal/nodes` kinds) locally, its Postgres projection in the cloud. The
console renders it at browsing density with a serif editorial voice; the TUI
renders it at operating density with a monospace grid. The invariant that
makes this cheap is already shipped: **sessions speak attach v1 to any head**
(`internal/agent/attach`, golden fixtures shared byte-identically with
`packages/contracts/src/agents-attach.ts`), and **the event vocabulary is
closed** (11 session-event kinds; the cloud console's conversation fold and
the TUI's must produce the same turn structure from the same stream).

Three consequences drive everything below:

- **Connection is a status, not a mode.** There is no "cloud mode". Every
  surface renders from local state unconditionally; an authenticated online
  session *adds lanes* (org work items, remote sessions, attention, org
  activity) into the same lists, tagged with provenance. Loss of connectivity
  removes the lanes and marks the status line — nothing else changes.
- **Vocabulary is imported, not invented.** Workspace, Work, rung, Agents,
  session, Activity/run, Catalog, attention, routine, profile — the TUI uses
  the console's nouns (`nav-items.ts`, `work.ts`, `agents` contracts) so a
  user moving between heads never translates.
- **The session is the durable thing; every surface is a client.** The TUI
  never runs the agent loop; it attaches (AL decision, reaffirmed). Likewise
  it never owns run state; it subscribes.

## 4. Design language: Northwind Mono

The console's design system is Northwind (`docs/northwind-design.md`):
editorial serif display, sans UI, mono refs, a closed tone vocabulary, calm
whitespace. Northwind Mono is its terminal projection — the same temperament
under monospace constraints. It lives in `internal/tui2/design` and is the
only place styles are defined.

**4a. Temperament.** Calm, editorial, precise. Whitespace over borders: at
most one border weight on screen (a muted rule between chrome and stage);
panes separate by spacing and dim rules, never nested boxes. Color is
information, not decoration: the closed tone set — `neutral`, `info`,
`success`, `warning`, `error`, `live` — maps 1:1 to the console's `Tone`
type and is the *only* semantic color channel. Identity color (the brand
accent) appears in exactly two places: the wordmark glyph and the active
surface indicator. The animated gradient wordmark is retired.

**4b. Motion policy — motion means work.** A closed set of animations, one
scheduler (§7):

| Animation | When | Frame cost |
|---|---|---|
| Braille spinner on the status line | ≥1 in-flight operation | region-only |
| `live` pulse dot on session/run rows | entity is running | region-only |
| Streaming text append in conversation | delta frames arriving | append-only |
| Progress bar in run rows | run executing | region-only |

Nothing else moves. An idle cockpit produces **zero frames**. There is no
brand tick, no ambient shimmer, no toast slide-ins (toasts are a one-line
status-line notice with a timeout).

**4c. Layout grammar.** Three fixed bands:

```
 orun   Home  Work  Agents  Activity  Catalog  Events  Secrets      acme/platform
 ────────────────────────────────────────────────────────────────────────────────
                                                                                 
   [stage — one surface, owns everything between the rules]                      
                                                                                 
 ────────────────────────────────────────────────────────────────────────────────
 ⣾ deploying checkout · 2 sessions live · 1 needs you        ⏺ cloud   :cmd ?help
```

- **Header (1 line):** static wordmark, the seven surfaces (active one
  accented), right-aligned scope (`org/project` or `local`).
- **Stage:** exactly one surface. Surfaces may split internally (list +
  detail) on wide terminals, single-column with drilldown below 100 cols.
  A right inspector drawer (`i`) overlays rather than reflows when narrow.
- **Status line (1 line):** left — current activity + attention summary;
  right — connection status (`⏺ local` / `⏺ cloud` / `⏺ offline` with tone),
  hint pair. This is Claude Code's status line role: the single place the
  system talks about itself.
- **Overlays:** palette, help, dialogs, confirmations render on a proper
  overlay stack (§6), dimming the stage. `esc` pops one level, always.

**4d. Typography in a grid.** Hierarchy through weight and tone, not size:
titles bold, metadata dim, refs/digests in the mono accent used for `code`.
Tables use the console's data-row pattern: primary cell + dim secondary line,
right-aligned status pill. Timestamps are relative with absolute on focus.

**4e. Interaction grammar (the Claude Code contract).**

| Key | Meaning — everywhere |
|---|---|
| `1`–`7` | Jump to surface |
| `ctrl+k` or `:` | Command palette (every action lives here; keys are shortcuts *into* it) |
| `/` | Filter the list in focus, in place |
| `enter` / `esc` | Drill in / back out (esc also pops overlays; never quits) |
| `tab` | Cycle focus between a surface's panes |
| `i` | Inspector drawer for the focused entity |
| `?` | Help overlay (generated from the palette registry — never hand-maintained) |
| `ctrl+c` | Quit guard (double-press), Claude Code style |
| `g` | Contextual generate/compose (on a component: compose flow) |

Rules: no single-letter global actions that could collide with a focused text
input; when a composer has focus, only chorded keys act globally. Every
destructive or execution action confirms through one shared dialog component
with the action named in the button (`Run 4 jobs`, not `OK`).

## 5. Information architecture: seven surfaces

Mapped 1:1 onto the console's workspace nav, minus surfaces that are
meaningless locally (Teams, Billing, Integrations — the palette deep-links
those to the console instead).

| # | Surface | Console analog | Content (local lane) | Added when online (cloud lane) |
|---|---|---|---|---|
| 1 | **Home** | Overview | Greeting-free, dense: stat tiles (components, live sessions, last run), **Needs attention**, **Latest activity**, current scope | Org-wide attention queue, org activity, quota notices |
| 2 | **Work** | Work | Sealed epic/spec snapshots (`.orun/{epics,specs}`), tasks linked to sessions | Full work plane: items by rung, triage count, item detail + timeline, review verdicts (read + verdict reply) |
| 3 | **Agents** | Agents | Live local sessions (registry), agent types, launch flow, **the conversation head** | Remote sessions via relay head, presence, attention items, profiles/routines (read) |
| 4 | **Activity** | Activities | Runs feed (newest first, status facets) → run → jobs → steps → logs; live runs stream | Org runs feed (`remotestate/platform`), remote run detail + logs |
| 5 | **Catalog** | Catalog + Docs | Entity explorer by kind, entity detail with relations, component work surface (compose/plan/run), entity docs (markdown-lite) | Org catalog projection, scorecards/maturity, rendered docs |
| 6 | **Events** | Events | Local execution events (`ExecutionEvent`, agent session events) | Org event bus with facets and correlation groups |
| 7 | **Secrets** | Secrets | Secret chain refs, `secret://` resolution status, orphaned brokered secrets | Rotation health, scope policies (read) |

Settings (scope, auth, theme, prefs) is an overlay (`,` or palette), not a
surface — matching its "Manage" demotion in the console.

**Navigation model.** Flat surfaces + within-surface drill stacks. Each
surface owns a `[]route` stack (e.g. Activity: `feed → run → job → step`);
`enter`/`esc` push/pop; the global back/forward history spans surfaces. No
aliases, no inert bindings, no mode/tab mismatch: **a surface is a tab is a
route**. Deep links are addressable strings (`activity/run/01J…/job/build`)
used by the palette, the attention queue, and session↔work links — the
terminal equivalent of the console's URL-driven nav.

**What the old modes become:** Catalog → surface 5. Plan Studio → the
**Compose flow**, a drill under a component (and palette verb `compose`),
not a place. Run Dashboard + Log Explorer + History + Activity → surface 4
(one drill hierarchy; the log viewer is the leaf). Agent → surface 3.

## 6. The shell kernel (`tui2/shell`)

One Bubble Tea program, but the root model is a thin dispatcher, not a god
object. The kernel owns exactly four things:

- **The router.** `Route` = `{Surface, Path []string}`. Surfaces implement
  `Surface interface { Init, Update, View(Region), Routes }`. The router owns
  the drill stacks and the global history. Surfaces are pointers, not copied
  values.
- **The overlay stack.** `[]Overlay`, LIFO. Palette, help, dialogs, launch,
  confirmations are all `Overlay`s: top gets keys first, `esc` pops, painting
  composes over the stage. The five booleans die; precedence bugs become
  impossible by construction.
- **The focus model.** One focus owner at a time (a pane or an overlay). Key
  routing is: overlay top → focused pane → surface → global. A focused text
  input swallows unchorded printables — enforced in the kernel, not per view.
- **The command bus.** Every user-visible action is a `Command{ID, Title,
  Scope, Run}` registered in one registry. The palette lists it, `?` renders
  it, keybindings reference command IDs, and tests invoke commands directly.
  (The current palette dispatches strings into a switch; the registry
  inverts that.)

**The store (`tui2/store`).** All shared state lives in one `AppStore` of
revisioned slices (`Catalog`, `Runs`, `Sessions`, `Work`, `Connection`, …).
Slices are updated only by reducer messages carrying data fetched in
`tea.Cmd`s — **`Update` never performs I/O** (today `RunPlan` and `TailLogs`
are called inside the reducer). Each slice carries a monotonic `rev`;
renderers memoize on it (§7). Detail views the user is inspecting pin their
entity by ref, so a background refresh can never clobber a drilldown — the
`runDetails` re-apply hack dies here.

## 7. The frame layer (`tui2/frame`)

The part that makes the bug classes of §1a/§1b structurally impossible.

**Fixed dimensions by construction.** The unit of rendering is a `Box`:
`Render(size Size) Cell` where the contract — enforced by a debug-build
assertion and property tests — is that output is *exactly* `size`. Layout is
computed once per resize (splits, bands, drawer widths) and produces a box
tree with concrete sizes. Children can't inflate the frame, so `clipLines`,
`clipBox`, `fitToScreen`, and the `w-2` soft-wrap defense have nothing to do
and don't exist. `ClearScreen` is never issued after startup.

**Region memoization.** Each box caches its last render keyed by
`(store revs it declares, size, focus)`. The root frame is the concatenation
of cached regions; an idle spinner tick re-renders one status-line cell
region and reuses everything else. Target: idle-tick `View()` under 250 µs
vs today's 5–8 ms — and truly idle produces no tick at all.

**The animation scheduler.** One ticker, owned by the frame layer, running
only while ≥1 registered animator exists (a live run, a streaming session, an
in-flight op). Animators invalidate their own region. When the set empties
the ticker stops — idle CPU is zero by design, not by tuning.

**Chrome measured, not rendered.** Band heights are constants (1/rule/…);
`chromeHeight()`-by-rendering disappears.

**Profiling is a first-class citizen.** The uncommitted `profile.go`
harness (NDJSON per-frame `update_us`/`view_us`/`view_bytes`) is promoted
into `tui2/frame/profile`, kept behind `ORUN_TUI_PROFILE`, and extended with
region cache hit-rates. `make tui-bench` replays recorded msg scripts through
the kernel and asserts the §11 budgets — perf regressions fail CI, not users.

## 8. The data plane (`tui2/data`)

One interface, three implementations:

```go
type Source interface {
    Snapshot(ctx, Query) (Snapshot, error)      // point-in-time read
    Subscribe(ctx, Topic) (<-chan Delta, error) // cursor-resumable stream
    Capabilities() Caps                          // what this source can do
}
```

- **LocalSource** wraps `internal/cockpit/bridge` + `objmodel` for reads.
  Subscriptions come from **fs-watch** (fsnotify on `.orun/refs`,
  `.orun/objectmodel/refs`, `.orun/agents/live/`, debounced ~100 ms) — the
  3 s workspace ticker becomes a degraded fallback used only if the watcher
  fails. Run topics come from runner hooks (below); session topics from
  attach v1 heads.
- **CloudSource** wraps `internal/remotestate`: platform reads (runs, work,
  catalog, events, attention), the work SSE stream
  (`GET …/work/events/stream?from=seq`), and relay heads for remote
  sessions. All topics are cursor-resumable (seq-based), so reconnects
  replay-then-follow — the same discipline attach v1 already has.
- **MockSource** replaces `MockOrunService`: seeded fixtures + scripted
  deltas, powering every test through TR7.

The kernel composes Local + Cloud into merged, provenance-tagged slices;
surfaces never know which lane a row came from beyond its badge.

**Closing the streaming gap (runner work, small but load-bearing).**
`internal/runner.RunnerHooks` grows `BeforeStep`/`AfterStep` alongside the
existing job-level hooks, and the TUI's run service translates them into
`RunEvent`s. This kills the 600 ms working-tree poll *and* improves
`status --watch` for free. The events remain advisory (rendering only); the
sealed run record stays the source of truth.

**Backpressure and coalescing** stay as shipped: log batches coalesce (≤256
lines/frame), streams carry generation ids, per-head queues bound with
`bye{lagged}`. These patterns survive the rewrite verbatim — they're the part
of the old cockpit that was right.

## 9. The Agents surface — the same head, better rendered

AL0–AL5 shipped the hard parts; this epic re-renders them.

- **Sessions list:** local live sessions (registry) and — online — org
  sessions from the cloud lane, one list, provenance-badged, with `live`
  pulse dots, run-kind, work-item link, and tokens/cost when known.
- **The conversation head** is a Claude Code session in the terminal:
  streaming turns (delta frames render append-only), collapsible tool cards
  (collapsed by default, `ctrl+o` expands), note lines for harness events,
  **sticky approval cards** above the composer answered by keystroke
  (posting attach-v1 `verdict` frames), an always-on composer (steer queues
  at the turn boundary), `esc` interrupts with the standard guard, detach
  leaves the session running. Presence line shows other attached heads.
- **The fold is contract-tested.** `tui2/agentfold` folds the 11-kind event
  stream into turns exactly as the console's `lib/agents/conversation.ts`
  does; both fold the *shared golden fixtures* and must agree on turn
  boundaries, attribution, and approval placement. Divergence is a CI
  failure — this is what "appears close to orun cloud" means, mechanically.
- **Launch** is an overlay on the command bus: agent type → driver →
  task/work-item link → brief preview → spawn detached body → attach. The
  2 s `time.Sleep` poll loop is replaced by watching `.orun/agents/live/`
  through the same fs-watch subscription everything else uses.
- **Attention** items for sessions (awaiting approval, failed-retryable,
  stuck) surface on Home and in the status-line count; selecting one
  deep-links into the session with the approval card focused.

## 10. Cloud connect

- **Sign-in is in-app.** The status line shows `⏺ local`; the `sign in`
  command runs the device flow (`internal/cliauth` + the RFC-8628 endpoints):
  the TUI displays code + URL, polls, and stores the rotating credential
  exactly as `orun login` does — one credential store, CLI and TUI
  interchangeable. Scope (org/project) is a picker fed by platform reads,
  persisted in prefs, switchable from the palette.
- **What lights up** is exactly the cloud lanes in §5's table — added rows
  and panes, never a different screen.
- **The degradation law:** any cloud lane may vanish at any moment
  (offline, 401, lease lost). Vanishing must (a) never disturb local lanes,
  (b) mark the status line (`⏺ offline · retrying`), (c) resume by
  cursor-replay on reconnect, and (d) never lose composed-but-unsent input.
  Auth expiry surfaces one non-blocking notice with a `sign in` action —
  never a modal wall.
- **Trust boundary.** The TUI renders remote content (work items, session
  text) as *data*: markdown-lite rendering neutralizes control sequences;
  terminal escape bytes in any streamed content are stripped at the data
  plane (this is a real injection surface for a terminal app — §12 tests it).

## 11. Performance budgets (CI-enforced via `make tui-bench`)

| Metric | Budget | Today |
|---|---|---|
| Idle frames/sec (nothing live) | **0** | ~12.5 (spinner + brand ticks) |
| Animation tick `View()` (region path) | p95 < 250 µs | 5–8 ms full repaint |
| Full-frame render, 220×60 | p95 < 2 ms | 2.9–8.4 ms |
| `Update()` any message | p95 < 200 µs, **no I/O** | up to 210 µs, with sync I/O paths |
| Keystroke → echo (composer) | < 16 ms | unmeasured |
| Resize reflow | < 10 ms | unmeasured |
| Startup to first frame (warm store) | < 150 ms | unmeasured |

Region cache hit-rate is recorded by the profiler; the bench fails if an
idle-tick replay renders more than the status-line region.

## 12. Quality strategy

- **Golden frames.** Every Northwind Mono component and every surface state
  renders against `MockSource` fixtures into checked-in goldens (teatest-
  style), light/dark and 80/120/220 col widths.
- **Frame invariants as property tests.** For arbitrary message sequences at
  arbitrary sizes: rendered height == terminal height, width never exceeded,
  no `ClearScreen` emitted, esc from any state pops exactly one level.
- **Fold parity.** `tui2/agentfold` vs the shared attach-v1/session-event
  golden fixtures (the same files the cloud contract tests use).
- **Stream chaos tests.** Subscriptions under disconnect/reconnect/lag:
  cursor resume, no duplicate rows, no clobbered pinned details.
- **Escape-byte fuzzing** of every render path fed by remote data (§10).
- **Perf bench** in CI (§11).
- Tests target the command bus and store, not key-event simulation, except
  for the routing property tests — that's what makes the suite survive
  design iteration, which the old suite (inert legacy bindings kept "so
  tests compile") did not.

## 13. Invariants

1. A frame is always exactly terminal-sized; no post-hoc clipping exists.
2. `Update` never blocks: no I/O, no sleeps, no synchronous service calls.
3. An idle cockpit renders zero frames and holds zero tickers.
4. Every user-visible action is a registered command; palette and help are
   generated, never hand-maintained.
5. `esc` always dismisses/pops and never quits; `ctrl+c` always quit-guards.
6. Connection is a status: no surface changes identity when online/offline
   flips; cloud lanes only add and remove rows.
7. The TUI never writes session or run state except through the same seams
   the CLI uses (attach inputs, runner, refs). Heads stay ephemeral.
8. The session-event and attach vocabularies do not grow in this epic.
9. Remote-originated text is rendered inert (no control-sequence
   passthrough).
10. The old cockpit remains buildable and default until TR8 flips it.

## 14. Sharpness register / follow-ons

- **Remote execution.** `RunPlan` against the cloud coordination loop from
  the TUI (today explicitly rejected). The `Source.Capabilities()` seam and
  the confirm-dialog grammar are designed so this drops in later without
  surface changes.
- **Bubble Tea v2.** The frame layer isolates the renderer contact surface;
  evaluating v2's compositor becomes a `tui2/frame`-internal experiment.
- **Routines/budgets authoring** from the terminal (read-only in TR8).
- **Notifications when no head is attached** (system notify on attention
  items) — pairs with the cloud notification plane.
- **A `--plain` accessibility mode** (no alt-screen, line-oriented output)
  sharing the same command bus — the TR9 narrow-terminal pass lays the
  groundwork.
- **Docs rendering depth** — TechDocs parity beyond markdown-lite (tables,
  admonitions) if catalog docs usage grows.
