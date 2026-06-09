# Data Model

> Every persisted schema for the software catalog: the shared entity envelope,
> the typed relation graph, per-kind `spec` blocks, the composition manifest, the
> live-plane overlays, scorecard definitions, the extension model, the on-disk
> catalog tree, and the hashing/identity impact. JSON is the on-disk form;
> `lowerCamelCase`, object IDs `"<algo>:<hex>"`, RFC 3339 / Z. "MUST/SHOULD/MAY"
> carry RFC 2119 weight here. Go shapes live in `internal/catalogmodel` and
> `internal/nodes`; this doc is the contract they generate/serialize.

## 1. Conventions

- All L1 blobs are **canonically encoded** (sorted keys, no insignificant
  whitespace) for content addressing, and **pretty-encoded** when materialized for
  reading (`orun objects cat` / `checkout`), exactly as today.
- An **entityKey** is `<namespace>/<repo>/<name>` (per
  `orun-component-catalog/identity-and-keys.md` §4), now paired with `kind` so the
  same name under two kinds is distinct.
- `provenance` is **excluded** from `manifestHash` (per §10 there); all other
  envelope blocks are included.
- `null`/absent: optional blocks are emitted as `null` (today's manifest emits
  `"contacts": null`); readers MUST treat absent and `null` identically.

## 2. The entity envelope (shared by all kinds)

```jsonc
{
  "apiVersion": "orun.io/v1",
  "kind": "Component",                       // EntityKind enum (§2.1)
  "identity": {
    "entityKey": "default/orun/identity-worker",
    "kind": "Component",
    "name": "identity-worker",
    "namespace": "default",
    "repo": "orun",
    "path": "apps/identity-worker/component.yaml"   // source file or dir
  },
  "metadata": {
    "title": "Identity Worker",
    "description": "Identity edge worker",
    "labels":  { "team": "platform", "runtime": "cloudflare" },  // queryable k/v
    "tags":    ["edge", "tier-1"],                               // flat, faceted search
    "annotations": null                                          // untyped escape hatch (§8)
  },
  "ownership": {
    "owner": "group:platform-edge",          // single primary, REQUIRED post-resolve
    "additionalOwners": ["group:sre"],
    "contacts": [ { "type": "slack", "value": "#edge", "primary": true },
                  { "type": "pagerduty", "value": "PXXXX" } ],
    "escalation": "pagerduty:edge-oncall",
    "source": "CODEOWNERS"                    // authored | CODEOWNERS | inherited | unknown
  },
  "lifecycle": {
    "stage": "production",                    // experimental | production | deprecated | retired
    "tier":  "tier-1",                        // criticality; drives scorecard strictness
    "maturity": "gold"                        // bronze|silver|gold — DENORMALIZED from L2 (§6), recomputed
  },
  "spec":     { /* kind-specific — §4 */ },
  "relations": [ /* §3 */ ],
  "contracts": {                              // APIs as first-class, with spec refs
    "provides": [ { "api": "default/orun/identity-api", "definition": "openapi",
                    "ref": "openapi/identity.yaml", "visibility": "public", "stability": "stable" } ],
    "consumes": [ { "api": "default/orun/auth-api" } ]
  },
  "integrations": {                           // typed join-keys (§ derived, rarely authored)
    "datadog":   { "service": "identity-worker", "team": "edge" },
    "pagerduty": { "serviceId": "PXXXX" }
  },
  "docs":  { "techdocs": "docs/", "runbooks": ["docs/runbook.md"], "adrs": [] },
  "links": [ { "title": "Dashboard", "url": "https://...", "icon": "dashboard" } ],
  "provenance": {                             // EXCLUDED from manifestHash
    "inheritedFrom": { "ownership.owner": "systems/identity/component.yaml" },
    "inferredFrom":  { "runtime.languages.javascript": ["apps/identity-worker/package.json"] },
    "manifestHash":  "sha256:…",
    "resolver":      { "orunVersion": "…", "resolverVersion": 2, "schemaVersion": "orun.io/v1" },
    "attestation":   null                     // SC12: { signature, signedBy }
  },
  "extensions": { /* x-<vendor> typed blocks — §8 */ }
}
```

### 2.1 `EntityKind` (promoted from `catalogmodel/entity_ref.go`)
`Component` · `API` · `Resource` · `System` · `Domain` · `Group` · `User` ·
`Composition` · `Environment` (derived) · `Deployment` (derived). The current
constants (`EntityKindComponent`…`EntityKindOwner`) are extended; `Owner` is
renamed to `Group` with a read-time alias (`migration.md` §3).

