---
title: Create a workspace and build it from a baseline
description: From a fresh install to a live product — sign in, create an Orunbase workspace, connect Cloudflare, verify the cirrus baseline is buildable, and build it on the platform or on your own machine.
---

This guide walks through the complete path from an empty machine to a running
multi-tenant SaaS product, using the **cirrus** baseline as the worked example.
Every command and every line of output below was run against the current
release; where the platform and the CLI disagree, the guide says so.

By the end you will have:

- an Orunbase **workspace** selected as your working workspace;
- **Cloudflare** connected to it, which is the only provider cirrus needs;
- a product repository built from `sourceplane/cirrus` at the tag the registry
  pins, either by the platform in its own sandbox or locally in a directory
  you choose;
- a way to verify the result and to rebuild from it.

:::note Two products, one flow
`orun` is the open-source engine. **Orunbase** is the hosted control plane it
talks to (`app.orunbase.com`, `api.orunbase.com`). The baseline registry, the
workspace, and the build sandbox live on Orunbase; the build itself is `orun`
running the baseline's blueprint. The Orunbase side of the platform is
documented at [docs.orunbase.com](https://docs.orunbase.com).
:::

## What a baseline is

A **baseline** is a complete, production-shaped product repository that the
platform can rebuild for you under your own name: your GitHub organisation,
your Cloudflare account, your product name and domain. It is registered in the
**baseline registry**, which records where the baseline lives (`sourceRepo`),
which release is published (`tag`), how long a build takes, and which providers
must be connected first (`requires`).

The registry knows *what* can be built. The baseline's own `blueprint.yaml`
knows *how*: the inputs it asks for, the secrets it mints, the phases it
places. The two never restate each other.

| Baseline | What it builds | Requires | Tier |
|---|---|---|---|
| `cirrus` | Worker fleet behind one edge API, a Next.js console, D1 + KV data plane, migrations, CI that converges on merge. Cloudflare only. | `cloudflare` | free |
| `lumen` | The same shape on Cloudflare + Supabase. | `cloudflare`, `supabase` | free |
| `multi-tenant-saas` | Lumen's architecture on its own release line, under the Sourceplane brand defaults. | `cloudflare`, `supabase` | paid |

Run `orun baseline list` for the live catalogue; the table above is what it
printed on 2026-09-22. See [Baselines](../concepts/baselines.md) for the model.

## 1. Install and sign in

Install the CLI (see [Installation](../start/installation.md) for other methods):

```bash
curl -fsSL https://raw.githubusercontent.com/sourceplane/orun/main/install.sh | sh
orun version
```

Sign in. The default flow opens your browser to approve the CLI and polls until
you do; there is no local listener, so it works from behind a firewall. On a
terminal with no browser, add `--device`.

```bash
orun auth login            # browser approval
orun auth login --device   # RFC 8628 device code, for headless terminals
```

`orun auth login` also links the repository you run it in, if you are inside
one. That is harmless here and unnecessary; pass `--no-link` to skip it.

Confirm the session. `orun auth status` checks that the login still works,
not just that a token is stored:

```text
$ orun auth status
User: pullely
Email: pullely@sourceplane.ai
Backend URL: https://api-edge-prod.oruncloud.workers.dev
Orgs:
  - cirrus-test — owner
  - test work (test-work) — owner
```

The session is kept in the operating system's credential store, with a
fallback to `~/.orun/credentials.json`. Non-secret settings, including the
workspace you select below, live in `~/.orun/config.yaml`.

:::tip Headless machines and CI
Set `ORUN_TOKEN` to a workspace API key instead of logging in. If a runner
hands you a file that it keeps refreshed, set `ORUN_TOKEN_FILE` to its path;
the file wins over the environment variable, because an exported copy of a
rotating token stops working at the first rotation.
:::

## 2. Create a workspace

A workspace is the tenancy boundary: members, integrations, secrets, projects,
and builds all belong to one. Create one and select it:

```bash
orun workspace create "Acme Cloud" --slug acme
```

```text
✓ workspace created
  id:   ws_XWDRVB8C
  slug: acme
  next: connect integrations (`orun integrations <provider> connect --org ws_XWDRVB8C`)
```

The `ws_…` id is permanent; the slug is what you will type. The slug is
derived from the name if you omit `--slug`. `orun cloud workspace create` is
the same command under its older spelling.

Creating a workspace does **not** select it. Select it once, and every cloud
command runs against it until you change it:

```bash
orun workspace use acme
orun workspace
```

```text
workspace: acme (ws_XWDRVB8C)
  from: `orun workspace use`
  change: `orun workspace use <ws-id|slug>`, or --workspace <ws> for one command
```

The selection is the last rung of the resolution chain on purpose. A flag, an
environment variable, an `intent.yaml`, or a repository link always wins over
it, so choosing a working workspace can never retarget a repository that
declares its own. See [`orun workspace`](../cli/orun-workspace.md) for the
full chain.

## 3. Connect Cloudflare

Cirrus needs one provider connection: Cloudflare. GitHub is not a connection
row; it arrives as the GitHub App installation and the repository link in
step 5.

There are two ways to connect.

**In the console (recommended).** Open the workspace at
[app.orunbase.com](https://app.orunbase.com), go to **Integrations**, and
connect Cloudflare with OAuth. The platform provisions its own scoped service
token; you never paste a key.

**From the CLI, by token paste.** Create an account API token in the
Cloudflare dashboard and pipe it to the CLI. The token is read from standard
input only, so it never lands in your shell history:

```bash
orun integrations cloudflare connect --workspace acme < cloudflare-token.txt
```

The token's permission groups must include **D1 Write** in addition to the
Workers permissions. Cirrus mints a separate D1 token from this connection
during the build, and the mint is refused if the parent token cannot grant it.
The full permission list is in the
[Orunbase integration catalog](https://docs.orunbase.com/platform/integrations/catalog#cloudflare).

Confirm the connection:

```text
$ orun integrations list
PROVIDER    CONNECTION                            ACCOUNT                 STATUS  SHARING    CONNECTED
cloudflare  int_bf097f5a95064bf8bdfbc399ff2a8944  Acme Cloudflare         active  workspace  5d
github      int_72e1a36e68a147ab907eb1e4712ae2cb  acme                    active  account    3d
```

Your Cloudflare account must be on the **Workers Paid** plan. Cirrus deploys
D1 databases and a dozen Workers per environment, and the free plan refuses
part of that.

## 4. Verify the baseline is buildable

Ask the registry whether this workspace can build cirrus:

```text
$ orun baseline show cirrus
cirrus —

source   sourceplane/cirrus@baseline-v10
budget   about 60 minutes

providers
  ✓ cloudflare

this workspace can build it
```

`orun baseline check` gives the same answer as an exit code, for a script or a
CI gate. Nothing is written and no build starts:

```bash
orun baseline check cirrus && echo ready
```

```text
cirrus is ready to build
ready
```

If a provider is missing, both commands name it, and `orun baseline new`
refuses to start until it is connected. The readiness check is a hard gate
even for a local build.

:::note The tag belongs to the registry
`cirrus@baseline-v10` names the release the registry publishes today. If you
address an older tag after the registry has moved on, the CLI resolves to the
current one and says so on standard error. There is no way to build a
withdrawn release through the registry.
:::

## 5. Build on the platform

The platform build runs `orun` in a sandbox it provisions, with a time-boxed
admin grant on your workspace, and writes the product into a repository you
have **linked**. Over about an hour it creates branches, opens and merges pull
requests, provisions D1 and KV with Terraform, deploys every Worker to `stage`
and `prod`, and writes the product's own CI. It is the same command the
platform's console runs when you press **Build**.

### Prerequisites

- **Your role in the workspace is `admin` or `owner`.** Starting a build lends
  admin to the build session, so the platform requires it of you.
- **The target repository is linked.** A repository link (`repl_…`) is created
  in the console once the GitHub App is installed on the organisation: either
  by the baseline flow's **Repository** step (open the workspace switcher and
  choose **Start with a baseline**), or from the repository's **Git** tab.
  This is the one step the CLI cannot do for you: `orun cloud link` creates a
  different record, the allow-list link used for remote state, and it is not
  enough here.
- **Credits.** A platform build debits a flat admission fee from the
  workspace's credits. Cirrus is a free-tier baseline, so no paid plan is
  needed; a paid baseline such as `multi-tenant-saas` requires one.

### Start the build

Name the baseline and the repository. Inputs the card declares are passed with
`--set`; anything the console would have derived from the repository, such as
`reponame` and `githuborg`, is filled by the platform.

```bash
orun baseline new cirrus --via-platform \
  --repo acme/storefront \
  --set productname="Acme Cloud" \
  --set productdomain=acme.dev \
  --set subdomain=acme
```

The CLI resolves and prints the target **before** anything starts, so a wrong
repository costs nothing. If one repository has more than one link, it refuses
to guess; pass `--repo-link repl_…` to choose.

```text
building cirrus@baseline-v10 into acme/storefront (repl_01J8…)
as_8f3c2d1e9b7a4c6d
```

The last line is the **session id** on standard output. Everything else is on
standard error, so `orun baseline new … --via-platform | pbcopy` gives you the
id alone.

:::warning Watch the build in the console
The CLI prints the session id and returns. It does not print a link, and
`orun agent attach` needs the session's own credentials, so the console is
where you watch: open the workspace at app.orunbase.com. The **Build** pill in
the masthead and the build panel on the **Overview** page show the running
build phase by phase, and the session transcript is under **Agents**. A
stopped build can be retried there from the setup it kept.
:::

The inputs cirrus asks for are the ones in its card. Missing required inputs
are refused up front with the list of keys, never discovered an hour in.

| Input | Meaning | Example |
|---|---|---|
| `productname` | The display name users see in the console, emails, and docs | `Acme Cloud` |
| `productdomain` | The apex domain the product answers on; it need not exist yet | `acme.dev` |
| `subdomain` | The `workers.dev` subdomain of the Cloudflare account the build deploys to | `acme` |

`reponame`, `githuborg`, and `apibaseurl` are derived from the repository you
chose and from `productdomain`.

### What the platform enforces

The server, not the CLI, decides all of the following, and each is reported
verbatim:

| Check | On failure |
|---|---|
| You hold the admin role | `403` |
| The baseline's tier is within your plan | `412`, naming the free baselines you can build instead |
| The manifest at `sourceRepo@tag` parses | `412` with the line and message, or `503` if GitHub did not answer |
| Every required provider is connected | `412 Connect <providers> before starting this blueprint` |
| Every required input has a value | `422`, naming the keys |
| No other build is running against the same repository | `409`, naming the running build and who started it |
| The admission fee can be debited | `412 credits_exhausted` |

One build per repository is a lease: a second build against the same
repository is refused until the first reaches a terminal state, and the lease
is released on every failure path.

### Flags that do nothing on the platform

`--out`, `--run-hooks`, `--resume`, `--redo`, `--phase`, `--until`,
`--progress`, and `--keep-checkout` describe a local build. They are accepted with
`--via-platform` and silently ignored. The platform always runs with hooks and
resume enabled.

## 6. Build locally instead

`--local` does the same build on your machine. It fetches `sourceplane/cirrus`
at the registry's tag into a temporary checkout, reads the registry's
`manifestPath` to find the build document, and places its phases into `--out`
through the same engine as [`orun new`](../cli/orun-new.md).

There are two very different things a local build can mean.

**Place the files only.** Without `--run-hooks`, `orun` renders and copies the
baseline's modules into the directory and stops. Nothing is installed, nothing
is deployed, and no provider is touched. This is the right way to read a
baseline or to start a fork by hand:

```bash
orun baseline new cirrus --local --out ./acme-cloud \
  --set productname="Acme Cloud" \
  --set productdomain=acme.dev \
  --set githuborg=acme \
  --set subdomain=acme
```

**Bootstrap the product.** With `--run-hooks`, each phase's declared hooks
run after its files are placed: creating the GitHub repository, installing
dependencies, minting Cloudflare secrets from the workspace's connection,
running Terraform, opening and landing pull requests, and deploying. This is
what the platform sandbox runs, and it needs the same environment:

- `git`, `gh` (authenticated to an organisation you can create repositories
  in), Node.js 20 or later, `pnpm`, and `python3` on your `PATH`;
- a resolved workspace with Cloudflare connected, and an `admin` API key in
  `ORUN_TOKEN` or a logged-in admin session;
- a Cloudflare account on the Workers Paid plan.

```bash
orun baseline new cirrus --local --out ./acme-cloud --run-hooks --resume \
  --values ./acme.yaml
```

`--values` takes a YAML file of inputs; `--set key=value` overrides it per
key. `--resume` places every phase not already derived as done, which is how
you continue after a stop. `--redo <name>` (repeatable, with `--resume`)
places the named phase again even though its files are in place — what you
want after a phase's pull request merged and its deploy failed, since the
tree cannot tell that from done. `--phase <name>` places one phase, and
`--until <name>` places every phase through that one:

```bash
orun baseline new cirrus --local --out ./acme-cloud --run-hooks --until 03-infrastructure
```

Cirrus declares these phases, in order:

| Phase | What it does | Touches Cloudflare |
|---|---|---|
| `01-scaffold` | Root manifests, tooling, CI, rebrand values | no |
| `02-foundation` | The shared packages: contracts, db, sdk, policy engine, cli | no |
| `03-infrastructure` | D1 and KV per environment, the migration runner | **yes** |
| `04-workers` | The twelve bounded-context Workers | yes |
| `04-workers-restore` | Restores the four service bindings the first pass deferred | yes |
| `05-edge` | `api-edge`, the single public front door | yes |
| `06-console` | `web-console-next` | yes |
| `07-domain` | Custom domain and DNS, only with `--set domain=true` | yes |
| `08-docs` | Probes the live URLs and writes the deployment record | no |

`--keep-checkout` keeps the temporary checkout for debugging and prints its
path. `--progress json` emits one event per line for another program to read.

## 7. Verify the product

Cirrus declares what a finished build must answer. For each of `stage` and
`prod`:

```bash
curl -fsS https://storefront-api-edge-stage.acme.workers.dev/health
curl -fsS https://storefront-web-console-next-stage.acme.workers.dev >/dev/null && echo console up
```

The pattern is `https://{reponame}-api-edge-{env}.{subdomain}.workers.dev/health`
and `https://{reponame}-web-console-next-{env}.{subdomain}.workers.dev`.

Inside the product repository, the build wrote `.orun/provenance.lock`: the
blueprint and source digests, a secret-free hash of the inputs, and the
per-module placement record. The product is an ordinary `orun` repository from
its first commit, so:

```bash
cd storefront
orun validate
orun plan --view dag
orun catalog list --kind Component
```

Every Worker, database, and migration in it is component intent that its own
CI compiles and converges on merge, exactly as the baseline's does.

## 8. Rebuild, upgrade, and register your own

**Rebuild from a newer baseline release.** When the registry moves cirrus to a
new tag, `orun new upgrade` re-renders the newer blueprint against the
provenance lock and three-way merges it into your tree. Files you edited
surface as conflicts and are never overwritten:

```bash
orun new upgrade --out ./storefront            # dry run: what would change
orun new upgrade --out ./storefront --apply    # apply the non-conflicting updates
```

**Register your own baseline.** Any repository your account has connected can
be registered as a baseline and built the same way. Registration is a Business
plan feature. Push the git tag in the baseline's repository **first**; the
registry refuses a tag it cannot prove.

```bash
git -C ../acme-baseline tag baseline-v1 && git -C ../acme-baseline push origin baseline-v1

orun baseline register acme-saas \
  --name "Acme SaaS baseline" \
  --source-repo acme/acme-baseline \
  --tag baseline-v1 \
  --manifest blueprint.yaml \
  --expected-minutes 45 \
  --requires cloudflare \
  --stack saas,cloudflare \
  --visibility unlisted
```

Later releases move the pin with `orun baseline publish acme-saas baseline-v2`,
again after the tag exists. The platform checks that the ref is a tag and not
a branch, that the files a build reads first are present in it, and that the
manifest parses, and leaves the registry untouched if any check fails.

The Orunbase-maintained baselines (`cirrus`, `lumen`, `multi-tenant-saas`)
are not published this way. Their catalogue is a file in the open-source
[orun-cloud](https://github.com/sourceplane/orun-cloud) repository,
`infra/baselines-registry/baselines.yaml`, and it moves by pull request.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `no workspace resolved; pass --workspace or link the repo` | Nothing in the resolution chain named a workspace. Run `orun workspace use <slug>` or add `--workspace <slug>`. |
| `orun auth status` reports the session no longer works | The refresh token was revoked or expired. Run `orun auth login` again. |
| `cirrus needs cloudflare connected` (exit non-zero from `check`) | Connect the provider in the console or with `orun integrations cloudflare connect`, then re-run. |
| `412 … requires a paid plan` | The baseline is paid tier. Upgrade the workspace's plan or build `cirrus` or `lumen`. |
| `409 A build of cirrus is already running against acme/storefront` | One build per repository. Wait for it to finish or stop it in the console. |
| `repository … has more than one link` | Pass `--repo-link repl_…`. |
| `parent_grant_insufficient` during phase `03-infrastructure` | The Cloudflare token behind the connection lacks **D1 Write**. Reconnect with a token that has it. |
| A local `--run-hooks` build stops part way | Fix the cause it printed and re-run the same command with `--resume`. Phases already placed are skipped. |
| A phase's pull request merged and its convergence failed | Its files are all in place, so `--resume` alone skips it. Add `--redo <phase>` to place it again; the failed event's `meta.failedLanes` names the jobs that failed. |

## Related

- [Baselines](../concepts/baselines.md) — the model: registry, card, build document, phases
- [`orun baseline`](../cli/orun-baseline.md) — the full command reference
- [`orun workspace`](../cli/orun-workspace.md) and [`orun auth`](../cli/orun-auth.md)
- [`orun new`](../cli/orun-new.md) — the scaffold engine every build runs through
- [Orunbase: integrations catalog](https://docs.orunbase.com/platform/integrations/catalog) — provider connection details
