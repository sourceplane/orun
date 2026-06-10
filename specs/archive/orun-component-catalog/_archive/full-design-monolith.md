# Design: `orun-component-catalog`

This is the next major Orun model after `orun-state-redesign`.

The key change is:

```text
Git/source state owns catalog state.
Catalog state owns plans, revisions, executions, and component history.
```

So the new lineage becomes:

```text
SourceSnapshot
  └─ CatalogSnapshot
      ├─ ComponentManifest[]
      ├─ CatalogGraph
      ├─ CatalogIndexes
      ├─ PlanRevision[]
      │   └─ ExecutionRun[]
      └─ ComponentHistory
```

This extends the existing Phase 1 state design, which already introduces trigger → revision → execution lineage, `StateStore`, refs, indexes, and remote-shaped paths. The current source design says new state writes should go through `StateStore`, with logical paths that can later map to object storage, so the catalog implementation should reuse that same storage contract rather than creating a parallel persistence layer. 

---

# 1. Problem

Today Orun treats components mainly as execution inputs. That is enough for planning, but not enough for a serious catalog experience.

The next model needs to answer:

```text
What components exist?
Who owns them?
Where do they run?
What system/domain do they belong to?
What APIs/resources do they provide or consume?
What changed between main and this branch?
Which plans and executions are tied to this component?
What is the latest successful/failed execution for production?
What does SaaS show as canonical state?
What does my local branch/worktree show as preview state?
```

The earlier state redesign fixes plan/execution lineage, but the catalog needs one more parent level: **source/catalog state**. The existing state design explicitly identifies the old problem as disconnected plans and executions, weak provenance, flat paths, and poor remote mapping. The catalog model should solve the same class of problem for components. 

---

# 2. Goals

## G1 — Component catalog as a first-class Orun object

A component is not only a YAML file. It becomes a resolved software catalog entity with ownership, metadata, environment activation, dependencies, runtime facts, and execution history.

## G2 — Git/source state is the root

Every catalog snapshot belongs to an exact source state:

```text
branch main at commit X, tree Y
PR 139 at commit A, tree B
local dirty workspace based on commit C plus dirty hash D
```

## G3 — Main branch is SaaS source of truth

Only clean authoritative refs, usually `main`, update the canonical SaaS catalog.

Branches, PRs, and dirty worktrees produce preview snapshots.

## G4 — Plan/revision/execution belongs under catalog snapshot

Plans and executions should be queryable from the catalog page:

```text
component → source selector → catalog snapshot → revisions → executions
```

## G5 — Fully materialized `ComponentManifest`

`component.yaml` stays small and authored. `ComponentManifest` is generated, inherited, validated, and complete.

## G6 — SaaS-ready but local-first

Phase 1 of the catalog work should be entirely local, but every path, key, manifest, and ref should be shaped for later remote sync.

## G7 — Additive CLI rollout

Existing `orun plan`, `orun run`, `orun status`, `orun logs`, and `orun describe` behavior should continue to work. The previous spec already makes backward compatibility a hard requirement for command rewiring. 

---

# 3. Non-goals for this phase

Do **not** implement yet:

```text
SaaS API server
Cloudflare Durable Object coordination
Supabase/D1 catalog indexing
R2/S3 StateStore driver
Backstage/Datadog export adapters
TUI full-screen catalog UI
distributed locking
remote auth
```

But the local schema and path model must be designed so these can be added later without changing the core model.

---

# 4. Core lineage

## Final lineage

```text
SourceSnapshot
  “exact git/worktree state”

CatalogSnapshot
  “resolved software catalog for that source state”

ComponentManifest
  “fully materialized component definition”

PlanRevision
  “plan compiled against that catalog”

ExecutionRun
  “run result for that plan”

ComponentHistory
  “query index from catalog component to plans/executions”
```

## Relationship to existing state redesign

The existing state redesign has:

```text
TriggerOccurrence
  └─ PlanRevision
      └─ ExecutionRun
```

The catalog design wraps this with a source/catalog parent:

```text
SourceSnapshot
  └─ CatalogSnapshot
      └─ TriggerOccurrence
          └─ PlanRevision
              └─ ExecutionRun
```

This preserves the existing trigger/revision/execution model while making catalog state the navigation root. The source design already says the first three execution lineage levels are first-class persisted records and that job/step records are reserved for later runner-native work. 

---

# 5. Local state layout

## Canonical layout

