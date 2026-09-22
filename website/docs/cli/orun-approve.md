---
title: orun approve
description: Resolve a workflow step paused on an approval gate — list what is pending, approve it, or reject it — and seal the verdict into the run record.
---

`orun approve` is the human half of a workflow **approval gate**. A step
that declares an `approval:` block pauses when it is reached, writes a
pending request under the workspace's `.orun/approvals/` tree, and waits for
a decision until its declared `timeout` expires. This command lists those
requests and records the decision. The pause and the verdict are **run
facts** — files sealed into the run record, never plan content — so a plan
is byte-identical whether it was later approved or rejected.

```bash
orun approve                              # list pending approvals
orun approve <jobID> <stepID>             # approve
orun approve <jobID> <stepID> --reject    # reject
```

Run it from the workspace root: the approvals tree is resolved against the
current directory.

## Listing

With no arguments, prints every request that has no decision yet, newest
first: the time it was requested, the job, the step, and the prompt the
workflow declared.

```text
$ orun approve
14:02:51  deploy-api  promote — Promote to production?
```

When nothing is waiting it prints `no pending approvals`.

## Deciding

With a job id and a step id, resolves the **newest** pending request that
matches, across executions, and writes the decision beside it. The waiting
step picks it up on its next poll and continues (approved) or fails
(rejected).

| Flag | Meaning |
|---|---|
| `--reject` | Reject instead of approving. |
| `--by <who>` | Who is deciding. Defaults to `$USER`. Recorded in the sealed decision. |

```bash
orun approve deploy-api promote
orun approve deploy-api promote --reject --by "rahul (change freeze)"
```

The command confirms with `approved deploy-api / promote` (or `rejected …`).
A job/step pair with nothing pending is an error, not a silent no-op.

## What is recorded

Each gate is a directory `.orun/approvals/<execId>/<jobID>/<stepID>/`. The
runner writes `pending.json` (prompt, execution, job, step, requested-at);
`orun approve` writes `decision.json` (`approved`, `by`, `decidedAt`). When
no decision arrives inside the step's `timeout`, the declared `onTimeout`
policy decides instead and is sealed the same way, marked `onTimeout: true`,
so a run always shows who or what resolved the gate.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Listed, approved, or rejected. |
| `1` | No pending approval for that job and step, or the approvals tree could not be read. |

## Related

- [Workflow actions](../concepts/workflow-actions.md) — declaring `approval:` on a step
- [`orun workflow`](./orun-workflow.md)
- [`orun run`](./orun-run.md), [`orun status`](./orun-status.md)
