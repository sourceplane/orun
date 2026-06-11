# Implementation Status — orun-service-catalog

> Live tracker for the SC0 → SC12 milestones (`implementation-plan.md`). Updated as
> each milestone lands. This is the as-built record; the design docs describe the
> intent.

| Field | Value |
|-------|-------|
| Status | **All in-scope milestones landed — SC0–SC8, SC10, SC11 (SC9/SC12 extracted/deferred)** |
| Shipped | SC0, SC1 (+SC1b CODEOWNERS), SC2, SC3, SC4, SC5, SC6, SC7, SC8, SC10, SC11 — the full resolved entity graph (envelope · relations.json · multi-kind `entities/<Kind>/` · derived Environments · Composition-as-Entity), the derived-on-read live plane, the integrations hub + extension registry, compositions as catalogued **producers** (`effects`→integrations), the examples dogfood, and the `orun catalog migrate` adoption lint |
| Follow-ups (in-milestone) | SC8 graph-effects (emit Deployment/Resource/API from `effects.graph`); SC11 lazy `v1alpha1→v1` up-conversion on read + composition codemod; SC10 broaden component coverage |
| Out of scope (extracted/deferred) | SC9 → `specs/orun-scaffolding/` (v2); SC12 → attestation + multi-tenant federation (follow-on); scorecard engine → `specs/orun-scorecards/` (v2) |
| Resolver version | bumped 1→8 across SC1–SC8 (one content-id move per envelope/tree change, S-1) |
| Builds on | `orun-object-model` (merged), `orun-component-catalog`, `orun-catalog-state`; legacy stack retired (`orun-legacy-retirement`) |
| Store | single object model (`.orun/objectmodel/`); no dual-write |
| apiVersion | `orun.io/v1` (graduated in SC1); lazy up-conversion of older blobs is SC11 |

