# Design

> The architecture. Schemas live in `data-model.md`; the storage contract in
> `object-store.md`; hashing/addressing in `identity-and-keys.md`; the runner
> in `runner-integration.md`; consumers in `remote-and-consumers.md`.

## 1. Goals / non-goals

**Goals**

- **G1 — One model.** A single content-addressed object graph is the source of
  truth for source, catalog, components, plans, revisions, triggers,
  executions, and per-job/step state. The CLI, runner, TUI, and SaaS are
  projections of it.
- **G2 — Content addressing.** Immutable nodes are addressed by the hash of
  their canonical bytes; identical content is stored once.
- **G3 — Tolerant-strict walk.** Every write command walks
  `source → catalog → revision → trigger → execution` in dependency order,
  always materializing a node at each level (degenerate/empty allowed), reusing
  content-addressed parents, and hard-failing only under `--strict`.
- **G4 — Legacy removed, no workarounds.** `internal/state`,
  `internal/statebackend/file`, the executionstate bridge, and the Phase 1/2
  dual-write paths are deleted once the parity matrix (`runner-integration.md`
  §Parity) is satisfied. Capabilities move into the new model natively.
- **G5 — Disk efficiency.** Dedup + zstd + reachability GC. No byte-copy
  mirrors.
- **G6 — Remote-shaped.** The object store has a remote driver seam; sync is
  object substitution. SaaS and TUI consume the same graph.
- **G7 — Inspectable.** A materialized working view + porcelain commands
  preserve `cat`/`jq`-grade introspection for the hot set and on demand.

**Non-goals (this phase)**

- Packfile delta compression (deferred milestone, profiling-gated).
- Production R2/S3 hardening, SaaS auth server, Supabase indexing, DO
  coordination, distributed locking.
- TUI visual redesign — only the **data seam** is in scope.

## 2. The five layers

```
L4  Working view      .orun/current/         materialized, human-readable, rebuildable
L3  Indexes           .orun/index/           derived reverse-lookup caches, rebuildable
L2  Refs              .orun/refs/            mutable name→hash pointers; GC roots
L1  Nodes             (typed objects)        Source/Catalog/Revision/Trigger/Execution/…
L0  Object store      .orun/objects/         content-addressed blob+tree, zstd, immutable
                      .orun/cache/           memoization (resolve), rebuildable
```

**The load-bearing rule:** L0–L1 are immutable and content-addressed. L2 is the
only mutable, authoritative surface. L3–L4 are derived and can be deleted and
rebuilt from L0+L2 at any time. If a derived layer disagrees with L0+L2, the
derived layer is wrong.

### 2.1 L0 — object store

Two structural object kinds, git-shaped (full contract in `object-store.md`):

- **`blob`** — opaque bytes (a `plan.json` body, a log segment, a manifest
  body).
- **`tree`** — a sorted list of entries `{ name, kind ∈ {blob,tree}, id }`.
  A tree's id is the Merkle hash over its entries, so identical subtrees
  collapse to one object.

Every object is stored once, zstd-compressed, at
`objects/<algo>/<hex[:2]>/<hex[2:]>`, named by the hash of its **uncompressed
canonical bytes** (compression never affects identity).

### 2.2 L1 — typed nodes

A node is a **record blob** (`<kind>.json`, canonical JSON, discriminated by a
`kind` field) bundled by a **tree** with its children/artifacts. The node's
**identity** is the hash of its *tree* (the Merkle root) for snapshot-like
nodes, giving "same id ⇒ same entire subtree, transitively."

| Node | Kind | Tree shape (entries) | Identity / addressing |
|------|------|----------------------|-----------------------|
| **SourceSnapshot** | content | `source.json` | git tree+dirty hash (input identity); see `identity-and-keys.md` §3 |
| **CatalogSnapshot** | content | `catalog.json`, `components/` (tree of `<name>.json` blobs), `graph/` (tree) | Merkle root of the catalog tree |
| **ComponentManifest** | content (blob) | n/a (a blob inside `components/`) | hash of manifest bytes |
| **PlanRevision** | content | `revision.json`, `plan.json` | Merkle root of the revision tree = f(plan content, catalog id) |
| **TriggerOccurrence** | event | `trigger.json` (record blob) | hash of the trigger record (unique via ULID+time; does not dedup) |
| **ExecutionRun** | event→sealed | live: working tree (mutable). sealed: `execution.json`, `jobs/` (tree of JobRun trees), `events/`, `logs/`, `artifacts/` | sealed Merkle root |
| **JobRun / JobAttempt / StepAttempt** | content (sealed) | nested trees under the execution | part of the sealed execution Merkle tree |

