---
title: Environment variables
description: Every ORUN_* variable the CLI reads, grouped by area, with where it is read and what it does — plus the variables orun injects into your steps.
---

`orun` reads a defined set of environment variables. Prefer CLI flags for per-command overrides and reserve environment variables for CI defaults or machine-wide configuration. Each table below names the source file that reads the variable.

## Core CLI behavior

| Variable | Read in | Meaning |
| --- | --- | --- |
| `ORUN_CONFIG_DIR` | `cmd/orun/commands_root.go` | Default value for the global `--config-dir` legacy fallback |
| `ORUN_RUNNER` | `cmd/orun/commands_root.go`, `command_run.go` | Default execution backend for `orun run` (`local`, `docker`, `github-actions`); a `--runner` flag wins |
| `ORUN_EXEC_ID` | `cmd/orun/commands_root.go`, `command_status.go`, `command_logs.go` | Execution ID for `orun run`, and the run that `status --remote-state` / `logs --remote-state` inspect; auto-generated for `run` when unset |
| `ORUN_PLAN_ID` | `cmd/orun/commands_root.go`, `command_run.go` | Plan reference injected into `orun run`; overrides the default `latest` resolution |
| `ORUN_GITHUB_API_URL` | `internal/flow/remote.go` | Override the GitHub API base used to fetch [remote workflow refs](../cli/orun-workflow.md#remote-references) (GitHub Enterprise; default `https://api.github.com`) |
| `ORUN_ARTIFACT_RETENTION_DAYS` | `internal/artifactstore/github/upload.go` | Retention in days for artifacts uploaded with `--artifact github` (default `14`) |
| `ORUN_SECRET_<KEY>` | `cmd/orun/run_secrets.go` | Local-run override for the `secret://` reference whose key is `<KEY>`. Local runs resolve secrets only from these overrides and fail closed when one is missing; runs against Orunbase resolve from the platform instead |
| `ORUN_COORDINATION` | `cmd/orun/command_run.go` | Set to `legacy` to fall back to the retired relational coordination path. Native event-sourced coordination is the default for remote-state runs |
| `ORUN_NO_TUI` | `cmd/orun/commands_root.go` | When truthy, a bare `orun` prints help instead of opening the cockpit (subcommands are unaffected) |
| `ORUN_TUI` | `cmd/orun/command_tui.go` | Set to `next` to open cockpit v2 from `orun tui`, bare `orun`, and bare `orun agent` — the same opt-in as `orun tui-next` / `--next` |
| `ORUN_NO_AUTO_REFRESH` | `cmd/orun/refresh_hook.go` | When truthy, disables the universal pre-command object-model catalog refresh hook |
| `ORUN_OBJECT_ZSTD_LEVEL` | `internal/objectstore/local.go` | Advanced: zstd compression level for objects written to the local object store (default `3`) |

## Colour and terminal detection

| Variable | Read in | Meaning |
| --- | --- | --- |
| `ORUN_NO_COLOR` | `internal/ui/ansi.go` | Disable ANSI colour output (any non-empty value). `orun run --background` sets it on the detached child |
| `NO_COLOR` | `internal/ui/ansi.go` | The standard opt-out; any non-empty value disables colour. Glyphs are never stripped |
| `CLICOLOR` | `internal/ui/ansi.go` | `CLICOLOR=0` also disables colour |
| `CI`, `GITHUB_ACTIONS`, `GITLAB_CI`, `CIRCLECI`, `BUILDKITE`, `JENKINS_URL` | `internal/ui/ansi.go` | Any of these set (and not `false`) marks the process as running in CI: `IsInteractiveWriter` reports false, so interactive rendering is switched off even on a TTY |

## Cloud and authentication

| Variable | Read in | Meaning |
| --- | --- | --- |
| `ORUN_REMOTE_STATE` | `cmd/orun/commands_root.go`, `command_run.go` | Set to `true` to coordinate the run through the Orunbase state backend. `orun run --remote-state` sets it |
| `ORUN_BACKEND_URL` | `cmd/orun/commands_root.go`, `internal/actions/cloud.go`, `internal/tui2/data/cloud.go` | Backend URL — Orunbase or a self-hosted backend. Precedence: `--backend-url` flag > this variable > `intent.yaml` `execution.state.backendUrl` > `~/.orun/config.yaml` `cloud.url` > the built-in default |
| `ORUN_TOKEN` | `cmd/orun/command_cloud.go`, `internal/remotestate/auth.go` | A headless bearer token for CI and automation. Honoured by every auth path, including `orun cloud link` and `orun cloud check`, so a container can self-link without a browser login. Interactive use should rely on `orun auth login` instead |
| `ORUN_TOKEN_FILE` | `cmd/orun/command_cloud.go`, `internal/remotestate/auth.go` | Path to a file whose content is the headless bearer token. **The file wins when both are set**: it is the agent runtime's refreshed token channel (the platform rotates the session credential roughly every 15 minutes), while an `ORUN_TOKEN` copied from it expires. `orun` prints a one-line warning to stderr when both are set and differ |
| `ORUN_WORKSPACE` | `cmd/orun/commands_root.go`, `internal/actions/cloud.go` | Workspace scope for remote state — a `ws_…` ID, slug, or `org_…` ID. Overrides the linked workspace and the one selected with [`orun workspace use`](../cli/orun-workspace.md). Equivalent to `--workspace` |
| `ORUN_ORG` | `cmd/orun/commands_root.go`, `internal/actions/cloud.go` | Retained alias of `ORUN_WORKSPACE`; the CLI reads either and prefers `ORUN_WORKSPACE`. Equivalent to `--org` |
| `ORUN_PROJECT` | `cmd/orun/commands_root.go`, `internal/tui2/data/cloud.go` | Project scope for remote state — overrides the linked project. Equivalent to `--project` |
| `ORUN_CREDENTIAL_STORE` | `internal/cliauth/storage.go` | Which credential store holds the login: `file` forces `~/.orun/credentials.json`, `keychain` forces the OS keychain (macOS only, skipping the usability probe), empty or `auto` picks the keychain when it works and the file otherwise |
| `ORUN_SESSION` | `cmd/orun/pr.go`, `cmd/orun/mcp_serve.go` | Default agent session ID written into the provenance manifest by `orun pr` and the MCP `pr_open` tool |
| `ORUN_VERBOSE` | `cmd/orun/refresh_hook.go` | When truthy, surfaces best-effort outcomes that are otherwise silent — the pre-command catalog refresh and `autopushCatalog` skips |

`ORUN_TOKEN` and `ORUN_TOKEN_FILE` take precedence over a stored session for the headless paths, but an operation that needs a stored session or a membership listing (for example resolving identity through a workspace-scoped key) fails with a message that says so.

## Agent runtime and sessions

`orun agent serve` is the in-sandbox entrypoint the platform launches; the identity below is seeded by the platform and read by `cmd/orun/command_agent_serve.go` unless a flag overrides it.

| Variable | Read in | Meaning |
| --- | --- | --- |
| `ORUN_CLOUD_API` | `cmd/orun/command_agent_serve.go`, `command_agent_live.go`, `build_events.go` | The api-edge base URL the session body dials home to; `orun agent attach` uses it with `ORUN_ORG_ID` to reach a remote session |
| `ORUN_ORG_ID` | same | The workspace (`org_…`) ID of the session |
| `ORUN_SESSION_ID` | `cmd/orun/command_agent_serve.go`, `build_events.go` | The `as_…` session ID (overridable with `--session`) |
| `ORUN_SESSION_TOKEN` | `cmd/orun/command_agent_serve.go`, `command_agent_live.go` | The session bearer — the service-principal credential |
| `ORUN_AGENT_TYPE` | `cmd/orun/command_agent_serve.go` | The agent type to serve (default for `--type`). Without one the tool policy is deny-by-default |
| `ORUN_TASK_KEY` | `cmd/orun/command_agent_serve.go` | The task key the session works (default for `--task`) |
| `ORUN_REPO_REMOTE` | `internal/agent/ground/ground.go` | The https clone URL that grounds the session in a repository; never carries a credential. Absent, the session boots ungrounded |
| `ORUN_REPO_FULL_NAME` | `internal/agent/ground/ground.go`, `ground/token.go` | The `owner/repo` full name — the only repository the session may mint a credential for |
| `ORUN_REPO_REF` | `internal/agent/ground/ground.go` | The branch or tag to clone at |

## Baseline build

The bootstrap driver (`internal/agent/driver/bootstrap.go`) reads its inputs from the environment when a session is asked to build a product from a baseline.

| Variable | Read in | Meaning |
| --- | --- | --- |
| `ORUN_BASELINE_ID` | `internal/agent/driver/bootstrap.go` | The baseline to instantiate, for example `cirrus` or `cirrus@baseline-v6` |
| `ORUN_BASELINE_OUT` | `internal/agent/driver/bootstrap.go` | Where to place the product; `orun agent serve` sets it to the session's working directory |
| `ORUN_BASELINE_VALUES` | `internal/agent/driver/bootstrap.go` | Path to a `--values` JSON file |

## Debugging

| Variable | Read in | Meaning |
| --- | --- | --- |
| `ORUN_DEBUG` | `cmd/orun/command_integrations_dynamic.go` | Any non-empty value prints integration-renderer diagnostics (for example a shadowed native extension) to stderr |
| `ORUN_TUI_PROFILE` | `internal/tui/profile.go`, `internal/tui2/frame/profile.go` | Path of an NDJSON file the cockpit writes per-frame timings to (`Update` and `View` durations, rendered bytes). When unset the profiler is not installed |
| `ORUN_BACKGROUND_CHILD` | `cmd/orun/command_run_background.go` | Internal marker set on the child that `orun run --background` re-execs, so it does not detach itself again |

## GitHub Actions

| Variable | Read in | Meaning |
| --- | --- | --- |
| `GITHUB_ACTIONS` | `cmd/orun/command_run.go`, `internal/ci/detect.go`, `internal/remotestate/runid.go` | When `true`, `orun run` auto-selects the GitHub Actions backend, CI refs are detected from the event payload, and the remote run ID is derived as `gha_{GITHUB_RUN_ID}_{GITHUB_RUN_ATTEMPT}` |
| `GITHUB_WORKSPACE` | `cmd/orun/command_run.go` | Default workdir for the GitHub Actions backend when `--workdir` is not set |
| `GITHUB_TOKEN`, `GH_TOKEN` | `internal/artifactstore/github/client.go` | GitHub token for the artifact API (`GH_TOKEN` is the fallback) |
| `ACTIONS_RUNTIME_TOKEN`, `ACTIONS_RESULTS_URL` | `internal/artifactstore/github/upload.go` | Runtime token and results URL for `@actions/artifact` upload; set automatically on GitHub-hosted runners |

## Self-hosted backend provisioning

These variables are read by `orun backend init`, `orun backend status`, and `orun backend destroy` (`cmd/orun/command_backend.go`).

| Variable | Meaning |
| --- | --- |
| `CLOUDFLARE_ACCOUNT_ID` | Cloudflare account ID (required) |
| `CLOUDFLARE_API_TOKEN` | Cloudflare API token with Workers/D1/R2 edit permissions |
| `ORUN_SESSION_SECRET` | Session HMAC secret for the Worker; generated securely if absent at init time and never stored in config |
| `ORUN_PUBLIC_URL` | Public URL for the Worker; inferred from `workers.dev` if omitted (also `--public-url`) |
| `ORUN_DASHBOARD_URL` | Dashboard URL for OAuth callback configuration (also `--dashboard-url`) |
| `GITHUB_CLIENT_ID` | GitHub OAuth app client ID for dashboard/CLI auth |
| `GITHUB_CLIENT_SECRET` | GitHub OAuth app client secret (never stored in config) |

## Variables injected during execution

The runner (`internal/runner/runner.go`) exports these into every step's environment. User-declared env keys must not use the reserved `ORUN_` prefix.

| Variable | Meaning |
| --- | --- |
| `ORUN_CONTEXT` | Runtime environment label such as `local`, `container`, or `ci` |
| `ORUN_RUNNER` | Resolved runner name for the current step |
| `ORUN_EXEC_ID` | Execution ID of the current run |
| `ORUN_PLAN_ID` | Plan checksum short-hash |
| `ORUN_JOB_ID` | Job ID of the currently running job (e.g. `api@dev.deploy`) |
| `ORUN_JOB_UID` | Content-addressed UID of the current job — stable across runs while the job's inputs are unchanged |
| `ORUN_JOB_RUN_ID` | Stable per-job identifier: `{execID}/{jobUID}` |
| `ORUN_ENVIRONMENT` | Environment name for the current job (e.g. `dev`, `production`) |
| `ORUN_COMPONENT` | Component name for the current job (e.g. `api-platform`) |
| `ORUN_ENV` | Path to the env file for persisting environment variables across steps (alias for `GITHUB_ENV`) |
| `ORUN_SECRET_OUTPUTS` | Path of the per-job sink file for [job output secrets](../concepts/secrets.md#secretoutputs--job-output-secrets) — exported only when the job declares `secretOutputs` |
| `TF_HTTP_ADDRESS`, `TF_HTTP_LOCK_ADDRESS`, `TF_HTTP_UNLOCK_ADDRESS`, `TF_HTTP_LOCK_METHOD`, `TF_HTTP_UNLOCK_METHOD`, `TF_HTTP_USERNAME`, `TF_HTTP_PASSWORD`, `TF_HTTP_RETRY_MAX` | Terraform `http` state backend wiring, exported to component-job steps on remote runs — see [Terraform state on the platform](../execute/terraform-state.md) |
| `ORUN_FLOW_SOURCE_REPO`, `ORUN_FLOW_SOURCE_REF`, `ORUN_FLOW_SOURCE_SHA`, `ORUN_FLOW_SOURCE_URL` | Provenance of a [remotely-fetched workflow](../cli/orun-workflow.md#remote-references), exported to every workflow `run:` step (empty for local files) |

## User-declared environment variables

You can declare environment variables at four levels in your configuration. These are resolved at plan time and injected into jobs at runtime.

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

Subscription env values override all lower-precedence levels. The full merge order from lowest to highest is: intent root → environment → component root → subscription. See [Runtime environment](../concepts/runtime-environment.md) for full merge semantics.

## GitHub Actions compatibility mode

When the GitHub Actions backend is active, `orun` also supports standard GitHub Actions workflow command behavior such as `GITHUB_ENV`, `GITHUB_OUTPUT`, and `GITHUB_PATH` handling inside the compatibility engine.

`ORUN_ENV` is always set as an alias for `GITHUB_ENV`, pointing to the same file. Writing `KEY=VALUE` pairs to either `$ORUN_ENV` or `$GITHUB_ENV` has the same effect — the variables become available to all subsequent steps in the job.

```bash
# Both of these are equivalent:
echo "MY_VAR=hello" >> "$ORUN_ENV"
echo "MY_VAR=hello" >> "$GITHUB_ENV"
```

When running inside actual GitHub Actions (detected via `GITHUB_ENV` in the host environment), the runner sets `ORUN_ENV` to the same path, so compositions work identically under the orun compatibility engine and on a GitHub-hosted runner.

## Related

- [Configuration](./configuration.md) — `intent.yaml`, `~/.orun/config.yaml`, and the credential store.
- [`orun login`](../cli/orun-login.md) and [`orun auth`](../cli/orun-auth.md) — interactive sessions versus `ORUN_TOKEN`.
- [`orun agent`](../cli/orun-agent.md) — the runtime that consumes the session variables.
- [Secrets](../concepts/secrets.md) — `secret://` references, `ORUN_SECRET_<KEY>` overrides, and `secretOutputs`.