```text
.orun/
  version.json

  sources/
    src-branch-main-cdef456a-t5ab21c3/
      source.json

      catalogs/
        cat-c8e91d2a/
          catalog.json

          components/
            api-edge/
              manifest.json
            identity-worker/
              manifest.json

          graph/
            dependencies.json
            systems.json
            apis.json
            resources.json
            owners.json

          indexes/
            components/
              api-edge.json
            owners/
              team-platform-edge.json
            systems/
              sourceplane-control-plane.json
            domains/
              edge.json
            types/
              cloudflare-worker.json

          revisions/
            rev-main-def456a-p8f31c09/
              trigger.json
              revision.json
              plan.json
              manifest.json

              executions/
                run-001/
                  execution.json
                  snapshot.latest.json
                  state.json
                  metadata.json
                  logs/
                  events/
                  artifacts/

          history/
            components/
              api-edge/
                events/
                  000000001-catalog-resolved.json
                  000000002-plan-created.json
                  000000003-execution-completed.json

  refs/
    sources/
      latest.json
      current.json
      main.json
      branches/
        feature-x.json
      prs/
        pr-139.json

    catalogs/
      latest.json
      current.json
      main.json
      branches/
        feature-x.json
      prs/
        pr-139.json

    revisions/
      latest.json

    executions/
      latest.json

  indexes/
    sources/
      src-branch-main-cdef456a-t5ab21c3.json
    catalogs/
      cat-c8e91d2a.json
    components/
      sourceplane-orun-api-edge.json
    revisions/
      rev-main-def456a-p8f31c09.json
    executions/
      run-001.json
```

## Why this layout is better

The catalog snapshot becomes the primary object for SaaS and TUI navigation.

For a component page:

```text
refs/catalogs/main.json
  → sourceSnapshotKey
  → catalogSnapshotKey
  → components/api-edge/manifest.json
  → indexes/components/api-edge.json
  → related revisions/executions
```

For an execution page:

```text
indexes/executions/run-001.json
  → sourceSnapshotKey
  → catalogSnapshotKey
  → revisionKey
  → execution path
```

This gives both directions:

```text
catalog → component → plan/run
run → revision → catalog/component
```

The existing `StateStore` contract already requires logical forward-slash paths, no machine-local path segments, and future remote portability, which matches this layout. 

---

# 6. Identity and key model

## 6.1 SourceSnapshot key

Use commit, tree, and dirty hash.

```text
clean main:
  src-branch-main-cdef456a-t5ab21c3

PR:
  src-pr139-cabc1234-t9aa7710

feature branch:
  src-branch-feature-x-cabc1234-t9aa7710

dirty workspace:
  src-branch-feature-x-cabc1234-t9aa7710-d91aa77b2

no git:
  src-local-nogit-d91aa77b2
```

Where:

```text
cdef456a   = short HEAD commit
t5ab21c3   = short Git tree hash
d91aa77b2  = catalog input dirty hash
```

## 6.2 CatalogSnapshot key

```text
cat-c8e91d2a
```

The `catalogHash` is derived from:

```text
resolved component manifests
catalog resolver version
schema version
intent catalog defaults
composition/stack metadata used for resolution
source snapshot key
```

## 6.3 Component key

```text
<namespace>/<repo>/<componentName>
```

Example:

```text
sourceplane/orun/api-edge
```

Do not include environment in component identity. Environment belongs under `spec.environments`.

## 6.4 Revision and execution keys

Keep the existing style:

```text
rev-main-def456a-p8f31c09
run-001
```

But every revision and execution now also carries:

```json
{
  "sourceSnapshotKey": "src-branch-main-cdef456a-t5ab21c3",
  "catalogSnapshotKey": "cat-c8e91d2a"
}
```

The previous state design already separates human-readable folder keys from opaque ULID IDs; keep that rule for catalog objects too. 

---

# 7. Data model

## 7.1 `SourceSnapshot`

File:

```text
sources/<sourceSnapshotKey>/source.json
```

Schema:

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

Dirty example:

```json
{
  "sourceSnapshotKey": "src-branch-feature-x-cdef456a-t5ab21c3-d91aa77b2",
  "sourceScope": "local-dirty",
  "branch": "feature-x",
  "headRevision": "def456a1b2c3",
  "treeHash": "5ab21c3",
  "workingTree": "dirty",
  "dirtyHash": "sha256:91aa77b2...",
  "catalogInputHash": "sha256:..."
}
```

## 7.2 `CatalogSnapshot`

File:

```text
sources/<sourceSnapshotKey>/catalogs/<catalogSnapshotKey>/catalog.json
```

Schema:

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

## 7.3 `ComponentManifest`

File:

```text
sources/<sourceSnapshotKey>/catalogs/<catalogSnapshotKey>/components/<componentName>/manifest.json
```

