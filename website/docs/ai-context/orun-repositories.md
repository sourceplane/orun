---
title: AI context for Orun repositories
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
| `intent.yaml` | Repo-level planning boundary: discovery, environments, groups, composition sources, triggers, defaults, policies. |
| `component.yaml` | Local ownership boundary for a component near its app, chart, package, or infrastructure code. |
| `stack.yaml` and `compositions/` | Versioned composition contracts: schemas, jobs, profiles, capabilities. |
| `.orun/compositions.lock.yaml` | Resolved composition source digests for reproducible planning. |
| Generated plan JSON | Concrete DAG evidence. Useful to inspect, not usually source to edit. |

## Safe AI workflow

Use non-destructive inspection first:

```bash
orun validate --intent intent.yaml
orun component --intent intent.yaml --long
orun compositions --intent intent.yaml
orun plan --intent intent.yaml --view dag
orun plan --intent intent.yaml --output /tmp/orun-plan.json
```

If a command cannot run because local tools, credentials, or the Orun binary are missing, document the failed command and continue by inspecting files. Do not deploy, publish, apply Terraform, mutate clusters, or run cloud-affecting commands unless the user explicitly asks for that operational action.

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

## Rules for AI agents

- Do not treat Orun as a generic CI script runner.
- Do not bypass `intent.yaml` for repo-level behavior.
- Do not duplicate composition logic inside component directories.
- Do not hide environment behavior in shell when a profile, default, policy, or subscription should express it.
- Do not manually patch generated plans.
- Prefer typed component inputs plus schema support over ad-hoc variables.
- Always validate and inspect the DAG after meaningful changes.

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

