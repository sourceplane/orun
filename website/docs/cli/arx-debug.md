---
title: arx debug
---

`arx debug` traces intent processing so you can inspect what the planner is doing before it materializes a final plan.

## Usage

```bash
arx debug \
  --intent intent.yaml \
  --config-dir assets/config/compositions
```

## What it is for

Use `debug` when you need to inspect:

- normalized intent shape
- environment and component expansion
- dependency resolution issues
- composition binding behavior

## Example

```bash
arx debug -i examples/intent.yaml -c assets/config/compositions
```

## Flags

| Flag | Meaning |
| --- | --- |
| `--intent`, `-i` | Intent file path |
| `--config-dir`, `-c` | Global flag used to load compositions |

If you need a final artifact after debugging, switch back to [arx plan](./arx-plan.md).