Schema:

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
    "maintainers": [
      "team/platform-edge"
    ],
    "contacts": {
      "slack": "#platform-edge",
      "email": "platform-edge@example.com"
    },
    "labels": {
      "domain": "edge",
      "tier": "critical",
      "repo": "orun",
      "namespace": "sourceplane"
    },
    "tags": [
      "cloudflare",
      "api",
      "edge"
    ],
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
    "parameters": {
      "workerName": "api-edge",
      "stackName": "api-edge"
    },
    "environments": {
      "development": {
        "profile": "worker.verify",
        "active": true
      },
      "staging": {
        "profile": "worker.pull_request",
        "active": true
      },
      "production": {
        "profile": "worker.release",
        "active": true
      }
    },
    "dependencies": {
      "components": [
        {
          "key": "sourceplane/orun/identity-worker",
          "name": "identity-worker",
          "relationship": "calls",
          "optional": false
        }
      ],
      "apis": {
        "provides": [
          "public-api"
        ],
        "consumes": [
          "identity-api"
        ]
      },
      "resources": {
        "uses": [
          "sourceplane/prod/main-postgres"
        ]
      }
    }
  },
  "runtime": {
    "inferred": {
      "languages": [
        "typescript"
      ],
      "packageManagers": [
        "pnpm"
      ],
      "frameworks": [
        "hono"
      ],
      "infra": [
        "cloudflare-worker"
      ]
    },
    "files": {
      "readme": "apps/api-edge/README.md",
      "package": "apps/api-edge/package.json",
      "dockerfile": null
    }
  },
  "status": {
    "latestRevisionKey": "rev-main-def456a-p8f31c09",
    "latestExecutionKey": "run-001",
    "latestExecutionStatus": "completed",
    "lastPlannedAt": "2026-05-31T00:00:00Z",
    "lastExecutedAt": "2026-05-31T00:03:00Z"
  },
  "resolution": {
    "inheritedFrom": {
      "metadata.labels.repo": "intent.yaml:catalog.defaults.labels.repo",
      "metadata.owner": "component.yaml:spec.owner",
      "spec.environments.production.profile": "component.yaml:spec.environments.production.profile"
    },
    "inferredFrom": {
      "runtime.inferred.languages": [
        "apps/api-edge/package.json"
      ],
      "runtime.inferred.frameworks": [
        "apps/api-edge/package.json"
      ]
    }
  }
}
```

## 7.4 `CatalogGraph`

File:

```text
graph/dependencies.json
```

Schema:

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "CatalogGraph",
  "sourceSnapshotKey": "src-branch-main-cdef456a-t5ab21c3",
  "catalogSnapshotKey": "cat-c8e91d2a",
  "nodes": [
    {
      "key": "sourceplane/orun/api-edge",
      "kind": "Component",
      "name": "api-edge"
    },
    {
      "key": "sourceplane/orun/identity-worker",
      "kind": "Component",
      "name": "identity-worker"
    }
  ],
  "edges": [
    {
      "from": "sourceplane/orun/api-edge",
      "to": "sourceplane/orun/identity-worker",
      "type": "calls",
      "optional": false
    }
  ]
}
```

## 7.5 `ComponentHistoryEvent`

File:

```text
history/components/<componentName>/events/000000003-execution-completed.json
```

Schema:

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

The existing state design already has an append-only event stream under execution directories. The catalog model should mirror that approach for component history, using sortable event files. 

---

# 8. Authored `component.yaml` model

## Minimal example

```yaml
apiVersion: orun.io/v1alpha1
kind: Component

metadata:
  name: api-edge
  title: API Edge Worker
  description: Public API gateway for tenant traffic
  labels:
    domain: edge
    tier: critical
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

  providesApis:
    - public-api

  consumesApis:
    - identity-api

  environments:
    development:
      profile: worker.verify
    staging:
      profile: worker.pull_request
    production:
      profile: worker.release
```

## Design rule

`component.yaml` is authored intent.

`ComponentManifest` is the full resolved catalog truth.

That means `component.yaml` may omit values that can be inherited or inferred.

---

# 9. Intent-level catalog defaults

Add a new optional block to `intent.yaml`.

```yaml
catalog:
  namespace: sourceplane

  defaults:
    lifecycle: experimental
    owner: team/platform
    contacts:
      slack: "#platform"
    labels:
      repo: orun
      namespace: sourceplane

  sourceOfTruth:
    canonicalBranches:
      - main
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

---

# 10. Resolution and inheritance

## Resolver pipeline

```text
1. Resolve source snapshot
2. Discover component.yaml files
3. Load root intent.yaml catalog defaults
4. Load stack/composition metadata
5. Load component-authored fields
6. Infer runtime facts from repo files
7. Resolve dependencies and entity refs
8. Apply inheritance
9. Validate manifest
10. Compute manifest hash
11. Build catalog graph
12. Compute catalog hash
13. Write source/catalog snapshot
14. Update refs and indexes
```

## Precedence

Explicit fields win.

```text
1. component.yaml explicit fields
2. component-local inferred files
3. composition defaults
4. intent.yaml catalog defaults
5. org/project defaults from future SaaS config
```

The lower layers should fill only missing values, unless a field is explicitly declared overridable.

## Field provenance

Every inherited or inferred field should be traceable:

```json
{
  "resolution": {
    "inheritedFrom": {
      "metadata.labels.repo": "intent.yaml:catalog.defaults.labels.repo",
      "metadata.owner": "component.yaml:spec.owner"
    },
    "inferredFrom": {
      "runtime.inferred.languages": [
        "apps/api-edge/package.json"
      ]
    }
  }
}
```

This is important for debugging and SaaS UX.

---

# 11. New packages

## `internal/sourcectx`

Owns source state.

```text
internal/sourcectx/
  model.go
  resolve.go
  git.go
  dirty.go
  keys.go
  hash.go
