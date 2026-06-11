# Spec: orun-work

**orun gains a first-class work plane — Linear-quality project management that is
native to the delivery graph, not bolted beside it. Initiatives, Epics, and Tasks
become entity kinds in the same typed graph as Components, Systems, and
Deployments: their *hot state* (status, assignee, comments, ordering) lives
mutable in Orun Cloud (D1, event-sourced, real-time synced), their *relations*
(`affects`, `implements`, `blockedBy`, `deliveredBy`) are typed edges the change
engine already understands, and their *history* seals into the content-addressed
object graph as snapshots agents can pull by hash.** A task linked to a component
is automatically connected to the code that implements it, the PRs that touch it,
and the deployment that releases it — because orun owns all three.

> **The defining property is that work state is derived from delivery truth.**
> Linear tracks what people *say* about the work; orun Work derives it from the
> work itself. A PR's diff maps to components (`internal/affected`), components
> map to open tasks, merge-with-gates-green moves a task to **Done**, and the
> Deployment entity reaching production moves it to **Released** — a status no
> external tracker can have, because none of them owns execution truth. The
> design protects the complementary carve-out: hot work state is the one
> fast-mutating plane that must *never* enter the immutable graph (CR-1) — only
> sealed snapshots and references are content.

## Status

| Field | Value |
|-------|-------|
| Status | **Draft (v1) — for review, not scheduled** |
| Builds on | `specs/orun-object-model/` (objects/refs/remote), the Orun Cloud backend (`cmd/orun/command_backend.go`: Worker + D1 + R2 + queue), `internal/affected` + `internal/objcatalog` (`specs/archive/orun-catalog-state/`) |
| Coordinates with | `specs/orun-service-catalog/` — work kinds join the entity envelope (SC1) and the typed relation graph (SC2) when those land; W0–W4 do **not** gate on them (see "Gating") |
| Prior art | Linear (the UX bar: optimistic sync, keyboard-first, views-as-queries); the repo's own `specs/*/implementation-plan.md` milestone convention (`Goal / Deps / Done when / Design refs`), which this spec promotes to a typed task contract |
| apiVersion | `orun.io/v1` (`Initiative`, `Epic`, `Task`, `WorkEvent`, `SpecSnapshot`, `WorkLedgerSegment`) |
| Decisions locked | (WD-1) hot work state lives in Orun Cloud (D1), **never** in the content-addressed graph — only sealed snapshots and `work://` references are content; (WD-2) the write model is an **append-only event log** with actor provenance (`user \| agent \| automation`) on every event; projections are derived; (WD-3) **one write path** — UI, MCP, and automation all mutate through the same Worker mutators; (WD-4) work entities join the **same typed relation graph** as catalog entities; the `affected` engine is reused, never duplicated; (WD-5) the **task contract** (`goal / affects / doneWhen / gates / designRefs`) is the structured task body — the spec-milestone convention promoted to schema; agent-readiness is derived from it; (WD-6) **status automation is derived from delivery truth** (PR → In Review, merge + gates → Done, production Deployment → Released); manual moves stay possible and are recorded as events; (WD-7) the DB is **authoritative for mutation**; sealed `SpecSnapshot`/`WorkLedgerSegment` objects are projections — write-back never goes object-ward; (WD-8) real-time sync is a **Durable Object per project** (event ordering + WebSocket fan-out) over optimistic client mutations; (WD-9) identity is ULIDs internally + Linear-style human keys (`ORN-142`) for tasks; (WD-10) **agents are principals**, the MCP is the only agent surface, and it writes through the same mutators as the UI |
| Milestone prefix | **W** (`W0 → W6`) |

## The one-paragraph thesis

The SaaS vision puts orun in one place: provision a baseline, browse the catalog
(portal), run delivery, manage the platform (stacks, secrets) — and plan the
work. Every other tracker integrates with delivery through webhooks and loses
the thread: it cannot know which components a task touches, whether the gates
that define "done" actually ran, or whether the change is live. orun can,
because it already owns the three hard parts: a content-addressed object graph
with org-routed remote sync (`specs/orun-object-model/remote-and-consumers.md`),
a resolved component catalog with a unified change engine (`internal/affected`
maps any diff to component sets), and a provisioned cloud backend (Worker + D1 +
R2). This spec adds the missing plane: Initiative/Epic/Task entities whose
mutable state is an event-sourced D1 log behind a single mutator surface, synced
to a Linear-grade client through per-project Durable Objects; whose component
links ride the same relation vocabulary as the catalog so the `affected` engine
auto-links PRs, computes a task's blast radius, and flags unplanned changes;
whose statuses advance from delivery truth itself — including **Released**,
derived from the Deployment overlay; and whose epics seal into the object store
as content-addressed `SpecSnapshot`s that `orun spec pull` fetches by hash, so a
coding agent implements against a frozen spec and reports status back through
the same MCP mutators a human's keyboard uses. The repo's own `specs/` tree is
the seed: its milestone format imports 1:1 as task contracts, and orun plans
orun from day one.

