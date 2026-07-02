# Design

> The work plane is two append-only logs — coordination (what people intend)
> and observation (what the world did) — and every read surface is a pure
> function of them. Intent is authored and seals content-addressed; fact is
> derived at read and never stored; coordination is the one small hot store.
> This doc fixes the founding observation, the three planes, the lifecycle
> ladder, the graph citizenship, sync-as-log-replay, decisions, invariants,
> and what we refuse to build. Schemas are in `data-model.md`; the agent
> surface is in `agents-and-mcp.md`.

## 1. The founding observation

Every tracker ever built — Jira, Linear, all of them — is a **ledger of human
opinions about the state of the world**. People drag cards to simulate
reality, and the product's quality is measured by how pleasant the simulation
is to maintain. Linear won by making the simulation feel instant; it is still
a simulation, because Linear cannot know the truth.

orun owns the truth: the component graph, the diff→component engine
(`internal/affected`), the execution record of every run and gate (native
coordination v2), and the deployment overlay that knows what is live
(`objread.ComponentDeployments`). So the clean design is not a better ledger
of opinions — it is: **stop storing opinions about anything the platform can
observe.** The tracker becomes a lens over delivery, plus the one thing
delivery cannot tell you: intent.

v1 (`specs/archive/orun-work-v1/`) got 70% of the way — status *automation*
derived from delivery truth — but kept status as a stored, authored column
that automation wrote to, and then needed invariants to keep the column
honest. v2 deletes the column. There is nothing to keep honest.

## 2. One principle, three planes

Everything in the work plane belongs to exactly one plane, split by **who is
allowed to be the source of truth** (WP-1):

| Plane | Contains | Truth source | Store |
|---|---|---|---|
| **Intent** | spec docs, task contracts (`goal / affects / doneWhen / gates`) | humans (agents may propose), deliberate, review-worthy | content-addressed, sealed — like everything else orun authors |
| **Fact** | lifecycle position, PR links, gate results, released-ness, blocked-ness, progress, drift | the world, via observation | **never stored** — derived at read (the CR-1 pattern, generalized) |
| **Coordination** | assignment, priority/ordering, comments, cancellation, pins | humans/agents/automation, instant, unreviewed | one small append-only event log in Postgres |

The planes have different change frequencies and different trust
requirements, which is why v1's alternatives analysis — which treated "work
state" as one substance and rejected git-native authoring because it would
commit-gate every drag — dissolved rather than resolved the tension. Contract
changes are rare and *should* be deliberate. Drags and assignments are
constant and *should* be instant. Lifecycle is not authored at all. Nothing
slow gates anything fast, and no fact is ever stored in two places — most
facts are never stored anywhere.

## 3. The whole architecture is two logs (WP-2)

```
  humans / agents (one mutator surface: console, MCP, CLI)
        │
        ▼
  COORDINATION LOG   work.events — create, edit, assign, comment,
        │            order, label, cancel, pin  (mandatory actor)
        │
        │                            THE WORLD
        │                (GitHub webhooks · CI · the native-coordination
        │                  run stream · the deployment overlay feed)
        │                                  │
        │                                  ▼
        │            OBSERVATION LOG  work.observations — branch_seen,
        │              pr_opened, pr_merged, gate_result, revision_live
        │                                  │
        └──────────────┬───────────────────┘
                       ▼
            fold(observations ⊕ coordination)
                       ▼
     every read surface: lifecycle · board · blast radius ·
     drift inbox · spec progress · agent brief · activity feed
```

- **The coordination log is authored.** Every event carries an actor
  (`user | agent | automation`, never blurred — v1 invariant 4 carries
  forward), applies optimistically on the client, and is totally ordered per
  workspace. It is deliberately tiny: nine event kinds (`data-model.md` §4).
- **The observation log is world-authored.** Append-only facts ingested from
  outside: "PR #412 merged at T", "gate `parity` red for revision R",
  "revision R live in `production`". Nobody — no human, no agent, no
  mutator — can write an opinion into it (WP-6). Observations are facts, not
  decisions, so ingestion is idempotent and replayable; re-deriving them from
  the source systems must converge.
- **Everything else is a fold.** A task's lifecycle is
  `fold(its observations, its overrides)`. Drop every cache, replay both
  logs, get identical state — v1's invariant-2 replay discipline, extended to
  cover the delivery bridge instead of stopping at the mutators.

Audit, sync, undo, agent attribution, drift detection, and progress-that-
cannot-lie stop being features and become corollaries of the two logs.

## 4. The model: two nouns (WP-4)

- **`Spec`** — the grouping document of intent; what v1 called an epic. A
  markdown body plus its tasks. It mirrors the repo's `specs/<name>/`
  convention 1:1, seals content-addressed (`spec@sha256:…`), and is what an
  agent pulls as a frozen brief. Authored in the console or imported verbatim
  from git; either way the sealed object is the durable form, so
  "prose stays PR-reviewed" is the default posture, not an accommodation.
