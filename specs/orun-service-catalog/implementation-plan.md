# Implementation Plan

> Milestone-based. Each states **goal**, **deps**, **suggested PR scope**, **done
> when**, **design refs**. Agents may split/merge while keeping each PR reviewable
> and green. The envelope reshape (SC1) and relation graph (SC2) are the
> load-bearing foundation; multi-kind (SC3) + derived entities (SC4) build the
> graph; the live plane (SC5) + integrations (SC6) deliver portal value;
> compositions (SC7–SC9) make the catalog self-producing; adoption (SC10–SC11)
> migrates the repo and the world; SC12 is the deferred enterprise wedge.

```
SC0 shared types ─► SC1 envelope ─► SC2 relations ─► SC3 multi-kind ─► SC4 derived (env/deploy)
                                          │                                   │
                                          │                     SC5 live plane (deployments/health)
                                          │                     SC6 integrations hub + extensions
                                          ▼                                   │
                       SC7 composition entity ─► SC8 effects→derivation ◄─────┘
                                          │       (produces SC4 + SC6 data)
                       SC10 migrate examples ─► SC11 legacy migration tooling + doc
                                          │
                                   SC12 attestation + federation (DEFERRED / follow-on)

  v2 epics (extracted, separate review):
    specs/orun-scorecards/   (SCR*) — scorecard engine; gated on SC5/SC8
    specs/orun-scaffolding/  (SCF*) — golden-path create; gated on SC7
```

Phases: **A** Foundation (SC0–SC2) · **B** Entity graph (SC3–SC4) · **C** Portal
value (SC5–SC6) · **D** Compositions (SC7–SC8) · **E** Adoption (SC10–SC11) · **F**
Enterprise (SC12, deferred). **Scorecards and scaffolding are extracted to their
own v2 epics** (`specs/orun-scorecards/`, `specs/orun-scaffolding/`).

---

## SC0 — Shared types & version foundation
**Goal:** the vocabulary every later milestone depends on.
- Promote `catalogmodel.EntityKind` to the full enum (§2.1); add the
  `EntityEnvelope` Go types + the per-kind `spec` types in `internal/catalogmodel`
  + mirror in `internal/nodes`. Reserve the `tenant` key segment (S-8). Stand up
  the `v1alpha1 → v1` conversion seam (no behavior yet). Bump the generated schema
  scaffolding (`schema/gen`).

**Deps:** none. **PR scope:** 1–2 PRs. **Done when:** types compile + serialize;
`go generate` is clean; no resolver behavior change yet. **Design:** `data-model.md`
§2/§8/§10.

## SC1 — Resolved envelope reshape + CODEOWNERS ownership
**Goal:** the manifest becomes the entity envelope.
- Split `metadata` → `metadata`/`ownership`/`lifecycle`; consolidate `provenance`;
  add `contracts`/`integrations`/`docs`/`links`/`extensions`. Add a CODEOWNERS
  resolution step (input-provided to keep `catalogresolve` pure) with
  `ownership.source`. Update `objplan/catalog.go` (`mapManifest` → `mapEntity`)
  and `objcatalog` read view. Bump `resolverVersion`; document the one-time
  catalog-id move.

**Deps:** SC0. **PR scope:** 2–3 PRs (envelope; ownership; read view). **Done
when:** a `Component` round-trips the full envelope; `manifestHash` excludes
`provenance`; `catalog describe` renders ownership/lifecycle; determinism +
round-trip parity green. **Design:** `design.md` §4, `data-model.md` §2.

## SC2 — Unified typed relation graph
**Goal:** one graph, shared with the change engine (CV-1).
- Replace `catalogresolve/graph.go` five-graph builder with a single
  `relations.json` builder (typed, forward-edge, sorted). Repoint
  `internal/affected` from `graph/dependencies.json` to `relations.json`,
  preserving `optional`/`include` semantics. Materialize inverse edges in
  `objcatalog`.

