# orun-cloud — Unified Onboarding (client side)

Status: Proposed. This doc specifies a CLI UX change that collapses the
three-step cloud onboarding (`orun auth login` → `orun cloud link` → `orun run`)
into **one step that "just works"**: a single login that authenticates *and*
links the repo, with the backend endpoint read from the repo's own
`intent.yaml`. It refines `design.md` (§2 tenancy, §7 degradation, §8 config)
and productizes the **"a project is a repo"** bijection already declared in
`orun-cloud/specs/epics/saas-orun-platform/design-v2.md`.

Paired platform epic: `orun-cloud/specs/epics/saas-unified-onboarding/`.

---

## 0. The problem (reproduced)

A user with a remote-state repo today hits a relay of dead ends. From the repo
root, with `intent.yaml` already declaring the backend:

```yaml
# intent.yaml
execution:
  state:
    mode: remote
    backendUrl: https://api-edge-stage.oruncloud.workers.dev
```

```console
➜  multi-tenant-saas orun run
✕ this repo is not linked to an Orun Cloud org/project;
  run `orun cloud link --backend-url https://api-edge-stage.oruncloud.workers.dev`

➜  multi-tenant-saas orun cloud link --backend-url https://api-edge-stage.oruncloud.workers.dev
✕ not logged in to Orun Cloud; run `orun auth login`

➜  multi-tenant-saas orun auth login          # ← also needs the backend URL, which is *already in intent.yaml*
✕ missing backend URL; pass --backend-url, set ORUN_BACKEND_URL, …
```

Three commands, two of which re-demand a `--backend-url` that the repo already
declares. The user is bounced between `run`, `cloud link`, and `auth login`
before anything works.

### Root causes (code reality)

1. **`auth` and `cloud` ignore `intent.yaml`.** Backend-URL resolution
   (`cmd/orun/remote_config.go:resolveBackendURLWithConfig`) reads the precedence
   chain *flag > `ORUN_BACKEND_URL` > intent `execution.state.backendUrl` > user
   config*. But only `orun run` ever passes a loaded intent into it.
   `runAuthLogin` (`cmd/orun/command_auth.go:70`) and `runCloudLink`
   (`cmd/orun/command_cloud.go:115`) both call `requireBackendURL(nil, …)` —
   `nil` intent — and `commandUsesIntent` (`cmd/orun/commands_root.go:206`) does
   **not** list `auth` or `cloud`, so intent auto-discovery never runs for them.
   The endpoint the repo declares is invisible to the two commands that need it
   most.

2. **Linking is a separate, manual, interactive step.** `orun cloud link`
   (`runCloudLink`) is a distinct command the user must remember, run once per
   repo, and answer prompts for (`pickOrgAndCreate` / `pickFromLinks`). Nothing
   does it automatically.

3. **`run` fails closed instead of self-healing.** When logged-in but unlinked,
   `setupRemoteStateHooks` returns `errRepoNotLinked`
   (`cmd/orun/remote_config.go:170`) — a hard stop that hands the user back to
   `cloud link`, even though every input needed to link (git remote, session,
   org list) is already in hand.

4. **The user-facing noun is "project", not "repo".** Output and errors say
   "org/project" even though the platform already models a project as exactly one
   repo (1:1). The vocabulary leaks an internal concept.

The platform side is **already ready** for the fix: the link API creates the
project on demand and **derives the project slug from the repo name** when no
`projectSlug` is given (`apps/state-worker/src/handlers/links.ts:deriveSlugFromRemote`),
and v2 enforces a `project == repo` bijection. The gap is entirely in the CLI's
orchestration and vocabulary.

---

## 1. Principles (delta over `design.md` §1)

These extend, never contradict, the existing principles (local-first forever;
one contract, two servers; same digests everywhere).

1. **One source of truth for "where".** The backend/auth endpoint is a property
   of the **repo**, declared once in `intent.yaml execution.state.backendUrl`.
   Every command — `auth`, `cloud`, `run`, `status`, `logs` — resolves it the
   same way, from the same chain. A flag or env var overrides it; nothing else
   needs to.
2. **One step to get connected.** A single command authenticates **and** links
   the current repo. Connecting a repo to Orun Cloud is not a checklist; it is
   one verb.
3. **A project is a repo.** The user never names, picks, or thinks about a
   "project". The repo *is* the unit. Its name is the git repo name. The only
   thing the system can't infer — *which org* a repo belongs to — is the only
   thing we ever ask, and we ask it at most once.
4. **Self-heal, don't dead-end.** When the CLI has everything it needs to make
   progress (a remote, a session, an unambiguous org), it links automatically
   instead of printing a "now run this other command" error. Errors are reserved
   for genuine ambiguity or genuine failure, and always name the *one* next
   action.

---

## 2. The unified experience

### 2.1 First run, happy path

```console
➜  multi-tenant-saas orun login
→ opening https://app-edge-stage.oruncloud.workers.dev/cli/approve?… in your browser
✓ logged in as rahul@sourceplane.ai
✓ linked this repo → acme/multi-tenant-saas        (backend: api-edge-stage.oruncloud.workers.dev)

