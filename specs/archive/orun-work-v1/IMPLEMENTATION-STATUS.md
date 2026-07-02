# Implementation Status (as-built)

> As-built ≠ intent. This file records what has actually shipped, kept distinct
> from the design/plan docs. Each milestone links the PR(s) that landed it.

## Milestones

| ID | Milestone | Status |
|----|-----------|--------|
| W0 | Work model, event log, mutators | ✅ Shipped — orun `internal/work` (#354) + orun-cloud `@saas/db/work` (#34) merged |
| W1 | Sync: Durable Object protocol + client contract | 🏗️ In progress — transport-agnostic core landed (orun-cloud); DO/WebSocket adapter pending |
| W2 | Delivery bridge I: component links + auto-linking | 🏗️ In progress — auto-linker core + PR-webhook ingestion (orun-cloud) + producer bridge (orun `internal/workbridge`) landed; integrations-worker HTTP handler pending |
| W3 | Delivery bridge II: gate-verified Done, Released, drift inbox | 🏗️ In progress — decision core (gate-verified Done, overlay-only Released, drift inbox) landed (orun-cloud); execution-truth/Deployment-overlay feed + apply orchestration pending |
| W4 | Sealing + `orun spec pull` | 🏗️ In progress — sealing core (content-addressed `SpecSnapshot`/`WorkLedgerSegment`) landed in `internal/work`; remote push + `orun spec pull` CLI pending |
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

The **PR-webhook ingestion** path has since landed (orun-cloud): `webhook.ts`
maps a GitHub `pull_request` event → `PullRequestContext`; `ingest.ts`
orchestrates parse → `listOpenTasks` → `computeAutoLinkPlan` →
`applyAutoLinkPlan`, consuming the affected set as the workbridge `AffectedSet`
(end-to-end tested against an in-memory repository).

**Still open for W2:** the thin HTTP handler in the integrations worker that
receives the GitHub delivery and calls `ingestPullRequest` with the
`AffectedSet` (produced by orun/CI), and the blast-radius read API surface.

## W3 — as-built (in progress)

The "status that cannot rot" decision core landed (orun-cloud `@saas/db/work`
`delivery.ts`), all pure and `actor: automation`:

- **`decideDone`** — a merged task reaches Done only when every contract gate
  verifies green from execution truth (Q-3); a failed/pending/missing gate parks
  it in `in_review` with the blocking gate surfaced; a task with no gates parks
  for a human. `automationDoneAllowed` keeps automation from ever moving a
  red-gated task to Done (a human override stays human-attributed, invariant 4).
- **`decideReleased`** — derives Released only from a Deployment-overlay
  observation of live state; a deploy *attempt* releases nothing (invariant 5).
- **`detectDrift`** — a merged PR whose affected components no open task claims
  raises exactly one drift item naming the components.

The full `in_review → done → released` walk is proven from delivery events.

**Still open for W3:** the producer feed of execution truth (run/check → gate
mapping and the Deployment-overlay events from orun) and the apply orchestration
that drives these decisions through the W0 mutators on merge/deploy webhooks.

## W4 — as-built (in progress)

The sealing core — the *system of proof* — landed in `internal/work` (`seal.go`),
building on the W0 types + canonical encoder:

- **`SpecSnapshot`** (§8.1): the frozen epic — epic doc + task envelopes +
  contracts + links, pinned to the resolving catalog id and the ledger seq. By
  type it carries **no hot state** (status/assignees/ordering live in the
  `StatusRow` projection, never in the envelope — invariant 1, asserted).
- **`WorkLedgerSegment`** (§8.2): a sealed event range with a `prev` chain that
  makes the audit log tamper-evident.
- **`ContentID`** = `sha256:` + hex(sha256(canonical bytes)): the same object
  has the same id on every machine. Resealing identical inputs is byte-identical
  (**invariant 6**, asserted); changing any input — or a segment's `prev` —
  shifts the id. Both sealed shapes are in the generated JSON Schema.

**Still open for W4:** pushing sealed objects to the remote object store via
`internal/objremote` (the org/project-routed `refs/work/…`), and the `orun spec
pull` / `orun work list/view/status` CLI surface (set-difference fetch,
read-only materialization).
