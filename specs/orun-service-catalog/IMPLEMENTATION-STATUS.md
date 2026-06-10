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
| SC1 — Resolved envelope reshape + CODEOWNERS ownership | A | **Partial (envelope landed; CODEOWNERS next)** | — | `mapManifest`→`mapEntity`: `metadata` splits into `metadata`/`ownership`/`lifecycle`; `relations`/`contracts`/`provenance` blocks emitted; `apiVersion`→`orun.io/v1`; `resolverVersion` 1→2 (one-time id move, S-1). `spec` kept lossless through SC1 (deps fully promote to relations.json in SC2). Ownership source = `authored`/`unknown` from authored owner; CODEOWNERS-derived ownership is the follow-up PR |
| SC2 — Unified typed relation graph | A | **Landed (builder + affected repoint; five-graph removal is the follow-up)** | — | `relations.json` written by `AssembleCatalog` from the entity relations (forward-edge, sorted, typed); `objcatalog` reads it; `internal/affected` consumes `dependsOn` (Component) edges from it with a legacy-graph fallback; CV-1 parity test green; `resolverVersion` 2→3 |
| SC3 — Multi-kind entities | B | Not started | — | API/Resource/System/Domain/Group first-class; kind-aware read |
| SC4 — Derived Environment & Deployment | B | Not started | — | from execution; coordinate with `orun-env-scoping` |
| SC5 — Live plane (deployments/health) | C | Not started | — | derived-on-read; reserves `lifecycle.maturity`. **Scorecard engine → `specs/orun-scorecards/` (v2)** |
| SC6 — Integrations hub + extension registry | C | Not started | — | typed `x-*`; SaaS fan-out seam (no vendor connectors) |
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
