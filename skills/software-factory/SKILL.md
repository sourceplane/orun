---
name: software-factory
title: Software factory
description: "Turn a product idea into a live multi-tenant SaaS on Orunbase, then build features on top of it. Given the idea and its details, author a grand epic (README, design, implementation plan, risks, as-built status), create the workspace, bootstrap a baseline (cirrus or lumen), mint the epic in the workspace's tracker or in Orunbase, land its doc set as the first pull request, and land every change through the provenance pen (orun/<KEY>-<slug> branches, Orun-Task trailers, one task per PR) with git and gh. Use when asked to \"start a product\", \"bootstrap from a baseline\", \"build X on cirrus/lumen\", \"create a workspace and build\", or \"set up the epic for\" a new product."
appliesTo: [bootstrapper, initiative-owner, orchestrator]
---
# Software factory

You are running the flow described in `references/software-factory.md`,
beside this file (`skill_get software-factory --file
references/software-factory.md` if you only have the registry). Read it
before the first run in a session: this skill is the checklist, that file
carries the reasoning, the phase timings, and the traps.

The two products: **Orunbase** is the hosted control plane (workspaces,
provider connections, secrets, the baseline registry, the work plane);
`app.orunbase.com` is its console, and a free account is enough. **`orun`**
is the open-source CLI: the baseline bootstrap (steps 3 and 4) runs it, so
this flow is one of the few that needs the binary signed in — `orun-cli`
says how, and is the only place that does. Everything after the bootstrap
is `git`, `gh` and the MCP tools. A **baseline** is a complete product
repository the platform rebuilds under the user's name.

The output is not a plan. It is a workspace with a live product on `stage`
and `prod`, an epic in Orunbase whose milestones and tasks describe the
product's first features, its design documents attached to that epic, and
every change landed through a task-carrying pull request. Work until that is
true, fix what breaks, and keep a notes file of every failure and fix.

## 0. Gather the inputs

Ask once, up front, for anything not already given. Never invent
credentials.

| Input | Constraint | Default if the user does not care |
|---|---|---|
| Product idea and a paragraph of detail | what it is, who it is for, the first three capabilities | ask |
| Product name | display name | derive from the idea |
| `reponame` | `^[a-z][a-z0-9-]{1,38}$`; also the URL prefix | slug of the name |
| Product domain | apex; need not exist | `<reponame>.app` |
| Account | which Orunbase account the workspace lands in; a `ws_…` id from the console's account settings | the user's default (earliest-owned) account |
| Workspace name and slug | | product name and its slug |
| Baseline | `cirrus` (Cloudflare only, free) or `lumen` (Cloudflare + Supabase, free) | `cirrus` |
| GitHub organisation | `gh` must be able to create repositories there and the Orunbase GitHub App must be installed on it | ask |
| Cloudflare token | account API token with Workers, KV, **D1 Write**; the account on the Workers Paid plan | ask; never echo it, never pass it as an argument |
| `workers.dev` subdomain | of that Cloudflare account | ask; verify it belongs to the token's account |
| Epic cluster code | two to four capitals, e.g. `AC` | first letters of the product name |

Create the notes file immediately (`<reponame>-factory-notes.md` in the
directory the product will be checked out beside) and append a timestamped
line at every decision, failure, and fix.

## 1. Preflight

```bash
orun version                              # v2.70.0 or later
orun auth status                          # not signed in → `orun-cli` §Sign in (device flow); come back here
gh auth status && gh api "orgs/<org>/memberships/$(gh api user -q .login)" -q .state
node --version; pnpm --version; python3 --version
command -v orun >/dev/null || echo "put orun on PATH: the baseline's hooks call it through bash -c"
```

Verify the Cloudflare token read-only, from a 0600 file, and confirm the
subdomain's account is the token's account:

```bash
umask 077; printf '%s' "$TOKEN" > "$NOTES_DIR/cf-token.txt"
curl -s -H "Authorization: Bearer $(cat "$NOTES_DIR/cf-token.txt")" \
  https://api.cloudflare.com/client/v4/accounts | jq '.result[] | {id, name}'
```

Stop and tell the user if any row fails. Everything downstream depends on it.

## 2. Author the grand epic, before any infrastructure

Write `specs/epics/<reponame>-<slug>/` **in a scratch directory** now; it
moves into the product repository after the bootstrap. Start from the
templates in `templates/` beside this file and follow them exactly:

- `README.md`: bold one-paragraph thesis; the **Status** table (`Draft`,
  the cluster code and milestone range, the owning components, "Builds on:
  `<baseline> baseline-vN`", Changes, Decisions locked, Gate, Shipped as
  empty); read order; milestone-at-a-glance.
- `design.md`: numbered sections. Resource model with id prefixes, API
  routes with envelopes, console surfaces, out of scope. Reuse the
  baseline's bounded contexts (identity, membership, projects, config,
  billing, webhooks, notifications, and so on) and name the Worker each
  piece lives in.
- `implementation-plan.md`: `## <CODE>0 — the spec` first, then one
  section per milestone with a **done when** list of observable facts, then
  a sequencing note. Three to six milestones for a first release.
- `risks-and-open-questions.md`: `## <CODE>-A — <title> (RISK|RESOLVED|ACCEPTED|SETTLED)`.
- `IMPLEMENTATION-STATUS.md`: "As-built ≠ intent." and an empty
  `Milestone | State | PR` table.
- `tasks/<CODE>-1.TaskContract.yaml` from `templates/TaskContract.yaml`:
  the spec's own contract (goal, `affects` covering the epic directory and
  the tasks directory, `doneWhen`, `gates: []`). The epic, its milestones
  and this task are minted in step 5; the files are their design and
  contract, never their declaration.

Show the user the milestone table and stop for a yes before spending money
on a build. This is the one blocking question in the skill.

## 3. Create the workspace and connect providers

```bash
orun workspace create "<Product>" --slug <slug>            # lands in the user's default account
# or, to name the account:
TOK=$(cd /tmp && orun auth token)
curl -s -X POST https://api.orunbase.com/v1/organizations \
  -H "Authorization: Bearer $TOK" -H "Content-Type: application/json" \
  -H "Idempotency-Key: <reponame>-ws-$(date +%s)" \
  -d '{"name":"<Product>","slug":"<slug>","accountId":"<ws_ACCOUNT>"}'

export ORUN_WORKSPACE=<ws_NEW>            # `orun workspace use` will not know it until the next login
orun integrations cloudflare connect --workspace <slug> < "$NOTES_DIR/cf-token.txt"
orun integrations list --workspace <slug>  # cloudflare active; github inherited from the account
orun baseline check <baseline> --workspace <slug>
```

Refuse to continue past a red `check`; connect what it names.

## 4. Bootstrap the baseline

If the user resolved the bootstrap in the console and handed you a `bst_…`
id, load `baselining` instead of this step: `orun baseline new --from` fills
the values file below from the record, and nothing is typed twice.

```bash
cat > "$NOTES_DIR/values.yaml" <<EOV
reponame: <reponame>
productname: <Product>
productdomain: <domain>
githuborg: <org>
orunWorkspace: <ws_NEW>
subdomain: <subdomain>
EOV
ORUN_NO_TUI=1 orun baseline new <baseline> --local --out <checkout-dir>/<reponame> \
  --run-hooks --resume --values "$NOTES_DIR/values.yaml" --workspace <slug> \
  --progress plain --keep-checkout < /dev/null 2>&1 | tee "$NOTES_DIR/build.log"
```

Run it in the background and watch the log for `✕`, `error`, `refused`,
`not linked`, and the phase lines. Expect about 70 minutes for cirrus. On any
failure: read the phase's message, fix the cause, note it, and re-run the
same command; `--resume` skips placed phases. Known fixes:

| Symptom | Fix |
|---|---|
| `not linked to workspace … re-run this phase` (phase 01, `link` hook) | `orun` is not on the hooks' PATH. `cd <checkout>/<reponame> && orun cloud link --workspace <ws_NEW> --project <reponame>`; put `orun` on PATH; resume |
| `parent_grant_insufficient` (phase 03) | the token lacks D1 Write; reconnect Cloudflare with a wider token |
| `✕ input "<key>" is required` | add it to `values.yaml` |
| a landing waits on CI for a long time | it waits for checks to conclude; do nothing unless the run itself failed on GitHub |
| `no workspace resolved` | `ORUN_WORKSPACE` is unset in this shell |
| `stored Orun login targets …` from `orun auth token` | run it from a neutral directory |

Done when the log ends with `repo gate: validate + plan --dry-run passed` and
every verify URL answers:

```bash
for env in stage prod; do curl -fsS "https://<reponame>-api-edge-$env.<subdomain>.workers.dev/health"; done
```

Delete the token file. Record the repo URL, the workspace id, the live URLs,
and the phase timings in the notes.

## 5. Mint the epic where the workspace creates work

