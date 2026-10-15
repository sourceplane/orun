---
title: orun cache
description: List the caches orun keeps with their size and last use, remove the ones not used for a while, and keep the whole of ~/.orun under a size budget.
---

`orun cache` lists, prunes and bounds the caches orun keeps on the machine
and in the checkout. Without it they only grew: one directory per action
version, per tool version, per composition digest and per run, with no age,
no budget and no command that removed them.

## Usage

```bash
orun cache ls
orun cache prune [--older-than 30d] [--kind <kind>] [--dry-run]
orun cache gc [--max-size 10G] [--dry-run]
```

## What it covers

| Kind | Where | One entry is |
| --- | --- | --- |
| `actions` | `~/.orun/actions/<owner>/<repo>/<sha>` | a remote action at one commit |
| `tool-cache` | `~/.orun/tool-cache/<tool>/<version>` (or the host runner's tool cache) | one version of a tool a setup action installed |
| `shared` | `~/.orun/actions/shared/<store>` | a store every job shares: `npm`, `pnpm-store`, `terraform-plugins`, `go-build`, … |
| `declared` | `~/.orun/actions/shared/caches/<name>` | a cache a composition step declared with `caches:` |
| `actions-cache` | `~/.orun/actions/shared/actions-cache/<entry>` | an entry `actions/cache` saved, listed by its key |
| `compositions` | `~/.orun/cache/compositions/<digest>` | a composition package pulled from an archive or an OCI registry (a `dir` source is read in place and never cached) |
| `runs` | `<checkout>/.orun/runs/<exec-id>` | a run's staging directory |
| `resolve-memo` | `<checkout>/.orun/objectmodel/cache/resolve/<key>` | one memoized catalog resolve |

The two checkout kinds appear when the working directory (or `--intent`'s)
holds a `.orun` directory.

An entry's *last use* is the stamp orun writes beside it on a hit (an action
loaded from the cache, a composition served from it, a declared cache
restored), else the entry's own modification time. An entry a running orun
process holds — an action being fetched, a declared cache or an
`actions/cache` entry being saved, a Terraform plugin cache a job has, a
composition being filled, any tool version while an installer runs — is
marked `in use` and never removed.

## ls

```bash
orun cache ls
orun cache ls --kind tool-cache
orun cache ls --json
```

Every entry with its kind, size and last use, oldest first within a kind,
followed by a total per kind.

## prune

```bash
orun cache prune                      # not used for 30 days
orun cache prune --older-than 7d --kind runs
orun cache prune --older-than 12h --dry-run
```

Removes the entries last used before the given age, of every kind or one.
`--dry-run` lists them and removes nothing.

## gc

```bash
orun cache gc                         # keep ~/.orun under 10 GB
orun cache gc --max-size 2G --dry-run
```

Keeps the machine-wide kinds (everything but `runs` and `resolve-memo`)
under the budget, removing the least recently used entries first until the
rest fit. Entries in use count against the budget and are never removed.

## Flags

| Command | Flag | Meaning |
| --- | --- | --- |
| `ls` | `--kind` | Only this kind |
| `ls` | `--json` | The entries as JSON (`kind`, `name`, `path`, `size`, `lastUsed`, `inUse`) |
| `prune` | `--older-than` | Age past which an entry is removed: `30d` (default), `12h`, `90m` |
| `prune` | `--kind` | Only this kind |
| `prune`, `gc` | `--dry-run` | Print what would be removed without removing anything |
| `gc` | `--max-size` | The budget: `10G` (default), `500M`, `1.5G` |

Related: [`orun gc`](./orun-gc.md) prunes the object model's executions,
which `orun cache` leaves alone; the caches themselves are described under
[Runners](../execute/runners.md#shared-caches).
