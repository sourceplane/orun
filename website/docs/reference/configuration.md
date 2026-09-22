---
title: Configuration
description: Where orun reads configuration from — intent.yaml, component manifests, composition sources, the per-user ~/.orun/config.yaml, and the credential store.
---

`orun` configuration is split across five user-facing surfaces:

1. `intent.yaml`
2. discovered `component.yaml` manifests
3. composition sources declared by `intent.compositions`
4. local CLI config in `~/.orun/config.yaml` for backend defaults, the selected workspace, and repo links
5. the login session in the OS credential store, with `~/.orun/credentials.json` as the fallback

## Intent file

Minimal example:

```yaml
apiVersion: sourceplane.io/v1
kind: Intent

metadata:
  name: demo

env:
  OWNER: sourceplane

discovery:
  roots:
    - services/

environments:
  development:
    parameterDefaults:
      "*":
        region: us-east-1
    env:
      AWS_REGION: us-east-1
```

The intent file is where you define environments, discovery roots, groups, selectors, defaults, and optional inline components. Root-level `env` provides global environment variables shared across all environments.

It also declares where compositions come from:

```yaml
compositions:
  sources:
    - name: example-platform
      kind: dir
      path: ./compositions
```

## Component manifest

Minimal example:

```yaml
apiVersion: sourceplane.io/v1
kind: Component

metadata:
  name: network-foundation

spec:
  type: terraform
  domain: platform-foundation
  env:
    REPO: network-foundation
  subscribe:
    environments: [development, staging, production]
  parameters:
    stackName: network-foundation
    terraformDir: .
```

Components carry type-specific inputs, root-level environment variables, labels, overrides, and dependency declarations. Root-level `env` applies across all subscribed environments and can be overridden per subscription.

### Conditional profile selection

Use `profileRules` to select different execution profiles depending on which trigger fires:

```yaml
spec:
  subscribe:
    environments:
      - name: dev-preview
        profile: plan-only
        profileRules:
          - profile: apply
            when:
              triggerRef: github-push-main
```

The `profile` field is the default fallback. Rules are evaluated in order (first-match-wins). See [profile rules](../concepts/profile-rules.md) for full details.

### Change watches

Declare which global intent sections should mark this component as changed during `--changed` planning:

```yaml
spec:
  change:
    watches:
      - environments
      - groups
      - env
```

Valid values: `environments`, `groups`, `env`, `automation`, `compositions`, `discovery`, `execution`.

Without `change.watches`, global intent changes do not affect the component. See [change watches](../concepts/change-watches.md) for full details.

## Composition sources

Declare composition sources in the intent and plan directly against that intent:

```bash
orun plan --intent intent.yaml
```

Each source can be a local package directory, a packaged archive, or an OCI reference. Orun resolves those sources into a cache and writes a lock file under `<intent-dir>/.orun/compositions.lock.yaml`.

`--config-dir` still works as a compatibility fallback for legacy folder-shaped compositions.

## Environment-specific control

Use CLI flags when you want to scope the effective configuration at compile time:

- `--env` to filter a single environment
- `--changed` and related flags for change-aware planning
- `--view` for DAG-focused render views

Read [environment variables](./environment-variables.md) if you want to control configuration through the shell.

## Environment promotion

Declare promotion dependencies between environments to establish deployment ordering:

```yaml
environments:
  staging:
    activation:
      triggerRefs: [github-push-main]
    promotion:
      dependsOn:
        - environment: preview
          strategy: same-component      # default
          condition: success            # default
          satisfy: same-plan-or-previous-success  # default
          match:
            revision: source            # default
    parameterDefaults:
      "*":
        lane: verify
```

| Field | Values | Default | Meaning |
| --- | --- | --- | --- |
| `environment` | string (required) | — | Environment that must succeed first |
| `strategy` | `same-component`, `environment-barrier` | `same-component` | How to link jobs across environments |
| `condition` | `success` | `success` | Required outcome in the dependency environment |
| `satisfy` | `same-plan`, `previous-success`, `same-plan-or-previous-success` | `same-plan-or-previous-success` | Whether to use DAG edges, gates, or both |
| `match.revision` | `source` | `source` | How to match prior success evidence |

See [environment promotion](../concepts/environment-promotion.md) for detailed behavior.

## User config: `~/.orun/config.yaml`

`~/.orun/config.yaml` is the non-secret, per-user CLI config (`cliauth.Config` in `internal/cliauth/types.go`; read and written by `internal/cliauth/storage.go`, mode `0600`). It never holds tokens.

```yaml
cloud:
  url: https://orun-backend.example.com   # Orunbase, or a self-hosted backend URL
  catalog:
    autopush: false                        # off by default; publishing the catalog is team-visible

workspace:                                 # written by `orun workspace use`
  id: ws_01hxyz
  slug: acme
  setAt: "2026-09-01T10:00:00Z"

repos:                                     # written by `orun cloud link`
  - backendUrl: https://orun-backend.example.com
    gitRemote: git@github.com:sourceplane/orun.git
    repoFullName: sourceplane/orun
    orgId: "org_123"
    orgSlug: acme
    projectId: "proj_456"
    projectSlug: platform
    linkedAt: "2026-09-01T10:05:00Z"
```

