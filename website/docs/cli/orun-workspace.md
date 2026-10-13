---
title: orun workspace
description: Choose the workspace every cloud command runs against — once, with orun workspace use — and understand the resolution chain that decides which workspace wins.
---

`orun workspace` is how you say **which workspace you are working in** — once,
instead of on every command. Every Orunbase command (`orun secrets`,
`orun integrations`, `orun policy`, `orun skills`, `orun run --remote-state`)
runs against exactly one workspace; this is where that choice lives.

```bash
orun workspace                    # what am I working in, and why?
orun workspace list               # every workspace this session can reach
orun workspace use <ws-id|slug>   # select it (persisted)
orun workspace use                # no argument: pick from a numbered list
orun workspace use --clear        # unselect
orun workspace create <name>      # create a new one (--account when you own several accounts)
orun workspace <ws-id|slug>       # look one workspace up on the platform
```

`ws` is an alias for the whole tree (`orun ws list`), and `switch`/`select` are
aliases for `use`.

## Selecting one

```bash
$ orun workspace list
   SLUG   WORKSPACE ID  ROLE   ACCOUNT
   ogpic  ws_91J9CD5W   owner  acme
   halo   ws_7F3AQ2P1   owner  acme
   lumen  ws_K4M8ZX02   owner  lab

None selected. Choose one: `orun workspace use ogpic`

$ orun workspace use ogpic
✓ working workspace: ogpic (ws_91J9CD5W)
```

The slug, the `ws_…` Workspace ID, and the legacy `org_…` id are all accepted,
case-insensitively. A name that isn't on your session fails with a "did you
mean" suggestion and the list of workspaces you can actually reach — nothing is
persisted on a miss.

The **ACCOUNT** column is the account each workspace belongs to — the billing
and ownership boundary above a workspace, and the value
`orun workspace create --account` takes. It is shown when the platform reports
it and omitted otherwise.

The selection is stored in `~/.orun/config.yaml`:

```yaml
workspace:
  id: ws_91J9CD5W
  slug: ogpic
  setAt: 2026-08-02T06:07:51Z
```

## Where it sits in the resolution chain

A workspace is resolved in this order, most specific first:

1. `--workspace <ws-id|slug>` on the command (`--org` is the legacy alias)
2. `ORUN_WORKSPACE` / `ORUN_ORG`
3. `intent.yaml` → `execution.state.workspace`
4. this repo's Orunbase link ([`orun cloud link`](./orun-cloud.md))
5. **`orun workspace use`**

The selection sits **last on purpose**: it is a default for when nothing else
applies, so choosing a working workspace can never silently retarget a repo
that declares or links its own tenancy. `orun workspace` always names the rung
that actually won:

```bash
$ orun workspace
workspace: ogpic (ws_91J9CD5W)
  from: `orun workspace use`
  change: `orun workspace use <ws-id|slug>`, or --workspace <ws> for one command
```

and says so when something more specific is overriding your selection here:

```bash
$ orun workspace
workspace: halo (ws_7F3AQ2P1)
  from: this repo's Orun Cloud link
  note: `orun workspace use` is set to ogpic, which this repo's Orun Cloud link overrides here
```

`orun workspace use` prints the same note at selection time, so you find out
immediately rather than from a surprising command later.

## When nothing resolves

Commands that need a workspace and have none say exactly that, and offer both
the one-off and the durable fix:

```
✕ specify a workspace: none is selected and none resolves here
  checked: --workspace, ORUN_WORKSPACE/ORUN_ORG, intent.yaml, this repo's link, then `orun workspace use`
  your workspaces: ogpic, halo, lumen
  pick one for good: `orun workspace use ogpic`
  or for this command only: `--workspace <ws-id|slug>`
```

## Creating one

```bash
orun workspace create "Acme Platform" --slug acme-platform
```

```text
✓ workspace created
  id:   ws_XWDRVB8C
  slug: acme-platform
  next: connect integrations (`orun integrations <provider> connect --org ws_XWDRVB8C`)
```

The slug is derived from the name by the server when `--slug` is omitted.
`orun cloud workspace create` is the same command under its older spelling.
Creating a workspace does **not** select it; follow with
`orun workspace use acme-platform`. The creator becomes the workspace's owner,
and it starts on the free plan. Works with a CLI session or with `ORUN_TOKEN`.

### Which account it belongs to

A workspace belongs to an account. A login that owns exactly one account
needs to say nothing; a login that owns **several** must name the one the
workspace lands in, with `--account <ws_…|slug>`:

```bash
orun workspace create "Acme Platform" --slug acme-platform --account acme
```

```text
✓ workspace created
  id:   ws_XWDRVB8C
  slug: acme-platform
  account: acme (ws_91J9CD5W)
  next: connect integrations (`orun integrations <provider> connect --org ws_XWDRVB8C`)
```

The account's slug and its `ws_…` id are both accepted. Without the flag the
platform refuses rather than guess, and the CLI turns the refusal into the
accounts you own and the exact command to rerun:

```text
✕ create workspace: You own more than one account. Name the account this workspace belongs to (accountId). (precondition_failed)

  this login owns more than one account; name the one the workspace belongs to with --account.
  accounts you own:
    ACCOUNT  ACCOUNT ID   NAME
    acme     ws_91J9CD5W  Acme Inc
    lab      ws_K4M8ZX02  The Lab
  rerun with one of:
    orun workspace create 'Acme Platform' --slug acme-platform --account acme
    orun workspace create 'Acme Platform' --slug acme-platform --account lab
```

`orun workspace list` shows which account each existing workspace belongs to.

## Looking one up

`orun workspace <ws-id|slug>` fetches one workspace from the platform and
prints its slug, id, and account. It works with a workspace-scoped API key in
`ORUN_TOKEN`, which carries no membership list and so cannot answer
`orun workspace list`; this is how a headless job confirms which workspace
its key belongs to.

## Headless use

Set `ORUN_TOKEN` to a workspace API key, or `ORUN_TOKEN_FILE` to a file a
runner keeps refreshed. The file wins when both are set: an exported copy of
a rotating token stops working at the first rotation, the file does not.

## Related

- [Workspaces and tenancy](../concepts/workspaces-and-tenancy.md) — the scope ladder and the two kinds of repository link
- [Create a workspace and build it from a baseline](../examples/bootstrap-a-product-from-a-baseline.md)
- [`orun cloud link`](./orun-cloud.md) — bind *this repo* to a workspace/project
- [`orun auth status`](./orun-auth.md) — who you are signed in as
- [Environment variables](../reference/environment-variables.md) — `ORUN_WORKSPACE`, `ORUN_TOKEN`, `ORUN_TOKEN_FILE`
