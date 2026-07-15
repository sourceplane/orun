# Scaffolding & instantiation (`orun new`)

**One engine, one language, two scales.** Scaffolding a single component and
instantiating a whole repo are the *same operation* at different sizes: resolve
typed inputs → resolve source(s) → order a set of **modules** by dependency →
place each (**render / copy / consume**) → gate the output → record provenance.
A single component is a Blueprint with one module; a repo is a Blueprint with
many. Nothing in the grammar changes with scale — only the module count and
which placement modes appear.

The engine (`internal/scaffold`) is pure and sandboxed: rendering is Go stdlib
`text/template` with a constrained funcmap (no file/exec/net/time/rand). All
ecosystem policy (pnpm, wrangler, Cloudflare, …) lives in the *baseline's own
blueprint + declared hooks*, never in orun.

## Commands

| Command | What it does |
|---------|--------------|
| `orun new --blueprint <ref> --out <dir>` | Scaffold/instantiate from a Blueprint. |
| `orun create …` / `orun instantiate …` | Aliases for `orun new` (same flow). |
| `orun new upgrade --out <dir> [--blueprint <newer>] [--apply]` | Re-render a newer Blueprint into an existing tree (3-way merge). |

Flags: `--blueprint` (path, required), `--out` (default `.`), `--values <file>`
(YAML), repeatable `--set key=value` (overrides `--values`), `--run-hooks`
(execute declared hooks, off by default). Missing inputs are **prompted for**
on an interactive terminal; on non-interactive stdin a missing required input
fails fast (exit `1`).

```bash
# interactive — prompts for any input not passed as a flag
orun new --blueprint ./blueprint.yaml --out ./my-service

# non-interactive
orun new --blueprint ./blueprint.yaml --out ./my-service \
  --set serviceName=billing-api --set domain=platform-billing
```

Exit codes: `0` success · `1` input-validation or gate failure · `6` unknown /
unparsable blueprint. The pipeline **fails closed** — a non-validating scaffold
is an error, never a half-written tree presented as success.

## The Blueprint

```yaml
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: cloudflare-worker

# (1) INPUTS — typed prompts / flags / a portal form, all from one schema.
inputs:
  serviceName: { type: string, pattern: "^[a-z][a-z0-9-]*$", required: true }
  runtime:     { type: enum, values: [node, python], default: node }
  apiToken:    { type: string, secret: true }        # collected without echo, never written

# (2) SOURCES — where module content comes from. Zero ⇒ inline templates.
sources:
  - name: baseline
    kind: dir                 # inline | dir | oci | git
    path: .                   # resolved relative to the blueprint's directory
ignore: [dist, .next, .turbo, coverage]   # build output never carried into a fork

# (3) MODULES — the atom of scaffolding. One = a component; many = a repo.
modules:
  - name: worker
    mode: template            # template | copy | consume
    source: baseline
    from: apps/{{ .serviceName }}
    to:   apps/{{ .serviceName }}
    bind: [wrangler.template.jsonc, component.yaml]   # files that take inputs
    dependsOn: [contracts]    # DECLARED edges — never sniffed from framework files
  - name: contracts
    mode: consume             # pinned dependency, emits no bytes
    source: baseline
    from: packages/contracts

# (4) PHASES — optional operational overlay (barriers + per-phase hooks).
phases:
  - name: foundation
    modules: [contracts]
    hooks: [{ id: lockfile, run: [pnpm, install, --lockfile-only] }]
  - name: services
    modules: [worker]

# (5) HOOKS — declared, ecosystem-specific, run outside the sandbox (opt-in).
hooks:
  postInstantiate:
    - id: install
      run: [pnpm, install, --lockfile-only]
```

### Inputs

Typed fields (`string | number | boolean | enum | object | array`) with
`default`, `values` (enum), `pattern` (RE2), and `required`. `secret: true`
fields are collected without echo, held in memory only, and **never written**
to any generated file — a template that would interpolate a secret, or a `copy`
whose bytes match one, fails the secret sweep. Secrets are also redacted from
`provenance.lock`.

