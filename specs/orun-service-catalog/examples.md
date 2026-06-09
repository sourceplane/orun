# Examples Migration

> The repo dogfoods the new model. Every `examples/**` `component.yaml` moves to
> the `orun.io/v1` envelope; the example compositions move to `kind: Composition`
> authoring with `contract`/`effects`/`policy`; and a `CODEOWNERS` is added so
> ownership **derives** (`ownership.source = CODEOWNERS`) rather than being
> hand-authored — proving the convention-over-configuration path end to end. This
> doc is the in-repo scope for **SC10** (`implementation-plan.md`): the inventory,
> representative before/after, the CODEOWNERS mapping, the System/Domain modeling,
> and the acceptance gates. It scopes the changes; it does not make them. RFC 2119
> keywords are binding on the acceptance contract (§7).

## 1. Goal & scope

The workspace at `examples/intent.yaml` (`kind: Intent`, `multi-environment-platform`)
discovers components under `apps/ infra/ deploy/ charts/ packages/ website/`
(`discovery.roots`) and sources compositions from `./compositions`. SC10 migrates
**that whole tree** to exercise the catalog as a developer portal:

1. **Components → `orun.io/v1` envelope.** Each `component.yaml` keeps its
   `apiVersion: sourceplane.io/v1` semantics but adopts the split envelope
   (`metadata`/`ownership`/`lifecycle`/`spec`/`relations`/`contracts`), adding
   `lifecycle.stage`+`tier`, `system`/`domain` membership, and `dependsOn`/
   `providesApi` where real. Owners are **left to CODEOWNERS** (§5).
2. **Compositions → `kind: Composition` authoring.** The eleven exported
   compositions under `examples/compositions/compositions/**` adopt the evolved
   authoring envelope (`compositions.md` §8): typed `contract`, `effects`
   (`scorecards.satisfies` + `integrations`), and inherited `policy`. The
   unchanged execution core (`jobs`/`profiles`/`schema`) stays verbatim.
3. **CODEOWNERS added so ownership derives.** A new `examples/CODEOWNERS` (none
   exists in the repo today) maps example dirs to example `group:*` owners; the
   resolver assigns `ownership.owner` by longest-prefix match with
   `ownership.source = CODEOWNERS` (S-2). Nothing is hand-authored on the
   components.

**Acceptance (detailed in §7):** `make examples-validate` / `make examples-plan`
pass; `orun catalog refresh` over `examples/` yields a populated **multi-kind**
catalog — Components + derived System/Domain/Environment + Composition entities —
with resolved owners. (Scorecard *levels* land with the v2 epic
`specs/orun-scorecards/`; `lifecycle.maturity` is reserved/`null` in this epic.)

## 2. Inventory

Fifteen tracked `component.yaml` files (`git ls-files 'examples/**/component.yaml'`),
grouped by discovery root. Proposed `kind`/`system`/`domain` per §6.

### Components

| Path | Current `spec.type` | → kind | Proposed system | Proposed domain |
|------|---------------------|--------|-----------------|-----------------|
| `apps/identity-worker/component.yaml` | `cloudflare-worker` | Component (worker) | `identity` | `platform` |
| `apps/projects-worker/component.yaml` | `cloudflare-worker` | Component (worker) | `projects` | `platform` |
| `apps/api-edge/component.yaml` | `cloudflare-worker-turbo` | Component (worker) | `platform-runtime` | `platform` |
| `apps/admin-console/component.yaml` | `cloudflare-pages-turbo-terraform` | Component (website) | `platform-ui` | `platform` |
| `apps/web-console/component.yaml` | `cloudflare-pages-turbo` | Component (website) | `platform-ui` | `platform` |
| `website/component.yaml` | `cloudflare-pages` | Component (website) | `docs` | `platform` |
| `infra/cloudflare-pages-terraform/component.yaml` | `cloudflare-pages-terraform` | Component (website) | `docs` | `platform` |
| `infra/infra-1/component.yaml` | `terraform` | Component (service/infra) | `foundation` | `platform` |
| `infra/infra-2/component.yaml` | `terraform` | Component (service/infra) | `foundation` | `platform` |
| `packages/shared/component.yaml` | `turbo-package` | Component (library) | `packages` | `platform` |
| `packages/sdk/component.yaml` | `turbo-package` | Component (library) | `packages` | `platform` |
| `charts/hello-chart/chart/component.yaml` | `helm-chart` | Component (service) | `identity` | `commerce` |
| `charts/chart-2/chart/component.yaml` | `helm-chart` | Component (service) | `commerce` | `commerce` |
| `deploy/hello-deploy/component.yaml` | `helm-values` | Component (service) | `identity` | `commerce` |
| `deploy/deploy-service2/component.yaml` | `helm-values` | Component (service) | `commerce` | `commerce` |

