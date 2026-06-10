# Design

> The catalog becomes a typed, multi-kind entity graph with ownership,
> lifecycle, relations, contracts, and a live plane — all *derived*, never
> hand-curated. Compositions become golden-path entities that produce that
> truth. This doc fixes the planes, the entity model, the envelope, the relation
> graph, the live plane, extensibility, provenance, package boundaries,
> invariants, and the sharpness register. Schemas are in `data-model.md`;
> composition detail is in `compositions.md`.

## 1. Problem

1. **The resolved manifest is flat, single-kind, and thin.** Today
   `nodes.ComponentManifest` is `identity/metadata/spec/provenance/type`
   (verified on a live `components/<name>.json` blob). `owner` is a bare string,
   there is no lifecycle/maturity, no first-class API/Resource/System entity, no
   ownership provenance, no contracts, no integration join-keys.
2. **The graph is five untyped sibling trees.** `catalogresolve/graph.go` builds
   `dependencies/systems/apis/resources/owners` as separate
   component-only graphs. A portal needs one typed graph spanning kinds, and the
   change engine (`internal/affected`) already wants exactly that.
3. **There is no live plane.** Deployments, health, and maturity are not
   modelled. orun *owns* this truth (the execution engine) but does not surface
   it as catalog state.
4. **Compositions are write-only execution contracts.** They define how a kind
   of thing is built and deployed, but contribute nothing back to the catalog —
   no ownership, no versioned identity, no declared contract, no derived effects.
5. **Ownership is undiscoverable and drifts.** `metadata.owner` is authored and
   optional; CODEOWNERS (the real source of truth) is **not** consulted by the
   resolver today.

## 2. Goals / non-goals

**Goals**
- A single **entity envelope** shared by every kind, so the portal renders
  Components, APIs, Resources, Systems, Environments, and Compositions
  generically.
- **Multi-kind** promotion of `EntityKind` to first-class resolved entities, plus
  orun-native **Environment** and **Deployment** entities derived from execution.
- **One typed relation graph**, consumed by the change engine and the portal.
- A mutable **live plane** (deployments, health) fed by orun's own execution
  data, strictly outside the immutable manifest. (Scorecards — which consume the
  live plane — are extracted to `specs/orun-scorecards/`, v2.)
- **Compositions as catalogued producers**: a versioned, owned, contract-bearing
  golden path whose `effects` emit entities, integration keys, and scorecard
  signals into the catalog.
- **Convention over configuration**: the developer authors the minimum; the
  resolver derives the rest (ownership from CODEOWNERS, integrations from the
  golden path, relations from the resolver, maturity from scorecards).
- **Non-destructive evolution**: `v1alpha1` manifests up-convert lazily on read;
  examples migrate in-repo; a legacy migration path ships (`migration.md`).

**Non-goals**
- Attestation/signing + multi-tenant federation — designed at a high level (§9)
  but implemented as a **follow-on** (SC12).
- Building the SaaS web UI (the read seam is kept open; build is out of scope).
- The **scorecard engine** — extracted to `specs/orun-scorecards/` (v2); this
  epic keeps only the foundations (§6).
- **Golden-path scaffolding** / `orun create` — extracted to
  `specs/orun-scaffolding/` (v2); only the authored composition `scaffold` block
  stays (`compositions.md` §5).
- Shipping vendor integration *connectors* (the registry defines the seam only).
- The single-env redesign (`specs/archive/orun-env-scoping/`); Environment-as-entity here
  is additive and coordinates with, but does not implement, that epic.

## 3. The three planes (formalized)

| Plane | Artifact | Storage | Mutability | Hash impact |
|-------|----------|---------|------------|-------------|
| **L0 Authored** | `component.yaml`, `composition.yaml` | git | human-edited | input to L1 |
| **L1 Resolved** | entity envelope blobs + `relations.json` + `catalog.json` + `impact/` | object graph (`objects/sha256/**`) under `catalogs/current` | **immutable**, content-addressed | defines `manifestHash`/`catalogHash` |
| **L2 Live** | deployments, health, scorecards, incidents, cost, vulns | mutable indexes / overlays keyed by `entityKey` | mutable | **none** |

**Cardinal rule (CR-1, MUST).** Nothing that changes without a source change may
enter an L1 blob. The manifest already drops the embedded `source` block
(parentage is structural); L2 already keeps execution history out of the
manifest. This spec extends the rule: scorecards, deployments, and live
integration *values* are L2; only the integration *declarations* (the join-key
shape the golden path produces) and authored config are L1.

## 4. The entity model

### 4.1 Kinds
First-class **authored or resolver-derived** entities, all sharing the envelope:
`Component` (`service|website|library|worker|job|cli|ml-model`), `API`,
`Resource` (`datastore|queue|topic|bucket|cache`), `System`, `Domain`, `Group`,
`User`, and **`Composition`** (`compositions.md`). orun-native **derived**
entities, emitted from execution (SC4/SC8): **`Environment`** and
**`Deployment`**. `catalogmodel.EntityKind` is promoted from scaffolding to the
canonical enum.

