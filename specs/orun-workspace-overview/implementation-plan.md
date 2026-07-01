# Orun Workspace Overview (CLI half) — Implementation plan

Status: Draft (revised 2026-07-01 to adopt `architecture-review.md`). This is
milestone **WO3** of the cross-repo epic (orun-cloud
`specs/epics/saas-workspace-overview/`) — the CLI half, in **Phase 2**. The
orun-cloud landing (**WO2**) ships first and needs nothing here; this milestone
lands behind a page that is already live.

Scope for WO3: the **`Repo`** kind, `docs.overview`, and `doc` objects. **`Product`
is deferred to WO6** (`model.md §7`). The added snapshot fields are additive — an
older orun-cloud ignores them until WO4 projects them.

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

**Ref:** mint the `Repo` ref from the **durable project/`ws_` id** — the stable
join key the platform already uses (`model.md §2c`) — **not** from
`CatalogSnapshot.Repo`, which is an un-normalized display string
(`internal/catalogresolve/catalog_snapshot.go` copies `ResolverInputs.Repo`
verbatim). Do **not** invent a remote-normalization here; the `path/ref/sha`
provenance on `doc_ref` carries the human remote for "view source".

**Done when:** `AllEntityKinds()` includes `Repo`; a repo with a `repo:` block
produces exactly one `entities/Repo/<name>.json` with a project-id-derived ref and
correct owner relation; `orun catalog list --kind Repo` validates.

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
  `PUT …/objects/{digest}` with `Orun-Object-Kind: doc`. The 25 MiB single-shot /
  multipart split (`internal/remotestate/objsync.go`) already covers doc blobs.

**Done when:** `orun catalog push` (or `plan --push-catalog`) on a repo with
`docs.overview` uploads the doc blob once; a re-push with an unchanged doc uploads
nothing (set-difference no-op); the entity JSON carries `doc_ref.digest`; a dirty
tree does not push bytes that mismatch the pinned sha.

## Step 5 — tests

- `internal/catalogmodel`: `Repo` kind registration, **ref derivation from the
  project id** (assert the ref, not just "a ref exists"), docs-overview round-trip.
- `internal/model`: parse the `repo:` block; assert `products:` is not consumed.
- `internal/catalogresolve`: the `Repo` emit path + owner relation in the graph.
- `cmd/orun`: an e2e mirroring `command_plan_pushcatalog_test.go` — push a repo
  with a `repo:` block + `docs.overview`, assert the `doc` object is in the
  closure, the head advances, an unchanged re-push is a no-op, and a dirty-tree
  push either reads from the commit or refuses the doc object.

**Done when:** the above pass and `orun catalog describe repo:<…>` renders the new
entity with its overview reference.

## Sequencing & compatibility

- **Ships behind WO2.** The orun-cloud landing needs nothing here; this milestone
  is not on the critical path to the front-door value.
- Additive: snapshots from this CLI against an older orun-cloud carry the extra
  `Repo` entity + `doc` objects harmlessly (unknown kinds stored as TEXT;
  unreferenced `doc` objects inert until WO4 adds the CHECK value + projection).
- Coordinate the `doc` object-kind CHECK: WO4 **reconciles** the
  `state.objects.kind` CHECK (bringing it in line with the real write-time kind
  set) and adds `doc` **before** this CLI pushes `Orun-Object-Kind: doc` to
  production. Until then, gate the doc-object push behind the same publish path
  (clean default branch, best-effort) so it never fails a plan.
- **`Product` is out of scope** for WO3; when WO6 is scoped, `products:` +
  `EntityKindProduct` + `ProductSpec` + the `Product` emit/merge path land then.
