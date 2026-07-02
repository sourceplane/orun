# Data Model

> Every persisted schema for the work plane: keys, the two entity shapes, the
> task contract, the coordination-event and observation vocabularies, the four
> Postgres tables, the fold, the graph edges, and the sealed objects. JSON is
> the wire/on-disk form; `lowerCamelCase`, object IDs `"<algo>:<hex>"`,
> RFC 3339 / Z. "MUST/SHOULD/MAY" carry RFC 2119 weight here. The Postgres DDL
> ships with orun-cloud (`work` bounded context); sealed shapes are canonical
> JSON per `specs/orun-object-model/` framing.

## 1. Identity & keys

| Thing | Internal id | Human key | Example |
|---|---|---|---|
| Workspace | `ws_…` (existing) | slug (existing) | `ws_01J9…` / `sourceplane` |
| Spec | ULID `spc_…` | slug, unique per workspace | `sourceplane/specs/orun-work` |
| Task | ULID `tsk_…` | `<PREFIX>-<seq>`, per workspace (WP-7) | `ORN-142` |
| CoordinationEvent | ULID `cev_…` | — | — |
| Observation | ULID `obs_…` | — | — |

- `PREFIX` MUST be 2–5 uppercase `[A-Z]`, unique per workspace; `seq`
  allocates transactionally per `(workspace, prefix)`; the pair is immutable
  once issued; canceled tasks never free keys.
- Principals are **membership subjects** (WP-8): `usr_…`, `sp_…`, `team_…`
  public ids from the identity/membership contexts. There is no work-local
  principal table. Agent principals are service principals whose registration
  MUST name a responsible owner (user or team).
- Component keys in contracts are the catalog's three-segment
  `<namespace>/<repo>/<name>` and MUST validate against the current catalog
  at edit time; failures degrade to `unresolved` (invariant 8).
- Fully-qualified work key: `<workspace>/<human-key>` (`sourceplane/ORN-142`);
  short forms resolve in workspace context.

## 2. Entity shapes

Two nouns (WP-4). The envelope is intent-plane only — nothing mutable-by-
observation lives in it; the envelope is what seals.

### 2.1 `Spec`

```jsonc
{
  "apiVersion": "orun.io/v1",
  "kind": "Spec",
  "id": "spc_01J9…",
  "key": "sourceplane/specs/orun-work",
  "workspace": "ws_01J9…",
  "title": "orun-work v2 — the work lens",
  "docRef": "sha256:…",              // content-addressed doc body (WO doc-blob spine);
                                     // imported docs round-trip byte-identical (P-4)
  "labels": { "area": "platform" },  // MAY; flat string map
  "createdBy": { "type": "user", "id": "usr_…" },
  "createdAt": "2026-07-02T09:00:00Z"
}
```

### 2.2 `Task`

```jsonc
{
  "apiVersion": "orun.io/v1",
  "kind": "Task",
  "id": "tsk_01J9…",
  "key": "ORN-142",
  "workspace": "ws_01J9…",
  "spec": "sourceplane/specs/orun-work",   // partOf target; MAY be null (inbox task)
  "title": "Route catalog reads through the objcatalog view",
  "labels": { "area": "catalog" },
  "contract": { /* §3 */ },                // MAY; presence+completeness derive Ready
  "createdBy": { "type": "agent", "id": "sp_…" },
  "createdAt": "2026-07-02T09:00:00Z"
}
```

| Field | Req. | Notes |
|---|---|---|
| `kind`, `id`, `key`, `workspace`, `title` | MUST | identity core; `title` is the only authoring requirement |
| `spec` | MAY | tasks without a spec live in the workspace inbox view |
| `docRef` | MAY (Spec: SHOULD) | content-addressed; the doc-blob pattern the catalog already ships |
| `contract` | MAY | §3; completeness ⇒ **Ready** (= agent-ready, one predicate) |
| `createdBy` | MUST | a membership subject ref (§1) |

Title/labels/contract edits are coordination events (`item_edited`,
`contract_edited`) folding into the current envelope; the envelope rows in §5
are themselves a fold cache of the coordination log (droppable, invariant 1).

## 3. The task contract (unchanged keystone, v1 → v2)

The spec-milestone convention (`Goal / Deps / Done when / Design refs`) as
schema. **Completeness** (goal + ≥1 affects + ≥1 doneWhen + ≥1 gate, all
`affects` resolved, all `deps` closed or absent) derives **Ready**.

