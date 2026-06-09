---
title: orun gc
---

`orun gc` reclaims disk by removing objects in the [object model](../concepts/state-model.md)
that are no longer reachable from any ref, under a retention policy.

## Usage

```bash
orun gc
```

By default, `gc` keeps the most recent 10 executions per scope and sweeps every
object not reachable from a retained ref. It is a mark-and-sweep — it deletes only
proven-unreachable objects — and reports the number of objects removed and
execution refs pruned, then reindexes.

## Common examples

Preview what would be removed without deleting anything:

```bash
orun gc --dry-run
```

Keep only the last 5 executions:

```bash
orun gc --keep 5
```

Remove executions older than 7 days:

```bash
orun gc --max-age 7
```

Remove all execution records:

```bash
orun gc --all
```

## Flags

| Flag | Meaning |
| --- | --- |
| `--dry-run` | Print what would be deleted without removing anything |
| `--keep` | Number of most recent executions to retain (default: `10`) |
| `--max-age` | Maximum age in days for retained executions (default: `30`) |
| `--all` | Remove all executions regardless of count or age |

## What gets removed

`gc` prunes execution refs beyond the `--keep` count (all of them with `--all`),
then sweeps every object — execution, job, step, log, plan, revision, and catalog
node — that is no longer reachable from a surviving ref. Objects shared with a
retained execution (by content addressing) are kept. Refs that are still current —
`catalogs/current`, `executions/latest`, and the named source/catalog refs — and
everything reachable from them are never removed.
