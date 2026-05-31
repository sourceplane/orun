---
title: orun state
---

`orun state` exposes maintenance commands for the on-disk state store.
The command is **hidden** from `orun --help` because end-users rarely
need it; tooling and migration scripts call it directly.

## Subcommands

### `orun state migrate`

Rehomes a pre-v2.10.0 `.orun/` workspace into the new revision-first
layout described in [State model](../concepts/state-model.md).

For every legacy `.orun/plans/<sha>.json`, the command:

1. Synthesizes a `system.migrated` `TriggerOccurrence` whose
   `triggerKey` reflects the plan's checksum.
2. Resolves the corresponding `PlanRevision` via the same
   `internal/revision.ResolveRevision` path used by live planning, so
   the revision key is identical to one produced by a fresh plan with
   the same content.
3. Walks `.orun/executions/` for any execution whose stored
   `planChecksum` matches the legacy plan, attaches it to the new
   revision under `revisions/<key>/executions/run-NNN/`, and promotes
   the legacy `state.json` / `metadata.json` through
   `executionstate.Bridge.MirrorRunnerOutput`.
4. Updates `refs/` and `indexes/` so resolver lookups find the rehomed
   revisions and executions.

Executions whose plan hash is no longer present in `.orun/plans/`
attach to an `ensureUnknownRevision` placeholder so no run is dropped.

#### Usage

```bash
orun state migrate           # apply
orun state migrate --dry-run # preview
```

The command is **idempotent** — running it twice produces identical
output. It is also safe to run after partial migrations: anything
already rehomed under the new layout is skipped.

#### When you need it

- You are upgrading a long-running workspace from a pre-v2.10.0 release
  and want `orun status`, `orun describe revision latest`, and
  `orun get plans` to surface historical runs.
- You are scripting a bulk import of plans/executions captured from a
  CI artifact archive.

You do **not** need to migrate to keep using orun — the state store
keeps reading legacy `.orun/plans/` and `.orun/executions/` paths
transparently. Migration upgrades the surface; it is not a correctness
prerequisite.

## Flags

| Flag | Meaning |
| --- | --- |
| `--dry-run` | Print every write that would occur, then exit without touching disk. |

## Related

- [State model](../concepts/state-model.md) — the layout you are
  migrating into.
- [`orun describe revision`](./orun-describe.md) — the read surface that
  benefits most from migration.
