# orun

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/sourceplane/orun.svg)](https://pkg.go.dev/github.com/sourceplane/orun)
[![Release](https://img.shields.io/github/v/release/sourceplane/orun)](https://github.com/sourceplane/orun/releases/latest)

**Write your platform as intent. Compile it into one deterministic state. Converge the deviation on every commit.**

`orun` is an **intent compiler for platform engineering**. You describe your
whole delivery platform as a portable collection of *intent* — what exists,
where it ships, under which rules, and how each kind of thing is built — and
`orun` compiles that collection into a single, reviewable platform state. Every
commit declares a new desired state; `orun` computes exactly what deviated and
converges it with a deterministic plan you can read before it runs.

```
   platform intent          component intent           golden-path intent
     intent.yaml      +       component.yaml      +       compositions
   (environments,            (one unit, declared          (how a type is
    policy, triggers)         next to its code)            built — portable)
                                     │
                                     ▼
                        orun · the intent compiler
                                     │
                  ┌──────────────────┼──────────────────┐
                  ▼                  ▼                  ▼
            plan.json           run anywhere        .orun/ record
        (the platform state   shell · docker · gha  (catalog + history,
         for this commit)                            content-addressed)
```

---

## The idea in one paragraph

Most teams encode their platform as the *product* of three forces — components,
environments, and triggers — smeared across workflow files, templates, shared
actions, and shell scripts. That encoding collapses **what should happen** into
**how it happens**: the environment matrix lives in `if:` expressions, policy
lives in review vigilance, dependency order lives in job names, and nobody can
answer "what will this change actually do?" without running it. `orun` rejects
that. It treats your platform as a body of **intent** and itself as a
**compiler**: a pure function from your declarations to a complete, deterministic
plan. The plan is the decision; everything downstream just executes and records
it.

## Everything is intent

`orun` has exactly one kind of input — intent — wearing three hats. None of them
contain a line of execution logic; each says *what should be true*, and the
compiler decides *how*.

| Intent | Declares | Authored by | Lives in |
|---|---|---|---|
| **Platform intent** | Environments, groups, policies, discovery roots, trigger bindings, composition sources | Platform team | `intent.yaml` (one per repo) |
| **Component intent** | One unit's identity, type, environment subscriptions, typed inputs, dependencies | The team that owns the code | `component.yaml` (next to the code) |
| **Golden-path intent** | How a *type* (`terraform`, `helm-chart`, `cloudflare-worker`) is validated and built — its schema, jobs, and profiles | Platform team | Compositions (a portable, versioned **Stack**) |

A composition is simply the platform team's *intent about a kind of thing* —
"this is how every Terraform component in our org is planned, verified, and
released." It is intent that happens to be reusable and distributable. That is
why it ships as an OCI artifact you can pull into any repo: **intent is the
substrate, and golden paths are intent you can package.**

> **Coming:** because compositions *are* intent, they can be authored in a file
> named `intent.yaml` too — collapsing the last naming distinction between the
> three hats. One envelope, one mental model, top to bottom.

## Compile your platform into a state

`orun plan` runs a six-stage compiler — load, normalize, expand, bind, resolve,
materialize — over your platform intent, the discovered component intents, and
the locked golden paths. The output is `plan.json`: an immutable DAG of jobs in
which **every default, policy merge, and dependency edge is explicit.**

This is the heart of the model: your platform is not a pile of scripts, it is a
**compiled artifact.**

- **Deterministic.** The compiler is a pure function of
  `(intent, components, locked composition digests, trigger context)`. Identical
  inputs produce byte-identical plans — so a plan diff in a pull request is a
  faithful preview of behavior, not a guess.
- **Complete.** If a behavior isn't visible in the plan, that's a bug. Implicit
  defaults become explicit; policy merges are shown; every dependency edge is
  named.
- **Policy-checked at compile time.** Group and environment policies are
  enforced when the plan is *built*, not when it runs. A non-compliant intent
  fails `orun validate` with a structured error — not a half-deployed
  environment at 2 a.m.

## Converge the deviation, one commit at a time

If Kubernetes is a control plane for *running* software — declare desired state,
controllers continuously reconcile reality toward it — `orun` is the equivalent
for *delivering* it. Delivery is event-driven, so instead of a daemon spinning a
reconcile loop, `orun` converges **per commit**:

1. **Your repo is the desired state.** The full collection of intent describes
   the platform you want.
2. **A commit introduces a deviation.** `orun`'s change-detection engine reads
   the content-addressed catalog and computes *exactly* which components the
   commit touched — directly changed, their dependents, the affected set — and
   over-reports on ambiguity so nothing silently drops.
3. **`orun` compiles the minimal plan to close the gap.** Same intent, scoped to
   the deviation. A pull request gets parallel verification; a tag gets an
   ordered release. One source of truth, behavior shaped by the trigger.
4. **The runner converges it** against the backend you choose, and the result is
   sealed into the record.

There is never drift between "what the repo says" and "what is deployed,"
because the repo *is* the desired state and every commit reconverges toward it —
with a plan you reviewed first.

```
commit ──▶ orun: what deviated? ──▶ compile the delta ──▶ converge ──▶ record
  ▲                                                                      │
  └──────────────── the repo is always the desired state ───────────────┘
```

## Write your platform as a portable collection of component intent

Because each component declares itself next to its code — its type, its
subscriptions, its inputs, its dependencies — a component is a **self-contained,
portable unit of intent.** That has consequences that compound:

- **Lift-and-shift.** Move a component (its `component.yaml` + code) into another
  platform and it keeps its meaning; the new platform's intent and golden paths
  bind it.
- **Golden paths travel.** Compositions distribute as OCI Stacks
  (`ghcr.io/your-org/platform-stack`). Pull a vetted `terraform` or
  `cloudflare-worker` path into any repo and every component of that type
  inherits it — versioned, lockable, deprecatable.
- **Grow a few components at a time.** A new platform is a directory of
  component intents plus a `Stack` reference. No global script to edit, no
  hidden coupling — add a `component.yaml` under a discovered root and it joins
  the plan.
- **A catalog you never curate.** The same resolved intent projects a typed
  service catalog — Components, Systems, Domains, APIs, Resources, Environments,
  Compositions — with ownership from `CODEOWNERS` and live deployments derived
  from real execution history. If it ships, it's in the catalog; if it's in the
  catalog, the sources say so.

Your platform becomes something you can read, diff, fork, and recompose — a
portable collection of component intent, not a bespoke CI snowflake.

## What you get

- **A planner.** `orun plan` compiles intent into an immutable, diffable
  `plan.json` DAG.
- **A compile-time policy engine.** Guardrails enforced before anything runs.
- **A backend-swappable runtime.** The plan is the boundary — execute the same
  `plan.json` on your local shell, in Docker, or on GitHub Actions without
  recompiling.
- **A derived service catalog.** A projection of your sources and run history —
  never hand-maintained, never stale.
- **A cockpit.** `orun status`, `orun logs`, and the `orun tui` terminal cockpit
  render the same state through the same view-model and design tokens. What you
  see in a CI log is what you see in the control room.
- **A git-shaped record.** Every catalog, plan, and run is stored as immutable,
  content-addressed objects under `.orun/` — no server to operate, no database
  to back up.

## What orun is not

- **Not a CI system.** It runs *inside* your CI (or your shell) and hands it a
  deterministic plan. Your CI provides compute and credentials; `orun` provides
  the decision.
- **Not an IaC or deployment tool.** It orchestrates Terraform, Helm, wrangler,
  turbo, and friends — it doesn't replace them. Golden paths wrap your existing
  tools in typed, versioned contracts.
- **Not a hosted platform.** A single binary; state lives in your repo's
  `.orun/`. (Optional remote-state and cloud backends exist for shared state.)
- **Not a catalog you curate by hand.** Catalog entities are derived from the
  same intent that drives execution.

---

## Installation

### Install script (recommended)

```sh
curl -fsSL https://raw.githubusercontent.com/sourceplane/orun/main/install.sh | sh
```

The script auto-detects your OS and architecture, downloads the latest release from GitHub, and installs the binary to `~/.local/bin`. Customize with environment variables:

| Variable | Default | Purpose |
|---|---|---|
| `ORUN_VERSION` | `latest` | Specific version to install (e.g. `v2.19.0`) |
| `ORUN_INSTALL_DIR` | `~/.local/bin` | Installation directory |

Example — install a specific version to `/usr/local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/sourceplane/orun/main/install.sh \
  | ORUN_VERSION=v2.19.0 ORUN_INSTALL_DIR=/usr/local/bin sh
```

### Manual download

Download the archive for your platform from the [releases page](https://github.com/sourceplane/orun/releases), extract, and place the binary on your `PATH`.

```sh
# Example: Linux amd64
curl -fsSL https://github.com/sourceplane/orun/releases/download/v2.19.0/orun_2.19.0_linux_amd64.tar.gz \
  | tar xz -C /usr/local/bin
```

Supported platforms: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.

### From source

```sh
git clone https://github.com/sourceplane/orun.git
cd orun
go build -o orun ./cmd/orun
```

Requires Go 1.25 or later.

### Via kiox

```sh
kiox init demo
kiox --workspace demo add ghcr.io/sourceplane/orun:v2.19.0 as orun
kiox --workspace demo exec -- orun plan --intent intent.yaml
```

### OCI image

```sh
docker run ghcr.io/sourceplane/orun:v2.19.0 plan --intent intent.yaml
```

The OCI image is a kiox provider artifact. Use `oras pull ghcr.io/sourceplane/orun:v2.19.0` to pull the raw provider package.

## Quick start

```sh
# 0. Open the cockpit (interactive). Bare `orun` on a terminal launches the TUI.
orun

# 1. Validate your intent
orun validate --intent examples/intent.yaml

# 2. See the compiler stages
orun debug --intent examples/intent.yaml

# 3. Compile your platform state
orun plan --intent examples/intent.yaml --output plan.json

# 4. Converge it
orun run --plan plan.json

# 5. Converge only what this commit deviated
orun run --changed --base main
```

Running `orun` with no arguments on an interactive terminal opens the
[Cockpit TUI](https://orun.sourceplane.ai/cli/orun-tui) (equivalent to `orun tui`).
In a non-interactive shell — CI, pipes, redirected output — or when
`ORUN_NO_TUI=1` is set, a bare `orun` prints help instead, so scripts stay
predictable. Explicit subcommands (`orun plan`, `orun run`, …) are unaffected.

## Command reference

### `orun plan`

Compile intent into a platform-state plan.

```sh
orun plan \
  --intent intent.yaml \
  --output plan.json \
  --format json \
  --debug
```

### `orun validate`

Validate intent against its schema without compiling a plan.

```sh
orun validate --intent intent.yaml
```

### `orun debug`

Emit a phase-by-phase dump of the compiler intermediate representation.

```sh
orun debug --intent intent.yaml
```

### `orun run`

Converge a compiled plan.

```sh
# Local shell runner (default)
orun run --plan plan.json

# GitHub Actions compatibility mode
orun run --plan plan.json --gha

# Docker runner (each step runs in a fresh container)
orun run --plan plan.json --runner docker

# Dry-run — print what would converge without running it
orun run --plan plan.json --dry-run

# Converge only the deviation this commit introduced
orun run --changed --base main
```

Runner selection order: `--gha` > `--runner` > `ORUN_RUNNER` env > auto-detect `github-actions` when `GITHUB_ACTIONS=true` or the plan contains a `use:` step > `local`.

### `orun compositions`

Manage golden-path intent (composition packages).

```sh
# List golden paths exported by the intent's declared sources
orun compositions --intent intent.yaml

# Resolve and write a source lock file
orun compositions lock --intent intent.yaml

# Build a portable composition archive
orun compositions package build \
  --root examples/compositions \
  --output dist/example-platform-compositions-0.9.2.tgz

# Push a composition archive to an OCI registry
orun compositions package push dist/example-platform-compositions-0.9.2.tgz oci://ghcr.io/org/compositions:v0.9.2
```

### `orun status` / `orun logs`

```sh
orun status
orun logs --failed
```

### `orun catalog`

```sh
orun catalog refresh                       # resolve + persist the component catalog
orun catalog affected --base main --json   # what this commit deviated
```

Resolves and inspects the content-addressed component catalog that powers
deviation detection (`--changed`) and the cockpit. `orun catalog affected`
reports the directly-changed, dependent, affected, and selected component
sets — the same engine `orun plan/run --changed` use.

### `orun cloud`

```sh
orun cloud status                          # show the active org/project link
orun cloud link --org acme                 # link this repo to an Orun Cloud org
orun cloud check                           # is this repo allow-listed for the resolved org?
orun cloud open                            # open this repo's console page
```

Manages the link between this repo and an Orun Cloud org/project for remote
state. `orun auth login` already links automatically (the project is the repo);
`orun cloud link` is the advanced path for choosing a specific org.

`orun cloud check` is a pre-flight for the credential-free CI path: it resolves
the org the way a run does (`--org` > `ORUN_ORG` > `intent.yaml`
`execution.state.org` > the cached link), lists the org's allow-list, and reports
whether **this** repo is on it — turning a mysterious CI `404` into a one-command
local diagnosis. A repo that is not allow-listed prints exactly how to add it.

### Flags

| Flag | Short | Description |
|---|---|---|
| `--intent` | `-i` | Path to intent YAML |
| `--output` | `-o` | Output plan file (default: `plan.json`) |
| `--format` | `-f` | Plan format: `json` or `yaml` |
| `--plan` | `-p` | Compiled plan file for `run` |
| `--config-dir` | `-c` | Legacy fallback path to folder-shaped compositions |
| `--debug` | | Verbose compiler logging |
| `--dry-run` | | Preview run without executing |
| `--verbose` | | Full step logs during `run` |
| `--gha` | | Shortcut for `--runner github-actions` |
| `--runner` | | Execution backend: `local`, `github-actions`, `docker` |

## Cockpit UX

Every Orun surface — `orun status`, `orun get`, `orun logs`,
`orun status --watch`, and `orun tui` — renders through the same
**cockpit** layer. The CLI is the TUI compressed to a single frame; the
TUI is the CLI with navigation.

```
.orun/  ──▶  cockpit/bridge  ──▶  cockpit/viewmodel  ──▶  cockpit/render
                  │                                              │
                  └──▶  cockpit/watch (live updates) ─────────────┤
                                                                  ▼
                                            cockpit/surface  →  stdout / TUI
```

What you get in practice:

```
▲ orun multi-environment-platform
  Plan: sha256-ad6ce · Run: gh-26563885741-... · State: completed · Duration: 0ms
  Scope: 1 component · 1 job
  Status:   ✓ 1 succeeded · ◐ 0 running · ○ 0 queued
  Progress: ▓▓▓▓▓▓▓ 100%
  ● api-edge-worker
  │  └─ ✓ verify-deploy  19.0s
```

`NO_COLOR` strips colour while keeping glyphs; `--output=json` falls through the
same renderers via the `JSONSurface`. See
[`docs/plans/2026-05-29-cockpit-ux-redesign.md`](docs/plans/2026-05-29-cockpit-ux-redesign.md)
for the phased rollout.

## Architecture

### Six-stage compiler pipeline

| Phase | Name | What it does |
|---|---|---|
| 0 | Load & Validate | Parse YAML, validate against JSON schemas, fail fast |
| 1 | Normalize | Resolve wildcards, default missing fields, canonicalize deps |
| 2 | Expand | Environment × component matrix, policy merge |
| 3 | Bind | Match component type → golden path, render step templates |
| 4 | Resolve | Convert component deps → job deps, detect cycles |
| 5 | Materialize | Emit `plan.json` with all references concrete |

### Configuration precedence (low → high)

```
type defaults < composition defaults < group defaults < environment defaults < component inputs
```

Policy rules are enforced at all levels and cannot be overridden.

### Platform intent (`intent.yaml`)

```yaml
apiVersion: sourceplane.io/v1
kind: Intent
metadata:
  name: my-deployment

discovery:
  roots:
    - services/
    - infra/

groups:
  platform:
    policies:
      isolation: strict
    defaults:
      timeout: 15m

environments:
  production:
    selectors:
      domains: ["platform"]
  staging:
    defaults:
      replicas: 2

components:
  - name: web-api
    type: helm
    domain: platform
    subscribe:
      environments: [staging, production]
    inputs:
      chart: my-org/web-api
```

Component intents can also declare themselves in a `component.yaml` next to their code; `orun` discovers them from the configured roots.

#### Remote state & tenancy (`execution.state`)

A repo using Orun Cloud for remote state declares it in `intent.yaml` — the
committed, reviewable home for the backend and the enforced org:

```yaml
execution:
  state:
    mode: remote
    backendUrl: https://api.orun.cloud
    org: acme            # slug or org_… — the declared, enforced tenancy
    # project: <repo>    # advanced override only; default is the repo
    requireOrg: true     # strict mode (implied whenever org is set)
    autopushCatalog: true # publish the resolved catalog after a clean default-branch plan
```

- **`org`** is sent on every remote op — including the credential-free GitHub
  Actions OIDC exchange — so the server can enforce `claim ⊆ authorized`.
  Precedence: `--org` > `ORUN_ORG` > `execution.state.org` > the cached link.
- **`requireOrg`** turns on strict mode: a non-interactive remote op that
  resolves no org fails fast, pointing at `execution.state.org`, instead of
  exchanging an empty claim into an ambiguous scope. Implied whenever `org` is
  set.
- **`project`** defaults to the repo (`project = repo`); declare it only for a
  rename or a monorepo split.
- **`autopushCatalog`** best-effort publishes the resolved catalog to the
  backend after a successful default-branch plan, keeping the org-global catalog
  head fresh without an explicit `--push-catalog`.

Run `orun cloud check` from a dev machine to confirm a repo is allow-listed for
the resolved org before wiring up CI.

### Golden-path intent (composition)

```yaml
apiVersion: sourceplane.io/v1alpha1
kind: CompositionPackage
metadata:
  name: platform-compositions
spec:
  version: 0.9.2
  orun:
    minVersion: ">=0.20.0"
  exports:
    - composition: helm
      path: helm/job.yaml
    - composition: terraform
      path: terraform/job.yaml
```

Golden paths support both `run:` shell steps and GitHub Actions-style `use:` steps. When the compiled plan contains a `use:` step, `orun run` auto-selects the `github-actions` runner.

### Output: the platform state

The compiled plan is a self-contained DAG:

```json
{
  "apiVersion": "orun.io/v1",
  "kind": "Plan",
  "metadata": { "name": "my-deployment", "generatedAt": "...", "checksum": "sha256-..." },
  "jobs": [
    {
      "id": "web-api@production.deploy",
      "component": "web-api",
      "environment": "production",
      "composition": "helm",
      "dependsOn": ["db@production.deploy"],
      "steps": [{ "name": "deploy", "run": "helm upgrade ..." }]
    }
  ]
}
```

## Design principles

`orun` is shaped by five load-bearing principles — every command and concept
traces back to one of them:

1. **Intent and execution are different layers.** What should happen never mixes
   with how it happens.
2. **The plan is the audit artifact.** `plan.json` is the record of what was
   decided, from what inputs, at what revision.
3. **Determinism over cleverness.** Identical inputs → byte-identical plans.
4. **Policy at compile time.** Guardrails are enforced before anything runs.
5. **One design language across every surface.** CLI, TUI, and docs share one
   set of tokens.

Read the full [design principles](https://orun.sourceplane.ai/principles).

## Repository structure

```
orun/
├── cmd/orun/            # CLI entry point
├── internal/
│   ├── composition/     # Golden-path resolution and caching
│   ├── loader/          # YAML parsing
│   ├── schema/          # JSON schema validation
│   ├── normalize/       # Canonicalization
│   ├── expand/          # Env × component expansion
│   ├── planner/         # DAG construction and cycle detection
│   └── render/          # Plan materialization
├── assets/config/
│   ├── schemas/         # JSON schemas for intent, composition, plan
│   └── compositions/    # Legacy folder-based compatibility fixtures
├── examples/            # Example intent, components, and compositions
├── docs/                # Architecture and design documents
├── website/             # Documentation site (orun.sourceplane.ai)
├── install.sh           # Install script
└── provider.yaml        # kiox provider manifest
```

## Documentation

Full documentation lives at **[orun.sourceplane.ai](https://orun.sourceplane.ai)**:

- [What is orun?](https://orun.sourceplane.ai/overview/what-is-orun) — the full picture
- [How orun works](https://orun.sourceplane.ai/overview/how-orun-works) — three artifacts, one loop
- [The resource model](https://orun.sourceplane.ai/overview/resource-model) — every behavior as typed intent
- [Design principles](https://orun.sourceplane.ai/principles) — the why behind the architecture
- [Quick start](https://orun.sourceplane.ai/start/quick-start) — compile and run your first plan in ten minutes

## Contributing

Contributions are welcome. Areas where help is most useful:

- New composition types and runner backends
- Schema validation improvements
- GitHub Actions compatibility coverage
- Documentation and examples

See [website/docs/contributing/contributing.md](website/docs/contributing/contributing.md) for the development loop.

## License

[MIT](LICENSE) — Copyright Sourceplane contributors.

## Community

- Issues: [github.com/sourceplane/orun/issues](https://github.com/sourceplane/orun/issues)
- Discussions: [github.com/sourceplane/orun/discussions](https://github.com/sourceplane/orun/discussions)
