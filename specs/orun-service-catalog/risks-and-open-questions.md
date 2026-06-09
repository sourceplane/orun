# Risks & Open Questions

> Live register. Decisions with a stated default are settled unless re-opened via
> the proposal protocol. Open questions name the milestone they must be called
> by. Risks carry likelihood/impact + mitigation. The sharpness register from
> `design.md` §12 (S-1…S-9) is the source of most of these.

## Decisions (settled, with defaults)

| # | Question | Default decision | Rationale |
|---|----------|------------------|-----------|
| D-1 | Curated vs derived catalog | **Derived-truth: resolve from git + execution, never hand-curated** | The whole thesis (README). Provenance is the defining property — every entity is resolved from a commit, content-addressed, and (SC12) signable; nothing that changes without a source change may enter L1 (CR-1). |
| D-2 | Plane separation | **Three planes (L0 authored / L1 resolved-immutable / L2 live-mutable); CR-1 = nothing mutable in an L1 blob** | Drift-free guarantee. Scores/deployments/integration *values* are L2; only authored config + integration *declarations* are L1 (`design.md` §3). |
| D-3 | One graph or two | **One typed relation graph (`relations.json`) shared with `internal/affected` (CV-1)** | Not separate catalog-vs-change graphs. The change engine already wants a typed multi-kind graph; `optional`/`include` carry through verbatim (`design.md` §5). |
| D-4 | Authoring burden | **Convention over configuration; `owner` from CODEOWNERS; most envelope fields DERIVED** | The developer authors the minimum; the resolver derives ownership, relations, integrations, runtime, maturity (`migration.md` §3). |
| D-5 | Schema evolution | **Lazy `v1alpha1 → v1` up-conversion on read; never destructive in-place migration** | Immutable snapshots keep their schema forever; conversion is in-memory, total, round-trip-tested (S-3, `migration.md` §2). |
| D-6 | Composition's role | **Compositions are catalogued PRODUCERS; their `effects` feed the catalog; declared-vs-actual is a signal, not silent trust** | The artifact that defines execution is best placed to declare what running it contributes (S-7, `compositions.md` §4). |
| D-7 | One store or dual-write | **Single store only (the object model); no dual-write** | The `catalogstore` era is over (`orun-legacy-retirement`). Every milestone writes the object model only (`implementation-plan.md` "No double-write"). |
| D-8 | `Owner` kind label | **Rename `EntityKind: Owner → Group`, with a read-time alias** | Portal vocabulary; non-destructive — persisted `Owner` nodes/edges read-alias to `Group`; owner keys (`group:*`/`user:*`) unchanged (`migration.md` §4). |
| D-9 | Scorecard missing input | **Missing-input predicate → `unknown` (counts as not-passed, rendered distinctly), never a silent pass** | A hand-maintained checklist that drifts is the failure mode; external-data rules degrade to `unknown`, not false-pass (S-5, `data-model.md` §7). |
| D-10 | Env-as-entity scope here | **Environment-as-entity is additive + read-only in this spec; the single-env redesign stays in `specs/orun-env-scoping/`** | This spec can *supply* the identity that epic needs without implementing it; avoids a collision (S-4, SC4). |
| D-11 | Non-Component authoring (was Q-1) | **Derive API/Resource/… from component specs; one escape hatch — a top-level `apis:`/`resources:` block in `intent.yaml` for shared/ownerless contracts. Dedicated `api.yaml` files deferred (L-6).** | Convention-first (D-4); covers the shared-contract case without a new file-discovery/parser surface. |
| D-12 | Live-plane persistence (was Q-2) | **Deployments/health derived on read (scan+filter over `objrun`); no persisted overlay, no `objindex` in v1.** | Avoids a cache-coherence problem; matches the `orun-catalog-state` scan+filter precedent; `objindex` deferred (L-7). |
| D-13 | Extension registry (was Q-4) | **Compiled-in for first-party `x-orun-*`; in-repo config for org `x-<vendor>-*`; remote-fetch deferred (L-3).** | First-party validated; orgs register without recompiling; remote carries supply-chain concerns. Mirrors composition source model. |
| D-14 | `effects` verification (was Q-5) | **Observe-and-signal in v1: record declared vs `objrun`-actual, surface divergence as a signal; never block runs. Integration-effect "actual" degrades to `unknown` until connectors exist.** | Avoids early false positives (e.g. PR-env deploys); honest about what orun can observe (D-9, S-7). |
| D-15 | Vestigial `objectModel` (was Q-7) | **Flatten at the single `v1` result-envelope cut (SC11); retained until then.** | Avoid churning the CLI output schema twice. Non-blocking. |
| D-16 | Deprecation window (was Q-8) | **apiVersion-anchored: authored `v1alpha1` fields accepted-with-warning across all of `v1`, removed only at `v2`; read-side up-conversion permanent.** | Calendar windows are meaningless pre-1.0; pair with `orun catalog migrate --write`. |
| D-17 | Scorecards scope | **Extracted to `specs/orun-scorecards/` (v2). This epic keeps only foundations: the reserved `lifecycle.maturity` (null until v2), the live plane, and the composition `effects.scorecards` *declaration*.** | Keeps the service-catalog epic focused; scorecards reviewed separately. The `expr`-language call (was Q-3) is decided in that epic (allowlisted predicate set; CEL upgrade path). |
| D-18 | Scaffolding scope | **Extracted to `specs/orun-scaffolding/` (v2). The composition `scaffold` block stays an authored foundation; the engine + `orun create` ship in the v2 epic.** | Self-service create reviewed separately. The engine call (was Q-6) is decided there (`text/template` + sandboxed funcmap). |

