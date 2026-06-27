# OIDC CI Tenancy

> Cross-repo spec. Mirrored in **`sourceplane/orun`** (`specs/oidc-ci-tenancy/`)
> and **`sourceplane/orun-cloud`** (`specs/epics/oidc-ci-tenancy/`). Keep the two
> copies in sync; each repo owns the half marked for it in §6.
>
> Working name `oidc-finetune` was renamed to `oidc-ci-tenancy` — it spans more
> than OIDC token shape: the **org claim**, the **repo allow-list**, and
> **project = repo** for the credential-free CI path.

| | |
|---|---|
| **Status** | Proposed (server half largely shipped — see §2.2) |
| **Repos** | `sourceplane/orun` (CLI, Go), `sourceplane/orun-cloud` (platform, TS) |
| **Pairs with** | orun-cloud OV3 (OIDC exchange), OC #184/#185/#186 (allow-list) |

## 1. Problem

GitHub Actions OIDC lets CI authenticate with **no stored secret**. But the
*tenancy* a workflow acts on — which org and project — is under-declared and
under-enforced today, and the **repo allow-list** that gates CI just landed
server-side. Three concrete asks drive this spec:

1. **Enforce the org claim is sent** when connecting via OIDC — declared in a
   committed, reviewable place, not only a forgettable CLI flag.
2. **OIDC must check the Orun Cloud repo allow-list** — now shipped server-side;
   the CLI must consume it and surface denials *actionably*.
3. **project = repo**, with a clear path to onboard a repo that has never been
   synced.

## 2. Current reality (cited)

### 2.1 CLI — `sourceplane/orun`

- OIDC selection (`GITHUB_ACTIONS` + `ACTIONS_ID_TOKEN_REQUEST_*`) and the
  exchange to `POST /v1/auth/oidc/exchange` are implemented; audience is frozen
  at `orun-cloud` — `internal/remotestate/auth.go:44,96-184`.
- The exchange **already sends an `org` claim**, but it is only ever filled from
  `--org` / `ORUN_ORG` / the cached link (via `ResolveOptions.Org`) —
  `auth.go:58-60`, `cmd/orun/command_run.go:461`, `cmd/orun/catalog_push.go`.
- **`intent.yaml` has no `org`/`project` field** — `IntentExecutionState` is
  `{mode, backendUrl, autopushCatalog}` — `internal/model/intent.go:31-44`.
- Scope precedence reads flag → env → cached link, **not** intent —
  `cmd/orun/remote_config.go:71` (`resolveScope`).
- Unlinked repo fails fast with `errRepoNotLinked` — `remote_config.go:189`;
  auto-link is **skipped under `GITHUB_ACTIONS`** — `command_run.go:481`.

### 2.2 Platform — `sourceplane/orun-cloud` (shipped, last cycle)

