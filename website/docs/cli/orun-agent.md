---
title: orun agent
description: The agent runtime — seal agent types from agents/*.md, delegate a task to a coding agent behind a driver, attach to and steer a live session, replay a sealed one, and serve a session inside a platform sandbox.
---

`orun agent` is the command-line face of the agent runtime. An **agent type**
is a markdown file under `agents/` — YAML capability frontmatter over a
persona body — sealed into the content-addressed object store as an
`AgentTypeSnapshot`. A **session** is one run of one type: a frozen brief
(the base literacy this binary ships, the type's persona, the task contract,
the affected set) handed to a **driver**, whose events stream into an
append-only session log that is sealed and replayable. The runtime never
hands a driver a raw credential; it hands over the brief and an MCP config
filtered through the type's tool policy.

Run bare, `orun agent` opens the cockpit on its Agents surface; `--next`
opens the [cockpit v2](./orun-tui-next.md) Agents surface instead.

```bash
orun agent                       # the interactive Agents surface (--next for cockpit v2)
orun agent lint    [dir]
orun agent import  [dir] [--id-only]
orun agent show    <name>[@sha256:…]
orun agent context [--seal]      # orun agent context id
orun agent run     --type <type> [--task KEY --spec <slug>] [--driver <id>] [--dry-run] [--detach] [--json]
orun agent ps
orun agent attach  <sessionId>
orun agent kill    <sessionId> [--force]
orun agent replay  <sessionId>[@sha256:…]
orun agent drivers
orun agent doctor
orun agent serve   [--driver <id>] [--session <id>] [--type <type>] [--task KEY]
```

Every subcommand that reads or writes the object store works against the
workspace's `.orun/` directory, so run it from the repository root (or a
subdirectory of it).

## Agent types

An agent type file has two halves in one document. The **frontmatter** is the
policy contract: `name`, `kind: agent-type`, `apiVersion: orun.io/v1`,
`harness` (a driver id), `model`, optional `runtime` tuning
(`effort`, `temperature`, `maxTokens`, `contextBudget`), `autonomyDefault`,
`tools` (`allow`, `ask`, `deny` — deny-by-default), `mayAffect` component
globs, optional `secrets.use`, a mandatory `owner`, and an optional `extends`.
The schema is closed: an unknown key fails the lint. The **body** is the
persona, stored verbatim as a content-addressed blob. Everything in the file
is identity — retuning the model or widening `mayAffect` seals a new version.

The binary ships three types — `bootstrapper`, `implementer`, and
`orchestrator` — as embedded fallbacks. A file at `agents/<name>.md` in the
workspace always wins over the shipped copy of the same name.

### `lint`

Validates every `*.md` under `dir` (default `agents`) without writing. A file
with no frontmatter, or whose `kind` is not `agent-type`, is skipped with a
notice, never an error. Errors (missing `owner`, empty persona, an unknown
frontmatter key, a bad `apiVersion`) are printed one per line and the command
exits non-zero.

```text
$ orun agent lint
agents/implementer.md: ok: agent-type "implementer" (harness claude-code, model claude-opus-4-8, owner sourceplane/team/platform)
agents/notes.md: notice: no frontmatter — not an agent type, skipped
```

### `import`

Seals every valid type under `dir` (default `agents`) into the object store
and moves `refs/agents/types/<name>/latest` to the new snapshot. The persona
becomes a body blob; the binary's base literacy is pinned through `extends`
unless the file pins a custom literacy explicitly (`name@id`). Import is
idempotent: an unchanged file re-seals to the same id. Lint errors block the
whole import.

| Flag | Meaning |
|---|---|
| `--id-only` | Print only `<id> <name>` lines, for scripting. |

```text
$ orun agent import
sealed  sha256:3f9c…  implementer (refs/agents/types/implementer/latest)
sealed  sha256:a01e…  orchestrator (refs/agents/types/orchestrator/latest)
```

### `show`

Resolves a type by name (through `refs/agents/types/<name>/latest`) or by an
explicit `@sha256:…` pin and prints the sealed envelope and persona from
content alone — the offline read-back of `import`.

```text
$ orun agent show implementer
agent-type implementer @ sha256:3f9c…
harness    claude-code · model claude-opus-4-8
owner      sourceplane/team/platform
autonomy   assist
extends    sha256:…
tools      allow=[…] ask=[…] deny=[*]

# Implementer
…
```

### `context`

Prints the versioned **base literacy** every agent type extends instead of
restating — the layer of orun understanding that ships with this binary
(`base-orun-literacy@v2`) and is pinned into every brief by content hash.

| Flag | Meaning |
|---|---|
| `--seal` | Also store the literacy as a content-addressed blob and pin `refs/agents/literacy/v2`. Needs an object store (`orun plan` first). |

`orun agent context id` prints the literacy's content id without writing
anything, as `<id> base-orun-literacy@v2`.

## Drivers

A driver is the seam between the runtime and a coding agent: it is launched
with a brief and an MCP config path, and emits a normalized event stream
(message, tool call, tool result, approval requested, artifact, cost, error,
done). Three drivers are registered:

| Driver | What it runs |
|---|---|
| `stub` | An in-process, deterministic script — no binary, no model, no network. The default for `run`; an interactive stub run (no `--task`) serves steers, approvals, and interrupts until a `/done` steer. |
| `claude-code` | The Claude Code CLI in headless mode. The runtime writes the orun MCP config under `.orun/agent-mcp/`, filtered through the type's tool policy, pins the type's `model`, and materializes hosted skills (see [`orun skills`](./orun-skills.md)). The default for `serve`. |
| `bootstrap` | A **product build with no model attached**. `serve` still supervises — heartbeat, relay, sealing — but the body is `orun baseline new <id> --local --out <dir> --run-hooks --resume --progress json [--values <file>]`, with `<id>`, `<dir>`, and the values file read from `ORUN_BASELINE_ID`, `ORUN_BASELINE_OUT`, and `ORUN_BASELINE_VALUES`. The driver narrates the build's `--progress json` stream into the transcript; the build reports its own stream to the platform. It is not steerable: a steer or interrupt is answered with a line saying so, and the child's exit status alone decides the outcome. |

`orun agent drivers` prints the registered ids, one per line. `orun agent
doctor` reports what the live plane needs: the drivers, the MCP tool count
`orun mcp serve` exposes, and whether a `claude` binary is on `PATH` (with
its version — the driver's wire protocol is pinned by fixtures, so version
drift is worth knowing before a session hits it).

```text
$ orun agent doctor
drivers    [bootstrap claude-code stub]
mcp tools  … (orun mcp serve)
claude     /opt/homebrew/bin/claude (… (Claude Code))
```

## `run`

Assembles a frozen, content-addressed brief and runs it through the chosen
driver, hosting the session locally so heads can attach.

| Flag | Meaning |
|---|---|
| `--type <type>` | Agent type: `agents/<type>.md`, or the shipped copy of that name. Supplies the persona, the tool policy, and the model. |
| `--task <KEY>` | Task key to implement (for example `ORN-142`). Without it the run is *interactive* rather than an implementation run. |
| `--spec <slug>` | A sealed brief under `.orun/specs/<slug>/snapshot.json` supplying the task's contract; the brief's content id is computed over the bytes on disk and recorded on the run. With `--task`, the task must carry a contract in that brief. |
| `--driver <id>` | Driver id (default `stub`). |
| `--dry-run` | Seal and print the brief — id, run kind, task, and the full instructions — without launching. |
| `--detach` | Fork the session body into its own process group and return; attach later with `attach`. Closing the terminal never kills a detached run. |
| `--json` | JSON output: `{briefId, runKind, task}` with `--dry-run`; the run result otherwise. |

The command needs an object store (`orun plan` first). A task run's branch
is `agent/<KEY>-<type>`. Sealing the session as a discoverable,
replayable record needs the type to have been sealed — run `orun agent import`
first; without it the run still executes and its segments are stored, but it
is not indexed under `refs/agents/sessions/<id>`.

```bash
# See exactly what the agent will be briefed with
orun agent run --type implementer --task ORN-142 --spec orn-142 --dry-run

# Smoke the whole loop with no model
orun agent run --type implementer --task ORN-142 --spec orn-142

# The real thing, left running in the background
orun agent run --type implementer --task ORN-142 --spec orn-142 --driver claude-code --detach
```

While it runs, the body prints `session as_… hosting — attach from another
terminal: orun agent attach as_…`, then renders each event as a line. At the
end:

```text
session  as_8f3c2d1e9b7a4c6d (driver claude-code)
brief    sha256:…
outcome  completed  pr=https://github.com/acme/api/pull/412
segments 3 sealed
session  sealed sha256:… (refs/agents/sessions/as_8f3c2d1e9b7a4c6d)
replay   orun agent replay as_8f3c2d1e9b7a4c6d
```

## Sessions: `ps`, `attach`, `kill`

A running session is a **body**; every terminal or console looking at it is a
**head**. The body hosts a socket under `.orun/agents/live/`, and the live
registry beside it is what `ps` reads.

### `ps`

Lists live sessions **on this machine only** — the local registry, with dead
bodies swept. Cloud sessions do not appear here; watch those in the console
or attach to them by id.

```text
$ orun agent ps
SESSION                  TYPE           TASK       STATE      AGE      PID
as_8f3c2d1e9b7a4c6d      implementer    ORN-142    running    4m12s    48213

attach: orun agent attach <session>   end: orun agent kill <session>
```

### `attach`

Attaches this terminal as a head: replays the session's event history, then
follows live. Everything you type is a **steer** — a user turn, attributed and
sealed into the session log. Lines starting with `/` are head commands:

| Command | Effect |
|---|---|
| `/approve <requestId> [reason]` | Answer a pending approval. |
| `/deny <requestId> [reason]` | Deny it. |
| `/interrupt` | Stop the current turn, not the session. |
| `/end` | Graceful terminal: the body seals and exits. |
| `/detach` (or `Ctrl+D`) | Leave; the session keeps running. |

Any other slash-word is passed through as conversation, since a driver may
have its own.

`attach` resolves the id locally first. When no local body has that id, it
attaches over the **cloud relay** instead, and for that it needs:

| Variable | Meaning |
|---|---|
| `ORUN_CLOUD_API` | The api-edge base URL. Required. |
| `ORUN_ORG_ID` | The workspace (`org_…`) id. Required. |
| `ORUN_SESSION_TOKEN` | The bearer presented to the relay. |

The relay endpoint is
`$ORUN_CLOUD_API/v1/organizations/$ORUN_ORG_ID/agents/sessions/<sessionId>`.
Without `ORUN_CLOUD_API` and `ORUN_ORG_ID` the command fails with
`no live local session "…", and no cloud attach config`. A local and a
remote session render identically.

### `kill`

Ends a session gracefully by dialing its socket and sending the terminal
command, so the body seals and exits. If the body is dead or unreachable:

| Flag | Meaning |
|---|---|
| `--force` | Kill the body process and sweep its registry entry. Sealed segments up to the last seal survive. |

## `replay`

Resolves a session by id (`refs/agents/sessions/<id>`) or by an explicit
`@sha256:…` pin and re-renders it from the sealed segment chain alone —
deterministic, from the object graph, with no live process.

```text
$ orun agent replay as_8f3c2d1e9b7a4c6d
session   as_8f3c2d1e9b7a4c6d @ sha256:…
runKind   implementation
agentType sha256:…
brief     sha256:…
outcome   completed  pr=https://github.com/acme/api/pull/412

transcript (17 events):
    1  message_agent        {"text":"reading brief …"}
    2  tool_call            {"tool":"catalog_affected","decision":"allow"}
  …
```

## `serve`

The **in-sandbox entrypoint**: the same delegation loop as `run`, with its
attach plane pointed at the per-session cloud relay. Event batches dial out
to the relay; steers, verdicts, and interrupts dial back. The console and a
remote `orun agent attach as_…` are interchangeable heads over that stream.
You do not normally run it by hand — the platform execs it in the sandbox it
provisions for a session or a platform baseline build.

| Flag | Meaning |
|---|---|
| `--driver <id>` | Driver id (default `claude-code`). `bootstrap` for a platform baseline build. |
| `--session <id>` | Session id (defaults to `ORUN_SESSION_ID`). |
| `--type <type>` | Agent type (defaults to `ORUN_AGENT_TYPE`). Without one, the tool policy is deny-by-default and every tool call is denied. |
| `--task <KEY>` | Task key (defaults to `ORUN_TASK_KEY`). |

Identity comes from the sandbox environment the control plane injects:

| Variable | Meaning |
|---|---|
| `ORUN_CLOUD_API` | The api-edge base URL. Also seeded to the harness as `ORUN_BACKEND_URL` when that is unset, so the orun MCP server and every in-sandbox `orun` verb find the platform. |
| `ORUN_ORG_ID` | The workspace (`org_…`) id. |
| `ORUN_SESSION_ID` | The `as_…` session id. |
| `ORUN_SESSION_TOKEN` | The session bearer — the service-principal credential. |

All four are required; the first lines on standard error name the binary's
version and which of them arrived (the token redacted). A **grounded**
session also carries its repository binding, and `serve` clones it before
the loop starts, creating the task-keyed branch and putting the driver's
working directory inside the checkout:

| Variable | Meaning |
|---|---|
| `ORUN_REPO_REMOTE` | The https clone URL. Never carries a credential; git authenticates through `orun git-credential`, which mints short-lived repo-scoped tokens with the session's own bearer. |
| `ORUN_REPO_FULL_NAME` | The `owner/repo` full name. |
| `ORUN_REPO_REF` | The ref to clone at. |

Without `ORUN_REPO_REMOTE` the session is ungrounded and boots as before. A
grounded session that cannot reach its repository fails loudly; it never
degrades to ungrounded. The session token is also written to
`ORUN_TOKEN_FILE` on every rotation, so `orun` verbs the harness spawns
authenticate as the live session with no configuration. With
`--driver bootstrap`, a grounded session builds **in** its clone.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success. |
| `1` | Any error: lint errors, no object store, an unknown type or driver, a missing session, a relay or identity failure. |

## Related

- [Agent runtime](../concepts/agent-runtime.md)
- [`orun tui-next`](./orun-tui-next.md) — the cockpit v2 Agents surface
- [`orun mcp`](./orun-mcp.md) — the tool surface a type's policy filters
- [`orun skills`](./orun-skills.md) — the playbooks a session materializes
- [`orun baseline`](./orun-baseline.md) — what the `bootstrap` driver builds
- [`orun task`](./orun-task.md), [`orun pr`](./orun-pr.md)