## Open questions (need a call before/within the cited milestone)

All eight original open questions are now **locked** (decisions D-11…D-18 above);
the two that belong to extracted epics are owned there.

| # | Question | Resolution |
|---|----------|------------|
| ~~Q-1~~ | non-Component authoring | **RESOLVED → D-11** (derive + `intent.yaml` escape hatch; `api.yaml` deferred L-6). |
| ~~Q-2~~ | live-plane persistence + GC | **RESOLVED → D-12** (derive-on-read; no overlay/`objindex` in v1). |
| ~~Q-3~~ | scorecard `expr` language | **MOVED → `specs/orun-scorecards/` (v2)** — decided there: allowlisted predicate set, CEL as upgrade path. |
| ~~Q-4~~ | extension registry delivery | **RESOLVED → D-13** (compiled-in first-party + in-repo org; remote deferred L-3). |
| ~~Q-5~~ | `effects` verification depth | **RESOLVED → D-14** (observe-and-signal; integration-actual → `unknown` until connectors). |
| ~~Q-6~~ | scaffolding engine | **MOVED → `specs/orun-scaffolding/` (v2)** — decided there: stdlib `text/template` + sandboxed funcmap. |
| ~~Q-7~~ | vestigial `objectModel` | **RESOLVED → D-15** (flatten at the v1 cut). |
| ~~Q-8~~ | deprecation window length | **RESOLVED → D-16** (apiVersion-anchored; remove at v2). |

> **Coordinated elsewhere** (not open here): the **scorecard engine** is owned by
> `specs/orun-scorecards/` (v2) and **golden-path scaffolding** by
> `specs/orun-scaffolding/` (v2), both drafted for separate review (L-8/L-9);
> no-default env resolution, `defaultEnvironment` placement, multi-env
> CI/promotion, single-env enforcement are owned by `specs/orun-env-scoping/` and
> finalized in that epic (L-5); `extends`-precedence, `effects` expressivity
> ceiling, and composition semver bump policy are tracked in `compositions.md`
> §11 and resolved within SC7–SC8.

