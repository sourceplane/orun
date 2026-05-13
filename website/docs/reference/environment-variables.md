---
title: Environment variables
---

`orun` uses a small set of environment variables for configuration and runtime context.

## Variables that affect CLI behavior

| Variable | Meaning |
| --- | --- |
| `ORUN_CONFIG_DIR` | Default value for the global `--config-dir` legacy fallback |
| `ORUN_RUNNER` | Default runner for `orun run` |
| `ORUN_EXEC_ID` | Execution ID injected into `orun run`; useful in CI for stable cross-job traceability |
| `ORUN_PLAN_ID` | Plan reference injected into `orun run`; overrides the default `latest` resolution |
| `ORUN_NO_COLOR` | Disable ANSI color output (any non-empty value) |
| `ORUN_REMOTE_STATE` | Set to `true` to enable remote state coordination via orun-backend |
| `ORUN_BACKEND_URL` | URL of the orun-backend instance (required when `ORUN_REMOTE_STATE=true`) |
| `ORUN_TOKEN` | Explicit short-lived Orun machine token fallback for CI or automation. Normal local remote-state usage should use `orun auth login`, not a GitHub PAT |
| `GITHUB_ACTIONS` | Causes `run` to auto-select the GitHub Actions backend when set to `true` |
| `GITHUB_WORKSPACE` | Used as the default workdir for the GitHub Actions backend when `--workdir` is not set |

## Variables for CI auto-detection

These variables are read by `orun plan --from-ci` to build an event context and match triggers.

### GitHub Actions

| Variable | Purpose |
| --- | --- |
| `GITHUB_ACTIONS` | Set to `true` — signals GitHub Actions environment |
| `GITHUB_EVENT_NAME` | Event type (e.g., `push`, `pull_request`) |
| `GITHUB_REF` | Full git ref (e.g., `refs/heads/main`, `refs/tags/v1.0.0`) |
| `GITHUB_BASE_REF` | Base branch for pull requests |
| `GITHUB_HEAD_REF` | Head branch for pull requests |
| `GITHUB_SHA` | Head commit SHA |
| `GITHUB_ACTOR` | User or app that triggered the workflow |
| `GITHUB_REPOSITORY` | Full repository path (`owner/repo`) |

### GitLab CI

| Variable | Purpose |
| --- | --- |
| `GITLAB_CI` | Present when running in GitLab CI |
| `CI_PIPELINE_SOURCE` | Pipeline trigger source (`push`, `merge_request_event`, etc.) |
| `CI_MERGE_REQUEST_TARGET_BRANCH_NAME` | Target branch for merge requests |
| `CI_COMMIT_SHA` | Current commit SHA |
| `CI_COMMIT_BRANCH` | Branch name |
| `CI_COMMIT_TAG` | Tag name (when triggered by tag) |

### Buildkite

| Variable | Purpose |
| --- | --- |
| `BUILDKITE` | Present when running in Buildkite |
| `BUILDKITE_PIPELINE_DEFAULT_BRANCH` | Default branch for the pipeline |
| `BUILDKITE_BRANCH` | Current branch |
| `BUILDKITE_COMMIT` | Current commit SHA |
| `BUILDKITE_TAG` | Tag name (when triggered by tag) |

## Variables for self-hosted backend provisioning

These variables are used by `orun backend init`, `orun backend status`, and `orun backend destroy`.

| Variable | Description |
| --- | --- |
| `CLOUDFLARE_ACCOUNT_ID` | Cloudflare account ID (required for `orun backend` commands) |
| `CLOUDFLARE_API_TOKEN` | Cloudflare API token with Workers/D1/R2 edit permissions |
| `ORUN_SESSION_SECRET` | Orun session HMAC secret; generated securely if absent at init time |
| `GITHUB_CLIENT_ID` | GitHub OAuth app client ID for dashboard/CLI auth |
| `GITHUB_CLIENT_SECRET` | GitHub OAuth app client secret (never stored in config) |
| `ORUN_DASHBOARD_URL` | Dashboard URL for OAuth callback configuration |

No GitHub PAT is required for `orun backend` commands.

`NO_COLOR` (the standard) and `CLICOLOR=0` are also honored for disabling color output. When color is disabled, the context banner printed during auto-scoping uses plain text without ANSI codes.

## Variables injected during execution

| Variable | Meaning |
| --- | --- |
| `ORUN_CONTEXT` | Runtime environment label such as `local`, `container`, or `ci` |
| `ORUN_RUNNER` | Resolved runner name for the current step |
| `ORUN_PLAN_ID` | Plan checksum short-hash (injected into every step environment) |
| `ORUN_JOB_ID` | Job ID of the currently running job (e.g. `api@dev.deploy`) |
| `ORUN_JOB_RUN_ID` | Stable cross-job identifier: `{planID}:{execID}:{jobID}` |

## GitHub Actions compatibility mode

When the GitHub Actions backend is active, `orun` also supports standard GitHub Actions workflow command behavior such as `GITHUB_ENV`, `GITHUB_OUTPUT`, and `GITHUB_PATH` handling inside the compatibility engine.

Prefer CLI flags when you need per-command overrides, and reserve environment variables for CI defaults or workspace-wide configuration.

Most new workflows should declare composition sources in `intent.yaml` instead of relying on `ORUN_CONFIG_DIR`.
