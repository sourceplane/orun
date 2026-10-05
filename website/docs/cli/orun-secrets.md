---
title: orun secrets
description: Manage the workspace secrets that secret:// references resolve against. Values are write-only; orun never prints them back.
---

`orun secrets` manages the workspace secrets that
[`secret://` references](../reference/scope-references.md) resolve against —
create, list, rotate, revoke, inspect versions, import, and (break-glass)
reveal. It is the **management** surface: values are written and read here
deliberately; jobs consume secrets only via references (see
[Secrets](../concepts/secrets.md)).

Authoring an **integration-bound** secret (a value minted by a connected
provider) lives under the integration namespace instead — see
[`orun integrations`](./orun-integrations.md) and the
[deprecation note](#from-broker-deprecated) below.

## Usage

```bash
orun secrets <subcommand> [KEY] [flags]
```

| Subcommand | What it does |
|---|---|
| `set <KEY>` | Create a secret (or add a version). `--value` for a static value, `--from-broker` for a provider-rotated one (deprecated — see below). |
| `list` | List secret metadata at the selected scope. `--chain` shows the resolution chain. |
| `import --from-dotenv <file> --env <env>` | Bulk-import a dotenv file into an environment scope. |
| `rotate <KEY>` | Add a new version. With no flags: a metadata bump for static heads, a **re-mint from the connected parent** for provider-rotated heads. `--remint` forces the re-mint posture (break-glass rotate-now). |
| `revoke <KEY>` | Revoke the secret. |
| `versions <KEY>` | List the version history. |
| `reveal <KEY> --break-glass --reason <why>` | Print a value — deliberately loud. Requires both flags; the preflight reports **every** missing precondition together (break-glass, reason, scope) with a ready-to-run example, not one error at a time. |

Persistent flags: `--backend-url`, `--workspace <ws-id|slug>` (which workspace
to talk to; `--org` is the legacy alias). Select one once with
[`orun workspace use <ws>`](./orun-workspace.md) and every command below
inherits it.

### Scope selection

Every subcommand that touches a secret accepts the shared scope selector:

| Flag | Rung |
|---|---|
| `--env <slug>` | Environment scope (within the linked project) |
| `--project` | Project scope (boolean — the linked project) |
| `--shared` | Workspace-shared scope (boolean — every project inherits) |

Workspace- and project-scoped secrets are fully reachable from the CLI —
`reveal`, `rotate`, and `versions` all take the selector, not just `--env`.

:::note `--shared` is the rung; `--workspace` is the tenancy
`--shared` picks *which rung of this workspace* a key lives at. `--workspace`
picks *which workspace* the command talks to. `--workspace` used to be the
rung boolean; the old spelling now fails with a one-line pointer to `--shared`
rather than silently changing meaning.
:::

### `set` flags

| Flag | Meaning |
|---|---|
| `--value` | The static value. Mutually exclusive with `--from-broker`. |
| `--personal` | A personal (owner-only) secret. |
| `--locked` | Lock the secret (implies `--shared`). |
| `--rotation <cadence>` | Rotation cadence for a rotated secret (e.g. `30d`). |
| `--display-name <s>` | Human display name. |
| `--from-broker <provider/template>` | Create a provider-rotated secret from a connected parent (deprecated spelling — see below). |
| `--connection <int_…>` | The integration connection backing `--from-broker`. |
| `--grace-seconds <n>` | Seconds the prior token stays valid after a rotation (server default 24h). |
| `--deliver-target <t>` | Materialize target re-delivered on rotation. |

### Output and errors

- **`--json` on every subcommand** — metadata only; no secret value is ever
  routed to JSON output.
- A typo'd subcommand fails **non-zero** with a "did you mean" suggestion —
  it never silently prints group help and exits 0.
- Provider-rotated secrets carry their rotation provenance in `--json`
  metadata.

## Examples

```bash
# A static environment-scoped secret
orun secrets set DATABASE_URL --env prod --value "$DB_URL"

# Import a dotenv file into the dev environment
orun secrets import --from-dotenv .env.dev --env dev

# A provider-rotated secret minted from a Cloudflare connection,
# re-minted every 30 days with a 1h overlap
orun secrets set CF_API_TOKEN --from-broker cloudflare/workers-deploy \
  --connection int_0123 --rotation 30d --grace-seconds 3600

# Rotate now, from the provider parent (break-glass)
orun secrets rotate CF_API_TOKEN --remint

# Inspect a workspace-scoped secret's history
orun secrets versions SHARED_LICENSE_KEY --shared --json

# Reveal — loud and audited, both flags required
orun secrets reveal DATABASE_URL --env prod --break-glass --reason "incident #4711"
```

## Provider-rotated secrets

`--from-broker provider/template` creates a secret whose versions are
**minted by the platform from a connected provider parent** — the CLI never
reads, sends, or prints a value (the create request carries no value field at
all). The server mints v1 from the connected parent; the rotation engine keeps
it fresh on the `--rotation` schedule; `rotate --remint` forces a fresh mint
immediately. `--from-broker` is mutually exclusive with `--value` and
`--personal`.

### `--from-broker` deprecated {#from-broker-deprecated}

Integration-namespaced authoring supersedes it. `orun secrets set
--from-broker` **keeps working for one release** and prints the exact
replacement for your invocation on stderr — the
[`orun integrations <provider> secret create`](./orun-integrations.md) form
with the connection, template, `--mode rotated`, and the rotation/grace/
deliver-target/display-name/scope flags you actually passed.

## See also

- [Secrets](../concepts/secrets.md) — how references, optional refs, and job output secrets work
- [Scope references](../reference/scope-references.md) — the `secret://` grammar
- [`orun integrations`](./orun-integrations.md) — integration-bound secret authoring
- [`orun policy`](./orun-policy.md) — test who may resolve what
