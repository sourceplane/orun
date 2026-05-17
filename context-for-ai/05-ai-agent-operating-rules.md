# AI agent operating rules

Use these rules as the working checklist for any AI agent editing an Orun-modeled repository.

## Start with the model

Before editing, identify:

- The intent file in scope.
- The discovery roots.
- The affected component manifests.
- The component type and composition source.
- The selected environments and profiles.
- The dependency edges into and out of the component.
- The validation and plan commands available in the repo.

## Choose the right layer

| If the user asks to change... | Edit... |
| --- | --- |
| Which components exist | `component.yaml` files or inline `intent.components` |
| Which environments exist | `intent.environments` |
| Which components run in an environment | `subscribe.environments` or environment selectors |
| Shared defaults | `intent.environments.*.defaults` or `intent.groups.*.defaults` |
| Guardrails | `intent.groups.*.policies`, `intent.environments.*.policies`, or profile policies |
| Required component inputs | `ComponentSchema` in the composition package |
| Execution steps | `JobTemplate` in the composition package |
| PR vs release behavior | `ExecutionProfile` |
| Ordering | `dependsOn` |
| CI event activation | `automation.triggerBindings` and environment activation |

## Anti-patterns

Do not:

- Add a standalone CI script that bypasses Orun for behavior that should be planned.
- Hide environment branching inside shell when a profile or environment default would make it explicit.
- Copy job steps into multiple components.
- Add a component input that the schema does not allow.
- Change a composition without considering every component of that type.
- Remove or rename a component without updating dependents.
- Edit generated plans as source.
- Treat `.orun/component-tree.yaml` as more authoritative than source manifests.

## Preferred PR explanation

When summarizing changes, include:

- Which intent/component/composition layer changed.
- Which component types and profiles are affected.
- Whether dependencies or environments changed.
- Which Orun commands were run.
- What changed in the rendered DAG or why the DAG is unchanged.

Example:

```text
Updated the terraform composition profile used by production release components.
The component schemas are unchanged. The release profile now includes the
terraform.validate capability before terraform.plan. `orun validate` passes and
the generated DAG still has the same job ordering.
```

## If confused

Ask or investigate in this order:

1. What does `intent.yaml` say?
2. Where is the affected `component.yaml`?
3. What composition validates this type?
4. Which profile is selected for the environment?
5. What does the plan say will actually run?

The plan is the compiled truth. If the plan surprises you, change the declarative inputs and regenerate it.

