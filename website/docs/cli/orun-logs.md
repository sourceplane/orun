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

## Log storage

Logs are written to `.orun/executions/{exec-id}/logs/{job}/{step}.log` during execution. Each step's raw output is stored separately, making it easy to diff, archive, or forward logs from individual steps.
