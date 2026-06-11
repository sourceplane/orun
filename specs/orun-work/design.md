# Design

> orun gains a work plane: Initiative/Epic/Task entities whose hot state is an
> event-sourced log in Orun Cloud (D1) behind one mutator surface, synced in
> real time through per-project Durable Objects; whose component links reuse the
> `affected` engine for auto-linking, blast radius, and status automation
> (including **Released**, derived from deployment truth); and whose epics seal
> into the object graph as content-addressed snapshots that agents pull by hash.
> This doc fixes the two-store split, the work model, the status machine, the
> delivery bridge, the sync architecture, decisions, invariants, and the
> sharpness register. Schemas are in `data-model.md`; the agent surface is in
> `agents-and-mcp.md`.

## 1. Problem

1. **The SaaS has every plane except planning.** Portal (catalog), Delivery
   (runs, deployments), Platform (stacks, secrets) all render one graph; the
   work that *drives* those planes lives in an external tracker that knows
   nothing about components, gates, or deployments.
2. **External trackers integrate by webhook and lose the thread.** A
   GitHub-linked issue knows a PR merged. It cannot know which components the
   diff touched, whether the acceptance gates ran, or whether the change is live
   in production. Status becomes what someone remembered to drag.
3. **The object graph is the wrong store for hot work state.** It is immutable,
   content-addressed, and poll-based — correct for plans and history, fatal for
   a 60 fps board where every keystroke would mint objects and churn CAS refs.
   The complement of CR-1: fast-mutating state needs a mutable plane.
4. **The repo already plans work in a machine-shaped format and gets no leverage
   from it.** Every spec epic encodes milestones as
   `Goal / Deps / Done when / Design refs` and tracks them by hand-editing
   `IMPLEMENTATION-STATUS.md` tables. The format is a task contract in all but
   schema; nothing consumes it.
5. **Agents are coming and have no surface.** A coding agent needs a frozen spec
   to implement against, the component subgraph it touches, and a governed way
   to report status. Today that is "paste markdown into a prompt."

## 2. Goals / non-goals

**Goals**

- A **work entity model** — `Initiative → Epic → Task` (+ optional cycles) with
  Linear-style human keys and a typed **task contract** promoted from the spec
  milestone convention.
- An **event-sourced system of record** in Orun Cloud: every mutation is an
  append-only `WorkEvent` with actor provenance; board/list/timeline views are
  projections.
- **One write path**: a single Worker mutator surface serving the SaaS UI, the
  MCP, and automation — the place policy, gates, and attribution are enforced.
- A **Linear-grade sync architecture**: optimistic client mutations, per-project
  Durable Object ordering + WebSocket fan-out, <50 ms perceived interaction.
- The **delivery bridge**: component links as typed relations; `affected`-powered
  PR auto-linking; status automation from delivery truth (In Progress → In
  Review → Done → **Released**); blast radius on every task; a drift inbox for
  unplanned changes.
- **Sealing + pull**: epics and event-log segments seal into the object graph as
  canonical, content-addressed `SpecSnapshot` / `WorkLedgerSegment` objects;
  `orun spec pull` fetches a frozen `spec@hash` by set-difference.
- An **agent-ready substrate**: humans and agents are principals; the MCP writes
  through the mutators; agent-readiness is a derived property of a task.
- **Dogfood from day one**: the `specs/` tree imports as the first epics.

**Non-goals**

- Building the SaaS UI here (the portal repo implements it; this spec fixes the
  data, mutator, and sync contracts it consumes).
- The Agents section (dispatch, fleet) — designed-for, not built (see
  `agents-and-mcp.md` §5).
- Replacing git-reviewed design docs. Epics *reference* design material; deep
  prose can stay in-repo. What moves to the work plane is the *tracking*:
  milestones, status, assignment, progress.
- CRDT collaborative editing of epic documents (deferred, L-2).
- External tracker adapters (Linear/Jira import) — after W6.

## 3. The two-store split (system of record vs system of proof)

The architecture extends the line the catalog drew (CR-1: nothing that changes
without a source change enters an immutable blob) with its complement:

> **Hot work state lives in Orun Cloud and is never content. The object graph
> holds only sealed snapshots and references. The DB is authoritative for
> mutation; sealed objects are projections; write-back never goes object-ward
> (WD-1, WD-7).**