- **Allow-list = active workspace links.** `hasActiveWorkspaceLink(org, project)`
  probes `state.workspace_links WHERE status='active'` —
  `packages/db/src/state/repository.ts` (#184).
- **OIDC is gated twice.** At **mint**, the exchange verifies the GitHub token,
  resolves the repo to `(org, project)`, gates on the link's CI settings, and
  binds the minted token's scope. At **push**, `requireWorkflowRepoAllowed`
  re-probes the link before any index/R2 write —
  `apps/state-worker/src/authz.ts`, `handlers/objects.ts` (#184). Unlinking a
  repo immediately revokes CI's ability to push (defense-in-depth).
- **Denials are `404`, not `403`** — deliberate *resource-hiding* ("not a
  member" and "not allowed" collapse to the same Not-Found) — `authz.ts`.
- **project = repo is explicit.** The project slug is derived from the remote
  (`deriveSlugFromRemote`) and the project is **created on demand** on
  `POST /v1/organizations/{orgId}/cli/links` — `handlers/links.ts`.
- **Allow-list listing:** `GET /v1/organizations/{orgId}/cli/links` (#185).
- **Console "add from GitHub"** adds repos to the allow-list (#186).

## 3. Goals / non-goals

**Goals**
- A repo *declares* its tenancy (org) in a committed file; the claim is always
  sent and optionally *required*.
- Allow-list denials become *actionable* CLI guidance.
- Onboarding an unsynced repo is one frictionless, explicit step.
- `project = repo` is first-class.

**Non-goals**
- A client-side allow-list (the server is the single source of truth).
- **Silent auto-provision of an unknown repo on the OIDC path** — explicitly
  rejected (Decision D1); it would undercut the deny-by-default gate just
  shipped.
- Changing the server's `404` resource-hiding posture.

## 4. Design

### 4.1 Org claim (Ask 1) — declare in `intent.yaml`, override by flag

Add to `intent.yaml`:

```yaml
execution:
  state:
    backendUrl: https://api.orun.cloud
    org: acme          # slug or org_… — the declared, enforced tenancy
    # project: <repo>  # advanced override only; default is the repo (see 4.3)
    requireOrg: true   # optional strict mode (default: implied when org is set)
```

- **Why intent.yaml, not a flag:** enforcement that lives in a flag isn't
  enforcement — a flag is per-invocation and easy to omit in one CI job of
  twenty. The org is a property of the repo's identity, so it belongs in the
  committed artifact that already declares the backend. (This is the documented
  direction: orun-cloud `design-v2.md §3 / DV5`, which reverses the older
  "org/project never from intent" rule.)
- **Precedence (unchanged):** `--org` → `ORUN_ORG` → `execution.state.org` →
  cached link.
- **Always send** the resolved org in the exchange body and the API-key request.
- **Strict mode:** when `org` is declared (or `requireOrg: true`), a
  non-interactive remote op with no resolvable org **fails fast** with a message
  pointing at `execution.state.org`, instead of silently exchanging an empty
  claim and landing in an ambiguous scope.

### 4.2 Allow-list (Ask 2) — consume, don't replicate

The server owns the allow-list (shipped). The CLI's job:

- **Always send the claim** so the server *can* enforce `claim ⊆ authorized`.
- **Disambiguate denials.** Because denials are `404` *resource-hiding*, the CLI
  **must not** assert "not allow-listed" from the status code alone. When a
  workflow/remote op 404s where a link is expected, the CLI consults
  `GET …/cli/links` and, only if the repo is genuinely absent, emits:
  > `sourceplane/orun` isn't allow-listed for org `acme` — add it from the
  > console (Git Repos → add from GitHub) or run `orun cloud link`.
  If the listing is itself empty/denied, degrade to the generic
  not-found/not-linked message (never over-claim).
- **Pre-flight:** add `orun cloud check` — answers "is this repo allow-listed for
  the resolved org?" *before* CI runs, turning a mysterious CI `404` into a
  one-command local diagnosis.

### 4.3 project = repo (Ask 3)

Already explicit server-side. CLI changes:
- Keep the project **derived from the repo** by default.
- Demote `--project` / `execution.state.project` to an **advanced override**
  (rename / monorepo-split); the common path declares only `org`.

### 4.4 Onboarding an unsynced repo (Ask 3) — explicit, not silent

Reconciled with the shipped deny-by-default gate:
- **No silent auto-provision on OIDC.** A never-added repo's CI push is denied by
  design; the OIDC mint itself already requires the link.
- **Sync = explicit add**, made frictionless: console *add-from-GitHub*, or
  `orun cloud link` (works for any git remote, trust-free; creates the project on
  demand). The CLI points at exactly these from the denial path.
- For a CI-only repo, document the **one-time** add (from a dev machine or the
  console); it cannot bootstrap itself from inside the gated workflow.

## 5. CLI ↔ server contract

| Endpoint | Purpose | Notes |
|---|---|---|
| `POST /v1/auth/oidc/exchange` | mint workflow token | body `{token, org?}` → `{accessToken, expiresAt, orgId, projectId}`; `org` checked `⊆` link/installation |
| `GET /v1/organizations/{orgId}/cli/links` | list allow-list | powers `orun cloud check` and denial disambiguation (#185) |
| `POST /v1/organizations/{orgId}/cli/links` | create/sync link | project created on demand; **human/CLI path only**, not workflow |
| `GET /v1/cli/links/resolve?remoteUrl=` | candidate orgs/projects | powers the link picker |

Error codes the CLI maps: **`404`** denial/missing (resource-hiding — do not
infer "forbidden"), `412` entitlement/limit, `409` already-linked, `422` bad
remote.

## 6. Work breakdown

### orun (CLI) — this repo's half
- [ ] Add `org`, `project`, `requireOrg` to `IntentExecutionState`
      (`internal/model/intent.go`).
- [ ] Wire `resolveScope` / `ResolveOptions.Org` to read the intent org/project
      at the right precedence (`cmd/orun/remote_config.go`).
- [ ] Populate `OIDCTokenSource.Org` from the resolved org on every remote path
      (run, catalog push, plan --push-catalog).
- [ ] Strict/`requireOrg` enforcement with an actionable fail-fast error.
- [ ] Consume `GET …/cli/links`; map a workflow/remote `404` to the
      "add your repo" message (with the resource-hiding caveat).
- [ ] `orun cloud check` pre-flight command.
- [ ] Demote `--project` to advanced; update CLI + configuration docs.

### orun-cloud (platform) — mostly shipped
- [x] Allow-list gate at mint + push (#184).
- [x] `GET …/cli/links` allow-list listing (#185).
- [x] Console add-from-GitHub (#186).
- [ ] Confirm/document the stable exchange denial shape the CLI relies on
      (remains `404` resource-hiding).
- [ ] Confirm the exchange does **not** auto-create on the workflow path (D1).

## 7. Open decisions

- **D1 — auto-provision an unknown repo on first CI?** **No** (recommended):
  align with deny-by-default; onboarding is an explicit add.
- **D2 — `requireOrg` default?** Strict **when `org` is declared** (recommended)
  vs always-strict-in-CI.
- **D3 — add `execution.state.project`?** **org-only first** (recommended);
  derive project from the repo.
- **D4 — denial UX under resource-hiding?** Consult the listing to phrase the
  message; **never over-claim** "forbidden" from a `404`.

## 8. Test plan

- **CLI:** intent `org`/`project`/`requireOrg` parse + precedence; OIDC exchange
  sends the declared org; `requireOrg` fail-fast; `404` → listing → actionable
  message; `orun cloud check` happy/denied paths.
- **Platform:** covered by the #184/#185 suites; add a contract test pinning the
  `404` denial shape the CLI keys on.

## 9. Rollout

1. **Phase 1 (CLI-only, ships now):** intent `org` field + precedence +
   always-send + `requireOrg` + the denial → actionable message. No platform
   dependency (the endpoints exist).
2. **Phase 2:** `orun cloud check`; project = repo polish (`--project` demotion).
3. **Phase 3:** golden-path GHA workflow docs (`permissions: id-token: write`,
   `execution.state.org`), configuration reference updates.
