---
title: The agent runtime
description: The orun binary is the agent runtime — agent types sealed from agents/*.md, frozen briefs, replayable sessions, a driver seam, and the MCP as the agent's hands — running the same way in a terminal and in an Orunbase sandbox.
---

orun already compiles intent, resolves the catalog, computes what a change
affects, and coordinates runs. The **agent runtime** lets the same binary
delegate work to a coding agent — Claude Code first, any binary behind a
driver seam — without giving the agent any new trust. The agent is a client
of a truth engine that already exists: it receives a frozen, content-addressed
brief, runs behind a driver with the orun MCP as its hands, and produces a
pull request that the [task plane](task-plane.md) judges like a human's.

The runtime is local-first. `orun agent` in a terminal is the whole thing;
Orunbase runs the identical binary in a sandbox it provisions, and supplies
only the box, the credential, and the relay.

```text
  agents/<type>.md ──import──▶ AgentTypeSnapshot ─┐
  base literacy (ships in the binary) ────────────┤
  task contract  (tasks/<KEY>.TaskContract.yaml) ─┼──▶ brief (sealed) ──▶ driver ──▶ session log ──▶ AgentSessionSnapshot
  frozen affected set (catalog affected) ─────────┘                         │                 (sealed, replayable)
                                                                   orun mcp serve
                                                              (the pen · the platform)
```

## Agent types

An **agent type** is one markdown file under `agents/`, git-authored and
reviewed like code. It has two halves:

- **Capability** — YAML frontmatter, a closed schema parsed into a typed
  envelope: `name`, `harness` (a driver id), `model`, optional `runtime`
  tuning, `autonomyDefault`, a deny-by-default `tools` policy
  (`allow` / `ask` / `deny`), a `mayAffect` blast-radius ceiling of component
  globs, optional `secrets.use` reference globs, a mandatory `owner`, and
  `extends`.
- **Character** — the markdown body, the persona, stored verbatim as a blob.
  It carries no policy weight and never restates orun mechanics.

```yaml
---
name: implementer
kind: agent-type
apiVersion: orun.io/v1
harness: claude-code
model: claude-opus-4-8
tools:
  allow: [catalog_search, catalog_get_entity, task_get, pr_open]
  ask:   [task_create]
  deny:  ["*"]
mayAffect: [sourceplane/orun-cloud/billing-*]
owner: sourceplane/team/payments
---
# Implementer
You take one ready task to a merged-quality PR …
```

`orun agent import` seals every file into a content-addressed
**AgentTypeSnapshot** and moves `refs/agents/types/<name>/latest`; an
unchanged file re-seals to the same id, and two files differing only in key
order or whitespace produce the same object. Re-tuning the model or widening
`mayAffect` is a new sealed version, because a different capability is a
different type. `orun agent lint` validates without writing; `orun agent show
<name>[@sha256:…]` reads a sealed type back from content alone.

## Base literacy

Every agent type `extends` a **base literacy**: the versioned document of what
an agent must understand about orun — the object graph, the catalog and
affected engine, the shape of a brief, and the invariants (no status-write
tool, few and attributed writes, stay inside the blast radius, one task one
branch one PR, secrets are references). It ships with the binary and is
pinned into every brief by content hash, so the persona never restates it and
understanding tracks the orun version. `orun agent context` prints it;
`--seal` stores it and pins `refs/agents/literacy/<version>`.

## Briefs and sessions

`orun agent run --type <type> --task <KEY>` assembles a **brief** — base
literacy, the type's persona, the task contract, and the frozen affected
set — seals it, records its content id on the run, and launches the driver.
`--dry-run` seals and prints the brief without launching: the reviewable
"here is exactly what the agent will see". Nothing moves under the agent
mid-run.

A **session** is what a run becomes. The driver's events stream into an
append-only session log; a human's mid-run messages and approval verdicts
land in the same log as attributed events; on terminal state the log seals
as a chained **AgentSessionSnapshot** under `refs/agents/sessions/<id>`.
Session ids are `as_…`. `orun agent replay <id>` re-renders the transcript
from content alone, no live process needed.

A session has one **body** (the runtime process, sole writer of the log) and
any number of **heads**. `orun agent ps` lists live sessions on this machine;
`orun agent attach <id>` replays the history, then follows live. Everything
you type is a steer; `/approve` and `/deny` answer a pending approval,
`/interrupt` stops the turn, `/end` seals the session, `/detach` leaves it
running. Killing a head never kills the session; `orun agent kill` does.
The cockpit's Agents surface is the same head in a TUI.

## Drivers

A **driver** is the executor behind the seam — the same swap discipline as
shell, Docker, and GitHub Actions runners. `orun agent drivers` lists what is
registered; `orun agent doctor` reports whether each can run for real.

| Driver | What it supervises | Steerable |
|---|---|---|
| `claude-code` | The Claude Code harness, headless, bidirectional stream-JSON; permission prompts bridge to the approval loop. The default for `serve`. | Yes |
| `stub` | A deterministic canned transcript for tests and fixtures. The default for `run`. | — |
| `bootstrap` | A **product build** — no model attached. It runs `orun baseline new <id> --local --out <dir> --run-hooks --resume --progress json` from `ORUN_BASELINE_ID`, `ORUN_BASELINE_OUT`, and `ORUN_BASELINE_VALUES`, and narrates the build's progress into the session transcript. | No — a build has no turns; a message typed at it is answered with a line saying so |

The `bootstrap` driver exists so that a platform [baseline](baselines.md)
build gets everything `serve` already provides — heartbeat, token rotation,
the attach plane, sealing, and a terminal state the console can read —
without a second supervisor. The build reports its own event stream; the
driver only reads the progress lines to narrate them.

## `orun agent serve`

`serve` is the in-sandbox entrypoint. It runs the same loop with its attach
plane pointed at a per-session relay, so the console and a remote
`orun agent attach as_…` are interchangeable heads over one stream. Identity
comes from the environment the control plane injects: `ORUN_CLOUD_API`,
`ORUN_ORG_ID`, `ORUN_SESSION_ID`, and `ORUN_SESSION_TOKEN`. `serve` writes
each rotation of the session token to `ORUN_TOKEN_FILE`, so every `orun`
invocation inside the sandbox — and the MCP server it spawns — authenticates
as the session, fresh, for as long as the session lives.

### Grounded sessions

A session is **grounded** when it starts where the work lives. Given
`ORUN_REPO_REMOTE`, `ORUN_REPO_FULL_NAME`, and `ORUN_REPO_REF`, `serve`
clones the linked repository at that ref before the loop starts, creates a
task-keyed branch (`agent/<task>-<type>`), and hands the checkout to the
driver as its working directory. No git credential arrives in the
environment: git authenticates through a credential helper that mints
short-lived, repository-scoped tokens through the session's own credential,
so killing the session severs everything at once. A clone failure fails the
session loudly; a grounded session never degrades to an ungrounded one.
When the agent opens its PR, the pen renames the branch onto the
`orun/<KEY>-<slug>` grammar.

## The MCP as the agent's hands

The runtime writes the driver's MCP config with one server, `orun mcp serve`,
spawned by absolute path. The [orun MCP](../cli/orun-mcp.md) composes two
planes over one connection:

| Plane | Mounts when | Tools |
|---|---|---|
| **The pen** | The server runs inside a repository checkout | `pr_open` — the PR with its lineage written in |
| **The platform** | Cloud auth resolves | 33 tools over the Orunbase API: catalog, runs and logs, audit, events, access, usage, billing, config, secret metadata, webhooks, skills, and the task plane; 24 reads plus 9 policy-gated writes |

The agent type's `tools` policy filters that surface at write time: denied
tools are absent from the config, and the runtime denies them again if a
harness reports one anyway. `ask` tools raise an approval that a head
answers. There is no status-write tool anywhere on the roster, and `secrets_list`
returns metadata only; values are write-only platform-wide. `--read-only`
drops the platform writes without touching the pen, because the pen changes
your checkout and your GitHub — the whole reason it is mounted.

## Skills

A **skill** is a hosted playbook an agent follows: a sealed, content-addressed
revision (`sha256:…` of the canonical body) in a registry where the
Sourceplane defaults are shadowed by anything your workspace publishes under
the same name. Agents read them through `skills_list` and `skill_get` on the
platform plane; `orun agent run` materializes pinned revisions as native
skill files, and the revisions a session ran under are written into the PR
manifest. `orun skills list` and `orun skills pull` are the same surface for
humans and CI — each pulled `SKILL.md` carries its `orun-rev` in frontmatter.

## Related

- [`orun agent`](../cli/orun-agent.md) — run, serve, attach, ps, kill, replay, import, lint, show, context, doctor
- [`orun mcp`](../cli/orun-mcp.md) — the tool surface, plane by plane
- [`orun skills`](../cli/orun-skills.md) — list and pull hosted playbooks
- [`orun tui-next`](../cli/orun-tui-next.md) — the cockpit head for sessions
- [The task plane](task-plane.md) — the contract an agent is briefed on and the verdict its PR earns
- [Baselines](baselines.md) — the build the `bootstrap` driver supervises
- [Workspaces and tenancy](workspaces-and-tenancy.md) — the repository link a grounded session needs
