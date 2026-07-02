# Implementation Plan

> Milestone-based; each states **goal**, **deps**, **done when**. The order is
> half the design: dogfood first (import + derived read), coordinate second,
> then bridge depth, seals, MCP. Every milestone ends with something the team
> uses that week — no cores-without-a-spine (the v1 failure mode this plan
> exists to not repeat).
>
> Split of work: the model/fold/contract types and the CLI live in orun
> (`internal/worklens`); the logs, ingesters, query API, and console live in
> orun-cloud (see `orun-cloud specs/epics/orun-work/`). The Go fold is the
> conformance oracle; the TS fold must replay the same fixtures.

```
WP0 import + derived read  ─►  WP1 coordination + console board
                                        │
                          WP2 observation ingestion (PRs) + drift inbox
                                        │
                          WP3 gates→Done + Released (overlay feed)
                                        │
                     WP4 sealing + orun spec pull  ─►  WP5 the orun MCP
```

## WP0 — Import + the derived read (dogfood zero)

**Goal:** the two-log substrate exists and the first fold renders: both
repos' `specs/` trees import as Specs/Tasks, and lifecycle lights up from
git/GitHub *history* alone — the team's own stale status tables coming true
on their own.

- orun-cloud: the four tables + sequences (`work` context migration), the
  coordination mutators (create/edit/contract/assign/comment/order/cancel —
  no status mutator exists), the fold (lifecycle/blocked/progress/claim), a
  minimal query API behind api-edge, and a read-only console list (plain
  react-query — no sync engine yet).
- orun: `internal/worklens` — envelope/contract types, the reference fold
  (conformance oracle), fixtures both sides replay; `orun work import`
  per `cli-surface.md` §3 (+ a one-shot GitHub history backfill producing
  `pr_opened`/`pr_merged` observations for imported tasks' key parses).

**Deps:** shipped catalog graph, membership subjects. **Done when:** import
`--dry-run` maps both repos' spec trees losslessly (golden fixtures, P-4);
dropping every cache and replaying both logs reproduces all reads (invariant
1/2 assertion); an event without an actor is rejected; the imported
`orun-work` v2 spec itself renders with derived lifecycle in the console;
the Go and TS folds replay identical fixtures byte-for-byte.

## WP1 — Coordination + the board (daily-drivable)

**Goal:** the plane is usable for real planning: assign, comment, order,
pin — instant.

- The local-first console store: snapshot bootstrap + cursor replay over
  both logs (SSE via LISTEN/NOTIFY), optimistic apply, structured verdicts
  (the shape the MCP will share); board/list views as client-side queries.
- Pins per design §5 (render-beside-truth, auto-expire, agent-forbidden);
  the activity feed straight off the coordination log.
- Fold performance pass: per-subject incremental folds, p95 budget recorded
  on the dogfood corpus (P-1); pin-race property tests (P-5).

**Deps:** WP0. **Done when:** the team plans this epic in it (the dogfood
gate: `IMPLEMENTATION-STATUS.md` stops being hand-edited for orun-work);
a 2-client convergence fixture passes under interleaved optimistic
mutations + reconnect replay; a rejected mutation rolls back with the
verdict surfaced; pinned rungs always render beside observed truth.

## WP2 — Observation ingestion: PRs + drift (the bridge, half 1)

**Goal:** the world starts writing the log live: PRs claim tasks by
themselves; unplanned changes surface.

- The GitHub webhook ingester (integrations-worker, riding the shipped
  integration-tenancy install): `pr_opened/merged/closed` + `branch_seen`
  observations with `dedupeKey` idempotency and a versioned fact contract
  (P-2).
- The affected-set producer: orun/CI attaches `Result.Affected` to PR
  observations (parity with `orun catalog affected` by construction).
- The claim join (key parse ∨ affects-overlap; ambiguity → inbox
  suggestion), derived `implementedBy` edges, blast-radius read
  (`affects` closed over dependents, owner attribution as available), and
  the drift inbox as a standing query.

**Deps:** WP1; integration tenancy. **Done when:** a fixture PR claims by
key parse and by component overlap; ambiguous overlap suggests, never
links; replaying the same webhook delivery twice folds identically
(invariant 4); an unplanned merge raises exactly one drift item; blast
radius matches `orun catalog affected` on the same diff.

## WP3 — Gates → Done, overlay → Released (the bridge, half 2)

**Goal:** the rungs nobody else can have, from execution truth only.

- The run-stream ingester: native-coordination run/check results →
  `gate_result` observations; the `contract.gates` ↔ run/check identity
  mapping fixed here against real run data (P-3).
- The overlay ingester: `saas-resources-runtime` `liveObservation` →
  `revision_live` observations; derived `delivers` edges.
- Fold completion: Done (merge + all gates verified green), Released
  (revision live in target env), unknown-to-orun rendered unknown.

**Deps:** WP2; resources-runtime's liveObservation feed (coordinate — if it
slips, Released ships dark behind the same fold with a fixture feed).
**Done when:** a fixture walks In Review → Done → Released purely from
observations; a gate GitHub reports green but orun has no record of renders
unknown and the task stays In Review (P-3 fixture); a deploy *attempt*
releases nothing (invariant 5); a human pin over a red gate renders beside
the red gate, and expires when it greens.

