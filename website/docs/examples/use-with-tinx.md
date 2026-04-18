---
title: Use with tinx
---

`arx` can run as an OCI-distributed provider inside a `tinx` workspace.

## Initialize a workspace

```bash
tinx init demo -p ghcr.io/sourceplane/arx:<tag> as arx
```

## Run arx through the workspace

```bash
repo_root="$(pwd)"

tinx --workspace demo -- arx plan \
  --intent "$repo_root/examples/intent.yaml" \
  --config-dir "$repo_root/assets/config/compositions" \
  --output "$repo_root/plan.json"
```

## Why the paths are absolute

When `arx` runs inside `tinx`, workspace-run provider commands resolve relative paths against the workspace root. Use absolute repository paths for intent files and composition directories when the source lives outside the workspace.

If you need a legacy provider alias, initialize the workspace with `as ciz` or `as lite-ci` instead.