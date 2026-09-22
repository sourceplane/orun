---
title: orun intent
description: Inspect the effective intent — what each inherited preset contributes, and the fully merged document the planner actually sees.
---

`orun intent` shows you the intent **after** inheritance. An `intent.yaml`
that lists Stack presets under `extends:` is not the document the planner
compiles; the effective intent is the merge of every preset, in order, with
the repository's own file applied last. These two subcommands make that
merge visible: `explain` attributes each merged field to the preset that
contributed it, and `render` prints the merged document.

```bash
orun intent explain
orun intent render [-o <file>]
```

Both honor the global flags: `-i, --intent <path>` picks the intent file
(auto-discovered when unset; default `intent.yaml`) and `-c, --config-dir`
points at the job definitions. Presets are resolved through the composition
sources the intent declares, so a preset that cannot be resolved is an error
here, exactly as it would be at plan time.

## `explain`

Lists the presets in merge order, then the fields each one contributed,
grouped by preset. When the intent has no `extends:` it says so — `No
presets applied. The intent is used as-is.` — and exits `0`.

```text
$ orun intent explain

Intent Presets

  ● aws-platform-stack:standard
    standard
  ● aws-platform-stack:github-actions
    github-actions

Contributions

  aws-platform-stack:github-actions
    ├─ automation.triggerBindings.push
    ├─ discovery.roots

  aws-platform-stack:standard
    ├─ env.ORG
    ├─ environments.dev
    ├─ environments.prod
```

Colour is used when standard output is a terminal.

## `render`

Marshals the fully merged intent to YAML. When presets were applied the
document opens with the comment `# Effective intent (presets merged)`; an
intent with no `extends:` renders unchanged and without the header.

| Flag | Meaning |
|---|---|
| `-o, --output <file>` | Write to a file instead of standard output. The command then prints `Effective intent written to <file>`. |

```bash
orun intent render
orun intent render -o /tmp/effective-intent.yaml
```

Rendering is a read: nothing under `.orun/` is written, and the source
`intent.yaml` is never modified. Use it to diff what the planner sees
against what you authored, or to hand a self-contained intent to someone
without the Stack.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Explained or rendered. |
| `1` | The intent could not be loaded, an `extends:` reference is invalid, a preset could not be resolved or merged, or the output file could not be written. |

## Related

- [Intent model](../concepts/intent-model.md)
- [Intent presets](../concepts/intent-presets.md) — authoring presets and the merge rules
- [`orun validate`](./orun-validate.md), [`orun plan`](./orun-plan.md)
