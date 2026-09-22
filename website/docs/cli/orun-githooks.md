---
title: orun githooks
description: Install the repository hook that stamps the Orun-Task trailer on every commit made on a task branch, so provenance costs nothing to keep.
---

`orun githooks` installs repository hooks that keep **provenance**
effortless. The [provenance pen](./orun-pr.md) expects two things from a
task's commits: the branch follows the grammar `orun/<task-key>-<slug>`,
and each commit carries an `Orun-Task: <KEY>` trailer naming the task. The
branch is the pen's job; the trailer is this hook's. With it installed, you
commit as usual and the trailer is stamped for you — and `orun pr check`
stops flagging commits that lack it.

```bash
orun githooks install [--force]
```

## `install`

Writes a `commit-msg` hook into the current repository's hooks directory
(`$(git rev-parse --git-dir)/hooks/commit-msg`) and marks it executable.
On every commit the hook:

1. Reads the current branch. If it is not an `orun/*` branch, it does
   nothing.
2. Extracts the task key from the branch name — the grammar's key forms are
   typed (`PAY-T14`), legacy (`ORN-142`), and triage (`WRK-9`). No key, no
   change.
3. Appends `Orun-Task: <KEY>` to the commit message unless an `Orun-Task:`
   line is already there, so amends and rebases never stack trailers.

| Flag | Meaning |
|---|---|
| `--force` | Replace an existing `commit-msg` hook that is not orun's. Without it, a foreign hook is left alone and the command fails, naming the file. |

The hook carries an `# orun githooks` marker; re-running `install` over
orun's own hook is always allowed and simply rewrites it.

```text
$ orun githooks install
installed .git/hooks/commit-msg — commits on orun/* branches gain the Orun-Task trailer
```

```text
$ git checkout -b orun/PAY-T14-invoice-rounding
$ git commit -m "Round invoice totals half-up"
$ git log -1 --format=%B
Round invoice totals half-up

Orun-Task: PAY-T14
```

Because the hook lives under `.git/`, it is per clone: each contributor
runs `orun githooks install` once per checkout. A repository that sets
`core.hooksPath` keeps its own hooks directory; the command writes to
whatever `git rev-parse --git-dir` reports, so check where the hook landed
in that case.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Installed. |
| `1` | Not inside a git repository, a foreign `commit-msg` hook without `--force`, or the file could not be written. |

## Related

- [`orun pr`](./orun-pr.md) — the branch grammar, the trailer, the manifest, and `orun pr check`
- [`orun task`](./orun-task.md) — where task keys come from
