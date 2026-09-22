---
title: Baselines
description: A baseline is a complete product repository the platform can rebuild under your name — registered in a catalogue, described by a blueprint card, built phase by phase by orun.
---

A **baseline** is a production-shaped product repository that can be rebuilt
for a new owner: a new GitHub organisation, a new cloud account, a new product
name. Where a template gives you files, a baseline gives you a live product on
`stage` and `prod` with its own CI, its own migrations, and its own catalog,
in about an hour.

Baselines sit on top of two things orun already has: the
[scaffold engine](../cli/orun-new.md) that places a `kind: Blueprint` into a
directory, and Orunbase's workspaces, integrations, and secrets. The
[guide](../examples/bootstrap-a-product-from-a-baseline.md) walks the flow
end to end; this page explains the pieces.

## Three documents, one build

```text
   registry row                 blueprint card                 build document
 baselines.yaml       ──▶       blueprint.yaml       ──▶     repo-blueprint.yaml
 (identity, tag,               (inputs, requires,            (modules, phases,
  tier, manifestPath)           secrets, bootstrap)            hooks, gates)
        │                              │                              │
        └────────── what can be built  ┴  what the operator supplies  ┴  what orun places
```

**The registry row** answers *what can be built and by whom*. It lives in the
platform's baseline registry and carries the id the console routes on, the
`sourceRepo`, the pinned `tag`, the `tier` (free or paid), `visibility`
(public, unlisted, private), the measured `expectedMinutes`, the providers it
`requires`, and `manifestPath`, the path of the card inside the source
repository. The Orunbase-maintained rows are a file in the open-source
`orun-cloud` repository, `infra/baselines-registry/baselines.yaml`, reconciled
into the registry on every merge. Rows an account registers itself are written
by `orun baseline register`.

**The blueprint card** answers *what the operator supplies*. It is fetched
from `sourceRepo` at the registry's tag, never from a branch, and it declares:

- `spec.requires.integrations`, the providers that must be connected, and the
  API key role a build needs (`admin`, because it writes secrets);
- `spec.inputs`, each with a `pattern` its value is checked against before
  anything starts, and either asked, derived from another input
  (`derive: "https://api.{productdomain}"`), or filled from the chosen
  repository (`from: repo.name`) or the workspace (`from: workspace`);
- `spec.secrets`, a preview of the keys the build will mint from the
  workspace's provider connections, so the console can show them read-only;
- `spec.bootstrap.blueprint`, the path of the build document, and
  `spec.verify.urls`, what a finished build must answer.

The card carries **no tag**. It is fetched at the tag the registry pins, so a
tag inside it could only ever be a second, rotting copy.

**The build document** answers *what orun places*. It is an ordinary
`kind: Blueprint`: sources, modules with `template`, `copy`, or `consume`
placement, declared `dependsOn` edges, and `phases` with per-phase hooks. The
engine places modules in dependency order, phase by phase, and gates the
output: every generated `component.yaml` must parse, and the tree must pass
`orun validate` and `orun plan --dry-run` before the build reports success.

## Blueprint-driven and shell-layer baselines

A baseline is **blueprint-driven** when its build document declares every
phase and hook, and orun runs it with no model attached. `cirrus`, `lumen`,
and `multi-tenant-saas` are blueprint-driven: their registry rows declare
`manifestPath` and nothing else, and the platform sandbox runs
`orun agent serve --driver bootstrap`, which in turn runs
`orun baseline new <id>@<tag> --local --run-hooks --resume`.

A baseline with a **shell layer** additionally declares a `briefPath` (an
agent brief) and an `umbrellaPath` (the workflow the brief runs). A row
declares both or neither; one of the two is a row someone edited halfway.
`stratus` is the current example.

## Phases, hooks, and derived state

A phase is a placement barrier with hooks attached. All of phase N is placed
before phase N+1 begins, and no dependency edge may point forward across a
phase boundary. A phase can state what must be true before it runs
(preconditions) and can wait without failing when a condition it depends on
is not yet met, such as a pull request's CI.

Hooks are declared argv, never shell, run **after** placement, **outside**
the render sandbox, and only with `--run-hooks`. A hook either runs a command
or calls a typed action from a closed registry: opening a task, ensuring an
epic and milestone, minting a brokered secret, reconciling a workspace
connection, opening and landing a pull request, and the like. A bad
parameter is a parse error before the build starts.

Phase state is **derived**, not recorded: a fresh container can look at the
tree and the platform and answer which phases are done. That is what makes
`--resume` safe, and what lets the console retry a stopped build from the
setup it kept. The one thing the tree cannot tell is a phase that landed and
then failed to deploy, so a retry names it: `--redo <phase>` places it again.

## Where a build runs

| | `--via-platform` | `--local` |
|---|---|---|
| Runs on | A sandbox the platform provisions (4 vCPU, 8 GiB, open egress) | Your machine |
| Writes into | A repository you linked in the console | `--out <dir>` |
| Credentials | A time-boxed admin grant and brokered provider tokens | Your session or `ORUN_TOKEN`, your `gh` login |
| Watch it | The console: Overview and Agents | Your terminal, `--progress json` for machines |
| Hooks | Always on | Only with `--run-hooks` |
| Concurrency | One build per repository, enforced by a lease | Yours to manage |

Both paths gate on **readiness**: every provider the card requires must be
connected in the workspace, or nothing starts. Both resolve a stale `@tag`
forward to what the registry publishes now, and say so.

## Provenance and upgrade

Every build writes `.orun/provenance.lock` into the product: the blueprint
digest, each source digest, a secret-free hash of the inputs, and the mode
and target of every module. When the registry publishes a newer tag,
`orun new upgrade` re-renders it against the lock and three-way merges the
result. Files the baseline owns and you did not touch are updated; a file you
edited is a conflict and is never overwritten. A product built from a
baseline is upgradable, not a permanent fork.

## Publishing

Publishing a baseline release is two steps in a fixed order:

1. Push the git tag in the baseline's own repository.
2. Move the registry's pin: `orun baseline publish <id> <tag>` for a baseline
   your account registered, or a pull request on `baselines.yaml` for an
   Orunbase-maintained one.

The platform proves the tag before the registry moves: it must be a tag and
not a branch, the files a build reads first must be present in it, and the
card must parse. A tag that fails leaves the registry untouched and prints
the reason. Getting the order wrong costs a refused publish, never a broken
product.

## Related

- [Create a workspace and build it from a baseline](../examples/bootstrap-a-product-from-a-baseline.md)
- [`orun baseline`](../cli/orun-baseline.md) — command reference
- [`orun new`](../cli/orun-new.md) — the scaffold engine and the Blueprint grammar
- [Scaffolding design notes](https://github.com/sourceplane/orun/blob/main/docs/scaffolding.md)
