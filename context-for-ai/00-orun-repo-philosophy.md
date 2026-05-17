# Orun repo philosophy

An Orun repo is a component-native desired-state repository. It uses a small set of declarative documents to describe what exists, which environments it participates in, what contract it satisfies, and which dependencies constrain execution.

The repo is not organized around "whatever script happens to run in CI". It is organized around typed components and reusable composition contracts.

## The mental model

| Concept | Meaning | AI implication |
| --- | --- | --- |
| `intent.yaml` | Repository-level platform intent: discovery roots, environments, composition sources, groups, defaults, policies, triggers, and optional inline components. | Start here. It defines the planning boundary. |
| `component.yaml` | Local declaration for one component owned near its code, chart, package, or infrastructure. | Treat this as an ownership boundary, not just metadata. |
| `component.type` | Stable logical contract name, such as `terraform`, `helm-chart`, or `cloudflare-worker`. | Type decides which composition validates and executes the component. |
| Composition source | Versioned package of component contracts loaded from a directory, archive, or OCI ref. | Do not invent execution logic if a composition type should own it. |
| `ExecutionProfile` | Context-specific behavior overlay for a composition, often PR, verify, release, or deploy. | Use profiles to vary behavior by environment or trigger without duplicating jobs. |
| Dependency edge | Explicit relationship from one component to another. | Add dependencies when ordering or required context matters. Avoid hidden sequencing. |
| Plan DAG | Compiled output with concrete jobs, steps, dependencies, profiles, and paths. | Inspect this before assuming what will run. |
| Runner | Runtime backend that executes the compiled plan. | Runtime should consume the plan, not reinterpret intent. |

## Separation of concerns

Orun deliberately separates three layers:

| Layer | Owns | Should not own |
| --- | --- | --- |
| Intent layer | environments, discovery, groups, trigger activation, composition sources, repo-wide defaults | long shell scripts, tool-specific execution logic |
| Component layer | component name, type, domain, path, subscriptions, typed inputs, labels, dependencies | copied job templates, per-environment imperative branching |
| Composition layer | schema, job templates, profiles, capabilities, step ordering, runtime contract | app-specific desired state that belongs in component inputs |

This mirrors common CNCF API patterns: authors declare resources, platform teams publish controllers/contracts, and the compiler materializes the reconciled graph before execution.

## Composability-first thinking

A change is composable when another component can be added beside it without editing hidden global script state.

Prefer:

- A new or updated `component.yaml` under a discovered root.
- A typed input validated by the component schema.
- A profile or capability selection inside the composition contract.
- An explicit `dependsOn` edge when ordering matters.
- A default or policy at the environment or group level when it applies across components.

Avoid:

- Hardcoded environment branching inside component-owned scripts.
- Copying composition job logic into a component directory.
- Adding a CI workflow that bypasses `orun plan`.
- Relying on file order, directory order, or naming accidents for execution order.
- Making a component depend on another component through shell commands instead of `dependsOn`.

## Intent is not execution

`intent.yaml` answers questions like:

- Which composition packages are in scope?
- Which directories contain components?
- Which environments exist?
- Which triggers activate which environments?
- Which platform groups and policies apply?

Compositions answer questions like:

- What inputs are required for a `terraform` component?
- Which steps make up the `verify` or `release` profile?
- Which capabilities are included for pull request validation?
- Which runner-compatible `run` or `use` steps are emitted?

The plan answers:

- Which concrete jobs exist now?
- Which environment and profile does each job use?
- Which steps will run and in what order?
- Which jobs depend on which other jobs?

## Policies and defaults

Defaults are convenience. Policies are constraints.

Use defaults to reduce repetition in component inputs. Use policies to express guardrails that should not be bypassed by component authors.

As implemented by Orun's planner, component instance inputs are assembled from environment defaults, group defaults, and component inputs, with component inputs winning. Path has a specific precedence: component `path`, then group default `path`, then environment default `path`, then `./`.

Runtime `env` has its own merge chain: root intent `env`, environment `env`, component root `env`, then subscription `env`.

## Rules for AI agents

- Do not add hidden coupling.
- Do not duplicate composition logic inside components.
- Do not bypass `intent.yaml` when changing repo-level behavior.
- Do not create environment behavior that cannot be seen in `orun plan`.
- Do not hardcode values that belong in defaults, environment config, component inputs, or profiles.
- Prefer adding typed inputs and composition support over one-off shell.
- Always validate and inspect the DAG after changes.
- Explain changes in Orun terms: intent, component, composition, profile, dependency, plan.

