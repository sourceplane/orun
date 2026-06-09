# Compositions

> Compositions stop being write-only execution contracts and gain **two roles**:
> *Composition-as-Entity* (catalogued, owned, versioned, deprecatable — a
> `kind: Composition` riding the shared envelope) and *Composition-as-Producer*
> (a golden-path run emits entities, integration join-keys, and scorecard
> signals back into the catalog). This doc fixes the **authoring** shape and the
> **producer model** that feed the resolved `Composition` node in `data-model.md`
> §5: the entity framing, the typed `contract`, the `effects` keystone,
> scaffolding, policy/guardrails, and composability. Schemas it persists are in
> `data-model.md`; the runtime that consumes it is `design.md` §7. RFC 2119
> keywords are binding.

## 1. The reframe

The mental model the rest of this spec turns on: **components are derived from
git** (`component.yaml` → envelope, SC1), **the catalog's live truth is derived
from execution** (deployments, health, scorecards — the live plane, `design.md`
§6), and **compositions define execution** (jobs/profiles, today's
`internal/composition`). So the composition is the *natural producer* of the
catalog's richest data: the artifact that already knows how a thing builds and
deploys is best positioned to declare what running it contributes. This is the
orun moat. Backstage Templates **scaffold and walk away** — a Template emits
files and a catalog-info stub, then has no further relationship to the thing it
created. An orun composition defines `create → build → deploy` as one artifact
and keeps producing catalog truth on every run.

Concretely, compositions gain two roles:

| Role | What it is | Milestone |
|------|------------|-----------|
| **Composition-as-Entity** (§2) | a catalogued `kind: Composition` — metadata, ownership, lifecycle, semver, `usedBy` relations | SC7 |
| **Composition-as-Producer** (§4) | an `effects` block declaring what a run contributes (entities, integration keys, scorecard credit) | SC8 |

The contract (§3) and scaffolding (§5) bind the two: the same typed `inputs`
that power a scaffolding form (§5) also document the golden path's interface
(§3), and the same composition that scaffolds a service later builds and deploys
it (§5).

## 2. Composition-as-Entity

A composition is promoted to a first-class entity sharing the envelope
(`design.md` §4.2, `data-model.md` §2). It MUST carry:

- **metadata / ownership** — `metadata.title/description/labels/tags`, an
  `ownership.owner` (who maintains the golden path). Ownership derives from
  CODEOWNERS over the composition's source path exactly as for components (S-2).
- **lifecycle** — `lifecycle.stage` ∈ `stable | beta | deprecated` (the
  composition-flavoured stage enum; the component enum is
  `experimental|production|deprecated|retired`). A `deprecated` composition MUST
  still resolve, so existing components keep working while a migration window
  runs.
- **version** — **semver** layered *on top of* the existing content
  `ResolvedDigest` (`internal/composition.Composition.ResolvedDigest`). The
  digest stays the integrity primitive; the semver is the human-facing
  compatibility contract. `spec.version` + `spec.digest` both live in the
  resolved node (`data-model.md` §5).
- **relations** — `composes`/`composedBy` to the components on the path, and
  `extends` to a base composition (§7). The reader materializes the inverse
  `usedBy` (`composedBy` per `data-model.md` §3) so the portal answers
  "what is on this golden path?" for free.

Because lifecycle + semver + `usedBy` are now first-class, the portal renders
**"N components on a deprecated composition version"** with no extra authoring —
a query over `composedBy` edges filtered by `lifecycle.stage == deprecated`. The
existing `compositions.lock.yaml` is the on-disk anchor for the version +
deprecation state (§9).

## 3. The typed contract

A composition's `contract` is its **interface** — what it takes, what it
produces, what it needs from the platform, and what it offers to other entities.
This evolves today's single `parameterSchema`
(`internal/composition.Composition.ParameterSchema`) into four typed blocks:

```yaml
contract:
  inputs:                                  # rich parameter schema (evolves ParameterSchema)
    serviceName:  { type: string, required: true, pattern: "^[a-z][a-z0-9-]+$",
                    description: "DNS-safe service name" }
    runtime:      { type: enum, values: [node20, python312], default: node20 }
    cpu:          { type: number, default: 0.25, description: "vCPU request" }
    apiKey:       { type: string, secret: true, description: "upstream token" }
  outputs:                                 # what a successful run yields
    - { name: endpoint,   kind: endpoint,        description: "public URL" }
    - { name: image,      kind: artifact,        description: "built image ref" }
    - { name: deployment, kind: Deployment }     # → derived entity (§4)
  requires:                                # what the platform/component must provide
    integrations: [datadog, pagerduty]
    capabilities: [docker, terraform]
    permissions:  [secrets:read]
  provides:                                # what the path offers the graph
    apis:      [{ name: health, definition: openapi, ref: openapi/health.yaml }]
    resources: [{ name: cache, type: cache }]
```

- **`inputs`** — per-field `type` (`string|number|boolean|enum|object|array`),
  `default`, `values` (enum), `description`, `required`, `pattern`, `secret`.
  `secret: true` fields MUST be redacted in any rendered form and MUST NOT be
  written to an L1 blob (CR-1, `design.md` §3). This is the schema the portal
  renders as a **scaffolding form** (§5) *and* the schema the resolver compiles
  to validate a component's `parameters` (today's `compileSchema`).
- **`outputs`** — declared results by `kind` (`endpoint|artifact|Deployment|
  resourceHandle`). An output naming an entity (`Deployment`) is the
  authoring-side declaration that §4's `effects.graph` emits.
- **`requires`** — `integrations`/`capabilities`/`permissions` the path needs;
  the resolver MAY surface an unsatisfied `requires` as a scorecard signal.
- **`provides`** — `apis`/`resources` the path contributes to `contracts`
  (`data-model.md` §2) on the produced component.

The contract makes compositions **composable**: a consumer reads
`provides`/`requires` to wire one golden path's outputs into another's inputs,
and the portal type-checks the connection before a run.

## 4. The `effects` producer model (the keystone, SC8)

`effects` is the heart of Composition-as-Producer. It declares, **as authored
intent**, what running the golden path contributes to the catalog. The resolver
(`catalogresolve`) folds the declarations into the entity graph; the live plane
(`design.md` §6) records only what `objrun` **actually** produced.

```yaml
effects:
  graph:                                   # → folds into relations + derived entities (SC4)
    - { deploysTo:  Environment }          # run targets an Environment (runsOn/deployedTo)
    - { produces:   Deployment }           # run emits a Deployment record
    - { provisions: Resource, type: datastore, name: primary-db }
    - { exposes:    API, name: health }
  integrations:                            # → auto-populates component integrations join-keys
    - { provider: datadog,   key: service }   # deploy registers the service in Datadog
    - { provider: pagerduty, key: serviceId }
  scorecards:
    satisfies: [runs-tests, emits-sbom, has-rollback]   # rules this path satisfies
```

### 4.1 `effects.graph` — derived entities and edges

Each entry declares a graph effect of running the composition. The resolver
folds it into `relations.json` and the kind-partitioned `entities/` tree
(`data-model.md` §3/§4):

- `deploysTo: Environment` → `deployedTo`/`runsOn` edges to the target
  Environment entity (SC4).
- `produces: Deployment` / `provisions: Resource` / `exposes: API` → the
  corresponding **derived entity** is emitted (`data-model.md` §4: Deployment is
  the L1 record *that a deploy happened*; live health stays L2).

This is *how* SC4's derived entities and SC6's integration keys come to exist at
runtime — the resolver-only derivation lands first, then `effects` supplies the
composition-driven source (`implementation-plan.md`, "Sequencing notes").

### 4.2 `effects.integrations` — the paved road produces correlation keys