```
            SaaS UI ─┐                          ┌─ orun CLI / cockpit
   MCP (agents) ─────┤   one mutator surface    │
   automation ───────┤  (Cloudflare Worker)     │ read seam
                     ▼                          ▼
        ┌─ Orun Cloud: SYSTEM OF RECORD ─────────────────────┐
        │  D1: work_items · work_events (append-only) ·      │
        │      work_links · projections                      │
        │  DO: per-project ordering + WebSocket fan-out      │
        └────────────┬────────────────────────────────────────┘
                     │ seal (canonical JSON, on boundaries)
        ┌─ Object graph: SYSTEM OF PROOF ─────────────────────┐
        │  R2/local CAS: SpecSnapshot · WorkLedgerSegment     │
        │  refs: orgs/<org>/projects/<project>/refs/work/…    │
        │  ← orun spec pull (set-difference, frozen @hash)    │
        └─────────────────────────────────────────────────────┘
```

- **System of record (D1, event-sourced).** Every mutation appends a
  `WorkEvent { eventId(ULID), workKey, kind, actor{type,id}, at, payload }` and
  updates the item projection in the same transaction. Events are the truth;
  projections (current status, board ordering, counts) are derived and
  rebuildable. This mirrors the object model's own event pattern
  (`TriggerOccurrence` is append-only; executions are event → sealed) and buys
  audit, undo, activity feeds, and agent attribution structurally — not as
  features.
- **System of proof (CAS).** On boundaries — epic created/edited, milestone
  closed, on demand — the Worker seals a canonical-JSON `SpecSnapshot` (epic doc
  + task contracts + links) and periodic `WorkLedgerSegment`s (event batches)
  into the object store under the org/project routing the remote already
  defines. Pull is the existing set-difference walk. A snapshot is immutable,
  verifiable, and *frozen*: an agent implementing `spec@sha256:…` cannot have
  the ground shift under it.
- **The carve-out is load-bearing in both directions.** Work state in the graph
  would churn CAS refs per keystroke (and make GC reachability a UX feature);
  authoritative work state in markdown would commit-gate every drag. Each store
  does only what it is shaped for.

## 4. The work model

### 4.1 Hierarchy and keys

`Initiative → Epic → Task (→ subtask via parent)`, deliberately shallow.
Optional `Cycle` grouping. Identity: ULIDs internally; human keys are
Linear-style `<PREFIX>-<seq>` for tasks (`ORN-142`) and slugs for epics
(`acme/platform/epics/orun-work`). Human keys are a UX *and* automation
feature: speakable, greppable, and parsed out of branch names / PR titles by the
auto-linker (§6.1). Full grammar in `data-model.md` §1.

### 4.2 The task contract (the keystone)

The repo's milestone convention, promoted to schema:

```yaml
contract:
  goal: "Catalog reads route through the objcatalog view"
  affects: ["sourceplane/orun/api-edge", "sourceplane/orun/web"]   # component keys
  doneWhen:
    - "changed_parity_test green against legacy selection"
    - "coverage >= 90% on internal/objcatalog"
  gates: ["tests", "parity", "review"]
  designRefs: ["epic://acme/platform/epics/orun-work#design"]
```

One artifact, two consumers: a human reads a crisp definition of done; an agent
receives an executable brief. **Agent-ready** is derived, never authored: a task
with a complete contract + resolvable `affects` keys + defined gates surfaces a
badge (and is dispatchable, later). Contract completeness also powers honest
epic progress (§6.4).

### 4.3 The status machine

```
Triage → Backlog → Todo → In Progress → In Review → Done → Released
                                                  ↘ Canceled
```

| Transition | Driver |
|---|---|
| → In Progress | branch / draft PR referencing the task key (automation), or manual |
| → In Review | PR opened (automation) |
| → Done | PR merged **and** the contract's gates verified green (automation) |
| → **Released** | the Deployment overlay shows the revision containing the merge live in the target environment (automation; orun-exclusive) |
| any → any | manual move — always allowed, always an event with `actor`, rendered as an override |

`Blocked` is not a status: it is derived from open `blockedBy` edges and
rendered as a flag, so it can never go stale by forgetting to un-set it.
**Released is the demo**: no webhook-integrated tracker can derive it, because
none owns execution truth. orun reads it off its own Deployment overlay.

## 5. Work in the entity graph