- **`Task`** — the atom. Title + contract + coordination state. Human key
  `<PREFIX>-<seq>` allocated per **workspace** (WP-7): planning topology must
  not be forced to mirror deploy topology — `affects` already says what a
  task touches, across projects and (later) repos.

**Initiatives are a saved view over specs. Cycles are a saved view over
time.** Queries are cheap; entities are forever. (Linear shipped issues +
projects years before initiatives existed; the instinct was right.) The
closed vocabularies reserve nothing for them — adding them later is a schema
rev, which is the correct price for a new noun.

**Work kinds are catalog-graph citizens from day one (WP-5).** SC1–SC8 have
shipped; there is no side table and no "mechanical merge later".
`Task —affects→ Component`, `Task —implementedBy→ PR`,
`Deployment —delivers→ Task`, `Task —partOf→ Spec` are ordinary typed edges
in the shipped relation grammar, so ⌘K, graph views, ownership attribution,
and `internal/affected` work on work items for free.

## 5. The lifecycle: one ladder, each rung owned by a truth source (WP-3)

```
Draft ──▶ Ready ──▶ In Progress ──▶ In Review ──▶ Done ──▶ Released
  │         │            │              │           │          │
authored  derived:    observed:      observed:   observed:  observed:
(exists)  contract    branch/draft   PR open     merge +    revision live in
          complete    PR seen                    gates ✓     target env (overlay)
```

- **Ready is derived from contract completeness** — the same predicate as
  agent-ready. One definition of "actionable" for humans and agents. This
  deletes the Triage/Backlog/Todo taxonomy: *priority ordering* is
  coordination (authored, per-view), *readiness* is intent (derived). v1's
  authored statuses smeared the two together.
