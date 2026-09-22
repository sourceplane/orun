---
title: The task plane
description: Tasks, epics, and milestones whose identity comes from the platform, whose contract lives in the repository sealed by content hash, and whose status is derived from what was observed — never typed.
---

The **task plane** is where orun binds a unit of work to an enforceable
contract. It has one unusual property that shapes everything else on this
page: nobody, human or agent, can *write* a status. A task's rung is derived
from what the platform observed — a branch on the grammar, a pull request, a
merge, a gate result — and the tools that would let someone assert "done" do
not exist. You do the work; the evidence moves the rung.

Three parties hold the pieces:

| Piece | Who owns it | Where it lives |
|---|---|---|
| **Identity** (the key) | The cloud allocator, the single writer of keys | Orunbase |
| **Contract** (what done means) | You, in git | `tasks/<KEY>.TaskContract.yaml`, sealed by content hash wherever it travels |
| **Verdict** (how far along it is) | Derived by the platform from observations | Read back with `orun task show`, never written |

## Tasks, epics, milestones

A **task** is the unit of work. Its key comes from the allocator's ladder:
adopt a tracker's key (`--adopt ENG-42`, used only if free), derive one from a
repository issue (`--derive web#123` becomes `WEB-123`), or mint one from a
workspace sequence (`--prefix BASE` gives `BASE-1`, `BASE-2`, …; the default
prefix is `TSK`). Keys are never invented client-side. Every task also has a
`tsk_…` id; commands accept either.

An **epic** is the programme tasks club under. It has an `epc_…` id, an
`EP-n` key, and a slug you choose (`infra-baselining`); every task-plane verb
accepts any of the three. A **milestone** is one phase of an epic, in order,
with exit criteria; it has an `mls_…` id and implies its epic. Both are
orun-native containers here. An epic mirrored from a tracker belongs to the
tracker and refuses authoring.

```bash
orun task epic create --name "Infra baselining" --slug infra-baselining --owner me
orun task milestone create --epic infra-baselining --name "01 — scaffold" --first \
  --exit-criteria "repo pushed + workspace-linked"
orun task create --prefix BASE --title "phase(01-scaffold): repo and workspace" \
  --epic infra-baselining --milestone mls_01SCAF01 --assignee me \
  --contract flows/phases/01-scaffold/task-contract.yaml
```

Two behaviours make this safe to script. A slug the workspace already has is
**adopted, not suffixed**: `epic create` prints the existing epic and exits 0,
so a re-run bootstrap or a retrying agent lands on the same epic instead of
minting a second one. And where a task belongs is resolved **before** its key
is minted, so a bad `--epic` or `--milestone` is refused and never leaves a
half-made task behind.

## The TaskContract

The contract is a repo-authored document. It is read and validated before a
task is created, sealed locally (sha256 over canonical JSON), uploaded, and
recomputed by the server, which refuses a mismatch. The same bytes are what
`orun task attach` re-seals when you revise it, and what a task node in the
local object store points at (`refs/tasks/<KEY>`).

```yaml
# tasks/BASE-3.TaskContract.yaml
apiVersion: orun.io/v1
kind: TaskContract
metadata:
  name: BASE-3
spec:
  goal: D1, KV and db-migrate live on stage and prod
  affects: [infra/cloudflare-d1, infra/cloudflare-kv, infra/db-migrate]
  doneWhen: [WIRING_* secrets published for stage and prod]
  gates: []            # merge alone finishes it
```

| Field | Meaning |
|---|---|
| `goal` | What the task achieves, in one line |
| `affects` | The components the work may touch — a ceiling, checked against the diff |
| `doneWhen` | Human-checkable conditions |
| `gates` | The checks a merge must pass before the task can fold to `done` |
| `designRefs`, `deps`, `secrets`, `envs` | Design pointers, dependencies on other tasks, `secret://` references the work may resolve, environments in scope |

Unknown fields are refused: a typoed `secerts:` that silently narrowed nothing
is exactly the failure a contract exists to prevent. A `--contract FILE`
passed to `create` is a **template** with no `metadata.name`, because the key
does not exist until the allocator answers — which is how a baseline build
keeps one contract beside each of its phases.

`gates` has a third state besides "some" and "none". An explicit empty list
means *merge alone finishes the work*. Gates never declared means *gates
unknown*, and gates unknown are not gates passed: a merge on a contract-less
task parks at `in_review`.

`orun task check <key>` runs the authoring loop offline — strict validity,
completeness in the same terms the platform derives readiness from, and with
`--base` the components the diff actually touched against the `affects`
ceiling, using the same change engine as `orun plan --changed`. It is
advisory by construction; the workspace re-decides at enforcement, and
effective access is always resolved policy ∩ contract.

## The verdict ladder