### Sources

| `kind` | Resolution |
|--------|-----------|
| `inline` | Template bodies carried in the blueprint (`modules[].files`). |
| `dir` | A local path (relative to the blueprint), content-addressed + pinned. |
| `oci` | An OCI artifact, pulled + extracted. |
| `git` | `repo@ref` resolved to a commit, materialized + pinned. |

Every source is pinned by a content digest before any module reads it, so a
scaffold of the same `(blueprint, sources, inputs)` is byte-reproducible.
`ignore` (bare segment like `.next`, or a `**/dist` glob) excludes derived
output from both the digest and placement, keeping orun ecosystem-neutral (the
*blueprint* names the dirs).

### Modules and placement modes

- **`template`** — render each file under `from` (and the `to` path) through
  `text/template` + the constrained funcmap. `bind` names the files that
  legitimately interpolate inputs; a non-`bind` file that references `.inputs`
  is a lint error (keeps the interpolation surface auditable).
- **`copy`** — verbatim bytes, no engine. Still passes the secret sweep and
  path containment.
- **`consume`** — record a pinned dependency and emit no bytes (how a fork
  depends on shared packages without copying them).

Funcmap (closed allow-list): `lower upper title trim kebab slug quote default
indent`. A single-file `from` places to the exact `to` path; a directory `from`
re-roots its subtree under `to`. Every `to` must resolve **inside** `--out`.

### Ordering

Modules place in the order of orun's own dependency DAG over **declared**
`dependsOn`/`wiring` edges (never sniffed from `wrangler`/`package.json`). The
order is deterministic; a dependency cycle is an error unless the feedback edges
are named in `cycleBreak`, in which case the cluster places as one atomic batch.

### Phases (operational overlay)

Optional named, ordered groups that impose **placement barriers** and carry
their own hooks. The DAG remains the ordering authority — phases only add coarse
barriers (all of phase N placed before phase N+1) and a hook attachment point.
Rules (validated fail-closed): every module in exactly one phase, and **no
dependency edge may point forward across a phase boundary**. With no `phases`,
everything places in one implicit phase (default behavior). Per-phase hooks run
in phase order, then the global `postInstantiate` hooks.

### Hooks

Ecosystem post-steps orun must **not** internalize (lockfile resync, a
framework two-pass, secret seeding) are declared as explicit argv (no shell),
run **after** placement, **outside** the sandbox, and **opt-in** per run
(`--run-hooks`). orun executes the declared argv; it names no ecosystem itself.

## The output gate

Fail-closed, valid-by-construction, scaled by scope:

- **Component scale** — every generated `component.yaml` MUST pass both the
  permissive plan-engine parser and the strict catalog parser.
- **Repo scale** — if the scaffolded tree is an orun workspace (has an intent),
  it additionally MUST pass `orun validate` + `orun plan --dry-run` before the
  command reports success (the whole DAG compiles offline).

## Provenance & upgrade

Every scaffold writes `.orun/provenance.lock`: the `blueprint@digest`, each
`source@digest`, a secret-free inputs hash, and the per-module mode/target —
enough to prove lineage even for a single component. `orun new upgrade`
re-renders a newer blueprint/source against the lock and **3-way-merges** into
the target: blueprint-owned files the human did not touch are updated; a file
the human edited surfaces as a **conflict** and is never overwritten. This is
what makes an instance upgradable rather than a permanent fork.

## Example: instantiate a repo from a baseline

A whole-repo blueprint (one `dir`/`git` source, many modules) instantiates a
product repo — copying/consuming components in DAG order, gating every
`component.yaml`, then passing `orun validate` + `plan --dry-run`. See
`lumen/repo-blueprint.yaml` in the Lumen baseline for a real 46-component
blueprint with phases and declared hooks.

> **Spec:** `specs/orun-scaffolding/` — the unified Blueprint model, the module
> atom, source resolution, ordering, the sandbox/secret/containment contracts,
> the scope-scaled gate, and provenance + upgrade.
