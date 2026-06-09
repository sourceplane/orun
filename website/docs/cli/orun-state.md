---
title: orun state (removed)
---

:::caution Removed in v2.15.0
The `orun state` command — including the hidden `orun state migrate` — has been
**removed**. It existed only to rehome workspaces into the legacy revision/catalog
file store, which was retired when the content-addressed **object model** became
the single persistence stack. There is no migration target anymore, so the command
no longer has a purpose.
:::

## What replaced it

orun records all of its state — sources, catalogs, revisions, executions, jobs,
steps, and logs — in the object model under `.orun/objectmodel/`. Nothing needs to
be migrated into it: `orun plan`, `orun run`, and `orun catalog refresh` write it
directly, and every read command resolves through it.

- **Inspect state** with `orun status`, `orun logs`, `orun describe`, `orun get`,
  and the `orun catalog` group — all read the object model.
- **Reclaim space** with [`orun gc`](../concepts/state-model.md#garbage-collection),
  which mark-and-sweeps unreferenced objects under a retention policy. This is the
  only maintenance command the object model needs.

## Upgrading an old workspace

A workspace that still has a pre-v2.15.0 `.orun/plans/` or `.orun/executions/`
tree (or a v2.10–v2.14 `.orun/revisions/` tree) does **not** need migrating — orun
ignores those paths. You can delete them once you no longer need the historical
files. The object model itself is unchanged across the cutover, so recent catalogs
and executions remain readable.

## Related

- [State model](../concepts/state-model.md) — the object-model layout on disk.
- [v2.15.0 release notes](../release-notes/v2.15.0.md) — the retirement.
