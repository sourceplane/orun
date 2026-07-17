---
title: Workflow actions
---

`orun` has two execution vocabularies at the step level: `run:` (a shell
command) and `use:` (a GitHub Actions action). Both are opaque to orun's data
model — a shell step's output is unstructured text, a `use:` step is a foreign
runtime — so anything that needs to *call an authenticated system, take its
structured result, pass it to the next action, and branch on it* collapses into a
hand-rolled `curl | jq` pipeline with secrets smeared through it.

**Workflow actions** add the missing third vocabulary: **`workflow:`** — a
portable, provider-backed, connection-authenticated, expression-driven workflow,
executed by the [torkflow](https://github.com/sourceplane/torkflow) runtime. It
appears in two places that share **one** execution backend and **one** secret
bridge:

- a **`workflow:` plan step** — inside a composition job, beside `run:`/`use:`;
- a **`workflow:` blueprint hook** — in a `blueprint.yaml`, after scaffolding.

`orun` stays the compiler; torkflow becomes an execution backend bound at the
step/hook level — the precise analogue of how a `use:` step selects the
github-actions runner.

## The load-bearing law

> **A workflow is execution, never intent.**

The plan and the scaffolder's provenance lock capture only a workflow's
**reference + content digest + declared inputs** — never its runtime outcome.
That single rule is what lets a live, branching, side-effecting engine run
*inside* a compiler whose headline property is a byte-identical plan:

- `plan.json` carries `{ workflow, workflowDigest, with }` on the step and folds
  all of it into the plan checksum. **None** of the workflow's runtime output
  does — identical inputs still produce a byte-identical plan.
- At run time the workflow's step timeline and final context are **sealed into
  `.orun/`** as that step's output — the same place a `run:` step's captured
  output goes. There is no split-brain `.runs/` tree. Side effects the workflow
  caused (a PR URL, a message timestamp) appear in that sealed log as run facts;
  they are never promoted into the plan.

The test for any field: *would it differ between two runs with identical inputs?*
If yes, it is an outcome, and it stays on the execution side of the line.

## Surface A — a `workflow:` plan step

Inside a composition job, a step is exactly one of `run` / `use` / `workflow` (a
step that sets more than one is a compile error):

```yaml
steps:
  - name: notify-oncall
    workflow: workflows/notify-oncall.yaml   # a torkflow/v1 file
    with:
      channel: "{{ .env.SLACK_CHANNEL }}"
      component: "{{ .component }}"
      environment: "{{ .environment }}"
    timeout: 5m
    retry: 1
    onFailure: stop
```

At `orun plan` the referenced file is resolved (relative to the intent
directory), content-addressed, and materialized into the plan as
`{ workflow, workflowDigest, with }`. At `orun run` the executor:

1. re-verifies the on-disk file still matches the pinned digest — a workflow that
   changed since the plan is a hard error (fail-closed);
2. runs it through the pinned engine, injecting orun-resolved credentials
   in-memory and `with` as the workflow's Trigger context;
3. returns the run summary as the step output, sealed into `.orun/`.

A `workflow:` step runs under **any** runner — `local`, `docker`, or
`github-actions` — because the executor only shells the pinned engine. This is
unlike `use:`, which forces the github-actions runner. A failed workflow returns
a step error, so the job honors `timeout` / `retry` / `onFailure` — orun retries
the workflow as a black box; the workflow retries its own actions internally.

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

# the global list — runs last, after the whole scaffold
hooks:
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
`orun new upgrade` can tell whether a hook workflow changed.

Because hooks run after a passing gate, the tree is always valid: a hook failure
exits non-zero with the materialized tree left in place and a precise "scaffold
succeeded, hook failed" message, and the hook is re-runnable. There is no
pre-placement hook — a precondition like *ensure the repo exists* folds into the
workflow's own idempotent first action (`getOrCreate`).

## Secrets

Credentials come from `orun`'s own secret system, never a second store. For a
plan step, the job's resolved `secret://` values are injected into the engine
request in-memory; for a blueprint hook, the blueprint's `secret: true` inputs
are. Secrets are never written to the workflow file, the plan, the provenance
lock, or the sealed run — and the resolved values are swept from any output orun
persists or prints by the same redactor that masks shell-step output.

## The engine

The workflow engine is resolved and pinned by content digest. `orun` invokes it
as a subprocess over a JSON contract — the same process boundary torkflow uses
for its own providers — so no engine internals leak into orun. Point orun at the
engine binary with the `ORUN_TORKFLOW_ENGINE` environment variable (see
[Environment variables](../reference/environment-variables.md)). A plan or
scaffold that needs a workflow but finds no engine fails with a clear pre-flight
error, never a mid-step crash.

The provider actions a workflow calls (`slack.*`, `github.*`, `http.*`, …) live
in **torkflow's action store**, never in orun. `orun`'s core names no provider —
a build-time lint fails on any provider literal in the workflow execution code.

## v2: the data-flow evolution

The `orun-workflows-v2` epic grew the vocabulary in four user-visible ways:

**Connections grant** — credentials cross the boundary only through a declared,
compile-checked mapping from the workflow's own connection names to `secret://`
references. The plan is the reviewable grant; unmapped secrets never cross.

```yaml
- name: notify
  workflow: wf/notify.yaml
  connections:
    slack-main:
      token: secret://acme/api/prod/SLACK_BOT_TOKEN
```

**Outputs** — a workflow declares named outputs (`spec.outputs`), and later
steps of the same job consume them with `${{ steps.<id>.outputs.<name> }}`.
References are validated at plan time against the pinned file's declared names;
values are substituted at run time and sealed as run facts.

```yaml
- name: get-oncall
  workflow: wf/oncall.yaml         # declares spec.outputs.email
- name: page
  run: ./page.sh ${{ steps.get-oncall.outputs.email }}
```

**Engine pin** — declare the engine in intent (`execution.workflowEngine:
{ ref, digest }`); the pin materializes into the plan and a mismatched engine
refuses to run. Author it with `orun workflow engine-digest`.

**Resume & approvals** — `resume: true` beside `retry:` re-executes only the
steps that failed (over the engine's run store); an `approval:` block pauses a
workflow step for a human decision, resolved with `orun approve`, with a
mandatory `timeout` and declared `onTimeout` policy. The pause and the verdict
are sealed run facts — a plan is byte-identical whether it was approved or
rejected.

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
a content-addressed path, pinning identically to a local copy.

## Authoring standalone

Before wiring a workflow into a step or hook, author and debug it directly:

```bash
orun workflow validate workflows/notify-oncall.yaml   # structural check
orun workflow digest   workflows/notify-oncall.yaml   # the digest orun would pin
orun workflow run      workflows/notify-oncall.yaml --set channel=ops
orun workflow view     workflows/open-pr.yaml          # render its DAG
```

See the [`orun workflow`](../cli/orun-workflow.md) command reference.

## See also

- [`orun workflow`](../cli/orun-workflow.md) — the authoring subcommand
- [Execution model](./execution-model.md) — how plan and run stay separate
- [Compositions](./compositions.md) — where a `workflow:` job step is authored
- [Plan schema](../reference/plan-schema.md) — the plan step shape
