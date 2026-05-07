---
title: orun backend
---

The `orun backend` command group lets you provision, inspect, and remove a self-hosted Orun backend on your own Cloudflare account.

This is an alternative to using Orun Cloud (`https://orun-api.sourceplane.ai`). After running `orun backend init`, the resulting Worker URL is stored in `~/.orun/config.yaml` so that `orun auth login`, `orun cloud link`, `orun run --remote-state`, `orun status --remote-state`, and `orun logs --remote-state` can find it by default.

## Prerequisites

- A Cloudflare account with Workers, D1, R2, and Durable Objects enabled.
- A Cloudflare API token with the following permissions:
  - **Workers Scripts: Edit** (create/update/delete Worker scripts)
  - **Durable Objects: Edit**
  - **D1: Edit**
  - **R2: Edit**
- No GitHub PAT is required. GitHub OAuth is handled by the deployed Worker, not the CLI.

## Quick start

```bash
export CLOUDFLARE_ACCOUNT_ID=your-account-id
export CLOUDFLARE_API_TOKEN=your-api-token

# Provision all backend resources:
orun backend init

# Check readiness:
orun backend status

# After init, authenticate with your new backend:
orun auth login --backend-url https://orun-api.<subdomain>.workers.dev
```

## Commands

### `orun backend init`

Provisions the Orun backend on Cloudflare. Running `init` twice is safe — existing resources are reused idempotently.

```
orun backend init [flags]
```

**What it does:**

1. Creates (or reuses) a D1 database.
2. Creates (or reuses) an R2 bucket.
3. Applies bundled D1 migrations that have not yet been applied.
4. Uploads (or updates) the Cloudflare Worker script with all required bindings.
5. Enables the workers.dev route and discovers the Worker URL.
6. Sets required Worker vars (`GITHUB_JWKS_URL`, `GITHUB_OIDC_AUDIENCE`, and optionally `ORUN_PUBLIC_URL`, `ORUN_DASHBOARD_URL`).
7. Sets Worker secrets (`ORUN_SESSION_SECRET` is generated if not provided).
8. Stores non-secret bootstrap metadata in `~/.orun/config.yaml`.

**Flags:**

| Flag | Default | Description |
| --- | --- | --- |
| `--account-id` | `CLOUDFLARE_ACCOUNT_ID` | Cloudflare account ID |
| `--api-token` | `CLOUDFLARE_API_TOKEN` | Cloudflare API token |
| `--name` | `orun-api` | Worker script name |
| `--d1-name` | `orun-db` | D1 database name |
| `--r2-bucket` | `orun-storage` | R2 bucket name |
| `--oidc-audience` | `orun` | GitHub OIDC audience for the `GITHUB_OIDC_AUDIENCE` var |
| `--public-url` | (auto from workers.dev) | Public URL for `ORUN_PUBLIC_URL` |
| `--dashboard-url` | `ORUN_DASHBOARD_URL` env | Dashboard URL for `ORUN_DASHBOARD_URL` |
| `--github-client-id` | `GITHUB_CLIENT_ID` env | GitHub OAuth app client ID |
| `--github-client-secret` | `GITHUB_CLIENT_SECRET` env | GitHub OAuth app client secret (not stored in config) |
| `--session-secret` | `ORUN_SESSION_SECRET` env | Orun session HMAC secret (securely generated if absent) |
| `--dry-run` | false | Print planned actions without touching Cloudflare |
| `--json` | false | Output machine-readable JSON |

**GitHub OAuth setup (required for auth and dashboard):**

Create a GitHub OAuth app at https://github.com/settings/developers.

- **Homepage URL:** your `--public-url`
- **Authorization callback URL:** `<public-url>/v1/auth/callback`

Then run:

```bash
orun backend init \
  --github-client-id <id> \
  --github-client-secret <secret> \
  --dashboard-url https://your-dashboard.example.com
```

If you skip GitHub OAuth during init, the backend will work for GitHub Actions OIDC remote state, but `orun auth login` (human OAuth) will not work until you re-run `init` with the OAuth flags.

No GitHub PAT is required at any point.

### `orun backend status`

Reports the readiness of all backend resources. Safe to run in CI — requires only read/list Cloudflare permissions.

```
orun backend status [flags]
```

**Flags:**

| Flag | Default | Description |
| --- | --- | --- |
| `--account-id` | `CLOUDFLARE_ACCOUNT_ID` | Cloudflare account ID |
| `--api-token` | `CLOUDFLARE_API_TOKEN` | Cloudflare API token |
| `--name` | (stored or `orun-api`) | Worker script name |
| `--json` | false | Output machine-readable JSON |

Exits non-zero if any required resource is missing or migrations are not fully applied.

Secret values are never revealed — `status` only reports whether secrets are configured by name.

### `orun backend destroy`

Permanently removes all Orun backend resources managed by `orun backend init`. **This cannot be undone.**

```
orun backend destroy [flags]
```

Requires `--yes` to execute. Use `--dry-run` to preview what would be deleted.

`destroy` only deletes resources that were recorded by `orun backend init`. Pass `--adopted` to destroy resources by name that were not created by this CLI.

**Flags:**

| Flag | Default | Description |
| --- | --- | --- |
| `--account-id` | `CLOUDFLARE_ACCOUNT_ID` | Cloudflare account ID |
| `--api-token` | `CLOUDFLARE_API_TOKEN` | Cloudflare API token |
| `--name` | (stored) | Worker script name |
| `--yes` | false | Confirm destructive deletion |
| `--dry-run` | false | Show destruction plan without deleting resources |
| `--json` | false | Output machine-readable JSON |
| `--adopted` | false | Allow destroying resources not recorded by orun backend init |

**GitHub OAuth app configuration is not removed by `destroy`** — that lives in your GitHub account settings.

## Environment variables

| Variable | Description |
| --- | --- |
| `CLOUDFLARE_ACCOUNT_ID` | Cloudflare account ID (required) |
| `CLOUDFLARE_API_TOKEN` | Cloudflare API token (required) |
| `ORUN_SESSION_SECRET` | Orun session HMAC secret (generated if absent at init time) |
| `GITHUB_CLIENT_ID` | GitHub OAuth app client ID |
| `GITHUB_CLIENT_SECRET` | GitHub OAuth app client secret |
| `ORUN_DASHBOARD_URL` | Dashboard URL for OAuth callback |

No GitHub PAT or GitHub access token is required.

## Updating the backend

To update to a newer version of the Worker bundle:

1. Pull the latest `orun` CLI release (the bundle is embedded at build time).
2. Re-run `orun backend init` — it will update the Worker script and apply any new migrations without recreating resources.

## Config and security notes

`orun backend init` stores only non-secret bootstrap metadata in `~/.orun/config.yaml`:

```yaml
backendBootstrap:
  managedBy: orun-backend-init
  accountId: "..."
  workerName: orun-api
  d1DatabaseName: orun-db
  d1DatabaseUUID: "..."
  r2BucketName: orun-storage
  backendCommit: "..."
  initAt: "..."
backend:
  url: "https://orun-api...."
```

The config file is created with `0600` permissions. API tokens, session secrets, GitHub client secrets, and Orun access tokens are never written to disk by `orun backend`.
