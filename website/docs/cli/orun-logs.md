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

## Output

`orun logs` renders through the shared cockpit layer — same brand
wedge, status legend, and component tree as `orun status`, with log
content grouped per job/step:

```
▲ orun my-plan
  Run: my-plan-20240601-a1b2c3 · State: completed · Duration: 4m12s
  Scope: 7 components · 38 jobs
  Status:   ✓ 38 succeeded · ◐ 0 running · ○ 0 queued

  ● api-edge-worker
  │  └─ ✓ deploy           19.0s
  │     │  Uploading bundle to edge…
  │     │  Bundle hash: sha256-3f4f0bf
  │     │  Deploy id: dep_01HVZ…
  │     │  … 4 more lines
```

Default truncation is 8 lines per step (configurable via `LogsOptions`
on the renderer); pass `--raw` to print every line.

## Flags

| Flag | Meaning |
| --- | --- |
| `--exec-id` | Target a specific execution (defaults to `latest`) |
| `--revision` | Pin resolution to a specific `PlanRevision` key (combine with `--exec-id` for an exact lookup) |
| `--job` | Filter output to a specific job ID |
| `--step` | Filter output to a specific step ID within the selected job |
| `--failed` | Show only failed jobs or steps |
| `--raw` | Show full raw logs instead of compact 8-line excerpts |
| `--remote-state` | Fetch logs from orun-backend instead of local state |
| `--backend-url` | orun-backend URL for remote state (or set `ORUN_BACKEND_URL`) |
| `--follow` | Live-tail a job's log (requires `--remote-state` and `--job`); polls until the job completes |

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

Add `--follow` to live-tail a single job's log as it runs. The CLI polls the
backend from a sequence cursor and streams new lines until the job completes:

```bash
orun logs \
  --remote-state \
  --job api@dev.deploy \
  --follow
```

`--follow` requires both `--remote-state` and `--job` (the live tail reads one
job's stream from the backend).

Outside GitHub Actions, remote logs use the local Orun CLI session from `orun auth login` and the backend URL from `--backend-url`, `ORUN_BACKEND_URL`, `intent.yaml`, or `~/.orun/config.yaml`.

## Log storage

During local execution each step's raw output is stored as its own content-addressed log blob in the object model under `.orun/objectmodel/`, attached to that step's node. Per-step storage makes it easy to diff, archive, or forward logs from individual steps; `orun logs` reads them back through the object reader.

When running with `--remote-state`, logs are streamed to the backend during execution and retrieved via the backend API.
