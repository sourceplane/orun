---
title: Glossary
description: One-line definitions for every term of art in the orun ecosystem, with links to the page that explains each in depth.
---

The orun vocabulary, in one place. Terms link to the page that explains them
in depth.

## Declaring

| Term | Definition |
|---|---|
| **Intent** | The repository-level control document (`intent.yaml`): environments, groups, policies, discovery roots, trigger bindings, composition sources. Says *what*, never *how*. → [Intent model](../concepts/intent-model.md) |
| **Component** | A deployable or operable unit declared next to its code in `component.yaml`: a type, environment subscriptions, typed inputs, dependencies, and catalog metadata. → [Intent model](../concepts/intent-model.md) |
| **Type** | A component's contract name (e.g. `terraform`, `helm-chart`). Binds the component to the composition that validates and executes it. → [Compositions](../concepts/compositions.md) |
| **Composition** | A versioned execution contract for one component type: input schema, job templates, execution profiles, and declared effects. → [Compositions](../concepts/compositions.md) |
| **Stack** | The packaging format for composition sources — a manifest plus split-kind documents, distributable as a directory, archive, or OCI artifact. → [Stacks](../concepts/stacks.md) |
| **Intent preset** | Reusable intent scaffolding shipped inside a Stack; a repository opts in via `extends`. → [Intent presets](../concepts/intent-presets.md) |
| **Group (domain)** | A policy domain: components sharing defaults and non-negotiable constraints, declared in the intent. → [Intent model](../concepts/intent-model.md) |
| **Environment** | A named runtime context (dev, staging, production) with its own activation rules, defaults, and policies. → [Intent model](../concepts/intent-model.md) |
| **Policy** | A constraint declared at group or environment level and enforced at compile time. Cannot be overridden by component inputs. → [Design principles](/principles) |
| **Profile** | A context overlay on a composition (`pull-request`, `verify`, `deploy`): which jobs, steps, and capabilities run in that context. → [Profile rules](../concepts/profile-rules.md) |
| **Trigger binding** | A rule mapping an external event (PR, push, tag) to planning context: which environments activate, what scope compiles. → [Trigger bindings](../concepts/trigger-bindings.md) |
| **Dependency rule** | Per-trigger policy for whether `dependsOn` edges are enforced, advisory, or disabled. → [Dependency rules](../concepts/dependency-rules.md) |
| **Promotion** | An ordering constraint between environments (production after staging), compiled into the plan. → [Environment promotion](../concepts/environment-promotion.md) |
| **Change watch** | A component's opt-in (`spec.change.watches`) to being affected by changes in specific global intent sections. → [Change watches](../concepts/change-watches.md) |
| **Effects** | A composition's declaration of what running it contributes to the catalog: integrations, provided Resources, exposed APIs, satisfied scorecard rules. → [Service catalog](../concepts/service-catalog.md) |

## Compiling

| Term | Definition |
|---|---|
| **Planner / compiler** | The six-stage pipeline — load, normalize, expand, bind, resolve, materialize — that turns declarations into a plan. → [How orun works](how-orun-works.md) |
| **Component instance** | One cell of the environment × component matrix, with fully merged inputs and resolved policies. → [Plan DAG](../concepts/plan-dag.md) |
| **Job instance** | An executable DAG node (`component@environment.job`) with rendered steps and job-level dependency edges. → [Plan DAG](../concepts/plan-dag.md) |
| **Plan (plan DAG)** | The immutable compiled artifact (`plan.json`): every job, step, edge, and merged input made explicit. The artifact of record. → [Plan DAG](../concepts/plan-dag.md) |
| **Composition lock** | `compositions.lock.yaml` — every composition source pinned to a digest, so "which contract" is never a runtime question. → [Stacks](../concepts/stacks.md) |
| **Scope** | Which components/environments a plan covers: `full`, `changed`, or explicit `--component`/`--env`/`--all-envs` selection. → [Change detection](../concepts/change-detection.md) |
| **Change detection** | The engine classifying which components a file change affects, powering `--changed` and `orun catalog affected`. → [Change detection](../concepts/change-detection.md) |

## Executing

| Term | Definition |
|---|---|
| **Runner** | A swappable execution backend for a plan: local shell, Docker, or GitHub Actions. → [Runners](../execute/runners.md) |
| **Execution (run)** | One invocation of a plan: jobs, steps, attempts, logs. Live while running; sealed immutable when terminal. → [Execution model](../concepts/execution-model.md) |
| **Resume** | Re-running a plan such that already-succeeded jobs are skipped, with their prior logs carried forward. → [Execution model](../concepts/execution-model.md) |
| **Cockpit** | The unified operator surface — `orun status`, `orun logs`, and the TUI — all rendering the same view-model with the same design tokens. → [Cockpit overview](../cockpit/overview.md) |

## Recording

| Term | Definition |
|---|---|
| **Object model** | orun's persistence layer under `.orun/objectmodel/`: a git-shaped DAG of immutable, content-addressed objects plus named refs. → [State model](../concepts/state-model.md) |
| **Ref** | A named, mutable pointer into the object graph (`catalogs/current`, `executions/latest`). → [State model](../concepts/state-model.md) |
| **Source snapshot** | A content-addressed capture of the workspace sources a catalog or plan was resolved from. → [State model](../concepts/state-model.md) |
| **Catalog** | The typed service catalog derived from the sources at a snapshot: entities, relations, ownership, indexes. → [Service catalog](../concepts/service-catalog.md) |
| **Entity** | A typed catalog object in the shared `orun.io/v1` envelope. Kinds: Component, API, Resource, System, Domain, Group, Environment, Composition. → [Service catalog](../concepts/service-catalog.md) |
| **Relation graph** | The unified typed edge set (`relations.json`) — dependsOn, partOf, ownedBy, providesApi, consumesApi, runsOn, deployedTo, composedBy — consumed by both catalog reads and change detection. → [Service catalog](../concepts/service-catalog.md) |
| **Ownership provenance** | Where an entity's owner came from: `authored`, `CODEOWNERS`, or `inherited`. → [Service catalog](../concepts/service-catalog.md) |
| **Live plane** | Deployment and health state derived on read from execution history — never persisted into catalog blobs. → [Service catalog](../concepts/service-catalog.md) |
| **Plan revision** | A compiled plan sealed into the object model, pinned to the catalog it was compiled against. → [State model](../concepts/state-model.md) |

## Distributing

| Term | Definition |
|---|---|
| **kiox provider** | orun's distribution form: an OCI provider artifact (`ghcr.io/sourceplane/orun`) consumable by the kiox workspace tool. → [Use with kiox](../examples/use-with-kiox.md) |
| **OCI artifact** | The registry format used to distribute both orun itself and composition Stacks. → [Stacks](../concepts/stacks.md) |
