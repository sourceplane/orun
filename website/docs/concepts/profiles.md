---
title: Execution profiles
---

An **execution profile** is a named configuration that controls which composition steps run and how plan scoping behaves. Profiles let platform teams define modes like `dry-run`, `verify`, and `release` without duplicating intent or composition logic.

## What profiles control

A profile sets two things:

1. **Plan scope** — whether the planner considers all components (`full`) or only changed ones (`changed`).
2. **Controls** — per-composition-type toggles that enable or disable individual steps.

When `orun plan` runs with a selected profile, the planner applies the profile's controls to every component instance that matches. Steps whose control evaluates to `false` are excluded from the generated plan.

## Profile document format

```yaml
apiVersion: sourceplane.io/v1
kind: ExecutionProfile
metadata:
  name: verify
spec:
  description: Merge verification with full backend initialization
  compositionRef: terraform
  plan:
    scope: changed
  controls:
    terraform:
      fmt:
        enabled: true
      init:
        enabled: true
        backend: true
      validate:
        enabled: true
      plan:
        enabled: true
      apply:
        enabled: false
```

| Field | Meaning |
| --- | --- |
| `metadata.name` | Profile identifier used in references |
| `spec.description` | Human-readable purpose |
| `spec.compositionRef` | Optional — limits the profile to a specific composition type |
| `spec.plan.scope` | `full` or `changed` |
| `spec.controls.<type>.<key>` | Nested control values passed to composition step conditions |

## Where profiles live

Profiles can be defined at three levels:

### Intent-level profiles

Declared inline in `intent.yaml` under `execution.profiles`:

```yaml
execution:
  profiles:
    dry-run:
      description: Non-mutating checks only
      compositionRef: terraform
      plan:
        scope: changed
      controls:
        terraform:
          apply:
            enabled: false
```

### Stack-level profiles

Standalone YAML files in the `profiles/` directory at the stack root:

```text
my-stack/
├── stack.yaml
├── profiles/
│   ├── pull-request.yaml
│   ├── verify.yaml
│   └── release.yaml
└── compositions/
    └── ...
```

These are auto-discovered when `spec.profiles` is omitted from `stack.yaml`. Each file must contain a valid `ExecutionProfile` document.

### Composition-scoped profiles

Profiles specific to a single composition type live under `compositions/<type>/profiles/`:

```text
my-stack/
└── compositions/
    └── terraform/
        ├── compositions.yaml
        └── profiles/
            └── dry-run.yaml
```

Composition-scoped profiles are keyed with a fully-qualified name: `<composition-type>.<profile-name>` (e.g., `terraform.dry-run`).

## Auto-discovery

When the `spec.profiles` field is omitted from `stack.yaml`, orun automatically discovers:

- All `*.yaml` files in `profiles/` → stack-level profiles
- All `*.yaml` in each `compositions/<type>/profiles/` → composition-scoped profiles

To override auto-discovery, declare explicit entries:

```yaml
spec:
  profiles:
    - name: release
      path: profiles/release.yaml
```

## Profile resolution

When a plan is generated, each component×environment instance resolves its effective profile using this precedence (highest wins):

1. **Subscription entry override** — per-environment profile in the component's subscription
2. **Environment per-type profile** — `environments.<env>.execution.profiles.<composition-type>`
3. **Environment global profile** — `environments.<env>.execution.profile`
4. **Composition default profile** — `defaultProfile` in the composition document

### Example: environment-level selection

```yaml
environments:
  production:
    execution:
      profile: release
      profiles:
        cloudflare-worker: deploy
        cloudflare-pages: deploy
```

In this case, `production` terraform components use the `release` profile, while cloudflare components use `deploy`.

### Example: subscription-level override

```yaml
spec:
  type: terraform
  subscribe:
    - environments: [dev]
      profile: dry-run
    - environments: [staging, production]
      profile: release
```

## Control composition

Controls from multiple sources are merged in precedence order (lowest to highest):

1. Composition defaults (`controlDefaults` from the composition spec)
2. Profile controls (`profile.controls[<composition-type>]`)
3. Environment control overrides (`environments.<env>.execution.controlOverrides[<type>]`)
4. Component-level control overrides

Higher-precedence values overwrite lower ones at the leaf level.

## Selecting a profile at plan time

```bash
# Explicitly select a profile
orun plan --profile verify

# Let a trigger select the profile (see automation)
orun plan --from-ci github

# Use a named trigger
orun plan --trigger github-push-main
```

When no `--profile` or trigger is specified, the planner uses the environment-level or composition-default profile.

## Typical profile set

A common pattern uses three profiles with increasing scope:

| Profile | Scope | Purpose |
| --- | --- | --- |
| `pull-request` | `changed` | Fast PR checks — no backend init, no apply |
| `verify` | `changed` | Full verification with plan generation |
| `release` | `full` | Production release — all steps enabled |

Read [automation](./automation.md) to learn how triggers automatically select profiles based on CI events.
