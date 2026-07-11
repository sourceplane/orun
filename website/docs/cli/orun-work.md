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
| `import` | Map a `specs/` tree to the full hierarchy (Initiatives → Epics → Milestones → Tasks) and apply to the workspace |
| `list` | Render the workspace's tasks with their derived rungs and evidence |

## `orun work import`

Parses a spec tree into a deterministic import plan for the **planning
hierarchy** (orun-work v4):

| In the repo | Becomes |
| --- | --- |
| Epic folder's `README.md` | An **Epic** with a content-addressed doc digest |
| `implementation-plan.md` `## <KEY> — <Title>` headings | **Milestones** on that epic (Goal / Done-when / Deps → the milestone contract) |
| Checklist items under a heading | **Tasks** inside that milestone (one task per milestone materialized where none exist — the v2 mapping, preserved 1:1 under the new level) |
| `roadmap.md` cluster rows | **Initiatives** grouping the epics |

```bash
orun work import specs/ --dry-run          # print the plan, change nothing
orun work import specs/ --workspace my-org # apply (idempotent re-runs)
orun work import specs/ --prefix PAY       # task-key prefix (default WRK)
```

Dry-run prints the plan's shape (`initiatives: / specs: / milestones: /
tasks:`); apply reports created / skipped counts per level plus how many
pre-existing tasks were **migrated into milestones**.

- **Import writes intent, never decisions.** No lifecycle, no reviews, no
  approvals are imported — tasks surface wherever the logs say they are
  (usually Draft/Ready), and a fixture asserts no `approved` or
  `design_adopted` event is ever emitted `via: import`.
- Apply is **idempotent**: every created entity is labeled with its import
  provenance, so re-importing the same tree is a no-op.
- **Key-preserving migration.** A workspace imported under v2 (one task per
  milestone, no milestone level) upgrades in place: existing tasks keep
  their keys and history and are attached to the newly minted milestones —
  nothing is recreated.
- Milestone dependency tokens rewrite to the allocated keys.
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
- [`orun epic`](./orun-epic.md) — the approval-sealed epic brief (v4)
- [`orun mcp`](./orun-mcp.md) — the agent tool surface over the same fold
