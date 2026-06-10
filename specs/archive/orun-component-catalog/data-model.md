# Data Model

Every persisted catalog object. JSON on disk. `lowerCamelCase` field names.
RFC 3339 / Z timestamps. ULID IDs with type prefixes (`src_`, `cat_`, `cmp_`).

> Authoritative shapes. The resolver in `resolution-pipeline.md` produces
> these; the writer in `catalog-store.md` persists them; the CLI in
> `cli-surface.md` reads them. Field provenance for inherited or inferred
> values is recorded in `resolution.inheritedFrom` / `resolution.inferredFrom`.

---

## 1. SourceSnapshot

**File:** `sources/<sourceSnapshotKey>/source.json`
**Mutability:** immutable after first write.

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "SourceSnapshot",
  "sourceSnapshotKey": "src-branch-main-cdef456a-t5ab21c3",
  "sourceSnapshotId": "src_01JABC...",
  "repo": "sourceplane/orun",
  "remoteUrl": "git@github.com:sourceplane/orun.git",
  "ref": "refs/heads/main",
  "branch": "main",
  "sourceScope": "branch-main",
  "headRevision": "def456a1b2c3",
  "treeHash": "5ab21c3",
  "workingTree": "clean",
  "dirtyHash": "",
  "catalogInputHash": "sha256:...",
  "createdAt": "2026-05-31T00:00:00Z"
}
```

**`sourceScope` enum:** `branch-main`, `branch-protected`, `branch-feature`,
`pr`, `tag`, `local-dirty`, `local-nogit`, `ci-event`.

**Validation rules:**

- `sourceSnapshotKey` matches `^src-[a-z0-9-]{1,128}$`.
- `headRevision` is a 12+ character hex string OR empty when `sourceScope =
  local-nogit`.
- `treeHash` is 7+ character hex.
- `workingTree ∈ {clean, dirty}`. `dirty ⇒ dirtyHash` is non-empty and starts
  with `sha256:`.
- `catalogInputHash` is the dirty-hash inputs hash even when `workingTree = clean`
  (used as a tie-breaker for resolver caching).

## 2. CatalogSnapshot

**File:** `sources/<sourceSnapshotKey>/catalogs/<catalogSnapshotKey>/catalog.json`
**Mutability:** immutable after first write.

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "CatalogSnapshot",
  "catalogSnapshotKey": "cat-c8e91d2a",
  "catalogSnapshotId": "cat_01JABC...",
  "sourceSnapshotKey": "src-branch-main-cdef456a-t5ab21c3",
  "repo": "sourceplane/orun",
  "sourceScope": "branch-main",
  "headRevision": "def456a1b2c3",
  "treeHash": "5ab21c3",
  "workingTree": "clean",
  "authoritative": true,
  "preview": false,
  "resolver": {
    "orunVersion": "0.18.0",
    "schemaVersion": "orun.io/v1alpha1",
    "resolverVersion": 1,
    "stackSources": [
      "ghcr.io/sourceplane/stack-tectonic:0.12.0"
    ]
  },
  "catalogHash": "sha256:c8e91d2a...",
  "summary": {
    "components": 42,
    "systems": 6,
    "apis": 12,
    "resources": 18,
    "owners": 8,
    "domains": 4
  },
  "objects": {
    "components": [
      {
        "key": "sourceplane/orun/api-edge",
        "name": "api-edge",
        "path": "components/api-edge/manifest.json",
        "manifestHash": "sha256:..."
      }
    ]
  },
  "createdAt": "2026-05-31T00:00:00Z"
}
```

**Validation rules:**

- `catalogSnapshotKey` matches `^cat-[a-f0-9]{6,16}$`. Derived from
  `catalogHash` short prefix.
- `authoritative = true` ⇒ `sourceScope = branch-main` (or another branch
  configured under `intent.yaml` `catalog.sourceOfTruth.canonicalBranches`)
  AND `workingTree = clean`.
- `preview = !authoritative`.
- `objects.components[*].path` is relative to the catalog directory.
- `objects.components[*].manifestHash` matches the manifest's
  `source.manifestHash`.

## 3. ComponentManifest