### 2.2 Field contract (selected)
| Field | Req. | Derived? | Notes |
|-------|------|----------|-------|
| `identity.entityKey` | MUST | resolver | unique within a catalog per `(kind, entityKey)` |
| `ownership.owner` | MUST (post-resolve) | CODEOWNERS | `unknown` allowed only when no claim exists; flagged by a scorecard |
| `ownership.source` | MUST | resolver | provenance of the ownership claim (S-2) |
| `lifecycle.stage` | SHOULD | authored | defaults `experimental` |
| `lifecycle.maturity` | — | L2 sync | recomputed every resolve; never authored (CR-1) |
| `relations` | MUST (may be `[]`) | resolver | bidirectional, sorted (§3) |
| `contracts`, `integrations`, `docs`, `links`, `extensions` | MAY | mixed | absent → `null` |

## 3. The relation graph (`relations.json`)

One blob per catalog, replacing the `graph/` subtree.

```jsonc
{
  "apiVersion": "orun.io/v1",
  "kind": "RelationGraph",
  "edges": [
    { "from": "default/orun/identity-worker", "fromKind": "Component",
      "type": "dependsOn", "to": "default/orun/auth-svc", "toKind": "Component",
      "optional": false, "include": "if-selected" },
    { "from": "default/orun/identity-worker", "fromKind": "Component",
      "type": "ownedBy", "to": "group:platform-edge", "toKind": "Group" }
  ]
}
```

- **Edge types (and inverses):** `ownedBy`/`owns`, `partOf`/`hasPart`,
  `dependsOn`/`dependencyOf`, `providesApi`/`apiProvidedBy`,
  `consumesApi`/`apiConsumedBy`, `runsOn`/`hosts`, `deployedTo`/`hostsDeployment`,
  `composedBy`/`composes`.
- **Storage rule:** the **forward** edge is stored; inverses are materialized by
  the reader (`objcatalog`) so portals never reverse-walk. Determinism: edges
  sorted by `(from, fromKind, type, to)`.
- `optional` and `include` (`"always"|"if-selected"`) carry change-detection
  semantics and MUST be preserved through resolve → `internal/affected` (CV-1).
- Each edge endpoint references an entity also present in `entities/` **or** an
  external owner key (`group:*`/`user:*`) registered in `entities/Group/`.

## 4. Per-kind `spec` blocks

The envelope is identical; only `spec` differs.

- **Component** — `type`, `composition` (`{source, type}`), `parameters`,
  `environments` (`{<env>: {profile, active}}`), `runtime` (inferred
  `{languages, frameworks, infra}`). (This is today's `spec` minus the inlined
  `dependencies`, which moves to `relations`/`contracts`.)
- **API** — `type` (`openapi|asyncapi|grpc|graphql`), `definitionRef` (path into
  the snapshot), `visibility` (`public|internal`), `stability`
  (`experimental|stable|deprecated`), `version`.
- **Resource** — `type` (`datastore|queue|topic|bucket|cache`), `provider`,
  `parameters`.
- **System** / **Domain** — `members` (derived count), free `spec` metadata;
  membership lives in `relations` (`hasPart`).
- **Group** / **User** — `members` / contact info; sourced from CODEOWNERS/IdP.
- **Environment** (derived) — `type` (`dev|staging|production|preview`), `order`,
  `protected`; emitted from component env bindings + execution (SC4).
- **Deployment** (derived) — `component`, `environment`, `revision`,
  `executionId`, `status`, `deployedAt`; emitted from `objrun` (SC4/SC8). A
  `Deployment` is the L1 *record that a deploy happened*; live health is L2 (§6).

## 5. The Composition manifest (`kind: Composition`)

Authored shape and the `effects` producer model are specified in
`compositions.md`; the **resolved node** stored at
`entities/Composition/<name>.json` uses the shared envelope with this `spec`:

```jsonc
{
  "kind": "Composition",
  "identity": { "entityKey": "default/orun/cloudflare-worker", "kind": "Composition", "name": "cloudflare-worker" },
  "lifecycle": { "stage": "stable" },          // stable | beta | deprecated
  "spec": {
    "version": "2.3.0",                         // semver
    "digest": "sha256:…",                       // existing ResolvedDigest
    "source": { "kind": "oci", "ref": "ghcr.io/…", "exports": ["…"] },
    "applies": { "kind": "Component", "types": ["cloudflare-worker"] },
    "contract": { "inputs": {…}, "outputs": [...], "requires": {…}, "provides": {…} },
    "effects":  { "graph": [...], "integrations": [...], "scorecards": {…} },
    "policy":   { "allowedEnvironments": [...], "gates": {…} },
    "extends":  "default/orun/org-base"
  },
  "relations": [ { "type": "composes", "to": "default/orun/identity-worker", "toKind": "Component" } ]
}
```

