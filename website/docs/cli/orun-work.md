---
title: orun work
---

`orun work` is the CLI face of the **work lens** — orun's delivery-derived
work tracker (specs/orun-work v2). Its central invariant: **lifecycle is a
derived query, not a stored status**. A task's rung (Draft → Ready →
In Progress → In Review → Done → Released) is computed by folding two
append-only logs — the coordination log (human/agent events) and the
observation log (facts the platform observed: branches, PRs, merges, gate
verdicts, live revisions) — so nobody, human or agent, can *set* a status
anywhere. The CLI has no `orun work set-status`, deliberately and permanently.

```bash
orun work <subcommand> [flags]
```

Requires a linked workspace (see [`orun cloud`](./orun-cloud.md)) or explicit
`--workspace` / `--backend-url` flags.

## Subcommands

| Subcommand | Purpose |
| --- | --- |
| `import` | Map a `specs/` tree to Specs + Tasks and apply to the workspace |
| `list` | Render the workspace's tasks with their derived rungs and evidence |

## `orun work import`

Parses a spec tree (epic `README.md`s → Specs with content-addressed doc
digests; `implementation-plan.md` milestones → Tasks with Goal / Deps /
Done-when contracts) into a deterministic import plan.

```bash
orun work import specs/ --dry-run          # print the plan, change nothing
orun work import specs/ --workspace my-org # apply (idempotent re-runs)
orun work import specs/ --prefix PAY       # task-key prefix (default WRK)
```

- **No lifecycle is imported.** Imported tasks surface wherever the logs say
  they are — usually Draft/Ready. A migration cannot smuggle in a status.
- Apply is **idempotent**: every created entity is labeled with its import
  provenance, so re-importing the same tree is a no-op.
- Milestone dependency tokens rewrite to the allocated task keys.
- `--json` emits the plan/result as JSON for scripting.

## `orun work list`

```bash
orun work list --workspace my-org
```

Prints each task's key, title, derived rung **with the evidence that put it
there** (e.g. `in_review  PR #123 open @ abc1234`), pins rendered beside
observed truth, and blocked flags. What you see is the fold's output — the
same fold the console and the MCP serve.

## Related

- [`orun spec`](./orun-spec.md) — frozen, content-addressed spec briefs
- [`orun mcp`](./orun-mcp.md) — the agent tool surface over the same fold
