# Remote State Matrix Example

This example demonstrates distributed `orun` execution across multiple components and environments using the orun-backend for coordination. It exercises:

- Local filesystem state with advisory file locking
- Remote state coordination via orun-backend in GitHub Actions
- Remote status and log inspection

## What This Proves

The fixture defines a 3-component, 2-environment DAG:

```
foundation@dev.smoke     (no deps)
foundation@stage.smoke   (no deps)
api@dev.smoke            depends on foundation@dev.smoke
api@stage.smoke          depends on foundation@stage.smoke
web@dev.smoke            depends on api@dev.smoke
web@stage.smoke          depends on api@stage.smoke
```

When run via remote state in a GitHub Actions matrix, this DAG proves:

1. A single plan compiles once and is addressed by checksum prefix.
2. Compiled plan artifacts can be shared to parallel runners.
3. Matrix runners share a deterministic `ORUN_EXEC_ID` for the same backend run.
4. Each runner claims exactly one job via `--job`.
5. Duplicate runners exit cleanly when a job is already running or complete.
6. Jobs with unmet dependencies poll remote state until their deps finish.
7. Environment fan-out (`--env dev`, `--env stage`) works against the same plan.
8. `orun status --remote-state` and `orun logs --remote-state` work post-run.

## Prerequisites

- Go 1.22+ (to build orun from source)
- `jq` (for workflow scripts)

Build orun:

```bash
cd /path/to/sourceplane/orun
go build -o orun ./cmd/orun
export PATH="$PWD:$PATH"
```

Or use `go run ./cmd/orun` in place of `orun` below.

## Local Default State Test

### Validate the intent

```bash
orun validate --intent examples/remote-state-matrix/intent.yaml
```

### Compile a plan

```bash
cd examples/remote-state-matrix
orun plan --name remote-state-e2e --all
```

This seals the plan into the object model under `.orun/objectmodel/` and names it `remote-state-e2e`, so the commands below run it by name.

### Run all jobs locally

```bash
orun run remote-state-e2e
```

### Check status

```bash
orun status --exec-id <exec-id>
```

The exec-id is printed during the run. For the latest run:

```bash
orun status
```

### View logs

```bash
orun logs --exec-id <exec-id> --job foundation@dev.smoke
```

## Local State Lock / Concurrency Smoke

The local state backend uses advisory file locks (`flock(2)`) scoped to each execution ID. This prevents two processes sharing the same `--exec-id` from claiming the same job concurrently.

### Choose a shared execution ID

```bash
export ORUN_EXEC_ID=smoke-lock-test
```

### Run two processes concurrently

Open two terminals in `examples/remote-state-matrix/`:

```bash
# Terminal 1
orun run remote-state-e2e --job foundation@dev.smoke --exec-id smoke-lock-test

# Terminal 2 (start immediately after terminal 1)
orun run remote-state-e2e --job foundation@dev.smoke --exec-id smoke-lock-test
```

### Expected behavior

- **Terminal 1**: Claims `foundation@dev.smoke` and executes its steps.
- **Terminal 2**: Sees `foundation@dev.smoke` as `running` (or `completed`) and exits cleanly without re-executing.

Only one process performs the actual job execution.

### Inspect state

```bash
cat .orun/executions/smoke-lock-test/state.json | jq .
```

You should see `foundation@dev.smoke` with status `"completed"` (or `"running"` if checked mid-execution). The lock file lives at:

```
.orun/executions/smoke-lock-test/.lock
```

This is an advisory lock file used by `flock(2)`. It auto-releases when the process exits.

### Clean generated output

```bash
rm -rf .orun/
```

Or clean a specific execution:

```bash
rm -rf .orun/executions/smoke-lock-test/
```

## Remote State in GitHub Actions

### Required repository configuration

| Setting | Location | Value |
| --- | --- | --- |
| `ORUN_BACKEND_URL` | Settings → Variables → Actions | `https://orun-api.rahulvarghesepullely.workers.dev` |
| `REMOTE_STATE_TESTS` | Settings → Variables → Actions | `true` (enables on push/PR) |

### Required workflow permissions

```yaml
permissions:
  contents: read
  id-token: write
```

