---
title: orun cloud
---

`orun cloud` manages the link between the current repository and an Orun Cloud
workspace/project (the tenancy spine). A workspace can be named by its Workspace
ID (`ws_…`, short and immutable), its slug, or a legacy `org_…` id — all are
accepted wherever `--workspace`/`--org` is. The link is resolved once and cached in
`~/.orun/config.yaml`; every `--remote-state` call then runs under that scope.

This page covers the CLI surface. The Orun Cloud **platform** — workspaces,
access control, the API, webhooks, billing, and self-hosting — is documented at
[docs.orun.dev](https://docs.orun.dev).

:::tip Most teams don't run `orun cloud link` directly
[`orun auth login`](./orun-auth.md) already authenticates **and auto-links** the
current repo in one step, and `orun run --remote-state` self-heals an unlinked
repo on the fly. Reach for `orun cloud link` for explicit, scripted, or CI
bootstrap linking, or to inspect / change an existing link.
:::

## Commands

```bash
orun cloud link                                   # resolve git remote → pick/create org/project → cache the link
orun cloud link --workspace ws_3KF9TQ2P --project platform   # non-interactive (CI/bootstrap); --org is a retained alias, a slug is also accepted
orun cloud unlink                                 # drop the local link (the server-side link is untouched)
orun cloud status                                 # show the linked org/project, remote, and backend URL
orun cloud check                                  # is this repo allow-listed for the resolved org?
orun cloud open                                   # open the project's console page in the browser
```

## Check allow-listing (`orun cloud check`)

A pre-flight for the credential-free CI path. Before you wire up a workflow,
`orun cloud check` answers **"is this repo allow-listed for the org a run would
use?"** — turning a mysterious CI `404` into a one-command local diagnosis.

```bash
orun cloud check                 # use the resolved org (intent / link)
orun cloud check --workspace ws_3KF9TQ2P   # check against a specific workspace (--org is a retained alias; a slug also works)
```

It resolves the workspace exactly the way a run does — `--workspace`/`--org` >
`ORUN_WORKSPACE`/`ORUN_ORG` > `intent.yaml`
`execution.state.workspace`/`execution.state.org` > the cached link — lists the
allow-list (`GET /v1/organizations/{orgId}/cli/links`), and reports whether the
current repo is on it:

```
✓ sourceplane/lumen is allow-listed for org acme
  project: lumen
```

A repo that is **not** allow-listed exits non-zero and prints exactly how to add
it (console **Git Repos → add from GitHub**, or `orun cloud link`). Because the
platform hides denials as `404` (resource-hiding), `check` consults the listing
and never over-claims "not allow-listed" from a status code alone.

## Link the current repo

```bash
orun cloud link --backend-url https://api.orun.cloud
```

`orun cloud link`:

1. Ensures the CLI is logged in (requires `orun auth login`).
2. Detects the current git remote (`git remote get-url origin`).
3. Calls `GET /v1/cli/links/resolve?remoteUrl=…` with the CLI session token.
4. Resolves the scope:
   - **One existing link** → uses it.
   - **No link** → presents an org picker (the project is created on demand) and
     calls `POST /v1/organizations/{orgId}/cli/links`. When the account has **no
     orgs at all**, a **personal org is materialized** on first link instead of
     dead-ending.
   - **Several links** → presents an org/project picker.
5. Caches the resulting org/project IDs + slugs and the server's normalized
   `remoteUrl` in `~/.orun/config.yaml`.

The non-interactive form `--workspace <ws_…|slug> --project <slug>` (or the
retained alias `--org <slug>`) skips all prompts and is intended for CI and
bootstrap scripts.

No GitHub PAT or OAuth token is required — the Orun CLI session from
`orun auth login` is sufficient.

### OSS / local backend

Against the OSS single-tenant `orun backend` server (which serves the contract
with a fixed `_local/_local` scope), `orun cloud link` short-circuits to
`_local/_local` and does not call the workspace-link API.

### Error handling

| Backend response | Meaning | Fix |
|---|---|---|
| `422` | The git remote is not a recognized URL | Check the remote with `git remote -v` |
| `404` | Not authorized to link, or the org/project does not exist | Verify org membership or run `orun auth login` again |
| `409` | The remote is already linked to an active org/project | `orun cloud status`, or `orun cloud unlink` first |
| `412 limit_reached` | The org's project limit is reached | Upgrade the plan or pick an existing project |

## Fail-fast on `--remote-state`

`orun run --remote-state` self-heals an unlinked repo when you're logged in — it
auto-links and proceeds (disambiguate with `--workspace` / `--org` / `--project`
when you belong to several). It only fails fast before a backend call when it genuinely can't
proceed:

- **Not logged in** → run `orun auth login`.
- **Repo not linked and can't auto-link** (e.g. no git remote, or an ambiguous
  org in a non-interactive shell) → run `orun auth login` (which links), or
  `orun cloud link --workspace <ws_…|slug>` (alias `--org <slug>`).