### 4.2 The envelope
Every entity is one blob with the same top-level shape (`data-model.md` §2):

```
apiVersion · kind · identity · metadata · ownership · lifecycle · spec
          · relations · contracts · integrations · docs · links · provenance · extensions
```

Mapping from today's flat manifest: `metadata` splits into
`metadata`+`ownership`+`lifecycle`; `spec.dependencies` + the five graphs collapse
into `relations`+`contracts`; `provenance` stays and stays **hash-excluded**
(per `identity-and-keys.md` §10). `spec` is the only kind-specific block.

### 4.3 Derivation (what the resolver computes)
- `ownership.owner` ← CODEOWNERS longest-prefix over `identity.path`, with
  `ownership.source` recording the claim origin (S-2). Authored `metadata.owner`
  overrides with `source: authored`.
- `relations` ← resolved dependency edges + system/domain membership + provided/
  consumed APIs + composition + (derived) environment/deployment edges.
- `integrations` ← composition `effects.integrations` (SC8) + repo detection;
  rarely authored.
- `lifecycle.maturity` ← latest scorecard level (synced from L2; see §6 / CR-1 —
  the *value* is L2, but `lifecycle.maturity` is a denormalized convenience that
  MUST be recomputed on resolve, not authored).

### 4.4 Storage
The catalog tree object gains a kind-partitioned `entities/` subtree
(`entities/<Kind>/<name>.json`), replacing the flat `components/` tree, plus a
single `relations.json` blob (replacing the `graph/` subtree). `catalog.json`
carries per-kind counts. `impact/` (ownership map + fingerprints) is unchanged.

## 5. The typed relation graph

One blob, `relations.json` — a deterministic, bidirectional, typed edge set
spanning kinds (`data-model.md` §3): `ownedBy`/`owns`, `partOf`/`hasPart`,
`dependsOn`/`dependencyOf`, `providesApi`/`apiProvidedBy`,
`consumesApi`/`apiConsumedBy`, `runsOn`/`hosts`, `deployedTo`/`hostsDeployment`,
`composedBy`/`composes`. The existing edge attributes `optional` and
`include ("always"|"if-selected")` carry over verbatim.

**Convergence (CV-1).** `internal/affected` consumes `relations.json` instead of
`graph/dependencies.json`; the catalog graph and the change-detection graph
become **one artifact**. The five-graph compatibility view (if any consumer still
needs it) is a pure projection of `relations.json`, never a separate write.

## 6. The live plane (deployments / health)

L2 overlays computed from execution truth and surfaced **outside** the immutable
catalog tree (`data-model.md` §6). In v1 the live plane is **deployments and
health**, derived on read from `objrun` executions (which revision is live in
which environment, last status) — the same scan/join the cockpit already uses for
execution history, lifted to a typed, `entityKey`-keyed view (D-12). No persisted
overlay and no new index in v1 (objindex deferred, L-7).

> **Scorecards are extracted to a separate v2 epic, `specs/orun-scorecards/`**
> (for later review). This epic keeps only the **foundations** scorecards build
> on: the live plane above, the reserved `lifecycle.maturity` field (`null` until
> v2), and the composition `effects.scorecards` *declaration* (`compositions.md`
> §4.3) — none of which is evaluated here.

**CR-1 holds:** deployments/health are L2; `lifecycle.maturity` is a recomputed
denormalization (populated by the v2 scorecard engine), never authored, never
trusted across a source change.

## 7. Compositions as producers (overview)

Compositions gain two roles (full design in `compositions.md`): **(a)
Composition-as-Entity** — a `kind: Composition` envelope with ownership,
`lifecycle` (`stable|beta|deprecated`), semver atop the existing content
`ResolvedDigest`, and `usedBy` relations; **(b) Composition-as-Producer** — an
`effects` block declaring what running the golden path contributes to the catalog:
graph effects (emit `Deployment`/`Environment`, provision `Resource`, expose
`API`), integration effects (register the service in Datadog/PagerDuty → populate
`integrations`), and a carried scorecard-contributions *declaration* (`effects.scorecards` —
evaluated by the extracted v2 epic `specs/orun-scorecards/`). This is *how*
SC4's derived entities and SC6's integration keys are produced: the thing that
defines execution is the thing that feeds the catalog (S-7).

## 8. Extensibility & versioning

Three tiers, in preference order (`data-model.md` §8): (1) **well-known typed
fields** (the envelope; struct-generated, validated, first-class UI); (2)
**typed namespaced extensions** `extensions.x-<vendor>` validated against a
registered schema — onboard a new integration without a core bump; unknown
extensions render generically; (3) **`annotations`** untyped `string→string`,
last resort. `apiVersion` graduates `v1alpha1 → v1`; the resolver **up-converts
older manifests on read** (k8s-style conversion). Because L1 blobs are immutable
per snapshot, old snapshots keep their schema forever and convert lazily —
**never a destructive migration** (S-3).

