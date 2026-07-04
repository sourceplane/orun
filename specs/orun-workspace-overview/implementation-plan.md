# Orun Workspace Overview (CLI half) — Implementation plan

Status: **WO3 Steps 1–5 landed** (revised 2026-07-01 to adopt
`architecture-review.md`; implementation verified 2026-07-04 on `ogpic`). This is
milestone **WO3** of the cross-repo epic (orun-cloud
`specs/epics/saas-workspace-overview/`) — the CLI half, in **Phase 2**. The
orun-cloud landing (**WO2**) ships first and needs nothing here; this milestone
lands behind a page that is already live.

**Follow-up: WO3.1 (below) is ✅ shipped** — the read surface. `EntityView` now
carries the entity envelope (#449); `catalog describe` is kind-aware (#450);
`catalog docs <entity>` prints the overview bytes (#451). `catalog describe
repo:<…>` (Step 5's "done when") now renders. See §3d in `README.md`.

Scope for WO3: the **`Repo`** kind, `docs.overview`, and doc **blobs** (docs ride
the existing `blob` object kind — no new kind). **`Product` is deferred to WO6**
(`model.md §7`). The added snapshot fields are additive — an older orun-cloud
ignores them until WO4 projects them.

## Step 1 — `docs.overview` on the shared docs struct

- Add `Overview string \`json:"overview,omitempty"\`` to:
  - `internal/catalogmodel/entity_envelope.go` → `EntityDocs`
  - `internal/catalogmodel/component_manifest.go` → `ComponentDocs`
  - `internal/catalogmodel/component_yaml.go` → `ComponentYAMLDocs`
- Carry it through the resolve path that maps `component.yaml spec.docs` →
  `ComponentManifest.Docs` → `EntityEnvelope` (`internal/catalogresolve/assemble.go`
  passes docs through verbatim today — extend it).

**Done when:** a `component.yaml` with `spec.docs.overview` round-trips into the
entity envelope; existing docs fields are unchanged.

## Step 2 — the `Repo` kind (register + emit + relate)

This is **not** a one-line array change — budget three sites:

- **Register:** `internal/catalogmodel/entity_ref.go` — add `EntityKindRepo = "Repo"`
  and insert into `allEntityKinds` (sorted). `IsEntityKind`/`NormalizeEntityKind`/
  `AllEntityKinds` and `--kind` validation inherit it.
- **Model the spec:** `internal/catalogmodel/entity_envelope.go` — add `RepoSpec`
  (`Overview`, `Owner`, `Links`, `Tags`, derived `Members`).
  `internal/catalogmodel/catalog_snapshot.go` — add `Repos` to `CatalogSummary`.
- **Emit:** the resolver must **emit** exactly one `entities/Repo/<name>.json` from
  the `repo:` block. `System`/`Domain` are *derived* from component specs
  (`internal/catalogresolve/graph.go`), so there is **no existing emit path for a
  declared top-level entity** to reuse — this is new resolver code. Wire any `Repo`
  relations (e.g. owner → `Group`) into `buildGraphs()` (the graph types are
  hardcoded; a new relation needs a new builder branch).

**Ref:** the `Repo` entity key is the repo-local `FormatEntityKey(namespace,
repo, name)` = `<namespace>/<repo>/<name>` (name defaults from `metadata.name`,
else the repo segment), consistent with System/Domain keys. **No cloud project
id exists at resolve time** (grep-confirmed; `orun plan` is offline), so the ref
must not embed one; the platform joins the repo facet by `source_project_id` at
projection time. Do **not** invent a remote-normalization here.

**Done when:** `AllEntityKinds()` includes `Repo`; a repo with a `repo:` block
produces exactly one `entities/Repo/<name>.json` with a repo-local ref and its
docs.overview/owner/links carried on the entity; `orun catalog list --kind Repo`
validates.

## Step 3 — declare `repo` in intent

- `internal/model/intent.go`: add a top-level `Repo *Repo` to `Intent`, plus a
  shared `Docs` type with `Overview` (mirroring `EntityDocs`).
- Default `displayName`/`description` from `metadata` when omitted.
- **Do not** add `Products`/`products` — deferred to WO6.

**Done when:** a repo with a `repo:` block parses and defaults correctly; a repo
without one is unchanged (no `Repo` entity emitted).

## Step 4 — walk `docs.overview` into the closure, at the pinned commit

- In the snapshot writer / resolve step, for each entity with `docs.overview`:
  read the file bytes **from the git object at the resolved commit** (not the
  working tree) — or, if reading the working tree, **refuse to attach doc objects
  when the tree is dirty** (the same gate `catalog_autosync.go` already enforces
  for autopush) and log why. Compute `digest`, add the blob to the closure, and
  rewrite the entity's `docs.overview` to `{ path, ref, sha, digest }`.
  Rationale: `plan --push-catalog` can run on a dirty tree, and the pushed bytes
  must match the sha the provenance line advertises (`model.md §3a`).
- Default to the single `overview` file per entity. A `techdocs` *tree* is opt-in
  behind a flag/intent setting, with a per-object and per-closure byte cap; when a
  tree is truncated, print a clear notice (never silently drop).
- Ensure the blobs are part of the closure `objremote.Sync` walks (they must be
  reachable from the snapshot root so `Walk()` discovers them — this is explicit
  linking, not automatic) so they upload via `POST …/objects/missing` →
  `PUT …/objects/{digest}` with `Orun-Object-Kind: blob`. The 25 MiB single-shot /
  multipart split (`internal/remotestate/objsync.go`) already covers doc blobs.

**Done when:** `orun catalog push` (or `plan --push-catalog`) on a repo with
`docs.overview` uploads the doc blob once; a re-push with an unchanged doc uploads
nothing (set-difference no-op); the entity JSON carries `doc_ref.digest`; a dirty
tree does not push bytes that mismatch the pinned sha.

## Step 5 — tests

- `internal/catalogmodel`: `Repo` kind registration, **ref derivation from the
  repo-local key (assert the ref, not just "a ref exists"), docs-overview round-trip.
- `internal/model`: parse the `repo:` block; assert `products:` is not consumed.
- `internal/catalogresolve`: the `Repo` emit path + owner relation in the graph.
- `cmd/orun`: an e2e mirroring `command_plan_pushcatalog_test.go` — push a repo
  with a `repo:` block + `docs.overview`, assert the doc blob is in the
  closure, the head advances, an unchanged re-push is a no-op, and a dirty-tree
  push either reads from the commit or refuses the doc object.

**Done when:** the above pass. (The `orun catalog describe repo:<…>` render is
**moved to WO3.1** — `describe` is not yet kind-aware, so it cannot be a WO3
gate; the write-path tests above stand on their own.)

## Milestone WO3.1 — entity read surface + overview render (✅ SHIPPED — #449/#450/#451)

The gap found on 2026-07-04: WO3 resolves and pushes the `Repo` entity + overview
blob, but nothing surfaces them locally. Design in `README.md §3d`; three steps,
smallest first.

### 3.1a — enrich the entity read view (enabling)

- `internal/objcatalog`: extend `EntityView` with `DisplayName`, `Description`,
  `Owner`, `Tags []string`, `Links []map[string]any`, `Docs map[string]any`;
  populate them in `readEntities` from the decoded `nodes.Entity`
  (`Metadata`/`Ownership`/`Links`/`Docs`). Keep the existing fields.

**Done when:** loading the `ogpic` catalog yields a `Repo` `EntityView` whose
`Docs["overview"]` carries `{path, sha, digest}`, `Owner`/`Tags`/`Links` are
populated, and existing consumers (`list --kind`) still compile/pass.

### 3.1b — make `catalog describe` kind-aware

- `cmd/orun/catalog_describe.go`: add `selectObjEntity(view, kind, key)` and route
  a `<kind>:<key>` arg (or `--kind != Component`) to it; keep `selectObjComponent`
  as the default (no-prefix) path so `describe api-edge` is unchanged.
- Accept `describe repo:<key>`, `describe --kind Repo <name|key>`, and bare
  `describe repo` (resolve the single `Repo` in the snapshot).
- Render entity envelope sections (metadata/ownership/links/docs/relations/
  members); the `docs.overview` line prints `path` + `digest`. `--json` emits the
  entity envelope.
- Exit codes per `orun-service-catalog/cli-surface.md §2`: `4` ambiguous across
  kinds (list `kind/entityKey` candidates), `6` entity absent.

**Done when:** `orun catalog describe repo:sourceplane/ogpic/ogpic` (and bare
`describe repo`) renders the entity with its overview reference; `describe
api-edge` is byte-unchanged; ambiguity/absent exit 4/6.

### 3.1c — render the overview bytes

- Add `orun catalog docs <entity> [name]` (default `overview`): resolve
  `docs.<name>.digest` off the entity, `Get` the closure `blob` via the
  `objcatalog` store, print the markdown (text) or `--json {path, sha, digest,
  content}`. Exit `6` when the entity or the named doc is absent.

**Done when:** `orun catalog docs repo:sourceplane/ogpic/ogpic` prints
`docs/overview.md`'s bytes and the digest matches the `doc_ref.digest` in the
snapshot (local preview == what orun-cloud renders).

### 3.1 tests

- `internal/objcatalog`: `EntityView` carries docs/links/owner/tags for a `Repo`.
- `cmd/orun`: `describe repo:<key>`, bare `describe repo`, `--kind Repo`, the
  cross-kind ambiguity (exit 4) and absent (exit 6) paths; `describe api-edge`
  golden unchanged; `catalog docs` digest-parity.

## Sequencing & compatibility

- **Ships behind WO2.** The orun-cloud landing needs nothing here; this milestone
  is not on the critical path to the front-door value.
- Additive: snapshots from this CLI against an older orun-cloud carry the extra
  `Repo` entity + doc blobs harmlessly (an older orun-cloud already accepts `blob`
  objects; unreferenced doc blobs are inert until WO4 projects them).
- **No object-kind coordination:** docs ride the existing `blob` kind (legal since
  migration `250_state_refs`), so there is no CHECK migration and no WO3↔WO4
  ordering — this CLI can push doc blobs before WO4 lands. The doc push still rides
  the normal publish path (clean default branch, best-effort) so it never fails a
  plan.
- **`Product` is out of scope** for WO3; when WO6 is scoped, `products:` +
  `EntityKindProduct` + `ProductSpec` + the `Product` emit/merge path land then.