## 6. The live plane (L2 overlays — NOT in the catalog tree)

Mutable, keyed by `entityKey`, persisted under a live-plane index (sibling to the
catalog refs; exact path in `cli-surface.md`/`implementation-plan.md`). **Never**
content-addressed into an L1 blob (CR-1).

```jsonc
{
  "entityKey": "default/orun/identity-worker",
  "deployments": [ { "environment": "production", "revision": "a4e1409",
                     "executionId": "exec_…", "status": "healthy", "deployedAt": "…" } ],
  "scorecards":  [ { "id": "production-readiness", "score": 0.86, "level": "gold",
                     "checks": [ { "id": "has-owner", "pass": true, "weight": 1 } ],
                     "evaluatedAt": "…" } ],
  "health": { "status": "ok" }, "slo": {…}, "incidents": {…}, "cost": {…},
  "vulnerabilities": { "critical": 0, "high": 2 }
}
```

`deployments`/`health` are projected from `objrun` execution truth (the same
scan/join the cockpit uses for history). `lifecycle.maturity` in the envelope is
the denormalized `max(level)` recomputed at resolve from the latest scorecard.

## 7. Scorecard definitions — extracted to `specs/orun-scorecards/` (v2)

The scorecard engine, the `Scorecard` definition schema, the predicate
vocabulary, and the L2 scorecard overlay are **extracted to a separate v2 epic**,
`specs/orun-scorecards/` (drafted, for later review). This epic carries only the
**foundations** they consume:

- the **live plane** deployments/health view (§6);
- the reserved envelope field **`lifecycle.maturity`** (§2) — emitted `null` in
  v1, recomputed by the v2 scorecard engine;
- the composition **`effects.scorecards.satisfies`** *declaration*
  (`compositions.md` §4.3) — authored and carried into the resolved Composition
  node, but **not evaluated** here.

See `specs/orun-scorecards/data-model.md` for the `Scorecard` schema and the L2
overlay shape, and `specs/orun-scorecards/design.md` for the predicate
vocabulary (locked: a small allowlisted set; CEL as the named upgrade path).

## 8. Extensibility

1. **Well-known fields** — everything above; struct-generated schema; validated.
2. **Typed extensions** — `extensions.x-<vendor>` blocks validated against a
   **registered** schema (the extension registry, SC6). Unknown `x-*` blocks are
   preserved and rendered generically; never dropped on round-trip.
3. **`annotations`** — `map[string]string`, untyped, last resort, never drives
   critical UI.

`apiVersion` graduates `orun.io/v1alpha1 → orun.io/v1`; the resolver up-converts
older blobs on read (`migration.md` §2). New optional fields are additive within
`v1`; removals/renames require a new `apiVersion` + a conversion function.

## 9. On-disk catalog tree (object graph)

```
catalogs/current ─ref→ <catalog tree>
  ├─ blob  catalog.json            # CatalogSnapshot: summary + per-kind counts + resolver
  ├─ tree  entities/
  │         ├─ Component/<name>.json
  │         ├─ API/<name>.json
  │         ├─ Resource/<name>.json
  │         ├─ System/<name>.json
  │         ├─ Domain/<name>.json
  │         ├─ Group/<name>.json
  │         ├─ Composition/<name>.json
  │         ├─ Environment/<name>.json     # derived (SC4)
  │         └─ Deployment/<name>.json      # derived (SC4/SC8)
  ├─ blob  relations.json          # the one typed graph (§3)
  └─ tree  impact/                 # ownership.json + fingerprints/ (unchanged)

# L2 (mutable, sibling — NOT under the catalog tree):
refs/… live-plane/<entityKey>.json   # deployments · scorecards · health (§6)
```

`catalog.json` summary gains `countsByKind: { Component, API, Resource, … }`
(replacing the flat component count). `objplan/catalog.go:mapManifest` becomes
`mapEntity` (kind-aware); `objcatalog` enumerates `entities/<Kind>/`.

## 10. Hashing & identity impact

- **`manifestHash`** = canonical hash of the envelope **minus `provenance`**
  (identical rule to today; the envelope is larger but the rule is unchanged).
- **`catalogHash`** = canonical hash over all `entities/**` blobs + `relations.json`
  + `catalogInputHash` + `resolverVersion` (replaces "manifests + 5 graphs").
- The reshape **moves every catalog id once** (S-1): `resolverVersion`
  increments, the resolve memo misses once, content addressing re-stabilizes —
  the exact pattern documented for the CS1 `Path` change. No data migration; old
  snapshots remain readable and up-convert on read.
- **Reserved for SC12:** a `tenant` segment in the key/ref grammar (S-8) and an
  `attestation` block in `provenance` (already present as `null`).