## 9. SaaS / provenance / multi-tenancy (follow-on, SC12)

Designed now so nothing precludes it; built later. **Attestation:** sign the
catalog tree id — content-addressing already makes "this is provably what was
resolved from commit X" verifiable; a signature makes it auditable. **Tenancy:** a
`tenant` segment in refs/keys from day one (retrofitting is painful — S-8).
**Federation:** multi-repo → one catalog via multiple source refs + global
indexes (`objindex`); a System spans repos. **RBAC/visibility:**
`metadata.visibility` + ownership-derived edit rights; the portal filters the
graph per viewer. All ride existing primitives (`objectstore`, `objremote`,
`objindex`).

## 10. Package boundaries & data flow

```
component.yaml / composition.yaml
        │  (catalogmodel.ComponentYAML / CompositionYAML — both parsers + generated schema)
        ▼
catalogresolve.BuildCatalog ── derive ownership(CODEOWNERS) · relations · contracts · inference
        │                        + fold composition effects (SC8)
        ▼
nodes.<Entity> envelope  ──map── objplan/catalog.go  ──► entities/<Kind>/<name>.json
                                                          relations.json · catalog.json · impact/
        │                                                         │
        ▼                                                         ▼
objcatalog (kind-aware read view) ──► cmd/orun (catalog *)   internal/affected (relations.json)
        │                                                         │
        ▼                                                         ▼
live-plane view (deployments/health) ◄── objrun execution truth   [scorecards → v2: specs/orun-scorecards/]
```

`catalogresolve` MUST remain pure (no store imports — its `doc.go` contract);
CODEOWNERS + repo detection are provided as inputs, not read by the resolver
directly. `nodes`/`objplan`/`objcatalog` own the L1 representation; the live-plane
view owns L2 (deployments/health derived from `objrun`). The scorecard engine that
also reads L2 is the extracted v2 epic `specs/orun-scorecards/`.

## 11. Invariants

1. **CR-1** — no mutable fact in an L1 blob (§3).
2. **Determinism** — two resolves of one source produce byte-identical entity
   blobs, `relations.json`, and `catalog.json` (extends the existing
   `AssembleCatalog` determinism test).
3. **Losslessness** — the envelope carries every field the portal and the change
   engine need; a round-trip drops nothing (parity-guard, `test-plan.md`).
4. **Hash discipline** — `manifestHash` covers the envelope minus `provenance`;
   `catalogHash` covers all entity blobs + `relations.json` + resolver version;
   any envelope change bumps `resolverVersion` (one-time re-resolve, absorbed by
   content addressing).
5. **Both parsers + generated schema** — every authored field is accepted by the
   strict and permissive parsers and present in the generated schema.
6. **CV-1** — one relation graph, shared with the change engine (§5).
7. **Up-convert, never migrate-in-place** — older L1 schemas convert on read (§8).

## 12. Sharpness register

| # | Sharp edge | Resolution |
|---|------------|------------|
| S-1 | Envelope reshape changes `manifestHash` → every catalog id moves | Expected; `resolverVersion` bump; one memo miss; content addressing absorbs it (precedent: CS1 `Path` change). Documented in `migration.md`. |
| S-2 | Ownership claim source ambiguous (authored vs CODEOWNERS vs inherited) | `ownership.source` records origin; precedence authored > CODEOWNERS > system-inherited; surfaced in `describe`. |
| S-3 | Multiple envelope versions coexist on disk | Lazy up-conversion on read; immutable snapshots keep their schema; conversion is total + tested (round-trip). |
| S-4 | Derived Environment/Deployment entities collide with the env-scoping epic | Environment-as-entity is additive + read-only here; coordinated in `risks-and-open-questions.md`; can supply the identity that epic needs. |
| S-5 | Scorecards become a hand-maintained checklist that drifts | **Owned by the extracted v2 epic `specs/orun-scorecards/`** (rules evaluate orun-native data first; missing inputs → `unknown`, never false-pass). Out of scope here beyond the `lifecycle.maturity` foundation. |
| S-6 | Lossy graph→envelope mapping silently drops a field (the `Path` class) | Round-trip parity guard over a fixture covering every envelope block, per CS8's precedent. |
| S-7 | Composition `effects` over-claim (declare an integration it doesn't create) | `effects` are *declared* intent; the live plane only records what `objrun` actually produced — declared-vs-actual divergence is a scorecard signal, not silent trust. |
| S-8 | Tenancy retrofitted later breaks key formats | Reserve the `tenant` segment in the key/ref grammar now (SC0), even though SC12 implements it. |
| S-9 | Two authoring formats (component vs composition) drift in style | Shared envelope (`metadata`/`ownership`/`lifecycle`/`relations`); one generated-schema discipline; `compositions.md` mirrors `data-model.md`. |
