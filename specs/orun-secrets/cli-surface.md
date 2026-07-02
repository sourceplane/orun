# CLI surface (v3)

> The author/operator experience. Commands follow the existing Cobra structure
> (`cmd/orun/command_*.go`) and reuse the auth + scope resolution already in
> place (`orun auth login` auto-links; workspace/project default from the
> cached `RepoLink`, `internal/cliauth/types.go:74-88`; `--workspace`/`--org`
> flag pair per the v2.20 vocabulary). The guiding principle: **set is easy,
> read is rare, references are declarative.**

## 1. `orun secrets` — manage values (write-only + metadata)

```
orun secrets set      <KEY> --env <env> [--project <p> | --workspace | --account]
                            [--personal] [--value-stdin | --value <v>]
                            [--rotation <policy>] [--locked]
orun secrets import   --from-dotenv <file> --env <env> [--write-refs]     # bulk onboarding, write-only
orun secrets list     [--env <env>] [--chain] [--json]                    # metadata only — never values
orun secrets rotate   <KEY> --env <env> [--value-stdin]                   # append version; raises onRotate (SD-13)
orun secrets revoke   <KEY> --env <env> [--version <n>]                   # tombstone; alias: rm
orun secrets reveal   <KEY> --env <env> --break-glass --reason <s>        # SINGLE audited human reveal (SD-3)
orun secrets versions <KEY> --env <env>                                   # version history (metadata)
orun secrets syncs    [--env <env>] [--entity <ref>]                      # materialization state (SD-13)
```

- **Scope flags (v3, replaces v2's `--namespace`).** Default scope is the
  linked **project** at the given `--env` (environment scope). `--project <p>`
  targets another project you hold `secret.write` on; `--workspace` writes a
  workspace-shared row; `--account` writes an account-wide row. `--locked`
  (account/workspace scope only) sets `overridable: false` — lower rungs can
  no longer shadow the key (SD-12′). There is no `_shared/<group>` flag; the
  chain rungs are the sharing model.
- **`set`** reads the value from stdin by default so it never lands in shell
  history; `--value` warns. The value is enveloped server-side; the command
  prints metadata only.
  - **`--personal`** (SD-11′) stores a personal overlay for the calling
    subject — resolvable only by them, only on `local-cli`. The daily-dev
    flow: `orun secrets set DB_URL --env dev --personal`.
  - Project-wide defaults (v2's `base`): `orun secrets set SMTP_HOST
    --project api` with no `--env` writes the project-scope rung every
    environment inherits.
- **`import`** onboards an existing `.env` in one command — parses, uploads
  write-only, prints a per-key summary, and (with `--write-refs`) offers the
  matching `secretEnv:` block for `component.yaml`. Migration friction is a
  security feature — every key not imported is a key still in a dotfile.
- **`list --chain`** renders the inheritance view for an env: which keys come
  from account / workspace / project / environment, which are locked, and
  which the caller personally shadows. Values never shown.
- **`rotate`** appends the new version and reports the convergence plan: which
  deploy profiles will re-materialize (`onRotate`), with run links as they
  fire.
- **`reveal`** is the only value-returning human command: requires
  `--break-glass` + `--reason`, is gated by the elevated `secret.reveal`
  action (owner/admin only), and emits a `secret.revealed` alert event.
  Expected to be near-zero use.

## 2. `orun policy` — manage and test access (portable)

```
orun policy list                                  # documents in scope, by tier: composition → stack → intent
orun policy show   <name>
orun policy test   --ref secret://acme/api/prod/DB_URL \
                   --as team:platform-admins --env prod \
                   --component-type terraform --platform ci-oidc
orun policy lint                                  # predicate vocabulary + narrow-only overlay checks (SD-10)
orun policy push                                  # push resolved tier-tagged documents to the backend
```

`orun policy test` is the Doppler-missing superpower: a platform author proves,
before shipping a Stack, exactly who can read what from where. It exercises the
real decision engine (`POST …/config/secret-policies/evaluate`) with
hypothetical facts and reports **both layers**: the Layer-1 role decision (with
`via` provenance — direct / team / account_cascade) and the Layer-2 rule id.
`orun policy list` shows each rule's tier and source
(`composition:terraform`, `stack:acme-platform@1.4.0`, `intent`), so "why is
this denied" is answerable locally.

## 3. Declarative references (the common path — no command at all)

Most usage is **not** a command: authors declare `secretEnv` references in
`component.yaml`/`intent.yaml`, composition authors declare `secretBindings`
and `materialize` in the Stack (`data-model.md` §2). Then:

```
orun plan        # per job:  secrets: [DATABASE_URL@prod (environment), STRIPE_KEY@prod (workspace)]
                 #           materialize: 2 secrets → worker:api            (SD-13)
                 #           ⚠ 1 key personally overridden for you (dev)    (SD-11′)
                 # fails fast if a reference is not resolvable/grantable (compile-time check)
orun run         # claim (lease) → resolve → inject → redact → materialize on deploy
```

`orun plan --json` includes `secretRefs` (with `grant`/`servesFrom`/
`personalShadow` annotations), the `materialize` block, and the compile-time
decision per job, so CI and the console can render "this run will read these
secrets and sync them to these targets" for review.

## 4. Orun Cloud console surface

The hosted console projects the same model — chain view, three policy tiers
with a visual test matrix, the per-component facet beside its scorecard grade,
the audit stream, and the access explainer. Detailed in
`platform-integration.md` §4.

## 5. Self-hosted backend (contract parity, deferred surface)

The state-api contract is normative for both the hosted platform and the OSS
self-host backend (`_local/_local` scope). The secret routes ride the same
contract, so a self-hosted deployment gains them by implementing contract §4
v3 — with a **customer-provided KEK** (bring-your-own custody, documented sharp
edges; Q-2). The D1 schema translation of `data-model.md` §7 and the
`orun backend init` bundle extension are tracked as deferred work (D-2 in
`risks-and-open-questions.md`) — the hosted platform is the canonical
implementation and ships first.
