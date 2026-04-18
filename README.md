# arx - Schema-Driven Planner Engine

A **policy-aware workflow compiler** that turns **intent** into executable **plan DAGs**. Built on CNCF principles.

Transform declarative CI/CD intents into deterministic execution plans with automatic environment expansion, policy validation, and multi-platform support.

```
Intent.yaml + Job Compositions + Schemas
          ↓
    Planner Engine (6-stage compiler)
          ↓
    Plan.json (Fully resolved DAG)
```

## ✨ Key Features

- **🎯 Schema-Driven** - All validation rules live inside the provider
- **🔐 Policy-First** - Non-negotiable constraints at the group/domain level
- **🔄 Auto-Expansion** - Environment × Component matrix with intelligent merging
- **🔗 Dependency Resolution** - Automatic DAG creation with cycle detection
- **🐍 Cross-Platform** - Linux, macOS support (amd64, arm64)
- **🐳 OCI Distribution** - Docker/Podman/containerd/Kubernetes compatible
- **⚡ Deterministic** - Same inputs → Same outputs, every time
- **📊 Debuggable** - Detailed phase-by-phase IR dumps

## Documentation

- Start with the docs landing page: [website/docs/intro.mdx](website/docs/intro.mdx)
- Run the local docs site: `cd website && npm install && npm run docs:start`
- Build the static docs site: `cd website && npm run docs:build`

## Manual Cloudflare Pages deploy

The docs site builds into `website/docs-build/`. To publish it manually to Cloudflare Pages:

```bash
cd website
npm ci
npm run docs:build
wrangler login
wrangler pages deploy docs-build --project-name arx-docs
```

Replace `arx-docs` with your Cloudflare Pages project name if it is different.

## Installation

### Option 1: Direct Binary Download

Replace `<tag>` with the release you want from the GitHub releases page.

```bash
# macOS (arm64 - Apple Silicon)
curl -L https://github.com/sourceplane/arx/releases/download/<tag>/arx_<tag>_darwin_arm64.tar.gz | tar xz
sudo mv entrypoint /usr/local/bin/arx
chmod +x /usr/local/bin/arx

# macOS (amd64 - Intel)
curl -L https://github.com/sourceplane/arx/releases/download/<tag>/arx_<tag>_darwin_amd64.tar.gz | tar xz
sudo mv entrypoint /usr/local/bin/arx
chmod +x /usr/local/bin/arx

# Linux (amd64)
curl -L https://github.com/sourceplane/arx/releases/download/<tag>/arx_<tag>_linux_amd64.tar.gz | tar xz
sudo mv entrypoint /usr/local/bin/arx
chmod +x /usr/local/bin/arx

# Linux (arm64)
curl -L https://github.com/sourceplane/arx/releases/download/<tag>/arx_<tag>_linux_arm64.tar.gz | tar xz
sudo mv entrypoint /usr/local/bin/arx
chmod +x /usr/local/bin/arx
```

Verify installation:
```bash
arx --version
arx --help
```

### Option 2: From Source

```bash
git clone https://github.com/sourceplane/arx.git
cd arx
go build -o arx ./cmd/arx
sudo mv arx /usr/local/bin/
```

`make build` also emits deprecated `ciz` and `liteci` aliases for local compatibility.

### Option 3: Docker/OCI Container

```bash
# Docker
docker run ghcr.io/sourceplane/arx:<tag> plan -i intent.yaml

# Podman (recommended for CI/CD)
podman run ghcr.io/sourceplane/arx:<tag> plan -i intent.yaml

# Kubernetes
kubectl run arx --image=ghcr.io/sourceplane/arx:<tag>
```

### Option 4: Using tinx

```bash
repo_root="$(pwd)"
tinx init demo -p ghcr.io/sourceplane/arx:<tag> as arx
tinx --workspace demo -- arx plan \
  --intent "$repo_root/examples/intent.yaml" \
  --config-dir "$repo_root/assets/config/compositions"
```

If you need legacy provider aliases, `tinx init demo -p ghcr.io/sourceplane/arx:<tag> as ciz` and `tinx init demo -p ghcr.io/sourceplane/arx:<tag> as lite-ci` still work.

### Option 5: Using ORAS (OCI Registry As Storage)

```bash
# Pull the provider artifact
oras pull ghcr.io/sourceplane/arx:<tag>

# Extract binaries
tar -xzf arx_<tag>_linux_amd64_oci.tar.gz
./entrypoint plan -i intent.yaml
```

## Architecture

- Core overview: [docs/CORE-ARCHITECTURE.md](docs/CORE-ARCHITECTURE.md)
- Detailed design: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

### 6-Stage Compiler Pipeline

