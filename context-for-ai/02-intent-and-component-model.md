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
| `groups` | Domain defaults and policies. |
| `environments` | Environment defaults, policies, selectors, env vars, and trigger activation. |
| `components` | Optional inline components. Many repos prefer discovered components. |
| `execution` | Runtime state configuration, such as local or remote state. |

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
  inputs:
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
| `spec.domain` | Group/policy domain. | Ensure the matching group exists if group defaults or policies are expected. |
| `spec.path` | Job working directory. | Keep relative to the intent root unless the repo documents otherwise. |
| `spec.subscribe.environments` | Environments where the component participates. | Prefer explicit subscriptions for component-owned behavior. |
| `spec.inputs` | Type-specific configuration validated by the composition schema. | Add fields only if the schema allows them or update the composition. |
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

## Defaults and inputs

For component instance inputs, the current planner uses this precedence from lowest to highest:

1. Environment defaults.
2. Group defaults for the component domain.
3. Component inputs.

`path` is handled specially:

1. Component `path`.
2. Group default `path`.
3. Environment default `path`.
4. `./`.

Use defaults for shared values, but keep component-specific facts in component inputs.

## Runtime env vars

Runtime `env` is distinct from `inputs`.

Plan-time merge order from lowest to highest:

1. Intent root `env`.
2. Environment `env`.
3. Component root `env`.
4. Subscription `env`.

The `ORUN_` prefix is reserved for runtime-injected variables. Do not define user env vars that start with `ORUN_`.

## Policies

Policies are constraints, not ordinary defaults. Group policies and environment policies are collected for the component instance and should be treated as platform guardrails.

If a change seems to require bypassing a policy, stop and explain the conflict. Do not encode a workaround in a component script.

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

