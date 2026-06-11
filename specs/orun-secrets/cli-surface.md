# CLI surface

> The author/operator experience. Commands follow the existing Cobra structure
> (`cmd/orun/command_*.go`) and reuse the auth/backend resolution already in
> place (`cmd/orun/command_auth.go`, `cmd/orun/remote_config.go`). The guiding
> principle: **set is easy, read is rare, references are declarative.**

## 1. `orun secret` — manage values (write-only + metadata)

```
orun secret set     <KEY> --env <env> [--namespace <ns>] [--personal] [--value-stdin | --value <v>] [--rotation <policy>]
orun secret import  --from-dotenv <file> --env <env> [--namespace <ns>]      # bulk onboarding, write-only
orun secret list    [--env <env>] [--namespace <ns>] [--chain] [--json]      # metadata only — never values
orun secret rotate  <KEY> --env <env> [--value-stdin]                        # new version; raises onRotate redeploys (SD-13)
orun secret revoke  <KEY> --env <env> [--version <n>]                        # tombstone a version (or all)
orun secret reveal  <KEY> --env <env> --break-glass [--reason <s>]           # SINGLE audited human reveal (SD-3)
orun secret versions <KEY> --env <env>                                       # version history (metadata)
orun secret syncs   [--env <env>] [--entity <ref>]                           # materialization state (SD-13)
```

- **`set`** reads the value from stdin by default (`--value-stdin`) so it never
  lands in shell history; `--value` is discouraged and warns. The value goes to
  `orun-api` over TLS and is enveloped server-side (`data-model.md` §3). Prints
  metadata only.
  - **`--personal`** (SD-11) stores a personal overlay for the calling GitHub
    user — resolvable only by them, only on `local-cli`. The daily-dev flow:
    `orun secret set DB_URL --env dev --personal`.
  - `--env base` writes org-shared defaults every environment inherits.
- **`import`** onboards an existing `.env` in one command — parses, uploads
  write-only, prints a per-key summary, and (with `--write-refs`) offers the
  matching `secretEnv:` block to paste into `component.yaml`. This is the first
  ten minutes (`platform-integration.md` §5); migration friction is a security
  feature — every key not imported is a key still in a dotfile.
- **`list --chain`** renders the inheritance view for an env: which keys come
  from `base`, which the env defines, which the caller personally shadows.
  Values never shown.
- **`rotate`** writes the new version and reports the convergence plan: which
  deploy profiles will re-materialize (`onRotate`), with run links as they
  fire.
- **`reveal`** is the only value-returning human command: requires
  `--break-glass` + `--reason`, is gated by an elevated policy action, and
  emits a `secret.revealed` alert event. Expected to be near-zero use.
- Namespace defaults to the linked repo namespace
  (`cmd/orun/remote_config.go:49-76`); `--namespace acme/_shared/<group>`
  targets a shared group (SD-12).

## 2. `orun policy` — manage and test access (portable)

```
orun policy list   [--namespace <ns>]            # documents in scope, by tier: composition → stack → intent
orun policy show   <name>
orun policy test   --ref secret://ns/prod/DB_URL \          # dry-run a decision (calls /v1/policies/evaluate)
                   --as gh:user:@octocat --env prod \
                   --component-type terraform --platform github-actions-oidc
orun policy lint                                 # predicate vocabulary + narrow-only overlay checks (SD-10)
```

`orun policy test` is the Doppler-missing superpower: a platform author can
prove, before shipping a Stack, exactly who can read what from where. It
exercises the real decision engine (`policy-model.md` §5) with hypothetical
facts. `orun policy list` shows each rule's tier and source
(`composition:terraform`, `stack:acme-platform@1.4.0`, `intent`), so "why is
this denied" is answerable locally.

## 3. Declarative references (the common path — no command at all)

Most usage is **not** a command: authors declare `secretEnv` references in
`component.yaml`/`intent.yaml` (`data-model.md` §2.1), composition authors
declare `secretBindings` and `materialize` in the Stack (`data-model.md`
§2.2–2.4). Then:

```
orun plan        # per job:  secrets: [DATABASE_URL@prod, STRIPE_KEY@prod]   (NO values)
                 #           materialize: 2 secrets → worker:api             (SD-13)
                 #           ⚠ 1 key personally overridden for you (dev)     (SD-11)
                 # fails fast if a reference is not grantable here (compile-time check)
orun run         # resolves at step launch, injects as env, redacts logs, materializes on deploy
```

`orun plan --json` includes `secretRefs`, the `materialize` block, and the
compile-time decision per job, so CI and the cockpit can render "this run will
read these secrets and sync them to these targets" for review.

## 4. Orun Cloud / dashboard surface

The hosted console projects the same model — secrets/env-chain, the three
policy tiers with a visual test matrix, the per-component facet beside its
scorecard grade, the audit stream, and GitHub App administration. Detailed in
`platform-integration.md` §4.

## 5. Backend provisioning (existing command, extended)

`orun backend init` already provisions Worker + D1 + R2 + DO + Queues
(`cmd/orun/command_backend.go`). Secrets add: the D1 tables (`data-model.md`
§7) to the embedded migration bundle, the **KEK** as a new Worker secret
(alongside `ORUN_SESSION_SECRET`, set via `SetWorkerSecret`,
`internal/cloudflare/client.go:437`), and the secret/policy/sync routes. No new
infrastructure — `orun backend status` reports KEK presence (name only) and
secret route health.
