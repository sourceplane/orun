# Data Model

> Every persisted node schema. All records are canonical JSON blobs (see
> `object-store.md` §3). `id` fields are `"<algo>:<hex>"`. Optional fields are
> omitted when empty. Times are RFC 3339 / Z. Fields that would create a
> self-reference (a node embedding its own id) are **forbidden** and noted.

## Conventions

- `kind` discriminates the schema. Required on every record.
- `*Id` fields are **edges** — object ids of other nodes.
- `humanKey` is a non-identifying, human-readable label preserved from the
  Phase 1/2 key formats (for porcelain + working view). Never part of the hash
  input for *its own* node, but it is part of the bytes (so it must be
  deterministic).

---

## 1. SourceSnapshot — `source.json`

Edge: none (root). Identity: input-addressed (`identity-and-keys.md` §3).

```json
{
  "kind": "SourceSnapshot",
  "humanKey": "src-branch-feature-x-cabc1234-t9aa7710-d91aa77b2",
  "scope": "branch",
  "repo": "sourceplane/orun",
  "headRevision": "abc1234def56",
  "treeHash": "9aa7710…",
  "branch": "feature/x",
  "pr": "",
  "workingTree": "dirty",
  "dirtyHash": "sha256:91aa77b2…",
  "resolvedAt": "2026-06-02T10:04:11Z"
}
```

- `scope ∈ {"main","branch","pr","local-nogit"}`.
- `local-nogit`: `headRevision`/`treeHash`/`branch` omitted; `dirtyHash` present
  (or a sentinel `"empty"` for an empty directory).
- `resolvedAt` is informational; it is part of the bytes, so a re-resolve of the
  same git state at a different time produces a *different* source object.
  **Decision:** to maximize source dedup, `resolvedAt` is **excluded from the
  source record** and recorded instead on the ref/event that observed it. (See
  risks doc R-1; default = exclude.)

## 2. CatalogSnapshot — `catalog.json`

Edge: `sourceId`. Identity: Merkle root of the catalog tree
(`identity-and-keys.md` §4). MUST NOT embed `catalogId`.

```json
{
  "kind": "CatalogSnapshot",
  "humanKey": "cat-branch-feature-x-…",
  "sourceId": "sha256:aaa…",
  "resolverVersion": 1,
  "componentCount": 7,
  "components": [
    { "componentKey": "sourceplane/orun/api-edge", "name": "api-edge",
      "manifestId": "sha256:111…" }
  ],
  "graphIds": {
    "dependencies": "sha256:201…", "systems": "sha256:202…",
    "apis": "sha256:203…", "resources": "sha256:204…", "owners": "sha256:205…"
  },
  "issues": [ { "severity": "warning", "component": "…", "message": "…" } ]
}
```

- `components[]` is the manifest index (sorted by `componentKey`); the actual
  manifest blobs live in the `components/` subtree.
- `issues[]` are non-fatal validation findings (the tolerant part of
  tolerant-strict). Under `--strict`, a non-empty error-severity set fails the
  walk.

## 3. ComponentManifest — `components/<name>.json` (blob)

Edge: implicit (lives under a catalog tree). Identity: blob hash. Same shape as
Phase 2 `ComponentManifest` (carried over unchanged), abbreviated here:

```json
{
  "kind": "ComponentManifest",
  "identity": { "componentKey": "sourceplane/orun/api-edge",
                "name": "api-edge", "namespace": "sourceplane", "repo": "orun" },
  "type": "cloudflare-worker",
  "metadata": { "labels": {…}, "annotations": {…} },
  "spec": { "owners": […], "dependencies": […], "runtime": {…} },
  "provenance": { "inheritedFrom": {…}, "inferredFrom": {…} }
}
```

- `componentKey` regex `^[a-z0-9._-]+/[a-z0-9._-]+/[a-z0-9._-]+$`. Environment is
  never part of identity.
- The Phase 2 per-component `manifestHash` is now simply the blob id.

## 4. CatalogGraph — `graph/<edgeKind>.json` (blobs)

Edge: implicit. Identity: blob hash per file. Nodes+edges as Phase 2:

```json
{ "kind": "CatalogGraph", "edgeKind": "dependencies",
  "nodes": [ { "key": "sourceplane/orun/api-edge", "kind": "Component", "name": "api-edge" } ],
  "edges": [ { "from": "…", "to": "…", "type": "calls" } ] }
```

MUST NOT embed `catalogId`.

## 5. PlanRevision — `revision.json`

Edge: `catalogId`. Identity: Merkle root of `{revision.json, plan.json}`. MUST
NOT embed the trigger, `executionId`, or a timestamp (see
`identity-and-keys.md` §6).

```json
{
  "kind": "PlanRevision",
  "humanKey": "rev-branch-abc1234-p8f31c09",
  "catalogId": "sha256:bbb…",
  "sourceId": "sha256:aaa…",
  "planHash": "sha256:8f31c09…",
  "scope": { "mode": "full", "components": [], "matchedTriggers": [] },
  "jobCount": 4,
  "legacyChecksum": "sha256-8f31c09…"
}
```

- `plan.json` is the canonical compiled plan blob; `planHash = blobId(plan.json)`.
- `legacyChecksum` preserves the old `sha256-<hex>` form for tooling/migration
  reads. It is deterministic from the plan, so it does not break dedup.
