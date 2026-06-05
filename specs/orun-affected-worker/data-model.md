# Data Model

> What the worker consumes (the impact index, by content id) and what it returns
> (`AffectedResult`). The worker stores no canonical data of its own — only
> immutable index objects fetched from R2 and a mutable pointer in KV mirrored
> from `orun`'s refs.

## 1. Consumed: the impact index

Fetched by content id from remote object storage (R2), exactly as `orun` wrote
it (`orun-catalog-state/data-model.md` §2). The worker reads two artifacts:

- **`impact/ownership.json`** — the ownership map + classification rules
  (`ImpactOwnership`, `schemaVersion`-gated). This is the worker's primary input.
- **`graph/dependencies.json`** — the component→component dependency edges
  (`CatalogGraph`, `edgeKind: "dependencies"`), for reverse closure. Already
  published by the resolver; the worker does **not** require a separate edge
  artifact for Tier 0.

The worker also reads `catalog.json` (the `CatalogSnapshot` header) for
`componentCount` / the full component list (needed when `global` → all
components) and `catalogId` provenance.

```ts
interface LoadedIndex {
  id: string;            // ownership.json blob id (provenance + cache key)
  catalogId: string;     // CatalogSnapshot id
  schemaVersion: number; // rejected if unknown
  ownership: ImpactOwnership;          // components map + classification rule data
  components: ComponentKey[];          // full set (from catalog.json)
  edges: DependencyEdge[];             // from graph/dependencies.json
  coversBase: boolean;                 // §4 base selection — set by the worker, not stored
}
```

The worker **MUST** reject a `LoadedIndex` whose `schemaVersion` it does not
support — escalate to `needsFullResolve`, never best-effort-parse (drift guard,
design §7).

## 2. Mutable pointer (KV)

The only mutable state the worker reads. Mirrored from `orun`'s ref publish
(`catalogs/current`, `catalogs/main`, `catalogs/branches/<b>`):

```
KV key:   affected:<repo>:<scope>            e.g. affected:sourceplane/orun:main
KV value: { impactIndexId, catalogId, sourceHead, updatedAt }
```

`sourceHead` (the catalog's source `HeadRevision`) is what the worker compares an
event's base against for `coversBase` (design §4). Everything the pointer
references is immutable and content-addressed → safe to cache forever at the
edge.

## 3. Webhook → changed files (`extractChange`)

From the GitHub event payload — **no checkout**:

- **push**: `base = before`, `head = after`, `changedFiles` = union of
  `commits[].{added,removed,modified}` (or the compare API if truncated).
- **pull_request**: `base = pull_request.base.sha`, `head =
  pull_request.head.sha`, `changedFiles` from the PR files API (paginated).
- Truncation/edge cases (force-push, huge diffs) → if the changed-file set is
  unavailable or truncated, the worker **MUST** escalate (`needsFullResolve`),
  never silently classify a partial set (C-1).

```ts
interface ChangeSet {
  repo: string;
  base: string;          // sha
  head: string;          // sha
  changedFiles: string[];// workspace-relative, slash-separated
  truncated: boolean;    // true → escalate
}
```

## 4. Returned: `AffectedResult` (the response contract)

Byte-compatible with `orun catalog affected`'s `CatalogAffectedResult.data`
(`orun-catalog-state/cli-surface.md` §3) — same field names/semantics, so the two
consumers are interchangeable and conformance is a direct equality:

```json
{
  "affected": ["sourceplane/orun/api-edge", "sourceplane/orun/web"],
  "directlyChanged": ["sourceplane/orun/web"],
  "confidence": "high",
  "needsFullResolve": false,
  "impactIndexId": "sha256:…",
  "catalogId": "sha256:…",
  "base": "abc123",
  "head": "def456",
  "coversBase": true
}
```

- `confidence` ∈ `{"high","low"}`. `low` ⇔ a `structural` change was seen.
- `needsFullResolve` = `structural` ∨ `global-uncertain` ∨ `!coversBase` ∨
  `truncated` ∨ `unknown schemaVersion`. **This is the C-3 gate signal.**
- `affected` is **always a superset** of the truly-affected set (C-1 / invariant
  1). The worker never returns a tighter set than truth.
- `base`/`head`/`coversBase` are added (over the CLI shape) for the event
  context; consumers that share the CLI shape ignore them.

## 5. Validation / safety rules

- An empty `changedFiles` (no-op push) → `affected: []`, `confidence: "high"`.
- A `global` classification with an **unparseable** `intent.yaml` block →
  treat as `global` over **all** components (C-1 over-report), not `ignore`.
- The reverse closure **MUST** be cycle-safe (the resolver forbids cycles, but the
  worker walks defensively with a visited set).
- All output arrays sorted lexically for determinism (so identical events on an
  identical index produce byte-identical responses → cacheable).
