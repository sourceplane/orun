# Design

> Phase 2 of the Orun local state model. This document is the architectural
> contract; the schemas live in `data-model.md`, the storage rules in
> `catalog-store.md`, the resolver rules in `resolution-pipeline.md`, and the
> milestone slicing in `implementation-plan.md`.

---

## 1. Problem

Phase 1 (`specs/orun-state-redesign/`) gave Orun a clean
trigger → revision → execution lineage backed by `internal/statestore`. That
unblocked deterministic plan/run state but left the **component view** weak:

- Components are still treated as execution inputs, not as first-class
  catalog entities with ownership, environments, dependencies, and history.
- There is no canonical answer to "what is the state of `api-edge` on `main`
  right now, vs on PR #139, vs on my dirty worktree?"
- Plans and executions are not queryable from a component-centric page —
  reverse traversal `component → revisions → executions` requires full-tree
  scans.
- Git/source state is implicit. Catalog correctness depends on the worktree
  but is never persisted as a hashable, comparable object.

Phase 2 fixes this by introducing two new parent levels above the existing
revision lineage and making the component catalog a first-class persisted
object with the same correctness guarantees as the Phase 1 state.

## 2. Goals

- **G1.** `ComponentManifest` is a first-class Orun object, fully resolved
  (inherited + inferred + validated), with field provenance.
- **G2.** Every catalog snapshot belongs to an exact `SourceSnapshot`
  identifying the precise Git/worktree state it was resolved from.
- **G3.** Main-branch catalog state is the canonical SaaS source of truth.
  Branches, PRs, and dirty worktrees produce preview snapshots.
- **G4.** Plans and executions live under the catalog snapshot, so
  `component → revisions → executions` is a direct directory walk plus
  per-component indexes.
- **G5.** `component.yaml` stays small and authored. `ComponentManifest` is
  the generated, complete artifact.
- **G6.** SaaS-ready but local-first: every path, key, manifest, and ref must
  be shaped for later remote sync without changes to the core model.
- **G7.** Existing `orun plan`, `orun run`, `orun status`, `orun logs`, and
  `orun describe` behavior continues to work. Backward compatibility is a
  hard requirement.

## 3. Non-goals

- SaaS API server, Cloudflare Durable Objects, Supabase/D1 indexing,
  R2/S3 driver — all Phase 3.
- Backstage / Datadog export adapters.
- TUI catalog screens (consumed by `.kiro/specs/orun-tui-cockpit/` later).
- Distributed locking, remote auth, dirty-preview remote sync.
- Real remote networking. The `Syncer` interface ships with a `NoopSyncer`
  only.

## 4. Lineage

```text
SourceSnapshot                 “exact git/worktree state”
  └─ CatalogSnapshot           “resolved software catalog for that source state”
      ├─ ComponentManifest[]   “fully materialized component definitions”
      ├─ CatalogGraph          “dependency / system / API / resource edges”
      ├─ CatalogIndexes        “catalog-local lookup tables”
      ├─ TriggerOccurrence     ← Phase 1, unchanged
      │   └─ PlanRevision      ← Phase 1, now lives under catalog parent
      │       └─ ExecutionRun  ← Phase 1, unchanged shape
      └─ ComponentHistory      “append-only events per component”
```

Phase 1 lineage shape is preserved; only the **parent path** of revisions
changes. `RevisionKey` format, `executionKey` format (`run-NNN` /
`gh-{run_id}-{attempt}-{sha}` from `internal/runbundle`), and `TriggerOccurrence`
schema are unchanged.

## 5. On-disk layout

Canonical `.orun/` tree (forward slashes; `StateStore` translates separators):

```text
.orun/
  version.json

  sources/
    <sourceSnapshotKey>/
      source.json

      catalogs/
        <catalogSnapshotKey>/
          catalog.json

          components/<componentName>/manifest.json
          graph/{dependencies,systems,apis,resources,owners}.json
          indexes/{components,owners,systems,domains,types}/<key>.json

          revisions/<revisionKey>/
            trigger.json
            revision.json
            plan.json
            manifest.json
            executions/<executionKey>/
              execution.json
              snapshot.latest.json
              state.json
              metadata.json
              logs/
              events/
              artifacts/

          history/components/<componentName>/events/
            <000000001>-<eventKind>.json

  refs/
    sources/{latest,current,main}.json
    sources/branches/<branch>.json
    sources/prs/<pr>.json

    catalogs/{latest,current,main}.json
    catalogs/branches/<branch>.json
    catalogs/prs/<pr>.json

    revisions/latest.json       ← Phase 1, unchanged
    executions/latest.json      ← Phase 1, unchanged

  indexes/
    sources/<sourceSnapshotKey>.json
    catalogs/<catalogSnapshotKey>.json
    components/<componentKey>.json
    revisions/<revisionKey>.json    ← Phase 1, unchanged
    executions/<executionKey>.json  ← Phase 1, unchanged
```

