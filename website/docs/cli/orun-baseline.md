---
title: orun baseline
description: The baseline registry from the command line — list what can be built, check whether this workspace can build it, build it here or on the platform, and register and publish your own.
---

`orun baseline` is the command-line face of the **baseline registry**: the
catalogue of complete product repositories the platform can rebuild under
your name. Every subcommand runs against one workspace, resolved the same
way as every other cloud command (see [`orun workspace`](./orun-workspace.md)).

```bash
orun baseline list [--public] [--json]
orun baseline show <id[@tag]> [--json]
orun baseline check <id[@tag]>
orun baseline new <id[@tag]> --local --out <dir> [build flags]
orun baseline new <id[@tag]> --via-platform [--repo owner/name | --repo-link repl_…]
orun baseline register <id> --name … --source-repo owner/name --tag … --expected-minutes N [flags]
orun baseline publish <id> <tag>
```

Every subcommand accepts `--workspace <ws_…|slug>` and `--backend-url <url>`.

## `list`

Lists the baselines this workspace's account may build: the public catalogue
plus anything the account registered itself. `--public` reads the catalogue a
signed-out reader sees; `--json` emits the rows as JSON.

```text
$ orun baseline list
ID                 TAG           TIER  MINS  STACK                            SUMMARY
cirrus             baseline-v10  free  60    saas,cloudflare,d1,nextjs        Worker fleet behind one edge API, a Next.js console, D1 + KV data pla…
lumen              baseline-v29  free  75    saas,cloudflare,supabase,nextjs  Cloudflare + Supabase multi-tenant SaaS: worker fleet, edge API, cons…
multi-tenant-saas  baseline-v3   paid  75    saas,cloudflare,supabase,nextjs  Lumen's architecture on its own release line: the same Cloudflare + S…
```

## `show`

Shows one baseline and whether this workspace can build it: the source
repository and tag, the time budget, and one row per required provider.

```text
$ orun baseline show cirrus
cirrus —

source   sourceplane/cirrus@baseline-v10
budget   about 60 minutes

providers
  ✓ cloudflare

this workspace can build it
```

`--json` prints the registry row with its readiness block:

```json
{
  "blueprint": {
    "id": "cirrus",
    "sourceRepo": "sourceplane/cirrus",
    "tag": "baseline-v10",
    "manifestPath": "blueprint.yaml",
    "expectedMinutes": 60,
    "readiness": {
      "integrationsReady": true,
      "integrations": [{ "provider": "cloudflare", "connected": true }]
    }
  }
}
```

Addressing `id@tag` for a tag the registry has moved past resolves to the
published tag and says so on standard error.

## `check`

Readiness as an exit code, for a script or a CI gate. Exit `0` when every
provider the baseline needs is connected; non-zero otherwise, naming the
missing ones. Nothing is written and no build is started.

```bash
orun baseline check cirrus && orun baseline new cirrus --via-platform --repo acme/storefront
```

## `new`

Builds a registered baseline. Exactly one of `--local` or `--via-platform`
must be given; both or neither exits `2`. Both paths refuse to start unless
`check` would pass.

### `--local`

Fetches `sourceRepo` at the registry's tag into a temporary checkout, reads
the row's `manifestPath` to find the card, reads `spec.bootstrap.blueprint`
out of it, and places that document's phases into `--out` through the same
engine as [`orun new`](./orun-new.md). Joining those facts by hand is work
nobody should repeat.

| Flag | Meaning |
|---|---|
| `--out <dir>` | Output directory for the product. Required. |
| `--set key=value` | A blueprint input. Repeatable; overrides `--values` per key. |
| `--values <file>` | A YAML file of inputs. |
| `--run-hooks` | Run each phase's declared hooks after its files are placed. Without it the command places files and stops; with it, it bootstraps a product: repositories, installs, secrets, Terraform, pull requests, deploys. |
| `--resume` | Place every phase not already derived as done. |
| `--redo <phase>` | With `--resume`, place this phase again even though its files are in place: its hooks run again. Repeatable. For retrying a phase that landed and then failed to converge. |
| `--phase <name>` | Place only this phase. |
| `--until <name>` | Place every phase through this one. |
| `--progress auto\|plain\|verbose\|json` | Progress rendering. `json` emits one event per line. |
| `--keep-checkout` | Keep the temporary checkout and print its path, for debugging. |

```bash
orun baseline new cirrus --local --out ./acme-cloud \
  --set productname="Acme Cloud" --set productdomain=acme.dev \
  --set githuborg=acme --set subdomain=acme
```

