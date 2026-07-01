# Orun Workspace Overview — CLI half

> **Cross-repo spec.** The platform epic and the **normative shared model** live
> in **`sourceplane/orun-cloud`** (`specs/epics/saas-workspace-overview/`, model
> in `model.md`). This file is the **`sourceplane/orun` (CLI/engine, Go)** half:
> the `intent.yaml` surface and the catalog-snapshot changes that feed the
> Workspace Overview. Keep it in sync with the orun-cloud epic; the ownership
> split is in §5.

| | |
|---|---|
| **Status** | Proposed (design complete; no code landed) |
| **Repos** | `sourceplane/orun` (CLI, Go — this half) · `sourceplane/orun-cloud` (platform, TS) |
| **Target branch** | `claude/orun-workspace-overview-design-qonyiv` |
| **Pairs with** | orun-cloud WO2b (projection + `state.repo_facet` + `doc` object kind), `specs/orun-service-catalog` (the entity model this extends), `specs/oidc-ci-tenancy` (the org/project push scope this reuses) |
| **Normative model** | orun-cloud `specs/epics/saas-workspace-overview/model.md` |

## 1. Problem

orun-cloud is adding a **Workspace Overview** — a per-workspace landing that
answers "what is this product, is it healthy, what do I do next", with the
narrative **authored in the repo** and rendered by the console. For that to work
without the console reaching back into a git provider, the **CLI must carry two
things it does not carry today**:

1. **Repo and product identity.** `intent.yaml` declares `metadata.{name,
   description, namespace}` and components, but there is **no first-class notion
   of the repo describing itself, nor of a product** the repo (or several repos)
   compose. The catalog snapshot has no `Repo`/`Product` entity.
2. **The overview document, as content.** Docs today are **path pointers**
   (`spec.docs.{techdocs,runbooks,adrs}`) that reference files but never carry
   their bytes. The console cannot render an overview it never receives — and the
   design's decision is to **not** fetch it live from a provider. So the CLI must
   push the referenced doc **bytes** as a content-addressed object.

This spec adds both over the **existing** catalog-push spine — no new wire call,
no provider coupling.

## 2. Current reality (cited)

- **Entity kinds** — `internal/catalogmodel/entity_ref.go`: `Component, API,
  Resource, System, Domain, Group, User, Composition, Environment, Deployment`
  (+ legacy `Owner`→`Group`), validated by the `allEntityKinds` array;
  `IsEntityKind`/`NormalizeEntityKind`/`AllEntityKinds` are array-driven. No
  `Repo`/`Product`.
- **Docs are pointers** — `internal/catalogmodel/entity_envelope.go`
  `EntityDocs = { techdocs, runbooks[], adrs[] }` (shared by every kind);
  authored forms in `component_manifest.go` (`ComponentDocs`) and
  `component_yaml.go` (`ComponentYAMLDocs`). All **file paths**, no bytes.
- **Repo is an identity segment, not an entity** — `entity_envelope.go`
  `EntityIdentity.Repo`, `catalog_snapshot.go` `CatalogSnapshot.Repo` (normalized
  git remote), `keys.go` `FormatEntityKey(namespace, repo, name)`. System/Domain
  are **derived** from component `spec.system`/`spec.domain`
  (`internal/catalogresolve/graph.go`); `Groups`/`Environments`/`Components` are
  **declared** in `internal/model/intent.go` (`Intent.Groups` etc.). No top-level
  `repos`/`products`.
- **Snapshot is a Merkle tree** — `catalog.json` + `entities/<Kind>/*.json` +
  `relations.json`; `CatalogSnapshot.Objects` is a list of `ManifestRef{ key,
  name, path, manifestHash }`.
- **Push is set-difference object sync** — `internal/objremote/objremote.go`
  `Sync(...)` copies only missing blobs; `internal/remotestate/objsync.go` does
  `POST …/state/objects/missing` then `PUT …/state/objects/{digest}` with header
  `Orun-Object-Kind`; `internal/remotestate/catalog.go` `AdvanceCatalogHead`.
  Triggered from `cmd/orun/catalog_push.go` via `cmd/orun/command_plan.go`
  (`--push-catalog`) or `cmd/orun/catalog_autosync.go`
  (`execution.state.autopushCatalog`, clean default branch, digest-changed).