| Milestone | Phase | Status | PR | Notes |
|-----------|-------|--------|----|-------|
| SC0 — Shared types & version foundation | A | **Landed** | — | promoted `EntityKind` (full enum + Owner→Group alias); `EntityEnvelope` + per-kind specs in `catalogmodel`, mirrored as `nodes.Entity`; typed relation vocabulary + inverses; reserved `tenant` segment; `v1alpha1→v1` up-convert seam (no rewrites yet) |
| SC1 — Resolved envelope reshape + CODEOWNERS ownership | A | **Landed** | — | `mapManifest`→`mapEntity`: `metadata` splits into `metadata`/`ownership`/`lifecycle`; `relations`/`contracts`/`provenance` blocks emitted; `apiVersion`→`orun.io/v1`. CODEOWNERS-derived ownership via `internal/codeowners` + `objplan.OwnerResolverForWorkspace`, wired into every catalog-build path (refresh/plan/seam) so the content id is path-independent; precedence authored > CODEOWNERS > unknown (`ownership.source`, S-2); `spec` kept lossless (deps promoted to relations.json in SC2) |
| SC2 — Unified typed relation graph | A | **Landed (builder + affected repoint; five-graph removal is the follow-up)** | — | `relations.json` written by `AssembleCatalog` from the entity relations (forward-edge, sorted, typed); `objcatalog` reads it; `internal/affected` consumes `dependsOn` (Component) edges from it with a legacy-graph fallback; CV-1 parity test green; `resolverVersion` 2→3 |
| SC3 — Multi-kind entities | B | **Landed (derivation + kind-aware read + countsByKind; Component→entities/Component/ unification is the follow-up)** | — | `AssembleCatalog` derives API/Resource/System/Domain/Group entities from each manifest's relations/contracts and writes `entities/<Kind>/<name>.json`; `catalog.json` gains `countsByKind`; `objcatalog` enumerates them (`view.Entities`); `catalog list` owner now reads the ownership block; `nodes.Entity.Validate`; `resolverVersion` 3→4 |
| SC4 — Derived Environment & Deployment | B | **Environment landed; Deployment → SC5 live plane** | — | component env bindings emit `deployedTo` edges + derived `Environment` entities (additive, read-only, coordinates with `orun-env-scoping`); `resolverVersion` 4→5. Deployment records are execution-derived → folded into the SC5 derived-on-read live plane (design §6: deployments are L2) |
| SC5 — Live plane (deployments/health) | C | **Landed** | — | `objread.ComponentDeployments`: latest deployment per environment derived on read from `objrun` (the same scan/join as history, refined to (component, env) pairs), with a status-derived health; surfaced in `catalog describe` (text + `--json`). Pure projection — nothing persisted (CR-1). `lifecycle.maturity` reserved (emitted `null` since SC1). **Scorecard engine → `specs/orun-scorecards/` (v2)** |
| SC6 — Integrations hub + extension registry | C | **Landed** | — | authored `integrations`/`links`/`docs`/`extensions` carry through the resolver to the envelope (x-`<vendor>` blocks preserved verbatim on round-trip); `internal/catalogext` typed extension registry (register schema → validate; unknown namespaced blocks preserved; non-namespaced rejected); surfaced in `catalog describe`; generated schema updated; `resolverVersion` 5→6 |
| SC7 — Composition envelope + contract | D | **Landed (Composition-as-Entity from the lock; authored contract/semver pending)** | — | `objplan.CompositionResolverForWorkspace` reads `.orun/compositions.lock.yaml` (type→composition via `exports`); `mapEntity` emits `spec.composition` (source+digest) + a `composedBy` relation; `nodes.deriveEntities` projects each used composition as `entities/Composition/<name>.json` enriched with source/digest; `composes` is the materialized inverse; surfaced in `catalog describe`; verified end-to-end on `examples/`; `resolverVersion` 6→7 |
| SC8 — Composition `effects` → derivation | D | **Landed (integrations + declaration carry; full graph effects pending)** | — | `effects` authoring on the composition manifest (`model.CompositionEffects`); the resolver reads per-type effects from dir-source manifests; `effects.integrations` populate each backed component's integrations (authored wins — declared-vs-actual, S-7); `effects.scorecards`/`provides` declarations carried into `spec.composition.effects` → the Composition entity (evaluation is the v2 scorecards epic). Verified end-to-end on `examples/`; `resolverVersion` 7→8 |
| SC9 — Golden-path scaffolding | D | **Extracted (v2)** | — | → `specs/orun-scaffolding/` (SCF*, gated on SC7) |
| SC10 — Migrate the in-repo examples | E | **Started (CODEOWNERS + representative component; compositions migration pending)** | — | `examples/CODEOWNERS` added (ownership now derives for all 15 example components); `apps/api-edge` enriched with `system`/`lifecycle`/`integrations`/`links`/`docs`/`extensions` — verified end-to-end via `catalog describe` (owner from CODEOWNERS longest-prefix, System entity, integrations, preserved `x-orun-demo`). `validate`/`plan` green. Remaining: convert example compositions + broaden component coverage |
| SC11 — Legacy migration tooling + document | E | **Started (`orun catalog migrate` lint; lazy up-conversion pending)** | — | `orun catalog migrate` — a read-only, advisory v1-readiness lint over authored `component.yaml` (legacy apiVersion · unowned-with-no-CODEOWNERS · missing lifecycle/system), text + `--json`. Demonstrates the additive, non-destructive migration path (S-3). Remaining: lazy `v1alpha1→v1` up-conversion on read + composition codemod |
| SC12 — Attestation + multi-tenant federation | F | **Deferred (follow-on)** | — | separate spec recommended |

## Notes

- The convergence prerequisite is already satisfied: the single object-model
  catalog is the only store (`orun-legacy-retirement` deleted `catalogstore`/
  `statestore`), so every milestone writes one representation — no dual-write, no
  dying store to avoid.
- SC8 is the keystone — SC4/SC6 define the shape of derived entities and
  integration keys; SC8 is what produces them at runtime via composition `effects`.