**File:** `sources/<sourceSnapshotKey>/catalogs/<catalogSnapshotKey>/components/<componentName>/manifest.json`
**Mutability:** immutable after first write. `status.*` fields are mirrored
into the component-execution-index instead of mutating the manifest.

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "ComponentManifest",
  "identity": {
    "componentId": "cmp_01JABC...",
    "componentKey": "sourceplane/orun/api-edge",
    "name": "api-edge",
    "namespace": "sourceplane",
    "repo": "sourceplane/orun",
    "path": "apps/api-edge",
    "sourceFile": "apps/api-edge/component.yaml"
  },
  "source": {
    "sourceSnapshotKey": "src-branch-main-cdef456a-t5ab21c3",
    "catalogSnapshotKey": "cat-c8e91d2a",
    "ref": "refs/heads/main",
    "branch": "main",
    "headRevision": "def456a1b2c3",
    "treeHash": "5ab21c3",
    "workingTree": "clean",
    "manifestHash": "sha256:..."
  },
  "metadata": {
    "title": "API Edge Worker",
    "description": "Public API gateway for tenant traffic",
    "owner": "team/platform-edge",
    "maintainers": ["team/platform-edge"],
    "contacts": { "slack": "#platform-edge", "email": "platform-edge@example.com" },
    "labels": { "domain": "edge", "tier": "critical", "repo": "orun", "namespace": "sourceplane" },
    "tags": ["cloudflare", "api", "edge"],
    "annotations": {
      "github.com/team": "platform-edge",
      "datadoghq.com/service-name": "api-edge"
    }
  },
  "spec": {
    "type": "cloudflare-worker",
    "lifecycle": "production",
    "system": "sourceplane-control-plane",
    "domain": "edge",
    "tier": "critical",
    "composition": {
      "source": "ghcr.io/sourceplane/stack-tectonic:0.12.0",
      "type": "cloudflare-worker"
    },
    "parameters": { "workerName": "api-edge", "stackName": "api-edge" },
    "environments": {
      "development": { "profile": "worker.verify",       "active": true },
      "staging":     { "profile": "worker.pull_request", "active": true },
      "production":  { "profile": "worker.release",      "active": true }
    },
    "dependencies": {
      "components": [
        { "key": "sourceplane/orun/identity-worker",
          "name": "identity-worker",
          "relationship": "calls",
          "optional": false }
      ],
      "apis":      { "provides": ["public-api"], "consumes": ["identity-api"] },
      "resources": { "uses":     ["sourceplane/prod/main-postgres"] }
    }
  },
  "runtime": {
    "inferred": {
      "languages":       ["typescript"],
      "packageManagers": ["pnpm"],
      "frameworks":      ["hono"],
      "infra":           ["cloudflare-worker"]
    },
    "files": {
      "readme":     "apps/api-edge/README.md",
      "package":    "apps/api-edge/package.json",
      "dockerfile": null
    }
  },
  "resolution": {
    "inheritedFrom": {
      "metadata.labels.repo": "intent.yaml:catalog.defaults.labels.repo",
      "metadata.owner":       "component.yaml:spec.owner",
      "spec.environments.production.profile": "component.yaml:spec.environments.production.profile"
    },
    "inferredFrom": {
      "runtime.inferred.languages":  ["apps/api-edge/package.json"],
      "runtime.inferred.frameworks": ["apps/api-edge/package.json"]
    }
  }
}
```

**Validation rules:**

- `identity.componentKey` matches `^[a-z0-9._-]+/[a-z0-9._-]+/[a-z0-9._-]+$`
  (3 segments: `<namespace>/<repo>/<componentName>`).
- `identity.name = last segment of componentKey`.
- `spec.environments.<name>.active` is `true` unless explicitly disabled.
- `spec.dependencies.components[*].relationship ∈ {calls, depends-on,
  deploy-after, links-to}`.
- `metadata.owner` is required when `intent.yaml`
  `catalog.validation.requireOwner = true`.
- `source.manifestHash` is the SHA-256 of the canonical (sorted-keys, no
  whitespace) JSON encoding of `{identity, metadata, spec, runtime}`.
- `resolution.inheritedFrom` keys are JSON-pointer-style paths
  (`metadata.labels.repo`); values are `<file>:<JSON pointer>` provenance.

## 4. CatalogGraph

**File:** `sources/<sourceSnapshotKey>/catalogs/<catalogSnapshotKey>/graph/dependencies.json`
**Mutability:** immutable; siblings `systems.json`, `apis.json`,
`resources.json`, `owners.json` follow the same shape with the relevant
`kind` and edge type vocabulary.

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "CatalogGraph",
  "sourceSnapshotKey": "src-branch-main-cdef456a-t5ab21c3",
  "catalogSnapshotKey": "cat-c8e91d2a",
  "nodes": [
    { "key": "sourceplane/orun/api-edge",        "kind": "Component", "name": "api-edge" },
    { "key": "sourceplane/orun/identity-worker", "kind": "Component", "name": "identity-worker" }
  ],
  "edges": [
    { "from": "sourceplane/orun/api-edge",
      "to":   "sourceplane/orun/identity-worker",
      "type": "calls",
      "optional": false }
  ]
}
```

