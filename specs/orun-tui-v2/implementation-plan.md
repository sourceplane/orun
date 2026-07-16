# Implementation plan: orun-tui-v2 (TR0–TR9)

Sequencing principle: kernel before pixels, pixels before surfaces, surfaces
before cloud. Every milestone lands as one or more PRs to `main` behind the
`orun tui --next` / `ORUN_TUI=next` gate; the old cockpit stays default and
untouched until TR8. Milestones TR3–TR7 are surface work over a frozen kernel
and can proceed in parallel once TR2 lands (TR3 first — it derisks the most).

---

## TR0 — The kernel

**Goal.** `internal/tui2/{shell,frame,store}`: router + drill stacks +
overlay stack + focus model + command bus; fixed-dimension `Box` compositor
with region memoization and the animation scheduler; revisioned `AppStore`;
the profiling harness promoted from the uncommitted `internal/tui/profile.go`
into `tui2/frame/profile`; `make tui-bench` with the §11 budgets wired into
CI.

**Deps.** None (pure new code + Bubble Tea v1).

**Done when.** A demo binary (`orun tui --next`) runs three stub surfaces
with palette, help, drill/pop, and an inspector drawer; property tests prove
frame-size invariants over arbitrary message/size sequences; the bench shows
0 idle frames/sec and region-only tick renders; no `ClearScreen`, `clipBox`,
or `fitToScreen` equivalents exist anywhere in `tui2`.

## TR1 — Northwind Mono

**Goal.** `internal/tui2/design`: tokens sourced from
`internal/cockpit/style.DefaultPalette` (kept as the single reskin point),
the closed tone set mirroring the console's `Tone` vocabulary, and the
component kit — status line, header band, pills/status text, data rows,
tables, filter chips, drawers, dialogs (shared confirm), palette, list/detail
scaffold, markdown-lite renderer, stat tiles.

**Deps.** TR0.

**Done when.** Every component has golden frames at 80/120/220 cols in
light/dark; a `tui2` gallery surface (the terminal `/demo`) renders the kit;
the animated wordmark and toast components from v1 have no successors.

## TR2 — The data plane

**Goal.** `internal/tui2/data`: the `Source` interface;
`LocalSource` over `cockpit/bridge`/`objmodel` with fs-watch subscriptions
(`.orun/refs`, `.orun/objectmodel/refs`, `.orun/agents/live/`, debounced);
`MockSource` with seeded fixtures + scripted deltas; **step-level runner
hooks** (`BeforeStep`/`AfterStep` in `internal/runner.RunnerHooks`) and their
translation into streamed run events; merged provenance-tagged slices in the
store.

**Deps.** TR0. The runner-hook change is a standalone PR (it also benefits
`status --watch`).

**Done when.** Stream chaos tests pass (disconnect/replay/no-clobber); a
live run in the demo surface shows step transitions with **no** disk polling
(verified by the bench: zero `loadRunDetail`-style reads during steady-state
streaming); the 3 s ticker exists only as the watcher-failure fallback and is
covered by a test that kills the watcher.

## TR3 — Agents

**Goal.** Surface 3 on the new kernel: sessions list (local lane),
conversation head over attach v1 (`attach.SocketClient`), `tui2/agentfold`
with parity goldens against the shared fixtures, collapsible tool cards,
sticky approval cards → `verdict` frames, composer (steer/interrupt/detach),
launch overlay on the command bus with fs-watch-based readiness (retiring
the 20 ms sleep poll), presence line.

**Deps.** TR1, TR2. Reuses AL0–AL5 machinery unchanged.

**Done when.** Fold parity CI job passes against the golden fixtures; a
stub-driver session is launched, steered, approved, detached, and re-attached
entirely in `tui2`; delta streaming renders append-only (bench-verified);
escape-byte fuzzing of the conversation renderer passes.

## TR4 — Activity

**Goal.** Surface 4: runs feed with status facets → run detail → job →
step → log leaf, all stream-driven (TR2 step events + coalesced log batches
with generation ids carried over); the v1 Run Dashboard, Log Explorer, and
History surfaces retire into this one drill hierarchy; DAG view as an
alternate run-detail lens.

