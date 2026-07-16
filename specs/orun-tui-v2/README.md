# Spec: orun-tui-v2 — the cockpit, rebuilt as the terminal head of orun cloud

**The current cockpit works, but it fights its own renderer and drifts from the
product.** `internal/tui` is 17.5k lines around a 2,481-line root model, seven
internal modes squeezed under three tabs, five independent overlay booleans,
three layers of manual frame clipping plus forced `ClearScreen`s to stop
alt-screen ghosting, and four polling tickers — including a 600 ms disk poll
for live step progress and a 5–8 ms full-screen repaint on every idle spinner
tick (measured; see `design.md §1`). Meanwhile orun cloud shipped a coherent
product surface (Overview, Work, Agents, Activities, Catalog, Events) over the
*same* state store, with a named design system (Northwind), a closed event
vocabulary, and an attach protocol explicitly built to be rendered by two
heads. This epic rebuilds the TUI as that second head: **a power-user terminal
client with the interaction grammar of Claude Code and the information
architecture of orun cloud**, local-first, cloud-lit when online.

## Status

| Field | Value |
|-------|-------|
| Status | **Draft** — authored, ready for review; open decisions in `risks-and-open-questions.md` |
| Cluster | **TR** (TUI rebuild — orun-owned end to end; consumes cloud contracts read-only) |
| Ground truth | `internal/tui` as-built (frozen during the rebuild), `internal/cockpit` (viewmodel/bridge/watch), `internal/agent/attach` (attach v1, shipped AL0–AL5), `internal/remotestate` (platform reads, work SSE), orun-cloud `apps/web-console-next` (Northwind, nav model, conversation fold) |
| Builds on | `orun-agents-live` AL0–AL5 (the attach plane and the interactive head — reused byte-identically) · `orun-work` (work item contracts) · `orun-object-model` (the content-addressed store the surfaces read) · cloud `packages/contracts` (event vocabulary, attach-v1 golden fixtures, tone vocabulary) |
| Runtime | New package `internal/tui2`, gated behind `orun tui --next` / `ORUN_TUI=next` until TR8, default at TR8, old package deleted at TR9 |
| Decisions locked | (1) **Rebuild, not refactor** — a fresh kernel in `internal/tui2`; the old cockpit stays untouched and shippable until the default flips. (2) **Parity by contract, not shared code** — the TUI mirrors cloud vocabulary, surface structure, event folds, and status tones; parity is enforced by shared golden fixtures, not by porting TypeScript. (3) **One store, two heads** — every surface renders the same entities the console renders; being online is a *status*, never a *mode*: surfaces gain cloud lanes when authenticated and degrade to local silently. (4) **Frame discipline by construction** — every region renders into a fixed-size box; regions are memoized by (state revision, size); one animation scheduler that ticks only while something is genuinely live; no clip layers, no forced `ClearScreen`. (5) **Streams over polls** — fs-watch on `.orun` refs, step-level runner events (new), attach v1 for sessions, work SSE when online; tickers survive only as degraded fallbacks. (6) **Claude Code grammar** — calm chrome (one header line, one status line), `esc` always dismisses, a command palette is the escape hatch for everything, agent sessions are composer-first conversations, motion always means work. (7) **Bubble Tea v1 stays** — we own frame composition in `tui2/frame`; a Bubble Tea v2 migration is a contained follow-on, not a dependency. |
| Milestone prefix | **TR** |
| Gate | Human-independent through TR7 (MockSource + recorded fixtures). TR8 needs a stage org token for live cloud smoke; everything else runs offline. |

## The one-paragraph thesis

orun already made the expensive architectural bet: state is a content-addressed
object graph, sessions are append-only event logs, and the attach protocol was
designed so that "the TUI head" and "the console head" are one protocol
rendered two ways. The cloud console cashed that bet in — it has a real
product shape, a design language, and live planes. The terminal never did: the
cockpit accreted modes, tickers, and clipping hacks instead. The cheapest path
to a first-class terminal product is not to patch the accretion but to rebuild
the head on the seams that already exist — `cockpit/bridge` for reads,
`attach` for sessions, `remotestate` for the cloud — under a kernel that makes
the old bug classes (ghost rows, frame oscillation, stale clobbers, idle-burn
repaints) structurally impossible, and a surface map that a cloud user
recognizes instantly. When it lands, `orun` in a terminal and orun cloud in a
browser are the same product at two densities — and the terminal one is the
faster of the two.

## What changes for a user

