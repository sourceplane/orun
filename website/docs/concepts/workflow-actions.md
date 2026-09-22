---
title: Workflows
---

`orun` has two execution vocabularies at the plan-step level: `run:` (a shell
command) and `use:` (a GitHub Actions action). Both are opaque to orun's data
model — a shell step's output is unstructured text, a `use:` step is a foreign
runtime — so anything that needs to *call an authenticated system, take its
structured result, pass it to the next action, and branch on it* collapses into a
hand-rolled `curl | jq` pipeline with secrets smeared through it.

**Workflows** add the missing third vocabulary: **`workflow:`** — a portable,
connection-authenticated, expression-driven workflow file, executed by orun's
own **in-process engine**. It appears in two places that share **one** engine
and **one** secret bridge:

- a **`workflow:` plan step** — inside a composition job, beside `run:`/`use:`;
- a **`workflow:` blueprint hook** — in a `blueprint.yaml`, after scaffolding.

Since **orun-workflows-v3** there is no external engine: the workflow language
(`apiVersion: orun.dev/v1, kind: Workflow`) is parsed, validated, and executed
by the same binary that compiles the plan. The former torkflow engine boundary
— the subprocess contract, the engine pin, `ORUN_TORKFLOW_ENGINE` — is deleted.
Workflow files from the torkflow era are **not** accepted; see
[Upgrading from torkflow](#upgrading-from-torkflow).

## The load-bearing law

> **A workflow is execution, never intent.**

The plan and the scaffolder's provenance lock capture only a workflow's
**reference + content digest + declared inputs** — never its runtime outcome.
That single rule is what lets a live, branching, side-effecting engine run
*inside* a compiler whose headline property is a byte-identical plan:

- `plan.json` carries `{ workflow, workflowDigest, with }` on the step and folds
  all of it into the plan checksum. **None** of the workflow's runtime output
  does — identical inputs still produce a byte-identical plan.
- At run time the workflow's step timeline and final outputs are **sealed into
  `.orun/`** as that step's output — the same place a `run:` step's captured
  output goes. Side effects the workflow caused (a PR URL, a message timestamp)
  appear in that sealed log as run facts; they are never promoted into the plan.

The test for any field: *would it differ between two runs with identical inputs?*
If yes, it is an outcome, and it stays on the execution side of the line.

## The workflow language

A workflow file is a DAG of named steps. Each step performs exactly one verb:

- **`run:`** — an argv array, executed directly, **never through a shell**. A
  `;` or `$( )` in an argument is a literal. Shell behavior is an explicit,
  review-visible opt-in: `["bash", "-lc", "…"]`.
- **`action:`** — a built-in action: `http.request`, `script` (sandboxed JS,
  pure compute, no I/O), or `sleep`.
- **`workflow:`** — a nested workflow run (cycle-detected).

Edges are pull-only `needs:` lists; expressions are
[CEL](https://cel.dev) — in `if:`, `poll.until`, declared `outputs:`, and
`{{ … }}` interpolation inside `run[]`, `env`, `cwd`, and `with`. Validation is
a **compile check**: `orun workflow validate` verifies the DAG, verb
exclusivity, action `with:` shapes, CEL compilation, and reference visibility —
a step may only reference `steps.X` when `X` is a transitive ancestor via
`needs`, so reading a parallel branch's outputs is rejected as a race by
construction.

```yaml
apiVersion: orun.dev/v1
kind: Workflow
metadata:
  name: open-and-wait
inputs:
  repo: { type: string, required: true }
steps:
  - name: open-pr
    run: ["gh", "pr", "create", "--repo", "{{ inputs.repo }}", "--fill", "--json", "number"]
    outputs:
      number: "output.json.number"
  - name: wait-for-ci
    needs: [open-pr]
    run: ["gh", "pr", "checks", "{{ steps.open-pr.outputs.number }}", "--json", "state"]
    poll: { interval: 30s, timeout: 20m, until: "exitCode == 0" }
outputs:
  pr: "steps.open-pr.outputs.number"
```

`poll:` is the wait primitive: the verb re-executes every `interval` until
`until` is true or `timeout` expires — and a `run:` step's non-zero exit is an
evaluable attempt, not a terminal fault, which is exactly what a
"poll `gh pr checks` until it exits 0" loop needs. `retry:` handles transient
faults; `poll` and `retry` on one step are rejected (the poll loop subsumes
retry).

The complete field-by-field reference — schema, CEL variables, built-in
actions, run-state layout, resume semantics — lives at
[Workflow schema](../reference/workflow-schema.md).

:::note One word, two dialects
A **plan step's** `run:` (in a composition job) is a shell string executed on a
CI runner; a **workflow step's** `run:` is an argv array executed with no shell.
The asymmetry is deliberate: plan steps live in the CI idiom, workflow steps in
the hygienic-execution idiom.
:::

## Surface A — a `workflow:` plan step

Inside a composition job, a step is exactly one of `run` / `use` / `workflow` (a
step that sets more than one is a compile error):

```yaml
steps:
  - name: notify-oncall
    workflow: workflows/notify-oncall.yaml   # an orun.dev/v1 Workflow file
    with:
      channel: "{{ .env.SLACK_CHANNEL }}"
      component: "{{ .component }}"
      environment: "{{ .environment }}"
    timeout: 5m
    retry: 1
    onFailure: stop
```

At `orun plan` the referenced file is resolved (relative to the intent
directory), **fully validated** (an invalid workflow fails the plan, not the
run), content-addressed, and materialized into the plan as
`{ workflow, workflowDigest, with }`. The step's `connections:` grant is checked
against the file's declared connections, and `${{ steps.X.outputs.Y }}`
references against its declared outputs. At `orun run` the executor:

1. re-verifies the on-disk file still matches the pinned digest — a workflow that
   changed since the plan is a hard error (fail-closed);
2. runs it **in-process**, with `with:` as the workflow's inputs and the
   resolved `secret://` connection values injected in-memory;
3. returns the run summary as the step output, sealed into `.orun/`.

A `workflow:` step runs under **any** runner — `local`, `docker`, or
`github-actions` — because the engine travels inside the orun binary itself.
This is unlike `use:`, which forces the github-actions runner. A failed workflow
returns a step error, so the job honors `timeout` / `retry` / `onFailure` — orun
retries the workflow as a black box; the workflow retries its own steps
internally. Each workflow step keeps its run state under `.orun/wfruns/`, so a
step-level retry re-enters the latest recorded exec and re-runs only what did
not succeed.

## Surface B — a `workflow:` blueprint hook

A [blueprint](./compositions.md) hook can be a workflow instead of a bare argv.
Hooks run **after** the gated tree is written, opt-in via `orun new --run-hooks`,
in two granularities:

```yaml
# a per-phase hook — runs after this phase's modules are placed
phases:
  - name: contracts
    modules: [contracts, sdk]
    hooks:
      - id: verify-contracts
        workflow: workflows/verify-contracts.yaml

# the run-level lists — preInstantiate before the first phase, postInstantiate
# after the whole scaffold
hooks:
  preInstantiate:
    - id: epic
      uses: orun.task/ensure@v1
      with: { kind: epic, slug: "{{ .serviceName }}", name: "{{ .serviceName }}" }
  postInstantiate:
    - id: open-pr
      workflow: workflows/open-pr.yaml
      with:
        org: "{{ .orgName }}"
        serviceName: "{{ .serviceName }}"
        branch: "scaffold/{{ .serviceName }}"
```

A hook is exactly one of `run` / `workflow`. Each workflow hook is pinned in
`.orun/provenance.lock` by `{ id, phase, workflow, digest }` — reference and
digest only, never the outcome — recorded even when `--run-hooks` is off, so an
`orun new upgrade` can tell whether a hook workflow changed. The digest format
(`sha256:<hex>` over the raw bytes) is unchanged from v2, so existing
provenance locks stay stable.

Because hooks run after a passing gate, the tree is always valid: a hook failure
exits non-zero with the materialized tree left in place and a precise "scaffold
succeeded, hook failed" message, and the hook is re-runnable. There is no
pre-placement hook — a precondition like *ensure the repo exists* folds into the
workflow's own idempotent first step.

## Secrets and connections

Credentials come from `orun`'s own secret system, never a second store.
Credentials cross the boundary only through a declared, compile-checked
**connections grant** — a mapping from the workflow's own connection names to
`secret://` references. The plan is the reviewable grant; unmapped secrets never
cross, and nothing ambient leaks in:

```yaml
- name: notify
  workflow: wf/notify.yaml
  connections:
    slack-main:
      token: secret://acme/api/prod/SLACK_BOT_TOKEN
```

Inside the workflow, an `http.bearer` connection injects
`Authorization: Bearer <token>` on the `action: http.request` steps that name it
via `connection:` — mapped-only, never via the environment. Secrets are never
written to the workflow file, the plan, the provenance lock, or the sealed run —
and resolved values are swept from any output orun persists or prints by the
same redactor that masks shell-step output.

For standalone runs, `orun workflow run --connection name.field=value` is the
grant.

## Outputs, resume, and approvals

**Outputs** — a workflow declares named outputs (CEL over its final state), and
later steps of the same job consume them with `${{ steps.<id>.outputs.<name> }}`.
References are validated at plan time against the pinned file's declared names;
values are substituted at run time and sealed as run facts. Outputs are
**declared-only**: a workflow never dumps its raw context across the boundary.

```yaml
- name: get-oncall
  workflow: wf/oncall.yaml         # declares outputs.email
- name: page
  run: ./page.sh ${{ steps.get-oncall.outputs.email }}
```

**Resume** — `resume: true` beside `retry:` re-executes only the steps that did
not succeed, over the engine's file-backed run store. A resumed run must execute
the file it started with — a changed digest is refused.

**Approvals** — an `approval:` block pauses a workflow step for a human
decision, resolved with `orun approve`, with a mandatory `timeout` and declared
`onTimeout` policy. The pause and the verdict are sealed run facts — a plan is
byte-identical whether it was approved or rejected.

```yaml
- name: promote
  workflow: wf/promote.yaml
  retry: 1
  resume: true
  approval:
    prompt: "Promote to production?"
    timeout: 24h
    onTimeout: fail
```

Workflows can also ship **inside a composition Stack**: a reference that isn't
in your repo resolves from the golden path's own package and is materialized at
a content-addressed path, pinning identically to a local copy. And a standalone
run can reference a workflow that lives in another repo entirely — see
[remote references](../cli/orun-workflow.md#remote-references).

## Upgrading from torkflow

orun-workflows-v3 ships **no converter** and no compatibility layer. What to
know when upgrading:

- A `torkflow/v1` file is rejected by name with a pointer to the manual mapping
  (`specs/orun-workflows-v3` design §12). The headline moves: the `spec:`
  wrapper is gone, `outboundEdges`/`nextStepName`/`branchName` become `needs:`
  pull edges, `actionRef` becomes one of the three verbs, `Trigger.*` references
  become `inputs.*`, and `maxParallelSteps` becomes `maxParallel`.
- An intent that declares `execution.workflowEngine` **fails to load** — delete
  the block. `orun workflow engine-digest` no longer exists, and
  `ORUN_TORKFLOW_ENGINE` is ignored because nothing reads it.
- Digests are unchanged (`sha256:` over raw bytes) — re-pinning a file that did
  not change does not change your plan or provenance lock.
- The v2 grant/outputs/resume/approvals surfaces described above are unchanged.

## Authoring standalone

Before wiring a workflow into a step or hook, author and debug it directly:

```bash
orun workflow validate flows/release.yaml   # full compile check
orun workflow digest   flows/release.yaml   # the digest orun would pin
orun workflow run      flows/release.yaml --set service=api
orun workflow view     flows/release.yaml   # render its DAG
```

See the [`orun workflow`](../cli/orun-workflow.md) command reference.

## See also

- [`orun workflow`](../cli/orun-workflow.md) — the authoring subcommand
- [Workflow schema](../reference/workflow-schema.md) — the complete field reference
- [Execution model](./execution-model.md) — how plan and run stay separate
- [Compositions](./compositions.md) — where a `workflow:` job step is authored
- [Plan schema](../reference/plan-schema.md) — the plan step shape
