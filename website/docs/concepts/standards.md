---
title: Standards
description: Every standard a platform team can declare in orun, organised by the question it answers, with what orun does about each one today - enforced, recorded in the plan, or declared only.
---

A standard in orun is a declaration the platform can read: a schema, a profile, a rule, an
ordering, a contract. This page lists every one of them by the question it answers, and
says plainly what orun does with each today.

| Label | Meaning |
|---|---|
| **Enforced** | A violation fails a command: `orun validate`, `orun plan`, `orun task check`, or the agent runtime. Run that command in CI and the standard cannot be skipped. |
| **Recorded** | The declaration is resolved into the plan, the catalog, or a sealed brief as evidence, but nothing blocks on it. |
| **Declared only** | orun parses and carries the declaration, but does not act on it yet. |

## What must a component look like?

| Standard | Declared in | Status |
|---|---|---|
| **Component schema.** The parameters a type requires and the shape of each | `ComponentSchema` in the composition | **Enforced** by `orun plan`: every component's merged parameters are checked against its type's schema, and a mismatch fails the plan |
| **Known type.** A component's `type` must resolve to a composition from a declared source | `component.yaml` `type`, `intent.yaml` `compositions.sources` | **Enforced** by `orun plan` |
| **Reserved environment names.** User-declared `env` may not use the `ORUN_` prefix | Any `env` block | **Enforced** by `orun validate` and `orun plan` |
| **Pinned parameters.** A parameter value a component may not override, per composition type or for every type | `pinnedParameters` in a group's or environment's `policies` | **Enforced** by `orun validate` and `orun plan`: a component or subscription that sets a different value fails; otherwise the pinned value is applied. Recorded on each plan job under `policies` |
| **Policy vocabulary.** `policies` accepts only the keys orun enforces, so a typo cannot switch a guardrail off | `policies` on groups and environments, including those merged from presets | **Enforced** by `orun validate` and `orun plan`: an unknown key or ill-typed value fails |

## Which version of a golden path?

| Standard | Declared in | Status |
|---|---|---|
| **A pinned source.** A composition source fixed to an exact content digest | `digest:` on a source in `intent.yaml` | **Enforced** by `orun plan`: if the source resolves to a different digest, the plan fails |
| **A versioned source.** A source fixed to a version tag | The source's `ref` | **Recorded**: the tag is resolved at plan time, and the digest it resolved to is written into the plan (`spec.compositionSources`), so a moved tag shows in the plan diff |
| **Lifecycle.** A golden path marked `stable`, `beta`, or `deprecated` | `spec.lifecycle` on the `Composition` | **Recorded** on the composition's catalog entity, so "which components ride a deprecated path?" is a catalog query. No warning is printed |

See [Stacks](./stacks.md) and [intent presets](./intent-presets.md) for how golden paths and
platform rules are packaged and adopted.

## How may it run in each lane?

| Standard | Declared in | Status |
|---|---|---|
| **Execution profile.** Which steps of a job run in a lane, and with which overrides | `ExecutionProfile` in the composition; `profile:` on a subscription | **Enforced** by `orun plan`: a subscription naming a profile the composition does not define fails, and only the steps the profile selects are rendered into the plan |
| **Profile rules.** Which profile applies under which trigger | `profileRules` on a subscription | **Enforced**: `orun validate` rejects malformed rules, and `orun plan` applies them and records which rule chose the profile |
| **Trigger bindings.** Which CI events activate which environments, and how much each plans | `automation.triggerBindings`, `environments.<name>.activation` | **Enforced** by `orun plan` when it plans for an event (`--trigger`, `--from-ci`): an environment the event does not activate is not planned |
| **Required profile.** Every component in a group or environment must run a given profile | `requireProfile` in `policies` | **Enforced** by `orun validate` and `orun plan`: an instance whose resolved profile matches none of the names fails |
| **Pinned terraform version.** `terraformVersion` must be an exact release, not a range or `latest` | `requirePinnedTerraformVersion` in a profile's or an intent layer's `policies` | **Enforced** by `orun validate` and `orun plan` |
| **Clean git tree.** A job may only run from committed code | `requireCleanGitTree` in a profile's or an intent layer's `policies` | **Enforced** by `orun run`, which refuses to start when the working tree has uncommitted or untracked changes. Recorded on each plan job |
| **Approval.** A person signs off before a job runs | `requireApproval` in a profile's or an intent layer's `policies` | **Enforced** by `orun run`, which pauses the job before any step until [`orun approve`](../cli/orun-approve.md) decides. Recorded on each plan job; the verdict is sealed under `.orun/approvals` |

