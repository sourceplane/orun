# Standards, presets, and agent guardrails

An orun repository writes its standards down. Before changing anything, know which ones
orun enforces, where inherited ones come from, and which guardrails apply to you as an
agent.

## Which standards orun enforces

| Standard | Declared in | What fails if you break it |
| --- | --- | --- |
| A component's parameters match its type's schema | `ComponentSchema` in the composition | `orun plan` |
| A component's `type` resolves to a declared composition | `component.yaml`, `intent.yaml` `compositions.sources` | `orun plan` |
| A subscription names a profile the composition offers | `ExecutionProfile`, `subscribe.environments[].profile` | `orun plan` |
| Profile and dependency rules are well formed | `profileRules`, dependency rules | `orun validate` |
| No dependency cycles | `dependsOn` | `orun plan` |
| Secret slots hold `secret://` references, never values | `secretEnv`, `optionalSecretEnv` | `orun plan` |
| User `env` does not use the reserved `ORUN_` prefix | any `env` block | `orun validate`, `orun plan` |
| A pinned source resolves to its digest | `digest:` on a source in `intent.yaml` | `orun plan` |
| A task branch stays inside its contract | `affects` in `tasks/<KEY>.TaskContract.yaml` | `orun task check <KEY>` |

Recorded but **not enforced**: group, environment, and execution-profile `policies`;
cross-plan promotion gates; `condition` on a component `dependsOn`; a composition's
`lifecycle`; an agent type's `mayAffect`. Respect them as stated intent, but do not tell a
user they are guardrails.

Run `orun validate` and `orun plan` after every meaningful change. `orun validate` does not
load compositions or apply presets, so only `orun plan` checks schemas and the effective
intent.

## Inherited standards: presets and Stacks

`intent.yaml` may not be the whole story. If it has an `extends:` list, platform rules are
inherited from presets published inside a Stack:

```yaml
compositions:
  sources:
    - name: platform
      kind: oci
      ref: oci://ghcr.io/acme/platform-stack:v1.4.0
extends:
  - source: platform
    preset: github-actions
```

- Run `orun intent explain` to see every field a preset contributed, and
  `orun intent render` to see the effective intent the planner uses.
- Presets only fill in what the repository leaves out, in `extends:` order: the
  repository wins, then the first preset. Change a repository value in `intent.yaml`;
  change a shared rule in the Stack, not by copying it into this repository.
- Golden paths come from composition sources. To change one for every repository, change
  the Stack and publish a new version; to adopt a new version here, change the source's
  `ref` (and `digest:`, if pinned) and review the plan diff.

## Guardrails for agent work

- **Your tools are filtered.** When `orun agent` launches you, the agent type's
  `tools.allow`, `tools.ask`, and `tools.deny` have already filtered the MCP roster:
  denied tools are absent, and `ask` tools wait for a human.
- **Ask the platform instead of guessing.** `orun mcp serve` exposes the catalog (what
  exists, who owns it, what depends on what), runs and logs, skills, and the task plane.
  `orun catalog affected --base main` gives the blast radius of a change.
- **Read the base literacy.** `orun agent context` prints the versioned document every
  agent type extends; it is pinned into your brief by hash.
- **Work one task at a time.** Branch `orun/<KEY>-<slug>`, the `Orun-Task: <KEY>` trailer
  on every commit (`orun githooks install`), one pull request per task. `orun pr check`
  runs the same provenance rules the platform's compliance check does.
- **Stay inside the contract.** `orun task check <KEY> --base main` fails when the diff
  touches a component outside the contract's `affects`. If the work genuinely needs more,
  stop and say so.
- **Never assert progress.** There is no tool to set a task's status; it is derived from
  branches, pull requests, merges, and gates.
