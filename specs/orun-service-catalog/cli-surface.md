# CLI Surface

> The CLI deltas this epic introduces: the `catalog` read family becomes
> **kind-aware** (`list`/`describe`/`tree`, the new `graph` and `scorecard`), the
> composition surface gains entity rendering + golden-path scaffolding, and two
> migration commands (`catalog migrate` / `compositions migrate`) carry the repo
> from `v1alpha1 → v1`. These are **deltas** over `orun-component-catalog`/
> `orun-catalog-state`; the envelope conventions (`apiVersion`/`kind`/`data`/
> `warnings`, `lowerCamelCase`, logical paths root-relative to
> `.orun/objectmodel/`) and the selector flags (`--catalog-source`/`--source`/
> `--catalog-snapshot`) are unchanged and assumed.

**House exit codes** (every `catalog` command shares this contract): `0` success ·
`1` validation error · `2` resolver/internal error · `3` state/persistence
failure · `4` resource ambiguity · `5` diff-exit (differences exist) · `6`
resource/index absent. Read commands work transparently on both `v1alpha1` and
`v1` catalogs via lazy up-conversion (§8).

## 1. `orun catalog list` — kind-aware (SC3/SC5)

**Purpose.** Enumerate entities of a chosen kind in the selected catalog.
Today's command lists Components only; it now lists any `EntityKind`, defaulting
to `Component` for back-compat.

**Flags.**

| Flag | Notes |
|------|-------|
| `--kind <Component\|API\|Resource\|System\|Domain\|Group\|Composition\|Environment\|Deployment>` | Which kind to enumerate. **Default `Component`** (existing output unchanged). |
| `--owner <owner>` | Resolved primary `ownership.owner` (CODEOWNERS-derived). |
| `--system` / `--domain` | Membership filters (via `relations`). |
| `--type <t>` | Kind-specific `spec.type`. |
| `--status <s>` | Last-execution status (live entities only). |
| `--json` | Stable machine-readable output. |

**Output.** Text is a fixed-width table; columns adapt to `--kind`. The kind-aware
set adds **KIND**, **OWNER** (from resolved `ownership`, no longer a bare
`metadata.owner` string), and **MATURITY** (the denormalized scorecard level,
§4); `LAST EXEC`/`STATUS` render only for live kinds. `--json` emits
`kind: CatalogListResult` with one row per entity carrying `entityKey`, `kind`,
`name`, `owner`, `ownerSource`, `system`, `maturity`, and (live kinds)
`lastExecutionStatus`. Rows are sorted by `entityKey` (byte-stable).

**Exit codes.** `0` (possibly empty) · `1` invalid selector or unknown `--kind`
· `3` state failure · `6` catalog absent.

## 2. `orun catalog describe <entity>` — full envelope (SC1/SC3/SC5)

**Purpose.** Render one entity's complete envelope. Replaces today's
component-only manifest render.

**Flags.** `--kind <…>` disambiguates the same `name` across kinds (default:
search all kinds, then Components); `--json`; the shared selector flags. The
`<entity>` arg accepts a bare name or a fully-qualified `entityKey`.

**Output.** Sectioned text over every envelope block: `metadata` · `ownership`
(owner, additionalOwners, contacts, escalation, **source** — authored / CODEOWNERS
/ inherited / unknown, S-2) · `lifecycle` (stage, tier, maturity) · `spec`
(kind-specific) · `relations` (typed, inverses materialized) · `contracts`
(provides/consumes APIs) · `integrations` (join-keys) · `docs` · `links` ·
`provenance` (manifestHash, resolver, inheritedFrom/inferredFrom). For **live**
entities the render appends the **L2 plane** — deployments (per environment:
revision, status, deployedAt) and scorecards (level + per-rule results). `--json`
emits `kind: CatalogDescribeResult` with `data.entity` (the envelope) and, for
live entities, `data.live`.

**Exit codes.** `0` · `1` missing arg / invalid selector · `3` state failure ·
**`4` ambiguous name across kinds** (lists candidate `kind/entityKey` pairs;
resolve with `--kind` or a full key) · `6` entity absent.

## 3. `orun catalog tree` / `orun catalog graph` — typed relations (SC2/SC3)

**Purpose.** Render the unified typed relation graph (`relations.json`, the one
graph that replaced the `graph/` subtree and is now also consumed by
`internal/affected`). `tree` keeps its rooted-forest text rendering; new
`graph` is the flat node/edge projection (the canonical `--json` surface).

**Flags** (both commands):

| Flag | Notes |
|------|-------|
| `--kind <…>` | Restrict nodes to one kind (edges to/from other kinds still render as leaves). |
| `--relation <type>` | Filter to one edge type: `dependsOn`, `ownedBy`, `partOf`, `providesApi`, `consumesApi`, `runsOn`, `deployedTo`, `composedBy` (and inverses). |
| `--direction out\|in\|both` | Traversal relative to the optional root (default `out`). |
| `--json` | Stable output. |

**Output.** `tree` text is the indented forest with `→ [relationType]` edge
annotations (`(optional)` preserved). `graph --json` emits
`kind: RelationGraph` with sorted `nodes` (`entityKey`, `kind`, `name`) and
`edges` (`from`, `fromKind`, `type`, `to`, `toKind`, `optional`, `include`).
Edges are deterministic (sorted by `(from, fromKind, type, to)`); inverses are
materialized by the reader.

**Exit codes.** `0` (possibly empty) · `1` invalid selector / unknown
`--direction` / unknown `--relation` · `3` state failure · `6` catalog or graph
absent.