## Risk register

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Envelope reshape moves every catalog id; `resolverVersion` churn confuses tooling | High | Low | Expected and one-time; `resolverVersion` bump, one memo miss, content addressing re-stabilizes — the CS1 `Path` precedent; documented in `migration.md` §8 (S-1). |
| Ownership claim source ambiguous (authored vs CODEOWNERS vs inherited) | Med | Med | `ownership.source` records origin; precedence authored > CODEOWNERS > inherited; surfaced in `describe` (S-2). |
| Multiple envelope versions coexist on disk | Med | Med | Lazy up-conversion on read; immutable snapshots keep their schema; converter total + round-trip-tested (S-3). |
| Derived Environment/Deployment entities collide with the env-scoping epic | Med | **High** | Env-as-entity additive + read-only here; coordinated at SC4; can supply that epic's identity (S-4, D-10). |
| Scorecards drift into a hand-maintained checklist | Med | Med | Rules evaluate orun-native data first; external-data rules degrade to `unknown`; definitions are versioned config (S-5, D-9). |
| Lossy graph→envelope mapping silently drops a field (the `Path` class) | Med | **High** | Round-trip parity guard over a fixture covering every envelope block, per CS8 precedent (S-6, Invariant 3). |
| Composition `effects` over-claim a gold rating (declare an integration it never creates) | Med | **High** | `effects` are *declared* intent; the live plane records only `objrun`-actual; divergence is a scorecard signal, not silent credit (S-7). |
| Tenancy retrofitted later breaks key/ref formats | Low | **High** | Reserve the `tenant` segment in the grammar at SC0, even though SC12 implements it (S-8). |
| Two authoring formats (component vs composition) drift in style | Med | Med | Shared envelope blocks; one generated-schema discipline; `compositions.md` mirrors `data-model.md` (S-9). |
| Scope is large (SC0–SC12, many milestones) | High | Med | Phased A→F; each milestone independently shippable + parity-gated; SC12 explicitly deferred (`implementation-plan.md`). |
| Examples migration breaks the permissive plan-engine parser | Med | **High** | Both-parsers + generated-schema gate (`migration.md` §6); SC10 keeps both parsers green over `examples/**`. |
| Migration silently changes `internal/affected` selection | Med | **High** | CV-1 repoint is parity-gated (SC2): selection unchanged vs the five-graph path before the old builder is removed. |

## Deferred / needs-later-attention register

> Consolidated record of everything intentionally pushed out of this spec, so
> nothing is lost. Each names the trigger that should pull it back in.

| # | Item | Why deferred | Pull back in when |
|---|------|--------------|-------------------|
| **L-1** | **Attestation/signing + multi-tenant federation** (SC12) — sign the catalog tree id, implement the reserved `tenant` segment, cross-repo federation via `objindex`, `metadata.visibility` + RBAC | Enterprise wedge; designed at a high level (`design.md` §9) but not a v1 requirement; recommended as a separate spec | enterprise / compliance demand materializes |
| **L-2** | **SaaS web UI build** (read-only consumer) | Out of scope to build; the read/action seam is kept open (`orun-catalog-state` consumers, L-5 there) | the web UI is designed |
| **L-3** | **Vendor integration connectors / SaaS fan-out implementation** | The registry defines the *seam* only (SC6); vendor adapters ship separately | a connector is prioritized |
| **L-4** | **R2/S3 remote object driver** | Rides the existing `objremote` closure; no change needed in this spec | a remote-object backend is required |
| **L-5** | **System-wide single-env enforcement** | Breaking run-path change beyond the catalog boundary; owned by `specs/orun-env-scoping/` | the env-scoping epic lands; coordinate at SC4 |
| **L-6** | **Dedicated per-kind authoring files** (`api.yaml`/`resource.yaml`) | Only if Q-1 chooses derive-only for v1 | a kind needs first-class authoring beyond component specs |
| **L-7** | **`objindex` component/entity→execution index** | v1 uses an L2 scan + filter join over `objrun` (the cockpit's existing pattern) | the scan + filter join is measured too slow at scale |
| **L-8** | **Scorecard engine** (production-readiness / maturity) | Extracted for separate review; this epic keeps only the foundations (D-17) | review `specs/orun-scorecards/` (drafted, v2) — gated on SC5/SC8 |
| **L-9** | **Golden-path scaffolding** (`orun create`) | Extracted for separate review; the composition `scaffold` block stays here (D-18) | review `specs/orun-scaffolding/` (drafted, v2) — gated on SC7 |