```jsonc
{
  "goal": "Catalog reads route through the objcatalog view",
  "affects": ["sourceplane/orun/api-edge", "sourceplane/orun/web"],
  "doneWhen": [
    "changed_parity_test green against legacy selection",
    "coverage >= 90% on internal/objcatalog"
  ],
  "gates": ["tests", "parity", "review"],   // names mapped to orun run/check identity (P-3)
  "designRefs": ["spec://sourceplane/specs/orun-work#design"],
  "deps": ["ORN-140"]                       // derives blockedBy edges (derived, not stored)
}
```

| Field | Req. | Notes |
|---|---|---|
| `goal` | SHOULD | one or two sentences; the agent brief's first line |
| `affects[]` | SHOULD | catalog component keys; the fold's join key for auto-claim, blast radius, drift |
| `doneWhen[]` | SHOULD | human acceptance checklist; not machine-verified (`gates` are) |
| `gates[]` | SHOULD | named checks that MUST verify green from orun execution truth before the fold shows Done |
| `designRefs[]` | MAY | `spec://`, `https://`, or repo-relative refs |
| `deps[]` | MAY | task keys; `blockedBy` is derived from them at read (never stored) |

## 4. The two logs

### 4.1 `CoordinationEvent` (authored)

```jsonc
{
  "eventId": "cev_01J9…",
  "workspace": "ws_01J9…",
  "subject": "ORN-142",                    // task or spec key
  "kind": "pinned",                        // §4.1.1 vocabulary
  "actor": { "type": "user",               // user | agent | automation
             "id": "usr_…",
             "via": "console" },           // console | mcp | cli | import
  "at": "2026-07-02T09:14:03Z",
  "payload": { "rung": "done", "note": "shipping the hotfix as-is" },
  "seq": 18421                             // per-workspace total order
}
```

**Kinds (v1 → closed set of 9):**
`item_created · item_edited · contract_edited · assigned · unassigned ·
comment_added · ordered · pinned · canceled`

- Closed per schema version; unknown kind ⇒ write-time error.
- `actor` MUST be present; `automation` MUST NOT wear a user/agent id;
  agents MUST NOT author `pinned` (invariant 3, WP-10).
- `ordered` carries per-view ordering floats (priority is coordination).
- `pinned` payload names a rung and optional note; `payload.rung: null`
  unpins. Pins auto-expire on observed catch-up (invariant 6) — expiry is a
  fold rule, not an event.
- `imported` is not a kind: imports write ordinary `item_created` /
  `contract_edited` events with `via: import`.

### 4.2 `Observation` (world-authored)

```jsonc
{
  "obsId": "obs_01J9…",
  "workspace": "ws_01J9…",
  "source": "github-webhook",              // github-webhook | run-stream | deploy-overlay | ci
  "sourceVersion": 1,                      // versioned fact contract (P-2)
  "kind": "pr_merged",                     // §4.2.1 vocabulary
  "at": "2026-07-02T09:14:03Z",
  "dedupeKey": "github:pr:sourceplane/orun#412:merged",  // idempotency (invariant 4)
  "payload": {
    "pr": "sourceplane/orun#412",
    "revision": "sha256:…",
    "taskKeys": ["ORN-142"],               // parsed from branch/title, MAY be empty
    "affected": ["sourceplane/orun/api-edge"]  // Result.Affected, produced by orun/CI
  },
  "seq": 90311                             // per-workspace total order (separate sequence)
}
```

**Kinds (v1, closed set of 6):**
`branch_seen · pr_opened · pr_merged · pr_closed · gate_result ·
revision_live`

- Observations MUST be idempotent by `dedupeKey`: the same world fact
  ingested twice folds identically.
- No mutator may append here (WP-6); each `source` is a named ingester with
  a versioned contract that fails loudly on shape drift (P-2).
- `gate_result` payload: `{ gate, revision, status: green|red|pending,
  runRef }` — gate identity per the P-3 mapping, `runRef` into native-
  coordination execution truth.
- `revision_live` payload: `{ revision, environment, deploymentRef }` — the
  `saas-resources-runtime` liveObservation shape.

## 5. Postgres tables (the whole schema)

```sql
work.specs        (id TEXT PK, ws TEXT, key TEXT, title TEXT, doc_ref TEXT,
                   labels JSONB, created_by JSONB, created_at TIMESTAMPTZ,
                   UNIQUE (ws, key));
work.tasks        (id TEXT PK, ws TEXT, key TEXT, spec_key TEXT, title TEXT,
                   contract JSONB, labels JSONB, created_by JSONB,
                   created_at TIMESTAMPTZ, UNIQUE (ws, key));
work.events       (event_id TEXT PK, ws TEXT, subject TEXT, kind TEXT,
                   actor JSONB, at TIMESTAMPTZ, payload JSONB, seq BIGINT,
                   UNIQUE (ws, seq));                  -- coordination log
work.observations (obs_id TEXT PK, ws TEXT, source TEXT, source_version INT,
                   kind TEXT, at TIMESTAMPTZ, dedupe_key TEXT, payload JSONB,
                   seq BIGINT,
                   UNIQUE (ws, seq), UNIQUE (ws, dedupe_key));  -- observation log
```

