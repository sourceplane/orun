---
title: orun mcp
---

`orun mcp serve` runs **the orun MCP** — the ecosystem's one local
[Model Context Protocol](https://modelcontextprotocol.io) server: a minimal,
dependency-free JSON-RPC 2.0 server over stdio that gives an agent hands on
everything orun through a single connection.

One loop composes two tool planes — **34 tools under one initialize**:

- **The work plane** (9 tools) — orun's delivery-derived work tracker:
  tasks with *derived* lifecycle and evidence, sealed spec briefs,
  mutator-only writes. Mounted when a workspace scope resolves.
- **The platform plane** (25 tools) — the Orun Cloud public API: catalog,
  runs and logs, audit, events, access, usage, billing, config, secret
  metadata, webhooks. 19 reads plus 6 policy-gated writes. Mounted whenever
  cloud auth resolves.

```bash
orun mcp serve
```

"Who owns billing-worker?", "why did the last prod run fail?", and "what's
in flight on the work plane?" — one server answers all three.

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
- **Work tools mount when a workspace scope resolves** (the `--workspace`
  flag, the linked repo, or intent config). Without one, the server starts
  platform-only and says so on stderr.
- **Workspace defaulting.** When serve resolves an ambient workspace, it
  fills an absent `workspace` argument on platform tools and the advertised
  schemas mark it optional. An explicit argument always wins.

## Flags

```bash
orun mcp serve [--workspace <ref>] [--backend-url <url>] [--read-only]
```

| Flag | Effect |
| --- | --- |
| `--workspace <ref>` | Target workspace (org id or slug; defaults to the linked repo's). Mounts the work plane and becomes the platform tools' default `workspace`. |
| `--backend-url <url>` | Backend URL (Orun Cloud or self-hosted). |
| `--read-only` | Drop the 6 platform write tools from the roster (28 tools instead of 34). Filtered from `tools/list` *and* blocked at execution. |

`--read-only` deliberately does **not** touch the work plane's write tools:
they are mutator-shaped by design (one audited mutator surface for UI, MCP,
and CLI — the work spec's WP-6 decision), and the work plane's safety model
is sealed briefs plus mutator-only writes, not a read-only mode. The flag is
a platform-plane switch.

## `orun mcp tools`

Print the merged roster without starting a server:

```bash
orun mcp tools               # NAME / PROVIDER / READ-ONLY / DESCRIPTION table
orun mcp tools --json        # the same rows as JSON
orun mcp tools --read-only   # the roster as `serve --read-only` advertises it
```

## The work plane (9 tools)

| Tool | Kind | Purpose |
| --- | --- | --- |
| `work_query` | read | The fold summary — every task's derived rung WITH its evidence |
| `work_get` | read | One task in full (contract, lifecycle, evidence, pins) |
| `work_timeline` | read | One item's unified timeline: coordination and observation logs interleaved by time, evidence attached |
| `spec_get` | read | A sealed, content-addressed SpecSnapshot (intent only — fold output is asserted absent from the sealed bytes) |
| `spec_doc` | read | A spec's cloud document revision (content-addressed; latest when `rev` is omitted) |
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

## The platform plane (25 tools)

Every platform tool calls the Orun Cloud public API with **your own
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

### Writes (dropped by `--read-only`)

| Tool | Purpose |
| --- | --- |
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

The platform tools are the same 25 served by the hosted remote MCP server
(Streamable HTTP, part of Orun Cloud) — **identical names, schemas, and
semantics**, so prompts and docs are portable between the local and remote
surfaces. The contract is a machine-readable tool manifest exported from the
hosted plane, vendored into this repo, and enforced by a parity test: any
drift fails CI. The server identifies itself as serverInfo `orun` (renamed
from `orun-work` when the surface unified).

## Related

- [`orun work`](./orun-work.md) — the same work fold in the terminal
- [`orun spec`](./orun-spec.md) — sealed briefs (`spec_get`'s CLI twin)
- [`orun auth`](./orun-auth.md) / [`orun cloud`](./orun-cloud.md) — the
  session and repo link the server mounts from