**Deps:** SC1. **PR scope:** 2 PRs (builder; affected repoint + parity). **Done
when:** `relations.json` is deterministic; `internal/affected` selection is
**unchanged** vs the five-graph path (parity test); `catalog tree`/`graph` render
from it. **Design:** `design.md` §5, `data-model.md` §3.

## SC3 — Multi-kind entities (API / Resource / System / Domain / Group)
**Goal:** promote graph-node-only kinds to first-class entities.
- Emit `entities/<Kind>/<name>.json` for API/Resource/System/Domain/Group derived
  from component specs (+ optional dedicated authoring, e.g. `api.yaml`). Rename
  `Owner`→`Group` with a read alias. Make `objcatalog` + `catalog list/describe/tree`
  kind-aware; `catalog.json` gains `countsByKind`.

**Deps:** SC2. **PR scope:** 2–3 PRs. **Done when:** all kinds enumerate +
describe; relations link across kinds; round-trip parity per kind. **Design:**
`data-model.md` §4/§9, `cli-surface.md`.

## SC4 — Derived Environment & Deployment entities
**Goal:** orun-native entities from execution truth.
- Emit `Environment` from component env bindings + execution; emit `Deployment`
  records from `objrun`. Add the `deployedTo`/`runsOn` relations. **Coordinate
  with `specs/archive/orun-env-scoping/`** — Environment identity here is additive,
  read-only, and may supply that epic's needs.

**Deps:** SC3. **PR scope:** 2 PRs (Environment; Deployment). **Done when:**
`catalog list --kind Environment|Deployment` works; deployment records map 1:1 to
executions; no change to `orun plan`/`run` semantics. **Design:** `data-model.md`
§4, `risks-and-open-questions.md` (S-4).

## SC5 — Live plane (deployments / health)
**Goal:** surface what's actually running, from orun's own execution data.
- Provide the L2 live-plane view (deployments/health) **derived on read** from
  `objrun`, keyed by `entityKey`, outside the catalog tree (D-12 — no persisted
  overlay, no objindex in v1). Reserve `lifecycle.maturity` in the envelope
  (emitted `null` until the scorecard engine lands).

> **Scorecards are extracted to `specs/orun-scorecards/` (v2).** SC5 ships only
> the live plane + the `lifecycle.maturity` foundation; the scorecard engine,
> `internal/scorecard`, and `orun catalog scorecard` live in that epic.

**Deps:** SC4. **PR scope:** 1–2 PRs. **Done when:** `catalog describe` shows
per-environment deployments/health for a live entity; the view is a pure
projection of `objrun` (CR-1 — nothing persisted into an L1 blob). **Design:**
`design.md` §6, `data-model.md` §6.

## SC6 — Integrations hub + extension registry
**Goal:** the catalog as correlation hub, extensibly.
- Typed `integrations` block + the `extensions.x-<vendor>` registry (schema
  registration + generic render + round-trip preservation). Define the SaaS
  fan-out **seam** (resolve integration pointers → L2) without shipping vendor
  connectors.

**Deps:** SC1 (+SC5 for L2 sink). **PR scope:** 2 PRs. **Done when:** a registered
extension validates + round-trips; an unknown `x-*` is preserved; integration
keys render in `describe`. **Design:** `data-model.md` §8, `cli-surface.md`.

## SC7 — Composition envelope + contract (Composition-as-Entity)
**Goal:** compositions become catalogued, owned, versioned golden paths.
- Add the `Composition` authoring envelope + typed `contract`
  (inputs/outputs/requires/provides) + semver + `lifecycle`. Project each used
  composition as `entities/Composition/<name>.json` with `composes`/`usedBy`
  relations. Evolve `compositions.lock.yaml` (semver + deprecation).

**Deps:** SC1, SC3. **PR scope:** 2–3 PRs. **Design:** `compositions.md` §1–§2/§7,
`data-model.md` §5. **Done when:** `catalog describe --kind Composition` renders;
`usedBy` links resolve; lock round-trips.

