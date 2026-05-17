# Development and testing workflow

This workflow is for making changes inside an Orun-modeled repository.

## Before editing

Answer these questions:

1. Is the change about repo-level planning, a component's desired state, or reusable execution behavior?
2. Which component(s) and environment(s) are affected?
3. Which composition type validates the affected component(s)?
4. Which profile will be selected in each environment?
5. Does the change add or remove dependency edges?
6. Will the plan DAG change?

## Common change paths

### Add a component

1. Choose the correct existing `type`.
2. Place the component under a directory included by `intent.discovery.roots`.
3. Add `component.yaml` with `apiVersion`, `kind`, `metadata.name`, and `spec`.
4. Set `domain`, `path`, `subscribe.environments`, `inputs`, labels, and dependencies.
5. Validate against the composition schema.
6. Inspect the component view and DAG.
7. Update docs and AI context if the repo keeps a component inventory.

Commands:

```bash
orun validate --intent intent.yaml
orun component <component-name> --intent intent.yaml --long
orun plan --intent intent.yaml --view component=<component-name>
orun plan --intent intent.yaml --view dag
```

### Modify a component

1. Read the component schema for its type.
2. Change only component-owned desired state in `component.yaml`.
3. If you need a new input, update the composition schema instead of smuggling it through shell.
4. If ordering changes, add or update `dependsOn`.
5. Validate and inspect the plan.

### Add environment behavior

Prefer these locations:

| Need | Put it here |
| --- | --- |
| Environment activates on a CI event | `intent.automation.triggerBindings` plus `environments.<name>.activation.triggerRefs` |
| Shared default for all components in an environment | `environments.<name>.defaults` |
| Shared env var for all jobs in an environment | `environments.<name>.env` |
| Shared constraint for an environment | `environments.<name>.policies` |
| Component-specific behavior in one environment | `component.subscribe.environments[].profile`, `env`, or inputs |
| Reusable behavior difference such as PR vs release | `ExecutionProfile` |

### Add or modify a composition type

1. Add or update the Stack composition directory.
2. Define or update the `Composition` facade.
3. Define or update `ComponentSchema`.
4. Define or update `JobTemplate` documents.
5. Define or update `ExecutionProfile` documents.
6. Ensure the package exports the composition, or relies on Stack auto-discovery.
7. Bind the source in `intent.compositions` if needed.
8. Validate all components that use the type.

## Validation ladder

Use the cheapest check that proves the change, then climb as risk increases:

| Change risk | Checks |
| --- | --- |
| Docs only | Website/docs build or markdown-specific checks if present. |
| Component input only | `orun validate`, `orun component --long`, targeted `orun plan --view component=...`. |
| New dependency edge | Targeted component view, `orun plan --view dependencies`, `orun plan --view dag`. |
| Composition schema/job/profile | `orun compositions --long`, `orun validate`, full plan render. |
| Runtime behavior | `orun plan --output /tmp/orun-plan.json`, then `orun run --plan /tmp/orun-plan.json --dry-run` if supported. |
| CI integration | Inspect workflow plus trigger-bound plan output. |

## What to inspect in the plan

After rendering a plan, inspect:

- Job IDs for affected components.
- Selected environment and profile.
- Working directory path.
- Concrete step list after profile filtering.
- Rendered `run` and `use` fields.
- Step phases and ordering.
- Job dependencies.
- Runtime env and config.
- Composition source digests in plan spec.

## Safety boundaries

Non-destructive:

- Validate.
- Render plan to `/tmp`.
- Inspect component, composition, DAG, and dependency views.
- Dry-run execution when supported.

Potentially destructive:

- `orun run` without dry-run when steps deploy or apply.
- Terraform `apply`.
- Helm upgrade/install against real clusters.
- Cloud provider deploy commands.
- `orun publish` to OCI registries.
- CI workflow changes that auto-deploy on merge.

Ask for explicit user intent before destructive operations.

## Runtime environment variables

Steps running under Orun have access to `ORUN_ENV`, a file path for persisting environment variables across steps within a job. This is an alias for `GITHUB_ENV` — both point to the same file. Compositions should prefer `ORUN_ENV` for portability.

```bash
# Persist a variable for subsequent steps
echo "DEPLOY_SHA=$(git rev-parse HEAD)" >> "$ORUN_ENV"
```

The `ORUN_` prefix is reserved for runtime-injected variables and cannot be used in user-declared `env` at any level (intent root, environment, component root, subscription). Key runtime variables: `ORUN_CONTEXT`, `ORUN_RUNNER`, `ORUN_EXEC_ID`, `ORUN_PLAN_ID`, `ORUN_JOB_ID`, `ORUN_JOB_UID`, `ORUN_JOB_RUN_ID`, `ORUN_ENVIRONMENT`, `ORUN_COMPONENT`, `ORUN_ENV`.

