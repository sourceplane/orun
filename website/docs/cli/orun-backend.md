---
title: orun backend
---

The `orun backend` command group lets you provision, inspect, and remove a self-hosted Orun backend on your own Cloudflare account.

This is an alternative to using Orun Cloud (`https://orun-api.sourceplane.ai`). After running `orun backend init`, the resulting Worker URL is stored in `~/.orun/config.yaml` so that `orun auth login`, `orun cloud link`, `orun run --remote-state`, `orun status --remote-state`, and `orun logs --remote-state` can find it by default.

## Prerequisites

- A Cloudflare account with Workers, D1, R2, Durable Objects, and Queues enabled.
- A Cloudflare API token with the following permissions:
  - **Workers Scripts: Edit** (create/update/delete Worker scripts)
  - **Durable Objects: Edit**
  - **D1: Edit**
  - **R2: Edit**
  - **Queues: Edit** (create/delete queues and manage consumers)
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
2. Applies bundled D1 migrations that have not yet been applied (currently 6, through `0006_tenant_routes.sql`).
3. Creates (or reuses) an R2 bucket.
4. Creates (or reuses) the catalog ingest queue (default `orun-catalog-ingest`).
5. Creates (or reuses) the catalog dead-letter queue (default `orun-catalog-ingest-dlq`).
6. Uploads (or updates) the Cloudflare Worker script with all required bindings in a single call:
   - Durable Objects: `COORDINATOR` (RunCoordinator), `RATE_LIMITER` (RateLimitCounter)
   - D1: `DB`
   - R2: `STORAGE`
   - Queue producer: `CATALOG_INGEST_QUEUE`
   - Plain-text vars (included in upload metadata to avoid binding clobber)
7. Enables the workers.dev route and discovers the Worker URL.
8. Sets Worker secrets (`ORUN_SESSION_SECRET` is generated if not provided).
9. Attaches a Worker queue consumer to the catalog queue with Task 0018 settings (`batch_size=10`, `max_retries=3`, `max_wait_time_ms=30000`, dead-letter queue = catalog DLQ).
10. Sets the cron schedule (default `*/15 * * * *`) for scheduled Worker tasks.
11. Stores non-secret bootstrap metadata in `~/.orun/config.yaml`.

**Note on multi-shard D1:** `DB_CATALOG_0` and `DB_CATALOG_1` catalog shard bindings are intentionally not provisioned by bootstrap until the cross-shard JOIN proposal (`ai/proposals/task-0016-spec-update.md`) is resolved. The single-DB fallback handles all catalog storage for self-hosted deployments.

**Flags:**

| Flag | Default | Description |
| --- | --- | --- |
| `--account-id` | `CLOUDFLARE_ACCOUNT_ID` | Cloudflare account ID |
| `--api-token` | `CLOUDFLARE_API_TOKEN` | Cloudflare API token |
| `--name` | `orun-api` | Worker script name |
| `--d1-name` | `orun-db` | D1 database name |
| `--r2-bucket` | `orun-storage` | R2 bucket name |
| `--catalog-queue` | `orun-catalog-ingest` | Catalog ingest queue name |
| `--catalog-dlq` | `orun-catalog-ingest-dlq` | Catalog dead-letter queue name |
| `--catalog-cron` | `*/15 * * * *` | Worker cron schedule |
| `--oidc-audience` | `orun` | GitHub OIDC audience for the `GITHUB_OIDC_AUDIENCE` var |
| `--public-url` | (auto from workers.dev) | Public URL for `ORUN_PUBLIC_URL` |
| `--dashboard-url` | `ORUN_DASHBOARD_URL` env | Dashboard URL for `ORUN_DASHBOARD_URL` |
| `--github-client-id` | `GITHUB_CLIENT_ID` env | GitHub OAuth app client ID |
| `--github-client-secret` | `GITHUB_CLIENT_SECRET` env | GitHub OAuth app client secret (not stored in config) |
| `--session-secret` | `ORUN_SESSION_SECRET` env | Orun session HMAC secret (securely generated if absent) |
| `--dry-run` | false | Print planned actions without touching Cloudflare (no credentials required) |
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

Checks:
- Worker script exists
- D1 database exists
- All bundled migrations are applied
- R2 bucket exists
- Catalog queue exists
- Catalog DLQ exists
- Worker queue consumer is attached with the expected settings
- Cron schedule is configured
- Expected secret names are present

Exits non-zero if any required resource is missing or misconfigured.

Secret values are never revealed — `status` only reports whether secrets are configured by name.

**Flags:**

| Flag | Default | Description |
| --- | --- | --- |
| `--account-id` | `CLOUDFLARE_ACCOUNT_ID` | Cloudflare account ID |
| `--api-token` | `CLOUDFLARE_API_TOKEN` | Cloudflare API token |
| `--name` | (stored or `orun-api`) | Worker script name |
| `--json` | false | Output machine-readable JSON |

### `orun backend destroy`

Permanently removes all Orun backend resources managed by `orun backend init`. **This cannot be undone.**

```
orun backend destroy [flags]
```

Requires `--yes` to execute. Use `--dry-run` to preview what would be deleted.

**Destroy order** (avoids Cloudflare API conflicts with dangling bindings):

1. Clear cron schedule
2. Delete queue consumer (before deleting the queue)
3. Delete Worker script
4. Delete catalog queue
5. Delete catalog DLQ
6. Delete D1 database
7. Delete R2 bucket

Missing resources at any step are treated as warnings, not fatal blockers.

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

## GitHub Actions catalog sync and WAF

`POST /v1/catalog/sync` via a custom domain may be blocked by Cloudflare WAF managed challenges for GitHub Actions runner IPs. If this occurs, use the `workers.dev` fallback URL (`https://<worker-name>.<subdomain>.workers.dev`) in CI workflows until the WAF policy is explicitly updated to allow GHA runner ranges.

## Updating the backend

To update to a newer version of the Worker bundle:

1. Pull the latest `orun` CLI release (the bundle is embedded at build time).
2. Re-run `orun backend init` — it will update the Worker script, apply any new migrations, and update queue consumer settings and the cron schedule without recreating existing resources.

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
  catalogQueueName: orun-catalog-ingest
  catalogQueueID: "..."
  catalogDLQName: orun-catalog-ingest-dlq
  catalogDLQID: "..."
  catalogCron: "*/15 * * * *"
  backendCommit: "..."
  initAt: "..."
backend:
  url: "https://orun-api...."
```

The config file is created with `0600` permissions. API tokens, session secrets, GitHub client secrets, and Orun access tokens are never written to disk by `orun backend`.
