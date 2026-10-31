---
title: orun new
description: Scaffold a component or instantiate a whole repository from a kind:Blueprint — one engine at two scales, fail-closed, provenanced, and upgradable.
---

Scaffolding is how a standard gets applied to new code: a blueprint encodes the shape a component or a whole repository must have, and `orun new` places it, records its provenance, and can upgrade it later.

`orun new` places a `kind: Blueprint` into a directory. A blueprint with one
module and no sources is the single-component scaffolder; the same grammar
with a source and many modules instantiates a whole product repository. The
engine is the same at both scales, and it is what
[`orun baseline new`](./orun-baseline.md) runs once it has fetched a
baseline. `create` and `instantiate` are aliases.

```bash
orun new --blueprint <file> [--out <dir>] [--set k=v]... [--values <file>] [--run-hooks]
orun new --blueprint <file> --out <dir> --status [--json]
orun new upgrade --out <dir> [--blueprint <newer>] [--apply]
```

## Flags

| Flag | Meaning |
|---|---|
| `--blueprint <path>` | The Blueprint document. Required. |
| `--out <dir>` | Output directory, created if absent. Default `.`. |
| `--set key=value` | An input. Repeatable; overrides `--values` per key. A key the blueprint does not declare is refused before any prompt. |
| `--values <file>` | A YAML file of inputs. |
| `--run-hooks` | Execute declared hooks after placement, outside the sandbox. Off by default. |
| `--phase <name>` | Place only this phase. |
| `--until <name>` | Place every phase through this one. A run that stops before the last phase may keep a late-bound input's placeholder. |
| `--resume` | Place every phase not already derived as done. With `--phase` or `--until`, only within that phase or those phases (orun ≥ v2.90.0); `--phase` with `--until` is refused. |
| `--status` | Derive and print each phase's state without writing anything. `--json` for machines. |
| `--progress auto\|plain\|verbose\|json` | Progress rendering. Default `auto`, which ends with a summary: files placed, the repository gate's result (components, environments, and jobs the scaffolded intent plans to), and where the provenance lock is. `verbose` also lists every phase, batch, and file. |

Missing inputs are prompted for on an interactive terminal. On
non-interactive input a missing required input fails fast.

```bash
# interactive: prompts for any input not passed as a flag
orun new --blueprint ./blueprint.yaml --out ./my-service

# non-interactive
orun new --blueprint ./blueprint.yaml --out ./my-service \
  --set serviceName=billing-api --set domain=platform-billing
```

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | Input validation or output-gate failure |
| `6` | Unknown or unparsable blueprint |

The pipeline fails closed. A tree that does not validate is an error, never a
half-written result reported as success.

## The Blueprint

```yaml
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: cloudflare-worker

inputs:                       # typed prompts, flags, or a console form, one schema
  serviceName: { type: string, pattern: "^[a-z][a-z0-9-]*$", required: true }
  runtime:     { type: enum, values: [node, python], default: node }
  apiToken:    { type: string, secret: true }   # collected without echo, never written
  subdomain:                  # supplied by a later phase (see "Late-bound inputs")
    type: string
    default: your-workers-subdomain
    lateBound: true

sources:                      # where module content comes from; none means inline
  - name: baseline
    kind: dir                 # inline | dir | oci | git
    path: .
ignore: [dist, .next, .turbo, coverage]

modules:                      # the atom of scaffolding
  - name: worker
    mode: template            # template | copy | consume
    source: baseline
    from: apps/{{ .serviceName }}
    to:   apps/{{ .serviceName }}
    bind: [wrangler.template.jsonc, component.yaml]
    dependsOn: [contracts]
  - name: contracts
    mode: consume             # a pinned dependency that emits no bytes
    source: baseline
    from: packages/contracts

phases:                       # optional barriers with hooks
  - name: foundation
    modules: [contracts]
    hooks: [{ id: lockfile, run: [pnpm, install, --lockfile-only] }]
  - name: services
    modules: [worker]

hooks:
  preInstantiate:             # before the first phase places
    - id: epic
      uses: orun.task/ensure@v1
      with: { kind: epic, slug: platform-baseline, name: "Platform baseline" }
  postInstantiate:            # after the last phase
    - id: install
      run: [pnpm, install, --lockfile-only]
```

**Inputs** are typed (`string`, `number`, `boolean`, `enum`, `object`,
`array`) with `default`, `values`, `pattern` (RE2), and `required`. A
`secret: true` input is held in memory only and never written to a generated
file; a template that would interpolate one, or a copied file whose bytes
match one, fails the secret sweep. Secrets are redacted from the provenance
lock. `from: workspace` fills an unset input with the Orun workspace the build
runs in.

### Late-bound inputs

An input declared `lateBound: true` (orun ≥ v2.90.0) is one the build may not
have until a later phase runs. A workers.dev subdomain is the usual example: it
belongs to the Cloudflare account the product deploys into, and an account that
has not connected one has no subdomain to give yet. orun.work builds such an
account a code-only app by stopping after the scaffold stage, and runs the live
stage later.

