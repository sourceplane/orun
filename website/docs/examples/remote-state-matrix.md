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

On the first `orun run --remote-state` outside GitHub Actions, the CLI auto-resolves the repo namespace from the current Git remote by calling `POST /v1/accounts/repos/link` with the active CLI session, then caches the result in `~/.orun/config.yaml`.  Subsequent runs use the cached namespace ID.

Token resolution order (for `orun run --remote-state`):

1. GitHub Actions OIDC (when `ACTIONS_ID_TOKEN_REQUEST_URL` is set)
2. `ORUN_TOKEN` environment variable (short-lived machine token, explicit fallback — requires pre-cached namespace link)
3. Stored Orun CLI session from `orun auth login` (auto-resolves namespace on first run)

**GitHub PATs are not the normal local auth path.**  They are never stored by the Orun CLI.  Use `orun auth login` for interactive machines and `orun auth login --device` for headless environments.

## Prerequisites

| Item | Purpose | Required |
|---|---|---|
| Go 1.22+ | Build orun from source | Yes |
| `jq` | Harness and workflow scripts | Yes |
| `orun auth login` | Local remote-state auth | Yes |
| `orun cloud link` | Pre-cache repo namespace (optional — auto-resolved on first run) | No |
| orun-backend instance | Coordinate remote state | Yes |
| `ORUN_BACKEND_URL` | URL of the backend | Yes |

### Install orun

```bash
cd /path/to/sourceplane/orun
go build -o orun ./cmd/orun
export PATH="$PWD:$PATH"
```

Or from the module path:

```bash
go install github.com/sourceplane/orun/cmd/orun@latest
```

### Install jq

```bash
# macOS
brew install jq

# Ubuntu / Debian
apt-get install -y jq

# Fedora / RHEL
dnf install -y jq
```

## Local remote-state harness

The harness at `examples/remote-state-matrix/run-local-harness.sh` proves the same backend coordination semantics as GitHub Actions without leaving your laptop.

### What it proves (live run)

- Local CLI sessions can claim, heartbeat, update, and upload logs through the backend.
- Two local processes targeting the same job do not both execute it (duplicate claim check: exactly one log contains execution markers, the other contains zero).
- Jobs with unmet dependencies poll `/runnable` instead of failing because local state is empty.
- `orun status --remote-state` returns `success` for expected jobs from a separate command.
- `orun logs --remote-state` returns non-empty output for the executed job.

### What ORUN_DRY_RUN=1 proves (no credentials needed)

`ORUN_DRY_RUN=1 ./run-local-harness.sh` prints the full intended command sequence and exits 0 **without making any real backend calls**.

Dry-run mode verifies:
- Command construction is correct (right flags, right job IDs, right backend URL).
- Shared `ORUN_EXEC_ID` is exported before concurrent processes launch.
- Duplicate and dep-wait processes are included.
- Status and log commands follow the run.

Dry-run mode does **not** prove:
- Live backend duplicate-claim enforcement.
- Real dependency polling via `/runnable`.
- Actual remote status or log content.

Use dry-run for CI structure checks.  Use the live run (after `orun auth login`) to prove real backend behavior.  `orun cloud link` is optional — namespace is auto-resolved on first run.

### How to run (live)

```bash
# 1. Log in (required)
orun auth login
# or headless:
orun auth login --device

# 2. Run the harness — namespace is auto-resolved on first run
cd examples/remote-state-matrix
./run-local-harness.sh
```

Override the backend URL:

```bash
ORUN_BACKEND_URL=https://my-backend.example.com ./run-local-harness.sh
```

Pin a specific exec ID:

```bash
ORUN_EXEC_ID=local-my-test-run ./run-local-harness.sh
```

### How to run (dry-run — no credentials)

```bash
cd examples/remote-state-matrix
ORUN_DRY_RUN=1 ./run-local-harness.sh
```

### Manual step-by-step equivalent

