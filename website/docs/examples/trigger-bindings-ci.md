---
title: Trigger-aware CI with GitHub Actions
---

This example shows how to use trigger bindings to automatically scope CI plans based on the GitHub event type — pull requests plan development, pushes to main plan staging, release tags plan production.

## Intent file with triggers

```yaml
apiVersion: sourceplane.io/v1
kind: Intent

metadata:
  name: my-platform

compositions:
  sources:
    - name: platform-compositions
      kind: dir
      path: ./compositions

discovery:
  roots:
    - apps/
    - infra/

automation:
  triggerBindings:
    github-pull-request:
      description: PR validation
      on:
        provider: github
        event: pull_request
        actions: [opened, synchronize, reopened]
        baseBranches: [main]
      plan:
        scope: changed
        base: pull_request.base.sha
        head: pull_request.head.sha

    github-push-main:
      description: Post-merge verification
      on:
        provider: github
        event: push
        branches: [main]
      plan:
        scope: changed
        base: before
        head: after

    github-tag-release:
      description: Release
      on:
        provider: github
        event: push
        tags: ["v*"]
      plan:
        scope: full

environments:
  development:
    activation:
      triggerRefs: [github-pull-request]
    parameterDefaults:
      "*":
        namespacePrefix: dev-

  staging:
    activation:
      triggerRefs: [github-push-main]
    parameterDefaults:
      "*":
        namespacePrefix: stg-

  production:
    activation:
      triggerRefs: [github-tag-release]
    parameterDefaults:
      "*":
        namespacePrefix: prod-
```

## GitHub Actions workflow

```yaml
name: CI

on:
  pull_request:
  push:
    branches: [main]
    tags: ["v*"]

permissions:
  contents: read
  id-token: write

jobs:
  plan:
    name: Orun Plan
    runs-on: ubuntu-latest
    outputs:
      job-matrix: ${{ steps.plan.outputs.job-matrix }}
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: sourceplane/orun-action@v1

      - name: Plan
        id: plan
        run: |
          orun plan \
            --from-ci github \
            --event-file "$GITHUB_EVENT_PATH" \
            --output plan.json
          echo "job-matrix=$(jq -c '[.jobs[] | {"job-id": .id, "job-name": .checkName}]' plan.json)" >> $GITHUB_OUTPUT

      - uses: actions/upload-artifact@v4
        with:
          name: orun-plan
          path: plan.json

  execute:
    name: ${{ matrix.job-name }}
    needs: plan
    if: needs.plan.outputs.job-matrix != '[]'
    runs-on: ubuntu-latest
    strategy:
      fail-fast: false
      matrix:
        include: ${{ fromJson(needs.plan.outputs.job-matrix) }}
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/download-artifact@v4
        with:
          name: orun-plan

      - uses: sourceplane/orun-action@v1

      - name: Execute
        run: |
          orun run \
            --plan plan.json \
            --runner github-actions \
            --job "${{ matrix.job-id }}"
```

## What happens for each event type

| Event | Matched trigger | Active environments | Scope |
| --- | --- | --- | --- |
| Pull request to main | `github-pull-request` | development | changed |
| Push to main | `github-push-main` | staging | changed |
| Push tag `v1.2.0` | `github-tag-release` | production | full |

## Local simulation

Reproduce CI behavior locally without a real event:

```bash
# Simulate PR planning
orun plan --trigger github-pull-request --base main --head HEAD

# Simulate staging deployment
orun plan --trigger github-push-main --base main~1 --head main

# Simulate release
orun plan --trigger github-tag-release
```

CLI `--base` and `--head` override trigger-derived values, so you can test with any ref range.