Work kinds are graph citizens, not a side table (WD-4). Edges use the same
`{from, fromKind, type, to, toKind}` grammar as the catalog relation graph:

| Edge | Meaning |
|---|---|
| `Task —affects→ Component` | the contract's component links; the bridge's join key |
| `Task —partOf→ Epic`, `Epic —partOf→ Initiative` | hierarchy |
| `Task —blockedBy→ Task` | dependency; derives the Blocked flag |
| `Task —implementedBy→ PR/revision ref` | written by the auto-linker |
| `Deployment —delivers→ Task` | written by the Released automation |
| `Task —assignedTo→ Principal` | humans and agents uniformly |

Until `orun-service-catalog` SC2 lands, these edges live in `work_links` (D1)
with the same vocabulary; when the typed multi-kind relation graph ships, work
edges merge mechanically (same grammar, additive kinds). The cross-lens payoff:
a Component page lists its open tasks; a Task page shows the live state of what
it shipped; everything is one ⌘K jump apart because it is one graph.

## 6. The delivery bridge

All four features are projections over machinery that already ships
(`internal/affected`, the ownership map, `internal/objcatalog`):

### 6.1 Auto-linking
A PR event reaches the Worker (the backend already ingests CI triggers). The
diff maps to component keys via the `affected` engine; components map to open
tasks whose contracts claim them; task keys parsed from the branch/PR title
short-circuit the match. Result: `implementedBy` links + the §4.3 transitions,
automatic, with every automated move recorded as an `actor: automation` event.

### 6.2 Blast radius
A task's `affects` set, closed over the dependency graph
(`Result.Dependents`), renders as: "touches `auth-svc`; 7 downstream
components, 2 owned by other teams" — with suggested reviewers from ownership
edges. Triage with the graph, not vibes.

### 6.3 The drift inbox
A merged PR whose affected components intersect **no** open task's `affects`
raises a triage item: "unplanned change to `billing-worker`". The affected
engine run in reverse, as a planning-integrity check.

### 6.4 Epic progress that cannot lie
An epic's progress derives from delivery truth: tasks Released vs Done vs open,
gate pass-rates, scorecard deltas on affected components (when
`orun-scorecards` lands). It replaces the hand-edited
`IMPLEMENTATION-STATUS.md` table with a projection that is correct by
construction.

## 7. Sync architecture (the Linear bar)

The feel is a sync-engine property, engineered directly (WD-8):

- **Client**: a normalized in-memory store; every mutation applies optimistically
  (<50 ms perceived) and enqueues to the Worker. Views (board/list/timeline) are
  client-side queries over the store — instant filtering, no spinner-per-click.
- **Server**: the mutator validates, appends the event, updates projections in
  one D1 transaction, then publishes through the project's **Durable Object**,
  which owns event ordering and WebSocket fan-out for that project. Per-project
  DOs shard naturally and keep ordering local.
- **Reconciliation**: clients carry a per-project event cursor; on reconnect they
  replay the gap (the event log *is* the sync log). A rejected optimistic
  mutation (policy, gate, conflict) rolls back locally with the server verdict
  rendered — same contract for the MCP, which simply has no optimistic layer.
- **The UX bar is part of the contract**: ⌘K over the whole graph (tasks *and*
  components *and* deployments), keyboard-first, views as saved queries, a
  triage inbox. Interaction latency is treated as a correctness requirement the
  way determinism is in the compiler.

## 8. Decisions (locked)

