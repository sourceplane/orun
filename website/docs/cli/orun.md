---
title: orun CLI
description: The orun command map, grouped by the four pillars of platform discipline as code (declare, package and evolve, ground agents, verify, plan and execute), with the global flags every command shares.
---

**Platform discipline as code.** You declare your platform's structure and
standards as intent (`intent.yaml`, `component.yaml`, compositions); you
package and evolve them like code (Stacks, presets, blueprints, baselines);
you ground your coding agents in them so they don't deviate (`mcp`, `skills`,
`agent`, the task plane); and a runner verifies the intent, compiles a plan,
and executes it (`validate`, `plan`, `run`). The cloud-client commands support
all four.

`orun` is one binary with one command tree. These pages are written against
the same code that runs, so `orun <command> --help` is always the
authoritative reference; the pages add examples, context, and the reasons
behind the flags.

Running `orun` with no arguments on an interactive terminal opens the
[cockpit TUI](./orun-tui.md). In a non-interactive shell, or with
`ORUN_NO_TUI=1`, it prints help instead.

## Command map

### 1 · Declare

| Command | Purpose |
| --- | --- |
| [`orun intent`](./orun-intent.md) | Explain and render the effective intent after presets and defaults |
| [`orun component`](./orun-component.md) | List components or inspect one merged component view |
| [`orun compositions`](./orun-compositions.md) | List, lock, pull, and package the composition sources an intent declares |
| [`orun work`](./orun-work.md) | Check and sync the work tree an intent declares (epics and task contracts) |

### 2 · Package and evolve

| Command | Purpose |
| --- | --- |
| [`orun new`](./orun-new.md) | Scaffold a component or instantiate a repository from a `kind: Blueprint`, and upgrade it later (`create`, `instantiate`) |
| [`orun baseline`](./orun-baseline.md) | The baseline registry: list, check, build here or on the platform, register, publish |
| [`orun pack`](./orun-pack.md) | Build a composition package archive without uploading it |
| [`orun publish`](./orun-publish.md) | Package and publish a Stack to an OCI registry |
| [`orun fetch`](./orun-fetch.md) | Download a Stack from an OCI registry |
| [`orun login`](./orun-login.md) | Authenticate to an OCI registry (not Orunbase; that is `orun auth login`) |

### 3 · Ground agents

| Command | Purpose |
| --- | --- |
| [`orun mcp`](./orun-mcp.md) | Serve the orun MCP: one agent surface over the pen and platform planes |
| [`orun skills`](./orun-skills.md) | Hosted agent playbooks: list the registry, pull native skill files |
| [`orun agent`](./orun-agent.md) | The agent runtime: types, base literacy, sessions, drivers, serve, attach, replay |
| [`orun task`](./orun-task.md) | Tasks, epics, and milestones: cloud-issued identity, repo-authored contracts, offline checks |
| [`orun spec`](./orun-spec.md) | Spec docs: repo-authored markdown annotated to epics, pushed with their commit |
| [`orun pr`](./orun-pr.md) | The provenance pen: open, preflight, and land task-carrying pull requests |
| [`orun githooks`](./orun-githooks.md) | Install the repository hooks that keep provenance effortless |

### 4 · Verify, plan, execute

