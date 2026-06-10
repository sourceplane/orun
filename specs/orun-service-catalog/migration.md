# Migration

> The catalog graduates `orun.io/v1alpha1 → orun.io/v1` **additively and never
> destructively**. Already-persisted L1 blobs are immutable per snapshot and are
> **up-converted lazily on read**, never rewritten in place. Authored files
> (`component.yaml` / `composition.yaml`) migrate via an **opt-in codemod**
> (`orun catalog migrate` / `orun compositions migrate`), with legacy forms
> accepted-with-warning through a deprecation window. This doc is the
> authoritative old→new field mapping and the compatibility contract every
> SC11 PR is verified against. "MUST / SHOULD / MAY" carry RFC 2119 weight.
> Schemas are in `data-model.md`; composition authoring in `compositions.md`.

## 1. Principles

The posture matches the predecessor migration docs
(`archive/orun-component-catalog/compatibility-and-migration.md`,
`orun-object-model/compatibility-and-migration.md`): **additive, never
destructive**. Concretely:

1. **No in-place data migration (S-3, MUST).** L1 entity blobs are immutable and
   content-addressed per snapshot. An older snapshot keeps its `v1alpha1` schema
   on disk **forever**; the resolver converts it to the `v1` envelope *in memory*
   when read (k8s-style conversion, `design.md` §8). No tool ever rewrites a
   persisted blob's bytes — that would change its id (`data-model.md` §10).
2. **Re-resolve, don't rewrite (MUST).** When authored sources move to `v1`, the
   resolver re-resolves them into the new envelope and writes a *new* catalog
   tree. The one-time catalog-id move (S-1) is absorbed by content addressing
   exactly as the CS1 `Path` change was (§8).
3. **Two distinct audiences.** Migration is two separate, non-coupled flows:
   - **(a) Authored files in a repo** — `component.yaml` / `composition.yaml`.
     Migrated by an **opt-in codemod** (`orun * migrate --write`, §5). Legacy
     authored forms keep resolving with a deprecation warning during the window
     (§7).
   - **(b) Already-persisted catalog blobs** — `entities/**`, `relations.json`,
     refs in the object graph. **Auto up-converted on read** (§2); never touched
     by a codemod, never a user action.
4. **In-repo examples migrate (SC10).** `examples/**` are migrated as authored
   files via the codemod, dogfooding the path before it ships to the world; see
   `examples.md`.

This spec never adds a step that deletes or rewrites a user's existing data.

## 2. Lazy `v1alpha1 → v1` up-conversion on read (MUST)

The conversion seam is stood up in **SC0** (types + a no-op converter) and made
**total + tested** in **SC11**.

| Property | Contract |
|----------|----------|
| Trigger | Any read of an L1 blob whose `apiVersion` ≠ `orun.io/v1` (`objcatalog`, `catalog *`, `internal/affected`). |
| Direction | Forward only (`v1alpha1 → v1`). There is no down-conversion. |
| Effect | In-memory only. The persisted blob is **byte-identical** before and after; its id is unchanged. |
| Totality | The converter MUST handle **every** `v1alpha1` envelope without error; an unconvertible blob is a defect, not a runtime fallback. |
| Round-trip | Up-conversion MUST be tested by a property: `convert(read(blob))` equals the result of resolving the equivalent `v1` source (`test-plan.md` parity gate). |
| Unknown fields | Unknown `x-*` extensions and `annotations` are preserved verbatim through conversion (`data-model.md` §8); never dropped (S-6). |

Up-conversion is transparent to every read command — no flag, no surface change
(`cli-surface.md` §8). Multiple envelope versions coexisting on disk is the
expected steady state (S-3), not an error.

## 3. Component old→new field mapping (MUST)

The authored `component.yaml` stays **terse** — convention over configuration
(README). Most new envelope fields are **DERIVED, not authored** (`owner` from
CODEOWNERS, `relations` from the resolver, `runtime` from repo detection,
`maturity` from scorecards). The table below maps the *resolved* shape; the
"Authored?" column flags the few fields an author still writes.

Old = today's flat `nodes.ComponentManifest` / authored `component.yaml`
(`catalogmodel.ComponentYAMLSpec`, `internal/catalogmodel/component_yaml.go`).
New = the entity envelope (`data-model.md` §2).