- **Done requires gates verified green from orun execution truth** — a merge
  whose contract gates are red, pending, or unknown to orun reads
  In Review with the blocking gate surfaced. GitHub reporting green is not
  orun knowing green (v1's Q-3 discipline carries forward).
- **Released is derived only from the deployment overlay** observing the
  merge's revision live in the target environment — never from a deploy
  attempt. This remains the moat: no webhook tracker can have this status.
- **Blocked, progress, drift, reviewer suggestions: derived.** Blocked from
  open `blockedBy` edges; spec progress from the fold over its tasks; drift
  from merged PRs whose affected components no open task claims; reviewers
  from ownership edges (gated on the `teams-ownership` resolver).
- **Pins are the escape hatch, and they are honest.** A human may pin a task
  to any rung; the pin is a coordination event with an actor, and the UI
  always renders *both*: "pinned **Done** by @rahul — delivery truth says
  In Review (gate `parity` red)". The tracker never lies for you; it lets you
  overrule it in public. Agents may never pin (WP-10). Unpinning is an event;
  a pin also auto-expires the moment observed truth reaches the pinned rung.
- **Canceled is authored** (the world cannot know you gave up) and terminal.

The column in every other tracker *is* the state; here the column is a
**claim the platform continuously audits**.

## 6. The delivery bridge is just ingestion

v1 had a "delivery bridge" with automation that wrote status. v2 has
ingestion producers appending observations, and folds:

- **Auto-claim (was auto-link).** A PR observation carries the affected
  component set (produced by orun/CI via the same `Result.Affected` the
  `--changed` planner uses — parity by construction) plus task keys parsed
  from branch/title. The fold joins observations to tasks by key parse or by
  `contract.affects` ∩ affected-set; the join *is* the `implementedBy` edge,
  materialized as a derived edge, not written by a robot actor.
- **Gate results** ride the native-coordination run stream (the default
  execution-truth wire); the `contract.gates` name↔check mapping is fixed in
  WP3 against real run data, not in the abstract.
- **Released** consumes the `liveObservation` feed owned by
  `saas-resources-runtime` (its `DeploymentObservation` shape is already kept
  structurally compatible on the cloud side).
- **The drift inbox** is a standing query: merged PR observations whose
  affected components intersect no open task's `affects` — rendered as
  triage items in the inbox view, resolved by creating/claiming a task
  (coordination events, like everything humans do).

## 7. Sync: the log is the protocol (WP-9)

No Durable Objects, no bespoke WebSocket protocol, no second stack. The
platform is Postgres; the logs are already ordered per workspace.

- **Client**: a local-first store (IndexedDB), bootstrapped by snapshot,
  advanced by cursor replay over both logs. Views — board, list, timeline,
  cycles, initiatives — are client-side queries over the store, which is what
  makes every interaction instant. Optimistic apply for coordination
  mutations; structured verdicts (accept/reject + reason) in one shape shared
  verbatim with the MCP.
- **Transport**: SSE fanned out from Postgres LISTEN/NOTIFY. Reconnect =
  replay from cursor. The sync protocol and the audit log are the same bytes,
  so there is no separate sync engine to drift.
- **The Linear bar is met where it is felt** — optimistic local apply is a
  client property (<50 ms perceived), not a transport property. If scale ever
  demands a fancier pipe, the mutation/verdict contract is the seam: swap the
  pipe, keep the contract. The seam is permanent; the transport is an
  implementation detail behind it.

## 8. Decisions (locked)

| # | Decision | Rationale | Trade-offs |
|---|----------|-----------|------------|
| WP-1 | Three planes by truth source: intent authored+sealed, fact derived-never-stored, coordination hot | No fact stored in two places; most facts stored nowhere; nothing to keep honest | Fold cost at read (cacheable — caches are droppable by construction) |
| WP-2 | Two append-only logs; every read model a rebuildable fold | Audit/sync/undo/attribution/drift as corollaries, not features | Ingestion must be idempotent; observation sources need contracts |
| WP-3 | Lifecycle is a derived query; authored acts are exist/cancel/pin; pins render beside observed truth | Status cannot rot; overrides are public, attributed, auto-expiring | Requires trustworthy observation feeds before rungs light up |
| WP-4 | Two nouns (`Spec`, `Task`); initiatives/cycles are saved views | Ship the atom; entities are forever, queries are cheap | Teams wanting initiative *entities* wait for a schema rev |
| WP-5 | Work kinds enter the shipped catalog graph directly; no side link table | SC1–SC8 landed; one graph, one ⌘K, `affected` works on work for free | Work-kind churn now rides catalog schema discipline |
| WP-6 | One mutator surface (console/MCP/CLI) for coordination; observations enter only via ingestion | Policy, attribution, verdicts in one place; nobody can author a fact | Two write paths to operate (mutators + ingesters) — by design, they write different logs |
| WP-7 | Tasks are workspace-scoped; `affects` carries delivery topology | Planning ≠ deploy topology; keys allocate per workspace (`ORN-142` expectations); cross-project work has a home | Per-workspace prefix uniqueness to manage |
| WP-8 | Principals are membership subjects (users, service principals, teams); agents are service principals with a mandatory owner | No second identity system; Teams RBAC + effective-access shipped | Work plane depends on the membership context (it should) |
| WP-9 | Log-native sync: snapshot + cursor replay over SSE/LISTEN-NOTIFY; engine-agnostic mutation/verdict seam | The event log is already the replication log; no transport product to own | If p95 misses under load, swap transport behind the seam (measured trigger, not vibes) |
| WP-10 | Agents write via MCP only; may never author Done or pins; briefs are sealed `spec@hash` | The less agents can assert, the less they can hallucinate into planning state | Agent "status" expressiveness is deliberately capped |

## 9. Invariants

1. **No fact is stored.** No table holds lifecycle position, gate state,
   released-ness, blocked-ness, or progress; every read is a fold. Caches
   must be droppable: drop + replay ⇒ identical reads (asserted in CI).
2. **Both logs are append-only.** No update, no delete; corrections are new
   events/observations. Sealing carries the fold's cursor positions.
3. **Every coordination event has an actor**; `automation` never wears a
   human's or agent's name; agents never author `pin` or cancellation-of-
   others'-work events.
4. **Observations are world-authored only.** No mutator writes to the
   observation log; every observation names its source and is idempotent
   (same fact twice ⇒ same fold).
5. **Derived truth is conservative.** Done needs gates *verified* from orun
   execution truth; Released needs the overlay, never a deploy attempt;
   unknown-to-orun is rendered as unknown, not green.
6. **Pins never mask.** A pinned rung renders alongside observed truth and
   auto-expires when truth catches up.
7. **Seal determinism.** `SpecSnapshot` is canonical JSON; identical inputs ⇒
   identical ObjectID; pull is set-difference; pulled views are read-only.
8. **Intent references degrade visibly.** A contract `affects` key that no
   longer resolves renders `unresolved`, never silently drops, and surfaces
   in the drift inbox.

## 10. What we refuse to build

- A bespoke realtime transport (WP-9 seam; revisit on measured need only).
- Initiative or Cycle entities (saved views).
- CRDT collaborative editing (doc bodies are content-addressed; last-write
  + events until a real need is proven).
- A work-local identity system (WP-8).
- Any write path that is not the mutator surface or a named ingester.
- Any feature whose truth source is "someone remembered to update it" —
  that is the disease this plane exists to cure.

## 11. Sharpness register

- **P-1 (fold performance).** Lifecycle-at-read must stay cheap on a hot
  workspace. Folds are per-subject and incremental (cursor-cached,
  droppable); WP1 records fold p95 on the imported dogfood corpus and locks
  a budget before the console ships.
- **P-2 (observation source contracts).** Each ingester (GitHub webhooks,
  run stream, overlay feed) needs a versioned fact contract; a source that
  changes shape must fail loudly at ingest, never silently skew folds.
- **P-3 (gate name mapping).** `contract.gates` ↔ orun run/check identity is
  fixed in WP3 against real run data; the fixture must include a gate GitHub
  reports green but orun has no record of ⇒ renders unknown, task stays
  In Review.
- **P-4 (import round-trip).** Imported spec docs remain byte-identical
  through import → seal → pull (golden fixtures over this repo's own
  `specs/` tree), or diffing against the source tree breaks and trust dies.
- **P-5 (pin semantics under races).** A pin placed while observations are
  in flight must resolve deterministically (fold order is log order; pins
  auto-expire on catch-up) — property-tested in WP1.
