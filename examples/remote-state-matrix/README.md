# Remote State Matrix Example

This example demonstrates `orun` execution across multiple components and environments using a shared execution ID. It exercises both local filesystem state and (optionally) the remote state backend.

## Prerequisites

```bash
go build -o orun ./cmd/orun
```

## Local State Lock Smoke

The local state backend uses advisory file locks (flock) scoped to each execution ID. This prevents two processes sharing the same `--exec-id` from claiming and executing the same job concurrently.

### Compile a plan

```bash
orun plan --intent examples/remote-state-matrix/intent.yaml
```

This writes the compiled plan to `.orun/plans/latest.json`.

### Run two local jobs with the same ORUN_EXEC_ID

Open two terminals:

```bash
# Terminal 1
export ORUN_EXEC_ID=smoke-lock-test
orun run --job foundation-dev --exec-id $ORUN_EXEC_ID

# Terminal 2 (start immediately after terminal 1)
export ORUN_EXEC_ID=smoke-lock-test
orun run --job foundation-dev --exec-id $ORUN_EXEC_ID
```

### What success looks like

- **Terminal 1**: Claims `foundation-dev` and executes its steps.
- **Terminal 2**: Sees `foundation-dev` as `running` (or `completed`) and exits cleanly without re-executing.

Only one process performs the actual job execution. The other recognises the job is already claimed and skips it.

### Inspect state

```bash
cat .orun/executions/smoke-lock-test/state.json | jq .
```

You should see `foundation-dev` with status `"completed"` (or `"running"` if checked mid-execution). The lock file lives at:

```
.orun/executions/smoke-lock-test/.lock
```

This is an advisory lock file used by `flock(2)`. It is safe to delete after all processes exit.

### Clean generated output

```bash
rm -rf .orun/
```

Or to clean only a specific execution:

```bash
rm -rf .orun/executions/smoke-lock-test/
```

## How the lock works

1. Before executing a job, the process acquires an exclusive `flock` on `.orun/executions/<exec-id>/.lock`.
2. Under the lock, it reads `state.json` and checks the job's current status.
3. If the job is `pending`, it marks it `running` and writes state atomically (write to `.tmp`, then rename).
4. The lock is released. The process proceeds to execute the job.
5. On completion, the lock is re-acquired to write the terminal status (`completed`/`failed`).

If a process crashes, the kernel automatically releases the advisory lock. The job will remain in `running` state until manually reset or re-run with `--retry`.

## Dependency semantics

When a job has `dependsOn`, the claim check also verifies dependency status:

- All dependencies `completed` → claim proceeds
- Any dependency `pending` or `running` → claim returns `DepsWaiting` (process should wait or exit)
- Any dependency `failed` → claim returns `DepsBlocked` (downstream cannot proceed)