## 4. `orun catalog scorecard` — extracted to `specs/orun-scorecards/` (v2)

The `orun catalog scorecard` command — its flags (`--scorecard`/`--kind`/
`--min-level`/`--failing`/`--json`), output, and exit codes — is specified in the
**`specs/orun-scorecards/` v2 epic** (for later review), since the scorecard
engine itself is extracted there. This epic ships only the scorecard
**foundations** (the live plane, §5; the reserved `lifecycle.maturity`; the
composition `effects.scorecards` declaration) — no scorecard CLI here.

## 5. `orun catalog refresh` — unchanged contract, multi-kind write (SC1–SC4)

**Purpose.** Resolve the current workspace and persist a catalog snapshot. The
**CLI contract is unchanged** (`[--json] [--strict]`, the `CatalogRefreshResult`
envelope); only what it resolves and writes grows.

**Behavior delta.** The resolver now produces the multi-kind entity catalog and
the refresh writes `entities/<Kind>/<name>.json`, `relations.json`, and
`impact/`. It remains **object-model-only** — there is no dual write (the
`catalogstore` era is over, `orun-legacy-retirement`). The text summary and the
envelope `data` gain `countsByKind: { Component, API, Resource, System, Domain,
Group, Composition, Environment, Deployment }` replacing the flat component
count.

**Risk note.** The `data.objectModel` sub-block (`{catalogId, sourceId,
components}`) is now **vestigial** — with the single-store write it duplicates the
top-level fields and its `components` scalar is subsumed by `countsByKind`. It
**could be flattened** into the envelope root (a byte-stable break for golden
tests; cross-ref `risks-and-open-questions.md`, S-1). Retained for now.

**Exit codes.** `0` · `1` validation error (`--strict`) · `2` resolver internal
· `3` persistence failure. (Best-effort sub-steps surface as `warnings[]`.)

## 6. Composition surface (SC7 / SC9)

**`orun compositions list` / `describe <composition>`.** Render the **Composition
entity** (`kind: Composition`, `data-model.md` §5), not just authored YAML: the
shared envelope (`metadata`/`ownership`/`lifecycle`) plus the composition `spec` —
typed `contract` (inputs/outputs/requires/provides), the `effects` producer block
(graph / integrations / scorecards it emits), `lifecycle.stage`
(`stable|beta|deprecated`), and semver `version` + `digest`. `describe` also shows
`composes`/`usedBy` relations. `--json` carries the full envelope.

**`orun compositions scaffold <composition> [--out dir]` — NEW (SC9).**
Golden-path self-service create: materialize starter files + a catalog-valid
`component.yaml` from the composition's `scaffold` template and `contract.inputs`
schema. A top-level **`orun create`** MAY front the same flow for the paved-road
create → build → deploy experience. Flags: `--out <dir>` (default cwd), plus
`contract.inputs` surfaced as `--<input>` flags or prompted. Exit `0` on a
scaffold that resolves cleanly · `1` on contract-input validation failure · `6`
unknown composition.

**`compositions.lock.yaml`.** Gains **semver + deprecation** metadata atop the
existing `ResolvedDigest` (SC7); `orun compositions lock` writes it, and
`describe` surfaces deprecation warnings for pinned-but-deprecated versions.

## 7. Migration commands — NEW (SC11)

Cross-ref `migration.md` (the authoritative old→new field mapping + deprecation
windows).

**`orun catalog migrate`.** Lint authored `component.yaml` files against the new
envelope and emit a codemod diff `v1alpha1 → v1`.

| Flag | Notes |
|------|-------|
| `--write` | Apply the codemod in place (default: print the unified diff). |
| `--check` | Report-only; non-zero if changes are pending (CI gate). |
| `--json` | Stable output (`kind: CatalogMigrateResult`: per-file `lintIssues[]` + `pending` diff stat). |

**`orun compositions migrate`.** The same shape for `composition.yaml` →
`contract`/`effects`/`policy` authoring (and the lock's semver fields).

**Output.** Text: per-file lint findings then a unified codemod diff. Both
commands flow every field through **both** `component.yaml` parsers + the
generated schema (the convention-over-configuration discipline, README §
"Convention over configuration").

**Exit codes.** `0` clean · **`1` on lint errors** · **`5` when `--check` finds
pending changes** (diff-exit) · `2` internal · `3` write failure.

## 8. Lazy up-conversion (read transparency) (SC11)

All **read** commands above (`list`/`describe`/`tree`/`graph`/`scorecard`,
composition render) operate on `v1alpha1` and `v1` catalogs **transparently**:
the resolver up-converts older L1 blobs on read (k8s-style, never destructive in
place — design §8, `migration.md` §2). Immutable snapshots keep their on-disk
schema forever; only the in-memory view is converted. No flag, no surface
change — old catalogs render with `v1` columns.

## 9. Object plumbing (unchanged — documents the new `entities/` path) (SC3)

The object porcelain is **unchanged**; only the tree layout it walks grew an
`entities/<Kind>/` partition (replacing the flat `components/`). Any entity blob
is still viewable through the raw path:

```
orun objects rev-parse catalogs/current        # → <catalog tree id>
orun objects cat <catalog-tree-id>             # → catalog.json + entities/ + relations.json
orun objects ls-tree entities/<Kind>           # → <name>.json blob ids per entity
orun objects cat <blob-id>                      # → the pretty-printed entity envelope
orun objects checkout catalogs/current          # → materialize the whole tree on disk
```

`relations.json` and `catalog.json` are plain blobs in the same tree. No new
commands — documented here only so the `entities/` layout is discoverable.
