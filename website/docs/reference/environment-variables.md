---
title: Environment variables
---

`arx` uses a small set of environment variables for configuration and runtime context.

## Variables that affect CLI behavior

| Variable | Meaning |
| --- | --- |
| `ARX_CONFIG_DIR` | Default value for the global `--config-dir` flag |
| `CIZ_CONFIG_DIR` | Deprecated alias for `ARX_CONFIG_DIR` |
| `LITECI_CONFIG_DIR` | Deprecated alias for `ARX_CONFIG_DIR` |
| `ARX_RUNNER` | Default runner for `arx run` |
| `CIZ_RUNNER` | Deprecated alias for `ARX_RUNNER` |
| `LITECI_RUNNER` | Deprecated alias for `ARX_RUNNER` |
| `GITHUB_ACTIONS` | Causes `run` to auto-select the GitHub Actions backend when set to `true` |
| `GITHUB_WORKSPACE` | Used as the default workdir for the GitHub Actions backend when `--workdir` is not set |

## Variables injected during execution

| Variable | Meaning |
| --- | --- |
| `ARX_CONTEXT` | Runtime environment label such as `local`, `container`, or `ci` |
| `ARX_RUNNER` | Resolved runner name for the current step |
| `CIZ_CONTEXT` | Deprecated compatibility alias |
| `CIZ_RUNNER` | Deprecated compatibility alias |
| `LITECI_CONTEXT` | Deprecated compatibility alias |
| `LITECI_RUNNER` | Deprecated compatibility alias |

## GitHub Actions compatibility mode

When the GitHub Actions backend is active, `arx` also supports standard GitHub Actions workflow command behavior such as `GITHUB_ENV`, `GITHUB_OUTPUT`, and `GITHUB_PATH` handling inside the compatibility engine.

Prefer CLI flags when you need per-command overrides, and reserve environment variables for CI defaults or workspace-wide configuration.