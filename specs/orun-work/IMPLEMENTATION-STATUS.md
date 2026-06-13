# Implementation Status (as-built)

> As-built ≠ intent. This file records what has actually shipped, kept distinct
> from the design/plan docs. Each milestone links the PR(s) that landed it.

## Milestones

| ID | Milestone | Status |
|----|-----------|--------|
| W0 | Work model, event log, mutators | 🏗️ In progress — orun-side `internal/work` landed; backend Worker (D1 + DO) pending |
| W1 | Sync: Durable Object protocol + client contract | 🗓️ Not started |
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

**Still open for W0 (backend Worker, `orun-cloud`):** the D1 migration
(`work_items`/`work_events`/`work_links`/`work_status`/`work_cursors`), the
Worker mutators mirroring `internal/work`, and the per-project Durable Object
skeleton that allocates `seq` + the `PREFIX-seq` human key. Tracked in the
`orun-cloud` companion PR.