The component-execution and component-revision links are stored both in the
catalog-local `indexes/components/<componentName>.json` (fast walk for the
catalog page) and in the global `indexes/components/<componentKey>.json` (fast
"latest across all sources" lookup).

## 6. Package boundaries

New packages, isolated by ownership:

| Package | Owns | Imports |
|---------|------|---------|
| `internal/sourcectx` | source state, key generation, dirty/tree hashes | go-git or git CLI shell-out, no other internal/ |
| `internal/catalogmodel` | pure data models (no FS) | none |
| `internal/catalogresolve` | repo files → resolved manifests + graph + hashes | `catalogmodel`, `sourcectx` |
| `internal/catalogstore` | persistence via `internal/statestore`, refs/indexes | `catalogmodel`, `statestore`, `revision` |
| `internal/catalogdiff` | source/catalog/component/graph diffs | `catalogmodel` |
| `internal/catalogsync` | future SaaS seam (Phase 2 ships interface + `NoopSyncer`) | `catalogmodel` |

Existing packages this spec composes (no breaking changes):

- `internal/statestore` — only path through which new layout files are written.
- `internal/triggerctx` — produces `TriggerOccurrence`; resolver pipeline
  consumes it to stamp source/catalog keys onto trigger objects.
- `internal/revision`, `internal/executionstate`, `internal/runbundle`,
  `internal/runner`, `internal/trigger` — unchanged on the wire; reference
  paths shift to the catalog-parent layout via path helpers.

## 7. Write order

Catalog snapshot writes are multi-object and not transactional. Phase 1's
**body-before-ref** rule applies. The contract for `orun catalog refresh`:

```text
1. Resolve SourceSnapshot.
2. Resolve CatalogSnapshot + ComponentManifest[] + CatalogGraph in memory.
3. Write source.json (CreateIfAbsent — sources are immutable).
4. Write component manifests.
5. Write graph/*.json.
6. Write catalog.json (CreateIfAbsent — catalogs are immutable).
7. Write catalog-local indexes/*.
8. Write global indexes/sources/<sourceKey>.json,
        global indexes/catalogs/<catalogKey>.json,
        global indexes/components/<componentKey>.json (CompareAndSwap).
9. Write refs/sources/current.json (CompareAndSwap).
10. Write refs/catalogs/current.json (CompareAndSwap).
11. If authoritative main: write refs/sources/main.json + refs/catalogs/main.json.
12. If branch: write refs/{sources,catalogs}/branches/<branch>.json.
13. If PR:     write refs/{sources,catalogs}/prs/<pr>.json.
```

For `orun plan` (extended Phase 1 flow):

```text
... existing trigger resolution ...
resolve SourceSnapshot (same as catalog refresh)
resolve or refresh CatalogSnapshot
select components
compile plan
compute plan hash
derive RevisionKey                       (existing logic, unchanged)
write revision under sources/<srcKey>/catalogs/<catKey>/revisions/<revKey>/
update component-execution-index, component-revision-index
update catalog history events
update refs/revisions/latest.json        (Phase 1, unchanged)
optional compatibility alias under .orun/revisions/<revKey>/ if
  stateCompatibilityWrites is enabled.
```

For `orun run` (extended Phase 1 flow):

```text
resolve revision (global revision index → catalog parent → revision dir)
load parent SourceSnapshot + CatalogSnapshot
create ExecutionRun under revision (Phase 1 layout, unchanged)
mirror via internal/executionstate.Bridge
append component history events
update component-execution-index entry
update ComponentManifest.status iff this catalog snapshot is the local-mutable latest
```

## 8. Correctness invariants

Non-negotiable. Verified by property tests (`test-plan.md`).

1. Every `CatalogSnapshot` belongs to exactly one `SourceSnapshot`.
2. Every `ComponentManifest` belongs to exactly one `CatalogSnapshot`.
3. Every `PlanRevision` belongs to exactly one `CatalogSnapshot`.
4. Every `ExecutionRun` belongs to exactly one `PlanRevision` and is therefore
   transitively traceable to one `CatalogSnapshot` and one `SourceSnapshot`.
