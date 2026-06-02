# Identity, Keys & Addressing

> How every node gets its id, how reuse/dedup is decided, the resolve memo
> cache, and how refs are named. RFC 2119 keywords are normative.

## 1. Two addressing regimes

| Regime | Applies to | Id is derived from | Dedups? |
|--------|------------|--------------------|---------|
| **Content-addressed** | SourceSnapshot, CatalogSnapshot, ComponentManifest, PlanRevision, sealed ExecutionRun, JobRun/Attempt/Step, blobs, trees | hash of canonical bytes / Merkle root | **Yes** |
| **Event-addressed** | TriggerOccurrence, ExecutionRun *handle* while live | a ULID (`trg_`, `exec_`) + timestamp inside the record | No (unique by design) |

Content ids are the spine; event ids are the leaves. A trigger record is itself
stored as a content object (it is immutable once created), but because it
embeds a unique ULID it never collides — that is intentional.

## 2. Hash algorithm

- **`sha256`** in v1. Wire form `"sha256:<lowerhex>"`. Pluggable via an `algo`
  parameter on store construction; objects are path-namespaced by algo
  (`objects/sha256/…`).
- Rationale: boring, proven, already emitted as `sha256-…` plan checksums today,
  zero new crypto risk. `blake3` is a future opt-in for speed.

## 3. SourceSnapshot identity (input-addressed)

Reuses the existing `sourcectx` rules verbatim. The source id is **derived from
git/worktree state**, not minted:

```
sourceContentHash = H( canonical{
    scope:        "main" | "branch" | "pr" | "local-nogit",
    headRevision: <12-char short SHA> | "",
    treeHash:     <git tree hash> | "",
    dirtyHash:    <sha256 over catalog-relevant dirty files> | "",
    repo:         "<owner>/<repo>" | "",
} )
sourceId = "sha256:" + sourceContentHash
```

- `dirtyHash` is computed over **catalog-relevant files only** (existing rule),
  so ordinary code edits do not churn the source id.
- **Degenerate sources are valid:** `local-nogit` populates only `dirtyHash`
  (or a zero marker for an empty dir). A non-git workspace always yields a
  well-formed source node — the tolerant-strict walk never dead-ends.
- The human key `src-<scope>[-<branch|pr>]-c<headShort>-t<treeShort>[-d<dirtyShort>]`
  is preserved **as a ref alias and a `humanKey` field**, not as storage.

## 4. CatalogSnapshot identity (Merkle, with input memo)

The catalog id is the **Merkle root of the catalog tree**:

```
catalogTree = tree{
    "catalog.json": <CatalogSnapshot record blob>,
    "components":   tree{ "<componentName>.json": <ComponentManifest blob>, … },
    "graph":        tree{ "dependencies.json", "systems.json", "apis.json",
                          "resources.json", "owners.json" },
}
catalogId = treeId(catalogTree)
```

- This is **pure content addressing**: two catalogs with identical components +
  graph share an id, regardless of how they were resolved.
- The legacy `catalogHash` (over inputs + manifest hashes) is **retained only as
  the memo-cache key** (§7), not as identity. This resolves the input-hash /
  content-hash duality cleanly (Nix CA-derivation style).
- `catalog.json` MUST NOT embed `catalogId` (self-reference); the id is computed
  by the store. `catalog.json` MAY embed `sourceId` (an edge, fine).

## 5. ComponentManifest identity

A `ComponentManifest` is a content blob; its id is `H(canonical(manifest))`. The
`componentKey` (`<namespace>/<repo>/<componentName>`, environment never part of
identity) is preserved as a record field and as the index key — not as the
object id. Identical manifests across catalogs are stored once.

## 6. PlanRevision identity (the dedup-across-triggers rule)

```
revisionTree = tree{
    "revision.json": <PlanRevision record blob, carrying catalogId, planHash, scope>,
    "plan.json":     <compiled plan blob>,
}
revisionId = treeId(revisionTree)
```

