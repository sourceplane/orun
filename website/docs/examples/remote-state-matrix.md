---
title: Distributed execution with remote state
---

Run `orun` jobs across parallel GitHub Actions matrix workers coordinated through [orun-backend](https://github.com/sourceplane/orun-backend).  The backend enforces DAG ordering — each matrix job polls until its dependencies complete, then claims work and executes.

## How it works

1. A **plan job** generates the plan and emits the job list as a `matrix` output.
2. A **matrix fan-out** starts one runner per job ID.
3. Each runner calls `orun run --remote-state --job <id>` — the backend ensures only jobs whose dependencies have completed are claimed.

## Prerequisites

| Item | Purpose |
| --- | --- |
| orun-backend instance | Coordinates remote state and DAG ordering |
| `ORUN_BACKEND_URL` | Repository variable pointing to the backend URL |
| `id-token: write` | Workflow permission for GitHub Actions OIDC authentication |

## Authentication

The live orun-backend authenticates via **GitHub Actions OIDC**. When your workflow has `permissions: id-token: write`, orun automatically fetches an OIDC token with audience `orun` — no secrets or static tokens are needed.

Auth resolution order:

1. **GitHub Actions OIDC** — automatic when `GITHUB_ACTIONS=true` and OIDC endpoint vars are set. Audience: `orun`.
2. **Local Orun CLI session** — for laptop runs, use `orun auth login` plus `orun cloud link`.
3. **`ORUN_TOKEN`** — explicit machine-token fallback for other automation.

## Full workflow

See [`examples/github-actions/remote-state-matrix.yml`](https://github.com/sourceplane/orun/blob/main/examples/github-actions/remote-state-matrix.yml) for the complete GitHub Actions workflow.

### Workflow permissions

```yaml
permissions:
  contents: read
  id-token: write
```

### Plan generation step

```yaml
- name: Compile plan
  id: plan
  working-directory: examples/remote-state-matrix
  run: |
    orun plan --name remote-state-e2e --all
    plan_id="$(orun get plans -o json | jq -r '.[] | select(.Name == "remote-state-e2e") | .Checksum')"
    run_id="gha-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}-${plan_id}"
    echo "plan_id=${plan_id}" >> "${GITHUB_OUTPUT}"
    echo "run_id=${run_id}" >> "${GITHUB_OUTPUT}"
```

### Matrix execution step

```yaml
run-one-job-per-runner:
  needs: plan
  runs-on: ubuntu-latest
  strategy:
    fail-fast: false
    matrix:
      include: ${{ fromJson(needs.plan.outputs.jobs) }}
  env:
    ORUN_BACKEND_URL: ${{ vars.ORUN_BACKEND_URL }}
    ORUN_REMOTE_STATE: "true"
    ORUN_EXEC_ID: ${{ needs.plan.outputs.run_id }}
  steps:
    - name: Run selected job
      working-directory: examples/remote-state-matrix
      run: |
        orun run '${{ needs.plan.outputs.plan_id }}' \
          --job '${{ matrix.job }}' \
          --remote-state \
          --backend-url "${ORUN_BACKEND_URL}" \
          --gha --verbose
```

### Environment fan-out

```bash
orun run <plan_id> --env dev --remote-state
orun run <plan_id> --env stage --remote-state
```

Separate GitHub Actions jobs can run different environments against the same plan, sharing the same backend run state.

## Intent configuration

Instead of passing `--remote-state` on every command, configure it in `intent.yaml`:

```yaml
execution:
  state:
    mode: remote
    backendUrl: https://orun-api.example.workers.dev
```

With this in place, `orun run`, `orun status`, and `orun logs` automatically use the backend.

## Monitoring

From any machine with access to the backend:

```bash
orun status --remote-state --backend-url https://… --exec-id gha-12345678-1-a1b2c3 --watch
orun logs --remote-state --backend-url https://… --exec-id gha-12345678-1-a1b2c3
```

## Local remote-state conformance

```bash
orun auth login --backend-url https://orun-api.example.workers.dev
orun cloud link --backend-url https://orun-api.example.workers.dev
orun plan --name remote-state-e2e --all
export ORUN_REMOTE_STATE=true
export ORUN_EXEC_ID=local-remote-state-e2e-$(date +%s)

orun run <plan_id> --job foundation@dev.smoke --remote-state &
orun run <plan_id> --job foundation@dev.smoke --remote-state &
orun run <plan_id> --job api@dev.smoke --remote-state &
wait
```

## Related

- [Remote state flags in `orun run`](../cli/orun-run.md#remote-state-distributed-execution)
- [Environment variables](../reference/environment-variables.md)
- [`examples/remote-state-matrix/` fixture](https://github.com/sourceplane/orun/tree/main/examples/remote-state-matrix)