Today a developer hand-pastes a Datadog service name and a PagerDuty service id
into config, and they drift. With `effects.integrations`, the composition
declares **"deploying via this path registers the service in Datadog/PagerDuty"**,
naming the join-`key` shape it produces. The resolver auto-populates the
component's `integrations` block (`data-model.md` §2) — the paved road
**produces** correlation keys instead of asking humans to copy them. Only the
*declaration* (the key shape) is L1; the resolved *value* is L2 (CR-1).

### 4.3 `effects.scorecards.satisfies` — the adoption flywheel

`satisfies` lists the production-readiness rule ids the golden path inherently
meets (it runs tests, emits an SBOM, has a rollback step). Every component on
that composition **inherits** scorecard credit for those rules from
`internal/scorecard` (`design.md` §6) — choosing the paved road raises a
service's maturity *for free*. This is the adoption flywheel: the more a
composition satisfies, the more attractive it is, the more components adopt it,
the more of the fleet is gold-rated.

> The `satisfies` list is an **authored declaration** carried into the resolved
> Composition node by SC8; its **evaluation and credit** are performed by the
> extracted v2 epic `specs/orun-scorecards/`.

### 4.4 Declared vs actual (S-7, MUST)

`effects` are **DECLARED intent**, never silently trusted. The live plane MUST
record only what `objrun` actually produced on a given run. When a composition
declares `effects.integrations: datadog` but a run produces no Datadog
registration — or declares `satisfies: [runs-tests]` but no test step ran —
the **declared-vs-actual divergence MUST surface as a scorecard signal**, not as
silent credit. This is sharpness S-7 in `design.md` §12: a composition cannot
over-claim its way to a gold rating.

## 5. Golden-path scaffolding

> **The scaffolding *engine* is extracted to `specs/orun-scaffolding/` (v2)** for
> later review. This section defines only the composition-side **`scaffold`
> block** — an authored declaration that stays a foundation in this epic. The
> renderer (`internal/scaffold`), `orun create`, and `orun compositions scaffold`
> ship in the v2 epic.

A composition MAY carry a `scaffold` template — starter files plus a generated
`component.yaml` — so **self-service "create a new service"** uses the *same*
composition that later builds and deploys it:

```yaml
scaffold:
  files:                                   # rendered with contract.inputs as the model
    - { from: templates/handler.ts.tmpl, to: "src/{{ .serviceName }}.ts" }
    - { from: templates/component.yaml.tmpl, to: component.yaml }
  postCreate: [git-init]
```

`internal/scaffold` (new, SC9) renders the templates against the values a
developer supplies for `contract.inputs` (the same schema the portal renders as
a form, §3). The generated `component.yaml` MUST resolve cleanly against the
catalog — scaffolding produces a *catalog-valid* component, not a stub.

The contrast with Backstage is the whole point: Backstage's **Template is a
separate artifact** from the runtime that builds the service, owned and versioned
independently, free to drift. orun's `create → build → deploy` is **one
composition artifact, one owner, one version** — the thing that scaffolds you in
is the thing that keeps producing your catalog truth (§4).

## 6. Policy & guardrails

Today's `model.ProfilePolicies` is a flat trio of booleans
(`requireCleanGitTree`, `requirePinnedTerraformVersion`, `requireApproval`). It
evolves into a **declarative, inherited** `policy` block — org policy codified
*once* in the golden path and inherited by every component on it (the README
"policy-aware, non-negotiable at compile time" promise):

```yaml
policy:
  allowedEnvironments: [dev, staging, production]
  requiredInputs: [serviceName, runtime]
  gates:
    production:
      requireApproval: true
      requireGreenScorecard: gold          # block prod deploy below gold
      requireCleanGitTree: true
```

- **`allowedEnvironments`** — the environments this path may target; a component
  on the path MUST NOT bind an environment outside this set.
