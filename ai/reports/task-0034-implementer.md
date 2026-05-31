# Milestone C4 PR-3 — internal/catalogstore: read-side Resolver + RebuildIndexes

Closes the C4 PR-3 surface in `specs/orun-component-catalog/catalog-store.md`:
the five read-side `Resolver` methods (T-STORE-2 cases) plus the
`RebuildIndexes` rebuild routine (T-STORE-3, §8).

Refs: `ai/tasks/task-0034.md`
Branch: `task-0034-catalogstore-c4-pr3-resolver`
Spec: `specs/orun-component-catalog/catalog-store.md` §6 (resolver) and §8 (rebuild)
Plan: `specs/orun-component-catalog/implementation-plan.md` (C4 PR-3)
Builds on: PR #174 (task 0032/0033, C4 PR-2)

## Surface delivered (PR-3)

### `resolver.go` — read-side Resolver (T-STORE-2)

Replaces the five `ErrNotImplemented` stubs with real implementations:

  - `ResolveCurrentSource(ctx) (*SourceSnapshot, error)`
    Reads `refs/sources/current.json` → loads the resolved source body
    from `sources/<key>/source.json`. Surfaces typed not-found via the
    `statestore.ErrNotFound` chain.

  - `ResolveSource(ctx, sel RefSelector) (*SourceSnapshot, error)`
    Resolves any of the five `RefSelector` shapes (Current, Main,
    Branch, PR, Latest, ExplicitKey) under the source side.

  - `ResolveCatalog(ctx, sel RefSelector) (*CatalogSnapshot, error)`
    Same five-shape resolver under the catalog side; reads from
    `refs/catalogs/*` then loads `sources/<src>/catalogs/<cat>/catalog.json`.

  - `ResolveComponent(ctx, sel RefSelector, name string) (*ComponentManifest, error)`
    Resolves the catalog ref first, then loads
    `sources/<src>/catalogs/<cat>/components/<name>/manifest.json`.

  - `ResolveComponentLatest(ctx, key ComponentKey) (*ComponentLatest, error)`
    Reads `indexes/components/<sanitized>.json` and returns the
    `Latest`/`Main`/`Previews` triple.

All resolvers honour the spec §6 selector precedence and never fabricate
data: an empty store returns a typed not-found through
`errors.Is(err, statestore.ErrNotFound)`.

### `rebuild.go` — §8 RebuildIndexes (T-STORE-3)

Walks the authoritative tree under `sources/*` via `StateStore.List`,
decodes each `source.json` / `catalog.json` / `manifest.json`, derives
single-source `ComponentGlobalIndex` shards via
`buildComponentGlobalIndexShard`, merges shards across sources using the
existing `mergeComponentGlobalIndex` semantics from `indexes.go`, then
writes the rebuilt indexes byte-identical to the Writer's output:

  - `indexes/sources/<source>.json`     ← `*Source` body
  - `indexes/catalogs/<catalog>.json`   ← `*Catalog` body
  - `indexes/components/<sanitized>.json` ← merged CGI per `ComponentKey`

Determinism is enforced by sorting source/catalog/component keys
ascending before write, reusing `catalogmodel.PrettyEncode` (the same
serializer the Writer uses), and reusing `indexesRetryBudget = 16` for
the component CAS loop.

Source-scope routing (matches Writer policy in `indexes.go`):
  - branch-main / branch-protected → `Main` pointer (with manifestPath)
    plus `Latest` pointer (no manifestPath, per data-model §9.1).
  - everything else (branch-feature, pr, tag, local-*, ci-event) →
    `Latest` pointer plus a `Previews` entry tagged with the SourceScope.

Empty tree behaviour: `RebuildIndexes` on an empty store is a no-op
(returns nil, writes nothing) — "nothing to walk" is success.

### Resolver interface change (`store.go`)

`Resolver` gains `RebuildIndexes(ctx) error`. The compile-time assertion
`var _ Resolver = (*store)(nil)` (already present) covers method-set
conformance; `TestPR3ResolversImplemented` now also exercises
`RebuildIndexes` to assert it is no longer `ErrNotImplemented`.

## Tests

### `resolver_test.go` (existing on branch from PR-3 partial)

Covers all five resolver methods × five selector shapes × happy/missing
paths. Asserts typed not-found via `errors.Is(err, statestore.ErrNotFound)`.

