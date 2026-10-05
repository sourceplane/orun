---
title: Workflow schema
description: The field reference for orun.dev/v1 workflow files - steps, inputs, outputs, polling, and approval gates.
---

The complete field reference for `orun.dev/v1` workflow files — the language
executed in-process by orun since v2.35.0, standalone via
[`orun workflow run`](../cli/orun-workflow.md) or embedded as a
[`workflow:` plan step or blueprint hook](../concepts/workflow-actions.md).

Parsing is strict (`KnownFields`): any key not documented here is a parse
error, never silently ignored.

## Top level

```yaml
apiVersion: orun.dev/v1          # exactly this value
kind: Workflow
metadata:
  name: release                  # ^[a-z][a-z0-9-]*$
inputs:                          # optional — map of input name → spec
  service:
    type: string
    required: true
connections:                     # optional — declared names only; values arrive via the grant
  gh: { type: http.bearer }
outputs:                         # optional — name → CEL over the final state
  pr: "steps.open-pr.outputs.number"
maxParallel: 4                   # optional — engine default 4
steps: [ … ]                     # required
```

| Field | Type | Meaning |
|---|---|---|
| `apiVersion` | string | Must be `orun.dev/v1`. A `torkflow/v1` file is rejected by name — no converter ships; the v3 design's mapping table is the manual migration path. |
| `kind` | string | Must be `Workflow`. |
| `metadata.name` | string | Workflow name, `^[a-z][a-z0-9-]*$`. Used in the default exec id (`<name>-<unixNano>`). |
| `inputs` | map | Declared inputs. Names match `^[a-z][a-zA-Z0-9]*$`. |
| `connections` | map | Declared connection names and types. Values never appear in the file — they arrive through the step's `connections:` grant or `--connection` flags. |
| `outputs` | map | Workflow-level outputs: name → CEL expression evaluated over the final state (sees every step). Declared-only — a workflow never dumps raw context across its boundary. |
| `maxParallel` | int | Concurrency bound for the scheduler. Workflow value > run option > default `4`. |
| `steps` | list | The DAG. |

### Input spec

| Field | Meaning |
|---|---|
| `type` | Input type (`string`, …). |
| `required` | Fail before the first step if unset and no `default`. Missing required input errors `inputs.X is required (--set X=...)`. |
| `default` | Value used when the input is not provided. |
| `values` | Enum of allowed values. |
| `pattern` | RE2 pattern the value must match. |
| `secret` | Mark the value secret — registered with the redactor. |
| `description` | Shown in help/errors. |

## Steps

```yaml
steps:
  - name: build                  # ^[a-z][a-z0-9-]*$, unique
    needs: [prepare]             # pull edges — the only edge form; AND-join
    if: "inputs.mode != 'skip'"  # CEL; false ⇒ skipped (satisfies dependents)
    run: ["make", "build"]       # exactly one verb per step
    env: { CGO_ENABLED: "0" }    # run: only
    envInherit: [SSH_AUTH_SOCK]  # run: only
    cwd: "./svc"                 # run: only; default = the step's run dir
    timeout: 10m                 # default 10m
    retry: { maxRetries: 3, baseDelay: 5s, kind: exponential }
    continueOnError: false
    poll: { interval: 30s, timeout: 20m, until: "exitCode == 0" }
    outputs:
      digest: "stdout.trim()"    # CEL over this step's sealed result
```

| Field | Applies to | Meaning |
|---|---|---|
| `name` | all | Step name, `^[a-z][a-z0-9-]*$`, unique in the file. |
| `needs` | all | Upstream step names. AND-join: the step starts when every upstream reached a satisfying terminal state. A `skipped` upstream satisfies dependents; a `failed` upstream blocks them (status `blocked`) unless this step sets `continueOnError`. |
| `if` | all | CEL condition. `false` ⇒ status `skipped`, which still satisfies dependents. |
| `run` | verb | Argv array — executed directly, **never through a shell**. `;` and `$( )` in arguments are literals. Shell semantics are an explicit opt-in: `["bash", "-lc", "…"]`. |
| `action` | verb | A built-in action: `http.request`, `script`, `sleep`. Unknown action → `unknown action "x" (built-ins: http.request, script, sleep)`. |
| `workflow` | verb | Path to a nested workflow, relative to the parent's directory. Cycle-detected by absolute path. `with:` becomes the child's inputs; the child's declared outputs become this step's `output`. |
| `with` | `action`/`workflow` | Arguments. Validated against the action's schema before invoke. Rejected on `run:` steps. |
| `env` | `run` | Extra environment for the child process. Values are interpolated; wins over `envInherit` on collision. |
| `envInherit` | `run` | List of **host** env var names passed through to the child — the declared, review-visible escape hatch for process-inherited channels like `SSH_AUTH_SOCK`. A name absent from the host env is silently omitted. |
| `cwd` | `run` | Working directory. Defaults to the step's run dir under `.orun/wfruns/<execId>/` — never the process cwd. |
| `connection` | `action` | A declared connection name. `http.bearer` injects `Authorization: Bearer <token>` on `http.request`. Undeclared name → compile error. |
| `timeout` | all | Per-attempt timeout (Go duration). Default `10m`. A timed-out `run:` is killed: `run: timed out (killed): <argv0>`. |
| `retry` | all | `{maxRetries, baseDelay, kind}`. Attempts = `maxRetries + 1`; `baseDelay` default `1s`; `kind` is `exponential` (default — delay doubles per attempt) or `fixed`. Mutually exclusive with `poll`. |
| `continueOnError` | all | This step starts even if an upstream failed. |
| `poll` | all | The wait primitive — see below. |
| `outputs` | all | Declared step outputs: name → CEL over the step's sealed result. If a step declares no `outputs:` and its verb produced an `output` map, that map is used verbatim. |