5. `refs/catalogs/main.json` is the canonical SaaS source of truth.
6. Non-main refs are preview unless explicitly configured otherwise.
7. Dirty-worktree snapshots are local-only by default.
8. Snapshots are immutable after first write (sources, catalogs, manifests,
   revisions, executions). Refs and indexes are mutable.
9. Indexes are rebuildable from source+catalog tree without data loss.
10. `ComponentManifest` is generated; `component.yaml` is authored intent and
    is never mutated by Orun.
11. Field provenance must be available for every inherited / inferred value
    (`resolution.inheritedFrom`, `resolution.inferredFrom`).
12. Global indexes allow lookup without knowing the source/catalog path.
13. Phase 1 plan/run/status/logs behavior continues to work unchanged from
    the user's CLI perspective.
14. Path helpers in `internal/catalogstore/paths.go` are the only producer of
    `.orun/sources/...` paths; raw concatenation is forbidden.
15. `ExecID` format from `internal/runbundle` is preserved verbatim
    (`gh-{run_id}-{attempt}-{sha}`).

## 9. Alternatives considered

- **Flat catalog under `.orun/catalogs/`, no source parent.** Rejected: cannot
  represent dirty-worktree previews next to canonical `main` without name
  collisions; loses the SaaS authoritative-vs-preview distinction.
- **Catalog as DB-only, computed on demand.** Rejected: breaks remote-shaped
  path goal (G6) and prevents offline/local-first usage. Phase 3 may add a
  DB index on top, but the snapshot files remain canonical.
- **Component identity includes environment.** Rejected: environment is a
  binding, not identity; multiple environments per component is the common
  case. Environments live under `spec.environments`.
- **Mutate `component.yaml` to embed inferred fields.** Rejected: violates the
  authored-vs-resolved separation (G5) and would create churn in user repos.
- **Skip `SourceSnapshot`, fold its fields into `CatalogSnapshot`.** Rejected:
  multiple catalog snapshots can share a source (e.g. resolver-version bump
  with no source change); also blocks future cross-repo source dedup.

## 10. Risk register (summary)

Full register in `risks-and-open-questions.md`. Headlines:

- **CR1.** Resolver non-determinism breaks `catalogHash` stability →
  property test, ordered map encoding, golden-fixture comparison.
- **CR2.** Phase 1 callers depending on `.orun/revisions/<revKey>` path →
  compatibility writes (alias) gated by `stateCompatibilityWrites`, with a
  documented sunset window.
- **CR3.** Dirty-hash churn on every keystroke → restrict dirty inputs to
  catalog-relevant files (intent, component.yaml, stack refs, README/package
  for inference) per `identity-and-keys.md`.
- **CR4.** Two `orun catalog refresh` runs racing the `current` ref →
  `CompareAndSwap` from Phase 1 `StateStore`; loser retries.
- **CR5.** Catalog migration mis-attaches Phase 1 revisions → orphan bucket
  `cat-orphan-<sourceKey>` for revisions with no resolvable catalog.

## 11. Dependency additions

- No new direct dependencies if `go-git` is not adopted; `internal/sourcectx`
  may shell out to `git` or import `github.com/go-git/go-git/v5`. The choice
  is made in C1 with the implementer; both options are spec-compatible.
- Existing `github.com/oklog/ulid/v2` (pinned in Phase 1 M0) is reused for
  `src_`, `cat_`, `cmp_` ULIDs.

## 12. CLI surface (summary)

Full surface in `cli-surface.md`. New commands:

```text
orun catalog refresh [--source ...] [--strict] [--no-infer] [--json]
orun catalog list [--source ...] [--owner ...] [--domain ...] [--type ...] [--status ...]
orun catalog describe <component> [--source ...] [--json]
orun catalog tree [<component>] [--direction ...] [--source ...]
orun catalog diff [--base ...] [--head ...] [<component>]
orun catalog history <component> [--source ...] [--trigger ...] [--profile ...] [--environment ...]
orun catalog refs
orun catalog validate [--strict] [--source ...]
```

New plan flags:

```text
orun plan --no-catalog-refresh
orun plan --catalog-source {current|main|<branch>|pr-<n>}
orun plan --catalog-snapshot <catalogKey>
orun plan --catalog-strict
```

## 13. Out-of-scope reminders

If implementation is tempted to address any of these, file a proposal:

- Real remote sync (`orun catalog refresh --sync`).
- Cross-repo component identity unification.
- Catalog encryption / signing.
- Backstage / Datadog adapters.
- TUI screens for catalog browsing.
- Auto-refresh on every `orun status`.