Existing `dependsOn` edges (carry over to `relations` verbatim): `cluster-addons →
network-foundation`; `identity-portal-chart → identity-portal-values`;
`identity-portal-values → network-foundation`; `commerce-checkout-chart →
commerce-checkout-values`; `commerce-checkout-values → cluster-addons`.

### Compositions

Eleven exported compositions (`examples/.orun/compositions.lock.yaml`
`exports`), authored under `examples/compositions/compositions/<name>/composition.yaml`
(each with sibling `schema.yaml`, `jobs/`, `profiles/`). The smoke-test
compositions under `remote-state-matrix/` and `github-artifact-demo/` use the
single-file `compositions.yaml` form and are **out of SC10 scope** (conformance
fixtures, not golden-path examples).

| Path (`examples/compositions/compositions/`) | Name | `applies.types` |
|----------------------------------------------|------|-----------------|
| `cloudflare-worker/composition.yaml` | `cloudflare-worker` | `cloudflare-worker` |
| `cloudflare-worker-turbo/composition.yaml` | `cloudflare-worker-turbo` | `cloudflare-worker-turbo` |
| `cloudflare-pages/composition.yaml` | `cloudflare-pages` | `cloudflare-pages` |
| `cloudflare-pages-turbo/composition.yaml` | `cloudflare-pages-turbo` | `cloudflare-pages-turbo` |
| `cloudflare-pages-terraform/composition.yaml` | `cloudflare-pages-terraform` | `cloudflare-pages-terraform` |
| `cloudflare-pages-turbo-terraform/composition.yaml` | `cloudflare-pages-turbo-terraform` | `cloudflare-pages-turbo-terraform` |
| `terraform/composition.yaml` | `terraform` | `terraform` |
| `helm-chart/composition.yaml` | `helm-chart` | `helm-chart` |
| `helm-values/composition.yaml` | `helm-values` | `helm-values` |
| `turbo-package/composition.yaml` | `turbo-package` | `turbo-package` |
| `workspace/composition.yaml` | `workspace` | `workspace` |

## 3. Component before/after

Two representative components: an edge **worker** and a **terraform** infra
component.

### 3.1 `apps/identity-worker/component.yaml` (cloudflare-worker)

**Before** (real, current):

```yaml
apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: identity-worker
spec:
  type: cloudflare-worker
  domain: platform-identity
  subscribe:
    environments:
      - { name: dev, profile: pull-request }
      - { name: staging, profile: verify }
      - { name: production, profile: deploy }
  parameters: { installCommand: pnpm install --no-frozen-lockfile, ... nodeVersion: "20" }
  labels: { team: platform, layer: runtime, surface: identity, runtime: cloudflare }
```

**After** (`orun.io/v1` envelope; owner left to CODEOWNERS):

```yaml
apiVersion: orun.io/v1
kind: Component
metadata:
  name: identity-worker
  title: Identity Worker
  labels: { team: platform, layer: runtime, surface: identity, runtime: cloudflare }
  tags: [edge, identity]
lifecycle:
  stage: production            # experimental | production | deprecated | retired
  tier: tier-1
spec:
  type: cloudflare-worker
  system: identity             # → partOf System (§6)
  domain: platform             # → partOf Domain
  subscribe:
    environments:
      - { name: dev, profile: pull-request }
      - { name: staging, profile: verify }
      - { name: production, profile: deploy }
  parameters: { installCommand: pnpm install --no-frozen-lockfile, ... nodeVersion: "20" }
contracts:
  provides:
    - { api: default/example-platform/identity-api, definition: openapi,
        ref: openapi/identity.yaml, visibility: internal, stability: stable }
# ownership intentionally absent → derived from CODEOWNERS (source: CODEOWNERS)
# relations (partOf/dependsOn/providesApi/ownedBy) materialized by the resolver
```

### 3.2 `infra/infra-1/component.yaml` (terraform)

**Before** (real, current):

```yaml
apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: network-foundation
spec:
  type: terraform
  domain: platform-foundation
  subscribe:
    environments:
      - { name: development, profile: pull-request }
      - { name: staging, profile: verify }
      - { name: production, profile: release }
  parameters: { stackName: network-foundation, terraformDir: ., terraformVersion: 1.9.8 }
  labels: { team: platform, layer: foundation, stack: network }
```