See [compositions](./compositions.md), [profile rules](./profile-rules.md),
[trigger bindings](./trigger-bindings.md), and [policies](./intent-model.md#policies).

## In what order?

| Standard | Declared in | Status |
|---|---|---|
| **Component dependencies.** One component's jobs wait for another's | `dependsOn` in `component.yaml` | **Enforced** by default: the edge is in the plan and the runner waits on it. `orun plan` rejects a dependency cycle |
| **Dependency mode.** Whether an edge blocks, is advisory, or is dropped, per environment or trigger | `dependencyMode`, dependency rules | **Enforced** for `enforced` edges; `advisory` edges are **recorded** in the plan as `advisoryDependsOn` and do not block |
| **Promotion within a plan.** Production waits for staging when both are in the same plan | `environments.<name>.promotion.dependsOn` | **Enforced**: compiled into DAG edges |
| **Promotion across plans.** Production may only run after staging succeeded in an earlier run | The same, when the environments are in separate plans | **Recorded**: compiled into evidence gates on the plan's jobs. orun does not yet check them before running |

See [dependency rules](./dependency-rules.md) and
[environment promotion](./environment-promotion.md).

## With which secrets?

| Standard | Declared in | Status |
|---|---|---|
| **References, never values.** A secret slot holds a `secret://` reference | `secretEnv`, `optionalSecretEnv` | **Enforced** by `orun plan`: a literal value in a secret slot fails the plan |
| **Declared output secrets.** A job may only publish the secret keys it declares | `secretOutputs` | **Enforced** by the runner, which discards undeclared keys |
| **Secret access policy.** Who may resolve which secrets, under which run facts | `SecretPolicy` documents | **Enforced** by Orunbase when a secret is resolved; `orun policy lint` and `orun policy test` check a policy before it is pushed |

See [secrets](./secrets.md) and [`orun policy`](../cli/orun-policy.md).

## Within what blast radius may a change go?

| Standard | Declared in | Status |
|---|---|---|
| **Task contract.** The components a change for a task may affect, and the gates it must pass | `tasks/<KEY>.TaskContract.yaml` `affects`, `gates` | **Enforced** by `orun task check <key>`, which fails when the branch changes a component outside `affects` |
| **Provenance.** A task's pull request carries its key in the branch name and an `Orun-Task` trailer on every commit, one task per PR | Repository convention, checked by the platform | **Enforced** by `orun pr check` locally and by the `orun/compliance` check on the pull request |
| **Agent tool policy.** Which tools an agent type may call, must ask about, or may never call | `tools.allow`, `tools.ask`, `tools.deny` in `agents/<name>.md` | **Enforced** by the agent runtime; tools not allowed are denied by default |
| **Agent reach.** The components an agent type is meant to work on | `mayAffect` in `agents/<name>.md` | **Recorded** in the sealed agent type and shown in the cockpit; the runtime does not check it. Use a task contract's `affects` for an enforced limit |

See the [task plane](./task-plane.md) and the [agent runtime](./agent-runtime.md).

## Declared, not yet enforced

This is parsed and carried through to each component instance, but neither the planner
nor the runner acts on it:

- **`condition` on a component's `dependsOn`** (`success`, `always`, `failure`). An
  enforced edge always waits for the dependency to succeed.

Write it down if it documents intent, but do not rely on it as a guardrail. For ordering,
use dependency rules and promotion.

## Making standards stick

A standard is only as strong as the command that checks it. In CI, run on every pull
request:

```bash
orun validate                  # the intent, its rules, and its policies
orun plan --changed            # schemas, pins, profiles, policies, secrets, cycles
orun task check "$TASK_KEY"    # on a task branch: the change stays inside its contract
orun pr check                  # the branch and commits carry the task's lineage
```

Then review the plan diff: it shows exactly what each standard resolved to for this
change. See [trigger bindings in CI](../examples/trigger-bindings-ci.md) for a complete
GitHub Actions setup.
