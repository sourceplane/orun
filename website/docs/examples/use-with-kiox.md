---
title: Use with kiox
---

`orun` can run as an OCI-distributed provider inside a `kiox` workspace.

## Initialize a workspace

```bash
kiox init demo -p ghcr.io/sourceplane/orun:<tag> as orun
```

## Run orun through the workspace

```bash
repo_root="$(pwd)"

kiox --workspace demo -- orun plan \
  --intent "$repo_root/examples/intent.yaml" \
  --output "$repo_root/plan.json"
```

## Why the paths are absolute

When `orun` runs inside `kiox`, workspace-run provider commands resolve relative paths against the workspace root. Use an absolute repository path for the intent file when the source lives outside the workspace. The composition package path can stay relative to the intent because it is resolved from the intent location.