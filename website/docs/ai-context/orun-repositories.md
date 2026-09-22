---
title: AI context for Orun repositories
description: How a coding agent should reason about a repository that uses the Orun component model — what to read, which layer to change, the rules to keep, and the runtime surfaces (MCP, base literacy, agent types, task contracts, skills) it now has.
---

This page explains how AI agents should reason about repositories that use the Orun component model. It is written for application, platform, and infrastructure repos that consume Orun concepts, not for agents editing the Orun CLI source code itself.

## The model

An Orun repository is a desired-state component repository:

```text
intent.yaml + discovered component.yaml files + composition sources
        -> validated component instances
        -> compiled plan DAG
        -> explicit runtime execution
```

The main job of an AI agent is to preserve that separation. Components declare what they are. Compositions define the reusable validation and execution contract for each component type. The plan makes the resulting jobs, steps, profiles, paths, and dependencies reviewable before anything runs.

## What to read first

| File | Why it matters |
| --- | --- |
| `intent.yaml` | Repo-level planning boundary: discovery, environments, groups, composition sources, triggers, defaults, policies, and the workspace the repo declares. |
| `component.yaml` | Local ownership boundary for a component near its app, chart, package, or infrastructure code. |
| `stack.yaml` and `compositions/` | Versioned composition contracts: schemas, jobs, profiles, capabilities. |
| `.orun/compositions.lock.yaml` | Resolved composition source digests for reproducible planning. |
| `tasks/<KEY>.TaskContract.yaml` | What a task may touch (`affects`), when it is done (`doneWhen`), and which gates a merge must pass. Your ceiling, if you are working a task. |
| `agents/*.md` | The agent types this repo defines: a capability envelope (harness, model, tool policy, `mayAffect`, owner) plus a persona. |
| `policies/*.SecretPolicy.yaml` | Portable secret-access rules. Secrets are `secret://` references; you never see values. |
| Generated plan JSON | Concrete DAG evidence. Useful to inspect, not usually source to edit. |

## Safe AI workflow

Use non-destructive inspection first:

```bash
orun validate --intent intent.yaml
orun component --intent intent.yaml --long
orun compositions --intent intent.yaml
orun plan --intent intent.yaml --view dag
orun plan --intent intent.yaml --output /tmp/orun-plan.json
orun catalog affected --base main          # the blast radius of what you changed
orun task check <KEY> --base main          # offline: does the diff stay inside the contract?
```

If a command cannot run because local tools, credentials, or the Orun binary are missing, document the failed command and continue by inspecting files. Do not deploy, publish, apply Terraform, mutate clusters, or run cloud-affecting commands unless the user explicitly asks for that operational action.

## The runtime surfaces you have

Since the agent runtime landed, a coding agent working in an Orun repository has more than files to reason from.

**`orun mcp serve` is your tool surface.** One local MCP server composes two planes: the **pen** (`pr_open`, mounted when the server runs inside a checkout) and the **platform** (33 tools over the Orunbase API — catalog search, runs and logs, audit, access, secret metadata, skills, and the task plane — mounted when cloud auth resolves). Register it once (`claude mcp add orun -- orun mcp serve`) or let `orun agent run` write the config for you. `orun mcp tools` prints the roster; `orun mcp doctor` says why a plane did not mount. When the runtime launches you, the agent type's `tools` policy has already filtered that roster: denied tools are absent, `ask` tools raise an approval a human answers.

**`orun agent context` prints the base literacy.** It is the versioned document every agent type `extends` — what orun is, the catalog and affected engine, the shape of a brief, and the invariants below. It is pinned into your brief by content hash, so what you read from that command is exactly what the operator's orun version guarantees. Read it before reading any persona.

**`agents/*.md` are the agent types.** Frontmatter is the policy contract (`harness`, `model`, `tools.allow|ask|deny`, `mayAffect`, `secrets.use`, `owner`); the body is the persona and carries no policy weight. `orun agent lint agents/` validates a file; `orun agent import` seals it. A persona that restates orun mechanics is a lint warning — the literacy already covers them.

**Task contracts and the pen are how work is judged.** A task's status is derived from what the platform observes; there is no tool to set it. If you are working task `KEY`: run `orun githooks install` so every commit carries the `Orun-Task: KEY` trailer, work on a branch named `orun/KEY-<slug>` (or let `orun pr open --task KEY` create it), keep the diff inside the contract's `affects`, and open one PR per task. `orun pr check KEY` runs the same rules the platform's compliance check will.

**Skills are hosted playbooks.** `orun skills list` shows the registry — Sourceplane defaults shadowed by anything the workspace published under the same name — and `orun skills pull` writes each as a native `SKILL.md` with its pinned `orun-rev`. Agents read the same revisions through `skills_list` and `skill_get` on the MCP; the revisions you ran under are recorded in the PR manifest.

## Choose the right layer

| Change | Preferred layer |
| --- | --- |
| Add or update a deployable/operable unit | `component.yaml` |
| Add an environment or trigger activation | `intent.yaml` |
| Share values across many components | Environment or group defaults |
| Enforce constraints | Group, environment, or profile policies |
| Add a typed input | `ComponentSchema` |
| Change reusable execution steps | `JobTemplate` |
| Change PR, verify, release, or deploy behavior | `ExecutionProfile` |
| Change ordering | `dependsOn` |
| Change what an agent type may do or touch | `agents/<name>.md` frontmatter (`tools`, `mayAffect`, `secrets.use`) |
| Change what a task may touch or when it is done | `tasks/<KEY>.TaskContract.yaml`, then `orun task attach KEY` |
| Change who may resolve which secret | `policies/*.SecretPolicy.yaml` |

## Rules for AI agents

- Do not treat Orun as a generic CI script runner.
- Do not bypass `intent.yaml` for repo-level behavior.
- Do not duplicate composition logic inside component directories.
- Do not hide environment behavior in shell when a profile, default, policy, or subscription should express it.
- Do not manually patch generated plans.
- Prefer typed component inputs plus schema support over ad-hoc variables.
- Always validate and inspect the DAG after meaningful changes.
- Do not try to assert progress. There is no status-write tool; push the branch, open the PR, let the gates run.
- Stay inside the blast radius: the contract's `affects` and the agent type's `mayAffect` are ceilings. If the work truly needs more, say so and stop; never widen scope silently.
- One task, one branch, one PR, with the `Orun-Task` trailer on every commit.
- Secrets are references, never content. Never write a secret value into code, a commit, a comment, or a transcript.
- When unsure whether something is affected, in scope, or safe, over-report. Orun's own engines do.

## Reusable context pack

This repository includes a copyable AI context pack under `context-for-ai/` with a deeper playbook:

- `context-for-ai/README.md`
- `context-for-ai/00-orun-repo-philosophy.md`
- `context-for-ai/01-repo-analysis-playbook.md`
- `context-for-ai/02-intent-and-component-model.md`
- `context-for-ai/03-compositions-and-execution-contracts.md`
- `context-for-ai/04-development-and-testing-workflow.md`
- `context-for-ai/05-ai-agent-operating-rules.md`

Use that pack when onboarding an AI agent to a repo implemented with Orun component concepts.

## Related

- [The agent runtime](../concepts/agent-runtime.md) — agent types, briefs, sessions, drivers
- [The task plane](../concepts/task-plane.md) — contracts, the verdict ladder, the pen
- [`orun mcp`](../cli/orun-mcp.md) — every tool, plane by plane
- [`orun agent`](../cli/orun-agent.md) — the runtime's commands
- [`orun skills`](../cli/orun-skills.md) — list and pull hosted playbooks
