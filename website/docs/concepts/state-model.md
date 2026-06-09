---
title: State model
---

`orun` keeps a complete history of every catalog it has resolved, every plan it
has compiled, and every execution it has run as a **content-addressed object
graph** under `.orun/objectmodel/`. This object model is the **single**
persistence stack — the legacy revision/catalog file store was retired in
v2.15.0, so there is one store and one write path.

This page documents the layout. It is the same layout the future R2/S3 and Orun
Cloud drivers will use; only the storage backend changes.

## Why content addressing

Every record is named by the hash of its content, and identical content is stored
once. That gives three properties for free:

- **Immutable history.** A node never changes after it is written; new state is a
  new node. Tampering is detectable (a read verifies `hash(body) == id`).
- **Cheap reuse.** Re-resolving an unchanged workspace, or re-running the same
  revision, re-derives the same ids and writes nothing new — it is a ref move, not
  a rewrite.
- **Trivial sync.** Because ids are content hashes, pushing or pulling to a remote
  store is a set difference (`Has`-before-`Put`).

The chain it records is explicit:

```
SourceSnapshot  →  Catalog  →  Revision  →  Execution  →  Job → Attempt → Step
   the VCS state     the         the plan      one run       the work, with logs
   at resolve time   resolved     + trigger
                     component    that produced
                     graph        it
```

## On-disk layout

Two directories sit under the object-model root (`.orun/objectmodel/`):

```
.orun/objectmodel/
├── objects/
│   └── <algo>/<hex[:2]>/<hex[2:]>     # immutable, content-addressed nodes
│                                      # (zstd-compressed). blobs = record bodies
│                                      # & raw logs; trees = the Merkle nodes that
│                                      # link a Source/Catalog/Revision/Execution
│                                      # to its record and children.
└── refs/
    └── <name>.json                    # the mutable layer: a named pointer
                                       # { "kind":"Ref", "target":"<algo>:<hex>",
                                       #   "updatedAt":..., "writer":... }
```

Objects are immutable and append-only; **all** mutation lives in `refs/`. A ref
write is an atomic temp-file + `fsync` + rename, and updates use a
compare-and-swap (with a per-ref lockfile locally, conditional writes remotely),
so concurrent writers never corrupt a pointer.

### The refs

Refs are logical paths under `refs/`. The standard namespaces:

| Ref | Points at |
| --- | --- |
| `sources/current`, `sources/main`, `sources/branches/<name>`, `sources/prs/<n>` | the latest `SourceSnapshot` for a scope |
| `catalogs/current`, `catalogs/main` | the latest resolved `Catalog` snapshot |
| `revisions/latest`, `revisions/by-hash/<planHash>` | a `PlanRevision` (trigger + plan) |
| `executions/latest` | the newest sealed execution across all revisions |
| `executions/by-id/<execId>` | a sealed execution by its execution id |
| `executions/live/<execId>` | an in-flight run's working handle (removed on seal) |

`orun status`, `orun logs`, `orun describe`, and `orun get` resolve a single ref
(default `executions/latest`, or `executions/by-id/<execId>` for `--exec-id`) and
walk the graph from there.

## The component catalog

The `catalogs/current` ref points at the resolved **component catalog**: the
component set, its dependency graphs, and an `impact/` index (an ownership map
plus per-component fingerprints).

This catalog is the read model for **change detection** (`orun plan/run --changed`,
[`orun catalog affected`](../cli/orun-catalog.md)) and the cockpit's component view.
Because it is content-addressed, re-resolving an unchanged workspace is a cheap ref
move rather than a rewrite. `orun catalog refresh` writes it explicitly; `orun plan`
and a universal pre-run refresh hook keep `catalogs/current` fresh transparently,
and the cockpit refreshes it on open. See [`orun catalog`](../cli/orun-catalog.md)
for the full command group and [v2.14.0 release notes](../release-notes/v2.14.0.md)
for the change-detection engine.