| # | Decision | Rationale | Trade-offs |
|---|----------|-----------|------------|
| WD-1 | Hot work state in Orun Cloud (D1); never content; only sealed snapshots + `work://` refs enter the graph | The complement of CR-1; CAS is wrong for 60 fps state | Two stores to operate; sealing is the bridge |
| WD-2 | Append-only `WorkEvent` log with `actor{type: user\|agent\|automation}`; projections derived | Audit/undo/feeds/attribution structurally; matches the object model's event pattern | Projection rebuild machinery; event schema discipline |
| WD-3 | One write path (Worker mutators) for UI, MCP, automation | One place for policy, gates, attribution — the `bridge.Source` discipline applied to writes | Mutator API breadth grows with features |
| WD-4 | Work entities use the catalog's edge grammar; `affected` reused, never duplicated | One graph, one change engine; SC2 merge stays mechanical | Work edges live in D1 until SC2 lands |
| WD-5 | The task contract is the structured body; agent-readiness derived | One artifact serves humans and agents; imports the repo's own convention | Contract-less quick tasks get fewer derived features |
| WD-6 | Status automation derived from delivery truth; manual moves are recorded overrides | Status that cannot rot; Released becomes possible | Automation must be conservative (gates verified, not inferred) |
| WD-7 | DB authoritative; sealed objects are projections; no write-back | Kills dual-source-of-truth drift (the Backstage failure) | Agents mutate via MCP only, never by editing pulled snapshots |
| WD-8 | Per-project Durable Object for ordering + fan-out; optimistic clients; D1 authoritative | Linear-grade feel on the backend orun already provisions; no second vendor | DO/WebSocket protocol is ours to maintain |
| WD-9 | ULIDs internally; `PREFIX-seq` human task keys; slug epic keys | Speakable, greppable, parseable by the auto-linker | Sequence allocation is per-project state (DO-owned) |
| WD-10 | Agents are principals; MCP is the only agent surface | Policy-identical humans and agents; Agents section becomes a feature flip | MCP ships in W5, before any dispatch exists |

## 9. Invariants

1. **No hot state in the graph.** No object blob contains a mutable work field;
   sealing emits snapshots whose identity is content (asserted: seal twice with
   no events between → byte-identical objects).
2. **Events are the truth.** Every projection is rebuildable from
   `work_events` alone (tested: drop projections, replay, byte-equal).
3. **One write path.** No code path mutates `work_items` without appending the
   event in the same transaction; UI/MCP/automation share the mutators.
4. **Every event has an actor.** `actor.type` ∈ {user, agent, automation};
   automated transitions are never attributed to a human.
5. **Automation is conservative.** Done requires gates *verified* green;
   Released requires the Deployment overlay, not a deploy *attempt*.
6. **Seal determinism.** `SpecSnapshot`/`WorkLedgerSegment` use canonical JSON;
   same logical content → same ObjectID, push/pull is pure set-difference.
7. **Pull is read-only.** Nothing in the CLI/MCP writes work state through a
   pulled snapshot (WD-7).
8. **Derived flags are never stored.** Blocked, agent-ready, progress are
   computed at read; no stale-flag class of bugs.

## 10. Alternatives considered

- **Git-native tracking** (specs stay markdown-authoritative; the board is a
  projection): maximal provenance, but commit-gates every interaction — fails
  the Linear bar. Rejected as the *authoring* model; survives as the seal/pull
  spine and the import path.
- **Object-graph-native tasks** (work state as CAS objects + refs): one store,
  full provenance, but per-keystroke object churn, poll-based watch, GC in the
  hot path. Rejected; the graph keeps the proof role it is shaped for.
- **External tracker + deep integration** (Linear/Jira + webhooks): fastest to
  ship, but forfeits the moat (component-aware tasks, gate-verified Done,
  Released) and re-imports the drift problem. Rejected.
- **Postgres + off-the-shelf sync engine** (Zero/Replicache/Convex): excellent
  feel, but adds a second backend stack beside the already-provisioned
  Worker/D1/DO/R2 and an external dependency for the product's core feel.
  Rejected for v1; the DO protocol is ours (Q-2 tracks revisiting if it
  underdelivers).

## 11. Sharpness register

- **Q-1 (D1 write throughput)** — a hot org's event rate vs single-DB limits;
  mitigation: per-project DO serialization, batched appends, org sharding.
  Measure in W1 with a synthetic board soak.
- **Q-2 (DO sync protocol scope)** — how much of presence/conflict semantics do
  we own before it rivals an off-the-shelf engine; the W2 client must keep the
  mutator contract engine-agnostic so a swap stays possible.
- **Q-3 (gate verification source)** — "gates green" must come from orun's own
  execution truth (runs/checks recorded in the backend), not re-derived from
  GitHub statuses; the exact mapping is fixed in W3.
- **Q-4 (spec doc round-trip)** — epics imported from `specs/` keep their
  markdown as the doc body; the sealed snapshot must round-trip it losslessly
  (no normalization that breaks diffing against the source tree).
- **Q-5 (contract `affects` validation)** — component keys are validated against
  the *current* catalog at edit time; renamed/removed components degrade the
  link to `unresolved` (rendered, never silently dropped).
