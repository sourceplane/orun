# AI context for orun repositories

orun is platform discipline as code: a repository declares its platform's structure and
standards as intent, and a coding agent working in it should be grounded in those
declarations instead of inferring conventions from nearby code. This directory is a
reusable context pack that teaches an agent to do that.

The canonical narrative is the docs page
[Agents in orun repositories](https://orun-docs.pages.dev/ai-context/orun-repositories);
this pack is the same guidance in a form you can copy into another repository.

It is not a guide to modifying the Orun CLI source code. It explains how to understand and safely change an application, platform, or infrastructure repository that uses `intent.yaml`, discovered `component.yaml` files, composition sources, profiles, dependencies, and compiled plans.

## Reading order

1. Read [00-orun-repo-philosophy.md](00-orun-repo-philosophy.md) to get the mental model.
2. Read [01-repo-analysis-playbook.md](01-repo-analysis-playbook.md) before making any edits in a target repo.
3. Read [02-intent-and-component-model.md](02-intent-and-component-model.md) to understand the control-plane files.
4. Read [03-compositions-and-execution-contracts.md](03-compositions-and-execution-contracts.md) before changing component types, schemas, jobs, or profiles.
5. Read [04-development-and-testing-workflow.md](04-development-and-testing-workflow.md) before implementing changes.
6. Keep [05-ai-agent-operating-rules.md](05-ai-agent-operating-rules.md) open as the checklist for day-to-day AI work.
7. Read [06-standards-presets-and-agent-guardrails.md](06-standards-presets-and-agent-guardrails.md) for which standards orun enforces, how presets and Stacks change them, and the guardrails around agent work (MCP, agent types, task contracts).

## Core idea

An Orun repo is a desired-state component repo. The important boundary is:

```text
intent.yaml + component.yaml files + composition sources
        -> compiled, deterministic plan DAG
        -> explicit runtime execution
```

Do not treat an Orun repo as a loose collection of CI scripts. Components declare what they are. Compositions define how a type is validated and executed. The plan is the reviewable compiled artifact where implicit behavior becomes explicit.

## Safe workflow for AI agents

Use this loop for most changes:

```bash
orun validate --intent intent.yaml
orun component --intent intent.yaml --long
orun compositions --intent intent.yaml
orun plan --intent intent.yaml --view dag
orun plan --intent intent.yaml --output /tmp/orun-plan.json
```

If `orun` is not available, inspect the files directly and document the missing command. Do not deploy, apply Terraform, publish packages, push OCI artifacts, or mutate cloud resources unless the user explicitly asks for that operational action.

## What this pack should help an AI do

- Recognize `intent.yaml` as the repo-level desired-state control plane.
- Treat each `component.yaml` as an ownership boundary for one deployable or operable unit.
- Preserve composability by changing typed component parameters and compositions instead of adding hidden scripts.
- Understand environment subscriptions, domains, defaults, profiles, dependencies, trigger bindings, and inherited presets.
- Know which standards orun enforces and stay inside them.
- Validate changes by inspecting the component view and plan DAG before execution.
- Explain changes in terms of component contracts, not only files edited.