## Post-epic hardening review (as-built audit)

Fixed in the hardening pass (`resolverVersion` 8→9):

- **Entity filename collision** — two derived entities of one kind sharing a
  sanitized name (Groups `@org-a/edge`/`@org-b/edge`, APIs `ns1/r/auth`/`ns2/r/auth`)
  failed the *entire* catalog write with "duplicate tree entry name". Colliding
  entries now fall back to the sanitized entityKey (then a content-hash suffix).
- **Untracked-input staleness** — CODEOWNERS and `.orun/compositions.lock.yaml`
  feed the resolved catalog but may be invisible to the source id (the lock is
  commonly gitignored). The resolve memo now folds a `WorkspaceInputsDigest`
  into its key, and `EnsureFresh`/`IsStale` gate on an inputs stamp — a lock
  re-pin can never serve a stale catalog (verified: same `sourceId`, moved
  `catalogId`).
- **CODEOWNERS precedence** — corrected to GitHub's documented order
  (`.github/` → root → `docs/`).
- **`provenance.manifestHash`** — the resolver's canonical manifestHash is now
  carried into the envelope (data-model.md §2; previously dropped).
- **Empty legacy `spec.composition`** — the zero `{"source":"","type":""}` block
  is no longer emitted for unbound components.
- **`catalog list --kind <Kind>`** — the SC3/SC4 "done when" surface, now
  implemented over `entities/<Kind>/`.

## Model-strengthening pass (post-review, resolverVersion 8→14)

Acting on the review's findings + recommendations — one PR each, verified +
merged:

1. **Property + golden test hardening** — a `rapid` generator over
   `AssembleCatalog` with adversarial colliding keys (the SC3-collision-class
   net) + an `EntityEnvelope` golden round-trip fixture.
2. **Key-grammar unification** — `NormalizeOwnerRef`/`QualifyEntityKey`: Group/
   User refs become `group:`/`user:`; System/Domain/Environment/Composition/API/
   Resource keys are qualified to `<ns>/<repo>/<name>`. (Resolves the deviation
   below.)
3. **Membership enrichment** — every derived entity carries `spec.members` +
   `memberCount` (the inverse edges); `catalog list --kind` shows a MEMBERS
   column.
4. **Composition version + lifecycle** — authored on the composition manifest,
   carried onto `spec.composition` + the Composition entity; `describe` renders
   `source@version (lifecycle)`.
5. **Inferred runtime** — `spec.runtime` (languages/frameworks/infra/
   packageManagers) surfaced on the Component envelope (was resolved-but-dropped).
6. **`effects.graph`** — composition `provides`→Resource entities (component
   `dependsOn`) and `exposes`→API entities (component `contracts`), the SC8
   producer keystone completed.
7. **Lazy `v1alpha1→v1` up-conversion on read** — invariant #7: a pre-SC1 flat
   blob reshapes to the v1 envelope on read, the stored bytes untouched.

Remaining recommended follow-ups (deliberate, recorded):

- **`catalogHash` / display key** — the legacy `cat-…` snapshot key is computed
  pre-binding (over manifests + five graphs) and does not cover the envelope or
  composition bindings; two object-model catalogs can share a display key. The
  authoritative identity is the object-model `catalogId` (content-addressed,
  correct), so this is a cosmetic display-label gap, not a correctness one;
  aligning the display key with §10 belongs to the five-graph retirement.
- **Hash discipline (invariant 4)** — the envelope blob id covers provenance
  (content addressing has no exclusion seam); the §10 "manifestHash excludes
  provenance" property is honored by the *carried* `provenance.manifestHash`,
  not by the blob id. Acceptable because provenance is deterministic from source.
- **Five-graph retirement** — `relations.json` already powers change detection;
  `graph/` is still written for `catalog diff`. Retiring it (projecting the diff
  off `relations.json`) + moving `catalogHash` to §10 is the remaining
  structural follow-up.
