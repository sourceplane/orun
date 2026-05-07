---
title: orun status
---

`orun status` shows the execution status of the latest run or a specific execution.

## Usage

```bash
orun status
```

## Common examples

Show the latest execution:

```bash
orun status
```

Show all executions:

```bash
orun status --all
```

Show step-level detail for the latest execution:

```bash
orun status --detailed
```

Show a specific execution:

```bash
orun status --exec-id my-plan-20240601-a1b2c3
```

## Output

The default view shows a compact execution header followed by a job list sorted by priority (running first, then failed, then completed, then pending):

```
EXECUTION my-plan-20240601-a1b2c3  ● running  4/38 jobs  2m
Plan: my-plan

  ● api-edge-worker@production.deploy           12s
  ✓ platform-shared@production.build            8s
  ○ web-console@staging.deploy
```

The `--all` view lists all executions with running ones sorted first:

```
EXECUTION                              STATUS        PLAN                 JOBS      DURATION      AGE
● my-plan-20240601-a1b2c3             running       my-plan              4/38      2m            now
✓ my-plan-20240531-d4e5f6             completed     my-plan              38/38     4m12s         1d
```

## Status icons

| Icon | Meaning |
| --- | --- |
| `●` | Running |
| `✓` | Completed |
| `✗` | Failed |
| `○` | Pending |

## Flags

| Flag | Meaning |
| --- | --- |
| `--exec-id` | Show a specific execution by ID |
| `--all` | List all stored executions |
| `--detailed` | Include step-level status in the output |
| `--json` | Output in JSON format |
| `--watch`, `-w` | Continuously refresh until the run reaches a terminal state |
| `--interval` | Refresh interval for `--watch` (default `1s`) |
| `--remote-state` | Fetch status from orun-backend instead of local state |
| `--backend-url` | orun-backend URL for remote state (or set `ORUN_BACKEND_URL`) |

## Remote status

When `--remote-state` is set, `orun status` fetches run and job state from the backend rather than the local `.orun/` store.

```bash
orun status \
  --remote-state \
  --backend-url https://orun-backend.example.com \
  --exec-id gh-12345678-1-a1b2c3
```

The same rendering is used for local and remote state.  `--watch` polls the backend until the run reaches a terminal state (completed or failed).  `--json` returns machine-readable output.

Outside GitHub Actions, remote status uses the local Orun CLI session from `orun auth login` and the backend URL from `--backend-url`, `ORUN_BACKEND_URL`, `intent.yaml`, or `~/.orun/config.yaml`.

Use `orun describe run <id>` for a fuller breakdown including metadata, timing, and job-level errors.
