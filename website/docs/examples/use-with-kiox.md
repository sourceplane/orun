---
title: Use with kiox
---

`gluon` can run as an OCI-distributed provider inside a `kiox` workspace.

## Initialize a workspace

```bash
kiox init demo -p ghcr.io/sourceplane/gluon:<tag> as gluon
```

## Run gluon through the workspace

```bash
repo_root="$(pwd)"

kiox --workspace demo -- gluon plan \
  --intent "$repo_root/examples/intent.yaml" \
  --output "$repo_root/plan.json"
```

## Why the paths are absolute

When `gluon` runs inside `kiox`, workspace-run provider commands resolve relative paths against the workspace root. Use an absolute repository path for the intent file when the source lives outside the workspace. The composition package path can stay relative to the intent because it is resolved from the intent location.