**After**:

```yaml
apiVersion: orun.io/v1
kind: Component
metadata:
  name: network-foundation
  title: Network Foundation
  labels: { team: platform, layer: foundation, stack: network }
  tags: [infra, foundation]
lifecycle:
  stage: production
  tier: tier-1                 # foundational infra → strictest scorecard
spec:
  type: terraform
  system: foundation
  domain: platform
  subscribe: { environments: [ ... ] }   # unchanged
  parameters: { stackName: network-foundation, terraformDir: ., terraformVersion: 1.9.8 }
contracts:
  provides:
    - { api: default/example-platform/network, definition: terraform-outputs, visibility: internal }
# cluster-addons already declares `dependsOn: [{ component: network-foundation }]`;
# that edge migrates verbatim into relations (dependsOn/dependencyOf).
```

## 4. Composition before/after

`cloudflare-worker` is the representative golden path (it builds + deploys the
two `cloudflare-worker` apps, `identity-worker` and `projects-worker`).

**Before** (real, `examples/compositions/compositions/cloudflare-worker/composition.yaml`):

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: Composition
metadata:
  name: cloudflare-worker
spec:
  type: cloudflare-worker
  description: Cloudflare Worker build, verify, and deploy pipeline
  schemaRef: { name: cloudflare-worker-component }
  defaultJob: verify-deploy
  defaultProfile: verify
  jobs:    [ { name: verify-deploy, templateRef: { name: cloudflare-worker-verify-deploy } } ]
  profiles:
    - { name: pull-request, profileRef: { name: cloudflare-worker-pull-request } }
    - { name: verify,       profileRef: { name: cloudflare-worker-verify } }
    - { name: deploy,       profileRef: { name: cloudflare-worker-deploy } }
```

**After** (`compositions.md` §8 — contract/effects/policy added; execution core
verbatim under `spec`):

```yaml
apiVersion: orun.io/v1
kind: Composition
metadata:
  name: cloudflare-worker
  title: Cloudflare Worker
  description: Edge worker build, verify, and deploy golden path
  labels: { runtime: cloudflare }
  tags: [edge, paved-road]
lifecycle:
  stage: stable                # stable | beta | deprecated
version: 1.0.0                  # semver atop the content digest in the lock
applies:
  kind: Component
  types: [cloudflare-worker]
contract:                       # evolves today's schema.yaml (cloudflare-worker-component)
  inputs:
    installCommand: { type: string, required: true }
    buildCommand:   { type: string, required: true }
    deployCommand:  { type: string, required: true }
    nodeVersion:    { type: string, required: true }
    productionBranch: { type: string, required: true }
  outputs:
    - { name: endpoint,   kind: endpoint }
    - { name: deployment, kind: Deployment }
effects:                        # the producer model (§4 of compositions.md)
  graph:
    - { deploysTo: Environment }
    - { produces:  Deployment }
  scorecards:
    satisfies: [runs-tests, has-rollback]   # the verify + deploy profiles
policy:
  allowedEnvironments: [dev, staging, production]
  gates:
    production: { requireApproval: true }
spec:                           # unchanged execution core (today's CompositionDocumentSpec)
  defaultJob: verify-deploy
  defaultProfile: verify
  jobs:    [ { name: verify-deploy, templateRef: { name: cloudflare-worker-verify-deploy } } ]
  profiles:
    - { name: pull-request, profileRef: { name: cloudflare-worker-pull-request } }
    - { name: verify,       profileRef: { name: cloudflare-worker-verify } }
    - { name: deploy,       profileRef: { name: cloudflare-worker-deploy } }
```

The `terraform` composition migrates the same way, with
`effects.scorecards.satisfies: [runs-tests]` (validate) and no deploy gate
(its profiles are `pull-request`/`verify`/`release`).

## 5. CODEOWNERS

No `CODEOWNERS` exists anywhere in the repo today (`find . -iname CODEOWNERS` is
empty). SC10 adds `examples/CODEOWNERS` so `ownership.source = CODEOWNERS`
resolves by longest-prefix match over `identity.path` — owners MUST NOT be
hand-authored on components. The `group:*` keys register as `entities/Group/`
endpoints in the relation graph (`data-model.md` §3).

```gitignore
# examples/CODEOWNERS — example ownership, longest-prefix wins
*                       @example/platform

apps/identity-worker/   @example/identity-team
charts/hello-chart/     @example/identity-team
deploy/hello-deploy/    @example/identity-team

apps/projects-worker/   @example/projects-team

