# Implementation Plan

> Milestone-based. Each states **goal**, **deps**, **done when**. Agents may
> split/merge while keeping each PR reviewable. The model + event log (W0) and
> the sync spine (W1) are the core; the delivery bridge (W2–W3) is the
> differentiation; sealing/pull (W4) and the MCP (W5) open the agent surface;
> the import (W6) makes orun plan orun.
>
> **W0–W4 gate only on what exists** (object model, Orun Cloud backend,
> `objcatalog`/`affected`). Graph unification with the service catalog rides
> SC2 later and is additive by construction (WD-4).

```
W0 model + event log + mutators ─► W1 sync (DO + client protocol) ─► W2 bridge: links + auto-link
                                                                          │
                                                          W3 bridge: gates→Done, Released, drift inbox
                                                                          │
                                              W4 sealing + orun spec pull ─► W5 orun MCP
                                                                          │
                                                              W6 specs/ import (dogfood)
```

---

## W0 — Work model, event log, mutators
**Goal:** the system of record exists: entities, events, projections, one write
path — agent-ready provenance from the first event.
- `internal/work`: envelope/contract/event/principal types per `data-model.md`
  §2–§4, §6; struct-generated schemas (`go generate`, never hand-edit JSON).
- Backend Worker: D1 migration (`data-model.md` §5); mutators for create/edit/
  status/assign/comment/link/contract; event append + projection update in one
  transaction; `actor` mandatory on every event.
- Human-key allocation (`PREFIX-seq`) via the project Durable Object skeleton.

**Deps:** existing backend (`cmd/orun/command_backend.go`). **Done when:** every
mutator appends exactly one event; dropping `work_status` and replaying
`work_events` reproduces it byte-for-byte (invariant 2); an event without an
actor is rejected; ≥90% coverage on `internal/work`; mutator fixtures cover the
closed event-kind set.

## W1 — Sync: Durable Object protocol + client contract
**Goal:** the Linear bar is reachable: ordered events, WebSocket fan-out,
optimistic mutations with rebase.
- Per-project DO: `seq` assignment, WebSocket fan-out, cursor replay on
  reconnect (design §7); mutation verdicts (accept/reject + reason) in one
  structured shape shared with the future MCP.
- A reference TypeScript client (the SaaS UI's contract): normalized store,
  optimistic apply, gap replay from `seq` cursors; conformance fixtures both
  sides replay.

**Deps:** W0. **Done when:** a 2-client soak shows convergent state under
interleaved optimistic mutations + disconnects; p95 server echo < 150 ms on the
soak rig; a rejected mutation rolls back cleanly with the verdict surfaced;
cursor replay after a dropped socket loses nothing (Q-1 throughput numbers
recorded).

## W2 — Delivery bridge I: component links + auto-linking
**Goal:** tasks are graph citizens; PRs find their tasks by themselves.
- `contract.affects` → validated component links (`work_links`, §7 vocabulary);
  unresolved keys degrade visibly (Q-5).
- The auto-linker: PR webhook → `internal/affected` over the diff → open tasks
  claiming those components (+ task-key parse from branch/PR title) →
  `implementedBy` links + In Progress/In Review transitions, all as
  `actor: automation` events.
- Blast radius read API: `affects` closed over `Result.Dependents`, with owner
  attribution for reviewer suggestions.

**Deps:** W1; shipped `internal/affected`/`objcatalog`. **Done when:** a fixture
PR auto-links by component overlap and by key parse; transitions are recorded
with automation provenance and render as such; blast radius matches
`orun catalog affected` for the same diff (parity fixture); no automation ever
attributes to a human (invariant 4 assertion).

## W3 — Delivery bridge II: gate-verified Done, Released, drift inbox
**Goal:** status that cannot rot, including the status nobody else can have.
- Done automation: merge + the contract's `gates` verified green **from orun
  execution truth** (the exact run/check mapping fixed here — Q-3); otherwise
  the task parks in `in_review` with the failing gate surfaced.
- Released automation: the Deployment overlay shows the merge's revision live
  in the target environment → `delivers` edge + Released event.
- Drift inbox: merged PR with no claiming task → triage item naming the
  affected components.

**Deps:** W2. **Done when:** a fixture run-through walks a task
In Review → Done → Released purely from delivery events; a red gate blocks Done
(and a human override is recorded as one); an unplanned merge raises exactly one
drift item; Released is derived only from the Deployment overlay, never from a
deploy attempt (invariant 5).

## W4 — Sealing + `orun spec pull`
**Goal:** the system of proof: epics and ledgers seal content-addressed; agents
pull frozen specs.
- Seal `SpecSnapshot` on epic boundaries and `WorkLedgerSegment` on cursor
  thresholds (`data-model.md` §8) — canonical JSON, org/project routing,
  `refs/work/…`; the `prev` chain on segments.
- `orun spec pull` per `cli-surface.md` §1 (set-difference, read-only
  materialization, `--catalog`, exit codes); `orun work list/view/status` per
  §2.

**Deps:** W0 (model), existing `internal/objremote`. **Done when:** sealing
twice with no intervening events is byte-identical (invariant 6); a snapshot
contains no hot state (invariant 1 assertion); pull fetches only missing
objects (transfer-count fixture); the ledger chain verifies end-to-end; pulled
views are read-only (WD-7).

## W5 — The orun MCP
**Goal:** the agent surface, policy-identical to the UI.
- The MCP server per `agents-and-mcp.md` §2–§3: reads over
  `objcatalog`/`affected`/snapshots/query API; writes through the W0 mutators
  with agent principals + scoped tokens.
- Guardrails in the mutator (§4): no agent-Done, contract-propose flagging,
  scope enforcement, structured verdicts.

**Deps:** W1 (verdict shape), W4 (spec_get). **Done when:** an end-to-end agent
fixture pulls a spec by hash, links a PR, comments, and moves to `in_review`;
every resulting event carries `actor: {type: agent, via: mcp}`; a
`task_update_status(…, done)` from an agent is rejected with a structured
verdict; tool schemas are generated from `internal/work` types (no drift).

## W6 — `specs/` import (dogfood)
**Goal:** orun plans orun: the repo's spec tree becomes the first project.
- `orun work import` per `cli-surface.md` §3: epic READMEs → epics (doc
  verbatim, Q-4); `implementation-plan.md` milestones → tasks with contracts;
  `IMPLEMENTATION-STATUS.md` rows → status events (`actor: automation`,
  `via: import`); initial `SpecSnapshot` per epic.
- Import this repo's live epics (`orun-work` itself included) into the dev
  backend; record divergences as fixtures.

**Deps:** W4. **Done when:** `--dry-run` over `specs/` maps every current epic
without loss (golden fixture); imported docs round-trip byte-identical; the
imported `orun-work` epic's own milestones render as agent-ready tasks; the
team stops editing `IMPLEMENTATION-STATUS.md` for new epics (the projection
replaces it — design §6.4).

---

## Cross-cutting (every milestone)

- **CR-1 complement** — no hot work state in any object blob; no object id in
  any hot row except sealed-snapshot bookkeeping (invariant 1).
- **One write path** — UI, MCP, CLI, automation share the W0 mutators; a second
  write path is a rejected PR (invariant 3, WD-3).
- **Provenance** — every event has an actor; automation never wears a human's
  name (invariant 4).
- **Determinism** — canonical JSON for everything sealed; struct-generated
  schemas; `errors.Is/As`; fixtures over golden bytes.
- **Engine-agnostic seam** — the client sync contract (W1) stays swappable
  (Q-2): no DO-specific types leak past the protocol boundary.
