# Orun Workspace Overview — CLI half

> **Cross-repo spec.** The platform epic and the **normative shared model** live
> in **`sourceplane/orun-cloud`** (`specs/epics/saas-workspace-overview/`, model
> in `model.md`). This file is the **`sourceplane/orun` (CLI/engine, Go)** half:
> the `intent.yaml` surface and the catalog-snapshot changes that feed the
> Workspace Overview. Keep it in sync with the orun-cloud epic; the ownership
> split is in §5.
>
> **2026-07-01 architecture review:** see `architecture-review.md` (this dir) for
> the CLI-half corrections grounded against current code — the `Repo` ref should
> not be minted from the un-normalized `CatalogSnapshot.Repo`; "read at HEAD" is
> really "read the working tree" (make the pin real); adding `Repo`/`Product` is
> emit-path + graph work, not an `allEntityKinds` poke; and docs ride the existing
> `blob` closure (no new object kind). The normative pass is in orun-cloud's
> `architecture-review.md`.

| | |
|---|---|
| **Status** | Proposed (design complete; no code landed) |
| **Repos** | `sourceplane/orun` (CLI, Go — this half) · `sourceplane/orun-cloud` (platform, TS) |
| **Target branch** | `claude/orun-workspace-overview-design-qonyiv` |
| **Pairs with** | orun-cloud WO4 (projection + `state.repo_facet` + `doc_ref`; docs ride the existing `blob` kind), `specs/orun-service-catalog` (the entity model this extends), `specs/oidc-ci-tenancy` (the org/project push scope this reuses) |
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
- **Object kinds today** (server CHECK, since migration `250_state_refs`): `plan |
  catalog-snapshot | composition-lock | artifact-manifest | blob | tree`. Docs ride
  the existing `blob` kind — **nothing net-new at the object-kind layer**.

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

### 3b. Declared `Repo` kind (`Product` deferred)

> **Revised 2026-07-02** (implementation): ship the **`Repo`** kind only. The
> `Repo` ref is the repo-local entity key **`<namespace>/<repo>/<name>`**
> (`FormatEntityKey`), consistent with every other derived entity — **not** a
> cloud project id (grep-confirmed: no project/workspace id exists at resolve
> time; `orun plan` runs offline) and **not** the un-normalized
> `CatalogSnapshot.Repo`. The platform joins the repo facet by `source_project_id`
> at projection time, so the ref never needs a cloud id. **`Product` and the
> `products:` block are deferred** to WO6 (spec in `model.md §7`).

One new **declared** top-level block in `internal/model/intent.go`, resolved into
an entity in the snapshot:

```yaml
repo:                              # singular — self-describes THIS repo (one per snapshot)
  displayName: Lumen Platform
  owner: group:platform
  docs: { overview: docs/overview.md }
  links: [ { title: Runbook, url: https://… } ]
  tags: [saas, baseline]
```

| Kind | Ref | Scope | Merges across repos? |
|------|-----|-------|----------------------|
| `Repo` | `<namespace>/<repo>/<name>` (`FormatEntityKey`, repo-local) — **not** a cloud project id (none exists at resolve time) and **not** the un-normalized `CatalogSnapshot.Repo`; the platform joins by `source_project_id` | repo-scoped, one per snapshot | No |
| `Product` *(WO6)* | `product:<namespace>/<name>` | namespace-scoped | **Yes** — like `System`; deferred |

Add `EntityKindRepo` (constant + `allEntityKinds`), `RepoSpec` (`overview` ref,
`owner`, `links`, `tags`, derived `members`), bump `CatalogSummary`, and emit
`entities/Repo/*.json`. Note the real cost: `IsEntityKind`/`--kind` validation
*do* inherit the array, but a kind that carries relations also needs an **emit
path** (System/Domain are *derived*, so there is nothing to reuse) and graph
wiring in `internal/catalogresolve/graph.go` `buildGraphs()` — it is not a
one-line array poke.

### 3c. Docs travel as content-addressed blobs (read at the pinned commit)

During resolution, for each entity carrying `docs.overview`:

1. read the file bytes **at the commit the head is advanced at** — a git object,
   **not** the working tree (see the invariant below), `digest = sha256(bytes)`;
2. add the blob to the object closure;
3. set the entity's `docs.overview = { path, ref, sha, digest }` (a **reference**
   with the content address, not the bytes inline).

The existing `objremote.Sync` then uploads the blob via set-difference with
header `Orun-Object-Kind: blob`. Because the doc is in the closure the catalog head
pins, it is **point-in-time-consistent** with the snapshot and re-pushing an
unchanged doc is a no-op. Default: **the single `overview` file per entity**; a
`techdocs` *tree* is opt-in and size-capped so nobody mirrors a big folder into
state. Docs ride the **existing `blob` kind** — no new object kind, no CHECK
migration; reachability GC reclaims superseded doc blobs like any other snapshot
object (`model.md §0`).

**Pin the bytes to the commit, not the working tree.** The resolver reads the
working tree today; on the autopush path (clean default branch) that equals HEAD,
but `plan --push-catalog` can run on a dirty tree. So when walking `docs.overview`
into the closure, read the bytes from the git object at the resolved commit, or
**refuse to attach doc objects on a dirty tree** (the autopush gate) and log why —
otherwise the pushed bytes can diverge from the sha the provenance line advertises
(`model.md §3a`).

This preserves the **"any git remote, no GitHub App"** invariant
(`orun-cloud/specs/components/18-state.md`): the CLI reads the repo and pushes;
nothing here depends on a provider API.

## 4. What does NOT change

- No new wire call — the `Repo` entity and `doc` blobs ride the existing
  `catalog-snapshot` push (`objremote.Sync` + `AdvanceCatalogHead`).
- No change to `run` — only `plan`/`catalog push` resolve and push.
- No change to scope resolution (`workspace|org` + `project`) or auth.
- No provider integration — docs are pushed bytes, never fetched.
- No console-authored content on the platform side (no `override_overview`) and no
  `/overview` endpoint — the platform assembles the page at the read edge.

## 5. Ownership split (cross-repo)

| Concern | Owner |
|---------|-------|
| `docs.overview` on the shared docs struct; declared `repo` block + `Repo` kind (ref = repo-local `<namespace>/<repo>/<name>`); walking docs into the closure as content-addressed **blobs** read at the pinned commit; `doc_ref={path,ref,sha,digest}` | **`sourceplane/orun`** (this spec, WO3) |
| Projecting `Repo`→`state.repo_facet`, `doc_ref` onto entities; scoped read-doc-by-digest; the read-edge-assembled Overview (no `/overview` endpoint); console render. **No object-kind change** — docs ride the existing `blob` kind | **`sourceplane/orun-cloud`** (WO2, WO4–WO5) |
| `Product` kind + `products:` block + explicit primary selection | **both**, **deferred** to WO6 |
| The normative model (kinds, refs, `doc_ref` shape, state tables, push flow) | **`sourceplane/orun-cloud`** `model.md` (shared; this spec conforms) |

## 6. Implementation

See `implementation-plan.md` (WO3, step-by-step with "done when").
