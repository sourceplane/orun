# Data Model

> Every persisted schema for the work plane: keys, the entity shapes, the task
> contract, the `WorkEvent` log, the D1 tables, the relation edge vocabulary,
> and the sealed objects (`SpecSnapshot`, `WorkLedgerSegment`). JSON is the
> wire/on-disk form; `lowerCamelCase`, object IDs `"<algo>:<hex>"`, RFC 3339 / Z.
> "MUST/SHOULD/MAY" carry RFC 2119 weight here. Go shapes live in
> `internal/work`; the D1 DDL ships with the backend Worker; sealed shapes are
> canonical JSON per `specs/orun-object-model/` framing.

## 1. Identity & keys

| Thing | Internal id | Human key | Example |
|---|---|---|---|
| Org / project | — | routing segments (existing remote scheme) | `acme/platform` |
| Initiative | ULID `ini_…` | slug | `acme/platform/initiatives/portal-ga` |
| Epic | ULID `epc_…` | slug | `acme/platform/epics/orun-work` |
| Task | ULID `tsk_…` | `<PREFIX>-<seq>` | `ORN-142` |
| Principal | ULID `prn_…` | handle | `@rahul`, `@claude-agent` |
| WorkEvent | ULID `wev_…` | — | — |

- `PREFIX` MUST be 2–5 uppercase `[A-Z]`, unique per project; `seq` is a
  monotonically increasing integer allocated by the project's Durable Object
  (WD-9). The pair is immutable once issued; canceled tasks never free keys.
- Slugs MUST match `^[a-z0-9-]+$` and are unique within their parent scope.
- The fully-qualified work key is `<org>/<project>/<human-key>`
  (`acme/platform/ORN-142`); short forms resolve within a project context.
- Component keys referenced from contracts are the catalog's three-segment
  `<namespace>/<repo>/<name>` and MUST validate against the current catalog at
  edit time (Q-5: failures degrade to `unresolved`, never silently dropped).

## 2. Entity shapes

One envelope; `kind`-specific fields under `spec`. The mutable runtime fields
(`status`, `assignees`, `ordering`, counters) live in projections (§5), not in
the envelope — the envelope is what seals.

```jsonc
{
  "apiVersion": "orun.io/v1",
  "kind": "Task",                       // Initiative | Epic | Task
  "id": "tsk_01J9XQ4N8…",
  "key": "ORN-142",                     // human key (slug for Epic/Initiative)
  "project": "acme/platform",
  "title": "Route catalog reads through the objcatalog view",
  "doc": "…markdown body…",             // optional; epics: the spec document
  "parent": "acme/platform/epics/orun-work",   // partOf target (Task→Epic, Epic→Initiative)
  "cycle": "2026-W24",                  // MAY; project-defined cycle id
  "labels": { "area": "catalog" },      // MAY; flat string map
  "contract": { /* §3 */ },             // MAY; Task only
  "createdBy": { "type": "user", "id": "prn_…" },
  "createdAt": "2026-06-11T09:00:00Z"
}
```

| Field | Req. | Notes |
|---|---|---|
| `kind`, `id`, `key`, `project`, `title` | MUST | identity core; `title` is the only authoring requirement — everything else is progressive |
| `doc` | MAY | markdown; for imported epics this is the source `README.md`/design body and MUST round-trip losslessly (Q-4) |
| `parent` | MUST for Task | Tasks parent to an Epic (or directly to a project backlog pseudo-epic); Epics MAY parent to an Initiative |
| `contract` | MAY | §3; presence + completeness derive `agentReady` |
| `createdBy` | MUST | a principal ref (§6) |

## 3. The task contract

The spec-milestone convention (`Goal / Deps / Done when / Design refs`) as
schema. All fields optional individually; **completeness** (goal + ≥1 affects +
≥1 doneWhen + ≥1 gate, all `affects` resolved) derives `agentReady`.

```jsonc
{
  "goal": "Catalog reads route through the objcatalog view",
  "affects": ["sourceplane/orun/api-edge", "sourceplane/orun/web"],
  "doneWhen": [
    "changed_parity_test green against legacy selection",
    "coverage >= 90% on internal/objcatalog"
  ],
  "gates": ["tests", "parity", "review"],
  "designRefs": ["epic://acme/platform/epics/orun-work#design"],
  "deps": ["ORN-140"]                    // sugar: materialized as blockedBy edges
}
```

| Field | Req. | Notes |
|---|---|---|
| `goal` | SHOULD | one or two sentences; the agent brief's first line |
| `affects[]` | SHOULD | catalog component keys; the delivery bridge's join key; resolution state per key: `resolved \| unresolved` |
| `doneWhen[]` | SHOULD | human-readable acceptance criteria; rendered as a checklist; not machine-verified (the `gates` are) |
| `gates[]` | SHOULD | named checks that MUST verify green (from orun execution truth, Q-3) before automation may move the task to Done |
| `designRefs[]` | MAY | `epic://`, `https://`, or repo-relative refs |
| `deps[]` | MAY | task keys; the mutator materializes them as `blockedBy` edges and keeps them in sync |

## 4. The `WorkEvent` log (the truth)

Append-only; one event per mutation; projections derive from it (invariant 2).