➜  multi-tenant-saas orun run                       # just works
```

`orun login`:
1. Resolves the backend URL from `intent.yaml execution.state.backendUrl`
   (flag/env still override). **No `--backend-url` needed.**
2. Runs the existing browser (or `--device`) auth flow against it.
3. Immediately **auto-links the current repo** (§3): one org → silent; many
   orgs → one prompt; remembered thereafter.
4. Prints the connected repo as `org/repo`, never "org/project".

### 2.2 Already logged in, new repo

```console
➜  other-service orun run
✓ linked this repo → acme/other-service             (auto-linked)
… run proceeds …
```

`orun run` in `mode: remote` with a session but no link **auto-links inline**
(§3) instead of failing. If the org is ambiguous and the terminal is
interactive, it prompts once; if non-interactive (CI), it fails with the precise
override (`--org`/`ORUN_ORG`), never a generic 404.

### 2.3 Command surface

| Command | Behavior |
|---|---|
| `orun login` | **New top-level verb.** Auth + auto-link in one step. Alias of an enhanced `orun auth login`. `--device`, `--org <slug>`, `--backend-url`. |
| `orun logout` | **New top-level alias** for `orun auth logout`. |
| `orun auth login` | Now also auto-links after authenticating (same engine as `orun login`). `--no-link` opts out. |
| `orun auth status` | Unchanged surface; "Current Git remote: … (linked)" line now reads `repo` vocabulary. |
| `orun status` | Cloud-aware: logged-in user, backend, and **this repo → org/repo**. |
| `orun cloud link` | **Demoted to advanced/override**, not a required setup step. Used only to *re-link*, switch org (`--org`), or force a different name. No longer referenced by any "do this next" error. |
| `orun cloud unlink` / `status` / `open` | Unchanged. |

`orun auth login` stays the canonical, documented entry (the user asked to
"unify with `orun auth`"); `orun login`/`orun logout` are the friendly
top-level aliases modern CLIs expose. Both run the identical engine.

---

## 3. Auto-link algorithm

A single helper — call it `autoLinkRepo(ctx, backendURL, opts)` in a new
`cmd/orun/cloud_autolink.go` — is shared by `login`, enhanced `auth login`, and
`run`. It is the existing `resolveOrCreateLink` logic
(`cmd/orun/command_cloud.go:164`) repackaged as a non-interactive-first,
self-healing primitive:

```
autoLinkRepo(backendURL, {org, interactive}):
  repo  ← resolveRepoContext(backendURL)          # git remote.origin.url
  if no git remote:        return  noop  (local-only repo; nothing to link)
  if already linked (RepoLink cached): return it
  if isOSSBackend:         persistLocalLink → _local/_local; return   # one contract, two servers
  token ← cloudSessionToken(backendURL)            # requires a session
  resolved ← client.ResolveLinks(token, repo.GitRemote)   # GET /v1/cli/links/resolve
  switch:
    len(resolved.Links) == 1            → use it                      # repo already has a home
    len(resolved.Links)  > 1            → interactive? prompt once : error(--org)
    len == 0:
       org ← chooseOrg(opts.org, resolved.Candidates, sessionOrgs, interactive)
       link ← client.CreateLink(token, org, repo.GitRemote, projectSlug="")  # ← repo name becomes the project
  persistWorkspaceLink(RepoLink); return link
```

`chooseOrg` precedence: `--org`/`ORUN_ORG` → single candidate → single session
org → (interactive) one-time picker → (non-interactive) error naming `--org`.

Key points:

- **Project name = repo name, always.** `CreateLink` is called with an empty
  `projectSlug`, so the platform derives it from the normalized remote
  (`deriveSlugFromRemote`). The CLI never invents or asks for a project name.
- **Ask about org at most once.** The chosen org is cached in the `RepoLink`
  (`~/.orun/config.yaml`), so subsequent commands never re-prompt.
- **Zero-org actors.** If the actor belongs to no org and none can be created
  for the remote, the platform's per-owner default-org materialization (epic
  OV2) applies; until that ships, fail with: *"no org available to link this
  repo; create one at <console> or pass `--org`."*
- **CI keeps working.** GitHub Actions OIDC already resolves scope server-side
  (`OIDCTokenSource.ResolvedScope`); auto-link is a no-op there and the existing
  CI path is untouched.

---

## 4. Backend-URL resolution becomes universal

The fix to root cause #1 is small and surgical:

1. Add `auth` and `cloud` to `commandUsesIntent`
   (`cmd/orun/commands_root.go:206`) so the persistent pre-run auto-discovers
   `intent.yaml` (walks up to the repo root) for them too.
2. Have `command_auth.go` and `command_cloud.go` load the resolved intent
   (`loadResolvedIntentFile(intentFile)`, `cmd/orun/main.go:1097`) and pass it
   into `requireBackendURL(intent, flag)` instead of `requireBackendURL(nil, …)`.

After this, the **unchanged** precedence chain (`design.md` §8) applies to every
command:

```
--backend-url
  > ORUN_BACKEND_URL
  > intent.yaml execution.state.backendUrl      ← now honored by auth + cloud, not just run
  > ~/.orun/config.yaml cloud.url  (backend.url deprecated alias)