## 5. ComponentHistoryEvent

**File:** `sources/<sourceSnapshotKey>/catalogs/<catalogSnapshotKey>/history/components/<componentName>/events/<seq>-<eventKind>.json`
**Mutability:** append-only. `<seq>` is a zero-padded 9-digit monotonic
counter scoped per component per catalog.

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "ComponentCatalogEvent",
  "eventType": "execution.completed",
  "componentKey": "sourceplane/orun/api-edge",
  "sourceSnapshotKey": "src-branch-main-cdef456a-t5ab21c3",
  "catalogSnapshotKey": "cat-c8e91d2a",
  "revisionKey": "rev-main-def456a-p8f31c09",
  "executionKey": "run-001",
  "triggerName": "github-push-main",
  "profile": "worker.release",
  "environment": "production",
  "status": "completed",
  "at": "2026-05-31T00:03:00Z"
}
```

**`eventType` enum:** `catalog.resolved`, `manifest.changed`,
`plan.created`, `plan.failed`, `execution.started`, `execution.completed`,
`execution.failed`, `dependency.added`, `dependency.removed`.

## 6. Authored `component.yaml`

Authored intent. Never mutated by Orun.

```yaml
apiVersion: orun.io/v1alpha1
kind: Component
metadata:
  name: api-edge
  title: API Edge Worker
  description: Public API gateway for tenant traffic
  labels:    { domain: edge, tier: critical }
  annotations:
    github.com/team: platform-edge
    datadoghq.com/service-name: api-edge
spec:
  type: cloudflare-worker
  lifecycle: production
  owner: team/platform-edge
  system: sourceplane-control-plane
  path: apps/api-edge
  dependsOn:
    - component: identity-worker
      relationship: calls
      optional: false
  providesApis: [public-api]
  consumesApis: [identity-api]
  environments:
    development: { profile: worker.verify }
    staging:     { profile: worker.pull_request }
    production:  { profile: worker.release }
```

### 6.1 Plan-engine authoring compatibility

A single `component.yaml` is authored for both the plan engine and the
catalog. The catalog schema therefore accepts the full plan-engine authoring
vocabulary (see `internal/model.Component`) in addition to the canonical fields
above. The authored object validates leniently: declared fields are
type-checked, but **unknown keys are accepted and ignored** rather than
rejected, mirroring the plan engine's `yaml.Unmarshal` tolerance. This keeps
existing repositories (and legacy fields such as `spec.inputs`, renamed to
`spec.parameters`) valid against the catalog without modification.

Additional authored fields the catalog reads:

- **`spec.domain`** — folds into the resolved `spec.domain` (and the
  `summary.domains` count).
- **`spec.parameters`** — author-defined map; folds into the resolved
  `spec.parameters` (values are rendered to strings).
- **`spec.labels`** — merged into the resolved `metadata.labels`. On a key
  conflict `metadata.labels` wins.
- **`spec.env`** — accepted for compatibility; not surfaced in the resolved
  manifest.

**Environment bindings — two accepted forms.** The canonical `spec.environments`
map (above) and the legacy `spec.subscribe` list both fold into the resolved
`spec.environments` map; the map form wins on a per-env key conflict. The
`subscribe` list mirrors `internal/model.ComponentSubscribe` and carries the
richer per-environment authoring vocabulary (`profileRules`, `dependencyMode`,
`dependencyRules`, `env`, `parameters`). Only the base `profile` is folded into
the resolved manifest; the rest is accepted but not interpreted by the resolver.
Each entry may be an object or the bare-string shorthand (`- production`), which
binds the environment with no explicit profile:

```yaml
# legacy subscribe form (folds into the resolved spec.environments map)
spec:
  type: cloudflare-worker
  domain: edge
  subscribe:
    environments:
      - name: staging
        profile: worker.pull_request
      - name: production
        profile: worker.release
        profileRules:
          - profile: worker.deploy
            when: { triggerRef: github-push-main }
      - dev            # bare-string shorthand → bound, no explicit profile
  parameters:
    workerName: api-edge
  labels: { team: platform }
