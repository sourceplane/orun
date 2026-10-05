---
title: Distributed execution with remote state
description: Run one plan across parallel GitHub Actions matrix jobs, or several machines, coordinated through remote state on Orunbase or a self-hosted backend, so the plan's ordering holds no matter how many runners execute it.
---

A plan's dependency order is part of the standard it encodes, and it has to hold even when
the jobs run on many machines at once. With **remote state**, every runner, whether a
GitHub Actions matrix job or a terminal on a laptop, reads and writes one shared run
record. Each job claims its work atomically and waits until its dependencies have
succeeded, so the DAG compiled into the plan is enforced across runners exactly as it is on
one machine.

Remote state is served by **Orunbase**, the hosted control plane, by default. You can
self-host a backend on Cloudflare with [`orun backend init`](../cli/orun-backend.md) and
point orun at it instead.

:::tip Terraform state needs no AWS either
Remote runs also export a `TF_HTTP_*` environment pointing Terraform's
`backend "http"` at the platform's native state store, so a matrix job needs
no S3 bucket or OIDC role for Terraform state — see
[Terraform state on the platform](../execute/terraform-state.md).
:::

## Local state and remote state

By default, `orun run` records execution in the object model under `.orun/objectmodel/`.
That works on one machine but cannot coordinate independent runners. `--remote-state`
moves coordination to the backend.

| | Local state | Remote state |
|---|---|---|
| Where the run record lives | `.orun/objectmodel/` on disk | The backend (Orunbase, or your own) |
| Coordination across machines | No | Yes: one shared run record |
| Claiming a job | Advisory file lock (`flock`) | Atomic claim on the backend |
| Waiting on dependencies | Polls local state | Polls the backend's runnable set |
| Authentication | None | GitHub Actions OIDC, or your `orun auth login` session |
| `orun status` / `orun logs` from another machine | No | Yes |

## Which backend

`orun run --remote-state` (and `orun status` / `orun logs` with `--remote-state`) resolve
the backend in this order:

1. `--backend-url`
2. `ORUN_BACKEND_URL`
3. `execution.state.backendUrl` in `intent.yaml`
4. `cloud.url` in `~/.orun/config.yaml`
5. Orunbase's production API, when nothing else names one

So on Orunbase you need no URL at all. Set one only for a self-hosted backend.

## Authentication

**On GitHub Actions**, give the workflow `permissions: id-token: write`. orun requests a
GitHub Actions OIDC token with audience `orun`, and the backend verifies it against
GitHub's signing keys. No secrets or static tokens are needed.

**On your machine**, sign in once with `orun auth login` (or `orun auth login --device` on
a headless terminal). The session is refreshed automatically. The repository is linked to
a workspace on first use; `orun cloud link` does it explicitly. See
[workspaces and tenancy](../concepts/workspaces-and-tenancy.md).

The credential is resolved in this order:

1. GitHub Actions OIDC, when `ACTIONS_ID_TOKEN_REQUEST_URL` is set
2. `ORUN_TOKEN` (or `ORUN_TOKEN_FILE`), a short-lived machine token for headless use
3. The session stored by `orun auth login`

GitHub personal access tokens are never used or stored.

## Prerequisites

| Item | Purpose | Required |
|---|---|---|
| orun | The CLI | Yes |
| `jq` | The harness and workflow scripts | Yes |
| `orun auth login` | Authentication for local runs | For local runs |
| A self-hosted backend and its URL | Only if you do not use Orunbase | No |

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

The harness passes its own default backend URL to every command; set
`ORUN_BACKEND_URL` to point it at Orunbase's API or at your own backend:

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
export ORUN_REMOTE_STATE=true
# Optional, for a self-hosted backend: export ORUN_BACKEND_URL=https://…

# Launch two processes for foundation.dev.smoke (duplicate claim; the workspace is resolved on first call)
orun run "${PLAN_ID}" --job foundation.dev.smoke --remote-state &
orun run "${PLAN_ID}" --job foundation.dev.smoke --remote-state &

# Launch api.dev.smoke: it waits until foundation.dev.smoke has succeeded
orun run "${PLAN_ID}" --job api.dev.smoke --remote-state &
wait

# Verify
orun status --remote-state --exec-id "${ORUN_EXEC_ID}" --json
orun logs   --remote-state --exec-id "${ORUN_EXEC_ID}" --job foundation.dev.smoke
```

## GitHub Actions conformance workflow

See [`.github/workflows/remote-state-conformance.yml`](https://github.com/sourceplane/orun/blob/main/.github/workflows/remote-state-conformance.yml) for the full conformance workflow.

### Required repository configuration

| Setting | Location | Value |
|---|---|---|
| `ORUN_BACKEND_URL` | Settings → Variables → Actions | Backend URL; leave unset to use Orunbase |
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
    workspace: ws_3KF9TQ2P   # Workspace ID (short, immutable); a slug or org_… id is also accepted
```

With this in place, `orun run`, `orun status`, and `orun logs` automatically use the backend. The `workspace` field is the leading spelling of the committed tenancy claim; `org` is a retained alias.

## Monitoring

From any machine with access to the backend:

```bash
orun status --remote-state --backend-url https://… --exec-id gha-12345678-1-a1b2c3 --json
orun logs   --remote-state --backend-url https://… --exec-id gha-12345678-1-a1b2c3 \
  --job foundation.dev.smoke
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
orun auth status
```

### Talking to the wrong backend

If runs, status, or logs do not appear where you expect, check which backend orun
resolved. The order is `--backend-url`, then `ORUN_BACKEND_URL`, then
`execution.state.backendUrl` in `intent.yaml`, then `cloud.url` in `~/.orun/config.yaml`
(the older `backend.url` key still works, with a deprecation warning), then Orunbase's
production API. `orun mcp doctor` probes the backend it resolves and reports what it found.

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
