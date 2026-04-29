---
title: orun debug
---

`orun debug` traces intent processing so you can inspect what the planner is doing before it materializes a final plan.

## Usage

```bash
orun debug \
  --intent intent.yaml
```

## What it is for

Use `debug` when you need to inspect:

- normalized intent shape
- environment and component expansion
- dependency resolution issues
- composition binding behavior

## Example

```bash
orun debug -i examples/intent.yaml
```

## Flags

| Flag | Meaning |
| --- | --- |
| `--intent`, `-i` | Intent file path |

If you need a final artifact after debugging, switch back to [orun plan](./orun-plan.md). `--config-dir` remains available as a global legacy fallback when the intent does not declare packaged sources.