## Read order

1. **`design.md`** — the problem, goals/non-goals, the two-store split (system
   of record vs system of proof), the work model and status machine, the
   delivery bridge (`affected`-powered automation, Released), the sync
   architecture, decisions, invariants, and the sharpness register.
2. **`data-model.md`** — schemas: work keys, the entity shapes, the task
   contract, the `WorkEvent` log, the D1 tables, the relation edge vocabulary,
   and the sealed `SpecSnapshot` / `WorkLedgerSegment` objects.
3. **`agents-and-mcp.md`** — principals, the orun MCP tool surface, guardrails,
   and the (out-of-scope, designed-for) dispatch model.
4. **`cli-surface.md`** — `orun work`, `orun spec pull`.
5. **`implementation-plan.md`** — milestones **W0 → W6**.
6. **`risks-and-open-questions.md`** — the decision ledger, open questions, and
   the deferred register.

## Phase boundaries

| In scope (this spec) | Out of scope |
|----------------------|--------------|
| The work entity model (`Initiative`/`Epic`/`Task`, cycles, the task contract); the event-sourced D1 store + Worker mutators in Orun Cloud; the per-project Durable Object sync protocol; the delivery bridge (component links, `affected` auto-link, status automation incl. **Released**, blast radius, the drift inbox); sealing (`SpecSnapshot`, `WorkLedgerSegment`) + `orun spec pull`; the orun MCP read/write surface; the `specs/` import path; the SaaS UI *contract* (queries, mutators, sync protocol) | The SaaS UI *implementation* (lives in the portal repo; this spec fixes the contract it consumes); the **Agents section** (dispatch, fleet view — designed-for via principals + MCP, built later); collaborative rich-text editing of epic docs (CRDT scoped to doc bodies, deferred — L-2); cycles/sprint analytics beyond basic grouping; external tracker import (Linear/Jira adapters); notifications/inbox delivery channels (email, Slack); the service-catalog entity envelope and relation graph themselves (owned by SC1/SC2; consumed here when they land) |

## Gating

**W0–W4 gate only on what exists**: the object model (merged), the Orun Cloud
backend (`orun backend init`), and the shipped catalog read surface
(`internal/objcatalog`, `internal/affected`). The deep graph unification — work
kinds rendered inside the service-catalog's typed relation graph — lands with
`orun-service-catalog` SC2 and is kept additive (W-edges use the same edge
grammar so the merge is mechanical). The Agents section is explicitly later;
W0 lays its rails (actor provenance, principals, one write path) so it is a
feature flip, not a rearchitecture.

## Convention over configuration (locked)

The developer authors the *minimum*: a task title is enough to exist; a contract
makes it gate-checked; linked components make it graph-aware. Everything else is
derived — `In Review` from the PR, `Done` from merge + gates, `Released` from
the Deployment overlay, blast radius from the dependency closure, suggested
reviewers from ownership edges, agent-readiness from contract completeness.
Derived state is never authored; manual overrides are events with provenance,
visible as such.

## Document conventions

- Go for interfaces, JSON for on-disk/wire schemas. Forward-slash logical paths.
- Object IDs `"<algo>:<hex>"`. `lowerCamelCase` JSON. RFC 3339 / Z timestamps.
- "MUST / SHOULD / MAY" carry RFC 2119 weight in `data-model.md`.
- Component keys are three-segment `<namespace>/<repo>/<name>` per
  `specs/archive/orun-component-catalog/identity-and-keys.md`; work keys are
  defined in `data-model.md` §1.

## Out-of-band references

- Object graph + remote routing: `specs/orun-object-model/object-store.md`,
  `remote-and-consumers.md` (org/project key routing, push/pull as
  set-difference).
- Change engine: `specs/archive/orun-catalog-state/change-detection.md`
  (`internal/affected`, the ownership map, `Result.Affected`).
- Backend: `cmd/orun/command_backend.go` (D1/R2/Worker/queue provisioning),
  `internal/backendbundle`, `internal/remotestate` (auth: GitHub OIDC + OAuth).
- Sibling control-plane epic: `specs/orun-secrets/` (the same
  Orun-Cloud-authoritative, never-content pattern, applied to values instead of
  work state).
- Packages changed: `internal/work` (new: model, events, contract), the backend
  Worker (mutators, sync DO, D1 schema), `internal/objremote` (seal/pull),
  `cmd/orun` (`work`, `spec pull`), the MCP server (new, thin over
  `internal/work` + `internal/objcatalog` + `internal/affected`).
