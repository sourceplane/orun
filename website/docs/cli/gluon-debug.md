---
title: gluon debug
---

`gluon debug` traces intent processing so you can inspect what the planner is doing before it materializes a final plan.

## Usage

```bash
gluon debug \
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
gluon debug -i examples/intent.yaml
```

## Flags

| Flag | Meaning |
| --- | --- |
| `--intent`, `-i` | Intent file path |

If you need a final artifact after debugging, switch back to [gluon plan](./gluon-plan.md). `--config-dir` remains available as a global legacy fallback when the intent does not declare packaged sources.