A hook-running local build needs `git`, `gh`, Node.js 20+, `pnpm`, and
`python3`, an admin session or `ORUN_TOKEN` for the workspace, and the
providers the baseline requires connected to it.

### `--via-platform`

Asks the platform to build into a repository this workspace has **linked**,
in a sandbox it provisions, and prints a session id to watch. The repository
defaults to the current checkout's `origin`; `--repo owner/name` names
another; `--repo-link repl_…` chooses when one repository has more than one
link. An ambiguous repository is refused, not guessed.

| Flag | Meaning |
|---|---|
| `--repo owner/name` | Repository to build into. Defaults to this checkout's origin. |
| `--repo-link repl_…` | The link to build into, when the repository has several. |
| `--profile <id>` | Agent profile to run the build as. The platform prepares one otherwise. |
| `--set`, `--values` | Inputs, as for `--local`. Inputs the card derives from the repository are filled by the platform. |

```text
$ orun baseline new cirrus --via-platform --repo acme/storefront --set productname="Acme Cloud" \
    --set productdomain=acme.dev --set subdomain=acme
building cirrus@baseline-v10 into acme/storefront (repl_01J8…)
as_8f3c2d1e9b7a4c6d
```

The target is resolved and printed on standard error **before** anything
starts. The session id is the only thing on standard output. Watch the build
in the console: the **Build** pill and the build panel on the workspace's
**Overview** page show it phase by phase, and the session transcript is under
**Agents**. The repository link itself is created in the console, by the
baseline flow's Repository step or from the repository's Git tab; the CLI can
list links but not create them.

Everything about a platform build is decided on the server and reported
verbatim: the admin-role requirement, the paid-tier gate, readiness, the
repository grounding, required inputs, the admission fee, and the
one-build-per-repository lease.

`--out`, `--run-hooks`, `--resume`, `--redo`, `--phase`, `--until`, `--progress`,
and `--keep-checkout` are accepted with `--via-platform` and ignored; the platform
always runs with hooks and resume on.

## `register`

Registers a baseline this account owns. The source repository must be one the
account has connected through the GitHub integration. Registration is a
Business plan feature.

| Flag | Meaning |
|---|---|
| `--name <s>` | Display name. Required. |
| `--source-repo owner/name` | Where the baseline lives. Required. Not a hostname. |
| `--tag <tag>` | The published tag. Required. Push it first. |
| `--expected-minutes N` | How long a build takes, measured. Required. |
| `--manifest <path>` | The card's path inside the source repository, normally `blueprint.yaml`. |
| `--summary <s>` | One paragraph a reader sees. |
| `--stack a,b` | Display chips. |
| `--requires a,b` | Providers that must be connected first. |
| `--visibility private\|unlisted` | Default `private`. `public` is granted by Orunbase and cannot be set here. |
| `--brief <path>`, `--umbrella <path>` | For a baseline with a shell layer. Pass both or neither. |

```bash
orun baseline register acme-saas --name "Acme SaaS baseline" \
  --source-repo acme/acme-baseline --tag baseline-v1 --manifest blueprint.yaml \
  --expected-minutes 45 --requires cloudflare --stack saas,cloudflare --visibility unlisted
```

A duplicate id is refused with the existing row.

## `publish`

Moves one of this account's registered baselines to a tag.

```bash
git push origin baseline-v2            # in the baseline's own repository, FIRST
orun baseline publish acme-saas baseline-v2
```

The platform proves the tag before the registry moves: that it is a tag and
not a branch or a commit, that the files a build reads first are in it, and
that the card parses. A tag that fails any check leaves the registry
untouched and the reason is printed. `id@tag` as the first argument is
refused; the tag is the second argument because it is the thing that changes.

An Orunbase-maintained baseline is not published this way. Its catalogue is
`infra/baselines-registry/baselines.yaml` in the
[orun-cloud](https://github.com/sourceplane/orun-cloud) repository and moves
by pull request; the refusal says so.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success, or (`check`) ready |
| `1` | A refused build, a failed readiness check, or a platform error, printed verbatim |
| `2` | Usage: neither or both of `--local` and `--via-platform`, or `--local` without `--out` |

## Related

- [Create a workspace and build it from a baseline](../examples/bootstrap-a-product-from-a-baseline.md)
- [Baselines](../concepts/baselines.md)
- [`orun new`](./orun-new.md), [`orun workspace`](./orun-workspace.md), [`orun integrations`](./orun-integrations.md)
