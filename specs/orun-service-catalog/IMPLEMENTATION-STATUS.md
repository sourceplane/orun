# Implementation Status — orun-service-catalog

> Live tracker for the SC0 → SC12 milestones (`implementation-plan.md`). Updated as
> each milestone lands. This is the as-built record; the design docs describe the
> intent.

| Field | Value |
|-------|-------|
| Status | **Draft — not started** |
| Builds on | `orun-object-model` (merged), `orun-component-catalog`, `orun-catalog-state`; legacy stack retired (`orun-legacy-retirement`) |
| Store | single object model (`.orun/objectmodel/`); no dual-write |
| apiVersion | `orun.io/v1alpha1` today → `orun.io/v1` (SC0/SC11) |

| Milestone | Phase | Status | PR | Notes |
|-----------|-------|--------|----|-------|
| SC0 — Shared types & version foundation | A | **Landed** | — | promoted `EntityKind` (full enum + Owner→Group alias); `EntityEnvelope` + per-kind specs in `catalogmodel`, mirrored as `nodes.Entity`; typed relation vocabulary + inverses; reserved `tenant` segment; `v1alpha1→v1` up-convert seam (no rewrites yet) |
| SC1 — Resolved envelope reshape + CODEOWNERS ownership | A | **Landed** | — | `mapManifest`→`mapEntity`: `metadata` splits into `metadata`/`ownership`/`lifecycle`; `relations`/`contracts`/`provenance` blocks emitted; `apiVersion`→`orun.io/v1`. CODEOWNERS-derived ownership via `internal/codeowners` + `objplan.OwnerResolverForWorkspace`, wired into every catalog-build path (refresh/plan/seam) so the content id is path-independent; precedence authored > CODEOWNERS > unknown (`ownership.source`, S-2); `spec` kept lossless (deps promoted to relations.json in SC2) |
| SC2 — Unified typed relation graph | A | **Landed (builder + affected repoint; five-graph removal is the follow-up)** | — | `relations.json` written by `AssembleCatalog` from the entity relations (forward-edge, sorted, typed); `objcatalog` reads it; `internal/affected` consumes `dependsOn` (Component) edges from it with a legacy-graph fallback; CV-1 parity test green; `resolverVersion` 2→3 |
| SC3 — Multi-kind entities | B | **Landed (derivation + kind-aware read + countsByKind; Component→entities/Component/ unification is the follow-up)** | — | `AssembleCatalog` derives API/Resource/System/Domain/Group entities from each manifest's relations/contracts and writes `entities/<Kind>/<name>.json`; `catalog.json` gains `countsByKind`; `objcatalog` enumerates them (`view.Entities`); `catalog list` owner now reads the ownership block; `nodes.Entity.Validate`; `resolverVersion` 3→4 |
| SC4 — Derived Environment & Deployment | B | **Environment landed; Deployment → SC5 live plane** | — | component env bindings emit `deployedTo` edges + derived `Environment` entities (additive, read-only, coordinates with `orun-env-scoping`); `resolverVersion` 4→5. Deployment records are execution-derived → folded into the SC5 derived-on-read live plane (design §6: deployments are L2) |
| SC5 — Live plane (deployments/health) | C | **Landed** | — | `objread.ComponentDeployments`: latest deployment per environment derived on read from `objrun` (the same scan/join as history, refined to (component, env) pairs), with a status-derived health; surfaced in `catalog describe` (text + `--json`). Pure projection — nothing persisted (CR-1). `lifecycle.maturity` reserved (emitted `null` since SC1). **Scorecard engine → `specs/orun-scorecards/` (v2)** |
| SC6 — Integrations hub + extension registry | C | **Landed** | — | authored `integrations`/`links`/`docs`/`extensions` carry through the resolver to the envelope (x-`<vendor>` blocks preserved verbatim on round-trip); `internal/catalogext` typed extension registry (register schema → validate; unknown namespaced blocks preserved; non-namespaced rejected); surfaced in `catalog describe`; generated schema updated; `resolverVersion` 5→6 |
| SC7 — Composition envelope + contract | D | Not started | — | Composition-as-Entity; semver + lifecycle |
| SC8 — Composition `effects` → derivation | D | Not started | — | **keystone**: produces SC4 + SC6 data; declared-vs-actual |
| SC9 — Golden-path scaffolding | D | **Extracted (v2)** | — | → `specs/orun-scaffolding/` (SCF*, gated on SC7) |
| SC10 — Migrate the in-repo examples | E | Not started | — | `examples/**` to v1; CODEOWNERS; both-parsers gate |
| SC11 — Legacy migration tooling + document | E | Not started | — | lazy up-conversion; `catalog/compositions migrate` |
| SC12 — Attestation + multi-tenant federation | F | **Deferred (follow-on)** | — | separate spec recommended |

## Notes

- The convergence prerequisite is already satisfied: the single object-model
  catalog is the only store (`orun-legacy-retirement` deleted `catalogstore`/
  `statestore`), so every milestone writes one representation — no dual-write, no
  dying store to avoid.
- SC8 is the keystone — SC4/SC6 define the shape of derived entities and
  integration keys; SC8 is what produces them at runtime via composition `effects`.