Four real tables. **No status column anywhere.** `work.specs` / `work.tasks`
are fold caches of the coordination log (current envelope) and MUST be
droppable: rebuild from `work.events` ⇒ identical rows (invariant 1). Key
sequences allocate via `work.sequences (ws, prefix, next)` transactionally.
Read-model caches beyond these (e.g. a lifecycle materialization for list
endpoints) MAY exist, MUST be droppable, and MUST NOT be written by anything
but the fold.

## 6. The fold (normative sketch)

```
lifecycle(task) :=
  if canceled(task.events)                      → Canceled
  else if pin := activePin(task.events, obs)    → Pinned(pin.rung)  ⊕ observed(task)   // both rendered
  else observed(task)

observed(task) :=
  if revision_live covering a merged claiming PR, in a target env   → Released
  else if pr_merged claiming task ∧ gates(task.contract) all green  → Done
  else if pr_merged claiming task ∧ gates red/pending/unknown       → In Review (gate surfaced)
  else if pr_opened claiming task                                   → In Review
  else if branch_seen / draft PR claiming task                      → In Progress
  else if contractComplete(task)                                    → Ready
  else                                                              → Draft

claiming(pr, task) := task.key ∈ pr.taskKeys
                      ∨ (task.contract.affects ∩ pr.affected ≠ ∅ ∧ unambiguous)
blocked(task)      := ∃ open dep in contract.deps                    -- flag, not a rung
drift(ws)          := merged PRs whose affected ∩ ⋃ open tasks' affects = ∅
progress(spec)     := fold over its tasks' lifecycles
```

Fold order is log order (`seq`); the fold is deterministic and incremental
(cursor-cached per subject). Ambiguous auto-claims (component overlap matching
multiple open tasks) surface as inbox suggestions, never silent links.

## 7. Graph edges (in the catalog graph, WP-5)

Work kinds register as entity kinds in the shipped service-catalog graph;
edges use the existing typed grammar. Authored edges come from intent
(`spec`, `contract`); derived edges come from the fold; none are hand-written
rows in a side table.

| Edge | From → to | Source |
|---|---|---|
| `partOf` / `hasPart` | Task→Spec | envelope (`spec`) |
| `affects` | Task→Component | contract (`affects`) |
| `blockedBy` / `blocks` | Task→Task | derived from `contract.deps` |
| `implementedBy` | Task→PR/revision ref | **derived** by the fold's claim join |
| `delivers` | Deployment→Task | **derived** from `revision_live` over a claimed merge |
| `assignedTo` | Task→Principal (membership subject) | coordination (`assigned`) |

## 8. Sealed objects (system of proof)

Canonical JSON, framed/addressed per `specs/orun-object-model/`; routed under
the workspace (`ws_` identity, not slugs — renames must not break the chain);
refs under `refs/work/…`. Identity is content (invariant 7).

### 8.1 `SpecSnapshot`

The frozen brief an agent pulls: spec doc + task envelopes + contracts +
authored edges — intent plane only, by type (no lifecycle, no assignment).

```jsonc
{ "kind": "SpecSnapshot", "apiVersion": "orun.io/v1",
  "spec": { /* §2.1 envelope, docRef resolved into the closure */ },
  "tasks": [ { /* §2.2 envelopes with contracts */ } ],
  "catalog": "sha256:…",          // the CatalogSnapshot the affects keys resolved against
  "coordSeq": 18421,              // coordination-log cursor at seal time
  "obsSeq": 90311 }               // observation-log cursor at seal time
```

Ref: `refs/work/specs/<slug>/latest`; history is prior objects. The two
cursors pin exactly which fold a snapshot reflects without embedding any
fold output.

### 8.2 Log segments

Both logs seal periodically as chained segments for audit/offline replay —
`WorkCoordinationSegment` and `WorkObservationSegment`, each
`{ fromSeq, toSeq, entries[], prev: "sha256:…" }` — the `prev` chain makes
both audit logs tamper-evident end-to-end. Refs:
`refs/work/coordination/<ws>/head`, `refs/work/observations/<ws>/head`.

### 8.3 `work://` references

Other planes point at work as
`work://<workspace>/<human-key>[@<specSnapshotId>]` — a pointer plus an
optional frozen-context pin, never embedded state.
