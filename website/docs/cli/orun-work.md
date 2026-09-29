---
title: orun work
description: The declared work tree — epic.yaml per epic, task contracts beside it — validated before a pull request merges, so a malformed declaration is refused in CI rather than on main.
---

`orun work` is the CLI face of **work as code** (orun-cloud epic
`saas-work-gitops`): a repository declares in `intent.yaml` where its
epics and task contracts live, each epic carries an `epic.yaml`, and the
tools read the tree instead of asking anyone to mint anything by hand.

```bash
orun work check [--base <ref>] [--json]
```

## The declaration

```yaml
# intent.yaml
work:
  epics: work/epics     # one directory per epic, named by its slug
  tasks: work/tasks     # <KEY>.TaskContract.yaml
  sync: on-merge        # off | on-merge
```

```yaml
# work/epics/saas-work-gitops/epic.yaml
apiVersion: orun.io/v1
kind: Epic
metadata:
  name: saas-work-gitops          # the slug — must equal the directory name
  key: WG                         # the task-key prefix this epic reserves
spec:
  title: "Work as code"
  summary: "One paragraph; becomes the epic's description."
  state: started                  # backlog | started | paused | completed | canceled
  owner: me                       # optional
  targetDate: 2026-10-15          # optional
  docs: ["*.md"]                  # optional; pushed as epic docs
  status: IMPLEMENTATION-STATUS.md # optional; the file a milestone-closing PR must touch
  milestones:
    - name: "WG0 — the spec"
      exitCriteria: ["the doc set is on main"]
      tasks: [WG-1]
    - name: "WG1 — the tree"
      tasks: [WG-2, WG-3]
```

A task contract may carry `metadata.title`, the display title a sync gives
the task it creates.

## `check`

Reads every `epic.yaml` and every contract and prints each problem as one
sentence naming the file, prefixed `error work-manifest`. Exit 1 on any
problem.

| Rule | Problem it names |
|---|---|
| declaration | unknown fields, a wrong kind, a slug that is not the directory name, a bad key prefix, an unknown state, a missing title, a milestone listed twice |
| membership | a listed key outside the epic's prefix; a key listed under two milestones anywhere in the tree; a listed key with no `<tasks>/<KEY>.TaskContract.yaml` |
| reservation | two epics reserving one prefix; a contract in a reserved prefix that no milestone lists (it would never reach a milestone) |

A contract with an unreserved prefix (`TSK-`, `WRK-`) is a stray and is
left alone: those tasks are minted by the platform, not declared here.

With `--base <ref>` the check also resolves, for the current branch's task
(the key in `orun/<KEY>-<slug>`): the milestone that lists it, whether
this branch closes that milestone (its other tasks read `done` on the
platform, or it has none), and whether the diff touches the epic's status
file.

```text
$ orun work check --base main
1 epic(s), 11 contract(s) under work/epics and work/tasks; reserved prefixes: WG
this branch's task sits in saas-work-gitops / WG2 — epic.yaml and the pre-merge check
closes the milestone: false; touches work/epics/saas-work-gitops/IMPLEMENTATION-STATUS.md: false
clean
```

`--json` returns `{"declared", "layout", "epics", "contracts", "prefixes", "problems", "placedIn", "epic"}`.

A repository with no `work:` section prints that and exits 0; nothing here
applies to it.

To refuse a malformed tree before it merges, run `orun work check` as a
step of the pull-request CI job.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | The tree is clean, or the repository declares no work section. |
| `1` | At least one problem, the tree could not be read, `sync` ran with `work.sync: off` and no `--force`, or a write failed (the message says how many were applied). |

## `sync`

```bash
orun work sync [--dry-run] [--force] [--repo <owner/name>] [--json]
```

Reconciles the tree into the platform — the CI job on the default branch,
under CI's own identity. **Git declares, the platform runs:** the sync
creates and updates what `epic.yaml` names and never deletes anything.

| Declared | Reconciled as |
|---|---|
| `metadata.name` | the epic, by slug — created when missing |
| `spec.title`, `summary`, `targetDate`, `owner` | updated when they differ (`owner` only when the platform has none) |
| `spec.state` | applied **forward only**: never backwards, never off a terminal state (`completed`, `canceled`) — reopening is a platform action |
| milestones, in order | created, given their exit criteria, re-positioned; one the tree no longer names is reported and left |
| task keys under a milestone | created with the declared key (`adoptKey`), the contract attached, the title from `metadata.title` or the goal's first sentence; a contract whose hash changed is re-attached; a task sitting in another milestone is reported (moving is a platform action today) |
| `spec.docs` | every matching file's committed copy, pushed as an epic doc — the server is idempotent by content hash |

Every write carries `Idempotency-Key: work:<repo>:<path>:<sha>[:<what>]`,
so re-running the same commit is a no-op at the edge, and the header
`X-Orun-Work-Sync: <repo>@<sha>`, which the platform stamps as the epic's
`managedBy` pointer and honours as the one writer a managed field accepts.
The pointer also reserves the epic's `metadata.key`: the sync sends it on
every epic write, so a task key in that prefix is minted only by the sync.
Every run moves that pointer: a commit that changes nothing the sync
writes still stamps the epic once (`stamp  epic <slug> — managedBy`, an
empty update under the header), and the same commit run twice stamps
nothing.
A run stops at the first failed write and says how many it applied; the
next push resumes from the tree.

`sync` runs only when `intent.yaml` says `sync: on-merge` (or with
`--force`). `--dry-run` prints the plan and writes nothing, whatever intent
says, and refuses a tree with problems the same way `check` does.

```text
$ orun work sync --dry-run
would create   milestone "WG3 — orun work sync and managed epics" — after mls_4NW6PPEM
would create   task WG-6 — "orun work sync reconciles the tree" in "WG3 — orun work sync and managed epics"
would push     doc design — work/epics/saas-work-gitops/design.md
skip           epic saas-work-gitops state — git says started but the platform is already at started
dry run: 3 write(s) would be made, 0 warning(s)
```

```yaml
# .github/workflows/ci.yml — the job
work-sync:
  if: github.event_name == 'push' && github.ref == 'refs/heads/main'
  steps:
    - uses: actions/checkout@v6
    - uses: sourceplane/orun-action@v1.2.0
      with: { version: v2.66.0 }
    - run: orun work sync
```