```

### 6.2 Limitations

The catalog reads `component.yaml` for inventory and provenance, not for
execution — the plan engine remains the authority on how a component deploys.
Known limitations of the catalog's view, by design:

1. **Unknown keys are accepted, not rejected.** The authoring schema is open,
   so a misspelled or unmodeled key (e.g. `spec.inputs`, a typo'd
   `spec.subscribe.environments[].profle`) validates rather than failing. Each
   is surfaced as a `component.field.unknown` **warning** (promoted to an error
   under `--strict` / `catalog validate`). Linting is bounded to the document
   root, `metadata`, `spec`, and `spec.subscribe.environments[]` objects;
   free-form maps (`spec.parameters`, `spec.labels`, `spec.env`,
   per-environment maps) and `spec.dependsOn` entries are **not** key-checked,
   so typos there pass silently.

2. **`subscribe` is folded to base profiles only.** From each subscribe entry
   the catalog records `name` and the base `profile`. `profileRules`,
   `dependencyMode`, `dependencyRules`, per-environment `env`, and
   per-environment `parameters` are accepted but **not interpreted** — the
   catalog's resolved `environments` map is a static snapshot, not a
   trigger-time evaluation. The bare-string shorthand binds an environment with
   an empty profile.

3. **`spec.env` is not surfaced.** It validates for plan-engine compatibility
   but has no slot in the resolved manifest.

4. **`spec.parameters` values are stringified.** Scalars render to their string
   form; nested maps/lists render to canonical (sorted-key) JSON. The resolved
   `spec.parameters` is `map[string]string`, so structure is preserved only as
   an encoded string, not as typed data.

5. **`spec.labels` merges into `metadata.labels`.** On a key conflict
   `metadata.labels` wins. The catalog has a single resolved label namespace;
   the authored split between `metadata.labels` and `spec.labels` is not
   preserved.

6. **Two parsers, one contract.** The authoring vocabulary is owned by the plan
   engine (`internal/model.Component`); the catalog mirrors it. When the plan
   engine gains a field, the catalog accepts it immediately via the open schema
   but will not interpret it until explicitly modeled. Keep the two in sync —
   see the compatibility tests in `internal/catalogresolve`.

## 7. `intent.yaml` catalog defaults

Optional new top-level block.

```yaml
catalog:
  namespace: sourceplane

  defaults:
    lifecycle: experimental
    owner: team/platform
    contacts:  { slack: "#platform" }
    labels:    { repo: orun, namespace: sourceplane }

  sourceOfTruth:
    canonicalBranches: [main]
    allowDirtySync: false

  inference:
    enabled: true
    readme: true
    packageJson: true
    dockerfile: true
    terraform: true
    helm: true

  validation:
    requireOwner: true
    requireLifecycle: true
    requireSystem: false
    allowUnknownOwners: true
```

## 8. Refs

### 8.1 SourceRef

**Files:**
`refs/sources/{latest,current,main}.json`,
`refs/sources/branches/<branch>.json`,
`refs/sources/prs/<pr>.json`.

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "SourceRef",
  "name": "main",
  "sourceScope": "branch-main",
  "sourceSnapshotKey": "src-branch-main-cdef456a-t5ab21c3",
  "headRevision": "def456a1b2c3",
  "treeHash": "5ab21c3",
  "workingTree": "clean",
  "authoritative": true,
  "updatedAt": "2026-05-31T00:00:00Z"
}
```

### 8.2 CatalogRef