| Old (flat manifest / `component.yaml`) | New (envelope) | Authored? | Notes |
|----------------------------------------|----------------|-----------|-------|
| `metadata.owner` (bare string) | `ownership.owner` + `ownership.source: authored` | rarely | Authored value wins; absent → CODEOWNERS-derived with `source: CODEOWNERS` (§ derivation). |
| `metadata.maintainers` / `contacts` | `ownership.additionalOwners` / `ownership.contacts` | rarely | Contact objects gain a typed `type`/`value` shape. |
| `spec.lifecycle` (string) | `lifecycle.stage` | SHOULD | Enum `experimental\|production\|deprecated\|retired`; defaults `experimental`. |
| `spec.tier` | `lifecycle.tier` | MAY | Criticality; drives scorecard strictness. |
| (none) | `lifecycle.maturity` | **no** | DENORMALIZED from L2 scorecards, recomputed every resolve (CR-1). Never authored. |
| `spec.system` | `spec.system` **+** `relations[partOf → System]` | yes | Stays in `spec`; **also** produces a relation. |
| `spec.domain` | `spec.domain` **+** `relations[partOf → Domain]` | yes | Same dual treatment. |
| `spec.dependsOn` / `dependencies.components` | `relations[dependsOn → Component]` | yes | `optional` / `include` (`always\|if-selected`) carry over verbatim (`data-model.md` §3). |
| `dependencies.apis` (consumed) | `contracts.consumes[]` + `relations[consumesApi]` | yes | API becomes first-class. |
| `spec.providesApis` | `contracts.provides[]` + `relations[providesApi]` | yes | Gains `definition`/`ref`/`visibility`/`stability`. |
| `dependencies.resources` | `relations[dependsOn → Resource]` | yes | Resource becomes first-class (SC3). |
| top-level `kind` + duplicated `type` | `kind` (envelope) + `spec.type` only | yes | The duplicated top-level `type` is dropped to `spec.type`; one source of truth. |
| `spec.runtime` (inferred) | `spec.runtime` | **no** | Inference unchanged; still repo-detected. |
| `spec.composition` / `parameters` / `environments` | `spec.composition` / `spec.parameters` / `spec.environments` | yes | Unchanged in `spec` (`data-model.md` §4). |
| `provenance` | `provenance` | **no** | Unchanged; stays hash-excluded (`data-model.md` §10). |

### 3.1 Before / after (authored `component.yaml`)

```yaml
# BEFORE — orun.io/v1alpha1 (authored)
apiVersion: orun.io/v1alpha1
kind: Component
metadata:
  name: identity-worker
  owner: platform-edge
spec:
  type: cloudflare-worker
  lifecycle: production
  system: identity
  providesApis: [identity-api]
  dependsOn:
    - { component: auth-svc, include: always }
```

```yaml
# AFTER — orun.io/v1 (authored; terse — owner now derives from CODEOWNERS)
apiVersion: orun.io/v1
kind: Component
metadata:
  name: identity-worker
spec:
  type: cloudflare-worker
  system: identity                 # still here; also emits partOf → System
lifecycle:
  stage: production
contracts:
  provides: [{ api: identity-api, definition: openapi, ref: openapi/identity.yaml }]
  consumes: [{ api: auth-api }]
# dependsOn → relations[] (resolver-derived); ownership.owner ← CODEOWNERS
```

The resolver fills `ownership`, `relations`, `integrations`, and `runtime` — the
author wrote less, and the resolved envelope is richer.

## 4. EntityKind: `Owner → Group` rename (MUST)

`catalogmodel.EntityKind` (`internal/catalogmodel/entity_ref.go`) renames the
`Owner` kind to **`Group`** (`data-model.md` §2.1). To stay non-destructive:

- **New writes** use `kind: Group` and `entities/Group/<name>.json`.
- **Read-time alias (MUST).** The up-converter maps a persisted `Owner` node
  (and `owns`/`ownedBy` edges whose endpoint kind is `Owner`) to `Group` on read,
  so an old `owners` graph and old `Owner` nodes still resolve. The alias lives
  in the §2 conversion path; no on-disk blob is rewritten.
- Owner *keys* (`group:*` / `user:*`) are unchanged; only the kind label moves.

## 5. Composition old→new mapping (MUST)

