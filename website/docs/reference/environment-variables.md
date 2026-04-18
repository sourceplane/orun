---
title: Environment variables
---

`arx` uses a small set of environment variables for configuration and runtime context.

## Variables that affect CLI behavior

| Variable | Meaning |
| --- | --- |
| `ARX_CONFIG_DIR` | Default value for the global `--config-dir` flag |
| `ARX_RUNNER` | Default runner for `arx run` |
| `GITHUB_ACTIONS` | Causes `run` to auto-select the GitHub Actions backend when set to `true` |
| `GITHUB_WORKSPACE` | Used as the default workdir for the GitHub Actions backend when `--workdir` is not set |

## Variables injected during execution

| Variable | Meaning |
| --- | --- |
| `ARX_CONTEXT` | Runtime environment label such as `local`, `container`, or `ci` |
| `ARX_RUNNER` | Resolved runner name for the current step |

## GitHub Actions compatibility mode

When the GitHub Actions backend is active, `arx` also supports standard GitHub Actions workflow command behavior such as `GITHUB_ENV`, `GITHUB_OUTPUT`, and `GITHUB_PATH` handling inside the compatibility engine.

Prefer CLI flags when you need per-command overrides, and reserve environment variables for CI defaults or workspace-wide configuration.