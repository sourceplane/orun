#!/usr/bin/env bash
# Builds the workspace the README recording runs in (scripts/demo/record.py):
#
#   <dir>/saas-baseline   a baseline: Acme's golden path for Node services
#                         (test, build, package as plain shell steps, so a
#                         local `orun run` does real work), a platform intent
#                         with staging → production promotion, and two
#                         services, api and web.
#
# The recording then runs, from <dir>:
#   orun new --blueprint saas-baseline/blueprint.yaml --out acme-shop --set name=acme-shop
#   cd acme-shop; orun plan; orun run <plan id>
#
# usage: scripts/demo/setup.sh <dir>
set -euo pipefail
dir="${1:?usage: setup.sh <dir>}"
rm -rf "$dir"; mkdir -p "$dir"
b="$dir/saas-baseline"
gp="$b/platform/stack/compositions/node-service"
mkdir -p "$gp/jobs" "$gp/profiles" "$b/product/apps/api" "$b/product/apps/web"

# --- the golden path -------------------------------------------------------
cat > "$b/platform/stack/stack.yaml" <<'YAML'
apiVersion: orun.io/v1
kind: Stack
metadata:
  name: acme-golden-paths
  version: 1.4.0
  description: How Acme builds and ships services.
  owner: acme-platform
YAML
cat > "$gp/composition.yaml" <<'YAML'
apiVersion: sourceplane.io/v1alpha1
kind: Composition
metadata:
  name: node-service
spec:
  type: node-service
  description: Test, build, and package a Node.js service
  schemaRef: { name: node-service-component }
  defaultJob: ship
  defaultProfile: verify
  jobs:
    - name: ship
      templateRef: { name: node-service-ship }
  profiles:
    - name: verify
      profileRef: { name: node-service-verify }
    - name: release
      profileRef: { name: node-service-release }
YAML
cat > "$gp/schema.yaml" <<'YAML'
apiVersion: sourceplane.io/v1alpha1
kind: ComponentSchema
metadata:
  name: node-service-component
spec:
  type: node-service
  schema:
    $schema: http://json-schema.org/draft-07/schema#
    type: object
    required: [name, type, inputs]
    properties:
      inputs:
        type: object
        required: [port]
        properties:
          port: { type: integer }
        additionalProperties: false
YAML
cat > "$gp/jobs/node-service-ship.yaml" <<'YAML'
apiVersion: sourceplane.io/v1alpha1
kind: JobTemplate
metadata:
  name: node-service-ship
spec:
  description: Test, build, and package a Node.js service
  timeout: 10m
  capabilities: [node-service.test, node-service.build, node-service.package]
  steps:
    - id: test
      name: test
      capability: node-service.test
      run: node --test
    - id: build
      name: build
      capability: node-service.build
      run: mkdir -p dist && cp server.js dist/
    - id: package
      name: package
      capability: node-service.package
      run: tar czf dist/{{.orun.component.name}}-{{.orun.environment.name}}.tgz -C dist server.js
YAML
cat > "$gp/profiles/node-service-verify.yaml" <<'YAML'
apiVersion: sourceplane.io/v1alpha1
kind: ExecutionProfile
metadata:
  name: node-service-verify
spec:
  jobs:
    ship:
      includeCapabilities: [node-service.test, node-service.build]
YAML
cat > "$gp/profiles/node-service-release.yaml" <<'YAML'
apiVersion: sourceplane.io/v1alpha1
kind: ExecutionProfile
metadata:
  name: node-service-release
spec:
  jobs:
    ship:
      includeCapabilities: [node-service.test, node-service.build, node-service.package]
YAML

# --- the product -----------------------------------------------------------
cat > "$b/product/intent.yaml" <<'YAML'
apiVersion: sourceplane.io/v1
kind: Intent
metadata:
  name: {{ .name }}
compositions:
  sources:
    - name: acme-golden-paths
      kind: dir
      path: ./stack
discovery:
  roots: [apps/]
environments:
  staging: {}
  production:
    promotion:
      dependsOn:
        - environment: staging
YAML
printf '.orun/\nnode_modules/\ndist/\n' > "$b/product/.gitignore"
for svc in api web; do
  port=8080; [ "$svc" = web ] && port=3000
  cat > "$b/product/apps/$svc/component.yaml" <<YAML
apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: $svc
spec:
  type: node-service
  domain: shop
  subscribe:
    environments:
      - { name: staging, profile: verify }
      - { name: production, profile: release }
  parameters:
    port: $port
YAML
  cat > "$b/product/apps/$svc/server.js" <<JS
const http = require('node:http');
const handler = (req, res) => res.end(JSON.stringify({ service: '$svc', ok: true }));
module.exports = { handler };
if (require.main === module) http.createServer(handler).listen(process.env.PORT || $port);
JS
  cat > "$b/product/apps/$svc/server.test.js" <<JS
const test = require('node:test');
const assert = require('node:assert');
const { handler } = require('./server.js');
test('$svc answers ok', () => {
  let body = '';
  handler({}, { end: (b) => { body = b; } });
  assert.deepStrictEqual(JSON.parse(body), { service: '$svc', ok: true });
});
JS
done
cat >> "$b/product/apps/web/component.yaml" <<'YAML'
  dependsOn:
    - component: api
YAML

# --- the blueprint ---------------------------------------------------------
cat > "$b/blueprint.yaml" <<'YAML'
apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: saas-baseline
inputs:
  name: { type: string, pattern: "^[a-z][a-z0-9-]*$", required: true }
sources:
  - name: baseline
    kind: dir
    path: .
modules:
  - name: standards       # the golden paths and platform rules
    mode: copy
    source: baseline
    from: platform
    to: .
  - name: product         # the intent and the first services
    mode: template
    source: baseline
    from: product
    to: .
    bind: [intent.yaml]
    dependsOn: [standards]
phases:
  - name: standards
    modules: [standards]
  - name: product
    modules: [product]
YAML
echo "demo workspace ready in $dir"
