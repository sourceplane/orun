#!/usr/bin/env bash
# Builds the workspace the README recording runs in (scripts/demo/record.py):
#
#   <dir>/platform        a copy of examples/ as a git repo: an orun-disciplined
#                         platform with intent, components, and golden paths
#   <dir>/acme-baseline   a small baseline as a git repo with two releases,
#                         v1.0.0 and v1.1.0 (the golden path moves to setup-node v5)
#
# usage: scripts/demo/setup.sh <dir>
set -euo pipefail
repo="$(cd "$(dirname "$0")/../.." && pwd)"
dir="${1:?usage: setup.sh <dir>}"
rm -rf "$dir"; mkdir -p "$dir"
gitc() { git -c user.email=demo@orun.dev -c user.name=demo -c init.defaultBranch=main "$@"; }

# The platform: examples/ as its own repository.
cp -r "$repo/examples" "$dir/platform"
rm -rf "$dir/platform/.orun"
(cd "$dir/platform" && gitc init -q && gitc add -A && gitc commit -qm "platform")

# The baseline: one golden path, a platform intent, and a service template.
b="$dir/acme-baseline"
mkdir -p "$b/platform/compositions/compositions" "$b/service"
cp -r "$repo/examples/compositions/compositions/cloudflare-worker" "$b/platform/compositions/compositions/"
cat > "$b/platform/compositions/stack.yaml" <<'YAML'
apiVersion: orun.io/v1
kind: Stack
metadata:
  name: acme-golden-paths
  version: 1.0.0
  description: Acme's golden paths.
  owner: acme
YAML
cat > "$b/platform/intent.yaml" <<'YAML'
apiVersion: sourceplane.io/v1
kind: Intent
metadata:
  name: acme-platform

compositions:
  sources:
    - name: acme-golden-paths
      kind: dir
      path: ./compositions

discovery:
  roots: [apps/]

automation:
  triggerBindings:
    pull-request:
      on: { provider: github, event: pull_request, baseBranches: [main] }
      plan: { scope: changed, base: pull_request.base.sha, head: pull_request.head.sha }
    push-main:
      on: { provider: github, event: push, branches: [main] }
      plan: { scope: changed, base: before, head: after }

environments:
  staging:
    activation: { triggerRefs: [pull-request, push-main] }
  production:
    activation: { triggerRefs: [push-main] }
    promotion:
      dependsOn:
        - environment: staging
YAML
cat > "$b/service/component.yaml" <<'YAML'
apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: {{ .serviceName }}
spec:
  type: cloudflare-worker
  domain: commerce
  subscribe:
    environments:
      - name: staging
        profile: verify
      - name: production
        profile: deploy
  parameters:
    installCommand: pnpm install --frozen-lockfile
    buildCommand: pnpm run build
    deployCommand: pnpm run deploy
    nodeVersion: "20"
    productionBranch: main
YAML
blueprint() {
cat > "$b/blueprint.yaml" <<YAML
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: acme-platform

inputs:
  serviceName: { type: string, pattern: "^[a-z][a-z0-9-]*\$", required: true }

sources:
  - name: baseline
    kind: git
    repo: acme-baseline
    ref: $1

modules:
  - name: platform        # the standards: intent and golden paths
    mode: copy
    source: baseline
    from: platform
    to: .
  - name: service         # a first service, already on a golden path
    mode: template
    source: baseline
    from: service
    to: apps/{{ .serviceName }}
    bind: [component.yaml]
    dependsOn: [platform]

phases:
  - name: standards
    modules: [platform]
  - name: services
    modules: [service]
YAML
}
blueprint v1.0.0
(cd "$b" && gitc init -q && gitc add -A && gitc commit -qm "acme baseline v1.0.0" && gitc tag v1.0.0)
sed -i 's|actions/setup-node@v4|actions/setup-node@v5|' \
  "$b/platform/compositions/compositions/cloudflare-worker/jobs/cloudflare-worker-verify-deploy.yaml"
sed -i 's|^  version: 1.0.0|  version: 1.1.0|' "$b/platform/compositions/stack.yaml"
blueprint v1.1.0
(cd "$b" && gitc commit -qam "acme baseline v1.1.0: setup-node v5" && gitc tag v1.1.0 && git checkout -q v1.0.0)
echo "demo workspace ready in $dir"
