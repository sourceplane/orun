---
title: orun mcp
---

`orun mcp serve` runs **the orun MCP** — the ecosystem's one local
[Model Context Protocol](https://modelcontextprotocol.io) server: a minimal,
dependency-free JSON-RPC 2.0 server over stdio that gives an agent hands on
everything orun through a single connection.

One loop composes two tool planes — **34 tools under one initialize**, plus
the built-in `connection_info`:

- **The pen plane** (1 tool) — `pr_open` writes a PR's lineage: the branch
  renamed onto the grammar, pushed, and the machine-readable manifest in the
  body. Mounted whenever the server runs inside a repository checkout — it
  needs git, not a credential.
- **The platform plane** (33 tools) — the Orunbase public API: catalog,
  runs and logs, audit, events, access, usage, billing, config, secret
  metadata, webhooks, skills, and the task plane (tasks, epics,
  milestones, contracts, derived verdicts). 24 reads plus 9 policy-gated
  writes. Mounted whenever cloud auth resolves.

```bash
orun mcp serve
```

"Who owns billing-worker?", "why did the last prod run fail?", and "open the
PR for what I just built" — one server answers all three.

## Setup

Authenticate once with the standard CLI session, then register the server
with your agent host:

```bash
orun auth login
```

**Claude Code**

```bash
claude mcp add orun -- orun mcp serve
```

**Cursor** — `.cursor/mcp.json` (project) or `~/.cursor/mcp.json` (global):

```json
{
  "mcpServers": {
    "orun": { "command": "orun", "args": ["mcp", "serve"] }
  }
}
```

**VS Code** — `.vscode/mcp.json`:

```json
{
  "servers": {
    "orun": { "type": "stdio", "command": "orun", "args": ["mcp", "serve"] }
  }
}
```

**Any other MCP client** — a stdio server: command `orun`, arguments
`mcp serve`. Stdout is protocol-pure; diagnostics go to stderr.

Authentication and workspace routing reuse the standard CLI session
([`orun auth`](./orun-auth.md) / [`orun cloud link`](./orun-cloud.md)) —
there is no separate MCP credential.

## What mounts when

Mounting is contextual, never guessed:

- **Platform tools mount whenever auth resolves.** They take an explicit
  `workspace` argument, so one server reaches every workspace you belong to.
- **The pen mounts on the checkout.** Run the server from inside the repo
  and `pr_open` is there, with or without cloud auth. Outside one it skips
  and `connection_info` says why.
- **Workspace defaulting.** When serve resolves an ambient workspace, it
  fills an absent `workspace` argument on platform tools and the advertised
  schemas mark it optional. An explicit argument always wins.

## Flags

```bash
orun mcp serve [--workspace <ref>] [--backend-url <url>] [--read-only]
```

| Flag | Effect |
| --- | --- |
| `--workspace <ref>` | Target workspace (org id or slug; defaults to the linked repo's). Becomes the platform tools' default `workspace`. |
| `--backend-url <url>` | Backend URL (Orunbase or self-hosted). |
| `--read-only` | Drop the 9 platform write tools from the roster (25 tools instead of 34). Filtered from `tools/list` *and* blocked at execution. |

`--read-only` deliberately does **not** touch `pr_open`. The flag scopes what
the server may change *in the cloud*; the pen changes your checkout and your
GitHub, which is the whole reason it is mounted, and silently stripping it
would leave an agent with no way to deliver its work.

## `orun mcp tools`

Print the merged roster without starting a server:

```bash
orun mcp tools               # NAME / PROVIDER / READ-ONLY / DESCRIPTION table
orun mcp tools --json        # the same rows as JSON
orun mcp tools --read-only   # the roster as `serve --read-only` advertises it
```

## The pen plane (1 tool)

| Tool | Kind | Purpose |
| --- | --- | --- |
| `pr_open` | write | Open the task's PR with its lineage: the branch renamed onto the `orun/<task-key>-<slug>` grammar when needed, pushed, and the machine-readable manifest block written into the body — the task, the skill revisions the session ran under, the session id |

With a GitHub credential ambient (`GITHUB_TOKEN` / `GH_TOKEN` / `gh auth`)
the PR opens through the API; without one the pen still prepares everything
and returns the compare URL plus the body to use — honest either way. The
same rules are checkable locally with `orun pr check`, and
Orunbase's `orun/compliance` check verifies them on the PR itself.

Outside a repository checkout the tool is not mounted at all, and
`connection_info` reports the reason rather than the server guessing at git.

## The platform plane (33 tools)

Every platform tool calls the Orunbase public API with **your own
credential** — RBAC, rate limits, audit, and metering apply exactly as they
would to you. Results are one summary line plus compact JSON, byte-capped at
64 KiB with cursor/`fromSeq` continuation.

### Orientation

| Tool | Purpose |
| --- | --- |
| `whoami` | The authenticated actor and their workspace memberships |
| `workspaces_list` | Workspaces the caller belongs to |
| `projects_list` | Projects in a workspace; pass `project` to include its environments |

### Catalog

| Tool | Purpose |
| --- | --- |
| `catalog_search` | Search the org-wide service catalog (kind, owner, project, environment, free text) |
| `catalog_get_entity` | One entity by exact `entityRef` (e.g. `component:default/api`) |
| `catalog_read_doc` | Browse git-authored catalog docs; pass a row's `digest` to read one |

### Delivery

| Tool | Purpose |
| --- | --- |
| `runs_list` | Delivery runs, newest first — org-wide or per project |
| `runs_get` | One run's projection plus its plan-DAG job statuses |
| `runs_read_logs` | One job's assembled logs with a live-tail cursor (`fromSeq`) |

### Governance

| Tool | Purpose |
| --- | --- |
| `audit_search` | The immutable audit log: time range, actor, subject, event type, category |
| `events_search` | The typed event stream; pass `eventId` for one event's envelope |
| `security_events_list` | The calling actor's authentication/session security events |
| `access_explain` | Effective permissions with provenance, plus member and team rosters |

### Operations

| Tool | Purpose |
| --- | --- |
| `usage_summary` | Metered usage for one metric: totals plus hour/day rollups |
| `quota_check` | One metric against the workspace's quota: allowed/limit/used/remaining |
| `billing_summary` | Billing posture: plan, subscription, customer status, entitlements |

### Config

| Tool | Purpose |
| --- | --- |
| `config_read` | Settings and feature flags at one scope (organization, project, or project+environment) |
| `secrets_list` | Secret **metadata** only (keys, versions, rotation state) — values are write-only platform-wide |
| `webhook_deliveries_list` | Webhook endpoints; pass `endpoint` to page through its delivery attempts |

### The task plane

| Tool | Purpose |
| --- | --- |
| `task_list` | The workspace's tasks — key, title, epic/milestone membership, whether a contract is attached; narrow with `epic`, `milestone`, `assignee` (`me`, `agents`, or a subject ref) |
| `task_get` | One ref, the full picture: a task's contract + derived verdict (rung, the observation behind it, dependencies, evidence); an epic's status beside its rollup, container contract and spec docs; a milestone with its epic |
| `policy_preview` | "If this contract were attached, which secrets would stop resolving?" — the exact narrow-only intersection enforcement uses, before anything is attached |

The verdict is derived from what the platform observed — a branch on the
`orun/<KEY>-<slug>` grammar, an open PR, a merge, gate results — never a
typed status. A merged PR folds a task to `done` only when its contract
declared its gates (an explicit empty list means "merge alone finishes
it"), which is why `task_create` can attach a contract in the same call.

### Writes (dropped by `--read-only`)

| Tool | Purpose |
| --- | --- |
| `task_create` | Create a task — key from the allocator (adopt / derive / mint), clubbed under an `epic` and/or `milestone`, with a `brief`, an `assignee`, and an optional `contract` attached under the same idempotency attempt |
| `epic_create` | Create an epic; a slug already taken comes back as the existing epic with `existed: true`, so re-runs adopt instead of duplicating |
| `milestone_create` | Add a phase to an epic, positioned with `after` (`mls_…` | `null` = first | omitted = last), with exit criteria |
| `project_create` | Create a project in a workspace |
| `environment_create` | Create an environment under a project |
| `flag_set` | Create or update a feature flag at one config scope |
| `webhook_create` | Create a webhook endpoint (plus its event subscriptions) |
| `webhook_delivery_replay` | Re-send a past delivery attempt through the normal signing/delivery path |
| `member_invite` | Invite a person by email with an organization role |

Writes ride safety rails: every attempt carries a per-attempt
`Idempotency-Key` (a retry replays instead of duplicating; pass your own
`idempotencyKey` to control it), every platform call is stamped
`x-client-surface: mcp` for audit provenance, and `member_invite` never
returns the invitation's one-time accept token. Availability is gated by the
workspace's `feature.mcp_server` entitlement (fail-open; an explicit denial
returns `entitlement_required`).

Tool failures return MCP `isError` results — structured verdicts the agent
can reason about (`forbidden: … (requestId: …)`), never protocol faults.

## One contract, two implementations

The platform tools are the same 33 served by the hosted remote MCP server
(Streamable HTTP, part of Orunbase) — **identical names, schemas, and
semantics**, so prompts and docs are portable between the local and remote
surfaces. The contract is a machine-readable tool manifest exported from the
hosted plane, vendored into this repo, and enforced by a parity test: any
drift fails CI. The server identifies itself as serverInfo `orun` (renamed
from `orun-work` when the surface unified).

## Related

- `orun pr` — the pen in the terminal (`pr_open`'s CLI twin) and its local preflight (`orun pr check`)
- [`orun auth`](./orun-auth.md) / [`orun cloud`](./orun-cloud.md) — the
  session and repo link the server mounts from
