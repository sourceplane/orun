# Sync Model (Future Seam)

Phase 2 ships **only** the local catalog. This document defines the future
remote contract so the local model is shaped correctly today and avoids a
breaking redesign when Phase 3 adds the SaaS sync.

What this phase delivers:

- `internal/catalogsync.Syncer` Go interface.
- `NoopSyncer` implementation (default; no networking).
- `SyncPayload` value types matching the local on-disk model.
- `orun catalog refresh --sync` that reports `remote sync not configured`.

What this phase **does not** deliver: any HTTP client, any auth, any DB
schema. Implementation of those belongs to a Phase 3 spec.

---

## 1. `Syncer` interface

```go
package catalogsync

type PushOptions struct {
    DryRun         bool
    AllowDirty     bool
    Reason         string                 // free-form, included in audit log
    ExtraMetadata  map[string]string
}

type PushResult struct {
    Accepted       bool
    RemoteSourceKey  string
    RemoteCatalogKey string
    Warnings       []string
}

type Syncer interface {
    PushCatalogSnapshot(
        ctx context.Context,
        snapshot SyncPayload,
        opts PushOptions,
    ) (PushResult, error)
}
```

`SyncPayload` is the union of everything needed to reconstruct the snapshot
remotely; it intentionally mirrors the local file shapes so a future remote
driver can stream files without translation:

```go
type SyncPayload struct {
    Source      catalogmodel.SourceSnapshot
    Catalog     catalogmodel.CatalogSnapshot
    Manifests   []catalogmodel.ComponentManifest
    Graphs      catalogmodel.CatalogGraphs
    LocalIndexes catalogmodel.CatalogLocalIndexes
    Refs        catalogmodel.RefUpdate
    HistoryEvents []catalogmodel.ComponentCatalogEvent
}
```

## 2. `NoopSyncer`

```go
type NoopSyncer struct{}

func (NoopSyncer) PushCatalogSnapshot(ctx context.Context,
    p SyncPayload, opts PushOptions) (PushResult, error) {
    return PushResult{
        Accepted: false,
        Warnings: []string{"remote sync not configured (Phase 3)"},
    }, nil
}
```

The CLI wires `NoopSyncer` by default; the `--sync` flag shows the warning
and exits 0.

## 3. Future remote object layout

When Phase 3 lands, the remote driver must accept the same path shape:

```text
orgs/<org>/projects/<project>/repos/<repo>/
  sources/<sourceSnapshotKey>/
    source.json
    catalogs/<catalogSnapshotKey>/
      catalog.json
      components/<componentName>/manifest.json
      graph/{dependencies,systems,apis,resources,owners}.json
      revisions/<revisionKey>/...
      history/components/<componentName>/events/*.json

  refs/
    sources/{latest,current,main}.json
    sources/branches/<branch>.json
    sources/prs/<pr>.json
    catalogs/{latest,current,main}.json
    catalogs/branches/<branch>.json
    catalogs/prs/<pr>.json

  indexes/
    sources/<sourceSnapshotKey>.json
    catalogs/<catalogSnapshotKey>.json
    components/<componentKey-sanitized>.json
```

The only delta from the local layout is the `orgs/<org>/projects/<project>/
repos/<repo>/` prefix. `internal/catalogstore.Path*` helpers accept an
optional prefix (Phase 3 plumbing) so no caller code changes when the
remote driver lands.

## 4. Future DB tables

Indicative; final schema belongs to Phase 3. Listed here only to validate
that the data-model is decomposable:

```text
source_snapshots(source_snapshot_key, repo, branch, head_revision,
                 tree_hash, working_tree, dirty_hash, created_at, ...)

catalog_snapshots(catalog_snapshot_key, source_snapshot_key, catalog_hash,
                  authoritative, preview, summary_components, ...)

component_manifests(component_id, component_key, catalog_snapshot_key,
                    manifest_hash, owner, lifecycle, system, type, ...)

component_edges(catalog_snapshot_key, from_key, to_key, type, optional)

component_execution_links(catalog_snapshot_key, component_key,
                          revision_key, execution_key, profile,
                          environment, status, created_at)

component_revision_links(catalog_snapshot_key, component_key, revision_key,
                         created_at)

source_refs(name, source_snapshot_key, authoritative, updated_at)
catalog_refs(name, catalog_snapshot_key, authoritative, preview, updated_at)
```

Component identity is `component_key` (string). Per-source state lives in
the link tables — never on the component row itself. This matches the
local convention where `ComponentManifest.status` is mirrored into a
sibling index, not mutated on the manifest.

## 5. SaaS write rules

```text
clean main (canonical branch in intent.yaml.catalog.sourceOfTruth):
  authoritative = true
  updates canonical component state
  triggers canonical re-index

clean other protected branch:
  authoritative = configurable; default false unless listed in
  catalog.sourceOfTruth.canonicalBranches

PR:
  authoritative = false
  creates preview catalog scoped to pr-<n>
  cleared automatically on PR close (TTL = 7d default)

feature branch (non-protected):
  authoritative = false
  creates preview scoped to branches/<name>
  TTL = 14d default

dirty workspace:
  local-only by default
  remote sync requires explicit --sync-dirty-preview AND
  intent.yaml.catalog.sourceOfTruth.allowDirtySync = true
  TTL = 24h default
```

## 6. SaaS query patterns

Component canonical page:

```text
1. Read catalog_refs WHERE name = 'main'
2. Load catalog_snapshots row by catalog_snapshot_key
3. Load component_manifests row by (catalog_snapshot_key, component_key)
4. Load latest 50 component_execution_links rows ordered by created_at
5. Render canonical state, ownership, dependency graph, history
```

Component PR preview:

```text
1. Read catalog_refs WHERE name = 'pr-139'
2. Load PR component manifest
3. Load main component manifest
4. Compute diff from manifest_hash + spec fields
5. Render "what changes if this PR merges" view
```

The local model already supports both queries via the catalog-local indexes
plus the global component index — the remote driver just translates path
reads into SQL.

## 7. Auth and tenancy boundaries

Phase 3 will add:

- `internal/catalogsync/orun_cloud` — HTTP client implementation of `Syncer`.
- Auth via existing Orun Cloud credential flow.
- Tenancy enforcement at the `org/project/repo` prefix.
- Conflict resolution: server runs the same idempotent
  `CreateIfAbsent`/`CompareAndSwap` semantics; clients retry on `409`.

Phase 2 must not encode tenancy assumptions into the local layout. The
`org/project/repo` prefix is added by the remote driver; locally there is
exactly one workspace per `.orun/`.

## 8. Constraints on Phase 2 implementations

To keep the seam honest:

- `internal/catalogsync` must compile without any HTTP / network dependency.
- The package may not import `internal/runner` or `cmd/orun`.
- `SyncPayload` is built from `internal/catalogmodel` types only; no
  translation layer is allowed (a translation layer would mean the local
  model is the wrong shape).
- `--sync` exits 0 when the resolver succeeds even though
  `Syncer.PushCatalogSnapshot` returns `Accepted: false`. Exit 6 is
  reserved for future "sync failure" semantics.
