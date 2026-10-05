---
title: orun policy
description: List, lint, test, and push the portable SecretPolicy documents that decide who may resolve which secrets, under which run facts.
---

`orun policy` manages and tests the portable secret-access policy (Layer 2) —
the `SecretPolicy` documents that decide *who may resolve which secrets,
under which run facts*. Documents live with your repo and compositions,
grouped by tier; the CLI lists, lints, dry-runs, and pushes them.

## Usage

```bash
orun policy <subcommand> [flags]
```

| Subcommand | What it does |
|---|---|
| `list` | List `SecretPolicy` documents in scope, grouped by tier. |
| `show <name>` | Show one document's rules. |
| `lint` | Check predicate vocabulary and narrow-only overlay rules. Exits non-zero on findings. |
| `test` | Dry-run a two-layer access decision against the backend engine. |
| `push` | Validate and push the resolved tier-tagged documents to the backend. |

## `policy test` — dry-run a decision

```bash
orun policy test --ref secret://acme/api/prod/DATABASE_URL \
  --as workflow --platform ci-oidc --branch main --declared
```

`--ref` and `--as` are required; the rest supply the run facts a real resolve
would carry:

| Flag | Meaning |
|---|---|
| `--ref` | The `secret://` reference to test. |
| `--as` | Subject to test as: `user:<id>`, `team:<slug>`, `service_principal:<id>`, `workflow`, `*authenticated`. |
| `--env` | Environment slug (defaults to the ref's environment). |
| `--component <name>` | The `component.name` fact — the component the job builds. |
| `--platform` | Execution platform: `local-cli` (default), `ci-oidc`, `service`. |
| `--serves-from` | The `servesFrom` fact: `environment`, `project`, `workspace`, `account`. |
| `--branch` | The `trigger.branch` fact. |
| `--declared` | The `trigger.declared` fact. |

:::note `--component-type` was retired
`component.type` was never a resolvable fact — no resolve path can populate
it, so a rule naming it tested green locally and did nothing in production.
The flag now produces a loud error pointing at the replacement: use
`--component <name>` (the one axis a resolve populates authoritatively,
server-side from the lease-verified job), or scope the secret to the component
directly.
:::

## See also

- [Secrets](../concepts/secrets.md) — the resolve postures policy gates
- [Scope references](../reference/scope-references.md) — the `secret://` grammar
- [`orun secrets`](./orun-secrets.md) — managing the secrets themselves
