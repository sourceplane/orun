---
title: orun mcp
---

`orun mcp serve` runs orun's **Model Context Protocol server**: a minimal,
dependency-free JSON-RPC 2.0 server over stdio that gives coding agents hands
on the work lens — and deliberately **not** a pen for status.

```bash
orun mcp serve --workspace my-org
```

Wire it into an MCP-capable agent host (Claude Code, etc.) as a stdio server.
Authentication and workspace routing reuse the standard CLI session
([`orun auth`](./orun-auth.md) / [`orun cloud link`](./orun-cloud.md)).

## The tool surface (closed at 7)

| Tool | Kind | Purpose |
| --- | --- | --- |
| `work_query` | read | The fold summary — every task's derived rung WITH its evidence |
| `work_get` | read | One task in full (contract, lifecycle, evidence, pins) |
| `spec_get` | read | A sealed, content-addressed SpecSnapshot (intent only — fold output is asserted absent from the sealed bytes) |
| `task_create` | write | Create a task through the cloud mutator |
| `task_comment` | write | Append a coordination comment |
| `task_assign` | write | Assign/unassign through the mutator |
| `contract_propose` | write | Edit a task contract — applied AND flagged with a review comment |

Three properties are structural, not policy:

- **No `task_update_status` exists.** Lifecycle derives from delivery facts;
  an agent moves a task to In Review by *opening a PR*, not by calling a tool.
  Asserted by test — adding such a tool fails the suite.
- **No pin tool exists.** Pins are public, attributed human overrides; the
  cloud mutator additionally rejects agent pins server-side (defense in
  depth, not client-side trust).
- **`contract_propose` cannot be quiet.** The edit is applied through the
  normal mutator *and* a "human review requested" comment is written in the
  same call — an agent cannot silently redefine its own definition of done.

Tool failures return MCP `isError` results (structured verdicts the agent can
reason about), never protocol faults.

## Related

- [`orun work`](./orun-work.md) — the same fold in the terminal
- [`orun spec`](./orun-spec.md) — sealed briefs (`spec_get`'s CLI twin)
