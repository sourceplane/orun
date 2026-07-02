# Spec: orun-secrets

**orun gains a first-class secret store — a Doppler-shaped experience built to
orun's invariants and, in v3, re-architected onto the platform that actually
shipped. Secret *values* live envelope-encrypted in Orun Cloud's
`config-worker` (Supabase Postgres); the repo, the plan, and every object carry
only typed `secret://` *references*. Resolution walks the platform's own scope
chain — `personal → environment → project → workspace → account` — the same
chain WID7 shipped for settings and reserved for secrets. Access is decided in
two layers: the shipped role×scope RBAC engine (its dormant `secret.*` actions
activated) and a portable, Stack-shipped `SecretPolicy` conditions document
over four axes orun already owns — who, which component, which
environment/trigger, and which execution platform. Secrets reach pipeline jobs
by lease-bound runner injection with log redaction, and reach *deployed
applications* by policy-governed materialization into the target platform's
native store. The whole surface projects into the service catalog as an entity
facet, scorecard-checkable like any other catalog fact.**

> **The defining property: a secret value never becomes content.** orun's
> object graph is immutable, content-addressed, and dedup'd org-wide on push. A
> plaintext (or even ciphertext) secret committed into that graph is
> unrecallable. So secrets are the one orun primitive that lives *outside* the
> graph — only references and resolved-version provenance are content. This is
> the load-bearing carve-out the whole design protects.

## Status

| Field | Value |
|-------|-------|
| Status | **Draft (v3) — re-architected to the shipped platform; for review** |
| Supersedes | v2 of this spec (which assumed the self-hosted D1 backend as the store, GitHub-numeric-id identity + a `gh_identity_map` subsystem, `namespace`/`base`/`_shared` tenancy, and a from-scratch policy engine) and the placeholder secret sections of `specs/orun-cloud/` |
| Builds on (all shipped) | `config-worker` secret CRUD + AES-256-GCM write-only encryption (070); the WID7 settings scope chain + `overridable` guardrails (430) — reserved for secrets by its own design notes; the WID6 account-RBAC cascade + TM6b1 per-action provenance (`packages/policy-engine`); the run/lease coordination plane (OP2 + the DO-backed default path); credential-agnostic CI auth → one ActorContext (OV3); `appendEventWithAudit`; the catalog entity-extension seam; the state-api contract |
| Platform slice | **OV8** in `orun-cloud/specs/epics/saas-orun-platform/` → detailed in `orun-cloud/specs/epics/saas-secret-manager/` |
| apiVersion | `orun.io/v1` (`SecretPolicy`, composition `secretBindings` + `materialize`) |
| Milestone prefix | **SEC** (`SEC0 → SEC7`) |

## What changed in v3 (gaps in v2, closed)

v2 nailed the security core but designed against a platform that shipped
differently. v3 keeps SD-1/3/5/6/7/8/9/10/13/14 and re-grounds the rest:

1. **Substrate.** The store is `config-worker` on Postgres — where a real
   (write-only) secret store already runs — not the self-hosted D1 bundle.
   The state-api contract remains the seam; self-host parity is deferred work,
   not a design premise.
2. **Tenancy.** `namespace`, the reserved `base` env, and `_shared/<group>`
   are replaced by the platform spine: account → workspace → project (== repo)
   → environment. The grammar is
   `secret://<workspace>/<project>/<env>/<KEY>[@v]`. v2's deferred D-1 rename
   is **done**.
3. **Inheritance = the shipped chain.** WID7's scope-resolution chain
   (settings today, explicitly reserved for secrets) becomes the secret
   resolution model, with a personal rung on top — plus WID7's guardrail
   (`overridable:false`) so account/workspace keys can be **locked**, which
   Doppler cannot do.
4. **Identity.** Subjects are platform principals (users, teams, service
   principals, `workflow` actors) — the identities every enforcement point
   already verifies. The v2 `gh_identity_map` subsystem is deleted; GitHub
   team sync is a later integrations-worker feature.
5. **Policy = two layers.** Layer 1 activates the policy-engine's dormant
   `secret.read/write/value.use` actions (role matrices, account cascade,
   provenance — all shipped). Layer 2 is the portable `SecretPolicy`
   conditions document (the four axes), evaluated at resolve. No second
   engine is built where one exists.
