# Implementation Status (as-built)

> As-built ≠ intent. This file records what has actually shipped, kept distinct
> from the design/plan docs. Each milestone links the PR(s) that landed it.

## Milestones

| ID | Milestone | Status |
|----|-----------|--------|
| W0 | Work model, event log, mutators | ✅ Shipped — orun `internal/work` (#354) + orun-cloud `@saas/db/work` (#34) merged |
| W1 | Sync: Durable Object protocol + client contract | 🏗️ In progress — transport-agnostic core landed (orun-cloud); DO/WebSocket adapter pending |
| W2 | Delivery bridge I: component links + auto-linking | 🏗️ In progress — auto-linker core (orun-cloud) + producer bridge (orun `internal/workbridge`) landed; live PR-webhook ingestion pending |
| W2 | Delivery bridge I: component links + auto-linking | 🗓️ Not started |
| W3 | Delivery bridge II: gate-verified Done, Released, drift inbox | 🗓️ Not started |
| W4 | Sealing + `orun spec pull` | 🗓️ Not started |
| W5 | The orun MCP | 🗓️ Not started |
| W6 | `specs/` import (dogfood) | 🗓️ Not started |

## W0 — as-built

The W0 *system of record* is split across the two repos by design (the model is
the conformance oracle; the backend Worker mirrors it). The **orun-side**
contract has landed:

**`internal/work`** (new, pure data package — import-isolated like
`internal/catalogmodel`):

- **Envelope + contract + principal types** per `data-model.md` §2–§3, §6:
  `Item` (Initiative/Epic/Task), `Contract` (goal/affects/doneWhen/gates/
  designRefs/deps) with structural `Complete()` + `AgentReady()` derivation,
  `Principal`/`Actor` with the human-vs-automation actor split.
- **The closed `WorkEvent` log** per §4: the 16-kind closed set
  (`event.go`), with `Validate()` rejecting unknown kinds, actor-less events,
  and empty subjects (invariant 4, W0 "an event without an actor is rejected").
- **Relation-edge vocabulary** per §7 (`link.go`): the 8 typed edges, shared
  grammar with the catalog graph.
- **The projection reducer** per §5 (`projection.go`): `State` holds the
  `work_items` / `work_status` / `work_links` projections; `Reduce` folds an
  event log into a fresh projection and `commit` is the one-write-path step
  every mutator runs through (append event + update projection together,
  invariant 3). The DO's seq + human-key (`PREFIX-seq`) allocation is modeled
  here as the reference the backend implements.
- **The mutator set** (`mutate.go`): create/edit/status/assign/comment/link/
  contract plus move/cycle/label/seal/import/cancel — every mutator appends
  exactly one event with a mandatory actor.
- **Struct-generated JSON Schema** (`schema/work.schema.json`, embedded) via
  `go generate ./internal/work/...`, gated by `make verify-generated` (no
  hand-edited JSON, determinism contract).

**Tests (≥90% floor met):** the invariant-2 proof — a fixture exercising all 16
event kinds, then dropping the projection and replaying the log via `Reduce`,
yields a byte-for-byte identical projection; the actor-rejection guard across
every mutator; the closed-event-kind coverage assertion; mutator argument/
not-found/conflict error paths. Package coverage ~92%.

**W0 backend (`orun-cloud`, #34, shipped):** the Postgres migration
(`200_work_foundation`: `work.items`/`events`/`links`/`status`/`cursors` +
the `sequences` allocator), the `@saas/db/work` mutators mirroring
`internal/work`, and the per-project sequence allocator (the Postgres
equivalent of the spec's Durable Object — the real backend is Supabase/Postgres,
not Cloudflare D1). The invariant-2 replay is proven on both sides.

## W1 — as-built (in progress)

The **transport-agnostic core** has landed in `orun-cloud` (`@saas/db/work`,
PR #35), honoring the engine-agnostic seam (Q-2 — no DO/WebSocket types in the
contract):

- **`sync.ts`** — the protocol (subscribe/mutate/event/verdict/replay), the
  `Mutation` intent, the accept/reject `Verdict` shape (shared with the W5 MCP),
  and `dispatch` (the single apply path for client *and* server).
- **`WorkSyncServer`** — the per-project ordering authority modeled purely:
  total-order `seq` assignment, event fan-out, cursor replay on (re)subscribe.
  The production Cloudflare Durable Object is a thin transport adapter over it.
- **`WorkSyncClient`** — the reference optimistic store (the console's contract):
  optimistic apply, retire-on-confirm, rebase, reject rollback with verdict
  surfaced, and gap/out-of-order replay losing nothing.

Conformance fixtures both sides replay (2-client convergence under interleaved
optimism, reject rollback, cursor replay).

**Still open for W1:** the Cloudflare Durable Object + WebSocket transport
adapter wrapping `WorkSyncServer`, and the soak-rig latency numbers (p95 server
echo < 150 ms).

## W2 — as-built (in progress)

The delivery bridge's auto-linker core (orun-cloud) and the producer-side wire
contract (orun) have landed:

- **orun (`internal/workbridge`)** — projects an `internal/affected.Result` into
  the `AffectedSet` DTO the cloud consumes. `Components` is, by construction,
  the engine's blast radius (`Result.Affected`), so the cloud's component
  matching has **parity with `orun catalog affected`** without a second closure
  (the W2 parity requirement). Parity + wire-shape tests included.
- **orun-cloud (`@saas/db/work` autolink)** — `computeAutoLinkPlan`: given the
  affected set + PR context, decides which open tasks to link (`implementedBy`)
  and transition, matched by `contract.affects` overlap **or** `PREFIX-n` key
  parse from the branch/title. Transitions are forward-only and never touch
  closed tasks; everything is `actor: automation` (invariant 4).
  `materializeAffects` degrades unresolved component keys visibly (Q-5);
  `applyAutoLinkPlan` drives the plan through the W0 one write path (WD-3).

**Still open for W2:** live PR-webhook ingestion (the path that runs
`internal/affected` for a real diff and feeds `AffectedSet` to the cloud
auto-linker), and the blast-radius read API surface.