| Command | Purpose |
| --- | --- |
| [`orun validate`](./orun-validate.md) | Verify: check the intent and its profile and dependency rules without planning |
| [`orun debug`](./orun-debug.md) | Print every compiler stage for an intent |
| [`orun plan`](./orun-plan.md) | Plan: check components against their schemas and compile a deterministic execution plan |
| [`orun run`](./orun-run.md) | Execute: run a compiled plan on the local, Docker, or GitHub Actions runner |
| [`orun workflow`](./orun-workflow.md) | Validate, digest, run, or view a standalone workflow file |
| [`orun approve`](./orun-approve.md) | Approve or reject a paused workflow step; list pending approvals |
| [`orun status`](./orun-status.md) | Show execution status for the latest or a named execution |
| [`orun logs`](./orun-logs.md) | Stream or filter step logs from an execution |
| [`orun describe`](./orun-describe.md) | Detailed view of a run, plan, job, component, revision, or trigger |
| [`orun get`](./orun-get.md) | List plans, runs, jobs, components, and environments |
| [`orun tui`](./orun-tui.md) | Open the cockpit TUI |
| [`orun tui-next`](./orun-tui-next.md) | Open the next-generation cockpit (preview) |
| [`orun github`](./orun-github.md) | Inspect GitHub Actions artifacts and workflow runs |
| [`orun catalog`](./orun-catalog.md) | Resolve, persist, push, and query the component catalog that drives change detection |
| [`orun objects`](./orun-objects.md) | Inspect, verify, push, pull, and garbage-collect the content-addressed object graph |
| [`orun gc`](./orun-gc.md) | Clean up old executions and orphan plans |

### Cloud client

| Command | Purpose |
| --- | --- |
| [`orun auth`](./orun-auth.md) | Sign in to Orunbase, check the session, print a token, sign out |
| [`orun workspace`](./orun-workspace.md) | Show, list, create, and choose the workspace you are working in (`ws`) |
| [`orun cloud`](./orun-cloud.md) | Link this repository to a workspace, check allow-listing, open the console |
| [`orun secrets`](./orun-secrets.md) | Manage secret values and metadata; values are write-only |
| [`orun integrations`](./orun-integrations.md) | Provider connections, scope templates, and brokered secrets |
| [`orun policy`](./orun-policy.md) | Manage and test the portable secret-access policy |
| [`orun backend`](./orun-backend.md) | Provision and manage a self-hosted backend on Cloudflare |

### Utility

| Command | Purpose |
| --- | --- |
| `orun version` | Print the version (same as `--version`) |
| `orun completion` | Generate shell completion for bash, zsh, fish, or PowerShell |
| `orun help` | Help about any command |

## Global flags

| Flag | Meaning |
| --- | --- |
| `--intent`, `-i` | Intent file path. Auto-discovered by walking up from the current directory to the git root when not set. |
| `--config-dir`, `-c` | Legacy fallback: a path or glob for folder-shaped job definitions, used when the intent declares no composition sources (also `ORUN_CONFIG_DIR`). Composition sources declared in the intent are the recommended path. |
| `--all` | Disable current-directory component scoping and process every component. |
| `--version`, `-v` | Print the CLI version. |
| `--help`, `-h` | Show help for any command. |

## Context-aware scoping

Run from inside a component directory and `orun` scopes to that component
and its dependencies automatically. `--all` overrides it. See
[context discovery](../concepts/context-discovery.md).

## Cloud commands and the workspace

Every command that talks to Orunbase runs against one workspace, resolved in
this order: `--workspace` (alias `--org`), then `ORUN_WORKSPACE` or
`ORUN_ORG`, then `execution.state.workspace` in `intent.yaml`, then this
repository's link, then the workspace selected with `orun workspace use`.
`ORUN_TOKEN` or `ORUN_TOKEN_FILE` replaces an interactive login on headless
machines. See [`orun workspace`](./orun-workspace.md).

## Typical flow

```bash
# 1 · declare: edit intent.yaml or a component.yaml, then see the effect
orun intent explain               # where each effective field came from
orun component network-foundation # one component's merged view

# 2 · package: pin the standards this repository uses
orun compositions lock

# 3 · ground agents: give a coding agent the same view of the platform
orun mcp serve

# 4 · verify, plan, execute
orun validate                     # the intent and its rules
orun plan                         # schemas, then a deterministic plan
orun run                          # execute it
orun status                       # how it went
orun logs --failed                # why, if it did not
```

## Exit codes

Most commands exit `0` on success and `1` on a failure they report. `orun
new` and `orun baseline new` exit `2` on a usage error and `6` on an
unparsable blueprint; `orun baseline check` exits non-zero when a provider
is missing. Command pages document anything beyond that.