```text
draft ──▶ ready ──▶ in_progress ──▶ in_review ──▶ done ──▶ released
  │         │            │              │           │
  contract  contract     a branch on    a PR is     merged, and the
  missing   complete     orun/<KEY>-…   open        contract's gates
  or                     was pushed                 passed (or gates: [])
  incomplete
```

Every rung is an observation. `orun task show <key>` prints the rung, the
evidence behind it, a pin or a dissenting assertion when one exists, and the
contract's dependencies with their states. `orun task epic show` folds the
verdicts of an epic's tasks into a rollup beside each phase's `done/total` at
read time; nothing is stored as a count.

## The provenance pen

What connects a branch to a task is the **pen**: a small set of rules that
write a PR's lineage into the places the platform can read without trusting
anyone's word.

- **Branch grammar.** `orun/<KEY>-<slug>`. The key half is the task; the slug
  half is `[a-z0-9-]`, slugified from the title or given verbatim with
  `--branch-slug` so a flow that names its landings (`03-infrastructure`)
  keeps the branch it documents.
- **Commit trailer.** Every commit ahead of the base carries
  `Orun-Task: <KEY>`. `orun githooks install` puts the `commit-msg` hook in
  place that stamps it, so a person never types it.
- **Manifest.** The PR body ends with a machine-readable block —
  `<!-- orun:manifest {"version":1,"task":"BASE-3","epic":"infra-baselining"} -->` —
  naming the task, the epic, the session and the skill revisions the work
  ran under.
- **One task, one PR.**

```bash
orun githooks install
orun pr check BASE-3 --base main          # the rules, locally, before the PR exists
orun pr open  --task BASE-3 --branch-slug 03-infrastructure --epic infra-baselining \
              --title "phase(03-infrastructure): d1, kv, db-migrate" --json
orun pr land  --number 42                 # wait for checks, merge pinned to that commit, return to a pulled base
```

`pr open` checks out the grammar branch from HEAD when you are not already on
it, pushes, renders the manifest, and opens the PR with the ambient GitHub
credential; without one it still prepares everything and prints the compare
URL plus the body to paste. `pr check` runs the same rules the platform's
`orun/compliance` check verifies on the PR itself — the two engines are pinned
byte-identical on shared fixtures — so lineage is fixed before the PR exists
rather than caught after. `pr land` polls until every check has a conclusion
(a queued check is not a passing check), merges pinned to the commit those
checks ran on, and returns the working tree to a pulled base. A repository
with no checks yet passes rather than waits, because the first landing of a
bootstrap creates the repository and its CI together.

The pen has a twin inside the [MCP](../cli/orun-mcp.md): `pr_open` is the one
tool on the pen plane, mounted whenever the server runs inside a checkout.

## Spec docs

A **spec doc** is markdown that lives in the repository and is annotated to
an epic. `orun spec push <file>… --epic <ref>` uploads the committed copy at
HEAD with its pointer — repository, path, sha — and the platform stores it
sealed beside the epic, where the console, the MCP, and the tracker sync read
it without a git credential. The slug derives from the filename and the title
from the first heading; working-tree edits are refused implicitly because the
sha must describe the bytes. Pushes are idempotent by content hash, so CI can
push on every merge. `orun spec list --epic <ref>` shows what an epic carries.

## How a baseline build authors itself

A [baseline](baselines.md) build is work, and it is tracked like work. Its
build document's hooks call typed actions — ensure an epic, ensure a
milestone, open a task, open and land a pull request — so the build writes
its own record into the task plane as it goes:

```text
epic       "Infra baselining"          slug infra-baselining (adopted on re-run)
milestone  01 — scaffold               one per phase, in order, with exit criteria
milestone  02 — foundation
milestone  03 — infrastructure
task       BASE-1  phase(01-scaffold)  one per landing, contract from flows/phases/01-…/task-contract.yaml
task       BASE-3  phase(03-infrastructure)
branch     orun/BASE-3-03-infrastructure   →  PR  →  merge  →  verdict folds to done
```

Because each contract declares `gates: []` and each landing PR is on the
grammar, the platform's observation drain folds every task to `done` from
evidence alone. Retrying a stopped build lands on the same epic and the same
milestones; only the task for the phase that did not finish is new.

## Related

- [`orun task`](../cli/orun-task.md) — create, list, show, attach, check; epics and milestones
- [`orun pr`](../cli/orun-pr.md) — the pen: open, check, land
- [`orun githooks`](../cli/orun-githooks.md) — the commit-msg hook that stamps the trailer
- [`orun spec`](../cli/orun-spec.md) — spec docs pushed to an epic
- [`orun mcp`](../cli/orun-mcp.md) — the task-plane tools an agent uses
- [The agent runtime](agent-runtime.md) — how an agent receives a contract as a frozen brief
- [Baselines](baselines.md) — the build that authors itself into this plane