### `rebuild_test.go` (new this PR)

Six T-STORE-3 cases:

  1. `TestRebuildIndexes_ByteIdenticalAfterScrub` — seeds source +
     catalog + two manifests, writes global indexes the Writer way,
     captures every `indexes/*.json` body, scrubs them, calls
     `RebuildIndexes`, asserts each rebuilt body equals the original
     byte-for-byte. This is the load-bearing T-STORE-3 assertion.
  2. `TestRebuildIndexes_IdempotentSecondRebuild` — calling rebuild
     twice produces byte-identical bodies on the second pass.
  3. `TestRebuildIndexes_NoSources` — empty store rebuild is a no-op.
  4. `TestRebuildIndexes_MultiSourceUnionsPreviews` — branch-main +
     PR sources for the same `ComponentKey`: rebuild yields one CGI
     with the main pointer from the main source, the latest pointer
     from the freshest source (PR by `CreatedAt`), and the PR shard
     unioned into `Previews`.
  5. `TestRebuildIndexes_SourceWriteErrorSurfaces` /
  6. `TestRebuildIndexes_CatalogWriteErrorSurfaces` /
     `TestRebuildIndexes_ComponentWriteErrorSurfaces` — Write failure on
     any of the three index kinds is surfaced wrapped (errors.Is true
     against the injected sentinel).
  7. `TestRebuildIndexes_SkipsCorruptManifest` — a sibling manifest with
     malformed JSON does not poison the rebuild; well-formed manifests
     still produce CGIs.

### `store_test.go` extension

`TestPR3ResolversImplemented` extended with two new assertions:
  - `RebuildIndexes` on an empty store returns nil (not ErrNotImplemented).
  - `RebuildIndexes` is not in the `ErrNotImplemented` chain.

## Validation

  - `go build ./...` — exit 0
  - `go test ./internal/catalogstore/... -race -count=1` — PASS (1.741s)
  - `go test ./internal/catalogstore/... -cover` — coverage 88.9%
  - `go test ./...` — full suite PASS
  - LSP clean across all changed files

## No secrets

  - [x] No credentials introduced
  - [x] No log lines emit user/token/key material
  - [x] Test fixtures use literal `src-branch-main-cabcdef-tabcdef0` /
        `cat-deadbeef` keys (no real-world identifiers)

## Open questions for verifier

  1. The byte-identity test (`TestRebuildIndexes_ByteIdenticalAfterScrub`)
     uses a test-side `makeRebuildCGI` helper to seed Writer-shaped
     CGIs. The helper must stay in lock-step with rebuild.go's
     `buildComponentGlobalIndexShard` — verifier may want to consider
     whether to expose the production helper for tests instead. Current
     duplication is intentional (test-side mirror keeps the byte-equality
     assertion honest).

  2. `RebuildIndexes` does not delete stale index files (a previous
     index for a component no longer present in any source's tree
     stays orphaned on disk). The spec §8 contract is "rebuild from
     authoritative tree", which the implementation satisfies, but the
     verifier may want to confirm whether C8 (`orun catalog validate
     --rebuild-indexes`) needs a separate scrub pass.

  3. Corrupt-manifest handling skips silently. Should this surface a
     warning via the optional logger (when one lands in C5) or remain
     fully silent? Current behaviour matches the spec's "trust on-disk
     truth" framing.

## Next steps for verifier

  1. Spec read: `specs/orun-component-catalog/catalog-store.md` §6 and §8.
  2. Code read: `internal/catalogstore/{resolver,rebuild}.go` plus the
     new tests.
  3. Smoke: `go test ./internal/catalogstore/... -race -count=1` should
     pass clean.
  4. Coverage: 88.9% statement coverage on `internal/catalogstore`.
  5. Confirm PR-3 closes the C4 read-side surface; PR-4 (HEAD detection
     plus orchestration glue) is the next milestone slice.

## References

  - `specs/orun-component-catalog/catalog-store.md` — §6 resolver, §8 rebuild
  - `specs/orun-component-catalog/implementation-plan.md` — C4 PR-3 row
  - `specs/orun-component-catalog/data-model.md` §9.1 — global index shape
  - `internal/catalogstore/indexes.go` — merge semantics reused
  - `internal/catalogstore/writer.go` — PrettyEncode + step ordering reused
  - PR #174 — predecessor C4 PR-2

Closes Task 0034.
