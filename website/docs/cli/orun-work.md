---
title: orun work
description: The declared work tree — epic.yaml per epic, task contracts beside it — validated before a pull request merges, so the work-manifest rule refuses a malformed declaration in CI rather than on main.
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
sentence naming the file — the same sentences the `work-manifest` rule
reports under `orun pr check --standards`. Exit 1 on any problem.

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
file — the `epic-status` rule's facts.

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

## In `orun pr check`

When the repository declares a `work:` section, `orun pr check --standards`
reads the tree too: every problem becomes a `work-manifest` finding (an
error under `enforce`, a warning under `warn`), and the branch's task gets
the `epic-status` fact. No new CI step: the `standards` job that already
runs `pr check` covers it.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | The tree is clean, or the repository declares no work section. |
| `1` | At least one problem, or the tree could not be read. |
