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

## Working, delegating, and the platform

| Term | Definition |
|---|---|
| **Agent session** | One run of the agent runtime (`as_…`): an append-only event log with one body and any number of attached heads, sealed on terminal state as an AgentSessionSnapshot and replayable with `orun agent replay`. → [The agent runtime](../concepts/agent-runtime.md) |
| **Agent type** | An `agents/<name>.md` file — a capability envelope in frontmatter (harness, model, tools policy, `mayAffect`, owner) plus a persona body — sealed by `orun agent import` into a content-addressed AgentTypeSnapshot. → [The agent runtime](../concepts/agent-runtime.md) |
| **Baseline** | A complete product repository the platform can rebuild under a new owner: registered in the baseline registry, described by a blueprint card, built phase by phase by `orun baseline new`. → [Baselines](../concepts/baselines.md) |
| **Baseline registry** | The catalogue of baselines — id, source repository, pinned tag, tier, visibility, required providers, manifest path. Orunbase-maintained rows live in `orun-cloud`'s `baselines.yaml`; account rows come from `orun baseline register`. → [`orun baseline`](../cli/orun-baseline.md) |
| **Blueprint** | A `kind: Blueprint` (`orun.dev/v1`) document `orun new` places into a directory: sources, modules, dependency edges, inputs, and phases with hooks. → [`orun new`](../cli/orun-new.md) |
| **Blueprint card** | `blueprint.yaml` at a baseline's pinned tag: the integrations it requires, its inputs, a preview of the secrets it mints, the path of its build document, and the URLs a finished build must answer. It carries no tag of its own. → [Baselines](../concepts/baselines.md) |
| **Build document** | The `kind: Blueprint` a card names in `spec.bootstrap.blueprint` — what orun actually places, phase by phase, with hooks and gates. → [Baselines](../concepts/baselines.md) |
| **Cockpit v2** | `orun tui-next` (also `orun tui --next`): the rebuilt terminal head — Home, Agents, Activity, Catalog, and Events over the same state store as the console. A preview until it becomes the default. → [`orun tui-next`](../cli/orun-tui-next.md) |
| **Epic** | The programme tasks club under: an `epc_…` id, an `EP-n` key, and a slug. Carries milestones and spec docs; its rollup is folded from task verdicts at read time. → [The task plane](../concepts/task-plane.md) |
| **Grounded session** | An agent session that starts in a real checkout of a linked repository on a task-keyed branch, with git authenticated through short-lived, repository-scoped tokens minted from the session's own credential. → [The agent runtime](../concepts/agent-runtime.md) |
| **MCP** | `orun mcp serve`, the one local Model Context Protocol server: the pen plane (`pr_open`) and the platform plane (33 tools over the Orunbase API), 34 tools under one connection; `--read-only` drops the platform writes. → [`orun mcp`](../cli/orun-mcp.md) |
| **Milestone** | One phase of an epic, in order, with exit criteria: an `mls_…` id that implies its epic. → [The task plane](../concepts/task-plane.md) |
| **Object graph** | The content-addressed store under `.orun/objectmodel/` seen whole — sources, catalogs, plans, executions, agent types, sessions, task nodes — inspected with `orun objects`. → [State model](../concepts/state-model.md) |
| **Orunbase** | The hosted control plane (app.orunbase.com): workspaces, shared state, the task plane's allocator and derived verdicts, sandboxed agents, the console, and the API the platform MCP plane calls. Formerly "Orunbase"; the `orun cloud` command group keeps the older name. → [Workspaces and tenancy](../concepts/workspaces-and-tenancy.md) |
| **Phase** | In a Blueprint, a placement barrier with hooks attached — all of phase N is placed before N+1, and phase state is derived from the tree, so `--resume` is safe. In the task plane, a milestone. → [Baselines](../concepts/baselines.md) |
| **Provenance lock** | `.orun/provenance.lock` (`ScaffoldProvenance`): the blueprint and source digests, a secret-free input hash, and each module's mode and target — the base `orun new upgrade` three-way merges against. → [`orun new`](../cli/orun-new.md) |
| **Provenance pen** | `orun pr`: the branch grammar `orun/<KEY>-<slug>`, the `Orun-Task` commit trailer, and the manifest block in the PR body — `open` writes them, `check` preflights them, `land` merges once checks conclude. → [The task plane](../concepts/task-plane.md) |
| **Repository link** | Two distinct things: the CLI link `orun auth login` or `orun cloud link` caches, which puts a repository on a workspace's allow-list; and the console `repl_…` link created through the GitHub App, which lets the platform clone, push, and open PRs on the repository. → [Workspaces and tenancy](../concepts/workspaces-and-tenancy.md) |
| **Skill** | A hosted agent playbook: a content-addressed revision in a registry where a workspace's revisions shadow the Sourceplane defaults by name. `orun skills pull` writes each as a native `SKILL.md` carrying its `orun-rev`. → [`orun skills`](../cli/orun-skills.md) |
| **Spec doc** | Repository-authored markdown annotated to an epic. `orun spec push` uploads the committed copy with its repo, path, and sha, idempotent by content hash; the platform stores it sealed beside the epic. → [`orun spec`](../cli/orun-spec.md) |
| **Task** | The unit of work: a key issued by the cloud allocator (adopted, derived, or minted), a contract in the repository, and a verdict derived from observations. → [The task plane](../concepts/task-plane.md) |
| **TaskContract** | `tasks/<KEY>.TaskContract.yaml` (`orun.io/v1`): `goal`, `affects`, `doneWhen`, `gates`, and related fields, sealed by sha256 over canonical JSON wherever it travels. An explicit `gates: []` means merge alone finishes the work. → [The task plane](../concepts/task-plane.md) |
| **Verdict** | A task's derived rung — `draft → ready → in_progress → in_review → done → released` — read from the branch, PR, merge, and gates the platform observed; never typed by anyone. → [The task plane](../concepts/task-plane.md) |
| **Workspace** | The tenancy unit every cloud command runs under, named by a `ws_…` id or a slug (legacy `org_…`); resolved as `--workspace` > `ORUN_WORKSPACE` > `intent.yaml` > this repo's link > `orun workspace use`. → [Workspaces and tenancy](../concepts/workspaces-and-tenancy.md) |
| **Workspace link** | The CLI-side link between a repository and a workspace and project, created by `orun auth login` or `orun cloud link` and cached in `~/.orun/config.yaml`; the fourth rung of workspace resolution. → [`orun cloud`](../cli/orun-cloud.md) |

## Distributing

| Term | Definition |
|---|---|
| **kiox provider** | orun's distribution form: an OCI provider artifact (`ghcr.io/sourceplane/orun`) consumable by the kiox workspace tool. → [Use with kiox](../examples/use-with-kiox.md) |
| **OCI artifact** | The registry format used to distribute both orun itself and composition Stacks. → [Stacks](../concepts/stacks.md) |
