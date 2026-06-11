# Spec: orun-secrets

**orun gains a first-class secret store — a Doppler-shaped experience that fits
the orun model exactly: secret *values* live encrypted in the Orun Cloud backend
(D1 ciphertext, envelope-encrypted, never in the content-addressed graph), while
the repo, the plan, and every object carry only typed `secret://` *references*.
Access is governed by a portable, GitHub-native `SecretPolicy` that travels with
the platform Stack — close to the compositions it protects — and is evaluated
per-fetch against four axes orun already owns: who (GitHub user/team/OIDC
subject), which component, which environment, and which execution platform.**

A medium SaaS company should be able to run nearly all of its platform on orun:
the same Stack that ships its golden-path compositions also ships the secret
policy that says *who may read what, where, and from which runner* — portable
across repos, versioned with the compositions, enforced by Orun Cloud.

> **The defining property: a secret value never becomes content.** orun's object
> graph is immutable, content-addressed, and dedup'd org-wide on push
> (`specs/orun-object-model/remote-and-consumers.md:18-26`). A plaintext (or even
> ciphertext) secret committed into that graph is unrecallable. So secrets are
> the one orun primitive that lives *outside* the graph — only references and
> resolved-version provenance are content. This is the load-bearing carve-out the
> whole design is built to protect.

## Status

| Field | Value |
|-------|-------|
| Status | **Draft (v1) — for review, not scheduled** |
| Builds on | `specs/orun-object-model/` (objects/refs/remote), the Orun Cloud backend (`cmd/orun/command_backend.go`), the Stack/composition model (`website/docs/concepts/stacks.md`) |
| Depends on | The content-addressed store + refs (L0/L2); the CLI auth stack (`internal/cliauth`, `internal/remotestate/auth.go`); the trigger model (`internal/model/trigger.go`, `internal/triggerctx`) |
| Prior art | `multi-tenant-saas/apps/config-worker` (shipped AES-256-GCM envelope secret store) and `apps/policy-worker` (shipped deny-by-default RBAC) — orun converges on these patterns rather than reinventing them |
| apiVersion | `orun.io/v1` (`SecretPolicy`, composition `secretBindings`, the GitHub identity map) |
| Decisions locked | (SD-1) values live in Orun Cloud, **never** in the object graph — only `secret://` references; (SD-2) **envelope encryption**, per-namespace DEK wrapped by a KEK in Cloudflare Secrets Store; (SD-3) **write-only** secret API + a single audited break-glass reveal — no value ever in plan/logs/events; (SD-4) **GitHub numeric user id** is the portable identity key, teams via the GH App map; (SD-5) **policy is a portable document** that ships in the Stack, close to compositions; (SD-6) policy is **deny-by-default** and enforced at **two points** — compile-time grant validation (visible in `orun plan`) and fetch-time decision (the security boundary); (SD-7) policy is evaluated over a **locked allowlisted predicate vocabulary** for v1 (CEL named as the upgrade path); (SD-8) resolved values are **redacted from all logs** before any blob write; (SD-9) inter-job value passing is **declared job `outputs`** carried on `dependsOn`, sensitive outputs stored as references |
| Milestone prefix | **SEC** (`SEC0 → SEC6`) |

## The one-paragraph thesis

Every CI/CD platform needs secrets, and most bolt them on as an opaque key-value
bag with a reveal API — which is exactly what leaks. orun is positioned to do
better because it already has the three hard parts built: an authenticated cloud
backend (`orun backend init` provisions a Worker + D1 + R2 + Durable Objects on
Cloudflare, `cmd/orun/command_backend.go`), a GitHub-native identity stack (OIDC
for CI runners, OAuth sessions for humans, `internal/remotestate/auth.go:157-208`),
and a portable, versioned policy surface (Stacks distributed over OCI,
`website/docs/concepts/stacks.md`). The missing pieces are small and additive: a
`secret://` reference type that the planner renders **instead of** values, an
envelope-encrypted store in the backend that mirrors the already-shipped
`config-worker` (`multi-tenant-saas/apps/config-worker/src/encryption.ts`), a
`SecretPolicy` document that binds GitHub identities to secret scopes under
component/environment/platform conditions and ships *with the Stack* so a paved
road carries its own access rules, a runner that resolves references to plaintext
at step launch and never persists them, and a redactor that masks resolved values
in the log stream. The result is a Doppler-grade developer experience —
`orun secret set`, declarative references, automatic injection — that is also
*more* portable (policy as code, GitHub identity, no vendor lock) and *safer by
construction* (values never enter the immutable graph) than the systems it draws
from.

## Read order

1. **`design.md`** — the architecture: the carve-out, identity, the two-layer
   policy, delivery, redaction, inter-job passing, decisions, invariants,
   alternatives.
2. **`policy-model.md`** — the portable, GitHub-native, four-axis `SecretPolicy`:
   subjects, conditions, evaluation, the locked predicate vocabulary, the CEL
   upgrade path, decision provenance.
3. **`data-model.md`** — schemas: `secret://` reference grammar, the ciphertext
   envelope, the D1 tables, the `SecretPolicy` document, composition
   `secretBindings`, the plan additions, and the GitHub identity map.
4. **`runner-integration.md`** — resolution, env injection, the redactor,
   execution-platform awareness, offline behavior, sealed-run provenance.
5. **`cli-surface.md`** — `orun secret …`, `orun policy …`, and the Orun Cloud /
   dashboard surface.
6. **`implementation-plan.md`** — milestones `SEC0 → SEC6`.
7. **`risks-and-open-questions.md`** — decisions, open questions, risks, deferred
   register.

## How this fits Orun Cloud

Orun Cloud is the hosted projection of the catalog, stacks, operations, and
policy. Secrets are **another projection of the same backend** — the `orun-api`
Worker, D1, and R2 that already exist — not a new service:

- **Catalog / stacks** already define *what runs* and *what a component needs*.
  Compositions extend that with `secretBindings`: *this component type needs
  these logical secrets for this profile* (portable, OCI-shipped).
- **Policy enforcement** already aspires to be first-class but is inert in the
  compiler today (`internal/expand/expander.go:350-375` carries `policies` that
  nothing reads). Secrets are the feature that *forces* the enforcement seam into
  existence — and `SecretPolicy` is designed so the same engine later enforces
  non-secret platform policy.
- **Operations** (runs, the cockpit, audit) gain secret provenance for free: a
  sealed run records *which versioned references it resolved* (never values), so
  "what secrets did prod deploy #4821 use?" is answerable from the object graph.
- **Identity** is GitHub end-to-end, so a customer's existing GitHub org *is*
  their orun identity model — the GH App integration maps users/teams once, and
  every policy everywhere references stable GitHub numeric ids.
