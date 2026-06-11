# CLI surface

> The author/operator experience. Commands follow the existing Cobra structure
> (`cmd/orun/command_*.go`) and reuse the auth/backend resolution already in place
> (`cmd/orun/command_auth.go`, `cmd/orun/remote_config.go`). The guiding principle:
> **set is easy, read is rare, references are declarative.**

## 1. `orun secret` — manage values (write-only + metadata)

```
orun secret set   <KEY> --env <env> [--namespace <ns>] [--value-stdin | --value <v>] [--rotation <policy>]
orun secret list  [--env <env>] [--namespace <ns>] [--json]        # metadata only — never values
orun secret rotate <KEY> --env <env> [--value-stdin]                # new version, old retained
orun secret revoke <KEY> --env <env> [--version <n>]                # tombstone a version (or all)
orun secret reveal <KEY> --env <env> --break-glass [--reason <s>]   # SINGLE audited human reveal (SD-3)
orun secret versions <KEY> --env <env>                              # version history (metadata)
```

- **`set`** reads the value from stdin by default (`--value-stdin`) so it never
  lands in shell history; `--value` is discouraged and warns. Encrypts client-free
  — the value goes to `orun-api` over TLS and is enveloped server-side
  (`data-model.md` §3). Prints metadata only.
- **`list` / `versions`** never print values — mirrors `config-worker`'s
  metadata-only listing (`multi-tenant-saas/apps/config-worker/src/handlers/list-secrets.ts`).
- **`reveal`** is the only value-returning human command: requires `--break-glass`
  + `--reason`, is gated by an elevated policy action, and emits a `secret.revealed`
  alert event. Expected to be near-zero use.
- Namespace defaults to the linked repo namespace (`cmd/orun/remote_config.go:49-76`).

## 2. `orun policy` — manage and test access (portable)

```
orun policy list   [--namespace <ns>]                       # SecretPolicy documents in scope (Stack + intent)
orun policy show   <name>
orun policy test   --ref secret://ns/prod/DB_URL \          # dry-run a decision (calls /v1/policies/evaluate)
                   --as gh:user:@octocat --env prod \
                   --component-type terraform --platform github-actions-oidc
orun policy lint                                             # validate predicate vocabulary + narrow-only overlays
```

`orun policy test` is the Doppler-missing superpower: a platform author can prove,
before shipping a Stack, exactly who can read what from where. It exercises the
real decision engine (`policy-model.md` §5) with hypothetical facts.

## 3. Declarative references (the common path — no command at all)

Most usage is **not** a command: authors declare `secretEnv` references in
`component.yaml`/`intent.yaml` (`data-model.md` §2.1) and composition authors
declare `secretBindings` in the Stack (`data-model.md` §2.2). Then:

```
orun plan        # shows, per job:  secrets: [DATABASE_URL@prod, STRIPE_KEY@prod]   (NO values)
                 # fails fast if a reference is not grantable here (compile-time check)
orun run         # resolves references at step launch, injects as env, redacts logs
```

`orun plan --json` includes `secretRefs` and the compile-time decision per job, so
CI and the cockpit can render "this run will read these secrets" for review.

## 4. Orun Cloud / dashboard surface

The hosted console (the Cloudflare-backed Worker/dashboard,
`cmd/orun/command_backend.go`) exposes the same model as a projection:
- **Secrets**: per-namespace/env metadata, version history, rotation, last-used
  (from `secret_audit`) — values never shown except via audited reveal.
- **Policies**: view/edit `SecretPolicy` documents, a visual `policy test` matrix
  (subject × env × platform → allow/deny), and the diff a Stack upgrade would make.
- **Audit**: the `secret.resolved`/`secret.denied`/`secret.revealed` stream, joined
  to runs — "what did deploy #4821 read, under which rule?" (`policy-model.md` §8).
- **GitHub App**: install/refresh the `gh_identity_map`; show team→grant resolution.

## 5. Backend provisioning (existing command, extended)

`orun backend init` already provisions Worker + D1 + R2 + DO + Queues
(`cmd/orun/command_backend.go`). Secrets add: the D1 tables (`data-model.md` §6) to
the embedded migration bundle, the **KEK** as a new Worker secret (alongside
`ORUN_SESSION_SECRET`, set via `SetWorkerSecret`,
`internal/cloudflare/client.go:437`), and the secret/policy routes. No new
infrastructure — `orun backend status` reports KEK presence (name only) and secret
route health.
