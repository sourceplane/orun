# Intent and component model

This page explains how to reason about `intent.yaml` and `component.yaml` files in Orun-modeled repositories.

## `intent.yaml`

`intent.yaml` is the repo-level control plane. It defines the planning universe.

Common fields:

| Field | Meaning |
| --- | --- |
| `metadata` | Name, description, and namespace for the compiled plan. |
| `env` | Root-level environment variables shared by all jobs unless overridden. |
| `compositions.sources` | Where composition packages come from: `dir`, `archive`, or `oci`. |
| `compositions.resolution` | Source precedence and explicit type-to-source bindings. |
| `discovery.roots` | Directories to scan recursively for `component.yaml` files. |
| `automation.triggerBindings` | CI/event triggers that activate environments and plan scopes. |
| `groups` | `parameterDefaults` per domain, keyed by domain name, and an enforced `policies` map. |
| `environments` | `parameterDefaults`, selectors, env vars, trigger activation, promotion order, dependency mode (and a recorded `policies` map). |
| `components` | Optional inline components. Many repos prefer discovered components. |
| `execution` | Runtime state configuration, such as local or remote state. |
| `repo` | Optional self-description of the repo itself (see below). |

### Repo identity and `docs.overview`

A top-level `repo:` block lets the repo describe *itself* rather than a
component. It resolves to a first-class `Repo` catalog entity (one per repo,
keyed `<namespace>/<repo>/<name>`), listable with `orun catalog list --kind Repo`.

```yaml
repo:
  displayName: Lumen Platform
  owner: group:platform
  docs:
    overview: docs/overview.md   # front-page markdown for the repo
  links:
    - { title: Runbook, url: https://… }
  tags: [saas, baseline]
```

Any entity — the repo or an individual component — can point at front-page
markdown with `docs.overview: <path>`. On `orun plan` / `orun catalog push`, the
referenced file's bytes are read at the resolved commit and carried into the
catalog snapshot as a content-addressed blob (deduped and set-difference synced —
an unchanged doc is never re-uploaded). The entity records a `{path, sha, digest}`
pointer; there is no live git-provider call. Orunbase's workspace overview
renders these directly.

## Component manifests

A discovered component manifest looks like this:

```yaml
apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: network-foundation
spec:
  type: terraform
  domain: platform-foundation
  path: infra/network
  subscribe:
    environments:
      - name: development
        profile: pull-request
      - name: staging
        profile: verify
      - name: production
        profile: release
  parameters:
    stackName: network-foundation
    terraformDir: .
    terraformVersion: 1.9.8
  dependsOn:
    - component: identity-foundation
```

Key fields:

| Field | Meaning | How to change safely |
| --- | --- | --- |
| `metadata.name` | Stable component ID used in dependencies and plan jobs. | Rename only with a repo-wide dependency update. |
| `spec.type` | Composition contract name. | Choose an existing type unless you are also adding a composition. |
| `spec.domain` | The group whose `parameterDefaults` apply (groups are keyed by domain). | Ensure the matching group exists if group defaults are expected. |
| `spec.path` | Job working directory. | Keep relative to the intent root unless the repo documents otherwise. |
| `spec.subscribe.environments` | Environments where the component participates. | Prefer explicit subscriptions for component-owned behavior. |
| `spec.parameters` | Type-specific configuration, checked against the composition's schema by `orun plan`. | Add fields only if the schema allows them or update the composition. |
| `spec.env` | Component-level runtime environment variables. | Use for shell env, not typed configuration. |
| `spec.labels` | Metadata for ownership and classification. | Useful for humans and future selection features. |
| `spec.dependsOn` | Component dependency edges. | Add when ordering or required context matters. |
| `spec.overrides.steps` | Component-level step replacement or additive override. | Use sparingly. Prefer profiles/compositions for shared behavior. |

## Environment selection

Orun selects component instances per environment.

Selection rules:

1. If a component has `subscribe.environments`, those subscriptions decide where it participates.
2. If a component has no subscriptions, `environments.<name>.selectors.components` can select it.
3. `environments.<name>.selectors.domains` acts as an additional domain filter.
4. Disabled components are skipped.

Subscriptions can be simple strings or objects:

```yaml
subscribe:
  environments:
    - development
    - name: production
      profile: release
      env:
        RELEASE_CHANNEL: stable
```

## Defaults and parameters

For component instance parameters, the planner uses this precedence from lowest to highest:

1. Environment `parameterDefaults` (`"*"`, then the component's type).
2. Group `parameterDefaults` for the component's domain (`"*"`, then the type).
3. Component `parameters`.

The merged result is then checked against the composition's schema.

`path` is handled specially:

1. Component `path`.
2. Group default `path`.
3. Environment default `path`.
4. `./`.

Use defaults for shared values, but keep component-specific facts in component parameters.

## Runtime env vars

Runtime `env` is distinct from `parameters`.

Plan-time merge order from lowest to highest:

1. Intent root `env`.
2. Environment `env`.
3. Component root `env`.
4. Subscription `env`.

The `ORUN_` prefix is reserved for runtime-injected variables. Do not define user env vars that start with `ORUN_`.

## Policies

Groups and environments can declare a `policies` map, and execution profiles a `policies` block. The vocabulary is closed and enforced:

| Policy | Requires | Enforced by |
| --- | --- | --- |
| `pinnedParameters` (keyed by type or `"*"`) | A component or subscription may not set a different value; the pinned value is applied | `orun validate`, `orun plan` |
| `requireProfile` (names or globs) | The component's resolved profile matches | `orun validate`, `orun plan` |
| `requirePinnedTerraformVersion` | `terraformVersion` is exact (`1.9.8`), not a range | `orun validate`, `orun plan` |
| `requireCleanGitTree` | No uncommitted or untracked changes when the job runs | `orun run` |
| `requireApproval` | `orun approve <jobID> policy.requireApproval` before the job's first step | `orun run` |

An unknown key fails validation. Never work around a policy violation by editing the policy from a component change: if a change conflicts with one, stop and explain the conflict. Each plan job records its effective policies under `policies`.

The other guardrails orun enforces are the composition's schema, the profiles a composition offers, profile and dependency rules, secret references, source digest pins, and task-contract `affects`. See the docs page on standards (`website/docs/concepts/standards.md`).

## Dependencies

Use `dependsOn` to model component relationships:

```yaml
dependsOn:
  - component: network-foundation
  - component: identity-service
    environment: production
    scope: cross-environment
```

When `environment` is omitted, Orun resolves the dependency to the same environment as the current component instance.

Dependencies become job-level edges in the plan. Inspect them with:

```bash
orun plan --intent intent.yaml --view dependencies
orun plan --intent intent.yaml --view dag
```

## How to modify intent safely

1. Identify whether the change is repo-wide, component-specific, or composition-level.
2. Put repo-wide environment, discovery, trigger, source, default, and policy changes in `intent.yaml`.
3. Put component-specific desired state in the component manifest.
4. Put execution behavior in compositions and profiles.
5. Run `orun validate --intent intent.yaml`.
6. Run `orun component --intent intent.yaml --long` for the affected component.
7. Run `orun plan --intent intent.yaml --view dag`.
8. Explain the plan impact in the PR.