charts/chart-2/         @example/commerce-team
deploy/deploy-service2/ @example/commerce-team

apps/admin-console/     @example/ui-team
apps/web-console/       @example/ui-team

infra/                  @example/foundation-team
packages/               @example/packages-team
website/                @example/docs-team
```

The resolver maps `@example/<team>` → `group:<team>` and records
`ownership.owner: group:identity-team`, `ownership.source: CODEOWNERS`. An
authored `metadata.owner` (none in the examples) would override with `source:
authored` (S-2 precedence).

## 6. System / Domain modeling

The examples already carry a `spec.domain` string (`platform-identity`,
`commerce-checkout`, …). SC10 reshapes these into first-class **System** and
**Domain** entities (`data-model.md` §4) so the catalog exercises multi-kind +
cross-kind relations. One **Domain** with a few **Systems** beneath it:

| Domain | System | Member components |
|--------|--------|-------------------|
| `platform` | `identity` | `identity-worker`, `identity-portal-chart`, `identity-portal-values` |
| `platform` | `projects` | `projects-worker` |
| `platform` | `platform-runtime` | `api-edge-worker` |
| `platform` | `platform-ui` | `admin-console-pages-git`, `web-console-pages` |
| `platform` | `docs` | `docs-site-direct-upload`, `docs-site-git-source` |
| `platform` | `foundation` | `network-foundation`, `cluster-addons` |
| `platform` | `packages` | `platform-shared`, `platform-sdk` |
| `commerce` | `commerce` | `commerce-checkout-chart`, `commerce-checkout-values` |

Each component's `spec.system` yields a `partOf System` relation; each System's
`spec.domain` yields a `partOf Domain` relation; the resolver materializes the
inverse `hasPart` edges and the per-System/Domain member counts (`data-model.md`
§4). This populates `entities/System/`, `entities/Domain/`, and `entities/Group/`
alongside `entities/Component/`, so `catalog list --kind System|Domain|Group`
returns non-empty. (Systems may be declared in `spec.system` on each component,
or via dedicated `system.yaml` files — either path is acceptable for SC10.)

## 7. Acceptance checklist

The concrete SC10 gates (`implementation-plan.md` "Done when"):

1. **Plan/validate green.** `make examples-validate` (`orun validate --intent
   examples/intent.yaml`) and `make examples-plan` (`orun plan --intent …`) MUST
   pass against the migrated tree — both `component.yaml` parsers (strict
   `catalogmodel.ComponentYAML.OpenSchema()` + permissive plan-engine) accept
   every migrated file, and `make examples-gha-smoke` still runs.
2. **Refresh populates a multi-kind catalog.** `orun catalog refresh` over
   `examples/` MUST yield: **15 Components**, the **8 Systems + 2 Domains** of §6,
   the derived **Environments** (`dev`, `development`, `preview`, `staging`,
   `production` from `intent.yaml`), the **11 Composition** entities (§2), and the
   `group:*` **Group** entities from CODEOWNERS — `catalog.json.countsByKind`
   reflects each.
3. **Owners resolve.** Every Component/Composition has `ownership.owner !=
   unknown` with `ownership.source: CODEOWNERS` (no hand-authored owner remains);
   `catalog describe` surfaces the claim origin.
4. **Scorecard foundations present (evaluation is v2).** Compositions carry their
   `effects.scorecards.satisfies` *declaration* and every Component exposes the
   reserved `lifecycle.maturity` field (`null` here). Scorecard *evaluation* —
   producing a `bronze|silver|gold` level — lands with the extracted v2 epic
   `specs/orun-scorecards/` (CR-1 — L2-sourced, never authored).
5. **Relations carry over.** All five existing `dependsOn` edges (§2) appear as
   `dependsOn`/`dependencyOf` in `relations.json`; `partOf`/`hasPart` link
   components → Systems → the Domain; `composedBy`/`composes` link compositions to
   the components they build.
6. **CODEOWNERS added.** `examples/CODEOWNERS` exists and drives ownership; no
   component or composition hand-authors `ownership.owner`.
7. **No legacy-only fields remain** except those covered by lazy up-conversion —
   migrated files are `apiVersion: orun.io/v1`; any `v1alpha1` artifact left in
   the conformance fixtures (`remote-state-matrix/`, `github-artifact-demo/`) MUST
   up-convert cleanly on read, never requiring an in-place rewrite (S-3 / SC11).
8. **`examples.md` matches reality.** The before/after blocks above MUST stay
   byte-faithful to the migrated files (the parity discipline of `test-plan.md`).
