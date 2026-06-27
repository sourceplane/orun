---
title: orun cloud
---

`orun cloud` manages the link between the current repository and an Orun Cloud
org/project (the tenancy spine). The link is resolved once and cached in
`~/.orun/config.yaml`; every `--remote-state` call then runs under that scope.

:::tip Most teams don't run `orun cloud link` directly
[`orun auth login`](./orun-auth.md) already authenticates **and auto-links** the
current repo in one step, and `orun run --remote-state` self-heals an unlinked
repo on the fly. Reach for `orun cloud link` for explicit, scripted, or CI
bootstrap linking, or to inspect / change an existing link.
:::

## Commands

```bash
orun cloud link                                   # resolve git remote → pick/create org/project → cache the link
orun cloud link --org acme --project platform     # non-interactive (CI/bootstrap)
orun cloud unlink                                 # drop the local link (the server-side link is untouched)
orun cloud status                                 # show the linked org/project, remote, and backend URL
orun cloud open                                   # open the project's console page in the browser
```

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

The non-interactive form `--org <slug> --project <slug>` skips all prompts and is
intended for CI and bootstrap scripts.

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
auto-links and proceeds (disambiguate with `--org` / `--project` when you belong
to several). It only fails fast before a backend call when it genuinely can't
proceed:

- **Not logged in** → run `orun auth login`.
- **Repo not linked and can't auto-link** (e.g. no git remote, or an ambiguous
  org in a non-interactive shell) → run `orun auth login` (which links), or
  `orun cloud link --org <slug>`.