## SC8 — Composition `effects` → catalog derivation
**Goal:** the golden path *produces* catalog truth (the keystone).
- Implement `effects.graph` (emit `Deployment`/`Environment`, provision
  `Resource`, expose `API`) and `effects.integrations` (populate `integrations`).
  Carry the `effects.scorecards` *declaration* into the resolved Composition node
  — its **evaluation/credit lands in `specs/orun-scorecards/` (v2)**. Record
  **declared vs actual** (live plane records only what `objrun` produced — S-7).

**Deps:** SC4, SC6, SC7 (+ the SC5 live plane for the actual-side). **PR scope:**
2 PRs (graph; integrations). **Done when:** running a golden-path composition
emits the declared entities/keys; declared-vs-actual divergence surfaces as a
signal, not silent trust. The scorecard-credit half is verified in the v2
scorecards epic. **Design:** `compositions.md` §3/§4, `design.md` §7.

## SC9 — Golden-path scaffolding — EXTRACTED to `specs/orun-scaffolding/` (v2)
**Moved.** The scaffolding engine (`internal/scaffold`), `orun compositions
scaffold`, and `orun create` are specified in the **`specs/orun-scaffolding/`**
v2 epic (milestones SCF0–SCF3, gated on SC7). This epic keeps only the authored
composition `scaffold` block as a foundation (`compositions.md` §5).

## SC10 — Migrate the in-repo examples
**Goal:** the repo dogfoods the new model.
- Update `examples/**` `component.yaml` to the `orun.io/v1` envelope (ownership,
  lifecycle, relations/contracts, system/domain); convert example compositions to
  the new authoring (`contract`/`effects`/`policy`); add CODEOWNERS to examples
  so ownership derives. Keep both parsers green.

**Deps:** SC1–SC8. **PR scope:** 2–3 PRs (components; compositions; CODEOWNERS).
**Done when:** `examples-validate`/`examples-plan` pass; `orun catalog refresh`
over `examples/` yields a populated multi-kind catalog with owners + scorecards;
`examples.md` before/after matches reality. **Design:** `examples.md`.

## SC11 — Legacy migration tooling + document
**Goal:** a paved path for existing repos.
- Implement lazy `v1alpha1 → v1` up-conversion on read (the SC0 seam, now total +
  tested). Ship `orun catalog migrate` (lint authored `component.yaml` + emit a
  codemod diff to the new envelope) and `orun compositions migrate`. Publish
  `migration.md` as the authoritative old→new mapping + deprecation windows.

**Deps:** SC1–SC9. **PR scope:** 2–3 PRs (up-conversion; component codemod;
composition codemod). **Done when:** a `v1alpha1` fixture repo migrates with no
behavior change; up-conversion round-trips; deprecation warnings fire on legacy
fields. **Design:** `migration.md`.

## SC12 — Attestation + multi-tenant federation (DEFERRED / follow-on)
**Goal:** the enterprise wedge.
- Sign the catalog tree id (`provenance.attestation`); implement the reserved
  `tenant` segment; cross-repo federation via `objindex` global indexes;
  `metadata.visibility` + RBAC filtering.

**Deps:** SC1–SC11. **PR scope:** separate spec recommended. **Done when:**
specified separately. **Design:** `design.md` §9 (high-level only here).

---

## Sequencing notes
- **SC8 is the keystone**: SC4 (derived entities) and SC6 (integration keys)
  define the *shape*; SC8 is what *produces* them at runtime. SC4/SC6 can land with
  resolver-only derivation first, then SC8 adds the composition-driven source.
- **Parity gates** (SC2 affected, SC10 examples) follow the `orun-catalog-state`
  CS8 precedent: the old path is removed only after the new path is proven equal.
- **No double-write**: there is one store; every milestone writes the object model
  only (the `catalogstore` era is over — `orun-legacy-retirement`).
