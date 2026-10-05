<p align="center">
  <strong>orun</strong><br/>
  <em>Platform discipline as code.</em>
</p>

<p align="center">
  <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/License-MIT-blue.svg"></a>
  <a href="https://github.com/sourceplane/orun/releases/latest"><img alt="Release" src="https://img.shields.io/github/v/release/sourceplane/orun"></a>
  <a href="https://pkg.go.dev/github.com/sourceplane/orun"><img alt="Go Reference" src="https://pkg.go.dev/badge/github.com/sourceplane/orun.svg"></a>
  <a href="https://github.com/sourceplane/orun/actions/workflows/release-oci.yaml"><img alt="Release workflow" src="https://github.com/sourceplane/orun/actions/workflows/release-oci.yaml/badge.svg"></a>
  <a href="https://goreportcard.com/report/github.com/sourceplane/orun"><img alt="Go Report Card" src="https://goreportcard.com/badge/github.com/sourceplane/orun"></a>
  <a href="CODE_OF_CONDUCT.md"><img alt="Contributor Covenant" src="https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg"></a>
</p>

**Declare your platform's structure and standards as intent. Package them as
baselines that evolve like code. Ground your agents in them. Let the runner
verify, plan, and execute.**

Every platform has standards: how a service is shaped, how Terraform is
applied, what may ship to production and in what order. Most of them live in
wikis, review comments, and the heads of a few people, so they drift, and a
coding agent joining the team learns them by imitating the nearest example.

`orun` gives those standards a declarative language. You write the platform's
structure and standards down as intent, in the repository; you package and
version them so every repository can adopt and upgrade them like a library;
your coding agents read the same intent so they stay inside it; and a runner
checks the intent, compiles it into a deterministic plan you can review, and
executes that plan.

