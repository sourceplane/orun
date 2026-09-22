<p align="center">
  <strong>orun</strong><br/>
  <em>The intent compiler for platform engineering.</em>
</p>

<p align="center">
  <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/License-MIT-blue.svg"></a>
  <a href="https://github.com/sourceplane/orun/releases/latest"><img alt="Release" src="https://img.shields.io/github/v/release/sourceplane/orun"></a>
  <a href="https://pkg.go.dev/github.com/sourceplane/orun"><img alt="Go Reference" src="https://pkg.go.dev/badge/github.com/sourceplane/orun.svg"></a>
  <a href="https://github.com/sourceplane/orun/actions/workflows/release-oci.yaml"><img alt="Release workflow" src="https://github.com/sourceplane/orun/actions/workflows/release-oci.yaml/badge.svg"></a>
  <a href="https://goreportcard.com/report/github.com/sourceplane/orun"><img alt="Go Report Card" src="https://goreportcard.com/badge/github.com/sourceplane/orun"></a>
  <a href="CODE_OF_CONDUCT.md"><img alt="Contributor Covenant" src="https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg"></a>
</p>

**Write your platform as intent. Compile it into one deterministic state.
Converge the deviation on every commit.**

`orun` is an open-source intent compiler for platform engineering. You
describe your whole delivery platform as a portable collection of *intent*,
what exists, where it ships, under which rules, and how each kind of thing is
built, and `orun` compiles that collection into a single, reviewable platform
state. Every commit declares a new desired state; `orun` computes exactly what
deviated and converges it with a plan you can read before it runs.

