---
title: orun logs
---

`orun logs` streams raw step output from an execution record.

## Usage

```bash
orun logs
```

With no arguments, `logs` reads from the latest execution.

## Common examples

Show all logs for the latest execution:

```bash
orun logs
```

Show logs for a specific execution:

```bash
orun logs run/my-plan-20240601-a1b2c3
```

Filter to one job:

```bash
orun logs job/api-edge-worker@production.deploy
```

Combine exec and job filters:

```bash
orun logs --exec-id my-plan-20240601-a1b2c3 --job api-edge-worker@production.deploy
```

Filter to a specific step:

```bash
orun logs --job api-edge-worker@production.deploy --step deploy
```

## Slash notation

`logs` supports resource slash notation as positional arguments:

```bash
orun logs run/<exec-id>
orun logs job/<job-id>
```

## Flags

| Flag | Meaning |
| --- | --- |
| `--exec-id` | Target a specific execution (defaults to `latest`) |
| `--job` | Filter output to a specific job ID |
| `--step` | Filter output to a specific step ID within the selected job |
| `--failed` | Show only failed jobs or steps |
| `--raw` | Show full raw logs instead of compact 8-line excerpts |
| `--remote-state` | Fetch logs from orun-backend instead of local state |
| `--backend-url` | orun-backend URL for remote state (or set `ORUN_BACKEND_URL`) |

## Remote logs

When `--remote-state` is set, `orun logs` fetches job logs from the backend:

```bash
orun logs \
  --remote-state \
  --backend-url https://orun-backend.example.com \
  --exec-id gh-12345678-1-a1b2c3 \
  --job api@dev.deploy
```

Omit `--job` to fetch logs for all jobs in the run.

Outside GitHub Actions, remote logs use the local Orun CLI session from `orun auth login` and the backend URL from `--backend-url`, `ORUN_BACKEND_URL`, `intent.yaml`, or `~/.orun/config.yaml`.

## Log storage

Logs are written to `.orun/executions/{exec-id}/logs/{job}/{step}.log` during local execution. Each step's raw output is stored separately, making it easy to diff, archive, or forward logs from individual steps.

When running with `--remote-state`, logs are streamed to the backend during execution and retrieved via the backend API.