6. **The resolve is lease-bound for real.** Bound to the shipped coordination
   plane with **`leaseEpoch`** in the body (the contract sketch omitted it —
   a swept stale runner would be indistinguishable, the race heartbeat/complete
   already solve). state-worker verifies the lease; config-worker decrypts
   behind a service binding; the decrypt path is the **first in the codebase**
   (today's store cannot decrypt at all).
7. **Versioned values.** An append-only `secret_versions` table replaces
   today's rotate-overwrites-in-place, unlocking history, pinning, rollback,
   and per-version revoke.
8. **Key hierarchy.** Per-workspace DEKs wrapped by a KEK in Cloudflare
   Secrets Store (entitlement already confirmed) replace the single static
   key; `keyId` envelopes with lazy `k0` migration.
9. **Service tokens without a token system.** Doppler's service tokens map to
   existing `sk_` service principals + a pinning `SecretPolicy` (SD-19).

## Decisions locked (v3)

| # | Decision |
|---|----------|
| SD-1 | Values live in Orun Cloud, **never** in the object graph — only `secret://` references |
| SD-2′ | Envelope AES-256-GCM + `keyId`; per-workspace DEK, KEK in Cloudflare Secrets Store; append-only versions; decrypt only in the resolve handler |
| SD-3 | Write-only API + a single audited break-glass reveal |
| SD-4′ | Subjects are platform principals; GitHub team sync is a follow-up integration |
| SD-5 | Policy is a portable document that ships in the Stack |
| SD-6 | Deny-by-default; enforced at compile time (visible in `orun plan`) and fetch time (authoritative) |
| SD-7 | Locked predicate vocabulary in v1; CEL is the named upgrade path |
| SD-8 | Resolved values redacted once at capture, upstream of every log sink |
| SD-9 | Inter-job passing = declared job `outputs`; sensitive outputs route through the secret backend |
| SD-10 | Three-tier policy placement: composition-attached → Stack `policies/` → intent overlays; narrow-only downward |
| SD-11′ | Resolution chain `personal → environment → project → workspace → account` (the WID7 chain + personal) |
| SD-12′ | Sharing = workspace/account scope rows; visible (plan `servesFrom`, `--chain`, facet) and lockable |
| SD-13 | Runtime delivery = materialization into the target platform's native store; plan-visible, provenance-tracked, rotation-driven |
| SD-14 | Secrets are a catalog facet (`extensions.x-orun-secrets`), scorecard-checkable |
| SD-15′ | The state-api contract is normative; v3 revises §4 — one API, one CLI |
| SD-16 | `orun secrets` CLI group with scope flags |
| SD-17 | Two-layer policy: shipped RBAC engine + `SecretPolicy` conditions; one decision id |
| SD-18 | Resolve is lease-bound (`leaseEpoch`), verified for both coordination backends |
| SD-19 | Service tokens ≡ `sk_` service principals under a pinning policy |

## Positioning (why not just use X)

| | GitHub Actions secrets | Doppler | Vault | **orun secrets** |
|---|---|---|---|---|
| Identity | repo collaborators | proprietary users/SCIM | own auth backends | **your platform org — users, teams, CI workflows, one ActorContext** |
| Inheritance | none | root + branch configs | none | **account → workspace → project → environment (+ personal), with lockable keys** |
| Policy | env protection rules | token-per-config | path ACL policies | **role RBAC + four-axis conditions document, ships with the Stack** |
| Component-aware | no | per-project | no | **yes — grants conditioned on component type/domain** |
| Plan visibility | no | no | no | **`orun plan` shows every secret a run will read, and from which scope** |
| Runtime delivery | n/a | syncs + SDK | agent/SDK | **deploy-time materialization, catalog-tracked, rotation-driven** |
| CI trust | ambient | token | token | **lease-bound resolve: cryptographic CI identity AND a live job lease** |
| Posture/health | no | basic | no | **scorecards over the catalog facet** |
| Audit → runs | partial | access log | audit log | **decision id (both layers) sealed into the ExecutionRun** |

## Read order

1. **`design.md`** — the architecture: the carve-out, the v3 reconciliation,
   identity, the chain, two-layer policy, the lease-bound resolve, delivery,
   decisions, invariants, alternatives.
2. **`policy-model.md`** — Layer 1 (shipped engine, activated) and Layer 2
   (the portable four-axis `SecretPolicy`): tiers, subjects, conditions,
   evaluation, provenance.
3. **`data-model.md`** — the grammar, the chain, the envelope + key hierarchy,
   the Postgres schema, the policy document, plan additions, wire routes.
4. **`runner-integration.md`** — lease-bound resolution, injection, the
   redactor, materialization execution, platform awareness, offline behavior,
   sealed-run provenance.
5. **`platform-integration.md`** — the catalog facet, scorecards,
   operations/audit, the console.
6. **`cli-surface.md`** — `orun secrets …`, `orun policy …`, onboarding.
7. **`implementation-plan.md`** — `SEC0 → SEC7`, re-anchored on shipped code,
   with the inherited baseline called out.
8. **`risks-and-open-questions.md`** — decisions, questions, risks, deferred.

## How this fits Orun Cloud

Secrets are **another projection of the same platform** — not a new service:

- **Catalog.** Compositions declare what a component *needs*
  (`secretBindings`); the backend knows what is *bound, rotated, and synced*;
  the resolver joins the two onto the Component entity as
  `extensions.x-orun-secrets`. Scorecards grade it.
- **Stacks.** The Stack carries access policy at two tiers — per-composition
  fragments and stack-wide `policies/` — versioned with the compositions they
  protect.
- **Operations.** A sealed run records *which versioned references it
  resolved* (never values); a provisioned entity records *which versions were
  materialized into it*. "What secrets did prod deploy #4821 use, and is the
  running Worker on the latest rotation?" is answerable from the graph.
- **Policy.** Layer 1 is the same engine that authorizes every other route on
  the platform, cascade and provenance included; `SecretPolicy` is the first
  condition-aware profile on top of it — built so non-secret platform policy
  can adopt the same shape later.
- **Identity.** One ActorContext end-to-end: humans by session, CI by
  verified OIDC exchange, machines by `sk_` keys. No second user directory,
  no invite flow, no parallel identity plane.
