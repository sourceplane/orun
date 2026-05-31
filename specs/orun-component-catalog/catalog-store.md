# Catalog Store

`internal/catalogstore` is the persistence boundary for catalog objects.
Every byte written to `.orun/sources/...` flows through this package; raw
path concatenation is forbidden. The store is itself a thin layer over
`internal/statestore` (Phase 1) — it adds path conventions, write ordering,
typed refs, and reader fallback rules.

---

## 1. Public surface

```go
package catalogstore

type Writer interface {
    WriteSourceSnapshot(ctx context.Context, src catalogmodel.SourceSnapshot) error
    WriteCatalogSnapshot(ctx context.Context, src catalogmodel.SourceSnapshot,
                         cat catalogmodel.CatalogSnapshot,
                         manifests []catalogmodel.ComponentManifest,
                         graphs catalogmodel.CatalogGraphs,
                         localIndexes catalogmodel.CatalogLocalIndexes) error
    WriteRefs(ctx context.Context, refs catalogmodel.RefUpdate) error
    WriteGlobalIndexes(ctx context.Context, updates catalogmodel.GlobalIndexUpdate) error
    AppendComponentEvent(ctx context.Context, ev catalogmodel.ComponentCatalogEvent) error
}

type Resolver interface {
    ResolveCurrentSource(ctx context.Context) (catalogmodel.SourceSnapshot, error)
    ResolveSource(ctx context.Context, selector RefSelector) (catalogmodel.SourceSnapshot, error)
    ResolveCatalog(ctx context.Context, selector RefSelector) (catalogmodel.CatalogSnapshot, error)
    ResolveComponent(ctx context.Context, sel RefSelector, name string) (catalogmodel.ComponentManifest, error)
    ResolveComponentLatest(ctx context.Context, key string) (catalogmodel.ComponentLatest, error)
}

type Store interface {
    Writer
    Resolver
}

func New(state statestore.StateStore) Store
```

`RefSelector` is `{ Kind: "current"|"main"|"latest"|"branch"|"pr",
Branch?, PR? string }`. Implementations may add `Snapshot` (explicit
`catalogSnapshotKey`) for the C8 diff command.

## 2. Path helpers

All paths produced by `internal/catalogstore/paths.go`. Forward slashes,
relative to the `.orun/` root the underlying `StateStore` is configured with.

```go
func SourceDir(srcKey string) string                          // sources/<srcKey>
func SourceDocPath(srcKey string) string                      // sources/<srcKey>/source.json

func CatalogDir(srcKey, catKey string) string                 // sources/<srcKey>/catalogs/<catKey>
func CatalogDocPath(srcKey, catKey string) string             // sources/<srcKey>/catalogs/<catKey>/catalog.json

func ComponentDir(srcKey, catKey, name string) string         // .../components/<name>
func ComponentManifestPath(srcKey, catKey, name string) string // .../components/<name>/manifest.json

func CatalogGraphPath(srcKey, catKey, kind string) string     // .../graph/<kind>.json (kind ∈ dependencies|systems|apis|resources|owners)

func CatalogRevisionDir(srcKey, catKey, revKey string) string         // .../revisions/<revKey>
func CatalogRevisionPlanPath(srcKey, catKey, revKey string) string    // .../revisions/<revKey>/plan.json
func CatalogExecutionDir(srcKey, catKey, revKey, execKey string) string

func SourceRefPath(name string) string                        // refs/sources/<name>.json (name ∈ latest|current|main)
func SourceBranchRefPath(branch string) string                // refs/sources/branches/<branch>.json
func SourcePRRefPath(pr string) string                        // refs/sources/prs/<pr>.json

func CatalogRefPath(name string) string
func CatalogBranchRefPath(branch string) string
func CatalogPRRefPath(pr string) string

func ComponentLocalIndexPath(srcKey, catKey, name string) string  // <catalog>/indexes/components/<name>.json
func OwnerLocalIndexPath(srcKey, catKey, owner string) string
func SystemLocalIndexPath(srcKey, catKey, system string) string
func DomainLocalIndexPath(srcKey, catKey, domain string) string
func TypeLocalIndexPath(srcKey, catKey, typ string) string

func ComponentGlobalIndexPath(componentKey string) string     // indexes/components/<sanitized>.json
func CatalogGlobalIndexPath(catKey string) string             // indexes/catalogs/<catKey>.json
func SourceGlobalIndexPath(srcKey string) string              // indexes/sources/<srcKey>.json

func ComponentHistoryEventPath(srcKey, catKey, name string,
                               seq uint64, kind string) string
// .../history/components/<name>/events/<seq:09d>-<sanitizedKind>.json
```

