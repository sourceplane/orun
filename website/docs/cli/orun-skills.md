---
title: orun skills
description: The hosted skill registry from the command line — list every playbook's latest revision and pull them as native skill files a coding-agent harness discovers on its own.
---

`orun skills` is the human and CI face of the **skill registry**: hosted,
content-addressed agent playbooks — the Sourceplane defaults, shadowed by
anything your workspace publishes. Agents consume the same registry through
`skills_list` and `skill_get` on the platform MCP, and
[`orun agent run`](./orun-agent.md) and `serve` materialize it automatically
before a session starts. This group reads the registry; publishing stays with
the console, and nothing here writes to it.

```bash
orun skills list
orun skills pull [name] [--rev sha256:<hex>] [--dir <dir>]
```

Both subcommands take the shared cloud flags: `--workspace <org id|slug>`
(defaults to the linked repository's workspace), `--backend-url <url>`, and
`--json`. You need a login (see [`orun auth`](./orun-auth.md)).

## `list`

Prints every skill's latest revision: name, the short revision, the source
(`default` for a Sourceplane default, `org` for one your workspace published), and who published it.

```text
$ orun skills list
NAME               REV       SOURCE       PUBLISHED BY
implement-task     8c1d2e3f  default      -
review-pr          0a9b8c7d  org          usr_01J8…
```

`--json` emits the registry response as-is.

## `pull`

Writes each revision as a **native skill file** — `<dir>/<name>/SKILL.md`,
the project-skill layout Claude Code discovers on its own — with a
frontmatter block over the canonical body. The frontmatter carries `name`,
`description` when the skill has one, the pinned `orun-rev`, and
`orun-source`, so a skill on disk always names the revision it is.

| Flag | Meaning |
|---|---|
| `[name]` | Pull one skill. With no name, pull the whole registry. |
| `--rev sha256:<hex>` | An exact revision. Only valid with a name. |
| `--dir <dir>` | Target directory (default `.claude/skills`). |

```bash
orun skills pull                                  # everything, into .claude/skills
orun skills pull review-pr --rev sha256:0a9b…     # one skill, pinned
orun skills pull --dir ./agent-skills
```

```text
$ orun skills pull review-pr
wrote .claude/skills/review-pr/SKILL.md (0a9b8c7d)
```

`--json` returns `{"dir": …, "skills": [{"name", "rev"}, …]}` — the pins.

## How sessions use the registry

A `claude-code` session pulls the whole registry into the harness working
directory before launch and records the pins it ran under in
`.orun/agent-mcp/skills.json`. [`orun pr open`](./orun-pr.md) reads that
file and names the revisions in the PR's manifest block, so a review can
always re-read the playbook the agent followed. The materialization is
best-effort: a workspace that is not linked, not logged in, or offline
warns and the session continues without skills.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Listed or pulled. |
| `1` | No workspace or login could be resolved, the registry request failed, `--rev` without a name, or a file could not be written. |

## Related

- [`orun agent`](./orun-agent.md) — where skills are materialized automatically
- [`orun mcp`](./orun-mcp.md) — `skills_list` and `skill_get` for agents
- [`orun pr`](./orun-pr.md) — the manifest that names the pins
- [`orun workspace`](./orun-workspace.md), [`orun auth`](./orun-auth.md)