The input's `default` is the **placeholder** the stopped build writes. The rest
of the build must replace it and may never keep it:

- A run that `--until` stops **before the build's last phase** may leave the
  input unset. The placeholder is placed and recorded.
- **Every other run** must be given a value: a full run, `--resume` with no
  stop, `--phase` (the platform runs the live stage on its own this way, and
  orun cannot tell a scaffold phase from a live one), and `--until` the last
  phase. Without one the run is refused before anything is placed, exit `1`,
  naming the input and its placeholder. A value you pass explicitly is yours,
  even when it equals the placeholder.
- **A resumed run takes a given value** over the one the provenance lock
  recorded, without the conflict every other input raises, and the lock then
  records the new value. A recorded placeholder is never recovered as though
  it were a value; a recorded real value is.

orun does not re-place files an earlier stage already placed: a resume treats
a placed phase as done even when its files now render differently, because
re-placing a branded product would undo the branding. So the phase that needs
the value is also the one that replaces the placeholder wherever the scaffold
stage wrote it, in its own modules or hooks.

A late-bound input needs a `default` and at least two `phases`, and cannot be
`secret`: the lock never records a secret, so there would be nothing to
replace. Each is a parse error.

```bash
# the scaffold stage: no Cloudflare yet, so no subdomain
orun new --blueprint repo-blueprint.yaml --out ./acme --set reponame=acme --until 01-scaffold
# later, the live stage brings it
orun new --blueprint repo-blueprint.yaml --out ./acme --resume --phase 02-live --set subdomain=acme
```

**Sources** are pinned by content digest before any module reads them, so a
scaffold of the same blueprint, sources, and inputs is byte-reproducible.
`dir` is a local path, `oci` an artifact, `git` a `repo@ref` resolved to a
commit, `inline` a template body in the blueprint itself.

**Modules** place in one of three modes. `template` renders through Go
`text/template` with a closed function set (`lower upper title trim kebab
slug quote default indent`) and no file, exec, network, time, or random
access; `bind` names the files allowed to read inputs, and any other file
that references them is a lint error. `copy` is verbatim bytes. `consume`
records a pinned dependency and emits nothing. Every `to` must resolve inside
`--out`.

**Ordering** follows the declared `dependsOn` edges, never anything sniffed
from a `package.json` or a `wrangler` file. A cycle is an error unless its
feedback edges are named in `cycleBreak`, in which case the cluster places as
one batch.

**Phases** add coarse barriers and hook attachment points: every module in
exactly one phase, and no edge pointing forward across a phase boundary. A
phase can state preconditions and can wait rather than fail. With no
`phases`, everything places in one implicit phase.

**Hooks** are argv, not shell. They run outside the render sandbox, and only
with `--run-hooks`. A hook is either `run` (a command) or `uses` (a typed
action from orun's closed registry, such as opening a task, minting a brokered
secret, or landing a pull request). A bad parameter is a parse error before
anything is placed. A phase's hooks sit in its `pre`, `post` and `await`
slots; two run-level lists belong to no phase: `hooks.preInstantiate` runs
before the first phase places (where a bootstrap opens the epic and every
milestone and task its phases will land under, so the shape of the work is
visible before any of it starts) and `hooks.postInstantiate` runs after the
last. Both are run on a resume too, so what they do must be idempotent.

A `preInstantiate` hook marked `background: true`, and every hook after it,
runs beside the phases instead of before them (orun ≥ v2.88.0). A hook whose
action works on the task plane (`orun.task/ensure@v1`, `orun.task/rollup@v1`,
`orun.pr/land@v1`) waits for the background to finish; every other hook runs
while it is still going. A background hook that fails stops the run at the
next hook, and the run never ends before the background does. `background` is
only for `uses:` hooks: a `run:` hook would share the product tree with the
phase that is writing it.

## The output gate

At component scale, every generated `component.yaml` must pass both the
plan-engine parser and the strict catalog parser. At repository scale, if the
tree has an intent, it must also pass `orun validate` and a full `orun plan`
compile (no plan file is written) before the command reports success.

## Provenance and `upgrade`

Every scaffold writes `.orun/provenance.lock`: the blueprint digest, each
source digest, a secret-free hash of the inputs, and each module's mode and
target. `orun new upgrade` re-renders a newer blueprint or source against the
lock and three-way merges it into the tree. Blueprint-owned files you did not
touch are updated; a file you edited is reported as a conflict and never
overwritten.

```bash
orun new upgrade --out ./my-service                  # report only
orun new upgrade --out ./my-service --apply          # apply non-conflicting updates
orun new upgrade --out ./my-service --blueprint ../blueprint-v2.yaml --apply
```

## Related

- [`orun baseline`](./orun-baseline.md) — fetch a registered baseline and run its build document through this engine
- [Baselines](../concepts/baselines.md)
- [Scaffolding design notes](https://github.com/sourceplane/orun/blob/main/docs/scaffolding.md)