Every helper is total: invalid inputs return an error from a sibling
`Validate*` function before path construction. The helpers themselves never
panic.

## 3. Write order

Catalog writes are **not transactional**. The Phase 1 invariant
**body-before-ref** applies. The writer for `orun catalog refresh` runs:

```text
A. WriteSourceSnapshot
   - StateStore.CreateIfAbsent(SourceDocPath, body)
   - On ErrExists with byte-identical body: treat as success.
   - On ErrExists with different body: return ErrConflict.

B. WriteCatalogSnapshot
   B.1. For each ComponentManifest:
        StateStore.CreateIfAbsent(ComponentManifestPath, body)
   B.2. For each graph kind in {dependencies, systems, apis, resources, owners}:
        StateStore.CreateIfAbsent(CatalogGraphPath, body)
   B.3. StateStore.CreateIfAbsent(CatalogDocPath, body)
   B.4. For each catalog-local index:
        StateStore.Write(<localIndexPath>, body)            // overwrite is fine; rebuildable

C. WriteGlobalIndexes
   C.1. StateStore.Write(SourceGlobalIndexPath, body)
   C.2. StateStore.Write(CatalogGlobalIndexPath, body)
   C.3. For each component:
        CompareAndSwap(ComponentGlobalIndexPath, oldRev, newBody)
        - Loser retries with the latest body merged.

D. WriteRefs (each via CompareAndSwap)
   D.1. refs/sources/current.json
   D.2. refs/catalogs/current.json
   D.3. If authoritative: refs/{sources,catalogs}/main.json
   D.4. If branch: refs/{sources,catalogs}/branches/<branch>.json
   D.5. If PR:     refs/{sources,catalogs}/prs/<pr>.json
   D.6. refs/sources/latest.json, refs/catalogs/latest.json
```

Ordering rationale:

- Refs always point at content that already exists on disk (steps A, B before D).
- Global indexes update before refs so a reader following a fresh ref can
  always find the catalog via the index too.
- Local indexes are rebuildable, so plain `Write` is acceptable; global
  indexes coordinate concurrent writers via `CompareAndSwap`.