```

Responsibilities:

```text
ResolveSourceSnapshot()
ComputeTreeHash()
ComputeDirtyHash()
ComputeCatalogInputHash()
SourceSnapshotKey()
DetectSourceScope()
```

## `internal/catalogmodel`

Pure data models.

```text
internal/catalogmodel/
  source_snapshot.go
  catalog_snapshot.go
  component_manifest.go
  component_yaml.go
  graph.go
  refs.go
  indexes.go
  events.go
  entity_ref.go
```

No filesystem access.

## `internal/catalogresolve`

Turns repo files into resolved manifests.

```text
internal/catalogresolve/
  discover.go
  load.go
  inherit.go
  infer.go
  dependencies.go
  graph.go
  validate.go
  hash.go
  resolver.go
```

Responsibilities:

```text
Discover components
Resolve ComponentManifest
Build CatalogSnapshot
Build CatalogGraph
Validate catalog
Compute hashes
```

## `internal/catalogstore`

Persists catalog records via `internal/statestore`.

```text
internal/catalogstore/
  paths.go
  writer.go
  refs.go
  indexes.go
  resolver.go
  history.go
```

Responsibilities:

```text
WriteSourceSnapshot
WriteCatalogSnapshot
WriteComponentManifest
WriteCatalogGraph
WriteCatalogRefs
WriteCatalogIndexes
ResolveCatalog
ResolveComponent
AppendComponentEvent
```

## `internal/catalogdiff`

Diffs source/catalog snapshots.

```text
internal/catalogdiff/
  diff.go
  component_diff.go
  graph_diff.go
  render.go
```

## `internal/catalogsync`

Future seam only in this phase.

```text
internal/catalogsync/
  interface.go
  noop.go
  payload.go
```

Initial interface:

```go
type Syncer interface {
    PushCatalogSnapshot(ctx context.Context, snapshot CatalogSnapshot, opts PushOptions) error
}
```

Phase 1 implementation:

```text
NoopSyncer
```

Do not implement remote networking yet.

The existing spec’s package-boundary style is milestone-based and keeps new functionality isolated by ownership. Follow that same pattern for catalog packages. 

---

# 12. StateStore path helpers

Add helpers under `internal/catalogstore/paths.go`, not raw string concatenation.

```go
func SourceDir(sourceKey string) string
func SourceDocPath(sourceKey string) string

func CatalogDir(sourceKey, catalogKey string) string
func CatalogDocPath(sourceKey, catalogKey string) string

func ComponentDir(sourceKey, catalogKey, componentName string) string
func ComponentManifestPath(sourceKey, catalogKey, componentName string) string

func CatalogGraphPath(sourceKey, catalogKey, graphName string) string

func CatalogRevisionDir(sourceKey, catalogKey, revKey string) string
func CatalogRevisionPlanPath(sourceKey, catalogKey, revKey string) string
func CatalogExecutionDir(sourceKey, catalogKey, revKey, execKey string) string

func SourceRefPath(name string) string
func SourceBranchRefPath(branch string) string
func SourcePRRefPath(pr string) string

func CatalogRefPath(name string) string
func CatalogBranchRefPath(branch string) string
func CatalogPRRefPath(pr string) string

func ComponentIndexPath(componentKey string) string
func CatalogIndexPath(catalogKey string) string
func SourceIndexPath(sourceKey string) string
func ComponentHistoryEventPath(sourceKey, catalogKey, componentName string, seq uint64, kind string) string
```

The existing `StateStore` contract says callers must use path helpers and that raw path concatenation is forbidden to keep path policy enforceable. Follow that rule for catalog paths too. 

---

# 13. Write order

Catalog snapshot writes are multi-object and not transactional, so use body-before-ref ordering, just like the state redesign.

## `orun catalog refresh` write order

```text
1. Resolve SourceSnapshot
2. Resolve CatalogSnapshot and ComponentManifest[]
3. Write source.json
4. Write component manifests
5. Write graph files
6. Write catalog.json
7. Write catalog-local indexes
8. Write global indexes
9. Write refs/sources/current.json
10. Write refs/catalogs/current.json
11. If authoritative main:
      write refs/sources/main.json
      write refs/catalogs/main.json
