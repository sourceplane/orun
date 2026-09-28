---
title: orun skills
description: The hosted skill registry from the command line — list every playbook's latest revision, pull them as native skill files (with their bundled references and templates) into the directory your coding-agent client discovers, and check the local copies against the registry.
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
orun skills pull [name] [--rev sha256:<hex>] [--dir <dir> | --client <client> [--project]]
orun skills status [--dir <dir> | --client <client> [--project]]
```

All three take the shared cloud flags: `--workspace <org id|slug>` (defaults
to the linked repository's workspace), `--backend-url <url>`, and `--json`.
You need a login (see [`orun auth`](./orun-auth.md)).

## `list`

Prints every skill's latest revision: name, the short revision, the source
(`default` for a Sourceplane default, `org` for one your workspace published), and who published it.

```text
$ orun skills list
NAME               REV       SOURCE       PUBLISHED BY
orunbase           18f73d0c  default      -
software-factory   2bbb7cde  default      -
review-pr          0a9b8c7d  org          usr_01J8…
```

`--json` emits the registry response as-is.

## `pull`

Writes each revision as a **native skill bundle** — `<dir>/<name>/SKILL.md`,
the project-skill layout coding-agent clients discover on their own, with a
frontmatter block over the canonical body — plus the files the skill
carries: `references/*` (loaded on demand) and `templates/*` (copied). The
frontmatter carries `name`, `description` when the skill has one, the pinned
`orun-rev`, and `orun-source`, so a skill on disk always names the revision
it is.

A pull is a sync of the registry's copy, not a merge: a bundle file an
earlier pull wrote that the new revision no longer carries is removed
(`.orun-files` beside `SKILL.md` records what was written), and anything
else in the directory — a skill you wrote yourself, a note beside one — is
left alone.

| Flag | Meaning |
|---|---|
| `[name]` | Pull one skill. With no name, pull the whole registry. |
| `--rev sha256:<hex>` | An exact revision. Only valid with a name. |
| `--dir <dir>` | Target directory (default `.claude/skills`). |
| `--client <client>` | Resolve the directory from the client instead: `claude-code`, `codex`, `cursor` or `vscode`. Mutually exclusive with `--dir`. |
| `--project` | With `--client`: the repository's skills directory rather than the user's. |

| `--client` | user-level | `--project` |
|---|---|---|
| `claude-code` | `~/.claude/skills` | `.claude/skills` |
| `codex` | `~/.codex/skills` | `.codex/skills` |
| `cursor` | `~/.cursor/skills` | `.cursor/skills` |
| `vscode` | `~/.copilot/skills` | `.github/skills` |

```bash
orun skills pull --client claude-code                # everything, into ~/.claude/skills
orun skills pull --client vscode --project           # into .github/skills of this repository
orun skills pull software-factory --rev sha256:2bbb… # one skill, pinned
orun skills pull --dir ./agent-skills
```

```text
$ orun skills pull --client claude-code
wrote /home/dana/.claude/skills/orunbase/SKILL.md (18f73d0c)
wrote /home/dana/.claude/skills/software-factory/SKILL.md (2bbb7cde) + 7 file(s)
```

`--json` returns `{"dir": …, "skills": [{"name", "rev"}, …]}` — the pins.

## `status`

Compares the skills on disk — the `orun-rev` each `SKILL.md` carries — with
the registry's latest revisions, and exits 1 when anything is not current,
so a session-start hook can run it and pull only when it says to.

| Status | Meaning |
|---|---|
| `current` | The local copy is the registry's latest. |
| `stale` | A real revision, since superseded — pull again. |
| `unknown` | A revision the registry never published: the file was edited, or came from elsewhere. |
| `missing` | A registry skill with no local copy. |

```text
$ orun skills status --client claude-code
NAME              LOCAL     LATEST    STATUS
mcp-tools         9983f000  9983f000  current
orunbase          18f73d0c  4c1e9a02  stale
task-branch-pr    -         3c53498c  missing
✕ orun skills status: 2 skill(s) not current in /home/dana/.claude/skills — `orun skills pull --dir /home/dana/.claude/skills` refreshes them
```

`--json` returns `{"dir": …, "skills": [{"name", "local", "latest", "status"}, …], "current": bool}`.

## How sessions use the registry

A `claude-code` session pulls the whole registry into the harness working
directory before launch and records the pins it ran under in
`.orun/agent-mcp/skills.json`. [`orun pr open`](./orun-pr.md) reads that
file and names the revisions in the PR's manifest block, so a review can
always re-read the playbook the agent followed. The materialization is
best-effort: a workspace that is not linked, not logged in, or offline
warns and the session continues without skills.

The `orunbase` skill — the registry's entry point — starts every session by
comparing its own `orun-rev` with `skill_get orunbase` and following the
live body when they differ, so a stale copy is never the one that decides.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Listed, pulled, or every local copy is current. |
| `1` | No workspace or login could be resolved, the registry request failed, `--rev` without a name, `--dir` together with `--client`, a file could not be written, or (`status`) a skill is stale, unknown or missing. |