**Edges are hashes.** `revision.json` carries `catalogId`; `execution.json`
carries `revisionId` and `triggerId`; `catalog.json` carries `sourceId`;
`trigger.json` carries `revisionId`. The graph:

```
sourceId ◄── catalog.json
catalogId ◄── revision.json
revisionId ◄── trigger.json,  revisionId+triggerId ◄── execution.json
```

### 2.3 L2 — refs

Mutable `name → objectId` pointers under `.orun/refs/`, updated by
compare-and-swap. Refs are the **GC roots**. Layout (full list in
`identity-and-keys.md` §8):

```
refs/sources/{current,main}             → SourceSnapshot id
refs/sources/branches/<branch>          → SourceSnapshot id
refs/sources/prs/<pr>                    → SourceSnapshot id
refs/catalogs/{current,main}            → CatalogSnapshot id
refs/catalogs/branches/<branch>          → CatalogSnapshot id
refs/catalogs/prs/<pr>                    → CatalogSnapshot id
refs/revisions/latest                    → PlanRevision id
refs/named/<name>                        → PlanRevision id
refs/triggers/<name>/latest              → TriggerOccurrence id
refs/executions/latest                   → ExecutionRun id (sealed or live)
refs/executions/live/<execId>            → working-tree handle (mutable)
```

A ref body is a small JSON record `{ "kind": "Ref", "target": "<algo>:<hex>",
"updatedAt": "…", "writer": "cli|runner|tui|saas" }` so a ref move is itself an
atomic object write (temp+rename / CAS).

### 2.4 L3 — indexes

Derived reverse-lookup caches under `.orun/index/`, rebuildable by walking the
graph from refs:

```
index/components/<componentKey>.json     → revisions[], latestExecutions[]
index/executions/by-time/<bucket>.json   → execIds (newest first)
index/executions/by-status/<status>.json → execIds
index/sources/<sourceId>.json            → catalogIds
index/catalogs/<catalogId>.json          → revisionIds
```

Indexes are **never authoritative**. `orun reindex` rebuilds them; resolvers
fall back to a graph walk on index miss.

### 2.5 L4 — working view

A materialized, human-readable checkout under `.orun/current/` for the **hot
set**: the current source+catalog, and active + recently-sealed executions.
Rebuildable from L0+L2 (`orun checkout`/`orun materialize`). This is the
inspectability surface — plain JSON files you can `cat`/`jq`. It is a cache: it
may be deleted at any time without data loss.

## 3. The tolerant-strict write walk

Every state-producing command runs the same ordered walk. Each step reuses an
existing content node when its hash already exists (`Has` → skip write).

```
1. ResolveSource      → SourceSnapshot   (sourcectx; degenerate = local-nogit)
2. ResolveCatalog     → CatalogSnapshot  (memoized: cache/resolve/<srcId>-<rv> → catalogId;
                                          on miss, run resolver pipeline, write tree)
3. ResolveRevision    → PlanRevision     (compile plan; revisionId = hash(plan, catalogId);
                                          Has(revisionId) ⇒ reuse, else write tree)
4. RecordTrigger      → TriggerOccurrence (always a new event; points at revisionId)
5. (run only) Execute → ExecutionRun     (open working tree; seal on terminal)
6. Move refs (CAS), update indexes, refresh working view.
```

Properties:
- **Reuse is automatic** — steps 1–3 are `Has`-gated; identical inputs cost a
  hash, not a write.
- **Memoization** — step 2 short-circuits the 14-stage resolver when the source
  id + resolver version are unchanged (Nix input-addressing). Clean tree ⇒ near-
  instant.
- **Divergence** — steps 4–5 always append events; the trigger→revision edge is
  many-to-one.