`env`, `envInherit`, and `cwd` on an `action:` or `workflow:` step are a
validation error: `step X: env/envInherit/cwd apply only to run: steps`.

### `poll:` — the wait primitive

```yaml
poll:
  interval: 30s     # required, positive
  timeout: 20m      # required, positive
  until: "exitCode == 0"   # required CEL
```

The step's verb re-executes every `interval` until `until` evaluates true,
`timeout` expires, or the run is cancelled. For a `run:` step, a **non-zero
exit is an evaluable attempt, not a terminal fault** — `gh pr checks` exits
non-zero while checks are pending, which is exactly the loop `poll` exists
for. Outputs are sealed from the satisfying attempt; every attempt increments
the recorded `attempts` count. On expiry:
`poll: <until> not satisfied within <timeout> after N attempt(s)`.

`poll` and `retry` on one step are rejected at validate time — the poll loop
subsumes retry.

## Built-in actions

| Action | `with:` | Result (`output`) |
|---|---|---|
| `http.request` | `url` (required), `method` (default `GET`), `body`, `headers` | `{status, body, json?}` — `json` present when the body parses as JSON; 4 MiB read cap |
| `script` | `source` (required — JavaScript), `input` | `{value}` — sandboxed pure compute: no host bindings, no I/O, interrupted on timeout |
| `sleep` | `duration` (required, Go duration) | `{slept}` |

## Expressions

Expressions are [CEL](https://cel.dev), with the strings extension enabled
(`stdout.trim()` works). They appear in `if:`, `poll.until`, step `outputs`,
workflow `outputs`, and in `{{ … }}` interpolation inside `run[]`, `env`,
`cwd`, and `with` (recursively). A string that is exactly one `{{ }}` segment
keeps the expression's native type; mixed strings stringify.

| Variable | Where | Shape |
|---|---|---|
| `inputs` | everywhere | The resolved workflow inputs. |
| `connections` | everywhere | Declared connection values (granted at run time). |
| `steps.<name>` | everywhere (visibility-checked) | `{outputs, status, failed, skipped, exitCode, stdout, stderr}` of an **ancestor** step. |
| `output` | step `outputs`, `poll.until` | The verb's structured result (see action table; empty for `run:`). |
| `exitCode`, `stdout`, `stderr` | step `outputs`, `poll.until` | The `run:` result triple. Captures are capped at 4 MiB per stream. |
| `attempt` | `poll.until` | 1-based attempt counter. |

Dashed step names work: `steps.open-pr.outputs.number` is rewritten to
`steps['open-pr']…` before compilation.

### Compile-time guarantees

`orun workflow validate` (and `orun plan`, for embedded workflows) verifies:

- the DAG is acyclic and every `needs` target exists;
- every step has exactly one verb;
- every `action` resolves and its `with:` validates;
- every CEL expression compiles;
- **transitive-upstream visibility** — `steps.X` is referenceable only where
  `X` is an ancestor via `needs`. Reading a parallel branch's outputs is
  rejected as a race by construction;
- every `inputs.N` / `connections.N` reference is declared.

## Execution and run state

The scheduler is event-driven, bounded by `maxParallel`. Step status is one of
`succeeded`, `failed`, `skipped` (an `if:` that evaluated false), or `blocked`
(an upstream failed). A failed `run:` step records the full
`exitCode`/`stdout`/`stderr` triple and surfaces the stderr tail in its error.

Run state is file-backed JSON under `.orun/wfruns/<execId>/`:

```
.orun/wfruns/<execId>/
├── metadata.json          # {workflow, digest, startedAt}
├── steps/<name>.json      # per-step: status, outputs, exitCode, stdout, stderr, error, attempts
├── result.json            # {status, outputs}
└── .orun-bin/orun         # the self shim (see below)
```

**Resume** (`--resume <execId>`, or `resume: true` on a plan step) re-executes
only non-succeeded steps — `succeeded` and `skipped` stand; `failed`,
`blocked`, and in-flight states are cleared. A resumed run refuses a changed
file: `workflow digest changed (A → B) — a resumed run must execute the file
it started with`.

### How `run:` steps see the environment

The child environment is a hygienic allowlist, assembled in this order (later
wins):

1. `PATH`, `HOME`, `TMPDIR` — the base;
2. `ORUN_FLOW_SOURCE_REPO` / `ORUN_FLOW_SOURCE_REF` / `ORUN_FLOW_SOURCE_SHA` /
   `ORUN_FLOW_SOURCE_URL` — ambient provenance for
   [remotely-fetched workflows](../cli/orun-workflow.md#remote-references);
3. `envInherit` names present in the host environment;
4. `env:` declared keys (interpolated).

`PATH` is prepended with `<runDir>/.orun-bin`, which holds a shim to the very
binary running the workflow — a step that invokes `orun` always gets the
engine's own binary, never a stale install on the host `PATH`.

## Digest

`sha256:<hex>` over the file's raw bytes — printed by
`orun workflow digest`, pinned into `plan.json` and `.orun/provenance.lock`,
re-verified fail-closed at run time. The format is unchanged from v2, so
existing provenance locks stay stable.

## See also

- [Workflows](../concepts/workflow-actions.md) — the concept and the two embedded surfaces
- [`orun workflow`](../cli/orun-workflow.md) — validate / digest / run / view, remote refs, resume
- [Plan schema](../reference/plan-schema.md) — how a `workflow:` step materializes