It is a single Go binary. State lives in your repository. No server is
required; when a team needs shared state, live runs, agents, or a product
built from a baseline, the same CLI talks to **[Orunbase](https://orunbase.com)**,
the hosted control plane, which is itself
[open source](https://github.com/sourceplane/orun-cloud) and written as orun
intent.

```
   platform intent          component intent           golden-path intent
     intent.yaml      +       component.yaml      +       compositions
   (environments,            (one unit, declared          (how a type is
    policy, triggers)         next to its code)            built; portable)
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

## Table of contents

- [Why orun](#why-orun)
- [What you get](#what-you-get)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Create a workspace and build a product from a baseline](#create-a-workspace-and-build-a-product-from-a-baseline)
- [The command tree](#the-command-tree)
- [How it works](#how-it-works)
- [Documentation](#documentation)
- [Project status and roadmap](#project-status-and-roadmap)
- [Community and contributing](#community-and-contributing)
- [Security](#security)
- [License](#license)

## Why orun

Most teams encode their platform as the *product* of three forces, components,
environments, and triggers, smeared across workflow files, templates, shared
actions, and shell scripts. That encoding collapses **what should happen** into
**how it happens**: the environment matrix lives in `if:` expressions, policy
lives in review vigilance, dependency order lives in job names, and nobody can
answer "what will this change actually do?" without running it.

`orun` rejects that. It treats your platform as a body of **intent** and
itself as a **compiler**: a pure function from your declarations to a
complete, deterministic plan. The plan is the decision; everything downstream
executes and records it.

| Intent | Declares | Authored by | Lives in |
|---|---|---|---|
| **Platform intent** | Environments, groups, policies, discovery roots, trigger bindings, composition sources | Platform team | `intent.yaml`, one per repository |
| **Component intent** | One unit's identity, type, environment subscriptions, typed parameters, dependencies | The team that owns the code | `component.yaml`, next to the code |
| **Golden-path intent** | How a *type* (`terraform`, `helm`, `cloudflare-worker`) is validated and built: its schema, jobs, and profiles | Platform team | Compositions, distributed as a versioned OCI **Stack** |

- **Deterministic.** Identical inputs produce byte-identical plans, so a plan
  diff in a pull request is a faithful preview of behaviour.
- **Complete.** Every default, policy merge, and dependency edge is explicit
  in `plan.json`. If a behaviour is not visible in the plan, that is a bug.
- **Policy at compile time.** Guardrails are enforced when the plan is built.
  A non-compliant intent fails `orun validate` with a structured error, not a
  half-deployed environment.
- **Converge per commit.** The repository is the desired state. The
  change-detection engine computes exactly which components a commit touched
  and compiles the minimal plan to close the gap.
- **Portable.** A component is a self-contained unit of intent. Move it to
  another platform and it keeps its meaning; golden paths travel as OCI
  artifacts; a whole product can be rebuilt from a **baseline**.

## What you get

| Capability | Commands |
|---|---|
| A planner that compiles intent into an immutable, diffable DAG | `plan`, `validate`, `debug`, `intent`, `component` |
| A backend-swappable runtime: the same plan runs on your shell, in Docker, or on GitHub Actions | `run`, `workflow`, `approve` |
| A cockpit: the CLI, `--watch`, and the TUI render the same state through one view-model | `status`, `logs`, `get`, `describe`, `tui`, `tui-next` |
| A derived service catalog and change detection you never curate by hand | `catalog`, `objects` |
| Golden paths as versioned, lockable OCI packages | `compositions`, `pack`, `publish`, `fetch`, `login` |
| A scaffold engine and a baseline registry: a component, or a whole live product, from a `kind: Blueprint` | `new`, `baseline` |
| A cloud client for workspaces, secrets, integrations, and policy | `auth`, `workspace`, `cloud`, `secrets`, `integrations`, `policy`, `backend` |
| A task plane and a provenance pen: every pull request carries its lineage | `task`, `pr`, `githooks`, `spec` |
| An agent runtime and an MCP server for coding agents | `agent`, `mcp`, `skills` |

**What orun is not.** Not a CI system: it runs inside your CI or your shell
and hands it a deterministic plan. Not an IaC or deployment tool: it
orchestrates Terraform, Helm, wrangler, turbo, and friends through typed
contracts. Not a catalog you curate: catalog entities derive from the intent
that drives execution.

## Installation

Releases are built for `linux` and `darwin` on `amd64` and `arm64`, with a
`checksums.txt` beside every archive. The install script is the recommended
path:

```bash
curl -fsSL https://raw.githubusercontent.com/sourceplane/orun/main/install.sh | sh
```

It installs the latest release to `~/.local/bin`. `ORUN_VERSION=v2.58.15`
pins a version and `ORUN_INSTALL_DIR` changes the destination.

Other ways to install:

```bash
# go install (Go 1.25+)
go install github.com/sourceplane/orun/cmd/orun@latest

# from source
git clone https://github.com/sourceplane/orun.git && cd orun && make build

# as a pinned kiox provider, or under Docker
kiox init demo && kiox --workspace demo add ghcr.io/sourceplane/orun:v2.58.15 as orun
docker run --rm -v "$PWD":/work -w /work ghcr.io/sourceplane/orun:v2.58.15 plan
```

A release archive can be verified and installed by hand; see
[Installation](https://orun-docs.pages.dev/start/installation).

## Quick start

```bash
git clone https://github.com/sourceplane/orun.git && cd orun && make build

./orun validate --intent examples/intent.yaml        # 1. validate intent and components
./orun plan --intent examples/intent.yaml --view dag # 2. compile the platform state
./orun run --plan .orun/plans/latest.json --dry-run  # 3. preview convergence
./orun run --changed --base main                     # 4. converge only what this commit changed
./orun status                                        # 5. read the result; bare `orun` opens the TUI
```

The full walkthrough is the
[quick start](https://orun-docs.pages.dev/start/quick-start).

## Create a workspace and build a product from a baseline

A **baseline** is a complete, production-shaped product repository the
platform can rebuild under your name. `cirrus`, for example, is a
multi-tenant SaaS on Cloudflare alone: a Worker fleet behind one edge API, a
Next.js console, D1 and KV, migrations, and CI that converges on merge.
Building it takes about an hour and one provider connection.

```bash
orun auth login                                   # browser approval, or --device
orun workspace create "Acme Cloud" --slug acme    # prints ws_… and the slug
orun workspace use acme                           # every cloud command now targets it

# connect Cloudflare in the console (Integrations), or paste a token:
orun integrations cloudflare connect < cloudflare-token.txt

orun baseline list                                # cirrus, lumen, multi-tenant-saas …
orun baseline check cirrus                        # exit 0 when every required provider is connected

# build on the platform, into a repository linked in the console:
orun baseline new cirrus --via-platform --repo acme/storefront \
  --set productname="Acme Cloud" --set productdomain=acme.dev --set subdomain=acme
# prints the as_… session id; watch it on the workspace's Overview page

# or build on this machine:
orun baseline new cirrus --local --out ./acme-cloud --run-hooks \
  --set productname="Acme Cloud" --set productdomain=acme.dev \
  --set githuborg=acme --set subdomain=acme
```

The guide
[Create a workspace and build it from a baseline](https://orun-docs.pages.dev/examples/bootstrap-a-product-from-a-baseline)
covers prerequisites, every flag, what the platform enforces, verification,
upgrades, and registering your own baseline. The model is explained in
[Baselines](https://orun-docs.pages.dev/concepts/baselines).

## The command tree

```
orun
├── plan · validate · debug · intent · component · compositions · describe · get
├── run · workflow · approve · status · logs · tui · tui-next · github · gc
├── catalog · objects
├── new · baseline
├── pack · publish · fetch · login
├── auth · workspace · cloud · secrets · integrations · policy · backend
├── task · pr · githooks · spec
└── agent · mcp · skills
```

`orun <command> --help` is always current. The
[CLI reference](https://orun-docs.pages.dev/cli/orun) has one page per
command.

## How it works

`orun plan` runs a six-stage compiler over your platform intent, the
discovered component intents, and the locked golden paths:

| Stage | Name | What it does |
|---|---|---|
| 0 | Load and validate | Parse YAML, validate against JSON schemas, fail fast |
| 1 | Normalize | Resolve wildcards, default missing fields, canonicalize dependencies |
| 2 | Expand | Environment × component matrix, policy merge |
| 3 | Bind | Match component type to golden path, render step templates |
| 4 | Resolve | Convert component dependencies to job dependencies, detect cycles |
| 5 | Materialize | Emit `plan.json` with every reference concrete |

Parameter precedence, lowest to highest: type defaults, composition defaults,
group `parameterDefaults`, environment `parameterDefaults`, component
`parameters`. Policy rules apply at every level and cannot be overridden.

A minimal platform intent:

```yaml
apiVersion: sourceplane.io/v1
kind: Intent
metadata:
  name: my-platform

compositions:
  sources:
    - name: platform-stack
      kind: oci
      ref: ghcr.io/acme/platform-stack:v1

discovery:
  roots: [services/, infra/]

environments:
  staging:
    activation:
      triggerRefs: [github-push-main]
    parameterDefaults:
      "*": { lane: verify }
  production:
    activation:
      triggerRefs: [github-release]
    policies:
      requireApproval: "true"
```

and a component declared next to its code:

```yaml
apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: web-api
spec:
  type: helm
  domain: platform
  subscribe:
    environments: [staging, production]
  parameters:
    chart: acme/web-api
  dependsOn:
    - component: postgres
```

Everything orun records, sources, catalogs, plans, runs, jobs, and task
contracts, is stored as immutable, content-addressed objects under `.orun/`.
`orun objects` inspects the graph; Orunbase replicates it for a team.

Read more: [How orun works](https://orun-docs.pages.dev/overview/how-orun-works),
[the resource model](https://orun-docs.pages.dev/overview/resource-model),
[design principles](https://orun-docs.pages.dev/principles),
[architecture](https://orun-docs.pages.dev/architecture/internals).

## Documentation

The documentation site is <https://orun-docs.pages.dev>; its source is
[`website/`](website/).

- [What is orun?](https://orun-docs.pages.dev/overview/what-is-orun)
- [Installation](https://orun-docs.pages.dev/start/installation) and [quick start](https://orun-docs.pages.dev/start/quick-start)
- [Create a workspace and build it from a baseline](https://orun-docs.pages.dev/examples/bootstrap-a-product-from-a-baseline)
- [Concepts](https://orun-docs.pages.dev/concepts/intent-model): intent, compositions, plans, execution, catalog, state, secrets, tenancy, baselines, tasks, agents
- [CLI reference](https://orun-docs.pages.dev/cli/orun)
- [Release notes](https://orun-docs.pages.dev/release-notes/v2.58.0)

Orunbase, the hosted control plane, is documented at
<https://docs.orunbase.com>. Contributor documents live under
[`docs/`](docs/README.md), and design specs under [`specs/`](specs/).

## Project status and roadmap

orun is used in production by Sourceplane and by the products built from the
public baselines; see [ADOPTERS.md](ADOPTERS.md). The `v2` line is stable:
intent, plan, and object-model formats are versioned, and breaking changes
bump the major version. Releases are cut continuously; the current release is
on the [releases page](https://github.com/sourceplane/orun/releases/latest).

[ROADMAP.md](ROADMAP.md) describes what is planned. [CHANGELOG.md](CHANGELOG.md)
points at the release notes.

## Community and contributing

Contributions of every kind are welcome: bug reports, documentation, new
compositions, runner backends, and design proposals.

- Read [CONTRIBUTING.md](CONTRIBUTING.md) for the development loop, the
  project invariants, commit sign-off, and the pull request process.
- The project is governed as described in [GOVERNANCE.md](GOVERNANCE.md);
  maintainers are listed in [MAINTAINERS.md](MAINTAINERS.md).
- Everyone participating agrees to the [code of conduct](CODE_OF_CONDUCT.md).
- Ask questions in [GitHub Discussions](https://github.com/sourceplane/orun/discussions)
  and report bugs in [GitHub Issues](https://github.com/sourceplane/orun/issues).
  [SUPPORT.md](SUPPORT.md) says what to include.

```bash
make build            # build ./orun
make test             # go test ./...
make lint fmt         # go vet, go fmt
make verify-generated # regenerate the catalog schema and fail on drift
cd website && npm ci && npm run docs:build   # the docs site; broken links fail the build
```

## Security

Please report vulnerabilities privately through the
[security advisory form](https://github.com/sourceplane/orun/security/advisories/new),
never through public issues. [SECURITY.md](SECURITY.md) describes the
process, supported versions, and how to verify release artifacts.

## License

[MIT](LICENSE). Copyright Sourceplane contributors.
