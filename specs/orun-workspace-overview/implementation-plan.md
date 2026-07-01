# Orun Workspace Overview (CLI half) — Implementation plan

Status: Draft. This is milestone **WO2a** of the cross-repo epic
(orun-cloud `specs/epics/saas-workspace-overview/`). It is self-contained on the
CLI side and can land before the platform half (WO2b) — the added snapshot fields
are additive, so an older orun-cloud simply ignores them until WO2b projects them.

## Step 1 — `docs.overview` on the shared docs struct

- Add `Overview string \`json:"overview,omitempty"\`` to:
  - `internal/catalogmodel/entity_envelope.go` → `EntityDocs`
  - `internal/catalogmodel/component_manifest.go` → `ComponentDocs`
  - `internal/catalogmodel/component_yaml.go` → `ComponentYAMLDocs`
- Carry it through the resolve path that maps `component.yaml spec.docs` →
  `ComponentManifest.Docs` → `EntityEnvelope`.

**Done when:** a `component.yaml` with `spec.docs.overview` round-trips into the
entity envelope; existing docs fields are unchanged.

## Step 2 — `Repo` / `Product` kinds

- `internal/catalogmodel/entity_ref.go`: add `EntityKindRepo = "Repo"`,
  `EntityKindProduct = "Product"`; insert into `allEntityKinds` (sorted).
  `IsEntityKind`/`NormalizeEntityKind`/`AllEntityKinds` inherit them.
- `internal/catalogmodel/entity_envelope.go`: add `RepoSpec` (`Overview`,
  `Owner`, `Links`, `Provider`, `URL`, derived `Members`) and `ProductSpec`
  (`Overview`, `Owner`, `Systems`, derived `Members`).
- `internal/catalogmodel/catalog_snapshot.go`: add `Repos`/`Products` to
  `CatalogSummary`.
- `internal/catalogmodel/keys.go`: `Repo` ref = `repo:<normalized-remote>` from
  `CatalogSnapshot.Repo` (host/owner/name, **not** a provider numeric id);
  `Product` ref = `product:<namespace>/<name>` via the existing typed-prefix path.

**Done when:** `AllEntityKinds()` includes `Repo`/`Product`; `orun catalog list
--kind Repo` and `--kind Product` validate.

## Step 3 — Declare `repo` / `products` in intent

- `internal/model/intent.go`: add top-level `Repo *Repo` and
  `Products map[string]Product` to `Intent`, plus a shared `Docs` type with
  `Overview` (mirroring `EntityDocs`).
- Resolver: emit exactly one `entities/Repo/<name>.json` from the `repo:` block
  (defaulting `displayName`/`description` from `metadata` when omitted); emit one
  `entities/Product/<name>.json` per `products:` entry, with `partOf`/`hasPart`
  relations to the listed `systems`.

**Done when:** a repo with `repo:`/`products:` blocks produces the corresponding
entity JSONs in the snapshot tree with correct refs and relations.

## Step 4 — Walk `docs.overview` into the object closure

- In the snapshot writer / resolve step, for each entity with `docs.overview`:
  read the file bytes at the resolved `ref`, compute `digest`, add the blob to
  the closure, and rewrite the entity's `docs.overview` to
  `{ path, ref, sha, digest }`.
- Default to the single `overview` file per entity. A `techdocs` *tree* is opt-in
  behind a flag/intent setting, with a per-object and per-closure byte cap; when
  a tree is truncated, print a clear notice (never silently drop).
- Ensure the blobs are part of the closure `objremote.Sync` walks so they upload
  via `POST …/objects/missing` → `PUT …/objects/{digest}` with
  `Orun-Object-Kind: doc`.

**Done when:** `orun catalog push` (or `plan --push-catalog`) on a repo with
`docs.overview` uploads the doc blob once; a re-push with an unchanged doc uploads
nothing (set-difference no-op); the entity JSON carries `doc_ref.digest`.

## Step 5 — Tests

- `internal/catalogmodel`: kind registration, ref formatting for `Repo`/`Product`,
  docs-overview round-trip.
- `internal/model`: parse `repo:`/`products:` blocks.
- `cmd/orun`: an e2e mirroring `command_plan_pushcatalog_test.go` — push a repo
  with a `repo:` block + `docs.overview`, assert the `doc` object is in the
  closure, the head advances, and an unchanged re-push is a no-op.

**Done when:** the above pass and `orun catalog describe repo:<…>` /
`product:<…>` render the new entities with their overview reference.

## Sequencing & compatibility

- Additive: snapshots from this CLI against an older orun-cloud carry the extra
  entities/objects harmlessly (unknown kinds are stored as TEXT; unreferenced
  `doc` objects are inert until WO2b adds the CHECK value and the projection).
- Coordinate the `doc` object-kind CHECK (WO2b) so the server accepts
  `Orun-Object-Kind: doc` before this CLI pushes to production; until then,
  gate the doc-object push behind the same publish path (clean default branch,
  best-effort) so it never fails a plan.