12. If branch:
      write refs/sources/branches/<branch>.json
      write refs/catalogs/branches/<branch>.json
13. If PR:
      write refs/sources/prs/<pr>.json
      write refs/catalogs/prs/<pr>.json
```

The earlier `StateStore` design explicitly says compound writes are not transactional; callers should write the body before refs and rely on resolver scans as consistency fallback. 

---

# 14. CLI surface

## 14.1 New commands

```text
orun catalog refresh
orun catalog list
orun catalog describe <component>
orun catalog tree
orun catalog diff
orun catalog history <component>
orun catalog refs
orun catalog validate
```

## 14.2 `orun catalog refresh`

Purpose: materialize source/catalog snapshot.

```bash
orun catalog refresh
orun catalog refresh --source current
orun catalog refresh --source main
orun catalog refresh --json
orun catalog refresh --no-infer
orun catalog refresh --strict
```

Example output:

```text
✓ Catalog snapshot created

Source:   src-branch-main-cdef456a-t5ab21c3
Catalog:  cat-c8e91d2a
Ref:      refs/heads/main
Tree:     5ab21c3
State:    clean
Mode:     authoritative
Components: 42
Systems:    6
APIs:       12
Resources:  18
Path: .orun/sources/src-branch-main-cdef456a-t5ab21c3/catalogs/cat-c8e91d2a/catalog.json
```

Dirty output:

```text
✓ Catalog snapshot created

Source:   src-branch-feature-x-cdef456a-t5ab21c3-d91aa77b2
Catalog:  cat-f31cc991
State:    dirty
Mode:     local-preview
Components: 43

Note: dirty snapshots are local-only unless --sync-dirty-preview is used.
```

## 14.3 `orun catalog list`

```bash
orun catalog list
orun catalog list --source main
orun catalog list --source current
orun catalog list --owner team/platform-edge
orun catalog list --domain edge
orun catalog list --type cloudflare-worker
orun catalog list --status failed
```

Output:

```text
COMPONENT        TYPE                OWNER                 SYSTEM                    LAST EXEC   STATUS
api-edge         cloudflare-worker   team/platform-edge    sourceplane-control-plane run-001     completed
identity-worker  cloudflare-worker   team/identity         sourceplane-control-plane run-004     failed
```

## 14.4 `orun catalog describe <component>`

```bash
orun catalog describe api-edge
orun catalog describe api-edge --source main
orun catalog describe api-edge --source pr-139
orun catalog describe api-edge --json
```

Output sections:

```text
Component
Ownership
Source
Environments
Profiles
Dependencies
APIs
Resources
Runtime inference
Latest executions
Resolution provenance
```

## 14.5 `orun catalog tree`

```bash
orun catalog tree
orun catalog tree api-edge
orun catalog tree --source current
orun catalog tree --direction both
```

Shows component dependency graph.

## 14.6 `orun catalog diff`

```bash
orun catalog diff --base main --head current
orun catalog diff --base main --head pr-139
orun catalog diff api-edge --base main --head current
```

Output:

```text
Catalog diff: main → current

Changed components:
  api-edge
    owner: team/platform → team/platform-edge
    dependencies:
      + identity-worker calls required
    environments.production.profile:
      worker.verify → worker.release

Added components:
  billing-worker

Removed components:
  none
```

## 14.7 `orun catalog history <component>`

```bash
orun catalog history api-edge
orun catalog history api-edge --source main
orun catalog history api-edge --trigger github-push-main
orun catalog history api-edge --profile worker.release
orun catalog history api-edge --environment production
```

Output:

```text
TIME                 REVISION                    EXEC     TRIGGER            PROFILE          ENV          STATUS
2026-05-31 00:03     rev-main-def456a-p8f31c09   run-001  github-push-main   worker.release   production   completed
```

## 14.8 `orun catalog refs`

```bash
orun catalog refs
```

Output:

```text
REF              SOURCE                                      CATALOG       MODE
main             src-branch-main-cdef456a-t5ab21c3           cat-c8e91d2a  authoritative
current          src-branch-feature-x-cabc1234-t9aa7710      cat-d013aa11  preview
pr-139           src-pr139-cabc1234-t9aa7710                 cat-d013aa11  preview
```

## 14.9 `orun catalog validate`

```bash
orun catalog validate
orun catalog validate --strict
orun catalog validate --source current
```

Checks:

```text
component name validity
duplicate component names
missing owner
invalid environment binding
missing composition type
broken dependency refs
cycle policy
schema validity
unknown lifecycle
unknown owner, if strict
```

---

# 15. Integration with `orun plan`

## New plan flow

```text
load intent
resolve TriggerOccurrence
resolve SourceSnapshot
resolve or refresh CatalogSnapshot
select components from catalog snapshot
compile plan
compute plan hash
derive revision key
write revision under:
  sources/<sourceKey>/catalogs/<catalogKey>/revisions/<revKey>