- **Object kinds today** (server CHECK): `plan | catalog-snapshot |
  composition-lock | artifact-manifest`. `doc` is net-new (owned by orun-cloud).

## 3. Design (the CLI changes)

### 3a. `docs.overview` on the shared docs struct

Add `Overview string` to `EntityDocs`, `ComponentDocs`, and `ComponentYAMLDocs`.
Because every kind carries docs through the shared envelope, one field gives every
kind a canonical front-page md pointer:

```yaml
spec:
  docs:
    overview: docs/overview.md   # NEW — path, resolved to a content object (§3c)
    techdocs: docs/              # existing
    runbooks: [ops/runbook.md]   # existing
```

### 3b. Declared `Repo` and `Product` kinds

Two new **declared** top-level blocks in `internal/model/intent.go`, resolved into
entities in the snapshot:

```yaml
repo:                              # singular — self-describes THIS repo (one per snapshot)
  displayName: Lumen Platform
  owner: group:platform
  docs: { overview: docs/overview.md }
  links: [ { title: Runbook, url: https://… } ]
  tags: [saas, baseline]

products:                          # 0..N; a product may span repos
  lumen:
    displayName: Lumen
    description: The Lumen SaaS product
    owner: group:platform
    systems: [identity, billing, metering]
    docs: { overview: docs/product/lumen.md }
```

| Kind | Ref | Scope | Merges across repos? |
|------|-----|-------|----------------------|
| `Repo` | `repo:<remote-host>/<owner>/<name>` (from the normalized `CatalogSnapshot.Repo`, **not** a provider id) | repo-scoped, one per snapshot | No |
| `Product` | `product:<namespace>/<name>` | namespace-scoped | **Yes** — like `System` |

Add `EntityKindRepo`/`EntityKindProduct` (constants + `allEntityKinds`),
`RepoSpec`/`ProductSpec` (`overview` ref, `owner`, `links`, `systems`, derived
`members`), bump `CatalogSummary`, and emit `entities/Repo/*.json` +
`entities/Product/*.json`. `IsEntityKind`/CLI `--kind` validation inherit the new
kinds automatically (array-driven).

### 3c. Docs travel as content-addressed `doc` objects

During resolution, for each entity carrying `docs.overview`:

1. read the file bytes at HEAD, `digest = sha256(bytes)`;
2. add the blob to the object closure;
3. set the entity's `docs.overview = { path, ref, sha, digest }` (a **reference**
   with the content address, not the bytes inline).

The existing `objremote.Sync` then uploads the blob via set-difference with
header `Orun-Object-Kind: doc`. Because the doc is in the closure the catalog head
pins, it is **point-in-time-consistent** with the snapshot and re-pushing an
unchanged doc is a no-op. Default: **the single `overview` file per entity**; a
`techdocs` *tree* is opt-in and size-capped so nobody mirrors a big folder into
state.

This preserves the **"any git remote, no GitHub App"** invariant
(`orun-cloud/specs/components/18-state.md`): the CLI reads the working tree and
pushes; nothing here depends on a provider API.

## 4. What does NOT change

- No new wire call — `Repo`/`Product` entities and `doc` blobs ride the existing
  `catalog-snapshot` push (`objremote.Sync` + `AdvanceCatalogHead`).
- No change to `run` — only `plan`/`catalog push` resolve and push.
- No change to scope resolution (`workspace|org` + `project`) or auth.
- No provider integration — docs are pushed bytes, never fetched.

## 5. Ownership split (cross-repo)

| Concern | Owner |
|---------|-------|
| `docs.overview` on the shared docs struct; declared `repo`/`products`; `Repo`/`Product` kinds; walking docs into the closure as `doc` objects; `doc_ref={path,ref,sha,digest}` | **`sourceplane/orun`** (this spec, WO2a) |
| `doc` value in `state.objects.kind` CHECK; projecting `Repo`→`state.repo_facet`, `Product`→`org_catalog_entities`, `doc_ref` onto entities; `GET …/overview`; console render | **`sourceplane/orun-cloud`** (WO2b–WO5) |
| The normative model (kinds, refs, `doc_ref` shape, state tables, push flow) | **`sourceplane/orun-cloud`** `model.md` (shared; this spec conforms) |

## 6. Implementation

See `implementation-plan.md` (WO2a, step-by-step with "done when").