**Files:**
`refs/catalogs/{latest,current,main}.json`,
`refs/catalogs/branches/<branch>.json`,
`refs/catalogs/prs/<pr>.json`.

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "CatalogRef",
  "name": "main",
  "sourceScope": "branch-main",
  "sourceSnapshotKey": "src-branch-main-cdef456a-t5ab21c3",
  "catalogSnapshotKey": "cat-c8e91d2a",
  "catalogHash": "sha256:c8e91d2a...",
  "headRevision": "def456a1b2c3",
  "treeHash": "5ab21c3",
  "authoritative": true,
  "preview": false,
  "updatedAt": "2026-05-31T00:00:00Z"
}
```

## 9. Indexes

### 9.1 Component global index

**File:** `indexes/components/<componentKey-sanitized>.json`
(component key `sourceplane/orun/api-edge` becomes
`sourceplane-orun-api-edge.json`).

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "ComponentGlobalIndex",
  "componentKey": "sourceplane/orun/api-edge",
  "name": "api-edge",
  "repo": "sourceplane/orun",
  "latest": {
    "sourceSnapshotKey": "src-branch-feature-x-cabc1234-t9aa7710",
    "catalogSnapshotKey": "cat-d013aa11"
  },
  "main": {
    "sourceSnapshotKey": "src-branch-main-cdef456a-t5ab21c3",
    "catalogSnapshotKey": "cat-c8e91d2a",
    "manifestPath": "sources/src-branch-main-cdef456a-t5ab21c3/catalogs/cat-c8e91d2a/components/api-edge/manifest.json"
  },
  "previews": [
    { "sourceScope": "pr-139",
      "sourceSnapshotKey": "src-pr139-cabc1234-t9aa7710",
      "catalogSnapshotKey": "cat-d013aa11" }
  ]
}
```

### 9.2 Component execution index

**File:** `sources/<sourceSnapshotKey>/catalogs/<catalogSnapshotKey>/indexes/components/<componentName>.json`
(catalog-local; complemented by a global index under
`indexes/components/<componentKey-sanitized>.json` whose `latest` summarises
the per-source view above).

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "ComponentExecutionIndex",
  "componentKey": "sourceplane/orun/api-edge",
  "sourceSnapshotKey": "src-branch-main-cdef456a-t5ab21c3",
  "catalogSnapshotKey": "cat-c8e91d2a",
  "executions": [
    {
      "revisionKey": "rev-main-def456a-p8f31c09",
      "executionKey": "run-001",
      "triggerName": "github-push-main",
      "profile": "worker.release",
      "environment": "production",
      "status": "completed",
      "createdAt": "2026-05-31T00:00:00Z"
    }
  ]
}
```

### 9.3 Source / catalog / revision / execution indexes

The Phase 1 `indexes/revisions/<revKey>.json` and
`indexes/executions/<execKey>.json` shapes are unchanged; this spec adds two
new fields to each:

```json
{
  "sourceSnapshotKey": "src-branch-main-cdef456a-t5ab21c3",
  "catalogSnapshotKey": "cat-c8e91d2a"
}
```

New indexes:

- `indexes/sources/<sourceSnapshotKey>.json` — denormalized `SourceSnapshot`
  fields plus `catalogs[]` array and `latestCatalogSnapshotKey`.
- `indexes/catalogs/<catalogSnapshotKey>.json` — denormalized
  `CatalogSnapshot` summary plus `revisions[]` and `executions[]` (latest N).

## 10. Plan / Execution metadata addition (Phase 1 reuse)

Existing Phase 1 `PlanRevision` and `ExecutionRun` shapes get two new fields
added to their root:

```json
{
  "sourceSnapshotKey": "src-branch-main-cdef456a-t5ab21c3",
  "catalogSnapshotKey": "cat-c8e91d2a"
}
```

Plus a `metadata.catalog` block on `PlanRevision` for downstream consumers
(`internal/runbundle`, TUI cockpit):

```json
{
  "metadata": {
    "source": {
      "sourceSnapshotKey": "src-branch-main-cdef456a-t5ab21c3",
      "headRevision": "def456a1b2c3",
      "treeHash": "5ab21c3",
      "workingTree": "clean"
    },
    "catalog": {
      "catalogSnapshotKey": "cat-c8e91d2a",
      "catalogHash": "sha256:c8e91d2a..."
    }
  }
}
```

## 11. Determinism rules

JSON encoding for **all** persisted catalog objects:

- Keys sorted lexicographically.
- 2-space indent for human-readable files (`source.json`, `catalog.json`,
  manifests, refs); compact (no whitespace) for hashed inputs.
- Maps stored as JSON objects with sorted keys; arrays preserve order from
  the resolver (which itself sorts deterministically — see
  `resolution-pipeline.md`).
- Time fields in RFC 3339 with `Z` timezone.
- No trailing newline differences; writer uses `StateStore.Write` atomic
  rename so partial files never appear.
