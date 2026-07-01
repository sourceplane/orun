---
title: orun auth
---

`orun auth` manages the local CLI session used for remote state outside GitHub Actions.

## Commands

```bash
orun auth login
orun auth login --device
orun auth login --workspace ws_3KF9TQ2P   # or --org acme (alias); a slug is also accepted
orun auth login --no-link
orun auth status
orun auth logout
orun auth token --audience orun-backend
```

## Login

`orun auth login` is the single front door to Orun Cloud: it authenticates **and
auto-links the current repo** in one step (no separate `orun cloud link` first).

Interactive browser login:

```bash
orun auth login --backend-url https://orun-api.example.workers.dev
```

Headless device login:

```bash
orun auth login --device --backend-url https://orun-api.example.workers.dev
```

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

Shows:

- GitHub login
- backend URL
- access-token expiry
- whether the current Git remote is linked for local remote-state runs

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

- Prefer the OS credential store when available
- Fallback to `~/.orun/credentials.json` with `0600` permissions
- Store non-secret config in `~/.orun/config.yaml`
