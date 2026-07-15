# Workflow actions — examples

`workflow:` is orun's third step/hook execution vocabulary (beside `run:` and
`use:`), served by the [torkflow](https://github.com/sourceplane/torkflow)
runtime. A workflow is a DAG of authenticated, provider-backed actions wired by
`{{ }}` expressions — the data-flow orun's shell/action steps can't express.

The full design is in [`specs/orun-workflows`](../../specs/orun-workflows/). This
folder shows the two places a workflow is used. Both run on **one backend**, draw
credentials from **orun-secrets** in-memory, and pin the workflow by **content
digest** so only the *reference*, never the *outcome*, is durable state.

## Author standalone first

```sh
orun workflow validate examples/workflows/notify-oncall.yaml
orun workflow digest   examples/workflows/notify-oncall.yaml
orun workflow run      examples/workflows/notify-oncall.yaml --set channel=ops
orun workflow view     examples/workflows/open-pr.yaml
```

`run`/`view` front the pinned engine, resolved from `ORUN_TORKFLOW_ENGINE`.

## Surface A — a `workflow:` plan step

Inside a composition job, beside `run:`/`use:` steps. It runs during `orun run`
under any runner (local/docker/gha):

```yaml
steps:
  - name: notify-oncall
    workflow: examples/workflows/notify-oncall.yaml
    with:
      channel: "{{ .env.SLACK_CHANNEL }}"
      component: "{{ .component }}"
      environment: "{{ .environment }}"
    timeout: 5m
    retry: 1
```

At `orun plan` the file is content-addressed into `plan.json`
(`{ workflow, workflowDigest, with }`); at run time the digest is re-verified,
the workflow runs, and its step timeline is sealed into `.orun/`.

## Surface B — a `workflow:` blueprint hook

In a `blueprint.yaml`, on the shipped scaffolder hook seam — the global
`postInstantiate` list or a per-phase `phases[].hooks`. Runs after the gated tree
is written, opt-in via `orun new --run-hooks`:

```yaml
hooks:
  postInstantiate:
    - id: open-pr
      workflow: examples/workflows/open-pr.yaml
      with:
        org: "{{ .orgName }}"
        serviceName: "{{ .serviceName }}"
        branch: "scaffold/{{ .serviceName }}"
```

The hook's `workflow@digest` is pinned in `.orun/provenance.lock` (reference +
digest only — never the PR URL). A hook failure leaves the valid tree in place
and is re-runnable.

## Ecosystem-neutral core

The `slack.*` / `github.*` actions above live in **torkflow's action store**, not
in orun. orun executes a declared, pinned reference and names no provider
(invariant 7).