| Today (`internal/tui` as-built) | After this epic |
|---|---|
| Three tabs hiding seven modes; Plan Studio, Run Dashboard, Log Explorer, History reachable through side doors | Seven surfaces matching cloud nav: **Home · Work · Agents · Activity · Catalog · Events · Secrets** — number keys, palette, same names as the console |
| Idle cockpit burns 5–8 ms per spinner frame repainting a static screen; animated gradient wordmark always on | Idle cockpit renders **nothing** (zero frames/sec when nothing is live); motion appears only while work is happening |
| Live step progress polled off disk every 600 ms; workspace reloaded every 3 s | Step events streamed from the runner; catalog refresh driven by fs-watch on `.orun/refs`; no polling in the happy path |
| Occasional ghost rows, duplicated footers, flicker on drilldown (papered over with `ClearScreen`) | Fixed-dimension frame by construction; property-tested "frame height == terminal height, always" invariant |
| Cloud is read-only bolt-on (`--remote-state` history reads; `RunPlan` rejects remote; no login in-app) | Sign in from the status line; Work, attention queue, remote agent sessions, org activity light up; presence shows who else is attached |
| Work items invisible in the terminal | A Work surface: items, rungs, timelines, session links — the same derived lifecycle the console shows |
| No "needs you" aggregation | Home shows the attention queue: approvals, parked routines, failed sessions — answerable in place |

## Read order

1. This README.
2. [`design.md`](./design.md) — the evidence, the design language (Northwind
   Mono), the surface map, the kernel, the data plane, cloud connect,
   performance budgets, invariants.
3. [`implementation-plan.md`](./implementation-plan.md) — TR0–TR9 with
   goals, dependencies, and "done when".
4. [`risks-and-open-questions.md`](./risks-and-open-questions.md).
5. [`IMPLEMENTATION-STATUS.md`](./IMPLEMENTATION-STATUS.md) — living status.

## Milestones at a glance

| ID | Milestone | Status |
|----|-----------|--------|
| TR0 | The kernel: `tui2/{shell,frame,store}` — router + overlay stack, fixed-dim compositor with region memoization + animation scheduler, revisioned store; perf harness promoted from `profile.go`; budgets in CI | ☐ |
| TR1 | Northwind Mono: the terminal design system — tokens from `cockpit/style`, components (status line, pills, tables, drawers, dialogs, palette, markdown-lite), golden frames | ☐ |
| TR2 | The data plane: `tui2/data.Source` (Local/Mock), fs-watch subscriptions replacing the 3 s ticker, **step-level runner events** replacing the 600 ms disk poll | ☐ |
| TR3 | Agents: sessions list + conversation head over attach v1 (fold parity goldens vs cloud fixtures), approvals, composer, launch dialog | ☐ |
| TR4 | Activity: runs feed + run detail (jobs → steps → logs), fully stream-driven; log explorer folded in | ☐ |
| TR5 | Catalog: entity explorer + detail + component work surface (compose/plan/run — Plan Studio folded in as the Compose flow) | ☐ |
| TR6 | Work: items, rung lanes, item detail with timeline, session↔work links | ☐ |
| TR7 | Home + palette + Events: overview, attention queue, latest activity; full command registry; events explorer | ☐ |
| TR8 | Cloud connect: in-app device-flow login, scope switcher, CloudSource (platform reads, work SSE, attention, remote sessions via relay head, presence); **default flips to v2** | ☐ |
| TR9 | Cutover: delete `internal/tui`, rename `tui2`→`tui`, docs, perf regression gates, narrow-terminal/no-color pass | ☐ |

## Scope boundary

| In scope (this epic) | Out of scope (→ elsewhere) |
|---|---|
| The new kernel, design system, seven surfaces, local data plane, step-event runner hooks, cloud reads + live planes (work SSE, attach relay, attention), in-app login, cutover | Remote **execution** (`RunPlan` against the cloud backend — follow-on, see risks §4); cloud-side changes of any kind (relay, api-edge, console); routines/budgets **authoring** (read-only surfacing only); billing/usage/teams/integrations surfaces (palette deep-links to the console instead); Bubble Tea v2 migration |

## Relationship to existing work

- **`orun-agents-live` (AL0–AL5)** — the agent head this epic ships is the
  *same head* AL3 built, re-rendered under the new kernel. Attach v1, the
  socket client, the relay head, and the launch flow are reused; only the
  rendering and state management change.
- **`internal/cockpit`** — the viewmodel/bridge/watch layer is the read seam
  the new `Source` wraps. It stays shared with the CLI (`status --watch`).
- **`docs/plans/2026-05-29-cockpit-ux-redesign.md`** — the previous UX pass,
  which produced the current three-tab shape. This epic supersedes it.
- **orun-cloud `saas-agents-live`** — the console is mid-migration from 5 s
  polling to the SSE attach stream (AL7/AL8 there). The TUI targets the relay
  directly and should land *ahead* of the console on liveness.
