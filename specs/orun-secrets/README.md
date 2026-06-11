# Spec: orun-secrets

**orun gains a first-class secret store — a Doppler-shaped experience built to
orun's invariants and woven through every Orun Cloud pillar. Secret *values*
live encrypted in the Orun Cloud backend (D1, envelope-encrypted, never in the
content-addressed graph); the repo, the plan, and every object carry only typed
`secret://` *references*. Access is governed by a portable, GitHub-native
`SecretPolicy` that travels with the platform Stack — attachable down to the
individual composition — and is evaluated per-fetch against four axes orun
already owns: who (GitHub user/team/OIDC subject), which component, which
environment, and which execution platform. Secrets reach pipeline jobs by
runner injection and reach *deployed applications* by policy-governed
materialization into the target platform's native secret store. The whole
surface is projected into the service catalog as an entity facet, so binding
health and rotation age are scorecard-checkable like any other catalog fact.**

A medium SaaS company should be able to run nearly all of its platform on orun:
the same Stack that ships its golden-path compositions also ships the secret
policy that says *who may read what, where, and from which runner*; the same
catalog that tracks its components tracks their secret health; the same deploy
jobs that provision its infrastructure deliver its secrets to it.

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
| Status | **Draft (v2) — for review, not scheduled** |
| Builds on | `specs/orun-object-model/` (objects/refs/remote), the Orun Cloud backend (`cmd/orun/command_backend.go`), the Stack/composition model (`website/docs/concepts/stacks.md`), the service catalog (`specs/orun-service-catalog/`, entity envelope + `extensions`), scorecards (`specs/orun-scorecards/`) |
| Depends on | The content-addressed store + refs (L0/L2); the CLI auth stack (`internal/cliauth`, `internal/remotestate/auth.go`); the trigger model (`internal/model/trigger.go`, `internal/triggerctx`); composition effects → provisioned entities (`internal/model/composition.go:75-90`) |
| Prior art | `multi-tenant-saas/apps/config-worker` (shipped AES-256-GCM envelope secret store) and `apps/policy-worker` (shipped deny-by-default RBAC) — orun converges on these patterns rather than reinventing them; Doppler (config inheritance, personal configs, syncs) for the experience bar |
| apiVersion | `orun.io/v1` (`SecretPolicy`, composition `secretBindings` + `materialize`, the GitHub identity map) |
| Milestone prefix | **SEC** (`SEC0 → SEC7`) |

**Decisions locked** (full rationale in `design.md` §10):

| # | Decision |
|---|----------|
| SD-1 | Values live in Orun Cloud, **never** in the object graph — only `secret://` references |
| SD-2 | **Envelope encryption**, per-namespace DEK wrapped by a KEK in Cloudflare Secrets Store |
| SD-3 | **Write-only** secret API + a single audited break-glass reveal — no value ever in plan/logs/events |
| SD-4 | **GitHub numeric user id** is the portable identity key; teams/users map in via the GitHub App (`gh_identity_map`) |
| SD-5 | **Policy is a portable document** that ships in the Stack, close to compositions |
| SD-6 | Policy is **deny-by-default**, enforced at **compile time** (visible in `orun plan`) and **fetch time** (authoritative) |
| SD-7 | Policy evaluates a **locked allowlisted predicate vocabulary** in v1; CEL is the named upgrade path |
| SD-8 | Resolved values are **redacted from all logs** before any blob write |
| SD-9 | Inter-job value passing is **declared job `outputs`** on `dependsOn`; sensitive outputs route through the secret backend |
| SD-10 | **Three-tier policy placement**: composition-attached defaults → Stack `policies/` → intent overlays; each lower tier may only narrow |
| SD-11 | **Environment inheritance + personal configs**: per-key resolution walks `personal(env, user) → env → base`; personal overlays are local-cli-only and owner-only |
| SD-12 | **Namespace = repo by default**; org-shared secrets live in explicit `_shared/<group>` namespaces a component must bind and a policy must grant |
| SD-13 | **Runtime delivery is materialization**: a deploy profile may sync resolved secrets into the target platform's native store under the same policy + audit; rotation re-materializes; no runtime SDK, no runtime dependency on orun-api |
| SD-14 | **Secrets are a catalog facet**: requirements, binding status, rotation age, and sync state project onto the Component entity (`extensions.x-orun-secrets`) and are scorecard-checkable |

## What changed in v2

v1 nailed the security core but stopped at the pipeline boundary and never met
the catalog. v2 keeps SD-1..SD-9 verbatim and adds the product half:

- **Runtime materialization (SD-13).** v1 delivered secrets only to *jobs*. A
  SaaS's deployed Worker or container needs secrets at *application* runtime —
  the gap Doppler fills with "integrations/syncs". orun fills it natively: the
  deploy job materializes secrets into the platform's own store and the catalog
  remembers exactly what was synced where (`platform-integration.md` §3).
- **Catalog + scorecards (SD-14).** Secret requirements and health become a
  facet of the Component entity, so "all required bindings satisfied" and "prod
  secrets rotated ≤ 90 days" are scorecard rules, not a separate dashboard
  (`platform-integration.md` §1–2).
- **Environment inheritance + personal configs (SD-11).** `base → env →
  personal` per-key resolution — the Doppler ergonomic that makes daily dev
  pleasant — mapped onto orun's existing most-specific-wins precedence
  (`design.md` §6).
