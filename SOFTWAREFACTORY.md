# The software factory

**From a product idea to a live, multi-tenant SaaS in an afternoon, with the
`orun` CLI, and then building your own features on top of it.**

This is a working guide, not a tour. It comes from a real run on 2026-09-22:
a new workspace, the **cirrus** baseline bootstrapped into a fresh GitHub
repository, and the product live on `stage` and `prod` about 80 minutes
later, with one failure fixed along the way. Every command below was run and
every trap listed was hit. A Claude skill that runs this whole flow lives at
[`.claude/skills/software-factory/`](.claude/skills/software-factory/SKILL.md).

- [What you are working with](#what-you-are-working-with)
- [Prerequisites](#prerequisites)
- [Station 1: the workspace](#station-1-the-workspace)
- [Station 2: bootstrap the baseline](#station-2-bootstrap-the-baseline)
- [Station 3: make the work visible](#station-3-make-the-work-visible)
- [Station 4: build your product on top](#station-4-build-your-product-on-top)
- [Station 5: keep it current](#station-5-keep-it-current)
- [Traps, in the order you will meet them](#traps-in-the-order-you-will-meet-them)
- [Command sheet](#command-sheet)

## What you are working with

Two things, and the line between them matters.

**`orun`** is this repository: an open-source intent compiler and CLI. It
compiles platform intent into a deterministic plan, converges that plan on
every commit, scaffolds repositories from blueprints, and keeps a
content-addressed record of everything it does. It is local-first and needs no
server.

**Orunbase** is the hosted control plane `orun` talks to when you want more
than one machine involved: workspaces and members, provider connections
(Cloudflare, Supabase, GitHub), brokered secrets, remote state for CI, a task
plane for epics and tasks, agent sessions, and the **baseline registry**. Its
console is [app.orunbase.com](https://app.orunbase.com), its docs are
[docs.orunbase.com](https://docs.orunbase.com), and it is itself open source
([sourceplane/orun-cloud](https://github.com/sourceplane/orun-cloud)), written
as `orun` intent and converged by `orun`. A free account is enough for
everything in this guide.

A **baseline** is a complete, production-shaped product repository the
platform can rebuild under your name: your GitHub organisation, your cloud
account, your product name and domain. It is not a template. When the build
finishes you have a repository with its own CI, its own infrastructure, its
own migrations, and a product answering on `stage` and `prod`.

| Baseline | What it builds | Needs | Time |
|---|---|---|---|
| `cirrus` | Twelve bounded-context Cloudflare Workers behind one edge API, a Next.js console, D1 + KV, migrations, CI that converges on merge. **Cloudflare only.** | a Cloudflare account | ~60 min |
| `lumen` | The same shape on Cloudflare + Supabase (Postgres). | Cloudflare and Supabase accounts | ~75 min |

The factory has four stations. Each leaves something behind that the next
one reads.

```text
  1. workspace           2. baseline build          3. visible work            4. product loop
  ──────────────         ───────────────────        ───────────────────        ──────────────────
  orun workspace         orun baseline new          orun task epic|milestone   orun task create
  orun integrations      (phases → PRs → live)      orun spec push             orun pr open|land
        │                        │                         │                         │
        ▼                        ▼                         ▼                         ▼
  ws_… + providers       repo, CI, stage+prod,      epic + milestones +        one task, one branch,
  connected              .orun/provenance.lock      design docs, in Orunbase   one PR, converged on merge
```

The vocabulary you will meet:

| Term | Meaning |
|---|---|
| **Account** | Billing and ownership parent. Workspaces belong to one. |
| **Workspace** | The tenancy boundary a product lives in: members, integrations, secrets, projects, builds. A `ws_…` id and a slug. |
| **Project** | One repository in a workspace. Project equals repo. |
| **Blueprint card / build document** | `blueprint.yaml` (inputs, required providers, secrets preview) and `repo-blueprint.yaml` (the phases `orun` places) inside the baseline's repository, read at the registry's pinned tag. |
| **Phase** | A placement barrier with hooks. Each one places files, lands them as a PR, watches the convergence on `main`, and verifies the result. |
| **Epic, milestone, task** | The task plane's containers. An epic holds milestones (phases of work); a task is one landing with a sealed **TaskContract**. A task's verdict (`draft → ready → in_progress → in_review → done → released`) is derived from what the platform observes, never typed. |
| **The pen** | `orun pr open|check|land` plus `orun githooks install`: every PR carries its task's lineage on the branch (`orun/<KEY>-<slug>`) and an `Orun-Task: <KEY>` trailer on every commit. |
| **Spec doc** | Markdown in the repository, attached to an epic with `orun spec push`, as committed at `HEAD`. |

## Prerequisites

| Need | Why | Check |
|---|---|---|
| `orun` **v2.58.15 or later** | `--phase`, `--resume`, typed hook actions; cirrus `baseline-v10` needs at least v2.56.2 | `orun version` |
| An Orunbase login | everything past Station 1 | `orun auth login`, then `orun auth status` |
| `git` and `gh`, authenticated to an account that can **create repositories in your GitHub organisation** | phase 01 creates the repo; every phase opens and lands PRs | `gh auth status` |
| The **Orunbase GitHub App** installed on that organisation | repository links, CI authentication, the platform's view of your repo | console → Integrations → GitHub |
| Node.js 20+, `pnpm`, `python3` | the baseline's own hooks (installs, rebrand, dependency cycle-break) | `node --version && pnpm --version && python3 --version` |
| A Cloudflare account on the **Workers Paid** plan | cirrus deploys a dozen Workers and D1 databases per environment | Cloudflare dashboard |
| A Cloudflare **account API token** whose permission groups include Workers, KV, and **D1 Write** | the build mints scoped tokens from it | `curl -H "Authorization: Bearer $TOKEN" https://api.cloudflare.com/client/v4/accounts` |
| That account's `workers.dev` subdomain | every product URL is `<reponame>-<component>-<env>.<subdomain>.workers.dev` | Cloudflare → Workers & Pages |

Install the CLI if you have not:

```bash
curl -fsSL https://raw.githubusercontent.com/sourceplane/orun/main/install.sh | sh
orun version
```

Sign in once. The browser flow polls for your approval and needs no callback
port; `--device` is for a terminal without a browser:

```bash
orun auth login            # or: orun auth login --device
orun auth status           # a live check: refreshes the session and exits non-zero if it is dead
```

The first login creates your account and a first workspace if you have none.

## Station 1: the workspace

### Create it

```bash
orun workspace create "Acme Cloud" --slug acme
```

```text
✓ workspace created
  id:   ws_P1DMN63P
  slug: acme
  next: connect integrations (`orun integrations <provider> connect --org ws_P1DMN63P`)
```

The workspace lands under the account you created earliest and own. If you
belong to several accounts and want a different one, call the API the CLI
wraps and name the account. Its `ws_…` id is on the console's account
settings page.

```bash
TOK=$(cd /tmp && orun auth token)
curl -s -X POST https://api.orunbase.com/v1/organizations \
  -H "Authorization: Bearer $TOK" -H "Content-Type: application/json" \
  -H "Idempotency-Key: acme-ws-$(date +%s)" \
  -d '{"name":"Acme Cloud","slug":"acme","accountId":"<ws_ACCOUNT>"}'
```

### Select it

Creating does not select. And `orun workspace use` checks the session's
cached workspace list, which only `orun auth login` refreshes, so a workspace
created seconds ago is refused with `no workspace "acme" on this session`.
Either sign in again, or export the id for the rest of the shell:

```bash
export ORUN_WORKSPACE=ws_P1DMN63P     # the build's hooks read this too
orun workspace                        # names which rung of the resolution chain won
```

The chain is `--workspace` > `ORUN_WORKSPACE` > `intent.yaml` > this
repository's link > `orun workspace use`. The environment variable is the
right rung for a build, because the product repository does not exist yet.

### Connect providers

Pipe the Cloudflare token to the CLI. It is read from standard input only,
never from an argument, so it never lands in shell history:

```bash
orun integrations cloudflare connect --workspace acme < cloudflare-token.txt
```

```text
✓ cloudflare connected (int_388667988c4945b293c090b7f8c30688, active)
  account: Acme's Account
```

Or connect with OAuth in the console under **Integrations**, which needs no
token at all. GitHub is not a connection you make here: it arrives from the
App installation and shows as `account (inherited)`:

```text
$ orun integrations list --workspace acme
PROVIDER    CONNECTION                            ACCOUNT            STATUS  SHARING              CONNECTED
cloudflare  int_388667988c4945b293c090b7f8c30688  Acme's Account     active  workspace            now
github      int_72e1a36e68a147ab907eb1e4712ae2cb  acme               active  account (inherited)  3d
```

### Confirm the baseline is buildable

```bash
orun baseline list --workspace acme
orun baseline show cirrus --workspace acme
orun baseline check cirrus --workspace acme && echo ready
```

`check` is the script-shaped form: exit `0` when every provider the card
requires is connected. Nothing starts until it passes, on either build path.

## Station 2: bootstrap the baseline

### Choose the path

| | `--via-platform` | `--local --run-hooks` |
|---|---|---|
| Runs | in a sandbox Orunbase provisions | on your machine |
| Needs | a **repository link** created in the console (the baseline flow's Repository step, or the repository's Git tab); the CLI cannot create one | `gh` with repo-creation rights, the toolchain above |
| Watch it | console: the Build pill, the Overview build panel, Agents | your terminal or the log file |
| Fix and resume | retry from the console's kept setup | edit, then re-run with `--resume` |

The platform sandbox runs the local path (`orun agent serve --driver
bootstrap` executes `orun baseline new <id>@<tag> --local --run-hooks
--resume`). When you want to see and fix every step, run it locally. This
guide does.

### Inputs

Put them in a values file. The required ones (`reponame`, `productname`,
`productdomain`, `githuborg`) must be present on every invocation, including
single-phase ones.

```yaml
# acme-values.yaml
reponame: acme               # ^[a-z][a-z0-9-]{1,38}$ ; the GitHub repo and the URL prefix
productname: Acme Cloud      # what users see in the console, emails, docs
productdomain: acme.dev      # the apex domain; it need not exist yet
githuborg: acme              # where phase 01 creates the repository
orunWorkspace: ws_P1DMN63P   # the workspace the product's remote state lives in
subdomain: acme-7be          # the Cloudflare workers.dev subdomain
# domain: true               # only if the Cloudflare zone already exists (runs phase 07)
```

### Run it

`orun` must be on `PATH`. The baseline's hooks call a bare `orun` through
`bash -c`, and a shell alias does not reach them. If you run a build from
source, put a symlink directory first on `PATH`.

```bash
export PATH="$HOME/.local/bin:$PATH" ORUN_WORKSPACE=ws_P1DMN63P ORUN_NO_TUI=1

orun baseline new cirrus --local \
  --out ~/src/acme \
  --run-hooks --resume \
  --values acme-values.yaml \
  --workspace acme \
  --progress plain \
  --keep-checkout < /dev/null 2>&1 | tee build.log
```

`--run-hooks` is the difference between placing files and bootstrapping a
product. `--resume` places every phase not already derived as done and is
safe to re-run from anywhere. `--keep-checkout` keeps the baseline clone and
prints its path, which `--status` needs later. `< /dev/null` makes a missing
input fail fast instead of prompting.

### What happens, phase by phase

Each phase places its modules, lands them as a PR, watches the convergence run
on `main`, and verifies the outcome. Times are from the real run.

| Phase | Lands | Verified by | Took |
|---|---|---|---|
| `01-scaffold` | GitHub repo created and pushed; intent, CI, tooling, identity; repo linked to the workspace | repo pushed, `orun cloud check` green | 2 min |
| `02-foundation` | 13 shared packages (PR #1) | verify lanes green | 4 min |
| `03-infrastructure` | D1 and KV per environment, the migration runner (PR #2). The first Cloudflare touch: mints `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_D1_TOKEN`, `CLOUDFLARE_ACCOUNT_ID` from the connection | wiring secrets published on stage and prod | 8 min |
| `04-workers` | the 12-worker fleet with 4 feedback bindings stripped (PR #3) | convergence green, all 12 live | 27 min |
| `04-workers-restore` | the bindings put back (PR #4) | convergence green | 5 min |
| `05-edge` | `api-edge`, the single public door (PR #5) | `/health` 200 on stage and prod | 10 min |
| `06-console` | `web-console-next` (PR #6) | console live, talking to the edge | 12 min |
| `07-domain` | custom domain and DNS, only with `domain: true` | convergence green | skipped |
| `08-docs` | `ai/context/deployment.md` and `operations.md`, written from probed reality (PR #7) | committed record matches the probes | 1 min |

The run ends with the repository gate: `orun validate` and
`orun plan --dry-run` on the product tree.

```text
✓ scaffolded 1050 file(s) into /Users/you/src/acme
✓ Intent is valid
✓ Plan revision created
  │ 42 components × 3 envs → 89 jobs
  repo gate: validate + plan --dry-run passed
```

### Verify

```bash
for env in stage prod; do
  curl -fsS "https://acme-api-edge-$env.acme-7be.workers.dev/health"
  curl -fsS -o /dev/null "https://acme-web-console-next-$env.acme-7be.workers.dev" && echo "console $env up"
done
cd ~/src/acme && orun validate && orun catalog list --kind Component | head
```

### What the build left behind

- `~/src/acme`: an ordinary `orun` repository. Every Worker, database, and
  migration is component intent; its own CI runs `orun plan` on pull requests
  and `orun run` on merge. Read `ai/context/operations.md` first: it is the
  operating contract the build wrote for the product it just made.
- `.orun/provenance.lock`: the blueprint and source digests, a secret-free
  hash of the inputs, every module's placement. This is what makes the
  product upgradable rather than a fork.
- In the workspace: the project `acme`, an epic **`infra-baselining`** with a
  milestone per phase and a task per landing, the minted secrets, and the
  repository link.

## Station 3: make the work visible

The baseline authored its own epic. Your product's work gets the same
treatment, so that anyone opening the workspace can see what is planned, what
landed, and why. The standard below is the one Orunbase itself is built with.

### The epic doc set

One directory per epic, `specs/epics/<slug>/`, with five files. Templates for
all five are bundled with the skill under
[`.claude/skills/software-factory/templates/`](.claude/skills/software-factory/templates/).

| File | Holds |
|---|---|
| `README.md` | A one-paragraph **thesis** in bold. A **Status** table: Status, Cluster, Owner(s), Builds on, Changes, Decisions locked, Gate, Shipped as. A read order. A milestone-at-a-glance table. |
| `design.md` | Numbered sections: the resource or model (tables, ids and their prefixes), the API surface (routes and envelopes), the console surfaces, what is deliberately out of scope. |
| `implementation-plan.md` | One `## <CODE><n> — <name>` section per milestone with a **done when** list of observable facts, then a sequencing note. `<CODE>0` is always "the spec". |
| `risks-and-open-questions.md` | `## <CODE>-<letter> — <title> (RISK \| RESOLVED \| ACCEPTED \| SETTLED)` entries. |
| `IMPLEMENTATION-STATUS.md` | As-built, kept distinct from intent. A `Milestone \| State \| PR` table and every place the code departed from `design.md`. Created at the first landing, updated at every one. |

Conventions: the status legend is `Draft → Ready → In progress → ✅ Shipped →
⛔ Blocked → Closed`; a completed milestone is marked ✅ and recorded, never
deleted; only a fully closed program moves to `specs/epics/_archive/`; a
tightly coupled child epic lives in `sub-epics/<child>/` with the same doc
set. The cluster code is two to four capitals (`AC`), and milestone keys are
`AC0`, `AC1`, …

### Register it in the task plane

From inside the product repository, where the workspace resolves from the
link the build made.

```bash
cd ~/src/acme

# The epic. Idempotent by slug: a second create prints "reusing it" and exits 0.
orun task epic create --slug acme-tenant-invites \
  --name "Tenant invitations (AC)" \
  --description "Members invite teammates by email: a tokenless link, an approval queue, an audit trail. Done when an invited user lands in the right workspace with the right role." \
  --owner me --target-date 2026-10-31

# One milestone per implementation-plan section, in order. Exit criteria are its "done when" lines.
orun task milestone create --epic acme-tenant-invites --name "AC0 — the spec" \
  --exit-criteria "design, plan, risks and status docs merged and pushed"
orun task milestone create --epic acme-tenant-invites --name "AC1 — the invitation resource" \
  --exit-criteria "migration applied on stage and prod" \
  --exit-criteria "POST/GET/DELETE /v1/organizations/{org}/invitations live"
orun task milestone create --epic acme-tenant-invites --name "AC2 — the console surface" \
  --exit-criteria "Members page lists and revokes invitations" \
  --exit-criteria "invite email delivered on stage"

orun task epic show acme-tenant-invites      # the epic, its milestones with mls_… ids, its tasks
```

`milestone create` is not idempotent: `show` before you `create` when
resuming.

### Push the design documents

`orun spec push` reads each file **as committed at `HEAD`**, so land the doc
set first (Station 4 shows the landing). The slug derives from the filename,
the title from the first heading, and unchanged content is a no-op on the
server, so pushing on every merge is safe.

```bash
orun spec push specs/epics/acme-tenant-invites/*.md --epic acme-tenant-invites
orun spec list --epic acme-tenant-invites
```

The documents appear on the epic in the console, and the workspace's
assistant answers from them.

## Station 4: build your product on top

Every change is one task, one branch, one pull request, converged on merge.
The platform derives the verdict from what it observes; nobody types a status.

### Once per clone

```bash
orun githooks install        # commit-msg hook: stamps `Orun-Task: <KEY>` on orun/* branches
orun cloud status            # confirms the repository is linked to acme/acme
```

### Per task

**1. Write the contract and create the task under its milestone.** The
contract is a repository document. `affects` is the ceiling on what the diff
may touch; `gates: []` declares that merge alone finishes the work.

```yaml
# tasks/AC1-invitation-resource.TaskContract.yaml
apiVersion: orun.io/v1
kind: TaskContract
spec:
  goal: The invitation resource exists — table, migration, and the three routes — on stage and prod
  affects:
    - packages/db
    - apps/membership-worker
    - packages/contracts
  doneWhen:
    - migration applied by db-migrate on merge
    - POST/GET/DELETE /v1/organizations/{org}/invitations return the documented envelopes
    - contract tests green
  gates: []
```

```bash
orun task create --prefix AC --title "Invitation resource" \
  --milestone mls_…AC1… \
  --brief "Table, migration, and the three routes, behind the existing RBAC." \
  --assignee me \
  --contract tasks/AC1-invitation-resource.TaskContract.yaml
# → AC-1 (tsk_…) under acme-tenant-invites / AC1
```

**2. Branch on the grammar and work.**

```bash
git checkout -b orun/AC-1-invitation-resource
git mv tasks/AC1-invitation-resource.TaskContract.yaml tasks/AC-1.TaskContract.yaml
orun task attach AC-1                          # seal the contract against the issued key
# … implement inside the baseline's bounded contexts …
orun task check AC-1 --base main               # offline: contract valid and complete, diff within `affects`
orun plan --changed --base main --view dag     # what this commit will converge
git commit -s -m "Invitation resource: table, migration, routes"   # the hook appends Orun-Task: AC-1
```

**3. Open, preflight, land.**

```bash
orun pr check AC-1                             # branch grammar, trailer on every commit, one task per PR
orun pr open --task AC-1 --epic acme-tenant-invites --title "AC1: invitation resource" --body-file pr.md
orun pr land --number 12                       # waits for every check to conclude, squash-merges, returns to main
```

`land` waits for checks that have a conclusion, not for whatever exists when
it looks; a repository with no checks passes rather than waits. On merge the
product's own CI runs `orun run --changed`, `db-migrate` applies the
migration, the task folds to `done` when its gates are met, and to
`released` when the deployment is observed.

**4. Record it.** Mark the milestone ✅ in `implementation-plan.md`, add the
PR to `IMPLEMENTATION-STATUS.md`, and push the spec docs again from `main`.

### Watch the product

```bash
orun status --remote-state             # the convergence that merge triggered
orun logs --failed
orun catalog affected --base main~1    # what the merge touched
orun task list --epic acme-tenant-invites
orun tui                               # or open the workspace at app.orunbase.com
```

## Station 5: keep it current

**Upgrade to a newer baseline release.** When the registry moves cirrus to a
newer tag, re-render against the provenance lock. Files you edited surface as
conflicts and are never overwritten.

```bash
cd ~/src/acme
orun new upgrade --out .                       # report only
orun new upgrade --out . --apply               # apply non-conflicting updates, on a task branch
```

**Re-run one phase.** Phase state is derived from the tree, so any phase can
be run again months later from the kept checkout:

```bash
orun new --blueprint <checkout>/repo-blueprint.yaml --out ~/src/acme --status --values acme-values.yaml
orun new --blueprint <checkout>/repo-blueprint.yaml --out ~/src/acme --run-hooks --phase 08-docs --values acme-values.yaml
```

**Make your product a baseline.** Once it carries a `blueprint.yaml` card and
a `repo-blueprint.yaml`, tag it and register it. Push the tag first; the
registry proves it before it moves.

```bash
git tag baseline-v1 && git push origin baseline-v1
orun baseline register acme-saas --name "Acme SaaS baseline" --source-repo acme/acme \
  --tag baseline-v1 --manifest blueprint.yaml --expected-minutes 60 \
  --requires cloudflare --visibility unlisted
```

## Traps, in the order you will meet them

1. **`orun auth token` refuses inside a repository whose `intent.yaml` names
   a different backend.** Run it from a neutral directory (`cd /tmp`).
2. **A reused `Idempotency-Key` replays a failure**, including a `401` from
   an expired token, with the same request id. Mint a fresh key per attempt.
3. **`orun workspace use` does not know a workspace you just created.** Only
   `orun auth login` refreshes the list. Use `ORUN_WORKSPACE` or
   `--workspace` until then.
4. **The hooks cannot find `orun`.** They run `bash -c 'orun …'`; put the
   binary on `PATH`, not in an alias. The first symptom is phase 01's `link`
   hook: *"acme/acme is not linked to workspace … re-run this phase"*. Fix:
   `cd ~/src/acme && orun cloud link --workspace <ws> --project acme`, put
   `orun` on `PATH`, resume.
5. **The Cloudflare token lacks D1 Write.** Phase 03's `d1-edit` mint is
   refused with `parent_grant_insufficient`. Reconnect with a wider token.
6. **`✕ input "<key>" is required`.** Add it to the values file; the required
   inputs are validated before any phase runs, single-phase runs included.
7. **`orun spec push` sees the committed bytes, not your working tree.**
   Commit first.
8. **`orun pr open` needs the contract sealed to its key.** Rename the
   template to `tasks/<KEY>.TaskContract.yaml` and run `orun task attach
   <KEY>`.
9. **`--via-platform` needs a repository link the CLI cannot create.** Make
   it in the console, or build locally.
10. **With `--via-platform`, the flags `--out`, `--run-hooks`, `--resume`,
    `--phase`, `--until`, `--progress`, and `--keep-checkout` are ignored.**
    The platform always runs with hooks and resume on.

## Command sheet

```bash
# session and tenancy
orun auth login [--device]            orun auth status [--offline]         orun auth token
orun workspace [list|use <ws>|create <name> --slug <s>]     export ORUN_WORKSPACE=ws_…
orun integrations list                orun integrations <provider> connect < token.txt

# baselines
orun baseline list|show <id>|check <id>
orun baseline new <id> --local --out <dir> --run-hooks --resume --values <f> [--phase|--until <name>] [--keep-checkout]
orun baseline new <id> --via-platform --repo owner/name [--repo-link repl_…] --set k=v
orun baseline register <id> …         orun baseline publish <id> <tag>
orun new --blueprint <f> --out <dir> --status      orun new upgrade --out <dir> [--apply]

# the task plane
orun task epic create --slug <s> --name <n> [--description …] [--owner me] [--target-date …]
orun task milestone create --epic <s> --name <n> [--exit-criteria …]… [--after mls_…|--first]
orun task create --prefix <P> --title <t> --milestone mls_… --brief <b> --assignee me --contract <f>
orun task attach <KEY>                orun task check <KEY> --base main    orun task show|list [--epic <s>]
orun spec push <files>… --epic <s>    orun spec list --epic <s>

# the pen
orun githooks install                 git checkout -b orun/<KEY>-<slug>
orun pr check <KEY>                   orun pr open --task <KEY> --epic <s> --title <t> [--body-file <f>]
orun pr land --number <n>

# the product
orun validate                          orun plan --changed --base main --view dag
orun status [--remote-state]           orun logs --failed          orun catalog affected --base main~1
```

Further reading: the [baseline guide](https://orun-docs.pages.dev/examples/bootstrap-a-product-from-a-baseline)
and the [`orun baseline`](https://orun-docs.pages.dev/cli/orun-baseline),
[`orun task`](https://orun-docs.pages.dev/cli/orun-task), and
[`orun pr`](https://orun-docs.pages.dev/cli/orun-pr) references on the docs
site; the Orunbase side at [docs.orunbase.com](https://docs.orunbase.com).
