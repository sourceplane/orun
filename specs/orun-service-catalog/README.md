# Spec: orun-service-catalog

**orun's catalog graduates into a full software catalog / developer portal: a
typed, multi-kind *entity graph* — Components, APIs, Resources, Systems, Domains,
Groups, plus orun-native Environments and Deployments — that is *derived* from
git and execution, content-addressed, and (eventually) signed. Compositions
evolve in lockstep: from execution contracts into *golden-path entities* that
**produce** the catalog's live truth.** One model, authored once, rendered
everywhere — CLI, cockpit, and a future SaaS portal.

This is an enrichment-and-promotion spec, not a new store. It builds directly on
the **single** content-addressed object model (`specs/orun-object-model/`, merged;
legacy `catalogstore`/`statestore` retired in `orun-legacy-retirement`) and the
resolver (`internal/catalogresolve`, `internal/catalogmodel`). It (1) reshapes
the resolved manifest into a stable **entity envelope**, (2) promotes the
existing five graphs into one typed **relation graph** spanning multiple entity
kinds, (3) adds a mutable **live plane** (health, deployments, scorecards) fed by
orun's own execution engine, and (4) turns **compositions** into catalogued,
contract-bearing golden paths that emit entities, integration keys, and scorecard
signals back into the catalog.

> **The defining property is provenance.** Because every entity is resolved from
> a commit, content-addressed, and (SC12) signable, orun's catalog is verifiably
> drift-free in a way Backstage's hand-authored YAML and Datadog's
> telemetry-inferred services structurally are not. The design protects this
> property: nothing that changes without a source change may enter the immutable
> manifest.

## Status

| Field | Value |
|-------|-------|
| Status | **Draft → Ready for review** |
| Builds on | `specs/orun-object-model/` (object graph), `specs/archive/orun-component-catalog/` (`catalogresolve`, `catalogmodel`), `specs/orun-catalog-state/` (`objcatalog`, `internal/affected`) |
| Reshapes | the flat Phase-2 `nodes.ComponentManifest` (`identity/metadata/spec/provenance`) into the multi-kind **entity envelope**; `catalogresolve/graph.go` five-graph builder into one typed relation graph |
| Promotes | `catalogmodel.EntityRef` / `EntityKind` (scaffolding, currently unused) to first-class entity kinds |
| apiVersion | `orun.io/v1alpha1` → **`orun.io/v1`** (lazy up-conversion on read; SC0) |
| Coordinates with | `specs/archive/orun-env-scoping/` (env-as-entity, SC4, can unblock that epic) |
| Decisions locked | derived-truth catalog; three planes (authored / resolved-immutable / live-mutable); one typed relation graph shared with the change engine; compositions are catalogued producers of derived truth; convention-over-configuration authoring; lazy schema up-conversion (never destructive); examples migrate in-repo; a legacy migration path is delivered |

## The one-paragraph thesis

The resolved catalog already lives in the object graph as immutable
`ComponentManifest` blobs, but the model is flat, single-kind, and thin: no
ownership beyond a string, no lifecycle/maturity, no first-class APIs or
Resources, no live state, no notion of golden paths. A developer portal needs a
*typed entity graph* with ownership, lifecycle, relations, contracts, and a live
plane — and it needs that graph to stay true without humans curating it. orun is
uniquely placed to deliver this: it **resolves** the catalog from git, and it
**owns the execution engine**, so it can derive the live plane (deployments,
health, scorecards) and emit it through the same compositions that perform the
work. This spec makes the manifest a shared **entity envelope**, promotes the
graph to **typed multi-kind relations**, adds the **live plane + scorecards**,
and evolves **compositions** into golden-path entities whose `effects` populate
the catalog automatically — so choosing the paved road makes a service
well-owned, documented, observable, and gold-rated *for free*.

## The three planes

```
L0 AUTHORED (git)            L1 RESOLVED — immutable (object graph)        L2 LIVE — mutable (indexes / overlays)
────────────────             ─────────────────────────────────────        ──────────────────────────────────────
component.yaml      ─resolve─► entity envelope blobs                ──────► deployments · health · scorecards
composition.yaml              (entities/<Kind>/<name>.json)                 incidents · cost · vulnerabilities
   │                          relations.json (one typed graph)              (keyed by entityKey; NEVER in L1)
   │                          catalog.json + impact/                              ▲
   └── CODEOWNERS, repo,                                                          │
       inference ──────────►  ownership · lifecycle · integrations              objrun / execution truth
                                                                                  │
COMPOSITIONS as PRODUCERS:  a golden-path run emits Deployment + Environment entities,
                            integration join-keys, and scorecard signals into L1/L2 (SC8).
```