- **Composition-attached policy (SD-10).** "Policy close to the compositions"
  made literal: a composition directory may carry its own policy fragment;
  Stack and intent tiers can only narrow it (`policy-model.md` §1).
- **Namespace granularity decided (SD-12).** Repo-scoped by default, with
  explicit org-`_shared` groups for the one-Datadog-key-thirty-services case —
  closes v1's Q-6.
- **Onboarding.** `orun secret import --from-dotenv` and a first-ten-minutes
  flow, because adoption is a product feature (`cli-surface.md` §1).

## The one-paragraph thesis

Every CI/CD platform needs secrets, and most bolt them on as an opaque
key-value bag with a reveal API — which is exactly what leaks. orun is
positioned to do better because it already has the hard parts built: an
authenticated cloud backend (`orun backend init` provisions a Worker + D1 + R2 +
Durable Objects, `cmd/orun/command_backend.go`), a GitHub-native identity stack
(OIDC for CI runners, OAuth sessions for humans,
`internal/remotestate/auth.go:156-208`), a portable, versioned policy surface
(Stacks distributed over OCI, `website/docs/concepts/stacks.md`), a service
catalog with an extension seam on every entity
(`internal/catalogmodel/entity_envelope.go:36`), and a scorecard engine that
turns entity facts into graded checks (`specs/orun-scorecards/`). The missing
pieces are additive: a `secret://` reference type the planner renders **instead
of** values, an envelope-encrypted store mirroring the shipped `config-worker`,
a `SecretPolicy` that binds GitHub identities to secret scopes and rides the
Stack, a runner that injects plaintext at step launch and redacts it from logs,
a materialization step that carries secrets the last mile into the deployed
platform, and a catalog facet that makes all of it visible and scoreable. The
result is a Doppler-grade developer experience that is *more* portable (policy
as code, GitHub identity, no vendor lock) and *safer by construction* (values
never enter the immutable graph) than the systems it draws from.

## Positioning (why not just use X)

| | GitHub Actions secrets | Doppler | Vault | **orun secrets** |
|---|---|---|---|---|
| Identity | repo collaborators | proprietary users/SCIM | own auth backends | **your GitHub org, numeric ids, portable** |
| Policy | env protection rules | token-per-config | path ACL policies | **four-axis document, ships with the Stack** |
| Component-aware | no | per-project | no | **yes — grants conditioned on component type/domain** |
| Plan visibility | no | no | no | **`orun plan` shows every secret a run will read** |
| Runtime delivery | n/a | syncs + SDK | agent/SDK | **deploy-time materialization, catalog-tracked** |
| Posture/health | no | basic | no | **scorecards over the catalog facet** |
| Audit → runs | partial | access log | audit log | **decision id sealed into the ExecutionRun** |

## Read order

1. **`design.md`** — the architecture: the carve-out, identity, environments,
   the three-tier policy, delivery (injection + materialization), redaction,
   inter-job passing, decisions, invariants, alternatives.
2. **`policy-model.md`** — the portable, GitHub-native, four-axis
   `SecretPolicy`: placement tiers, subjects, conditions, evaluation, the locked
   predicate vocabulary, decision provenance.
3. **`data-model.md`** — schemas: `secret://` grammar, environments and
   personal overlays, the ciphertext envelope, D1 tables, the `SecretPolicy`
   document, composition `secretBindings` + `materialize`, plan additions, the
   GitHub identity map.
4. **`runner-integration.md`** — resolution, env injection, the redactor,
   materialization execution, execution-platform awareness, offline behavior,
   sealed-run provenance.
5. **`platform-integration.md`** — how secrets surface across Orun Cloud: the
   catalog facet, scorecard checks, operations/audit, the dashboard.
6. **`cli-surface.md`** — `orun secret …`, `orun policy …`, import/onboarding.
7. **`implementation-plan.md`** — milestones `SEC0 → SEC7`.
8. **`risks-and-open-questions.md`** — decisions, open questions, risks,
   deferred register.

## How this fits Orun Cloud

Orun Cloud is the hosted projection of the catalog, stacks, operations, and
policy. Secrets are **another projection of the same backend** — the `orun-api`
Worker, D1, and R2 that already exist — not a new service:

- **Catalog.** Compositions declare what a component *needs*
  (`secretBindings`); the backend knows what is *bound, rotated, and synced*;
  the resolver joins the two onto the Component entity as
  `extensions.x-orun-secrets` (`specs/orun-service-catalog/data-model.md` §8).
  Secret health is entity data, so **scorecards** grade it like anything else.
- **Stacks.** The Stack is already the portable, OCI-distributed unit of
  platform truth. It now carries access policy at two tiers — per-composition
  fragments and stack-wide `policies/` — versioned with the compositions they
  protect.
- **Operations.** A sealed run records *which versioned references it resolved*
  (never values); a provisioned entity records *which versions were
  materialized into it*. "What secrets did prod deploy #4821 use, and is the
  running Worker on the latest rotation?" is answerable from the graph.
- **Policy enforcement.** orun carries `policies` today that nothing enforces
  (`internal/expand/expander.go:350-375`). `SecretPolicy` is the first enforced
  profile of a general policy engine — same deny-by-default evaluation, same
  audit contract — built so non-secret platform policy can adopt it later.
- **Identity.** GitHub end-to-end: install the GitHub App once, and the org's
  users and teams (stable numeric ids) *are* the access model — no second user
  directory, no invite flow.