- **Tolerance** — a non-git workspace yields `local-nogit` source + (possibly
  empty) catalog; both are valid terminal nodes. `--strict` promotes resolution
  *errors* (not validation *issues*) to hard failures.
- **Stop point** — `orun plan` stops after step 4; `orun run` continues through
  step 6.

## 4. Atomicity & concurrency

- **Objects are write-once.** Same id ⇒ same bytes ⇒ writes never conflict.
  Write is temp-file + rename; a crash leaves an orphan object (inert; GC'd).
- **Refs use CAS.** `UpdateRef(name, oldId, newId)`; losers retry. The ref move
  is the **publish point**: write the full object closure first, move the ref
  last. A reader never observes a ref pointing at an absent or partial object.
- **Live executions** hold a working-tree lock (`refs/executions/live/<id>` +
  a lockfile, git-`index.lock`-style) with crash recovery on next open.

## 5. Correctness invariants (regression-tested)

1. **Content integrity.** For every object `o`, `hash(canonicalBytes(o)) == id(o)`.
   `orun fsck` verifies the whole store.
2. **Closure completeness.** Every ref target, and every hash referenced by a
   reachable object, exists in the store (no dangling edges among reachable
   objects).
3. **Reuse.** Writing byte-identical content twice yields one object and the
   same id. Two triggers producing identical plans share one revision id.
4. **Dedup of subtrees.** Two catalogs differing only in one component share all
   other component blobs and any identical subtrees.
5. **Seal immutability.** A sealed execution's id never changes; re-sealing
   identical content is idempotent.
6. **Ref publish ordering.** No ref ever points at an id whose closure is not
   fully present.
7. **Derived rebuildability.** Deleting `.orun/index/` and `.orun/current/` and
   rebuilding yields byte-identical derived state.
8. **GC safety.** GC never removes an object reachable from a ref or within a
   retained closure; GC is safe to interrupt.
9. **Tolerant totality.** `plan`/`run` succeed on a non-git, component-less
   workspace, producing `local-nogit` source + empty catalog nodes.
10. **Migration idempotence.** Running `orun migrate` twice ingests the same
    objects and moves refs to the same targets.

## 6. Package map

| Package | Owns | Replaces / relation |
|---------|------|---------------------|
| `internal/objectstore` | L0: object model, hashing, canonical encoding, zstd, loose layout, `ObjectStore` interface, local + remote drivers, GC | generalizes `internal/statestore` (which is retired into it) |
| `internal/objectstore/refstore` | L2 refs, CAS | new |
| `internal/nodes` | L1 node schemas, marshal/validate, tree assembly, edge accessors | merges `revision`, `executionstate`, `triggerctx`, `catalogmodel` record types |
| `internal/nodewriter` | the tolerant-strict walk, Has-gated reuse, resolve memoization | merges `revision.WriteRevision`, `catalog_plan_resolve`, `executionstate.CreateExecution` |
| `internal/objindex` | L3 indexes, reindex | merges `catalogstore` indexes + Phase 1 indexes |
| `internal/workingview` | L4 materialize/checkout | new |
| `internal/runner` | execution working tree + native job/step + seal | rewritten behind `ORUN_OBJECT_RUNNER`; legacy path deleted at M12 |
| `internal/objremote` | remote substitution driver (file://, R2/S3 seam), push/pull | new; Phase 2 `catalogsync` Syncer folds in |
| **deleted** | `internal/state`, `internal/statebackend/file`, `internal/executionstate/bridge.go`, Phase 1/2 dual-write writers | — |

Resolver inputs (`internal/sourcectx`, `internal/catalogresolve`) are **reused
unchanged** — they already produce content-addressable values; only their
*persistence* moves into `nodewriter` + `objectstore`.

## 7. What stays the same

- The resolver pipeline (discover→load→infer→inherit→validate→hash) and
  `sourcectx` identity rules — they already compute content hashes.
- `TriggerOccurrence` semantics (declared/system flavors) and `executionKey`
  formats (`run-NNN`, `gh-{run_id}-{attempt}-{sha}`) — preserved as fields.
- The "short human keys, fat JSON, inspectable" philosophy — preserved via L4 +
  porcelain, now backed by a dedup'd store instead of copied directories.