```jsonc
{
  "eventId": "wev_01J9XQ7M2…",
  "project": "acme/platform",
  "subject": "ORN-142",                  // work key the event applies to
  "kind": "status_changed",              // §4.1 vocabulary
  "actor": { "type": "automation",       // user | agent | automation
              "id": "bridge/pr-linker",  // principal id, or automation rule id
              "via": "github-webhook" }, // MAY: mcp | ui | cli | webhook
  "at": "2026-06-11T09:14:03Z",
  "payload": { "from": "in_progress", "to": "in_review",
               "cause": { "pr": "sourceplane/orun#412" } },
  "seq": 18421                           // per-project total order (DO-assigned)
}
```

### 4.1 Event kinds (v1, closed set)

`item_created · item_edited · status_changed · assigned · unassigned ·
comment_added · link_added · link_removed · contract_edited · moved ·
cycle_changed · labeled · unlabeled · sealed · imported · canceled`

- The set is closed per schema version; an unknown kind is a **write-time
  error** (no forward-compat dumping ground — extending the set is a schema
  rev).
- `seq` is assigned by the project Durable Object and is the client sync
  cursor (design §7).
- `actor` MUST be present on every event; automated transitions MUST NOT be
  attributed to a human (invariant 4).

## 5. D1 tables (system of record)

DDL sketch; authoritative DDL ships with the Worker migration.

```sql
work_items   (id TEXT PK, project TEXT, kind TEXT, key TEXT, title TEXT,
              doc TEXT, parent TEXT, cycle TEXT, labels JSON, contract JSON,
              created_by JSON, created_at TEXT,
              UNIQUE (project, key));
work_events  (event_id TEXT PK, project TEXT, subject TEXT, kind TEXT,
              actor JSON, at TEXT, payload JSON, seq INTEGER,
              UNIQUE (project, seq));
work_links   (project TEXT, from_key TEXT, from_kind TEXT, type TEXT,
              to_key TEXT, to_kind TEXT, created_by JSON, created_at TEXT,
              PRIMARY KEY (project, from_key, type, to_key));
work_status  (project TEXT, key TEXT, status TEXT, assignees JSON,
              board_order REAL, updated_seq INTEGER,
              PRIMARY KEY (project, key));        -- projection (rebuildable)
work_cursors (project TEXT, consumer TEXT, seq INTEGER,
              PRIMARY KEY (project, consumer));   -- seal + sync bookkeeping
```

- `work_status` (and any further read models) are **projections**: dropping and
  replaying `work_events` MUST reproduce them byte-for-byte (invariant 2).
- Mutators append the event and update projections in one transaction
  (invariant 3).

## 6. Principals

Humans and agents, uniformly (WD-10). Until `orun-service-catalog` lands its
`User`/`Group` kinds, principals live in D1 and map onto the backend's existing
GitHub identities (`internal/remotestate/auth.go`).

```jsonc
{ "id": "prn_01J9…", "type": "agent",          // human | agent
  "handle": "claude-agent", "displayName": "Claude (coding agent)",
  "github": { "userId": 9216234 },              // humans: the portable id (per orun-secrets SD-4)
  "owner": "prn_01J8…" }                        // agents MUST name a responsible human/team
```

## 7. Relation edges (work vocabulary)

Same grammar as the catalog graph (`{from, fromKind, type, to, toKind,
optional}`); stored in `work_links` until SC2 unifies the graphs (design §5).

| `type` | from → to | Writer |
|---|---|---|
| `partOf` / `hasPart` | Task→Epic, Epic→Initiative | mutator (from `parent`) |
| `affects` | Task→Component | mutator (from `contract.affects`) |
| `blockedBy` / `blocks` | Task→Task | mutator (from `contract.deps` or UI) |
| `implementedBy` | Task→revision/PR ref | the auto-linker (design §6.1) |
| `delivers` | Deployment→Task | the Released automation (design §4.3) |
| `assignedTo` | Task→Principal | mutator |

## 8. Sealed objects (system of proof)

Canonical JSON, framed and addressed per `specs/orun-object-model/`; routed
under `orgs/<org>/projects/<project>/…`; refs under `refs/work/…`. Sealing is
one-way (WD-7); identity is content (invariant 6).

### 8.1 `SpecSnapshot`

The frozen epic an agent pulls: doc + task envelopes + contracts + links — no
hot state (no status/assignees; invariant 1).

```jsonc
{ "kind": "SpecSnapshot", "apiVersion": "orun.io/v1",
  "epic": { /* §2 envelope, incl. doc */ },
  "tasks": [ { /* §2 envelopes with contracts */ } ],
  "links": [ { /* §7 edges, work-internal + affects */ } ],
  "catalog": "sha256:…",                 // the CatalogSnapshot the affects keys resolved against
  "ledgerSeq": 18421 }                   // the event seq this snapshot reflects
```

Ref: `refs/work/epics/<slug>/latest` → the snapshot's ObjectID; history is
prior objects (immutable, GC per retention).

### 8.2 `WorkLedgerSegment`

Batched event ranges for audit/offline replay:

```jsonc
{ "kind": "WorkLedgerSegment", "apiVersion": "orun.io/v1",
  "project": "acme/platform", "fromSeq": 18000, "toSeq": 18421,
  "events": [ /* §4 events, verbatim */ ],
  "prev": "sha256:…" }                   // previous segment → a verifiable chain
```

Ref: `refs/work/ledger/<project>/head`. The `prev` chain makes the audit log
tamper-evident end-to-end.

### 8.3 `work://` references

Where other planes need to point at work (e.g. a run annotated with the task it
implements), the reference grammar is
`work://<org>/<project>/<human-key>[@<specSnapshotId>]` — a pointer plus an
optional frozen-context pin, never embedded state.
