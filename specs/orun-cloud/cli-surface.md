# orun-cloud — CLI Surface

Status: Draft. Additions and changes only; everything else is untouched.
Naming follows the existing command tree (`auth`, `cloud`, `catalog`,
`secrets` is new).

## 1. `orun auth` (changed: real platform behind it)

```
orun auth login                # browser loopback against the platform (OC1)
orun auth login --device       # platform device flow (replaces GitHub device flow)
orun auth status               # user, orgs + roles, session expiry, backend URL
orun auth logout               # POST /v1/auth/cli/revoke + clear ~/.orun/credentials.json
orun auth token                # print a fresh short-lived access token (CI escape hatch)
```

`--backend-url` stays on `login` for self-hosted backends and is persisted to
user config on success.

## 2. `orun cloud` (changed: link gains the picker; new subcommands)

```
orun cloud link                # resolve git remote → pick/create org/project → cache RepoLink
orun cloud link --org acme --project platform    # non-interactive (CI/bootstrap)
orun cloud unlink              # drop the local RepoLink (server link untouched)
orun cloud status              # linked org/project, backend URL, contract version, last sync
orun cloud open                # open the project's console page (runs view) in the browser
```

## 3. `orun run` / `orun status` / `orun logs` (changed: cloud parity)

```
orun run --remote-state        # as today, now against the platform; plan blob synced first
orun run --local               # explicit escape hatch when cloud is configured but down
orun status [--watch]          # lists cloud runs when remote is configured (bridge.Source)
orun logs [--failed] --follow  # tails cloud logs via fromSeq cursor
```

`--exec-id` keeps working for resume; run IDs are the same ULIDs locally and
remotely.

## 4. `orun catalog` (new: push)

```
orun catalog push                      # sync resolved snapshot + advance head (current env scope)
orun catalog push --environment prod   # env-scoped head
orun catalog refresh --push            # refresh then push
```

Prints what the server was missing (n blobs, bytes) and the head transition
(`old → new digest`).

## 5. `orun secrets` (new command group)

```
orun secrets set KEY [--environment prod]   # value via stdin or $EDITOR prompt; never an argv arg
orun secrets list [--environment prod]      # metadata only: key, version, scope, rotated, last-used
orun secrets rm KEY [--environment prod]
```

Write-only by design — there is no `orun secrets get`. Values in argv are
rejected (shell-history hazard); stdin/prompt only.

> OC5 ships this `set`/`list`/`rm` subset. The full command group (`rotate`,
> `revoke` — `rm`'s alias, `reveal`, `import`, `versions`, `syncs`) and the
> `orun policy` companion are specified in `specs/orun-secrets/cli-surface.md`,
> which extends this same group on the same write-only contract.

## 6. Exit & error conventions

- Cloud-related failures always print the platform `requestId` when one exists.
- "Not logged in" / "not linked" are distinct, single-line errors naming the
  exact next command.
- Degradation behavior is normative in `design.md` §7.