1. **Load & Validate** - Parse YAML, validate against JSON schemas
2. **Normalize** - Resolve wildcards, default missing fields
3. **Expand** - Environment × Component matrix with policy merging
4. **Bind Jobs** - Match components to job definitions, render templates
5. **Resolve Dependencies** - Convert component deps → job deps, detect cycles
6. **Materialize** - Output final plan with all references resolved

### Core Principles

- **Intent is policy-aware, not execution-specific**
- **Jobs define HOW, Intent defines WHAT**  
- **Plans are deterministic and execution-runtime agnostic**
- **Schema controls everything** - inputs, expansion, validation

## Project Structure

```
arx/
├── cmd/arx/
│   ├── main.go           # CLI entry point & command handlers
│   └── models.go         # Domain models and types
├── internal/
│   ├── model/            # Data structures
│   │   ├── intent.go     # Intent model
│   │   ├── job.go        # Job composition model
│   │   └── plan.go       # Output plan model
│   ├── loader/           # YAML parsing & loading
│   ├── schema/           # JSON Schema validation
│   ├── normalize/        # Intent canonicalization
│   ├── expand/           # Env × Component expansion
│   ├── planner/          # Dependency resolution & DAG
│   │   ├── graph.go      # Graph algorithms
│   │   └── planner.go    # Planner orchestration
│   └── render/           # Plan materialization
├── assets/
│   └── config/
│       ├── schemas/      # JSON schemas (intent, jobs, plan)
│       └── compositions/ # Job definitions
│           ├── charts/   # Helm Charts composition
│           ├── helm/     # Helm deployment composition
│           ├── helmCommon/ # Common Helm composition
│           └── terraform/ # Terraform composition
├── examples/
│   └── intent.yaml       # Example intent file
├── docs/
│   ├── ARCHITECTURE.md   # Detailed design docs
│   ├── RUNTIME_TOOLS.md  # Container runtime options
│   └── *.md             # Additional documentation
└── provider.yaml         # Provider manifest
```

## Quick Start

### 1. List Available Compositions

```bash
arx compositions --config-dir assets/config/compositions
```

Output shows all available job compositions (helm, terraform, charts, etc.)

### 2. Validate Intent File

```bash
arx validate \
  --intent examples/intent.yaml \
  --config-dir assets/config/compositions
```

### 3. Debug Intent Processing

See detailed logs of each compiler stage:

```bash
arx debug \
  --intent examples/intent.yaml \
  --config-dir assets/config/compositions
```

### 4. Generate Execution Plan

```bash
arx plan \
  --intent examples/intent.yaml \
  --config-dir assets/config/compositions \
  --output plan.json \
  --debug
```

Output: Fully resolved execution DAG in `plan.json`

## Usage Examples

### Using with Docker

```bash
docker run \
  -v $(pwd):/workspace \
  ghcr.io/sourceplane/arx:<tag> \
  plan \
  --intent /workspace/intent.yaml \
  --config-dir /workspace/assets/config/compositions \
  --output /workspace/plan.json
```

### Using with Podman (Recommended for CI/CD)

```bash
podman run \
  -v $(pwd):/workspace \
  ghcr.io/sourceplane/arx:<tag> \
  plan \
  --intent /workspace/intent.yaml \
  --config-dir /workspace/assets/config/compositions
```

### Using in Kubernetes

```bash
kubectl run arx-planner \
  --image=ghcr.io/sourceplane/arx:<tag> \
  --rm -it \
  -- plan \
  --intent intent.yaml \
  --config-dir /config/compositions
```

### Using in GitHub Actions

```yaml
- name: Generate CI Plan
  uses: docker://ghcr.io/sourceplane/arx:<tag>
  with:
    args: |
      plan \
      --intent intent.yaml \
      --config-dir assets/config/compositions \
      --output plan.json
```

    This container-based usage is separate from GitHub Actions compatibility mode during `arx run`. When a compiled plan contains `use:` steps, `arx run` auto-selects the GitHub Actions executor unless you explicitly set `--runner`, `ARX_RUNNER`, or a deprecated compatibility alias.

## Configuration Schemas

### Intent Schema

The intent file defines your desired deployment across environments.

**Example:**
```yaml
apiVersion: sourceplane.io/v1
kind: Intent
metadata:
  name: microservices-deployment
  description: Multi-environment microservices deployment

discovery:
  roots:
    - services/
    - infra/
    - deploy/

# Domain-level configuration
groups:
  platform:
    policies:
      isolation: strict
      approval_required: true
    defaults:
      timeout: 15m
      retries: 2
  
# Environment definitions
environments:
  production:
    selectors:
      domains: ["platform"]
    policies:
      region: us-east-1
  
  staging:
    defaults:
      replicas: 2

# Inline components still work when you need them
components:
  - name: component-charts
    type: charts
    domain: platform
    subscribe:
      environments: [development, staging, production]
    inputs:
      registry: mycompany.azurecr.io/helm/charts
```