update catalog component indexes/history
update global revision indexes
update refs/revisions/latest.json
compatibility write, if enabled
```

The existing CLI rewire already makes `orun plan` always resolve a `TriggerOccurrence`, compile trigger metadata into the plan, compute a plan hash, write canonical state, and update refs/indexes. The catalog phase inserts source/catalog resolution before plan compilation. 

## Plan metadata addition

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
    },
    "trigger": {
      "name": "github-push-main"
    },
    "revision": {
      "key": "rev-main-def456a-p8f31c09",
      "planHash": "sha256:8f31c09..."
    }
  }
}
```

## New flags

```text
orun plan --no-catalog-refresh
orun plan --catalog-source current
orun plan --catalog-source main
orun plan --catalog-snapshot <catalogKey>
orun plan --catalog-strict
```

Default behavior:

```text
orun plan
  refreshes current source catalog if missing or stale

orun plan --changed
  resolves catalog for the same source scope used by changed detection

orun plan --from-ci
  resolves source/catalog from CI event context
```

---

# 16. Integration with `orun run`

## New run flow

```text
resolve revision
resolve parent sourceSnapshotKey/catalogSnapshotKey
create ExecutionRun under revision
execute runner
bridge logs/state as before
update execution refs
update component execution indexes
append component history events
update ComponentManifest.status, if this catalog snapshot is mutable-local latest
```

## Execution metadata addition

```json
{
  "apiVersion": "orun.io/v1alpha1",
  "kind": "ExecutionRun",
  "executionKey": "run-001",
  "sourceSnapshotKey": "src-branch-main-cdef456a-t5ab21c3",
  "catalogSnapshotKey": "cat-c8e91d2a",
  "revisionKey": "rev-main-def456a-p8f31c09",
  "status": "completed"
}
```

The previous design creates execution records under revision directories and updates latest execution refs. Keep that logic, but route the canonical execution path through the catalog parent. 

---

# 17. Compatibility strategy

## Existing global revision paths

For one release phase, support both:

```text
new canonical:
  sources/<sourceKey>/catalogs/<catalogKey>/revisions/<revKey>/plan.json

legacy Phase 1 canonical:
  revisions/<revKey>/plan.json
```

Recommended behavior:

```text
new writes:
  write only source/catalog canonical path
  write global indexes pointing to canonical path
  optionally write compatibility alias at revisions/<revKey>/plan.json if stateCompatibilityWrites is enabled

reads:
  check global revision index first
  if index missing, try old revisions/<revKey>
  if missing, try legacy .orun/plans/<hash>.json
```

This follows the same compatibility philosophy as the previous phase, where existing invocations and old paths must continue working and migration is additive. 

## Existing `orun describe revision latest`

Should show:

```text
Revision: rev-main-def456a-p8f31c09
Source:   src-branch-main-cdef456a-t5ab21c3
Catalog:  cat-c8e91d2a
Trigger:  github-push-main
Jobs:     12
Path:     .orun/sources/.../revisions/rev-main-def456a-p8f31c09/plan.json
```

---

# 18. Refs and indexes

## Source ref

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

## Catalog ref

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

