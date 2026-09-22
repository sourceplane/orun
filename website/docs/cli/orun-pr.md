---
title: orun pr
---

`orun pr` is the **provenance pen**: it opens a task-carrying PR with its
lineage written in — the branch on the grammar (`orun/<task-key>-<slug>`),
the `Orun-Task: <KEY>` trailer, and a machine-readable manifest block in the
body — preflights those rules locally before the PR exists, and lands the
PR once its checks have finished.

```bash
orun pr open  --task KEY [--title …] [--base main] [--draft] \
              [--branch-slug SLUG] [--epic REF] [--session ID] \
              [--body-file FILE|-] [--json]
orun pr check [task-key] [--base main] [--json]
orun pr land  --number N [--base main] [--wait] [--check-timeout SECS] \
              [--merge-method squash|merge|rebase] [--json]
```

The `commit-msg` hook that stamps the trailer is installed separately with
[`orun githooks install`](./orun-githooks.md) (`--force` replaces a foreign
hook) — it is a top-level command, not a subcommand of `pr`.

## `open`

A PR opens **for** a task. The pen checks out `orun/<KEY>-<slug>` from the
current HEAD when the current branch is not already on the grammar for
that key, pushes it, renders the manifest into the body, and opens the PR
with the ambient GitHub credential (`GITHUB_TOKEN`, `GH_TOKEN`, or `gh
auth`). Without a credential it still prepares everything — branch pushed,
body rendered — and prints the compare URL plus the body to paste.

| Flag | Meaning |
| --- | --- |
| `--task` | The task this PR closes (one task, one PR) |
| `--title` | PR title (default: the task key) |
| `--base` | Base branch (default `main`) |
| `--draft` | Open as a draft |
| `--branch-slug` | Slug half of the branch verbatim (`orun/<task>-<slug>`) instead of slugifying the title; `[a-z0-9-]` |
| `--epic` | The epic this task belongs to, for the manifest (`epc_…` or its slug) |
| `--session` | Session id for the manifest |
| `--body-file` | File with the PR body's prose (`-` for stdin); the manifest block is appended |
| `--json` | Emit JSON |

- `--branch-slug` uses the slug half **verbatim** instead of slugifying
  the title, so a flow that names its landings (`03-infrastructure`) keeps
  the branch it documents. It must already be in the grammar's alphabet
  (`[a-z0-9-]`); anything else is refused before git is touched.
- `--epic` writes the epic (`epc_…` or its slug) into the manifest, beside
  the task, the session and the skill revisions it ran under.
- `--session` names the agent session in the manifest explicitly, for a
  caller that already knows which session did the work.
- `--body-file` supplies the prose half of the body (`-` reads stdin); the
  manifest block is appended after it.
- `--json` returns `branch`, `pushed`, `opened`, `url`, `number` (the PR
  number, for a caller that lands it next) or `compareUrl`, and `body`.

```bash
orun pr open --task BASE-3 --branch-slug 03-infrastructure --epic infra-baselining \
  --title "phase(03-infrastructure): d1, kv, db-migrate" --body-file - --json <<'EOF2'
Automated phase landing.
EOF2
```

The body's manifest then reads
`<!-- orun:manifest {"version":1,"task":"BASE-3","epic":"infra-baselining"} -->`,
and the pushes, PR and merge on `orun/BASE-3-03-infrastructure` bind to
`BASE-3` on the platform.

## `check`

The same rules, locally, before the PR exists: the branch parses to a task
key, every commit ahead of the base carries the trailer, the manifest (when
present) is well-formed and names the same task, one task per PR. These are
the rules the cloud's `orun/compliance` check verifies, pinned byte-identical
on shared fixtures. Exit 1 on errors.

| Flag | Meaning |
| --- | --- |
| `--base` | Base branch to diff against (default `main`) |
| `--json` | Emit the findings as JSON |

## `land`

`land` finishes what `open` starts: it polls the PR's checks until every one
has a conclusion, merges the PR pinned to the commit those checks ran on,
checks out the base branch, and fast-forward pulls it — so the next phase of a
flow applies onto the landing it just made, not onto the tree as it was
before. It needs a GitHub credential (`GITHUB_TOKEN`, `GH_TOKEN`, or `gh
auth`) and prints `merged #N as <sha> (<n> check(s) seen)`.

Three behaviours are load-bearing:

- **A landing waits for CI to finish, not for the checks that exist when it
  looks.** GitHub registers a PR's runs seconds after the PR opens, and a
  plan-then-matrix workflow creates its lane jobs only after the plan job
  finishes — so the pen watches the workflow *runs* as well as the check
  runs, and a run still in progress holds the merge. A check that is merely
  queued is not a passing check.
- **A repository with no CI passes rather than waits** — but only after a
  short grace period with nothing registered, since the first landing of a
  bootstrap creates the repo and its CI together.
- **A phase lands only what it placed, and can land twice.** After the merge
  the pen deletes the merged head branch (remote and local). The branch name
  is derived from the task, so a phase re-run after a fix computes the same
  name; without the delete, the second push would be refused as
  non-fast-forward after a squash merge and a task could land exactly once.

A refused merge carries GitHub's own reason through (`#N was not merged:
…`) rather than a bare `422`, so an operator can act on it.

| Flag | Meaning |
| --- | --- |
| `--number` | The pull request to land (required) |
| `--base` | Base branch, and the branch to return to (default `main`) |
| `--wait` | Wait for checks before merging (default `true`; `--wait=false` merges immediately) |
| `--check-timeout` | Seconds to wait for checks to settle (default `1800`) |
| `--merge-method` | `squash` (default), `merge`, or `rebase` |
| `--json` | Emit JSON: `Merged`, `SHA`, `ChecksSeen`, `MergeSHA`, `BranchDeleted` |

```bash
# Open, then land, from a flow that knows its PR number
n=$(orun pr open --task BASE-3 --branch-slug 03-infrastructure --json | jq .number)
orun pr land --number "$n" --check-timeout 3600

# Land without waiting — the convergence you watch afterwards is the real gate
orun pr land --number 42 --wait=false --merge-method merge
```

`orun pr land` is a thin wrapper over the same call the `orun.pr/land@v1`
workflow action makes, so the command and the action cannot describe a
landing differently.

## Related

- [`orun task`](./orun-task.md) — the task the PR carries
- [`orun githooks`](./orun-githooks.md) — install the commit-msg hook that stamps the trailer
- [Task plane](../concepts/task-plane.md) — how branches, PRs and merges derive a task's verdict
