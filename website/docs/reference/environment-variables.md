---
title: Environment variables
---

`gluon` uses a small set of environment variables for configuration and runtime context.

## Variables that affect CLI behavior

| Variable | Meaning |
| --- | --- |
| `GLUON_CONFIG_DIR` | Default value for the global `--config-dir` legacy fallback |
| `GLUON_RUNNER` | Default runner for `gluon run` |
| `GLUON_EXEC_ID` | Execution ID injected into `gluon run`; useful in CI for stable cross-job traceability |
| `GLUON_PLAN_ID` | Plan reference injected into `gluon run`; overrides the default `latest` resolution |
| `GLUON_NO_COLOR` | Disable ANSI color output (any non-empty value) |
| `GITHUB_ACTIONS` | Causes `run` to auto-select the GitHub Actions backend when set to `true` |
| `GITHUB_WORKSPACE` | Used as the default workdir for the GitHub Actions backend when `--workdir` is not set |

`NO_COLOR` (the standard) and `CLICOLOR=0` are also honored for disabling color output. When color is disabled, the context banner printed during auto-scoping uses plain text without ANSI codes.

## Variables injected during execution

| Variable | Meaning |
| --- | --- |
| `GLUON_CONTEXT` | Runtime environment label such as `local`, `container`, or `ci` |
| `GLUON_RUNNER` | Resolved runner name for the current step |

## GitHub Actions compatibility mode

When the GitHub Actions backend is active, `gluon` also supports standard GitHub Actions workflow command behavior such as `GITHUB_ENV`, `GITHUB_OUTPUT`, and `GITHUB_PATH` handling inside the compatibility engine.

Prefer CLI flags when you need per-command overrides, and reserve environment variables for CI defaults or workspace-wide configuration.

Most new workflows should declare composition sources in `intent.yaml` instead of relying on `GLUON_CONFIG_DIR`.