- **`requiredInputs`** — `contract.inputs` keys that MUST be supplied (stricter
  than per-field `required` when org policy demands it).
- **`gates`** — per-environment enforcement. `requireApproval` and
  `requireCleanGitTree` carry over from `ProfilePolicies`;
  `requireGreenScorecard: <level>` is new and ties policy to the scorecard engine
  (`design.md` §6). These compile down to the existing enforcement primitive:
  `model.PromotionGate` / `PlanPromotionGate` already exist (`PlanJob.Gates`), so
  policy authoring adds a *declarative front end* over a mechanism the planner
  already enforces — no new runtime gate machinery.

Because policy lives on the composition and is inherited, a component author
cannot opt out of an org guardrail by editing their own `component.yaml`.

## 7. Composability / layering

Compositions compose via `extends`: an **org base** composition + a **team
overlay**. This builds directly on today's `ExecutionProfiles` + `ProfileJobSpec`
/ `StepOverrides` (`model.ProfileStepPatch`), generalized to the whole authoring
envelope:

```yaml
extends: default/orun/org-base            # inherit contract/effects/policy/jobs
# team overlay overrides/extends below
```

- Resolution is a **deep merge**, base then overlay: `contract.inputs` and
  `effects.*` union; `policy.gates` overlay-wins per key; `jobs`/`profiles` merge
  via the existing step-override mechanism.
- `extends` MUST be acyclic; the resolver MUST reject a cycle.
- The resolved node records the chain so the portal shows the **inheritance
  lineage** (base → overlay → effective). The `extends` target is itself a
  `Composition` entity, linked by an `extends` relation.

This lets a platform team set `policy.gates.production.requireApproval: true`
once on `org-base`, and every team composition inherits it.

## 8. The evolved authoring envelope

A complete `kind: Composition` authoring file (`apiVersion: orun.io/v1`),
symmetric with the component envelope so authors learn **one model**:

```yaml
apiVersion: orun.io/v1
kind: Composition
metadata:
  name: cloudflare-worker
  title: Cloudflare Worker
  description: Edge worker golden path
  labels: { runtime: cloudflare }
  tags: [edge, paved-road]
ownership:
  owner: group:platform-edge               # else derived from CODEOWNERS (S-2)
lifecycle:
  stage: stable                            # stable | beta | deprecated
version: 2.3.0                              # semver, atop the content digest
extends: default/orun/org-base             # composability (§7)

applies:
  kind: Component
  types: [cloudflare-worker]               # which component types this builds

contract:                                  # the typed interface (§3)
  inputs:
    serviceName: { type: string, required: true, pattern: "^[a-z][a-z0-9-]+$" }
    runtime:     { type: enum, values: [node20], default: node20 }
  outputs:
    - { name: endpoint, kind: endpoint }
    - { name: deployment, kind: Deployment }
  requires: { integrations: [datadog], capabilities: [docker] }
  provides: { apis: [{ name: health, definition: openapi, ref: openapi/health.yaml }] }

effects:                                   # the producer model (§4)
  graph:
    - { deploysTo: Environment }
    - { produces: Deployment }
  integrations:
    - { provider: datadog, key: service }
  scorecards:
    satisfies: [runs-tests, emits-sbom]

scaffold:                                  # self-service create (§5)
  files:
    - { from: templates/component.yaml.tmpl, to: component.yaml }

policy:                                    # inherited guardrails (§6)
  allowedEnvironments: [dev, staging, production]
  gates:
    production: { requireApproval: true, requireGreenScorecard: gold }

# --- the unchanged execution core (today's CompositionDocumentSpec) ---
spec:
  defaultJob: build-deploy
  defaultProfile: standard
  jobs: [ { name: build-deploy, steps: [ ... ] } ]
  executionProfiles:
    standard: { jobs: { build-deploy: { stepsEnabled: [build, deploy] } } }
```

