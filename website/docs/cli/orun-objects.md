---
title: orun objects
description: Plumbing for the content-addressed object graph under .orun/objectmodel — read objects and refs, verify integrity, check out a closure, sync with a remote store, and garbage-collect.
---

`orun objects` is the porcelain over the workspace's **object model**: the
content-addressed graph under `.orun/objectmodel/` where every source
snapshot, catalog, revision, execution, job, step, log, agent type, and agent
session lives as an immutable object named by the hash of its content, with
mutable **refs** on top. The everyday commands (`orun plan`, `orun run`,
`orun status`, `orun logs`) read and write this graph for you; these
subcommands are for looking at it directly, repairing it, moving it, and
reclaiming space.

```bash
orun objects cat       <id|ref>
orun objects ls-tree   <id|ref>
orun objects rev-parse <ref>
orun objects log
orun objects show      [ref]
orun objects checkout  [ref]
orun objects fsck
orun objects reindex
orun objects gc        [--dry-run] [--keep N] [--grace <duration>]
orun objects push      <remote-dir> [ref]
orun objects pull      <remote-dir> [ref]
```

Wherever a command takes `<id|ref>`, a literal object id (`sha256:<hex>`) is
used as-is and anything else is read as a ref name. Refs you will reach for:

| Ref | Points at |
|---|---|
| `catalogs/current` | The latest resolved component catalog |
| `revisions/latest`, `revisions/by-hash/<planHash>` | A plan revision (trigger + plan) |
| `executions/latest`, `executions/by-id/<execId>` | A sealed execution |
| `agents/types/<name>/latest` | A sealed agent type |
| `agents/sessions/<id>` | A sealed agent session |
| `agents/literacy/v2` | The sealed base literacy |

## Reading

### `cat`

Prints an object's body. A JSON blob is re-indented for readability; a
non-JSON blob or a tree is printed verbatim.

```bash
orun objects cat executions/latest
orun objects cat sha256:3f9c…
```

### `ls-tree`

Lists a tree object's entries as `<kind> <name> <id>`, one per line — the
Merkle node linking a record to its children.

```text
$ orun objects ls-tree revisions/latest
blob   revision.json sha256:…
blob   plan.json     sha256:…
```

### `rev-parse`

Resolves a ref name (or passes an id through) to an object id. Useful in
scripts that want to pin what "latest" meant at a point in time.

```bash
rev=$(orun objects rev-parse revisions/latest)
```

### `log`

Lists executions newest-first: live runs first (marked `(live)`), then
sealed ones, with status, start time, the revision they ran, and a job
count.

```text
$ orun objects log
exec_01J8…               succeeded 2026-09-21T10:14:02Z rev=3f9c… jobs=4/4
exec_01J7…               failed    2026-09-20T17:40:11Z rev=3f9c… jobs=2/4
```

### `show`

Shows one execution's jobs, attempts, and steps — from the live working tree
while it is in flight, from the sealed tree otherwise. Defaults to
`executions/latest`.

```text
$ orun objects show executions/by-id/exec_01J8…
execution exec_01J8…
  status:   succeeded
  revision: 3f9c…
  started:  2026-09-21T10:14:02Z
  finished: 2026-09-21T10:16:48Z
  jobs:     4/4 succeeded, 0 failed; 11 steps
  • build-api [succeeded]
      - checkout [succeeded]
      - test [succeeded] (log)
  …
```

### `checkout`

Materializes a readable checkout of an object closure — trees become
directories, blobs become files (JSON re-indented) — under
`.orun/objectmodel/current/<ref>`, with the ref name folded into one safe
path segment. Defaults to `revisions/latest`. The checkout is a cache: delete
and rebuild it freely.

```bash
orun objects checkout executions/latest
```

## Maintaining

### `fsck`

Verifies the store: every object re-hashes to its id, and every ref's
closure is complete. Problems are printed one per line and the command exits
non-zero with `fsck: N problem(s) found`; a clean store prints
`ok: object graph healthy`.

### `reindex`

Rebuilds the derived indexes (the execution listing `log` and the cockpit
read) from refs and objects alone, and reports how many executions it
indexed. Run it after a manual repair or an interrupted operation.

### `gc`

A **reachability mark-and-sweep**: marks the closure reachable from every
surviving ref, then deletes every object that is neither marked nor inside
the grace window.

| Flag | Meaning |
|---|---|
| `--dry-run` | Report what would be removed without deleting. |
| `--keep N` | Retention first: prune the `executions/by-id/*` refs of all but the newest `N` executions so their closures become unreachable. `0` (the default) keeps every execution. |
| `--grace <duration>` | Never sweep an object written within this window (default `1h`), so an in-flight seal whose ref has not moved yet is safe. |

```text
$ orun objects gc --keep 20 --dry-run
gc: scanned=4812 marked=3977 pruned-exec-refs=6 swept=835 skipped-grace=0 dry-run=true
```

Collection is safe to interrupt: it deletes only proven-unreachable objects,
so a partial run leaves a valid store. After a real run the indexes are
rebuilt.

#### `objects gc` and `orun gc`

Both run the same collector. [`orun gc`](./orun-gc.md) is the
**execution-cleanup front**: it speaks in retention policy — keep the last
`N` executions (default `10`), `--all` — and applies no grace window.
`orun objects gc` is the collector itself, exposed with the knobs a store
operator wants: keep everything unless told otherwise, and a grace window
that defaults to an hour. Reach for `orun gc` to tidy history and for
`objects gc` when you are repairing or shrinking the store deliberately.

## Syncing

`push` and `pull` copy a ref's closure between this store and a **remote
store on the filesystem** — a directory holding the same `objects/` and
`refs/` layout. Because ids are content hashes the transfer is a set
difference: objects the other side already has are skipped, and the ref is
moved last. The ref defaults to `executions/latest`.

```text
$ orun objects push /mnt/share/orun-store executions/latest
pushed executions/latest: closure=212 copied=38 skipped=174 ref-moved=true
```

```bash
orun objects pull /mnt/share/orun-store executions/by-id/exec_01J8…
```

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success, or (`fsck`) a healthy graph. |
| `1` | A ref or id that does not resolve, an unreadable store, a failed sync, or (`fsck`) problems found. |

## Related

- [State model](../concepts/state-model.md) — the on-disk layout and the refs
- [`orun gc`](./orun-gc.md) — retention-driven cleanup
- [`orun status`](./orun-status.md), [`orun logs`](./orun-logs.md)