It is a single Go binary. State lives in your repository. No server is
required; when a team needs shared state, live runs, agents, or a product
built from a baseline, the same CLI talks to **[Orunbase](https://orunbase.com)**,
the hosted control plane, which is itself
[open source](https://github.com/sourceplane/orun-cloud) and written as orun
intent.

```
  1 · DECLARE                2 · PACKAGE & EVOLVE          3 · GROUND AGENTS
  intent.yaml                Stacks (golden paths, OCI)    orun mcp · orun skills
  component.yaml       ◀──   intent presets (extends:) ──▶ agent types · base literacy
  compositions               baselines (whole products)    task contracts (affects)
        │
        ▼
  4 · VERIFY ──────────▶ PLAN ───────────────▶ EXECUTE ─────────────▶ RECORD
  orun validate          orun plan             orun run               .orun/ objects
  intent, rules          schemas, plan.json    shell · docker · gha   catalog · cockpit
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

Most teams encode their platform as the *product* of three forces,
components, environments, and triggers, smeared across workflow files,
templates, shared actions, and shell scripts. That encoding collapses **what
should happen** into **how it happens**: the environment matrix lives in `if:`
expressions, the golden path lives in whichever repository was copied last,
dependency order lives in job names, and nobody can answer "what will this
change actually do?" without running it.

`orun` treats the platform's structure and standards as code, in three kinds
of intent:

| Intent | Declares | Authored by | Lives in |
|---|---|---|---|
| **Platform intent** | Environments, groups, discovery roots, trigger bindings, promotion order, composition sources | Platform team | `intent.yaml`, one per repository, optionally inheriting presets |
| **Component intent** | One unit's identity, type, environment subscriptions, typed parameters, dependencies | The team that owns the code | `component.yaml`, next to the code |
| **Golden-path intent** | How a *type* (`terraform`, `helm-chart`, `cloudflare-worker`) is validated and built: its schema, jobs, and profiles | Platform team | Compositions, published as a versioned OCI **Stack** |

From that, four properties follow:

- **Declared.** A standard is a document the planner reads: a composition's
  schema, a profile, a dependency rule, a promotion order. If it is not
  declared, it does not exist.
- **Portable.** Golden paths travel as versioned Stacks, platform rules as
  intent presets inherited with `extends:`, and a whole product as a
  **baseline** that `orun new upgrade` can three-way merge forward. A source
  is pinned by version or by a `digest:` that `orun plan` verifies, and every
  plan records the digest each standard resolved to.
- **Shared with agents.** `orun mcp serve` gives a coding agent the catalog,
  runs, skills, and task plane derived from the same intent. Agent types carry
  a deny-by-default tool policy, and a task contract declares what a change
  may touch.
- **Verified before it runs.** `orun validate` checks the intent and its
  rules, `orun plan` checks every component against its composition's schema,
  and the plan is deterministic: identical inputs produce byte-identical
  plans, so a plan diff in a pull request is a faithful preview of behaviour.
  The repository is the desired state; `--changed` compiles only what a commit
  touched.

## What you get

| Pillar | Capability | Commands |
|---|---|---|
| Declare | Platform and component intent, golden paths as typed contracts, the effective intent explained field by field | `intent`, `component`, `compositions`, `work` |
| Package & evolve | Golden paths as versioned, pinnable OCI Stacks; a scaffold engine and a baseline registry: a component, or a whole live product, from a `kind: Blueprint`, upgradable later | `new`, `baseline`, `pack`, `publish`, `fetch`, `login` |
| Ground agents | One MCP server, hosted skills, an agent runtime with sealed briefs, a task plane, and a provenance pen: every pull request carries its lineage | `mcp`, `skills`, `agent`, `task`, `spec`, `pr`, `githooks` |
| Verify → plan → execute | A checker and a planner that compile intent into an immutable, diffable DAG; a backend-swappable runtime; a cockpit; a derived catalog and object graph | `validate`, `debug`, `plan`, `run`, `workflow`, `approve`, `status`, `logs`, `describe`, `get`, `tui`, `catalog`, `objects`, `gc` |
| Across all four | A cloud client for workspaces, secrets, integrations, and policy | `auth`, `workspace`, `cloud`, `secrets`, `integrations`, `policy`, `backend` |

**What orun is not.** Not a CI system: it runs inside your CI or your shell
and hands it a deterministic plan. Not an IaC or deployment tool: it
orchestrates Terraform, Helm, wrangler, turbo, and friends through typed
contracts. Not a wiki for standards: a rule orun does not read is a rule orun
does not know. Not a catalog you curate: catalog entities derive from the
intent that drives execution.

## Installation

Releases are built for `linux` and `darwin` on `amd64` and `arm64`, with a
`checksums.txt` beside every archive. The install script is the recommended
path:

```bash
curl -fsSL https://raw.githubusercontent.com/sourceplane/orun/main/install.sh | sh
```

It installs the latest release to `~/.local/bin`. `ORUN_VERSION=v2.73.4`
pins a version and `ORUN_INSTALL_DIR` changes the destination.

Other ways to install:

```bash
# go install (Go 1.25+)
go install github.com/sourceplane/orun/cmd/orun@latest

# from source
git clone https://github.com/sourceplane/orun.git && cd orun && make build

# as a pinned kiox provider, or under Docker
kiox init demo && kiox --workspace demo add ghcr.io/sourceplane/orun:v2.73.4 as orun
docker run --rm -v "$PWD":/work -w /work ghcr.io/sourceplane/orun:v2.73.4 plan
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

Or let an agent do all of it. **[SOFTWAREFACTORY.md](SOFTWAREFACTORY.md)**
explains how to hand a product idea to Claude Code (or any agent that reads a
`SKILL.md`) with the [`software-factory` skill](skills/software-factory/SKILL.md)
and get back a live product, an epic with milestones and tasks in the
workspace, its design documents, and every feature landed as a task-carrying
pull request.

## The command tree

```
orun
├── declare           intent · component · compositions · work
├── package & evolve  new · baseline · pack · publish · fetch · login
├── ground agents     mcp · skills · agent · task · spec · pr · githooks
├── verify → execute  validate · debug · plan · run · workflow · approve
│                     status · logs · describe · get · tui · tui-next · github
│                     catalog · objects · gc
└── cloud client      auth · workspace · cloud · secrets · integrations · policy · backend
```

`orun <command> --help` is always current. The
[CLI reference](https://orun-docs.pages.dev/cli/orun) has one page per
command.

## How it works

`orun plan` runs a six-stage compiler over your platform intent, the
discovered component intents, and the resolved golden paths:

| Stage | Name | What it does |
|---|---|---|
| 0 | Load and validate | Parse YAML, validate against JSON schemas, fail fast |
| 1 | Normalize | Resolve wildcards, default missing fields, canonicalize dependencies |
| 2 | Expand | Environment × component matrix, policy merge |
| 3 | Bind | Match component type to golden path, render step templates |
| 4 | Resolve | Convert component dependencies to job dependencies, detect cycles |
| 5 | Materialize | Emit `plan.json` with every reference concrete |

Parameter precedence, lowest to highest: environment `parameterDefaults`
(`"*"`, then the component's type), group `parameterDefaults` (`"*"`, then
the type), then the component's own `parameters`. Every component's
parameters are then checked against its composition's schema.

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
    promotion:
      dependsOn:
        - environment: staging
```

and a component declared next to its code:

```yaml
apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: web-api
spec:
  type: helm-chart
  domain: platform
  subscribe:
    environments: [staging, production]
  parameters:
    chartPath: charts/web-api
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
- [Software factory](SOFTWAREFACTORY.md): hand an agent a product idea and get a live product, built on a baseline with the work visible in Orunbase
- The four pillars: [declare](https://orun-docs.pages.dev/concepts/intent-model), [package and evolve](https://orun-docs.pages.dev/concepts/stacks), [ground agents](https://orun-docs.pages.dev/ai-context/orun-repositories), and [verify, plan, execute](https://orun-docs.pages.dev/overview/how-orun-works)
- [CLI reference](https://orun-docs.pages.dev/cli/orun)
- [Release notes](https://github.com/sourceplane/orun/releases)

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