The `metadata`/`ownership`/`lifecycle`/`contract`/`effects`/`policy` blocks
mirror the component envelope's `metadata`/`ownership`/`lifecycle`/`contracts`/
`integrations` blocks (S-9). The `spec` block is today's `CompositionDocumentSpec`
verbatim — jobs, profiles, and step overrides are **unchanged**, so existing
compositions remain valid and gain the new blocks additively.

## 9. Distribution & on-disk

Distribution stays **OCI-native** and unchanged in mechanism:

- Compositions are packaged and pushed as OCI artifacts (`stack.yaml` /
  `CompositionPackage`), pulled by `orun fetch`, cached by digest — the existing
  `internal/composition` OCI path (`fetchAndExtractOCICompositionsLayer`).
- `compositions.lock.yaml` (`model.CompositionLock`) records **semver +
  deprecation** alongside the existing `resolvedDigest` + `exports` per source —
  so a lock pins both the integrity digest and the human compatibility version.
- Each **used** composition is projected as
  `entities/Composition/<name>.json` (the resolved node, `data-model.md` §5),
  participating in the catalog like any other kind.

## 10. Package impact & phasing

| Area | Package | Change |
|------|---------|--------|
| Composition entity + contract | `internal/composition` | add `contract`/`lifecycle`/`version`/`extends` to the resolved `Composition`; emit the entity node |
| Authoring model | `internal/model` (`composition.go`) | evolve `ParameterSchema` → typed `contract.inputs`; evolve `ProfilePolicies` → `policy`; add `effects` types |
| Effects fold | `internal/catalogresolve` | fold `effects.graph`/`integrations`/`scorecards` into relations + derived entities (stays pure — inputs provided, `design.md` §10) |
| Deployment emission | `internal/objplan` | emit `Deployment`/`Environment` from `objrun` (the actual side of S-7) |
| Scorecard credit | `internal/scorecard` (**v2 → `specs/orun-scorecards/`**) | consume `effects.scorecards.satisfies` contributions; evaluate level |
| Scaffolding | `internal/scaffold` (**v2 → `specs/orun-scaffolding/`**) | render `scaffold` templates against `contract.inputs` |
| Composition kind | `catalogmodel` / `nodes` | the `Composition` kind + resolved node shape (`data-model.md` §5) |

**Phasing** (`implementation-plan.md` SC7–SC9):

- **SC7 — envelope + contract.** Composition-as-Entity: the `kind: Composition`
  envelope, typed `contract`, semver, `lifecycle`; project to
  `entities/Composition/<name>.json`; evolve the lock.
- **SC8 — effects → derivation (the keystone).** `effects.graph`/`integrations`;
  the `effects.scorecards` declaration is carried but **evaluated in the v2
  scorecards epic** (`specs/orun-scorecards/`). Declared-vs-actual divergence as a
  signal (S-7). Depends on SC4 (derived entities), SC6 (integrations), SC7, + the
  SC5 live plane.
- **SC9 — scaffolding (extracted, v2).** `internal/scaffold`, `orun create`, and
  `orun compositions scaffold` move to `specs/orun-scaffolding/`; only the
  authored `scaffold` block stays here (§5).

## 11. Open edges

The following are unresolved and tracked in `risks-and-open-questions.md`:

- **`extends` precedence vs source `resolution.precedence`** — how composition
  inheritance interacts with the existing multi-source precedence/binding
  resolution (`internal/composition.selectDefaultCompositions`).
- **`effects` expressivity ceiling** — whether `effects.graph` needs conditional
  effects (an effect that only fires for certain profiles/environments) or stays
  declaratively flat in v1.
- **Semver bump policy** — what counts as a breaking change to a composition's
  `contract` (input removal? a tightened `pattern`?) and whether the resolver
  should enforce semver discipline against the prior published version.
- **Scaffold template engine** — the templating language and its sandboxing for
  `internal/scaffold` are specified in the extracted v2 epic
  `specs/orun-scaffolding/`.
