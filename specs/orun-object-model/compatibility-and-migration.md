# Compatibility & Migration

> This phase **replaces** the Phase 1/2 on-disk layouts rather than preserving
> them, but it does so **additively and reversibly**: nothing is deleted from a
> user's existing `.orun/` until they opt in, and the rewrite ships behind a
> flag until proven.

## 1. Stance

- The Phase 1 global layout (`.orun/revisions/…`, `refs/…`, `indexes/…`) and the
  Phase 2 catalog-parent mirror (`.orun/sources/…/catalogs/…`) are **superseded**
  by the object graph. They are not written by the new code path.
- During the staged rollout (M0–M11) the legacy writers remain available behind
  `ORUN_OBJECT_RUNNER` off, so a user on the old default sees no change.
- At M12 the new model becomes the default and the legacy writers are deleted.
  Existing `.orun/` trees are migrated by `orun migrate`, never silently
  rewritten.

## 2. `orun migrate` — one-shot legacy ingest

```
orun migrate [--dry-run] [--from <path>] [--prune-legacy]
```

Ingests an existing legacy `.orun/` into the object graph. **Idempotent and
additive.**

Algorithm:

```
for each legacy plan  .orun/plans/<checksum>.json:
    plan blob → PutBlob; synthesize a PlanRevision (catalogId omitted or
    "migrated" sentinel); revisionId = treeId; record migration trigger
    (system.migrated) pointing at it.
for each legacy execution .orun/executions/<id>/:
    read state.json + metadata.json; reconstruct ExecutionRun + JobRun/Attempt/
    Step (best-effort from the legacy ExecState); seal as content; logs → blobs.
    Link to its revision by legacy planChecksum match; unmatched → a
    "rev-migrated-unknown" revision bucket.
move refs: refs/revisions/latest, refs/executions/latest to the newest ingested.
write version.json (objectModelVersion=1).
```

- `--dry-run` prints the plan (counts of plans/executions/objects that would be
  written, matched vs orphaned) and writes nothing. Two consecutive dry-runs
  produce identical output.
- Running `migrate` twice ingests the same content to the same ids (content
  addressing ⇒ idempotent); the second run is a near no-op (all `Has` hits).
- `--prune-legacy` (explicit opt-in only) removes the old `.orun/plans` and
  `.orun/executions` **after** a successful, verified ingest (`orun fsck` green).
  Default: leave legacy trees in place.

## 3. What is intentionally NOT preserved

- The flat `.orun/plans/<checksum>.json` write path (replaced by revisions +
  `refs/named`). Reads during migration only.
- The `.orun/executions/<id>/` mutable layout (replaced by working tree + seal).
- The catalog-parent byte-copy mirror (replaced by Merkle references).
- `internal/state` types as a public contract — callers move to `nodes`
  accessors (parity matrix, `runner-integration.md` §4).

## 4. Rollback

- While staged: `ORUN_OBJECT_RUNNER=0` returns to the legacy path; the object
  graph written so far is inert (and GC-able) and does not affect legacy reads.
- Post-M12: rollback is "checkout an earlier orun binary"; the object graph is
  forward-only, but `orun migrate` re-ingesting a restored legacy tree is always
  possible (idempotent).

## 5. Version gating

- `version.json.objectModelVersion` gates future on-disk format changes. A newer
  orun reading an older store runs an in-place, additive upgrade or refuses with
  a clear message; it never silently rewrites object bytes (that would change
  ids).
- `resolverVersion` bumps invalidate the resolve memo cache only (derived); no
  object rewrite.
