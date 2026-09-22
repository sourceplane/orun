---
title: Plan schema
description: The fields of the plan artifact orun plan writes and orun run executes — jobs, steps, the workflow step form, secret references, approval gates, and secretOutputs.
---

The plan is the artifact produced by `orun plan` and consumed by `orun run`. The core shape is described by the JSON Schema at `assets/config/schemas/plan.schema.yaml`; the complete field set below is taken from the plan types in `internal/model/plan.go` (`Plan`, `PlanJob`, `PlanStep`). Field names are the exact JSON keys.

## Top-level fields

| Field | Meaning |
| --- | --- |
| `apiVersion` | `orun.io/v1` |
| `kind` | Always `Plan` |
| `metadata` | Name, description, namespace, generation timestamp, checksum |
| `execution` | `concurrency`, `failFast`, and `stateFile` |
| `spec.jobBindings` | Optional metadata about bound jobs |
| `jobs` | The concrete execution DAG |

## Job fields

| Field | Meaning |
| --- | --- |
| `id` | Job ID in the form `<component>@<environment>.<job>` |
| `name`, `component`, `environment` | Required identity of the job |
| `composition`, `jobRegistry`, `job` | Which composition and job template the job was bound from |
| `runsOn`, `path` | Runner image and working directory |
| `steps` | The ordered step list (required) |
| `dependsOn` | Hard dependency edges on other job IDs |
| `advisoryDependsOn` | Advisory edges that order but do not gate |
| `dependencyMode`, `dependencySource`, `dependencyRuleTriggerRef` | How the dependency edges were derived |
| `gates` | Cross-plan promotion gates: `type` (`environment-promotion`), `environment`, `component`, `condition`, `match` |
| `timeout`, `retries` | Job-level limits |
| `env` | Merged environment variables |
| `secretRefs` | Secret references the job resolves at run time — see below |
| `materialize` | Value-free delivery step for resolved secrets — see below |
| `labels` | String labels |
| `parameters` | The merged component parameters the job was rendered with, including `secretOutputs` when declared |

## Step fields

Each step declares `id` and exactly one execution form: `run`, `use`, or `workflow`.

| Field | Meaning |
| --- | --- |
| `id`, `name` | Step identity |
| `phase` | `pre`, `main` (default), or `post` |
| `order` | Integer ordering within a phase |
| `run` | Shell command |
| `use` | GitHub Actions-style action reference |
| `workflow` | Workflow file to run — see below |
| `with`, `env` | Step inputs and environment |
| `shell`, `working-directory` | Shell and working directory overrides |
| `timeout`, `retry` | Step-level limits |
| `onFailure` | `stop` or `continue` |

## The `workflow:` step form

A `workflow:` step runs a `kind: Workflow` file through the workflow engine instead of a shell or an action. The compiler pins the reference so the plan records what will run, never the outcome:

| Field | Meaning |
| --- | --- |
| `workflow` | The workflow file (or remote reference) this step runs |
| `workflowDigest` | Content digest of the workflow pinned at compile time; folds into the plan checksum |
| `connections` | Compile-checked credential grant: connection name → field → `secret://` reference. Names and references only — the plan *is* the reviewable grant, and only mapped references cross to the engine at run time |
| `resume` | When `true`, a retry re-executes only the steps that did not succeed in the prior attempt; the default re-runs from the top |
| `approval` | A human gate evaluated before the engine is invoked — see below |

Every field here is durable plan content; the workflow's run state lives under `.orun/` as run facts.

## Approval gates

`approval` on a step declares a human gate. The run pauses before the step, and the pending request and the decision are sealed run facts under `.orun/approvals`, never plan content.

| Field | Required | Meaning |
| --- | --- | --- |
| `approval.prompt` | no | What the approver is asked |
| `approval.timeout` | yes | How long the gate waits; a gate without a timeout is rejected at validation so a forgotten gate cannot hang CI silently |
| `approval.onTimeout` | yes | `fail` or `proceed` |

`orun approve` answers a pending gate.

## Secret references

Secrets appear in the plan as references only; no value field exists, structurally.

- `secretRefs[]` on a job lists each `secret://` reference the runner resolves, as `{ "asEnv": "<ENV_VAR>", "ref": "secret://…", "optional": false }`. Hard references fail closed when the key is absent; `optional: true` marks a best-effort reference that is skipped instead.
- `materialize` on a job is the value-free delivery step rendered from the profile's runtime-delivery block: `{ "target": "<adapter id>", "secrets": ["KEY", …] }`. The adapter and key names fold into the checksum; values never do.
- `connections` on a `workflow:` step maps connection fields to `secret://` references, as described above.

## `secretOutputs`

`secretOutputs` is not a top-level plan field. It is a component parameter, so it arrives in the plan under `parameters.secretOutputs` as a comma-separated `KEY=producer-hint` list. When it is present the runner exports `ORUN_SECRET_OUTPUTS` — the path of a per-job sink file — to every step of the job, then parses the sink, enforces the declared-key allow-list, registers the values with the redactor, and publishes them over the run's lease-bound channel. See [secrets](../concepts/secrets.md#secretoutputs--job-output-secrets).

## Minimal example

```json
{
  "apiVersion": "orun.io/v1",
  "kind": "Plan",
  "metadata": {
    "name": "demo",
    "generatedAt": "2026-01-01T00:00:00Z",
    "checksum": "sha256-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  },
  "execution": {
    "concurrency": 4,
    "failFast": true,
    "stateFile": ".orun-state.json"
  },
  "jobs": []
}
```

Treat the plan as an immutable artifact. Do not hand-edit it unless you are debugging the runtime itself.

## Related

- [Workflow schema](./workflow-schema.md) — the `kind: Workflow` document a `workflow:` step runs.
- [Workflow actions](../concepts/workflow-actions.md) — `workflow:` steps, connections, resume, and approvals in context.
- [Secrets](../concepts/secrets.md) — `secretEnv`, `optionalSecretEnv`, and `secretOutputs`.
- [Scope references](./scope-references.md) — the `secret://` grammar.
- [`orun approve`](../cli/orun-approve.md) — answering a pending approval gate.