- `scope` is the planning scope (full vs changed-subset), carried for
  reproducibility; it is part of the plan inputs, hence part of identity.

## 6. TriggerOccurrence — `trigger.json` (blob)

Edge: `revisionId`. Identity: blob hash (unique via embedded ULID). This is an
**event**.

```json
{
  "kind": "TriggerOccurrence",
  "triggerId": "trg_01H8X…",
  "triggerName": "system.manual",
  "triggerKey": "system.manual:full",
  "revisionId": "sha256:ccc…",
  "source": { "flavor": "system", "system": "manual" },
  "scope": { "mode": "full" },
  "createdAt": "2026-06-02T10:04:11Z",
  "actor": "cli",
  "providerEvent": null
}
```

- `triggerName`/`source`/`providerEvent` carry the declared (CI) vs system
  (manual/changed/replay/api) distinction unchanged from `triggerctx`.
- `actor ∈ {"cli","runner","tui","saas","ci"}`.

## 7. ExecutionRun — `execution.json`

Edges: `revisionId`, `triggerId`. **Two states.**

**Live (working tree, mutable):** stored under the working area, not yet a
sealed object. The on-disk shape mirrors the sealed tree but files are rewritten
in place as the run progresses.

**Sealed (immutable object):** identity = Merkle root of the execution tree.

```json
{
  "kind": "ExecutionRun",
  "executionId": "exec_01H8Y…",
  "executionKey": "run-001",
  "revisionId": "sha256:ccc…",
  "triggerId": "trg_01H8X…",
  "status": "succeeded",
  "startedAt": "2026-06-02T10:04:12Z",
  "finishedAt": "2026-06-02T10:06:30Z",
  "dryRun": false,
  "runnerProfile": { "concurrency": 4, "failFast": true },
  "summary": { "jobsTotal": 4, "jobsSucceeded": 4, "jobsFailed": 0, "stepsTotal": 18 },
  "links": [ { "label": "…", "url": "…", "jobId": "…", "stepId": "…" } ],
  "jobIds": { "j-a8f31c09": "sha256:301…" }
}
```

- `status ∈ {"pending","running","succeeded","failed","cancelled"}`. Only
  terminal statuses are sealed; a live execution carries the same record in the
  working tree with mutating `status`/`summary`.
- `jobIds` maps the job folder name (`j-<shortHash(jobID)>`) to the sealed
  JobRun tree id.
- `executionKey`/`executionId` formats preserved (`run-NNN`,
  `gh-{run_id}-{attempt}-{sha}`).

### 7.1 JobRun — `jobs/<j-…>/job-run.json`

Edge: implicit (under the execution tree). Identity: Merkle root of the JobRun
tree `{job-run.json, attempts/}`.

```json
{ "kind": "JobRun", "jobId": "api-edge@deploy", "folder": "j-a8f31c09",
  "status": "succeeded", "attemptIds": { "1": "sha256:401…" },
  "startedAt": "…", "finishedAt": "…", "lastError": "" }
```

`jobId` is the original (may contain `@`,`.`,`/`); `folder` is the sanitized
name. The original is preserved here so the folder name can stay short/safe.

### 7.2 JobAttempt — `jobs/<j-…>/attempts/<n>/attempt.json`

Identity: Merkle root of `{attempt.json, steps/}`.

```json
{ "kind": "JobAttempt", "attempt": 1, "status": "succeeded",
  "startedAt": "…", "finishedAt": "…", "stepIds": { "s-build": "sha256:501…" } }
```

### 7.3 StepAttempt — `jobs/<j-…>/attempts/<n>/steps/s-<id>.json` (blob)

```json
{ "kind": "StepAttempt", "stepId": "build", "status": "succeeded",
  "startedAt": "…", "finishedAt": "…", "exitCode": 0,
  "logId": "sha256:601…", "heartbeatAt": "…" }
```

`logId` points at a log blob (or, for large logs, a `tree` of chunk blobs). Logs
are content-addressed too — identical log output dedups.

### 7.4 Execution events — `events/<seq>-<kind>.json` (blobs)

Append-only per-execution event records (`execution-created`,
`job-started`, `step-finished`, `execution-sealed`, …). `seq` is a zero-padded
20-digit counter. These are sealed into the execution tree.

## 8. ComponentHistory — derived event stream

Not a primary node. Component history is **derived** by indexing trigger +
execution events by `componentKey`. Materialized into
`index/components/<componentKey>.json`:

```json
{ "kind": "ComponentHistoryIndex", "componentKey": "sourceplane/orun/api-edge",
  "events": [ { "type": "execution.completed", "executionId": "exec_…",
                "revisionId": "sha256:ccc…", "at": "…", "status": "succeeded" } ],
  "latest": { "revisionId": "sha256:ccc…", "executionId": "exec_…", "status": "succeeded" } }
```

Rebuildable from the graph; never authoritative.

## 9. Ref record — `refs/<name>.json`

See `object-store.md` §6. `{ "kind":"Ref", "target":"<algo>:<hex>", "updatedAt":"…", "writer":"…" }`.

## 10. Store version — `version.json`

A non-object, store-level metadata file at the store root (mutable, single):

```json
{ "kind": "StoreVersion", "objectModelVersion": 1, "hashAlgo": "sha256",
  "resolverVersion": 1, "createdAt": "…" }
```

Bumping `objectModelVersion` gates migrations; `hashAlgo` records the active
algorithm for the store.