- `revision.json` MUST carry `catalogId` and the `planHash` (sha256 of canonical
  `plan.json`) but MUST NOT carry the trigger or any timestamp — otherwise two
  triggers could not produce the same revision id.
- **Dedup rule:** before writing, compute `revisionId`; if `Has(revisionId)`,
  reuse. Therefore two triggers (e.g. a manual run and a PR check) that compile
  byte-identical plans against the same catalog **share one revision object**.
  The trigger→revision edge is many-to-one.
- The human key `rev-<scope>-<headShort>-p<planHashShort>` is preserved as a
  `humanKey` field + `refs/named/<name>` aliases. Collision suffixes (`-xN`) are
  **no longer needed** — content identity is collision-free by construction.

## 7. Resolve memoization (Nix input-addressing)

The catalog resolve (the 14-stage pipeline) is expensive. Memoize it:

```
cache/resolve/<sourceId-hex>-rv<resolverVersion>.json  →  { "catalogId": "sha256:…" }
```

- On `ResolveCatalog`: look up the memo. **Hit** ⇒ skip the pipeline, reuse the
  catalog id (still verify `Has(catalogId)`; rebuild on miss). **Miss** ⇒ run
  the pipeline, write the catalog tree, write the memo.
- `resolverVersion` is an integer bumped whenever resolver logic changes; it
  invalidates memos without deleting anything.
- The cache is **derived** (`.orun/cache/`): deletable, rebuildable. It is never
  authoritative — a stale/absent memo only costs a recompute, never correctness
  (the recomputed catalog either matches an existing id — early cutoff — or is a
  new content node).

## 8. Ref naming

```
refs/sources/current                 # the working source (this checkout)
refs/sources/main                    # canonical main-branch source
refs/sources/branches/<branchSeg>    # branchSeg = sanitized branch name
refs/sources/prs/<pr>
refs/catalogs/current
refs/catalogs/main
refs/catalogs/branches/<branchSeg>
refs/catalogs/prs/<pr>
refs/revisions/latest
refs/named/<nameSeg>                 # user-named plan aliases (orun plan --name X)
refs/triggers/<triggerNameSeg>/latest
refs/executions/latest               # last sealed (or live) execution
refs/executions/live/<execId>        # mutable handle for an in-flight execution
```

- Each path segment matches `^[A-Za-z0-9._-]+$`. Branch names with `/` are
  sanitized (replace `/`→`-`, fold to alphabet) — the original lives in the
  record JSON.
- Ref targets are always full `"<algo>:<hex>"` ids.

## 9. Event ids

- `TriggerOccurrence.triggerId = "trg_" + ULID`, monotonic, sortable. Embedded
  in `trigger.json`. The object id of the trigger blob is content-derived but
  unique (because the ULID is inside).
- `ExecutionRun.executionId = "exec_" + ULID` for system runs, or the preserved
  `gh-{run_id}-{attempt}-{sha}` form for CI runs (kept verbatim for
  `runbundle` compatibility). The `executionKey` (`run-NNN`) remains the
  human/working-view folder name; the object identity of a sealed execution is
  its Merkle root.

## 10. Worked example (dedup in action)

```
# Manual plan on a clean main checkout:
sourceId   = sha256:aaa…           (refs/sources/current → aaa, refs/sources/main → aaa)
catalogId  = sha256:bbb…           (memo cache/resolve/aaa-rv1 → bbb; refs/catalogs/main → bbb)
revisionId = sha256:ccc…           (refs/revisions/latest → ccc)
trigger1   = trg_01H… (object ddd) (refs/triggers/system.manual/latest → ddd; ddd.revisionId=ccc)

# A PR check later compiles the SAME plan against the SAME catalog:
sourceId   = sha256:aaa…  (Has → reuse, no write)
catalogId  = sha256:bbb…  (memo hit, no resolve, no write)
revisionId = sha256:ccc…  (Has → reuse, no write)   ← ONE revision, shared
trigger2   = trg_01J… (object eee, eee.revisionId=ccc)  ← only the event is new

# Disk delta for the PR check: one ~300-byte trigger blob + ref moves. That's it.
```