```bash
cd examples/remote-state-matrix

orun auth login

orun plan --name remote-state-e2e --all

PLAN_ID="$(orun get plans -o json | jq -r '.[] | select(.Name == "remote-state-e2e") | .Checksum')"
export ORUN_EXEC_ID="local-$(date +%s)-${PLAN_ID}"
export ORUN_BACKEND_URL=https://orun-api.sourceplane.ai
export ORUN_REMOTE_STATE=true

# Launch two processes for foundation@dev.smoke (duplicate claim — namespace auto-resolved on first call)
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

### Trigger the conformance workflow

**Manual trigger** (always available):

1. Actions → "orun remote-state conformance" → Run workflow
2. Select the branch

**Automatic** (when `ORUN_REMOTE_STATE_E2E=true`):

Set the repository variable and the workflow runs automatically on push to `main` or on PRs touching remote-state code paths.

**Dry-run guard** (always-on, no credentials):

The `Harness dry-run guard` job always runs on every PR.  It checks harness syntax, dry-run command construction, and assertion helper correctness without requiring a live backend.

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

### `orun` not found

```
'orun' not found on PATH.
```

Install:

```bash
go install github.com/sourceplane/orun/cmd/orun@latest
# or build from source:
go build -o orun ./cmd/orun && export PATH="$PWD:$PATH"
# or set ORUN_BIN to the full path:
ORUN_BIN=/path/to/orun ./run-local-harness.sh
```

### `jq` not found

```
'jq' not found on PATH.
```

Install:

```bash
brew install jq           # macOS
apt-get install -y jq     # Debian/Ubuntu
dnf install -y jq         # Fedora/RHEL
```

### Missing auth — not logged in

```
Not logged in to Orun.
```

Run:

```bash
orun auth login              # browser OAuth (interactive)
orun auth login --device     # device flow (headless / SSH)
```

### Missing repo link — namespace not found

```
repo sourceplane/orun is not known to your Orun session; run `orun auth login` again to refresh namespace access
```

The backend does not have slug data for this repo in your session.  This typically means the session was created before the backend recorded namespace slug mappings.

Fix:

```bash
orun auth login
```

After re-login, re-run `orun run --remote-state` — namespace is auto-resolved from the fresh session.

### No Git remote found

```
Could not determine the current Git remote from 'orun auth status'.
```

Ensure you are running from within a Git repository that has a GitHub remote:

```bash
git remote -v   # should show a github.com origin
orun cloud link
```

### Expired tokens — `401 Unauthorized`

Access tokens expire.  The CLI refreshes them automatically.  If the refresh token is also expired or revoked:

```bash
orun auth logout
orun auth login
```

Check status:

```bash
orun auth status --backend-url https://orun-api.sourceplane.ai
```

### Backend URL mismatch

```
--remote-state requires --backend-url or ORUN_BACKEND_URL
```

Resolution order:

1. `--backend-url` flag
2. `ORUN_BACKEND_URL` environment variable
3. `execution.state.backendUrl` in `intent.yaml`
4. `backend.url` in `~/.orun/config.yaml`

### Revoked refresh tokens

```
orun auth status    # shows "(expired)" or error
orun auth logout    # clears local credentials
orun auth login     # start a fresh session
```

### Missing OIDC permission (GitHub Actions)

```
GitHub Actions OIDC token not available
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

Check:

- Did the upstream job fail? (`orun status --remote-state`)
- Is the upstream runner still running? (check GitHub Actions job)
- Is the backend healthy? (`curl -fsS $ORUN_BACKEND_URL/`)

### Logs empty after run

`orun logs --remote-state` returns empty output.

- Logs are uploaded best-effort.  If a runner crashed before uploading, logs may be missing.
- Verify `--exec-id` matches the run you are inspecting.
- Re-run the harness with a fresh exec ID.

### Why not GitHub PATs?

GitHub PATs are long-lived credentials with broad scopes.  `orun auth login` issues a short-lived Orun access token scoped to your Orun account, stored in your OS keychain or `~/.orun/credentials.json` at `0600`.  This is more secure and does not require rotation.

## Related

- [Remote state flags in `orun run`](../cli/orun-run.md#remote-state-distributed-execution)
- [`orun auth` commands](../cli/orun-auth.md)
- [`orun cloud link`](../cli/orun-cloud.md)
- [Environment variables](../reference/environment-variables.md)
- [`examples/remote-state-matrix/` fixture](https://github.com/sourceplane/orun/tree/main/examples/remote-state-matrix)