External components can live next to the code they own. Each `component.yaml` is loaded from the configured discovery roots, and if `spec.path` is omitted arx defaults the job working directory to the directory containing the manifest.

**Example component.yaml:**
```yaml
apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: web-app

spec:
  type: helm
  domain: platform
  subscribe:
    environments: [development, staging, production]
  inputs:
    chart: oci://mycompany.azurecr.io/helm/charts/default
  dependsOn:
    - component: common-services
```

**Schema validation at:**
```
assets/config/schemas/intent.schema.yaml
```

### Job Composition Schema

Compositions define how to deploy components.

**Available Compositions:**
- **helm** - Helm chart deployments
- **terraform** - Infrastructure as code
- **charts** - Kubernetes manifests  
- **helmCommon** - Common Helm definitions

**Example Composition:**
```yaml
# assets/config/compositions/helm/job.yaml
apiVersion: sourceplane.io/v1
kind: JobRegistry
metadata:
  name: helm-jobs
  description: Deploy Helm charts
jobs:
  - name: deploy
    description: Deploy Helm charts
    runsOn: ubuntu-22.04
    timeout: 15m
    retries: 2
    steps:
      - name: add-repo
        run: helm repo add myrepo https://repo.example.com
      - name: deploy
        run: helm install {{.Component}} {{.chart}} --namespace={{.namespace}}
    inputs:
      chart: myrepo/chart
      namespace: default
```

### GitHub Actions Steps In Compositions

`arx` also supports GitHub Actions-style `use:` steps inside a composition. `arx run` auto-selects the GitHub Actions executor when the compiled plan contains any `use:` step, and you can still force it with `--gha`.

See the example files at:

- `examples/compositions/gha-helm/job.yaml`
- `examples/compositions/gha-helm/schema.yaml`

Example step definitions:

```yaml
steps:
  - id: setup-helm
    use: azure/setup-helm@v4.3.0
    with:
      version: "{{.helmVersion}}"
  - id: setup-kubectl
    use: azure/setup-kubectl@v4
    with:
      version: "{{.kubectlVersion}}"
  - name: deploy
    run: helm upgrade --install {{.Component}} {{.chart}} --namespace {{.namespacePrefix}}{{.Component}}
```

To use that example composition:

1. Copy `examples/compositions/gha-helm/` into your config directory.
2. Set the component type to `gha-helm`.
3. Execute the compiled plan with `arx run --plan plan.json --execute`.

Example component snippet:

```yaml
spec:
  type: gha-helm
  inputs:
    chart: oci://mycompany.azurecr.io/helm/charts/default
    helmVersion: v3.15.4
    kubectlVersion: v1.30.2
```

Supported `use:` forms in GHA mode:

- `owner/repo@ref`
- `owner/repo/path/to/action@ref`
- `./local-action`
- `docker://image`

For production usage, pin remote actions to a full commit SHA and make sure the runner machine has any required runtimes such as Node.js or Docker.

### Output Plan Schema

The generated plan is a fully resolved DAG.

**Structure:**
```json
{
  "apiVersion": "arx.io/v1",
  "kind": "Plan",
  "metadata": {
    "name": "microservices-deployment",
    "description": "...",
    "generatedAt": "2026-04-18T00:00:00Z",
    "checksum": "sha256-..."
  },
  "execution": {
    "concurrency": 4,
    "failFast": true,
    "stateFile": ".arx-state.json"
  },
  "spec": {
    "jobBindings": {
      "helm": "helm-jobs",
      "terraform": "terraform-jobs",
      "charts": "charts-jobs"
    }
  },
  "jobs": [
    {
      "id": "web-app@production.deploy",
      "name": "deploy",
      "component": "web-app",
      "environment": "production",
      "composition": "helm",
      "steps": [
        {
          "name": "deploy",
          "run": "helm install web-app repo/chart --namespace=production"
        }
      ],
      "dependsOn": ["common-services@production.deploy"],
      "timeout": "15m",
      "retries": 2,
      "env": {
        "image": "web-app:1.0",
        "replicas": 3,
        "namespace": "production"
      },
      "config": {
        "image": "web-app:1.0",
        "replicas": 3,
        "namespace": "production"
      }
    }
  ]
}
```

### Configuration Merging (Priority Order)

```
Low Priority  ← Overridden by ←  High Priority
1. Type defaults
2. Composition defaults  
3. Domain/Group defaults
4. Environment defaults
5. Component inputs (highest)
```

**Policy Rules:** Cannot be merged or overridden - enforced at all levels

## Compiler Pipeline Phases

### Phase 0: Load & Validate
- Parse `intent.yaml` and compositions
- Validate against JSON schemas
- Fail fast on schema violations

