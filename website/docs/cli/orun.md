---
title: orun CLI
---

The root `orun` command is the entry point for planning, inspection, and execution.

## Command map

| Command | Purpose |
| --- | --- |
| `orun plan` | Compile intent and compositions into a deterministic execution plan |
| `orun run` | Execute a compiled plan (executes by default; use `--dry-run` to preview) |
| `orun auth` | Manage the local Orun CLI session for remote state |
| `orun cloud` | Link the current GitHub repo for local remote-state runs |
| `orun backend` | Provision, inspect, and remove a self-hosted Orun backend on Cloudflare |
| `orun status` | Show execution status for the latest or a specific execution |
| `orun logs` | Stream or filter per-step logs from an execution |
| `orun get` | List resources: `plans`, `runs`, `jobs`, `components`, `environments` |
| `orun describe` | Show detailed information for a run, plan, job, or component |
| `orun gc` | Clean up old executions and orphan plan files |
| `orun validate` | Validate intent and discovered components against schemas |
| `orun debug` | Inspect intent processing and planning internals |
| `orun compositions` | List or inspect available compositions |
| `orun component` | List components or inspect a merged component view |
| `orun completion` | Generate shell completion scripts |

## Global flags

| Flag | Meaning |
| --- | --- |
| `--intent`, `-i` | Intent file path (auto-discovered from CWD if not set) |
| `--config-dir`, `-c` | Legacy fallback path or glob for folder-shaped compositions |
| `--all` | Disable CWD-based component scoping; process all components |
| `--version` | Print the CLI version |
| `--help` | Show command help |

`--intent` auto-discovers `intent.yaml` by walking up the directory tree to the git root. Pass it explicitly to override.

`--config-dir` can also be set through `ORUN_CONFIG_DIR`, but packaged composition sources declared in the intent are the recommended path.

## Typical flow

```bash
# Plan from anywhere in the repo
orun plan

# Inspect what was planned
orun get jobs
orun status

# Execute
orun run

# Review logs if needed
orun logs
```

When you run commands from inside a component directory, `orun` automatically scopes to that component and its dependencies. Use `--all` to override scoping. See [context-aware discovery](../concepts/context-discovery.md) for details.

Read the command-specific pages next if you need examples and flag details.
