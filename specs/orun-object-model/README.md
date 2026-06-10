# Spec: orun-object-model

**Phase 3 of the Orun state model — the unification.** A full rewrite of Orun's
persisted state onto a single content-addressed object graph (git/Nix-shaped),
replacing the Phase 1 (`orun-state-redesign`) and Phase 2
(`orun-component-catalog`) layouts and **removing the legacy `internal/state`
module entirely** — no bridge, no dual-write, no compatibility workarounds.

This is the new **main model**. Everything else becomes a projection of it:
the CLI, the runner, the TUI cockpit, and the SaaS backend all read and write
the same object graph through the same interfaces.

## Status

| Field | Value |
|-------|-------|
| Status | **Draft → Ready for implementation** |
| Phase | 3 of 3 — unification + content-addressed rewrite |
| Predecessors | `specs/archive/orun-state-redesign/` (Phase 1), `specs/archive/orun-component-catalog/` (Phase 2) |
| Supersedes | the Phase 1 global layout (`revisions/…`) and the Phase 2 catalog-parent **mirror** (both collapse into one object graph) |
| Target branch | `main` (PRs merged incrementally) |
| Decisions locked | hybrid store (CAS + materialized working view); revisions dedup across triggers; loose+zstd+GC first (packfiles deferred); runner rewrite staged behind `ORUN_OBJECT_RUNNER`; sha256 (pluggable algo) |

## The one-paragraph thesis

Orun's state is a **DAG of immutable, content-addressed objects** with a thin
layer of **mutable refs** on top, exactly like git. Sources, catalogs,
component manifests, plans, revisions, and sealed executions are *content*:
addressed by the hash of their bytes, stored once, deduplicated, compressed,
and garbage-collected by reachability. Triggers and in-flight executions are
*events*: append-only, pointing at content by hash. "Reuse the same first hops,
diverge at the trigger" is not a feature we build — it is what content
addressing **is**. Remote sync (SaaS) becomes object substitution (git push /
Nix binary cache): push the hashes the remote lacks, move the remote ref. The
TUI and SaaS consume the identical graph.

## Lineage (the object graph)

```
SourceSnapshot      (content)  ── git/worktree state, addressed by tree+dirty hash
   ▲ resolve (memoized)
CatalogSnapshot     (content)  ── Merkle root over component manifests + graph
   ▲ catalogHash ref
PlanRevision        (content)  ── compiled plan + catalog ref; addressed by plan content
   ▲ revisionHash ref          (MANY triggers → ONE revision when plans are identical)
TriggerOccurrence   (event)    ── append-only; points at the revision it produced
   ▲ triggerHash ref
ExecutionRun        (event→sealed) ── live = mutable working tree; terminal = content
   └─ JobRun → JobAttempt → StepAttempt  (native; finally implemented)

ComponentHistory    (event log) ── per-component event stream, derived/indexed
```

Edges point at hashes. The spine (source→catalog→revision) is shared content;
the leaves (trigger, execution) are events.

## Read order

New contributors and implementing agents read in this order:

1. **`why-this-model.md`** — the rationale. Why a content-addressed object
   graph, what it buys, what it costs, and the argument for the rewrite. Team-
   and reviewer-facing.
2. **`design.md`** — the five-layer architecture (object store, nodes, refs,
   indexes, working view), the immutable/mutable split, correctness
   invariants, the kill plan for legacy.
3. **`object-store.md`** — the **L0 contract**: object model (`blob`/`tree`),
   hashing, canonical encoding, compression, the `ObjectStore` interface,
   atomicity, and garbage collection. This is the frozen contract.
4. **`identity-and-keys.md`** — hashing algorithm, addressing, node identity
   derivation, the resolve memo-cache, dedup rules, ref naming.
5. **`data-model.md`** — every node schema (JSON) field-by-field.
6. **`runner-integration.md`** — the working-tree/seal model, native
   job/attempt/step writers, the legacy parity matrix, and the staged kill of
   `internal/state` / `internal/statebackend` / the bridge.
7. **`remote-and-consumers.md`** — remote object substitution (SaaS), and how
   the TUI cockpit and SaaS backend consume the model to view and start runs.
8. **`cli-surface.md`** — command behavior + new porcelain (`orun cat`,
   `orun show`, `orun log`, `orun ls-tree`, `orun gc`, `orun fsck`).
9. **`compatibility-and-migration.md`** — one-shot legacy ingest, additive,
   never destructive.
10. **`claude-goals.md`** — operating goals, constraints, and definition-of-
    done for the implementing Claude agents.
11. **`implementation-plan.md`** — milestones **M0 → M13**, each with goal,
    dependencies, suggested PR scope, and "done when" criteria.
12. **`test-plan.md`** — coverage targets, property tests, E2E walk.
13. **`risks-and-open-questions.md`** — live risk and decision register.

## Phase boundaries

| In scope (this spec) | Out of scope |
|----------------------|--------------|
| `internal/objectstore` (CAS, blob/tree, hashing, zstd, loose layout, GC); refs; indexes; materialized working view; all node schemas as objects; resolve memoization; runner working-tree/seal + native job/step; CLI rewire + porcelain; remote-substitution interface + a directory/`file://` remote driver; TUI/SaaS consumption seam; one-shot legacy migration; **deletion of `internal/state`, `internal/statebackend/file`, the executionstate bridge, and the Phase 1/2 dual-write paths** | Production R2/S3 driver hardening; packfile delta compression; Supabase/D1 index service; Durable-Object coordination; distributed locking; real SaaS auth server; TUI visual redesign (only the data seam is in scope) |

## Document conventions

- Go for interfaces, JSON for on-disk schemas. Forward-slash logical paths,
  root-relative to `.orun/`.
- Object IDs are `"<algo>:<hex>"` in JSON (e.g. `"sha256:9f86d0…"`); on disk
  the object lives at `objects/<algo>/<hex[:2]>/<hex[2:]>`.
- Event IDs keep ULID type prefixes: `trg_`, `exec_`. Content nodes have **no**
  separate ID — their identity *is* their object hash.
- Times are RFC 3339 / Z. JSON is canonical: stable key order, no insignificant
  whitespace, trailing newline omitted inside objects (the hash is over the
  canonical bytes).
- "MUST / SHOULD / MAY" carry RFC 2119 weight inside contract docs
  (`object-store.md`, `identity-and-keys.md`).

## Out-of-band references

- Predecessor specs: `specs/archive/orun-state-redesign/`, `specs/archive/orun-component-catalog/`.
- Existing packages this spec consumes or replaces: `internal/statestore`
  (generalized into `internal/objectstore`), `internal/revision`,
  `internal/executionstate`, `internal/triggerctx`, `internal/sourcectx`,
  `internal/catalogresolve`, `internal/catalogstore`, `internal/state`
  (**deleted**), `internal/statebackend` (**file driver deleted**),
  `internal/runner` (**rewritten behind a flag**).
