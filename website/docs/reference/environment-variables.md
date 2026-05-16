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
| `ORUN_ENVIRONMENT` | Environment name for the current job (e.g. `dev`, `production`) |
| `ORUN_COMPONENT` | Component name for the current job (e.g. `api-platform`) |

## User-declared environment variables

You can declare environment variables at four levels in your configuration. These are resolved at plan time and injected into jobs at runtime. User-declared env keys must not use the reserved `ORUN_` prefix.

### Intent root-level env

```yaml
# intent.yaml
env:
  OWNER: sourceplane
  ORGANIZATION: sourceplane
```

Global env vars shared across all environments and components (lowest precedence).

### Intent environment-level env

```yaml
environments:
  dev:
    env:
      AWS_REGION: us-east-1
      TF_LOG: WARN
```

### Component root-level env

```yaml
# component.yaml
spec:
  env:
    REPO: aws-admin
    SERVICE: github-iam
```

Component-wide env vars applied across all subscribed environments.

### Component subscription-level env

```yaml
subscribe:
  environments:
    - name: dev
      env:
        STACK_NAME: api-platform
        TF_VAR_replicas: "1"
```

Subscription env values override all lower-precedence levels. The full merge order from lowest to highest is: intent root → environment → component root → subscription. See [Runtime environment](/concepts/runtime-environment) for full merge semantics.

## GitHub Actions compatibility mode

When the GitHub Actions backend is active, `orun` also supports standard GitHub Actions workflow command behavior such as `GITHUB_ENV`, `GITHUB_OUTPUT`, and `GITHUB_PATH` handling inside the compatibility engine.

Prefer CLI flags when you need per-command overrides, and reserve environment variables for CI defaults or workspace-wide configuration.

Most new workflows should declare composition sources in `intent.yaml` instead of relying on `ORUN_CONFIG_DIR`.