## PlanRevision

A `PlanRevision` is the immutable pairing of the **trigger occurrence** (the *why*
of a plan — a CI event, a declared trigger binding, or a `system.manual` trigger
for ad-hoc local runs) and the **compiled plan** (the *what*). Re-running the same
trigger with an unchanged plan re-derives the same revision (idempotent);
recompiling against a changed plan or commit creates a new revision next to it.
`revisions/latest` always points at the most recently created revision.

See [Trigger bindings](./trigger-bindings.md) for how a declared trigger matches a
CI event.

## ExecutionRun: live, then sealed

`orun run` records an execution in two phases:

1. **Live.** On start, it publishes an `executions/live/<execId>` handle and
   builds the run in an ephemeral working tree (`run/` under the object-model
   root). Each state tick and step log is projected into that working tree so
   in-flight readers (`orun status`, the cockpit Activity view) can follow the run.
2. **Sealed.** On finish, the working tree is written into immutable `objects/`,
   `executions/latest` (and `executions/by-id/<execId>`) is moved to the sealed
   root, and the live handle and working tree are removed.

There is a **single write path** — the runner writes the object graph directly.
The legacy dual-write (mirroring runner output into a separate revision/execution
file layout) was removed in v2.15.0.

Cross-run resume (`--exec-id`) reuses a prior run's sealed jobs: a job that already
succeeded is skipped and re-projected into the new execution, **carrying its prior
step logs forward** so the resumed seal is complete.

## Garbage collection

Because objects are immutable and shared, unreferenced nodes accumulate. `orun gc`
is a mark-and-sweep over reachability from refs plus a retention policy (keep the
last N sealed executions per scope, keep everything named or reachable from a kept
node):

```bash
orun gc --dry-run                 # report what would be reclaimed
orun gc --keep 20                 # keep the last 20 executions per scope
orun gc --prune-older-than 720h   # plus an age cutoff
```

GC is safe to interrupt — it deletes only proven-unreachable objects — and a grace
window protects objects newer than a recent seal so in-flight closures are never
collected.

## Resolution chain

`orun status`, `orun logs`, `orun describe`, and `orun run` follow the same
resolver against the object reader over `.orun/objectmodel`:

1. `--exec-id <id>` → `executions/by-id/<id>`.
2. `--revision <key>` → that revision's latest execution.
3. (default) → `executions/latest`.

From the resolved execution node, the reader walks to its jobs, attempts, steps,
and log blobs.

## Upgrading from a pre-v2.15.0 workspace

Earlier releases also wrote a parallel **legacy** layout — flat `.orun/plans/` and
`.orun/executions/` directories (and, on the v2.10–v2.14 line, a revision-first
`.orun/revisions/<key>/...` tree). v2.15.0 removed that layout and the
`orun state migrate` command that produced it. orun no longer reads or writes those
paths; they are inert and can be deleted once you no longer need the historical
files. The object model itself is unchanged, so catalogs and executions written by
recent releases remain readable.

## What is *not* in v1

- **R2 / S3 / Cloud object-store drivers.** The local driver is the only driver
  shipping today. The interface is frozen so remote drivers can be added without
  changing callers.
- **Packfiles.** Objects are stored loose (one zstd file per object) with a
  two-char fanout; packing with delta compression is a deferred, profiling-gated
  milestone.
- **Cross-host distributed locking.** Concurrent writes from a single host are
  safe via compare-and-swap on refs; cross-host coordination is a later problem.

## References

- [`orun plan`](../cli/orun-plan.md) — emits a fresh revision on every successful
  compile.
- [`orun run`](../cli/orun-run.md) — the live → sealed execution write path.
- [`orun describe`](../cli/orun-describe.md) — `revision`, `trigger`, and
  `execution` aliases.
- [`orun catalog`](../cli/orun-catalog.md) — the catalog command group over the
  object model.
- [Trigger bindings](./trigger-bindings.md) — how declared triggers match CI events.
