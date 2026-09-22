---
title: Workspaces and tenancy
description: The scope ladder every Orunbase call runs under — account, workspace, project, environment, component — how a workspace is resolved, the two kinds of repository link, and how a headless process authenticates.
---

Every command that talks to Orunbase runs against exactly one **workspace**.
This page is about how that workspace is chosen, what sits above and below it,
and how a repository and a headless process prove which one they belong to.

## The scope ladder

```text
account ─▶ workspace ─▶ project ─▶ environment ─▶ component
           ws_…         prj_…      env_…          (the catalog)
           (slug)       (== a repo)
```

| Rung | What it is | Where you meet it |
|---|---|---|
| **Account** | The billing and ownership boundary. A plan, its entitlements, and the baselines the account registered itself. | `orun baseline list` shows "this workspace's account" |
| **Workspace** | The tenancy unit: members, roles, integration connections, secrets, the task plane, agent sessions. Named by a `ws_…` id or a slug. | `--workspace`, `orun workspace` |
| **Project** | One repository. `orun auth login` links the current repository as a project named after it; there is no project without a repo behind it. | `--project`, `orun cloud status` |
| **Environment** | A named runtime context inside a project (`stage`, `prod`), the rung secrets are scoped to. | `--env` on `orun secrets`, `orun integrations` |
| **Component** | A deployable unit declared in the repository and resolved into the catalog. | `component.yaml` |

Secrets show the ladder most plainly: a key set with `--shared` is inherited
by every project in the workspace, `--project` by every environment in the
project, and `--env <slug>` lands on one environment. Lower rungs shadow
higher ones unless the higher one is `--locked`.

## Ids, slugs, and the legacy spelling

A workspace has a short, immutable **`ws_…` id** and a human **slug**. Both
are accepted, case-insensitively, wherever a workspace is named. The older
**`org_…` id** is still accepted everywhere too: `--org` is an alias of
`--workspace`, `ORUN_ORG` of `ORUN_WORKSPACE`, and `execution.state.org` of
`execution.state.workspace`. Read either, prefer the workspace spelling; a
sane configuration sets at most one.

## How a workspace is resolved

A workspace is resolved most-specific-first, and the chain is the same for
every cloud command:

1. `--workspace <ws-id|slug>` on the command (`--org` is the legacy alias)
2. `ORUN_WORKSPACE` (or `ORUN_ORG`)
3. `intent.yaml` → `execution.state.workspace`
4. this repository's link, cached from `orun auth login` or `orun cloud link`
5. the working workspace chosen with `orun workspace use`

The selection sits **last on purpose**. It fills in for a bare command in a
directory that declares nothing, and it can never silently retarget a
repository that declares or links its own tenancy. `orun workspace` names the
rung that actually won, and says so when something more specific overrides
your selection in this directory.

Declaring `execution.state.workspace` in `intent.yaml` is also the request to
enforce it: a non-interactive remote operation that then resolves no
workspace fails fast with an actionable message instead of exchanging an
empty claim into an ambiguous scope.

The backend URL is resolved the same way — `--backend-url`, then
`ORUN_BACKEND_URL`, then `intent.yaml` `execution.state.backendUrl`, then
`cloud.url` in `~/.orun/config.yaml` — and falls through to the hosted
Orunbase API when nothing names one. A self-hosted `orun backend init` server
is single-tenant; linking a repository to it caches the fixed `_local/_local`
scope and never calls the platform's link API.

## Two kinds of repository link

Two different things are both called "linking a repository", and a baseline
build needs the second one.

| | The CLI link | The console repository link |
|---|---|---|
| Created by | `orun auth login` (auto-links) or `orun cloud link --workspace <slug>` | The workspace's Git tab in the console, through the GitHub App |
| Identified by | The normalized git remote, cached in `~/.orun/config.yaml` with the workspace and project ids and slugs | A `repl_…` id, with the repository's default branch, a status, and an `agentAccess` switch |
| What it proves | This checkout belongs to this workspace and project; the repository is on the workspace's **allow-list**, which the credential-free CI path (GitHub OIDC exchange) checks | The platform may act on the repository itself: clone it into a sandbox, push branches, open and land pull requests |
| Needed by | `orun run --remote-state`, `orun catalog push`, `orun secrets`, and every other cloud command's rung 4 | `orun baseline new --via-platform` and every grounded agent session |
| Inspect | `orun cloud status`, `orun cloud check` | `orun baseline new --via-platform` prints the resolved link before anything starts |

`orun cloud check` lists the resolved workspace's allow-list and reports
whether this repository is on it — run it from a development machine before
wiring up CI, because a repository that is not allow-listed gets a 404 on the
OIDC path, by design, rather than a hint.

A platform build resolves the console link by name, case-insensitively,
because GitHub is. One link is used; none is an error naming the Git tab;
two links to the same repository are refused with both ids so you pass
`--repo-link repl_…` — an hour of automated commits lands there, and picking
for you is not something the CLI can get wrong quietly. A link whose
`agentAccess` is `off` is linked for everything except this, and is refused
with the switch to flip.

## Headless authentication

A person authenticates once with `orun auth login` and the CLI keeps a
refreshing session. A container, a CI driver, or a sandbox authenticates with
an ambient bearer instead:

- **`ORUN_TOKEN`** — a static bearer, read on every command.
- **`ORUN_TOKEN_FILE`** — a path whose contents are read lazily on every
  command. Inside an agent sandbox, `orun agent serve` writes each rotation
  of the session token here, so a credential that rotates every few minutes
  stays fresh across an hour-long build.

When both are set, **the file wins**, and orun says so on standard error: the
file refreshes for the process's whole life, while an environment copy is
frozen at export time. An agent that ran
`export ORUN_TOKEN=$(cat $ORUN_TOKEN_FILE)` doomed every call after the next
rotation. In CI, a GitHub Actions OIDC token is exchanged first; `ORUN_TOKEN`
comes next; a stored session last.

A workspace-scoped token sees no membership list, so `orun workspace list`
cannot answer for it. `orun workspace <ws-id|slug>` resolves a named
workspace against the backend instead and prints a stable `slug:` line that
flows can read.

## Related

- [`orun workspace`](../cli/orun-workspace.md) — show, list, use, create
- [`orun cloud`](../cli/orun-cloud.md) — the CLI link: link, unlink, status, check, open
- [`orun auth`](../cli/orun-auth.md) — login, logout, status, token
- [`orun secrets`](../cli/orun-secrets.md) — the rungs a secret is scoped to
- [Baselines](baselines.md) — why a platform build needs the console link
- [The agent runtime](agent-runtime.md) — grounded sessions and the token file