The `id-token: write` permission is mandatory. The live backend authenticates via GitHub Actions OIDC JWT verification — it does not accept static `ORUN_TOKEN` for mutable operations (claim, update, log upload).

### Why OIDC

The live orun-backend verifies the caller's identity via the GitHub Actions OIDC token, which cryptographically proves the request came from a specific GitHub Actions workflow run. This is more secure than a shared static token because:

- No secret rotation needed
- Tokens are ephemeral and scoped to a single workflow run
- The backend can enforce repository-level access control

### How to trigger the conformance workflow

**Manual trigger** (always available):

1. Go to Actions → "remote-state-conformance" → Run workflow
2. Select the branch

**Automatic** (when `REMOTE_STATE_TESTS=true`):

Runs on push to `main` or on PRs that touch remote-state code paths.

### How to find the run ID

The run ID is printed in the "Compile plan" step output:

```
run_id=gha-<GITHUB_RUN_ID>-<GITHUB_RUN_ATTEMPT>-<plan_checksum>
```

You can also find it in the verify step's status JSON output.

## Remote Status and Log Inspection

After a remote-state run completes, inspect from any machine:

### Status

```bash
ORUN_BACKEND_URL=https://orun-api.rahulvarghesepullely.workers.dev \
ORUN_EXEC_ID=gha-12345678-1-34f02d21c9d8 \
  orun status --remote-state --json
```

Or with explicit flags:

```bash
orun status \
  --remote-state \
  --backend-url https://orun-api.rahulvarghesepullely.workers.dev \
  --exec-id gha-12345678-1-34f02d21c9d8 \
  --json
```

### Logs

```bash
ORUN_BACKEND_URL=https://orun-api.rahulvarghesepullely.workers.dev \
ORUN_EXEC_ID=gha-12345678-1-34f02d21c9d8 \
  orun logs --remote-state --job foundation@dev.smoke
```

Note: Remote status and log inspection requires authentication. Outside GitHub Actions, set `ORUN_TOKEN` with a valid bearer token if the backend supports read access via token. Inside GitHub Actions, OIDC is automatic.

## Troubleshooting

### `intent does not declare compositions and no legacy --config-dir fallback`

The intent.yaml is missing `compositions.sources`. Ensure it includes:

```yaml
compositions:
  sources:
    - name: smoke-compositions
      kind: dir
      path: ./compositions
```

And that the `compositions/` directory contains a `stack.yaml` and at least one `compositions/<type>/compositions.yaml`.

### Missing OIDC permission

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

### Missing backend URL

```
--remote-state requires --backend-url or ORUN_BACKEND_URL
```

Set the repository variable `ORUN_BACKEND_URL` or pass `--backend-url` explicitly.

### `ORUN_TOKEN` limitations

`ORUN_TOKEN` is a fallback authentication method for environments outside GitHub Actions. The current live orun-backend (`orun-api.rahulvarghesepullely.workers.dev`) uses OIDC-only authentication for mutable operations. `ORUN_TOKEN` may work for backends that explicitly accept bearer tokens, but it is **not** the production path for the live backend.

### Dependency wait timeout

```
job <id>: dependency wait timeout (30m0s) exceeded
```

A job's upstream dependencies did not complete within 30 minutes. Check:

- Did the upstream job fail? (`orun status --remote-state`)
- Is the upstream runner still running? (check GitHub Actions job)
- Is the backend healthy? (`curl -fsS $ORUN_BACKEND_URL/`)

### Missing logs

`orun logs --remote-state` returns "No logs for this run yet" even though jobs completed.

- Logs are uploaded best-effort during execution. If a runner crashes before uploading, logs may be missing.
- Check that the exec-id matches the run you're inspecting.

### Generated `.orun` cleanup

The `.orun/` directory is created during plan generation and execution. It is gitignored. To clean:

```bash
rm -rf .orun/
```

## File Layout

```
examples/remote-state-matrix/
├── intent.yaml                              # Intent with 3 components × 2 envs
├── compositions/
│   ├── stack.yaml                           # Stack manifest (auto-discovers compositions)
│   ├── terraform/
│   │   └── compositions.yaml                # Smoke steps for foundation component
│   └── helm/
│       └── compositions.yaml                # Smoke steps for api/web components
├── .gitignore                               # Ignores .orun/ output
└── README.md                                # This file
```