From inside the product repository; the build linked it, so the workspace
resolves. The repository never mints work (`initiative-lifecycle`): the
epic, its milestones and its first task are created in the tracker or in
Orunbase, and the files from step 2 are their design and contract.

- **Orunbase** (a fresh workspace with no tracker connected): `epic_create`
  with the slug `<reponame>-<slug>`, the name and the summary;
  `milestone_create` for each milestone in order with its `exitCriteria`;
  `task_create` with the `<CODE>-1` contract inline and `adoptKey: <CODE>-1`
  under the first milestone. With the binary: `orun task epic create`,
  `orun task milestone create`, `orun task create --contract` (`orun-cli`).
- **The tracker** (Linear connected and chosen under Settings → Work): a
  Linear project with the same milestones and an issue for the spec; it
  arrives in Orunbase within seconds, and the issue key is the task key —
  use it in place of `<CODE>-1` below.

## 6. Land the spec as the first task

The spec is milestone `<CODE>0` and task `<CODE>-1`. The PR carries the
doc set and the contract; its review is the design's adoption, and its
merge is the task's landing.

```bash
git checkout -b orun/<CODE>-1-the-spec
mkdir -p specs/epics tasks
cp -r <scratch>/specs/epics/<reponame>-<slug> specs/epics/
cp <scratch>/tasks/<CODE>-1.TaskContract.yaml tasks/
git add specs tasks && git commit --trailer "Orun-Task: <CODE>-1" -m "<CODE>0: the <Product> epic doc set"
git push -u origin orun/<CODE>-1-the-spec
gh pr create --title "<CODE>0: the spec" --body-file body.md    # body ends with the manifest block — `pr-provenance`
gh pr checks --watch && gh pr merge --squash --delete-branch
```

After the merge, `task_get <reponame>-<slug>` shows the epic with its
milestones, and `task_get <CODE>-1` reads `done`. Set the README status to
`In progress` and add the PR to `IMPLEMENTATION-STATUS.md` in the next
landing.

## 7. Build each milestone on top of the baseline

Repeat per milestone, one task per landing (`task-branch-pr` is the full
loop). Keep each PR inside its contract's `affects` ceiling.

```bash
# create: task_create under the milestone with the contract inline and adoptKey <CODE>-k
#         (or an issue in the tracker, whose key you then use), and the contract file
cp templates/TaskContract.yaml tasks/<CODE>-k.TaskContract.yaml       # goal / affects / doneWhen / gates
git checkout -b orun/<CODE>-k-<slug>
# implement inside the baseline's bounded contexts: a migration in packages/db, routes in the owning
# worker, wire types in packages/contracts, the surface in apps/web-console-next, tests beside them
pnpm -r --filter "<changed packages>" test
git commit --trailer "Orun-Task: <CODE>-k" -m "<CODE>n: <what changed, as a sentence>"
git push -u origin orun/<CODE>-k-<slug>
gh pr create --title "<CODE>n: <name>" --body-file body.md
gh pr checks --watch && gh pr merge --squash --delete-branch
```

After each landing: `task_get <CODE>-k` reads `done`; `runs_list` for the
workspace shows the merge converged. Mark the milestone ✅ in
`implementation-plan.md`, add the PR and any departure from `design.md` to
`IMPLEMENTATION-STATUS.md` (in the PR that lands the milestone's last task;
no check reminds you), and note it. When every milestone is ✅, a last PR
sets the README status to `✅ Shipped` and fills in **Shipped as**, and
you mark the epic completed where it lives — the console or the tracker.

*With the `orun` binary* (`orun-cli`): `orun pr check`, `orun pr open` and
`orun pr land` replace the `gh` lines; `orun task check --base main`
compares the diff with the contract before CI does.

## 8. Report

End with: the workspace (slug, `ws_…`, account), the repository, the live
URLs for stage and prod, the epic (slug, `EP-n`) with its milestone and task
keys and their rungs from `task_get`, the docs the merge pushed, every
failure and its fix from the notes file, and what is left.

## Guardrails

- Secrets travel only through standard input or a 0600 file you delete at
  the end. Never on a command line, never in the notes, never in a commit.
- Never `--run-hooks` without a values file that names `orunWorkspace`; an
  empty one writes a placeholder that fails loudly rather than silently
  cross-tenanting.
- One task per PR, branch on the grammar, trailer on every commit, the
  manifest in the body. The `orun/compliance` check judges every PR the same way.
- Merge only when every check is green; never around a red one.
- Do not delete or archive a milestone; mark it ✅.
- Stop for the user only at the milestone-table review (step 2) and when a
  fix needs a credential you do not have.
