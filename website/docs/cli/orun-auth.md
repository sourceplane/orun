---
title: orun auth
description: Sign in to Orunbase from the CLI with browser approval or a device code, check that the session still works, print a short-lived token, and sign out.
---

`orun auth` manages the local CLI session that every cloud command uses
outside GitHub Actions: `orun workspace`, `orun baseline`, `orun secrets`,
`orun task`, remote state, and the rest. In GitHub Actions the runner's OIDC
identity is exchanged for a scoped token instead, and no session is needed.

## Commands

```bash
orun auth login
orun auth login --device
orun auth login --workspace ws_3KF9TQ2P   # or --org acme (alias); a slug is also accepted
orun auth login --no-link
orun auth status
orun auth status --offline
orun auth logout
orun auth token --audience orun-backend
```

## Login

`orun auth login` is the single front door to Orunbase: it authenticates **and
auto-links the current repo** in one step (no separate `orun cloud link` first).

Browser login (the default):

```bash
orun auth login
```

The CLI asks the platform to start a login, opens the approval page in your
browser, and **polls** until you approve it there. There is no local
listener and no callback port, so it works from behind a firewall or on a
machine whose browser is elsewhere: paste the printed URL anywhere.

Device login, for a terminal with no browser at all:

```bash
orun auth login --device
```

This is the RFC 8628 device flow: the CLI prints a code and a verification
URL and polls until the code is approved, denied, or expires.

`--backend-url` is only needed against a self-hosted backend; the default is
the production Orunbase API.

The CLI stores only Orun-issued access and refresh tokens. It does not store GitHub OAuth access tokens or GitHub PATs.

The backend URL is resolved from `--backend-url` → `ORUN_BACKEND_URL` →
`execution.state.backendUrl` in `intent.yaml` → `~/.orun/config.yaml`, so inside a
repo that already declares its backend you can usually drop the flag.

### Auto-link

After a successful login, `orun auth login` links the current repo and prints the
result inline:

```
✓ linked this repo → acme/my-service
```

How it resolves the link:

| Situation | Behavior |
| --- | --- |
| No git remote | No-op — prints `no git remote here — run orun auth login inside a repo to link it` |
| Already linked | Reuses the cached link (`✓ already linked this repo → …`) |
| One org, unambiguous | Links automatically |
| Several orgs, interactive | Prompts once |
| Several orgs, non-interactive | Errors and asks for `--workspace <ws_…\|slug>` (alias `--org`) |
| No orgs at all | Materializes a **personal org** (`✓ created your personal org <slug>`) and links |
| OSS / local backend | Short-circuits to the fixed `_local/_local` scope |

If login succeeds but linking can't complete automatically, the command still
exits 0 and tells you how to finish (`orun auth login --workspace <ws_…|slug>`
or `orun cloud link`).

| Flag | Meaning |
| --- | --- |
| `--device` | Use the platform device login flow (RFC-8628) for headless terminals |
| `--workspace <ws_…\|slug>` | Workspace to link this repo under, when you belong to several. Accepts a Workspace ID `ws_…`, a slug, or an `org_…` id |
| `--org <slug>` | Retained alias of `--workspace` (the CLI reads either and prefers `--workspace`) |
| `--no-link` | Authenticate only; don't auto-link the repo |
| `--backend-url <url>` | Backend URL (or set `ORUN_BACKEND_URL` / declare it in `intent.yaml`) |

## Status

```bash
orun auth status
```

```text
User: pullely
Email: pullely@sourceplane.ai
Backend URL: https://api-edge-prod.oruncloud.workers.dev
Orgs:
  - acme — owner
  - test work (test-work) — owner
Access token: 2026-09-22T11:42:07Z (valid)
Session: ✓ usable (access token valid for 14m; the refresh token is checked when it is next needed)
Current Git remote: acme/storefront (linked)
```

`status` **checks that the login still works**: it refreshes the session
against the platform and exits non-zero if the refresh is refused, so a
script can rely on it. `--offline` reports what is stored without the live
check. An unreadable credential store is reported as such, not as "not
logged in".

With a workspace-scoped API key in `ORUN_TOKEN`, there is no membership list
to show; use `orun workspace <ws-id|slug>` to confirm which workspace the key
belongs to.

## Logout

```bash
orun auth logout
```

Revokes the backend refresh token when available, then removes local credentials.

## Token

```bash
orun auth token --audience orun-backend
```

Prints the current short-lived Orun access token. This is intended for explicit debugging or automation handoff.

## Storage

- The session (access and refresh tokens, user, workspaces, backend URL) is
  kept in the OS credential store when one is available
- Fallback: `~/.orun/credentials.json` with `0600` permissions
- Non-secret configuration, including the selected workspace and cached
  repository links, is in `~/.orun/config.yaml`
- Headless machines use `ORUN_TOKEN`, or `ORUN_TOKEN_FILE` for a token a
  runner keeps refreshed (the file wins when both are set)

## Related

- [`orun workspace`](./orun-workspace.md) — choose the workspace to work in
- [`orun cloud`](./orun-cloud.md) — the repository link
- [Create a workspace and build it from a baseline](../examples/bootstrap-a-product-from-a-baseline.md)
- [Orunbase: CLI and CI authentication](https://docs.orunbase.com/platform/identity/cli-and-ci-auth) — the wire protocol
