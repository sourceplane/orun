---
title: orun task
---

`orun task` is the CLI face of the task plane: identity comes from the
cloud allocator (the single writer of keys), the contract is authored in the
repository and sealed by content hash wherever it travels, and the verdict —
`draft → ready → in_progress → in_review → done → released` — is derived from
what the platform observed (branches, PRs, merges, gates), never typed.

```bash
orun task create   [--adopt KEY | --derive web#123 | --prefix TSK] [--title …] \
                   [--epic REF] [--milestone mls_…] [--brief …] [--assignee me] \
                   [--contract FILE]
orun task attach   <key>            # seal tasks/<KEY>.TaskContract.yaml and upload it
orun task list     [--epic REF] [--milestone mls_…] [--assignee me|agents|REF]
orun task show     <key|tsk_id>     # the task, where it belongs, its derived verdict
orun task check    <key> [--base REF] [--head REF]   # offline: validity, completeness, affects vs diff

orun task epic create      --name … [--slug …] [--description …] [--target-date …] [--owner me]
orun task epic show        <epc_id|EP-n|slug>
orun task epic list
orun task milestone create --epic REF --name … [--after mls_…|--first] [--exit-criteria …]…
```

## `create` — one call, born clubbed and contracted

The key comes from the allocator's ladder: adopt a tracker's key (`--adopt
ENG-42`, used only if free), derive from a repo issue (`--derive web#123` →
`WEB-123`), or mint from a workspace sequence (`--prefix BASE` → `BASE-1`,
`BASE-2`, …; default `TSK`).

Where the task belongs is set in the same create and resolved **before**
the key is minted, so a bad ref is refused and never leaves a half-made task:

- `--epic` — an `epc_…` id, an `EP-n` key, or the epic's slug.
- `--milestone` — an `mls_…` id (a milestone is a phase of its epic, and
  implies it).
- `--brief` — what done looks like, in a paragraph (orun's own, not a
  tracker's description).
- `--assignee` — `me`, or a subject ref (`usr_…` member, `sp_…` agent
  principal).

### The contract decides whether a merge can finish the work

A task's verdict folds a **merged PR to `done` only when its contract
declared its gates**. An explicit empty list (`gates: []`) means "merge
alone finishes it"; gates that were never declared park the merge at
`in_review` — gates unknown are not gates passed. So create the task
contracted:

- `--contract FILE` attaches an explicit `TaskContract` document — a
  **template**, unbound to any key (`metadata.name` optional), which is what
  a bootstrap keeps beside its flows because the key does not exist until
  the allocator answers. It is read and validated *before* the create, so a
  malformed document costs no key.
- Without `--contract`, `tasks/<KEY>.TaskContract.yaml` is attached when
  one exists for the issued key (the repo-authored convention; see
  `attach`).

```yaml
# flows/phases/03-infrastructure/task-contract.yaml — a template
apiVersion: orun.io/v1
kind: TaskContract
spec:
  goal: D1, KV and db-migrate live on stage and prod
  affects: [infra/cloudflare-d1, infra/cloudflare-kv, infra/db-migrate]
  doneWhen: [WIRING_* secrets published for stage and prod]
  gates: []          # merge alone finishes it
```

```bash
orun task create --prefix BASE \
  --title "phase(03-infrastructure): d1, kv, db-migrate" \
  --epic infra-baselining --milestone mls_EF56GH78 --assignee me \
  --contract flows/phases/03-infrastructure/task-contract.yaml --json
```

The output names the key, where the task landed, and what the contract
lets a merge do:

```
created BASE-3 (tsk_3KF9TQ2P) in milestone 03 — infrastructure
contract 4f1c9a2e attached from flows/phases/03-infrastructure/task-contract.yaml (merge alone finishes it)
recorded as refs/tasks/BASE-3 in the local object store
```

A branch named `orun/BASE-3-<slug>` then binds its pushes, PRs and checks
to the task — see [`orun pr`](./orun-pr.md) for the pen that opens it.

## `list` and `show`

`list` prints key, id, title, where the task belongs and its contract;
narrow with `--epic`, `--milestone`, or `--assignee` (`me` for yourself,
`agents` for any agent principal). `show` adds the derived verdict: the
rung, the observation behind it, a pin or a dissenting assertion when one
exists, and the contract's dependencies with their states.

## `attach` and `check`

`attach <key>` seals `tasks/<KEY>.TaskContract.yaml` locally (sha256 over
canonical JSON), uploads it, and the server refuses a mismatch. `check
<key>` runs entirely offline: strict validity, completeness in the same
terms the cloud derives readiness from, and with `--base` the components
the diff touched against the contract's `affects` ceiling (`--head` names
the head ref for that diff; default: the working tree). Advisory by
construction — the workspace decides at enforcement, and effective access
is always resolved policy ∩ contract.

## Epics and milestones — the containers

An **epic** is the programme tasks club under; a **milestone** is one of
its phases, in order, carrying exit criteria. Both are orun-native objects
here (a tracker-mirrored epic is the tracker's, and refuses authoring).

```bash
orun task epic create --name "Infra baselining" --slug infra-baselining --owner me
orun task milestone create --epic infra-baselining --name "01 — scaffold" --first \
  --exit-criteria "repo pushed + workspace-linked"
orun task milestone create --epic infra-baselining --name "03 — infrastructure" \
  --after mls_02FOUND1 --exit-criteria "WIRING_* secrets published on stage+prod"
orun task epic show infra-baselining
```

- **A taken slug is adopted, not suffixed.** `epic create` on a slug the
  workspace already has prints `epic <slug> already exists (<key>) —
  reusing it` and exits 0 (`--json`: `{"epic": …, "existed": true}`), so a
  re-run bootstrap or a retrying agent lands on the same epic instead of
  minting a second one. Any other conflict is still an error.
- **Position is spoken.** `--after mls_…` names the sibling the phase
  follows; `--first` puts it first; neither puts it last. The server
  derives the sort key between the neighbours, so nothing else renumbers.
- **The same name twice makes two phases.** `milestone create` does not
  dedupe by name; when you mean "ensure", read the epic first
  (`epic show --json` lists `milestones[]`).
- `epic show` prints the epic's own word beside the derived rollup
  (`state orun: Planning · orun: 1/3 done`) and each phase's `done/total`,
  folded from the tasks' verdicts at read.
