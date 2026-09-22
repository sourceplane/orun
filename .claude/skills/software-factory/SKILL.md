---
name: software-factory
description: Turn a product idea into a live multi-tenant SaaS on Orunbase with the orun CLI, then build features on top of it. Given the idea and its details, author a grand epic (README, design, implementation plan, risks, as-built status), create the workspace, bootstrap a baseline (cirrus or lumen), register the epic, milestones and tasks in Orunbase, push the design documents, and land every change through the provenance pen (orun/<KEY>-<slug> branches, Orun-Task trailers, one task per PR). Use when asked to "start a product", "bootstrap from a baseline", "build X on cirrus/lumen", "create a workspace and build", or "set up the epic for" a new product.
---

# Software factory

You are running the flow described in `SOFTWAREFACTORY.md` at the root of the
`orun` repository (https://github.com/sourceplane/orun/blob/main/SOFTWAREFACTORY.md).
Read it before the first run in a session: this skill is the checklist, that
file carries the reasoning, the phase timings, and the traps.

The two products: **`orun`** is the open-source CLI you drive. **Orunbase**
is the hosted control plane it talks to (workspaces, provider connections,
secrets, the baseline registry, the task plane); `app.orunbase.com` is its
console, and a free account is enough. A **baseline** is a complete product
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
orun version                              # v2.58.15 or later
orun auth status                          # live check; `orun auth login` if it fails
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
templates beside this file (`templates/`) and follow them exactly:

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

## 5. Register the epic and its milestones in Orunbase

From inside the product repository; the build linked it, so the workspace
resolves.

```bash
cd <checkout-dir>/<reponame>
orun task epic show infra-baselining                  # the baseline's own epic; leave it alone
orun task epic create --slug <reponame>-<slug> --name "<Title> (<CODE>)" \
  --description "<the thesis in one paragraph, ending with what done looks like>" \
  --owner me --target-date <YYYY-MM-DD>
orun task milestone create --epic <reponame>-<slug> --name "<CODE>0 — the spec" \
  --exit-criteria "doc set merged and pushed"
orun task milestone create --epic <reponame>-<slug> --name "<CODE>1 — <name>" \
  --exit-criteria "<done when line>" --exit-criteria "<done when line>"
orun task epic show <reponame>-<slug>                 # capture every mls_… id in the notes
```

`epic create` is idempotent by slug; `milestone create` is not, so `show`
before you `create` when resuming.

## 6. Land the spec as the first task, and push it

Everything from here follows the pen. Once per clone:

```bash
orun githooks install
```

The spec is milestone `<CODE>0` and task `<CODE>-1`. `templates/TaskContract.yaml`
is the shape.

```bash
cp <skill>/templates/TaskContract.yaml tasks/spec.TaskContract.yaml     # fill goal/affects/doneWhen
orun task create --prefix <CODE> --title "The spec" --milestone <mls_CODE0> --assignee me \
  --brief "Author and land the epic's doc set." --contract tasks/spec.TaskContract.yaml   # → <CODE>-1
git checkout -b orun/<CODE>-1-the-spec
mkdir -p specs/epics && cp -r <scratch>/specs/epics/<reponame>-<slug> specs/epics/
git mv tasks/spec.TaskContract.yaml tasks/<CODE>-1.TaskContract.yaml && orun task attach <CODE>-1
git add specs tasks && git commit -s -m "<CODE>0: the <Product> epic doc set"   # hook stamps Orun-Task: <CODE>-1
orun pr check <CODE>-1
orun pr open --task <CODE>-1 --epic <reponame>-<slug> --title "<CODE>0: the spec" \
  --body-file <(printf 'The epic doc set: thesis, design, plan, risks, as-built status.\n')
orun pr land --number <n>
orun spec push specs/epics/<reponame>-<slug>/*.md --epic <reponame>-<slug>        # from main, as committed
orun spec list --epic <reponame>-<slug>
```

Set the README status to `In progress` and add the PR to
`IMPLEMENTATION-STATUS.md` in the next landing.

## 7. Build each milestone on top of the baseline

Repeat per milestone, one task per landing. Keep each PR inside its
contract's `affects` ceiling; `orun task check --base main` tells you before
the platform does.

```bash
orun task create --prefix <CODE> --title "<name>" --milestone <mls_CODEn> --assignee me \
  --brief "<what done looks like>" --contract tasks/<CODE>n-<slug>.TaskContract.yaml   # → <CODE>-k
git checkout -b orun/<CODE>-k-<slug>
git mv tasks/<CODE>n-<slug>.TaskContract.yaml tasks/<CODE>-k.TaskContract.yaml && orun task attach <CODE>-k
# implement inside the baseline's bounded contexts: a migration in packages/db, routes in the owning
# worker, wire types in packages/contracts, the surface in apps/web-console-next, tests beside them
pnpm -r --filter "<changed packages>" test
orun task check <CODE>-k --base main
orun plan --changed --base main --view dag
git commit -s -m "<CODE>n: <what changed, as a sentence>"
orun pr check <CODE>-k && orun pr open --task <CODE>-k --epic <reponame>-<slug> --title "<CODE>n: <name>"
orun pr land --number <n>
orun status --remote-state && orun logs --failed                                   # the merge converged
```

After each landing: mark the milestone ✅ in `implementation-plan.md`, add
the PR and any departure from `design.md` to `IMPLEMENTATION-STATUS.md`, push
the spec docs again, and note it. When every milestone is ✅, set the README
status to `✅ Shipped` and fill in **Shipped as**.

## 8. Report

End with: the workspace (slug, `ws_…`, account), the repository, the live
URLs for stage and prod, the epic (slug, `EP-n`) with its milestone and task
keys and their verdicts from `orun task list --epic …`, the spec docs pushed,
every failure and its fix from the notes file, and what is left.

## Guardrails

- Secrets travel only through standard input or a 0600 file you delete at
  the end. Never on a command line, never in the notes, never in a commit.
- Never `--run-hooks` without a values file that names `orunWorkspace`; an
  empty one writes a placeholder that fails loudly rather than silently
  cross-tenanting.
- One task per PR, branch on the grammar, trailer on every commit.
  `orun pr check` before `orun pr open`, always.
- Do not merge by hand; `orun pr land` is the landing.
- Do not delete or archive a milestone; mark it ✅.
- Stop for the user only at the milestone-table review (step 2) and when a
  fix needs a credential you do not have.