| Key | Meaning |
| --- | --- |
| `cloud.url` | The backend URL: Orunbase or a self-hosted backend. When unset, the CLI uses its built-in default (`remotestate.DefaultCloudURL`), which points at the hosted Orunbase API |
| `cloud.catalog.autopush` | Push the catalog snapshot automatically after a successful `orun plan`; equivalent to the intent-level `autopushCatalog` |
| `backend.url` | **Deprecated** alias of `cloud.url`, honoured for one release. `orun` prints a one-line warning when only the alias is set and prefers `cloud.url` when both are |
| `backendBootstrap` | Non-secret metadata written by `orun backend init` for a self-hosted backend: `managedBy` (always `orun-backend-init`), `accountId`, `workerName`, `d1DatabaseName`, `d1DatabaseUUID`, `r2BucketName`, the catalog queue and DLQ names and IDs, `catalogCron`, `backendCommit`, `initAt`. API tokens and secrets are never stored here |
| `workspace` | The working workspace chosen with `orun workspace use`: `id` (the `ws_…` Workspace ID when known, else the `org_…` ID), `slug`, and `setAt` (RFC 3339). It is a **default, not an override** — `--workspace`, `ORUN_WORKSPACE`, a repo's `intent.yaml`, and a repo link all win over it |
| `repos[]` | One `RepoLink` per linked repository: `backendUrl`, `gitRemote`, `repoFullName`, the org/project spine (`orgId`, `orgSlug`, `projectId`, `projectSlug`), the legacy single-tenant fields (`namespaceId`, `namespaceKind`, `repoId`), and `linkedAt` |

`orun auth login` also writes `cloud.url` from the session's backend URL, keeping the deprecated `backend.url` in sync only when it was already set.

## Credential store

The login session (access token, refresh token, their expiries, the user, and the workspaces the user belongs to) is a secret and lives outside `config.yaml`:

- On macOS, the default store is the OS keychain — service `io.sourceplane.orun`, account `cli-session` — when a usability probe succeeds.
- Otherwise the session is written to `~/.orun/credentials.json` with mode `0600`.
- `ORUN_CREDENTIAL_STORE=file` forces the file; `ORUN_CREDENTIAL_STORE=keychain` forces the keychain (macOS) and skips the probe. Test binaries never reach the developer's keychain.

The session is refreshed in place when the access token nears expiry, under a lock so concurrent commands do not race the refresh. `orun auth logout` clears it. Headless automation uses `ORUN_TOKEN` or `ORUN_TOKEN_FILE` instead of a stored session — see [environment variables](./environment-variables.md#cloud-and-authentication).

## Remote state configuration

Add an `execution.state` block to `intent.yaml` to enable remote state coordination through the Orunbase backend (or a self-hosted one):

```yaml
execution:
  state:
    mode: remote
    backendUrl: https://orun-backend.example.com
```

| Field | Values | Meaning |
| --- | --- | --- |
| `mode` | `local` (default) or `remote` | Where execution state is stored |
| `backendUrl` | URI | URL of the backend (Orunbase or self-hosted); when omitted the CLI falls back to `ORUN_BACKEND_URL`, then `~/.orun/config.yaml`, then the built-in default |
| `autopushCatalog` | bool (default `false`) | When `true`, a successful `orun plan` best-effort publishes the resolved catalog and advances the head — but only on the **clean default branch**, debounced so an unchanged catalog is a no-op, and never failing the plan (silent unless `ORUN_VERBOSE`). Equivalent to the user-level `cloud.catalog.autopush`; either being set enables it. For explicit, fail-loud publishing use `orun plan --push-catalog` or `orun catalog refresh --push`. |

The `backendUrl` can also be supplied via `--backend-url` or `ORUN_BACKEND_URL`; those take priority over the intent file.

When neither the flag, environment variable, nor intent file sets a backend URL, `orun` falls back to the `cloud.url` key in [`~/.orun/config.yaml`](#user-config-orunconfigyaml), and finally to the built-in default. `orun cloud link` writes the `repos` entries used for local session-authenticated remote-state runs.

### Workspace/project scope

State calls are scoped to a workspace/project (path `/v1/organizations/{orgId}/projects/{projectId}/state/…`). The scope is resolved with the precedence `--workspace`/`--org` and `--project` flags > `ORUN_WORKSPACE`/`ORUN_ORG` and `ORUN_PROJECT` env > the cached `RepoLink`. The OSS single-tenant backend uses a fixed `_local/_local` scope, so a workspace with no explicit scope keeps working.

Declare the workspace in `intent.yaml` under `execution.state`:

| Field | Values | Meaning |
| --- | --- | --- |
| `workspace` | `ws_…` id, slug, or `org_…` id | The leading, committed tenancy claim. A Workspace ID (`ws_…`) is short and immutable — safe to commit and durable across renames. The declared value is always a Workspace, never the parent Account. |
| `org` | slug or `org_…` id | Retained alias of `workspace` (read either, prefer `workspace`) — existing `org` configs keep working unchanged. |
| `requireOrg` | bool | Strict mode — a non-interactive remote op that resolves no workspace fails fast. Implied whenever `workspace`/`org` is set. |
| `project` | slug or `proj_…` id | Advanced override; the default is `project = repo`, derived from the git remote server-side. |

The `ws_…` / slug / `org_…` value is passed opaquely; the platform resolves it server-side.

When `mode: remote` is set, all three commands that read execution state (`run`, `status`, `logs`) automatically use the backend without requiring `--remote-state` on the command line.

## Related

- [Environment variables](./environment-variables.md) — the shell-level overrides for everything above.
- [Intent model](../concepts/intent-model.md) — the full `intent.yaml` reference.
- [Workspaces and tenancy](../concepts/workspaces-and-tenancy.md) — how the workspace scope is resolved.
- [`orun login`](../cli/orun-login.md), [`orun auth`](../cli/orun-auth.md), [`orun cloud`](../cli/orun-cloud.md), [`orun workspace`](../cli/orun-workspace.md) — the commands that write the user config and credential store.