Authoring evolves from `Stack` / `CompositionPackage` / `CompositionDocument`
(`internal/model/composition.go`) to the `kind: Composition` envelope
(`compositions.md` §8). **The execution core is unchanged in spirit:** `jobs`,
`executionProfiles`, `defaultJob`/`defaultProfile`, and step overrides stay
verbatim in `spec` (today's `CompositionDocumentSpec`), so existing compositions
remain valid and gain the new blocks **additively**.

| Old | New | Notes |
|-----|-----|-------|
| (none) | `metadata` / `ownership` / `lifecycle` | New envelope blocks (additive). `lifecycle.stage` ∈ `stable\|beta\|deprecated`; owner derives from CODEOWNERS. |
| `StackMetadata.version` / `CompositionPackageSpec.version` | `version` (semver) | Layered atop the existing content `ResolvedDigest`; the digest stays the integrity primitive. |
| `spec.parameterSchema` (`ParameterSchema`) | `contract.inputs` | Typed per-field inputs (`compositions.md` §3); the resolver still compiles it to validate a component's `parameters`. |
| (none) | `contract.outputs` / `requires` / `provides` | New, additive (opt-in). |
| `ExecutionProfile.policies` (`ProfilePolicies` booleans) | `policy` block | The flat trio (`requireCleanGitTree`/`requirePinnedTerraformVersion`/`requireApproval`) becomes the declarative inherited `policy` (`compositions.md` §6); compiles to the existing `PromotionGate`. |
| (none) | `contract` / `effects` / `scaffold` | NEW additive opt-in blocks (`compositions.md` §3–§5). Absence changes nothing. |
| `spec.jobs` / `executionProfiles` / overrides | `spec.jobs` / `executionProfiles` / overrides | **Unchanged.** |
| `CompositionLockSource` (`resolvedDigest` + `exports`) | + `version` (semver) + `deprecation` fields | Additive: `compositions.lock.yaml` pins both the integrity digest and the human compatibility version (`compositions.md` §9). No field renamed or removed. |

A legacy composition with none of the new blocks resolves unchanged; it simply
projects an `entities/Composition/<name>.json` with derived ownership and a
default `lifecycle.stage`.

## 6. The two-parser + generated-schema discipline (MUST)

A `component.yaml` is read by **TWO** parsers that MUST accept the same file:

1. The **strict catalog parser** — `catalogmodel.ComponentYAML`
   (`internal/catalogmodel/component_yaml.go`). Its `OpenSchema()` keeps
   plan-engine / legacy keys (`subscribe`, `env`, dependency `environment`/
   `scope`/`condition`, …) **non-fatal** while still type-checking declared
   fields.
2. The **permissive plan-engine parser** — `model.ComponentManifest` /
   `model.Component` (`internal/model/component_tree.go`), which tolerates unknown
   fields by construction.

**The rule (Invariant 5, MUST):** every authored field a migration adds or
renames MUST land in **BOTH** parsers **AND** be regenerated into the JSON schema
via `internal/catalogmodel/schema/gen` + `go generate ./internal/catalogmodel/...`.

> A migration that adds a field but updates only one parser, or forgets the
> schema regen, is a **defect** — not a partial success.

The `make verify-generated` gate (`Makefile`) runs `go generate` and fails CI if
`schema/component-yaml.schema.json` is stale. Every SC11 PR that touches an
authored field MUST leave this gate green. The same discipline applies to
composition authoring across `internal/model/composition.go` and the catalog-side
`Composition` shape (`catalogmodel`/`nodes`, `compositions.md` §10).

## 7. Deprecation windows & timeline

Legacy forms are accepted **read-only via up-conversion** or
**accepted-with-warning** during the window; **removal is deferred to a future
apiVersion**. Nothing in this spec removes a legacy form.

| Legacy form | Status during the window | Removal |
|-------------|--------------------------|---------|
| Persisted `v1alpha1` L1 blobs | Accepted read-only via lazy up-conversion (§2); never rewritten | **Never** (immutable snapshots keep their schema) |
| Authored `apiVersion: orun.io/v1alpha1` files | Accepted; resolve **emits a deprecation warning**; `migrate --check` flags them | Deferred to a future apiVersion |
| `metadata.owner` (bare string) | Accepted-with-warning; up-converted to `ownership.owner` | Deferred |
| top-level duplicated `type` | Accepted-with-warning; folded to `spec.type` | Deferred |
| `EntityKind: Owner` (persisted) | Read-aliased to `Group` (§4) | Deferred |
| Composition `parameterSchema` / `policies` booleans | Accepted-with-warning; up-converted to `contract.inputs` / `policy` | Deferred |

Deprecation warnings fire during resolve and during `migrate` (`cli-surface.md`
§7). The warning text MUST name the field and its `v1` replacement so the codemod
diff is self-explanatory.

## 8. Migration tooling (SC11)

Two opt-in commands (full surface in `cli-surface.md` §7):

- **`orun catalog migrate`** — lints authored `component.yaml` against the `v1`
  envelope and emits a codemod `v1alpha1 → v1`.
  - `--check` — report-only; **exits `5` (diff-exit) when changes are pending**,
    `1` on lint errors. The CI drift gate.
  - `--write` — applies the codemod to the `v1` envelope in place.
  - default — prints the unified diff (no write).
- **`orun compositions migrate`** — the same shape for `composition.yaml` →
  `contract` / `effects` / `policy` authoring and the lock's semver fields.

Both commands flow every field through **both** parsers + the generated schema
(§6) so a codemod can never emit a file one parser rejects. Deprecation warnings
(§7) fire on legacy fields during migration.

## 9. Rollback / safety

- **Up-conversion is total + round-trip-tested** (§2); a conversion defect is
  caught in CI, never silently degraded.
- **Old snapshots remain byte-identical and readable.** Rollback to an earlier
  orun binary is always safe: it reads its own `v1alpha1` blobs unchanged, and
  the object graph is forward-only and content-addressed.
- **The one-time catalog-id move (S-1)** from the envelope reshape is absorbed by
  content addressing: `resolverVersion` increments, the resolve memo misses
  once, ids re-stabilize — the exact pattern documented for the CS1 `Path`
  change (`data-model.md` §10). It is a re-resolve, **not** a data migration.
- **No destructive step anywhere.** There is no `--prune`, no in-place rewrite,
  no blob mutation in this spec. Codemods touch only authored files in the user's
  working tree, under explicit `--write`, producing a reviewable diff.
