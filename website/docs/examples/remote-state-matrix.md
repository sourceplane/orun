---
title: Distributed execution with remote state
---

Run `orun` jobs across parallel GitHub Actions matrix workers coordinated through [orun-backend](https://github.com/sourceplane/orun-backend).  The backend enforces DAG ordering — each matrix job polls until its dependencies complete, then claims work and executes.

## Why local remote-state?

Default `orun run` stores execution state on the local filesystem under `.orun/executions/{execID}/`. This works for a single machine but cannot coordinate between independent runners.

**Remote state** moves that coordination to orun-backend.  Every runner — whether a GitHub Actions job or a terminal on your laptop — reads and writes the same backend run record.

Running the harness locally is useful because:

- You can iterate on distributed-claim and dependency-wait behavior without pushing to CI.
- You can reproduce GitHub Actions failures from your laptop in minutes.
- You can inspect logs and status immediately via `orun status` and `orun logs`.

### How local remote-state differs from local filesystem state

| | Local filesystem state | Remote state |
|---|---|---|
| State location | `.orun/executions/{id}/` on disk | orun-backend (Cloudflare D1 + KV) |
| Coordination across machines | Not possible | Yes — shared backend run record |
| Claim enforcement | Advisory file lock (`flock`) | Backend atomic claim |
| Dependency wait | Polls local state | Polls backend `/v1/runs/{id}/runnable` |
| Auth required | No | Yes — OIDC (GHA) or Orun CLI session (local) |
| `orun status/logs` from another machine | No | Yes |

## How authentication works

### GitHub Actions

When your workflow has `permissions: id-token: write`, orun requests a GitHub Actions OIDC token with audience `orun` and sends it to the backend.  The backend cryptographically verifies the token against GitHub's JWKS endpoint.  No secrets or static tokens are needed.

### Local developer machine

Outside GitHub Actions, orun uses the credentials stored by `orun auth login` or `orun auth login --device`.  These are Orun-issued OAuth session tokens (not GitHub PATs).  The access token is refreshed automatically when it expires.

Token resolution order (for `orun run --remote-state`):

1. GitHub Actions OIDC (when `ACTIONS_ID_TOKEN_REQUEST_URL` is set)
2. `ORUN_TOKEN` environment variable (short-lived machine token, explicit fallback)
3. Stored Orun CLI session from `orun auth login`

**GitHub PATs are not the normal local auth path.**  They are never stored by the Orun CLI.  Use `orun auth login` for interactive machines and `orun auth login --device` for headless environments.

## Prerequisites

| Item | Purpose |
|---|---|
| Go 1.22+ | Build orun from source |
| `jq` | Workflow scripts and harness |
| `orun auth login` | Local remote-state auth |
| orun-backend instance | Coordinate remote state |
| `ORUN_BACKEND_URL` | URL of the backend |

Build orun:

```bash
cd /path/to/sourceplane/orun
go build -o orun ./cmd/orun
export PATH="$PWD:$PATH"
```

## Local remote-state harness

The harness at `examples/remote-state-matrix/run-local-harness.sh` proves the same backend coordination semantics as GitHub Actions without leaving your laptop.

### What it proves

- Local CLI sessions can claim, heartbeat, update, and upload logs through the backend.
- Two local processes targeting the same job do not both execute it (duplicate claim check).
- Jobs with unmet dependencies poll `/runnable` instead of failing because local state is empty.
- `orun status --remote-state` and `orun logs --remote-state` return valid data from a separate command.

### How to run

```bash
# 1. Log in
orun auth login
# or headless:
orun auth login --device

# 2. Optional: link the current repo (needed for namespaceId resolution)
orun cloud link

# 3. Run the harness from the example directory
cd examples/remote-state-matrix
./run-local-harness.sh
```

Override the backend URL if needed:

```bash
ORUN_BACKEND_URL=https://my-backend.example.com ./run-local-harness.sh
```

Pin a specific exec ID (useful for resuming or re-running the same backend run):

```bash
ORUN_EXEC_ID=local-my-test-run ./run-local-harness.sh
```

### Manual step-by-step equivalent

If you want to run the steps individually:

```bash
cd examples/remote-state-matrix

orun auth login
orun plan --name remote-state-e2e --all

PLAN_ID="$(orun get plans -o json | jq -r '.[] | select(.Name == "remote-state-e2e") | .Checksum')"
export ORUN_EXEC_ID="local-$(date +%s)-${PLAN_ID}"
export ORUN_BACKEND_URL=https://orun-api.sourceplane.ai
export ORUN_REMOTE_STATE=true

# Launch two processes for foundation@dev.smoke (duplicate claim)
orun run "${PLAN_ID}" --job foundation@dev.smoke --remote-state --backend-url "${ORUN_BACKEND_URL}" &
orun run "${PLAN_ID}" --job foundation@dev.smoke --remote-state --backend-url "${ORUN_BACKEND_URL}" &

# Launch api@dev.smoke — waits for foundation@dev.smoke via /runnable
orun run "${PLAN_ID}" --job api@dev.smoke --remote-state --backend-url "${ORUN_BACKEND_URL}" &
wait

# Verify
orun status --remote-state --backend-url "${ORUN_BACKEND_URL}" --exec-id "${ORUN_EXEC_ID}" --json
orun logs   --remote-state --backend-url "${ORUN_BACKEND_URL}" --exec-id "${ORUN_EXEC_ID}" \
  --job foundation@dev.smoke
```

## GitHub Actions conformance workflow

See [`.github/workflows/remote-state-conformance.yml`](https://github.com/sourceplane/orun/blob/main/.github/workflows/remote-state-conformance.yml) for the full conformance workflow.

### Required repository configuration

| Setting | Location | Value |
|---|---|---|
| `ORUN_BACKEND_URL` | Settings → Variables → Actions | URL of your orun-backend instance |
| `ORUN_REMOTE_STATE_E2E` | Settings → Variables → Actions | `true` to enable on push/PR (optional) |

### Workflow permissions

```yaml
permissions:
  contents: read
  id-token: write
```

The `id-token: write` permission is mandatory for OIDC authentication.

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
    echo "run_id=${run_id}"   >> "${GITHUB_OUTPUT}"
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
# Two GHA jobs, same plan, same exec ID, different env slices:
orun run <plan_id> --env dev   --remote-state
orun run <plan_id> --env stage --remote-state
```

### Trigger the conformance workflow manually

1. Actions → "orun remote-state conformance" → Run workflow
2. Select the branch

### Automatic trigger

Set the repository variable `ORUN_REMOTE_STATE_E2E=true`.  The workflow then runs automatically on push to `main` or on PRs that touch remote-state code paths.

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
orun status --remote-state --backend-url https://… --exec-id gha-12345678-1-a1b2c3 --json
orun logs   --remote-state --backend-url https://… --exec-id gha-12345678-1-a1b2c3 \
  --job foundation@dev.smoke
```

## Troubleshooting

### Missing repo access — `POST /v1/runs` returns 403

`orun run --remote-state` sends `namespaceId` in the create-run request.  If the current repo is not linked to your Orun account, `POST /v1/runs` returns 403.

Fix:

```bash
orun cloud link
```

This looks up the backend namespace for the current Git remote and saves it to `~/.orun/config.yaml`.  If the repo is not linked yet on the backend, visit the Orun dashboard to link it first.

### Expired tokens — `401 Unauthorized`

Access tokens expire.  The CLI refreshes them automatically before making requests.  If the refresh token is also expired or revoked:

```bash
orun auth logout
orun auth login
```

### Backend URL mismatch

```
--remote-state requires --backend-url or ORUN_BACKEND_URL
```

Resolution order for the backend URL:

1. `--backend-url` flag
2. `ORUN_BACKEND_URL` environment variable
3. `execution.state.backendUrl` in `intent.yaml`
4. `backend.url` in `~/.orun/config.yaml`

### Revoked refresh tokens

If you revoked your session (e.g., through `orun auth logout` on another machine) or an admin revoked it on the backend:

```bash
orun auth status           # shows "(expired)" or error
orun auth logout           # clears local credentials
orun auth login            # start a fresh session
```

### Missing OIDC permission (GitHub Actions)

```
GitHub Actions OIDC token not available: ACTIONS_ID_TOKEN_REQUEST_URL and
ACTIONS_ID_TOKEN_REQUEST_TOKEN must be set; add `id-token: write` to your
workflow permissions
```

Add to the workflow:

```yaml
permissions:
  contents: read
  id-token: write
```

### Dependency wait timeout

```
job <id>: dependency wait timeout (30m0s) exceeded
```

An upstream dependency did not complete within 30 minutes.  Check:

- Did the upstream job fail? (`orun status --remote-state`)
- Is the upstream runner still running? (check GitHub Actions job)
- Is the backend healthy? (`curl -fsS $ORUN_BACKEND_URL/`)

### Logs empty after run

`orun logs --remote-state` returns empty output even though jobs completed.

- Logs are uploaded best-effort.  If a runner crashed before uploading, logs may be missing.
- Verify the `--exec-id` matches the run you are inspecting.
- Re-run the harness with a fresh exec ID.

### Why not GitHub PATs?

GitHub PATs are long-lived credentials with broad scopes.  Storing or using a PAT as an Orun local auth token:

- Creates a credential rotation burden.
- Gives the PAT scope to the entire backend, not just your Orun session.
- Does not prove identity to the backend the way OIDC and Orun sessions do.

`orun auth login` issues a short-lived Orun access token (and a refresh token stored in your OS keychain or `~/.orun/credentials.json` at `0600`).  This token is scoped to your Orun account, not your GitHub account.

## Related

- [Remote state flags in `orun run`](../cli/orun-run.md#remote-state-distributed-execution)
- [`orun auth` commands](../cli/orun-auth.md)
- [`orun cloud link`](../cli/orun-cloud.md)
- [Environment variables](../reference/environment-variables.md)
- [`examples/remote-state-matrix/` fixture](https://github.com/sourceplane/orun/tree/main/examples/remote-state-matrix)