### Phase 1: Normalize
- Resolve component selectors
- Expand wildcards in domain/environment selectors
- Default missing fields
- Canonicalize dependency references

### Phase 2: Expand (Env × Component)
- For each environment, select matching components
- Skip disabled components
- Merge inputs according to precedence
- Validate policy constraints

### Phase 3: Job Binding
- Match component type → composition definition
- Create JobInstance per (component × environment)
- Render step templates with merged config

### Phase 4: Dependency Resolution  
- Convert component dependencies → job dependencies
- Handle same-environment and cross-environment deps
- Validate all dependencies exist

### Phase 5: DAG Validation
- Topological sort
- Detect cycles (error if found)
- Verify all references are concrete

### Phase 6: Materialize
- Render final `plan.json`
- All templates resolved
- All references concrete
- Ready for execution

## Command Reference

```bash
# List available compositions
arx compositions \
  --config-dir assets/config/compositions

# Validate intent without generating plan
arx validate \
  --intent intent.yaml \
  --config-dir assets/config/compositions

# Debug with detailed logging
arx debug \
  --intent intent.yaml \
  --config-dir assets/config/compositions

# Generate execution plan
arx plan \
  --intent intent.yaml \
  --config-dir assets/config/compositions \
  --output plan.json \
  --format json \
  --debug

# Preview execution from a compiled plan (dry-run)
arx run \
  --plan plan.json

# Execute plan steps
arx run \
  --plan plan.json \
  --execute

# Execute using the Docker backend
arx run \
  --plan plan.json \
  --execute \
  --runner docker

# Execute using GitHub Actions compatibility mode
arx run \
  --plan plan.json \
  --execute \
  --gha
```

**Flags:**
- `-i, --intent` - Path to intent YAML file
- `-c, --config-dir` - Path to compositions directory (required)
- `-o, --output` - Output plan file (default: plan.json)
- `-f, --format` - Output format: json or yaml (default: json)
- `--debug` - Enable verbose logging
- `-p, --plan` - Path to compiled plan file for `run`
- `-x, --execute` - Execute commands (without this, `run` is dry-run)
- `--gha` - Shortcut for GitHub Actions compatibility mode (`--runner github-actions`)
- `--runner` - Execution backend for `run`: `local`, `github-actions`, or `docker`

`run` selects its backend in this order:

1. `--gha`
2. `--runner`
3. `ARX_RUNNER` (`CIZ_RUNNER` and `LITECI_RUNNER` are still accepted as deprecated aliases)
4. Auto-detect `github-actions` when `GITHUB_ACTIONS=true`
5. Auto-detect `github-actions` when the compiled plan contains any `use:` step
6. Otherwise `local`

Runner notes:

- `local` uses the host shell and installed binaries.
- `github-actions` enables GitHub Actions-compatible `use:` steps, expression evaluation, file commands such as `GITHUB_ENV` and `GITHUB_OUTPUT`, and post-step handling for supported actions.
- `docker` runs each step in a fresh container, mounts the workspace at `/workspace`, and uses `job.runsOn` as the image. Common GitHub-style labels such as `ubuntu-22.04` map to `ubuntu:22.04`. If `runsOn` is empty, `ubuntu:22.04` is used.

## Troubleshooting

### "Config directory not found"
```bash
# Ensure path exists
ls -la assets/config/compositions/

# Use absolute path if relative doesn't work
arx plan -i intent.yaml -c $(pwd)/assets/config/compositions
```

### "Schema validation failed"
```bash
# Check your intent.yaml against the schema
arx validate -i intent.yaml -c assets/config/compositions
```

### "Circular dependency detected"
```bash
# Use debug mode to see dependency graph
arx debug -i intent.yaml -c assets/config/compositions
```

### Container authentication errors
```bash
# Login to GHCR
docker login ghcr.io
# or
podman login ghcr.io
```

## Performance

- **Typical plan generation:** < 1 second
- **Supported environments:** Unlimited
- **Supported components:** Unlimited  
- **Cycle detection:** O(V + E) with DFS
- **Topological sort:** O(V + E)

## Contributing

Contributions welcome! Areas:
- New composition types
- Schema enhancements
- Performance optimizations
- Documentation
- Container runtime support

## Resources

- **Documentation:** [docs/](docs/)
- **Examples:** [examples/](examples/)
- **Architecture:** [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
- **Runtime Tools:** [docs/RUNTIME_TOOLS.md](docs/RUNTIME_TOOLS.md)

## License

MIT License - See [LICENSE](LICENSE) file for details

## Support

- **Issues:** [GitHub Issues](https://github.com/sourceplane/arx/issues)
- **Discussions:** [GitHub Discussions](https://github.com/sourceplane/arx/discussions)
- **Email:** team@sourceplane.io
