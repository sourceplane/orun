# Risks, decisions, and open questions

## Decisions (locked — see `design.md` §9 for full rationale)

| # | Decision |
|---|----------|
| SD-1 | Values live in Orun Cloud only; the object graph carries `secret://` references |
| SD-2 | Envelope encryption; per-namespace DEK; KEK in Cloudflare Secrets Store |
| SD-3 | Write-only API + injection-only delivery; one audited break-glass reveal |
| SD-4 | GitHub numeric user id is the portable identity key; teams via the GH App map |
| SD-5 | Policy is a portable `SecretPolicy` document that ships in the Stack |
| SD-6 | Deny-by-default; enforced at compile (visible) and fetch (authoritative) |
| SD-7 | Locked predicate vocabulary in v1; CEL is the named upgrade path |
| SD-8 | Resolved values redacted from logs before any blob write |
| SD-9 | Inter-job passing = declared outputs on `dependsOn`; sensitive → references |

## Open questions (need product/business intent — not inferable from code)

| # | Question | Lean | Resolve by |
|---|----------|------|------------|
| **Q-1** | **Local/offline dev.** Does `orun run` with secrets require backend connectivity, or is there a sanctioned local override (`ORUN_SECRET_<KEY>`) gated to `platform==local-cli` + policy? This trades offline-first ergonomics against a uniform security boundary. | Allow a policy-gated local override for non-prod envs only; prod always requires the cloud. | SEC3 |
| **Q-2** | **Self-hosted-backend KEK custody.** `orun backend init` lets anyone provision their own Cloudflare backend. For self-hosted, who holds the KEK and what are the custody/HSM guarantees? Is the secret store **hosted-only (Orun Cloud)** or fully self-hostable with customer-held keys? | v1: hosted-only for the managed KEK; self-hosted backends get the store with a customer-provided KEK (bring-your-own, documented sharp edges). | Before SEC1 GA |
| **Q-3** | **Convergence with the SaaS `config-worker`.** Should orun's secret store and `multi-tenant-saas/apps/config-worker` become **one service**, or stay separate (different tenancy roots: namespace vs org/project)? They share crypto + authz patterns. | Converge the *crypto + audit contract*; keep orun's namespace tenancy. One library, two deployments. | Before SEC1 |
| **Q-4** | **Reveal policy.** Is a human `reveal` ever acceptable (break-glass), or is delivery strictly machine-to-workload? `config-worker` ships **no** reveal by design (`specs/components/07-config-secrets-flags.md:74-76`). | Single audited, alerted, elevated-action break-glass reveal (SD-3). | SEC6 |
| **Q-5** | **Team-grant blast radius.** A `gh:team` grant resolves to live membership; adding someone to a GitHub team silently grants secret access. Acceptable, or require an explicit orun confirmation step on team changes? | Acceptable + audited (the GitHub org *is* the access model, SD-4); surface team→grant diffs in the console on membership webhooks. | SEC2 |
| **Q-6** | **Namespace granularity.** Is the tenancy/secret namespace the **repo** or the **GitHub org**? Repo-level isolates blast radius; org-level eases sharing a secret across repos. | Repo namespace by default, with org-scoped secrets as an explicit `namespace: org/*` scope a policy must grant. | Before SEC1 |

## Risks

| # | Risk | Mitigation |
|---|------|------------|
| R-1 | **A value leaks into content** (the cardinal failure). | Structural: no value field in `PlanJob`; planner leak-guard (SEC0); `fsck` scanner asserts Invariant 1 in CI; redactor seeded before any step runs. |
| R-2 | **Redactor misses an encoding** (value echoed base64/url-encoded). | Register raw + base64 + url + json-escaped forms; fuzz the redactor; forbid trivially short secrets at the policy layer. |
| R-3 | **OIDC/platform spoofing** — a laptop claims `github-actions-oidc`. | Platform fact is derived server-side from the cryptographically-bound OIDC JWT, never self-reported (`runner-integration.md` §3). |
| R-4 | **Policy widening on Stack adoption** — a downstream repo loosens a grant. | Narrow-only overlay rule; `orun policy lint` rejects an intent `allow` broader than any Stack `allow` (`policy-model.md` §5). |
| R-5 | **Identity-map staleness** — revoked GitHub user still resolves. | `gh_identity_map` is a derived cache refreshed on webhooks; resolve-time membership check; short TTL + on-miss refresh. |
| R-6 | **Backend outage blocks all runs** (secrets are a hot-path dependency). | Per-job batched resolve + in-memory cache; DO-backed rate limiting already exists (`command_backend.go`); clear degraded-mode errors; Q-1 local override for non-prod. |
| R-7 | **Cryptoshred incompleteness** — a DEK destroy misses a cached copy. | DEK unwrapped only in Worker memory per-resolve, never persisted; `keyId` generations let a namespace rotate+shred atomically (`data-model.md` §3). |
| R-8 | **Compile-time/fetch-time drift** — `orun plan` says grantable, fetch denies. | Expected and safe (plan is existential, fetch is concrete); surface the distinction in plan output so it is not a surprise. |

## Compatibility

- **Additive.** `secretEnv`/`secretRefs`/`secretBindings`/`outputs` are new optional
  fields; existing plans without them are unaffected. Plaintext `env` is unchanged.
- **No migration of existing data.** Secrets are net-new; there is no legacy secret
  store to migrate.
- **Backend.** Additive D1 migrations + Worker routes via the existing
  `orun backend init` bundle; `orun backend status` reports new health.
- **The one new constraint:** once `secretEnv` is used, a literal value in that slot
  is a hard error (SEC0) — intentional, to make leak vector #1 unrepresentable.
