---
title: The resource model
description: Every behavior in orun is declared in a typed apiVersion/kind document or derived into one. This page is the map — authored kinds, compiled artifacts, recorded entities, and the platform objects they run under.
---

orun does not have configuration files in the loose sense — it has
**resources**: typed documents with a stable schema, a declared version, and a
defined owner. Everything the system does is either *declared* in one of these
documents, *compiled* from them, or *recorded* about them. There is no
behavior that lives outside the model.

Every resource shares the same envelope:

```yaml
apiVersion: <group>/<version>   # which schema governs this document
kind: <Kind>                    # what sort of thing it is
metadata:                       # identity — name, labels
  name: …
spec:                           # the declaration itself
  …
```

If you have used Kubernetes, this is deliberate and familiar. The envelope is
what makes the system schema-driven: every document can be validated before it
is used (`orun validate`), evolved behind a version (`v1alpha1` → `v1`), and
read by tools that don't understand its internals.

## Three classes of resource

The documents fall into three classes, and the class tells you who writes it
and whether it can change (a fourth class, the platform objects a document
runs under, is [below](#platform-objects--the-tenancy)):

| Class | Who writes it | Mutability | Analogy |
|---|---|---|---|
| **Authored** | Humans, in git | Edited freely; reviewed like code | Kubernetes `spec` — desired state |
| **Compiled** | The planner | Immutable; regenerated, never edited | A build artifact |
| **Recorded** | The runtime & resolver | Immutable; content-addressed | Kubernetes `status` — observed state |

This is orun's version of the desired-state/observed-state split. Authored
documents say what *should* be true. Recorded objects say what *is* true —
what was resolved, planned, executed, and deployed. The two never mix: orun
never rewrites an authored file, and you never edit a recorded object.

## Authored resources — the declarations

### Intent

One per repository. The control document: environments, policies, trigger
bindings, discovery roots, composition sources.

```yaml
apiVersion: sourceplane.io/v1
kind: Intent
metadata:
  name: shop-platform
spec:
  discovery: { roots: [apps/, infra/] }
  environments: { … }
  groups: { … }
  automation: { triggerBindings: { … } }
  compositions: { sources: [ … ] }
```

→ [Intent model](../concepts/intent-model.md)

### Component

One per deployable unit, living next to its code. Declares identity (`name`,
`system`, `lifecycle`), a *type* binding it to a composition, environment
subscriptions, typed inputs, dependencies — and, optionally, catalog
enrichment: `integrations`, `links`, `docs`, and namespaced `extensions`.

```yaml
apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: api-edge-worker
spec:
  type: cloudflare-worker-turbo
  system: edge-gateway
  lifecycle: production
  subscribe:
    environments:
      - { name: staging, profile: verify }
  parameters: { nodeVersion: "20" }
  dependsOn:
    - component: identity-foundation
```

→ [Intent model § components](../concepts/intent-model.md)

### Composition (and its split kinds)

The execution contract for a component *type*, authored by the platform team
and distributed as a versioned package (directory, archive, or OCI artifact).
A composition is itself a small family of kinds:

| Kind | Declares |
|---|---|
| `Composition` | The type's facade: schema ref, jobs, profiles, semver `version`, `lifecycle`, `effects` |
| `ComponentSchema` | JSON Schema validating component inputs for this type |
| `JobTemplate` | The steps — commands, phases, capabilities, timeouts |
| `ExecutionProfile` | Context overlays: what a `pull-request` run includes vs a `release` run |

The `effects` block is the composition declaring its *consequences* — the
integrations, Resources, and APIs that running this golden path contributes to
the catalog.

→ [Compositions](../concepts/compositions.md) ·
[Composition contract](../compositions/composition-contract.md)

### CODEOWNERS

Not an orun file — and that's the point. orun treats the `CODEOWNERS` file you
already maintain as a declarative ownership source: when a component authors
no owner, the resolver derives one from the last matching rule and records the
provenance (`ownership.source: CODEOWNERS`).

### The newer authored kinds

The same envelope carries everything orun has learned to declare since the
delivery core. Each is validated at load against a closed schema; unknown
fields are refused rather than ignored.

| Kind | `apiVersion` | Lives in | Declares |
|---|---|---|---|
| `Blueprint` | `orun.dev/v1` | A blueprint file passed to `orun new --blueprint`; a baseline's build document | Sources, modules placed by `template`, `copy`, or `consume`, `dependsOn` edges, inputs, phases with hooks and gates |
| `Workflow` | `orun.dev/v1` | A workflow file; `orun workflow validate\|run\|view` | DAG steps with `run:` argv, `action:` + `with:`, or a nested `workflow:`, joined by `needs:`, with inputs, outputs, `poll:`/`until:`, and approval gates |
| `TaskContract` | `orun.io/v1` | `tasks/<KEY>.TaskContract.yaml`, or a key-less template beside a flow | `goal`, `affects` (the component ceiling), `doneWhen`, `gates`, `designRefs`, `deps`, `secrets`, `envs`; sealed by sha256 over canonical JSON wherever it travels |
| `SecretPolicy` | `orun.io/v1` | `policies/*.SecretPolicy.yaml` in the repository or a Stack | Portable Layer-2 secret-access rules; `orun policy` lints, tests, and pushes them |
| `agent-type` | `orun.io/v1` | `agents/<name>.md` — frontmatter plus a persona body | `harness`, `model`, `runtime`, `tools` (`allow`/`ask`/`deny`), `mayAffect`, `secrets.use`, `owner`, `extends` |
| Repo declaration | — | Inside `intent.yaml` | The repository describing itself — display name, description, tags, docs, links — projected into the catalog's `Repo` entity |

→ [`orun new`](../cli/orun-new.md) · [`orun workflow`](../cli/orun-workflow.md) ·
[The task plane](../concepts/task-plane.md) · [Secrets](../concepts/secrets.md) ·
[The agent runtime](../concepts/agent-runtime.md)

## Compiled resources — the decisions

### Plan

`plan.json`: the immutable DAG the compiler materializes from the authored
resources plus the trigger context. Jobs, rendered steps, dependency edges,
merged inputs, selection metadata — every decision explicit, nothing left for
the runner to interpret. Plans are diffed in review, archived as deployment
records, and replayed against any runner.

→ [Plan DAG](../concepts/plan-dag.md) ·
[Plan schema](../reference/plan-schema.md)

### Composition lock

`compositions.lock.yaml` pins every composition source to a digest, the same
way a package lockfile pins dependencies. Floating tags fail the lock in CI —
determinism requires that "which contract" is never a runtime question.

→ [Stacks](../concepts/stacks.md)

### Provenance lock

`.orun/provenance.lock` (`orun.dev/v1`, kind `ScaffoldProvenance`) is what
`orun new` writes into a scaffolded tree: the blueprint digest, each source
digest, a secret-free hash of the inputs, and the mode and target of every
module. It is the base `orun new upgrade` three-way merges against, so a
product built from a [baseline](../concepts/baselines.md) is upgradable rather
than a fork.

## Recorded resources — the facts

Everything orun observes is persisted as **immutable, content-addressed
objects** under `.orun/objectmodel/` — a git-shaped store: hash-named objects,
plus a small mutable layer of named refs (`catalogs/current`,
`executions/latest`). Identical content is stored once; history is never
rewritten.

→ [State model](../concepts/state-model.md)

### Catalog entities (`orun.io/v1`)

The resolver projects the authored sources into a typed **service catalog**.
Each entity — whether authored directly or derived — is carried in a shared
`orun.io/v1` envelope: `metadata`, `ownership` (with provenance), `lifecycle`,
kind-specific `spec`, typed `relations`, `contracts`, `integrations`, `docs`,
`links`, `provenance`, and `extensions`.

| Kind | Comes from |
|---|---|
| `Component` | `component.yaml`, resolved and enriched |
| `System`, `Domain`, `Group` | Derived from component declarations and ownership |
| `API`, `Resource` | Derived from contracts and composition `effects` |
| `Environment` | Derived from the intent's environment matrix |
| `Composition` | Derived from the composition lock |
| `Repo` | The repository's own declaration in `intent.yaml`; one per snapshot, carrying the overview doc the console renders |

→ [Service catalog](../concepts/service-catalog.md)

### Agent and task records

The agent runtime and the task plane add node kinds to the same graph,
addressed the same way:

| Object | Ref | Sealed from |
|---|---|---|
| `AgentTypeSnapshot` | `refs/agents/types/<name>/latest` | An `agents/<name>.md` file — the capability envelope canonicalized, the persona as a body blob, the base literacy pinned via `extends` |
| Base literacy | `refs/agents/literacy/<version>` | The document `orun agent context` prints, when sealed with `--seal` |
| `AgentSessionSnapshot` | `refs/agents/sessions/<as_id>` | A session's append-only event log, chained and sealed on terminal state; `orun agent replay` reads it |
| Task node | `refs/tasks/<KEY>` | The task's key and its sealed contract, recorded by `orun task create` and moved by `orun task attach` |

→ [The agent runtime](../concepts/agent-runtime.md) ·
[The task plane](../concepts/task-plane.md)

### Plan revisions and executions

Each compiled plan is sealed as a **PlanRevision** pinned to the catalog it
came from; each run is an **Execution** — jobs, steps, attempts, logs — live
while running, sealed immutable when terminal. The catalog's live plane
(deployments, health) is derived on read from these records.

## Platform objects — the tenancy

A fourth class lives on Orunbase rather than in a file. These are not
`apiVersion`/`kind` documents; they are identities the platform issues and
every cloud command runs under, named by a prefixed id.

| Object | Id | What it scopes |
|---|---|---|
| Workspace | `ws_…` (slug accepted; legacy `org_…`) | Members, integration connections, secrets, the task plane, agent sessions |
| Project | `prj_…` | One repository, linked by `orun auth login` or `orun cloud link` |
| Environment | `env_…` | A runtime context inside a project; the rung secrets are scoped to |
| Repository link | `repl_…` | A repository the platform may act on, created through the GitHub App; what a platform baseline build and a grounded session need |
| Integration connection | `int_…` | A connected provider that brokered secrets are minted against |
| Epic, milestone, task | `epc_…` (or `EP-n`, slug), `mls_…`, `tsk_…` (or the key) | The task plane's containers and units |
| Agent session | `as_…` | One run of the agent runtime, local or sandboxed |
| Baseline registry row | The baseline id (`cirrus`, `lumen`) | What can be built, by whom, at which tag; described by a blueprint card fetched at that tag |
| Skill revision | `sha256:…` of the canonical body | A hosted playbook; workspace revisions shadow the Sourceplane defaults by name |

→ [Workspaces and tenancy](../concepts/workspaces-and-tenancy.md) ·
[Baselines](../concepts/baselines.md)

## How resources version

Schemas evolve the way Kubernetes APIs do: behind the `apiVersion`, with reads
that never break.

- **Immutable history, converted reads.** Recorded objects keep their original
  schema on disk forever. When the model advances (as `v1alpha1` → `v1` did
  for catalog entities), orun **up-converts on read** — old snapshots render
  through the new model with zero migration.
- **Advisory migration, never rewrites.** `orun catalog migrate` lints
  authored files for the richer model and tells you what to add. It never
  edits a file. Adoption is additive: an un-migrated component still resolves.
- **Validation at the edge.** Every authored document is validated against its
  declared schema at load — the first compiler stage — so version skew is a
  structured error, not a runtime surprise.

## Why a resource model

The payoff of forcing everything through typed documents:

- **Reviewability.** Every behavior change is a diff to a schema-validated
  file — including the compiled plan itself.
- **Composability.** Tools (the cockpit, the catalog, CI, your own scripts via
  `--json`) consume resources without bespoke parsers.
- **Determinism.** The compiler's inputs are enumerable: these documents, at
  these digests, with this trigger. Nothing ambient.
- **Longevity.** Versioned schemas plus immutable records mean today's
  history is readable by next year's orun.

## Where to go next

- [Design principles](/principles) — the five principles this model serves.
- [Glossary](glossary.md) — every term on this page, defined in one line.
- [Configuration reference](../reference/configuration.md) — the full surface
  of each authored document.