```

Net effect: with `backendUrl` in `intent.yaml`, **no command needs
`--backend-url`**. The flag and env var remain as overrides exactly as the user
expects ("a env variable and cli parameter can override that").

> Note: `mode: remote` is not required for the endpoint to resolve — a present
> `backendUrl` is enough for `login`/`cloud` to know where to authenticate.
> `mode` continues to gate whether `run` uses remote state
> (`remoteStateRequested`, `cmd/orun/command_run.go:163`).

---

## 5. `run` self-heals (degradation table delta, `design.md` §7)

One row changes from fail-fast to self-heal; the rest stand.

| Condition | Before | After |
|---|---|---|
| `--remote-state`, no session | fail: "run `orun auth login`" | unchanged — but the message names `orun login` |
| `--remote-state`, repo **unlinked** | fail: "run `orun cloud link`" | **auto-link inline** (§3). Only fail when org is ambiguous *and* non-interactive, naming `--org`/`ORUN_ORG`. |
| backend unreachable | error + `--local` hint | unchanged |

`errRepoNotLinked` (`cmd/orun/remote_config.go:170`) stops being a terminal
state in the interactive path; it survives only as the non-interactive,
ambiguous-org message and is reworded away from `orun cloud link` toward
`orun login --org <slug>`.

---

## 6. Vocabulary: "project" → "repo" in the CLI

All user-visible CLI strings move from project vocabulary to repo vocabulary.
IDs, config keys, and API paths are untouched (internal `projectID`/`projectSlug`
and the `/projects/` route stay — see the platform epic for the phased
surface rename).

- `linked … → org/project` → `linked this repo → org/repo`.
- `orun cloud status` labels: `Project:` → `Repo:`.
- `orun auth status`: "Current Git remote: … (linked)" stays; the link is
  described as a repo, not a project.
- Error copy: "not linked to an Orun Cloud org/project" → "this repo isn't
  connected to Orun Cloud yet — run `orun login`".

The `RepoLink` struct already carries `RepoFullName`; the display layer simply
prefers it over `ProjectSlug` (they are equal under the bijection).

---

## 7. Backward compatibility

- `orun cloud link` keeps working verbatim (incl. `--org`/`--project`,
  non-interactive CI bootstrap). It is demoted in docs, not removed.
- `orun auth login` keeps working; the only behavioral addition is the
  post-auth auto-link, which is suppressible with `--no-link` and is a no-op
  outside a git repo or when already linked.
- Existing cached `RepoLink`s and sessions are honored as-is; no migration.
- OSS single-tenant backend (`isOSSBackend`) still short-circuits to
  `_local/_local` with no link API call — "one contract, two servers" intact.
- CI / OIDC path unchanged.

---

## 8. Implementation milestones (cluster **UO**)

| ID | Deliverable | Done when |
|---|---|---|
| **UO0** | Universal backend-URL resolution | `auth`/`cloud` honor `intent.yaml backendUrl`; `commandUsesIntent` covers them; precedence test extended (`cmd/orun/config_precedence_test.go`). |
| **UO1** | `autoLinkRepo` primitive | Shared helper extracted from `resolveOrCreateLink`; non-interactive-first; unit tests for 0/1/N orgs, no-remote, OSS, already-linked. |
| **UO2** | `orun login` / `orun logout` | New top-level verbs; `auth login` gains auto-link + `--no-link`; one happy-path e2e. |
| **UO3** | `run` self-heal | Unlinked interactive `run` auto-links; ambiguous non-interactive fails naming `--org`; degradation test updated. |
| **UO4** | Vocabulary pass | All CLI strings repo-first; `cloud link` demoted in `--help` long text and `cli-surface.md`. |
| **UO5** | Docs | `README.md` getting-started gains a one-line cloud onboarding; `cli-surface.md` reordered around `orun login`. |

UO0 alone fixes the reproduced bug; UO1–UO3 deliver the one-step experience;
UO4–UO5 finish the vocabulary and docs.

---

## 9. Open questions

1. **Top-level verb naming.** `orun login` (recommended) vs only enhancing
   `orun auth login`. Recommendation: ship both; `auth login` canonical, `login`
   alias.
2. **Auto-link in `run` by default.** Recommended **on** for interactive
   sessions, **off** (fail-with-`--org`) for non-interactive, never silent in
   CI. Could be gated behind `execution.state.autolink: true` if teams want it
   explicit — deferred unless requested.
3. **Rename depth for "project".** CLI strings now; console/route/DB rename is
   the platform epic's call (phased). See `saas-unified-onboarding`.