## Read order

1. **`design.md`** — the three planes, the entity model and envelope, the typed
   relation graph, the live plane + scorecards, extensibility & versioning,
   SaaS/provenance, package boundaries, invariants, and the sharpness register.
2. **`data-model.md`** — every schema field-by-field: the entity envelope, each
   kind, the relation graph, the composition manifest, scorecards, the live
   overlays, and the on-disk catalog tree.
3. **`compositions.md`** — composition authoring evolution: Composition-as-Entity,
   the typed contract, the `effects` producer model, golden-path scaffolding,
   policy/guardrails, and composability.
4. **`cli-surface.md`** — kind-aware `catalog` commands, `catalog scorecard`,
   `catalog graph`, the composition + scaffold surface, and the migration commands.
5. **`migration.md`** — the legacy → new-model migration: lazy up-conversion,
   `orun catalog migrate` / `orun compositions migrate` (lint + codemod),
   deprecation windows, and per-field old→new mapping for both components and
   compositions.
6. **`examples.md`** — the in-repo scope: which `examples/**` components and
   compositions change, with representative before/after.
7. **`implementation-plan.md`** — milestones **SC0 → SC12**.
8. **`test-plan.md`** — coverage targets, determinism + round-trip property
   tests, the migration parity gate, the scorecard/effects fixtures, and the E2E walk.
9. **`risks-and-open-questions.md`** — decisions, open questions, the risk
   register, and the deferred register.

## Phase boundaries

| In scope (this spec) | Out of scope |
|----------------------|--------------|
| Entity envelope (`metadata`/`ownership`/`lifecycle`/`relations`/`contracts`/`integrations`/`docs`/`provenance`/`extensions`); promotion to multi-kind (Component/API/Resource/System/Domain/Group + derived Environment/Deployment); the unified typed relation graph; CODEOWNERS-derived ownership; the **live plane** (deployments/health derived from execution) + the reserved `lifecycle.maturity` foundation; the integrations hub + typed extension registry; composition envelope + contract + `effects` derivation (graph + integrations) + the authored `scaffold` / `effects.scorecards` declarations; updating `examples/**`; the legacy migration tooling + doc; lazy `v1` up-conversion | **The scorecard engine — extracted to `specs/orun-scorecards/` (v2)**; **golden-path scaffolding / `orun create` — extracted to `specs/orun-scaffolding/` (v2)**; attestation/signing + multi-tenant federation (SC12, **follow-on**); the SaaS web UI build (read seam kept open — `consumers.md` of `orun-catalog-state`, L-5); external integration *connectors* (the registry defines the seam; vendor adapters ship separately); the single-env redesign (`specs/archive/orun-env-scoping/`); R2/S3 remote object driver (rides `objremote`, deferred) |

## Convention over configuration (locked)

The developer authors the *minimum*; orun derives the world-class manifest.
`owner` from CODEOWNERS, `integrations` and `runtime` from repo detection,
`relations` from the resolver, `maturity` from scorecards, `Deployment`/
`Environment` entities from execution. Every L0 addition flows through **both**
`component.yaml` parsers (strict `catalogmodel.ComponentYAML.OpenSchema()` +
the permissive plan-engine parser) and the **struct-generated** JSON schema
(`internal/catalogmodel/schema/gen` + `go generate`) — see `migration.md` §6.

## Document conventions

- Go for interfaces, JSON for on-disk schemas. Forward-slash logical paths,
  root-relative to `.orun/objectmodel/`.
- Object IDs `"<algo>:<hex>"`. `lowerCamelCase` JSON. RFC 3339 / Z timestamps.
- "MUST / SHOULD / MAY" carry RFC 2119 weight in `data-model.md`,
  `compositions.md`, and `migration.md` (the correctness/compatibility contracts).
- Entity keys are three-segment `<namespace>/<repo>/<name>` per
  `specs/archive/orun-component-catalog/identity-and-keys.md`, generalized with a `kind`.

## Out-of-band references

- Predecessor specs: `specs/orun-object-model/`, `specs/archive/orun-component-catalog/`,
  `specs/orun-catalog-state/`.
- Coordinated epic: `specs/archive/orun-env-scoping/` (Environment entity, SC4).
- Packages changed: `internal/catalogmodel`, `internal/catalogresolve`,
  `internal/nodes`, `internal/objplan`, `internal/objcatalog`, `internal/affected`,
  `internal/composition`, `internal/model` (`composition.go`), `cmd/orun`, and
  `examples/**`. (`internal/scorecard` and `internal/scaffold` belong to the v2
  epics `specs/orun-scorecards/` and `specs/orun-scaffolding/`.)
