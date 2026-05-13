# orun

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/sourceplane/orun.svg)](https://pkg.go.dev/github.com/sourceplane/orun)
[![Release](https://img.shields.io/github/v/release/sourceplane/orun)](https://github.com/sourceplane/orun/releases/latest)

A policy-aware workflow compiler that turns declarative intent into deterministic execution plan DAGs.

```
intent.yaml + component manifests + composition packages
                        |
              Planner Engine (6-stage compiler)
                        |
              plan.json + composition source lock
```

## Overview

`orun` compiles CI/CD intent into reproducible execution plans. It applies a six-stage compiler pipeline — load, normalize, expand, bind, resolve, materialize — to produce a fully resolved dependency graph that any runner can consume without re-interpreting the intent.

Key properties:

- **Deterministic** — identical inputs always produce identical plans
- **Policy-first** — group and domain constraints are non-negotiable at compile time
- **Schema-driven** — all validation rules live inside the provider, not the runner
- **OCI-native** — distributed as a kiox provider via `ghcr.io/sourceplane/orun`

## Installation

### Install script (recommended)

```sh
curl -fsSL https://raw.githubusercontent.com/sourceplane/orun/main/install.sh | sh
```

The script auto-detects your OS and architecture, downloads the latest release from GitHub, and installs the binary to `~/.local/bin`. Customize with environment variables:

| Variable | Default | Purpose |
|---|---|---|
| `ORUN_VERSION` | `latest` | Specific version to install (e.g. `v1.12.3`) |
| `ORUN_INSTALL_DIR` | `~/.local/bin` | Installation directory |

Example — install a specific version to `/usr/local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/sourceplane/orun/main/install.sh \
  | ORUN_VERSION=v1.12.3 ORUN_INSTALL_DIR=/usr/local/bin sh
```

### Manual download

Download the archive for your platform from the [releases page](https://github.com/sourceplane/orun/releases), extract, and place the binary on your `PATH`.

```sh
# Example: Linux amd64
curl -fsSL https://github.com/sourceplane/orun/releases/download/v1.12.3/orun_1.12.3_linux_amd64.tar.gz \
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
kiox --workspace demo add ghcr.io/sourceplane/orun:v1.12.3 as orun
kiox --workspace demo exec -- orun plan --intent intent.yaml
```

### OCI image

```sh
docker run ghcr.io/sourceplane/orun:v1.12.3 plan --intent intent.yaml
```

The OCI image is a kiox provider artifact. Use `oras pull ghcr.io/sourceplane/orun:v1.12.3` to pull the raw provider package.

## Quick start

```sh
# 1. Validate an intent file
orun validate --intent examples/intent.yaml

# 2. Debug the compiler stages
orun debug --intent examples/intent.yaml

# 3. Generate an execution plan
orun plan --intent examples/intent.yaml --output plan.json

# 4. Run the plan
orun run --plan plan.json
```

## Command reference

### `orun plan`

Compile an intent file into an execution plan.

```sh
orun plan \
  --intent intent.yaml \
  --output plan.json \
  --format json \
  --debug
```

### `orun validate`

Validate an intent file against its schema without generating a plan.

```sh
orun validate --intent intent.yaml
```

### `orun debug`

Emit a phase-by-phase dump of the compiler intermediate representation.

```sh
orun debug --intent intent.yaml
```

### `orun run`

Execute a compiled plan.

```sh
# Local shell runner (default)
orun run --plan plan.json

# GitHub Actions compatibility mode
orun run --plan plan.json --gha

# Docker runner (each step runs in a fresh container)
orun run --plan plan.json --runner docker

# Dry-run — print what would execute without running it
orun run --plan plan.json --dry-run
```

Runner selection order: `--gha` > `--runner` > `ORUN_RUNNER` env > auto-detect `github-actions` when `GITHUB_ACTIONS=true` or the plan contains a `use:` step > `local`.

### `orun compositions`

Manage composition packages.

```sh
# List compositions exported by the intent's declared sources
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

## Architecture

### Six-stage compiler pipeline

| Phase | Name | What it does |
|---|---|---|
| 0 | Load & Validate | Parse YAML, validate against JSON schemas, fail fast |
| 1 | Normalize | Resolve wildcards, default missing fields, canonicalize deps |
| 2 | Expand | Environment × component matrix, policy merge |
| 3 | Bind | Match component type → composition, render step templates |
| 4 | Resolve | Convert component deps → job deps, detect cycles |
| 5 | Materialize | Emit `plan.json` with all references concrete |

### Configuration precedence (low → high)

```
type defaults < composition defaults < group defaults < environment defaults < component inputs
```

Policy rules are enforced at all levels and cannot be overridden.

### Intent schema

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

External components can declare themselves in a `component.yaml` next to their code; `orun` discovers them from the configured roots.

### Composition schema

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

Compositions support both `run:` shell steps and GitHub Actions-style `use:` steps. When the compiled plan contains a `use:` step, `orun run` auto-selects the `github-actions` runner.

### Output plan

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

## Repository structure

```
orun/
├── cmd/orun/            # CLI entry point
├── internal/
│   ├── composition/     # Package resolution and caching
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
├── install.sh           # Install script
└── provider.yaml        # kiox provider manifest
```

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