**Deps.** TR1, TR2.

**Done when.** A live `RunPlan` streams job+step progress end to end with
zero polling; pinned run details survive background list refreshes (property
test); log tail follows/filters at 10k lines without frame overrun.

## TR5 — Catalog

**Goal.** Surface 5: entity explorer (kind facets, per-kind tables), entity
detail with relation edges, entity docs via markdown-lite, and the component
work surface — change overlay, last-run status, and the **Compose flow**
(intent → plan preview → DAG → dry-run → run) absorbing Plan Studio as a
drill + palette verb rather than a place.

**Deps.** TR1, TR2 (TR4's run detail for post-run handoff).

**Done when.** Every v1 catalog/plan-studio capability has a `tui2`
equivalent reachable from the component row or the palette; compose → run
hands off into the Activity drill; goldens cover all ten entity kinds.

## TR6 — Work

**Goal.** Surface 6 (nav position 2): local lane from sealed epic/spec
snapshots (`.orun/{epics,specs}`, `worklens`); cloud lane (behind the TR8
flag but built here against `MockSource`/recorded fixtures) — items by rung,
item detail with timeline, relations, health, review verdicts (render +
reply), session↔work deep links both directions.

**Deps.** TR1, TR2; cloud lane consumes `remotestate/work.go` contracts.

**Done when.** A work item links to its sessions and back; rung lanes render
the derived lifecycle identically to console fixtures; verdict reply posts
through the same contract the console uses (verified against recorded
fixtures).

## TR7 — Home, palette, Events

**Goal.** Surface 1 (stat tiles, needs-attention fold, latest activity),
surface 6→Events explorer (local execution/session events with facets), the
full command registry (every action across surfaces), palette v2 with
fuzzy match + deep-link commands + console deep-links for out-of-scope
surfaces, help generated from the registry, settings overlay (scope, theme,
prefs).

**Deps.** TR3–TR6 (it aggregates them).

**Done when.** Every capability in `tui2` is invocable from the palette and
listed in help with its binding; the attention fold deep-links into sessions
with the approval card focused; `?` contains zero hand-written entries.

## TR8 — Cloud connect + default flip

**Goal.** `CloudSource` live: in-app device-flow sign-in (reusing
`internal/cliauth`), scope picker, platform reads (org runs, catalog,
events, attention), work SSE with cursor resume, remote sessions via
`relay_head` (SSE) in the same Agents list, presence, the degradation law
(§10) end to end. Flip the default: bare `orun` launches `tui2`;
`ORUN_TUI=legacy` keeps the old cockpit as the escape hatch.

**Deps.** TR3–TR7; a stage org token for live smoke (everything else runs
against recorded fixtures — the fixture-first discipline from AL applies).

**Done when.** Sign in → scope pick → org lanes appear in Home/Work/Agents/
Activity/Events without any surface changing shape; kill the network
mid-session → status flips to `⏺ offline`, local lanes untouched, reconnect
resumes streams by cursor (chaos test); stage smoke checklist (per
`stage-verification-setup` runbook) passes; default flipped.

## TR9 — Cutover

**Goal.** Delete `internal/tui`; rename `tui2`→`tui`; retire
`ORUN_TUI=legacy`, the legacy `Panel`/keymap shims, and `MockOrunService`;
docs + website update; perf regression gates made blocking; narrow-terminal
(≤80 col) and `NO_COLOR`/no-alt-screen degradation pass.

**Deps.** TR8 soaked (at least one release with the flag flipped).

**Done when.** `internal/tui` is the new implementation with no `2` suffix
anywhere; total idle CPU of `orun` at rest is 0 (measured); the §11 budget
table in CI is green and blocking; release notes ship the migration note.

---

## PR conventions

- Prefix: `tr0:`, `tr1:` … in PR titles; one milestone may span several PRs.
- Every PR that touches rendering carries goldens; every PR that touches the
  data plane carries a chaos or property test.
- The runner-hook PR (TR2) and the profiling-promotion PR (TR0) are
  standalone and reviewable without any TUI context.