For `orun plan` (extends Phase 1's `internal/revision.WriteRevision`):

```text
1. EnsureSourceAndCatalog (idempotent: reuse existing source/catalog if
   catalogInputHash + catalogHash match; otherwise refresh).
2. internal/revision.WriteRevision under CatalogRevisionDir(srcKey, catKey, revKey).
3. Append component-execution-index entries (one per component touched by the plan).
4. AppendComponentEvent(plan.created) per component.
5. WriteRefs (revisions/latest unchanged; sources/catalogs current updated only
   if EnsureSourceAndCatalog actually refreshed).
6. If stateCompatibilityWrites is enabled, write the alias under
   .orun/revisions/<revKey>/plan.json (Phase 1 path).
```

For `orun run`:

```text
1. ResolveRevision via internal/revision (reads global revision index).
2. Load parent SourceSnapshot + CatalogSnapshot via Resolver.
3. internal/executionstate.Bridge.WriteExecution under
   CatalogExecutionDir(srcKey, catKey, revKey, execKey).
4. AppendComponentEvent(execution.started / .completed / .failed).
5. Update component-execution-index entry status.
```

## 4. Reader fallback

For every read, the resolver tries paths in the following order:

| Want | Try in order |
|------|--------------|
| Current source | `refs/sources/current.json` → fall back to most recent `sources/*/source.json` by `createdAt` |
| Current catalog | `refs/catalogs/current.json` → fall back to most recent `catalogs/*/catalog.json` under the resolved source |
| Component manifest | `indexes/components/<sanitized>.json.main.manifestPath` → walk `sources/*/catalogs/*/components/<name>/manifest.json` filtered by `sourceScope` |
| Plan revision | Phase 1 `indexes/revisions/<revKey>.json` → fall back to walking `sources/*/catalogs/*/revisions/<revKey>/` |
| Execution | Phase 1 `indexes/executions/<execKey>.json` → fall back to walking `sources/*/catalogs/*/revisions/*/executions/<execKey>/` |
| Legacy revision (Phase 1 layout) | `.orun/revisions/<revKey>/` after the catalog walk fails |

The fallback walk uses `StateStore.List` with a glob; results are filtered
in-memory. Performance budget: ≤ 50 ms for a `.orun/` tree with 1k
revisions on a warm SSD, asserted by `BenchmarkResolveCatalogCurrent` in
`test-plan.md` §7.

## 5. Atomicity guarantees

Inherited from Phase 1 `internal/statestore`:

- `Write` performs a temp-file + atomic rename.
- `CreateIfAbsent` is exclusive across goroutines (and across processes on
  the same host).
- `CompareAndSwap` on refs uses an `oldRev` token from a prior `Read`.

Catalog-store-specific:

- `AppendComponentEvent` allocates `<seq>` via `CreateIfAbsent` on a
  sentinel `seq.lock` file in the events directory; on collision, increment
  and retry up to 16 times.
- `WriteGlobalIndexes` for components retries `CompareAndSwap` on conflict
  by re-reading the index, merging the new entry, and re-encoding. Cap retry
  at 8 attempts; surface `ErrConflict` after that (caller can re-run).

## 6. Error taxonomy

```go
var (
    ErrSourceMismatch    = errors.New("source body conflict for same key")
    ErrCatalogMismatch   = errors.New("catalog body conflict for same key")
    ErrManifestMismatch  = errors.New("manifest body conflict for same key")
    ErrCatalogNotFound   = errors.New("no catalog for selector")
    ErrComponentNotFound = errors.New("no manifest for component in catalog")
    ErrRefStale          = errors.New("ref CompareAndSwap retries exhausted")
)
```

Errors wrap `internal/statestore` errors (`ErrNotFound`, `ErrExists`,
`ErrConflict`, `ErrInvalid`) with `errors.Is` semantics preserved.

## 7. Mirror / compatibility writes

When `stateCompatibilityWrites = true` (default for the duration of Phase 2):

- After step C in `orun plan`, write a compatibility alias at
  `.orun/revisions/<revKey>/plan.json` pointing to the new canonical body
  (full copy, not a symlink — symlinks are not portable across remote
  drivers).
- `executionstate.Bridge` continues to mirror execution state into
  `.orun/executions/<execKey>/` exactly as in Phase 1.

A future flag `stateCompatibilityWrites = false` will be enabled in Phase 3
once all callers migrate; the sunset is not in this spec's scope.

## 8. Index rebuild

`Resolver.RebuildIndexes(ctx)` reconstructs every index file from the
catalog tree. Used by `orun catalog validate --rebuild-indexes` (added in
C8). The rebuild is idempotent and produces byte-identical index files for
the same input tree (verified by T-STORE-3).

## 9. Concurrency model

- `Writer.WriteCatalogSnapshot` is process-safe but not designed for
  multi-host concurrency (Phase 3 problem).
- Two concurrent `orun catalog refresh` processes are tolerated; both run
  the resolver, both write the same `(srcKey, catKey)`, and `CreateIfAbsent`
  guarantees byte-identical bodies dedupe naturally. Refs may contend; the
  loser retries.
- `orun plan` and `orun catalog refresh` may overlap; `orun plan` re-uses
  whichever catalog wins the `current` ref race at its sample point. Plan
  metadata records the actual catalog key used so audit is unambiguous.