## Global component index

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
    {
      "sourceScope": "pr-139",
      "sourceSnapshotKey": "src-pr139-cabc1234-t9aa7710",
      "catalogSnapshotKey": "cat-d013aa11"
    }
  ]
}
```

## Component execution index

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

---

# 19. SaaS sync design for later

No remote implementation now, but the local model should already match the future SaaS contract.

## Future remote object layout

```text
orgs/<org>/projects/<project>/repos/<repo>/
  sources/
    <sourceSnapshotKey>/
      source.json
      catalogs/
        <catalogSnapshotKey>/
          catalog.json
          components/<componentName>/manifest.json
          graph/dependencies.json
          revisions/<revisionKey>/...
          history/components/<componentName>/events/*.json

  refs/
    sources/main.json
    sources/branches/<branch>.json
    sources/prs/<pr>.json

    catalogs/main.json
    catalogs/branches/<branch>.json
    catalogs/prs/<pr>.json

  indexes/
    components/<componentKey>.json
    revisions/<revisionKey>.json
    executions/<executionKey>.json
```

## Future DB tables

```text
source_snapshots
catalog_snapshots
component_manifests
component_edges
component_execution_links
component_revision_links
catalog_refs
source_refs
teams
systems
apis
resources
```

## SaaS write rules

```text
clean main:
  authoritative = true
  updates canonical component state

clean protected branch:
  authoritative = configurable
  default false unless configured

PR:
  authoritative = false
  creates preview state

feature branch:
  authoritative = false
  creates branch preview

dirty workspace:
  local only by default
  remote sync requires explicit --sync-dirty-preview
```

## SaaS component page query

```text
component = api-edge
source selector = main

1. Read catalog_refs where name = main
2. Load catalog snapshot
3. Load component manifest
4. Load component execution links
5. Render status, history, dependency graph
```

## SaaS PR preview query

```text
component = api-edge
source selector = pr-139

1. Read catalog_refs where name = pr-139
2. Load PR component manifest
3. Load main component manifest
4. Compute diff
5. Render preview state and PR execution history
```

---

# 20. Dirty workspace rules

Dirty snapshots are useful locally but dangerous remotely.

## Dirty hash inputs

Only hash catalog-relevant files:

```text
intent.yaml
component.yaml files
stack/composition refs
package files used for inference
README files used for catalog descriptions
Dockerfile/Helm/Terraform metadata files used for inference
```

Do **not** hash every changed file by default, otherwise dirty snapshots churn too much.

## Dirty source key

```text
src-branch-feature-x-cabc1234-t9aa7710-d91aa77b2
```

## Dirty sync policy

```text
default:
  no remote sync

explicit:
  orun catalog refresh --sync-dirty-preview
```

Remote dirty previews should have TTL later.

---

# 21. Implementation milestones

## C0 — Catalog spec and model foundation

Add:

```text
specs/orun-component-catalog/
  README.md
  design.md
  data-model.md
  cli-surface.md
  sync-model.md
  test-plan.md
  risks-and-open-questions.md
```

Add packages:

```text
internal/catalogmodel
internal/sourcectx
```

Done when:

```text
go test ./internal/catalogmodel ./internal/sourcectx
schemas have validation tests
source key generation has table tests
no CLI changes yet
```

## C1 — SourceSnapshot resolver

Implement:

```text
ResolveSourceSnapshot()
Git HEAD/ref detection
tree hash detection
dirty catalog input hash
source scope detection
source key generation
```

Done when:

```text
clean branch source key stable
dirty branch source key includes dirty hash
no-git workspace works
PR/event scope can be injected from trigger context
```

## C2 — Component discovery and manifest resolver

Implement:

```text
discover component.yaml
load component authored model
merge intent catalog defaults
infer runtime metadata
resolve dependencies
validate required fields
emit ComponentManifest[]
```

Done when:

```text
component.yaml → ComponentManifest works
inheritance provenance is populated
broken dependency is reported as typed validation error
manifest hash is stable
```

## C3 — CatalogSnapshot builder

Implement:

```text
CatalogSnapshot builder
CatalogGraph builder
catalog hash
summary counts
component object list
```

Done when:

```text
same source + same inputs produce same catalog hash
changed component metadata changes catalog hash
graph includes component dependencies
```

## C4 — CatalogStore writer

Implement:

```text
WriteSourceSnapshot
WriteCatalogSnapshot
WriteComponentManifest
WriteCatalogGraph
WriteRefs
WriteIndexes
ResolveCatalog
ResolveComponent
```

Done when:

```text
all writes go through StateStore
paths use helpers only
refs/current and refs/main work
reader fallback scans if ref missing
```

The previous state design makes `StateStore` the only path for new layout writes; keep that invariant here. 

## C5 — Catalog CLI

Add:

```text
orun catalog refresh
orun catalog list
orun catalog describe
orun catalog tree
orun catalog diff
orun catalog history
orun catalog validate
orun catalog refs
```

Done when:

```text
all commands support --json
human output is stable
source selector works
dirty snapshot warning appears
```

## C6 — Plan integration

Update `orun plan`:

```text
resolve source snapshot
refresh/load catalog snapshot
compile plan from catalog manifests
write revision under catalog snapshot
write global revision index
preserve compatibility behavior
```

Done when:

```text
orun plan creates source/catalog/revision hierarchy
plan.metadata includes source and catalog fields
orun get plans still works
orun describe revision latest shows source/catalog
```

## C7 — Run integration

Update `orun run`:

```text
resolve revision through global index
load parent source/catalog
create execution under revision
emit component execution events
update component execution indexes
```

Done when:

```text
orun run creates execution under catalog-owned revision
orun status works
orun logs works
orun catalog history <component> shows execution
```

## C8 — Diff and history

Implement:

```text
catalog diff main/current
component diff
graph diff
component history filters
```

Done when:

```text
orun catalog diff --base main --head current
orun catalog history api-edge --trigger ... --profile ...
```

## C9 — Remote sync seam

Add interface only:

```text
internal/catalogsync.Syncer
NoopSyncer
SyncPayload models
```

Done when:

```text
orun catalog refresh --sync reports “remote sync not configured”
no networking required
models match future SaaS payload shape
```

---

# 22. Test plan

## Unit coverage targets

```text
internal/sourcectx       >= 90%
internal/catalogmodel    >= 90%
internal/catalogresolve  >= 90%
internal/catalogstore    >= 90%
internal/catalogdiff     >= 85%
```

The earlier state design uses explicit package coverage gates and property tests for core state packages; follow the same style here. 

## Property tests

```text
SourceSnapshotKey stability
CatalogHash stability
ComponentManifestHash stability
DirtyHash ignores non-catalog files
CatalogGraph edge roundtrip
ComponentKey sanitizer
Path helper validity
```

## E2E test

```text
1. Create temp workspace with intent.yaml and two component.yaml files.
2. Run orun catalog refresh.
3. Assert source.json exists.
4. Assert catalog.json exists.
5. Assert component manifests exist.
6. Assert refs/catalogs/current.json exists.
7. Run orun catalog list.
8. Run orun catalog describe api-edge.
9. Run orun plan.
10. Assert revision is written under source/catalog.
11. Run orun run --dry-run.
12. Assert execution is written under revision.
13. Run orun catalog history api-edge.
14. Assert execution appears.
15. Modify component.yaml.
16. Run orun catalog refresh.
17. Assert new dirty/source/catalog snapshot.
18. Run orun catalog diff --base main --head current.
```

## Compatibility tests

```text
orun plan still works without component catalog block
orun run <legacy hash> still works
orun status still falls back to legacy execution paths
orun describe revision latest works with old Phase 1 layout
catalog commands fail gracefully when no catalog exists
```

---

# 23. Correctness invariants

Use these as non-negotiable implementation laws:

```text
1. Every CatalogSnapshot belongs to exactly one SourceSnapshot.

2. Every ComponentManifest belongs to exactly one CatalogSnapshot.

3. Every PlanRevision belongs to exactly one CatalogSnapshot.

4. Every ExecutionRun belongs to exactly one PlanRevision.

5. Every ExecutionRun is therefore traceable to one CatalogSnapshot and one SourceSnapshot.

6. refs/catalogs/main.json is the canonical SaaS source of truth.

7. Non-main refs are preview unless configured otherwise.

8. Dirty worktree snapshots are local-only by default.

9. Snapshots are immutable.

10. Refs are mutable pointers.

11. Indexes are rebuildable.

12. ComponentManifest is generated; component.yaml is authored intent.

13. Field provenance must be available for inherited/inferred values.

14. Global indexes allow lookup without knowing the source/catalog path.

15. Existing plan/run/status/logs behavior must continue to work.
```

---

# 24. Open questions to record in the spec

```text
Q1. Should catalog snapshots be written automatically on every `orun status` if missing?
Default: no. Only plan/run/catalog refresh should write.

Q2. Should dirty workspace snapshots include README content hash?
Default: yes, only if README inference is enabled.

Q3. Should component owner be required by default?
Default: warn locally, error in --strict.

Q4. Should component dependency cycles fail catalog refresh?
Default: warn unless dependency type is “deploy-order”.

Q5. Should catalog refs be global or repo-scoped under .orun?
Default: global under .orun for local repo; remote adds org/project/repo prefix.

Q6. Should `orun plan --no-catalog-refresh` allow stale catalog?
Default: yes, but print warning unless --quiet.

Q7. Should SaaS accept branch snapshots from any branch?
Default: yes as preview, but canonical update only from protected main.
```

---

# 25. Best implementation approach

Build it in this order:

```text
sourcectx
  → catalogmodel
  → catalogresolve
  → catalogstore
  → catalog CLI
  → plan integration
  → run/history integration
  → sync seam
```

Do **not** start with SaaS. Do **not** start with TUI. Do **not** export to Backstage/Datadog yet.

The best first implementation target is:

```bash
orun catalog refresh
orun catalog list
orun catalog describe api-edge
```

Once those are stable, wire `orun plan` and `orun run`.

---

# Final model

```text
component.yaml
  authored by developer

SourceSnapshot
  exact git/worktree state

CatalogSnapshot
  resolved catalog for that source state

ComponentManifest
  full inherited/inferred component definition

PlanRevision
  plan created against that catalog

ExecutionRun
  execution of that plan

ComponentHistory
  query path from catalog page to execution history

SaaS canonical catalog
  clean main branch only

SaaS preview catalog
  PRs, branches, optionally dirty snapshots
```

This gives Orun a clean path from CLI workflow engine to full software catalog platform. The important architectural move is that **catalog is no longer metadata attached to a run; it is the parent state that owns operational history**.