## WP4 — Sealing + `orun spec pull`

**Goal:** the system of proof: briefs freeze content-addressed; both logs
chain tamper-evident.

- `SpecSnapshot` (intent-plane only, dual log cursors) + chained
  coordination/observation segments per `data-model.md` §8; workspace-id
  routing; seal on spec boundaries + cursor thresholds.
- `orun spec pull` + `orun work list/view/comment` per `cli-surface.md`
  (set-difference pull, read-only materialization, evidence-bearing views).

**Deps:** WP0 (model), existing `internal/objremote`. **Done when:** sealing
twice with no intervening entries is byte-identical (invariant 7); a
snapshot contains no fold output (asserted by type + fixture); pull fetches
only missing objects; both segment chains verify end-to-end against the
live logs; pulled views are read-only.

## WP5 — The orun MCP

**Goal:** the agent surface — evidence-bearing reads, a four-tool write
surface, and no way to lie.

- The MCP per `agents-and-mcp.md`: reads over objcatalog/affected/
  snapshots/fold-query; writes through the coordination mutators with
  service-principal tokens; verdicts shared with WP1's shape.
- Guardrails in the mutator: no agent pins, contract-propose flagging,
  scope/rate limits.

**Deps:** WP1 (verdict shape), WP4 (`spec_get`). **Done when:** an
end-to-end agent fixture pulls a frozen brief, self-assigns, opens a PR
(observed → In Review), comments — and every resulting event carries
`actor: {type: agent, via: mcp}`; there is no status tool to call
(asserted: the tool surface contains no lifecycle write); a
`contract_propose` lands flagged; tool schemas are generated from
`internal/worklens` types (no drift).

---

## Cross-cutting (every milestone)

- **No stored fact** — any PR adding a lifecycle/gate/released column, or
  any write to a read-model outside the fold, is a rejected PR (invariant 1).
- **Two logs only** — mutators write coordination; named ingesters write
  observations; nothing else writes anything (WP-6, invariant 4).
- **Provenance** — every event has an actor; automation never wears a name;
  agents never pin (invariant 3).
- **Determinism** — canonical JSON for everything sealed; the Go fold is
  the conformance oracle and the TS fold replays its fixtures; golden bytes
  over import round-trips (P-4).
- **The seam** — the mutation/verdict contract stays transport-agnostic;
  no SSE/LISTEN-NOTIFY types leak past it (WP-9).
- **Dogfood gate** — from WP1 on, this epic is planned in the plane itself;
  regressions are felt before they are reported.
