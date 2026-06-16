# Risks, decisions, and open questions

## Decisions (locked — see `design.md` §10 for full rationale)

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
| SD-10 | Three policy tiers (composition-attached → Stack → intent), narrow-only downward |
| SD-11 | Env chain `personal → env → base`; personal is owner + local-cli only |
| SD-12 | Namespace = repo by default; org sharing via explicit `_shared/<group>` (closes v1's Q-6) |
| SD-13 | Runtime delivery = materialization into the platform's native store; plan-visible, provenance-tracked, rotation-driven |
| SD-14 | Secrets project onto the Component entity as `extensions.x-orun-secrets`; scorecards grade it |
| SD-15 | The orun-cloud wire contract (§4 secrets, §6 policy map) is normative; orun-secrets elaborates it and `SecretPolicy` is the engine behind `secret.value.use` (supersedes the orun-cloud placeholder design) |
| SD-16 | CLI is the shipping `orun secrets` group; OC5 ships `set`/`list`/`rm`, orun-secrets extends it; `rm` aliases `revoke` |

## Open questions (need product/business intent — not inferable from code)

| # | Question | Lean | Resolve by |
|---|----------|------|------------|
| **Q-1** | **Fully-offline dev.** Personal overlays (SD-11) cover the daily local case but require connectivity. Is the `ORUN_SECRET_<KEY>` env override (local-cli + policy-gated, never prod) the right airplane-mode fallback, or should resolved non-prod values be cached encrypted on disk with a TTL? | Env override only; no on-disk cache in v1 (a cache is a new leak surface with marginal benefit). | SEC3 |
| **Q-2** | **Self-hosted-backend KEK custody.** `orun backend init` lets anyone provision their own Cloudflare backend. For self-hosted, who holds the KEK and what are the custody/HSM guarantees? Is the secret store **hosted-only (Orun Cloud)** or fully self-hostable with customer-held keys? | v1: hosted-only for the managed KEK; self-hosted backends get the store with a customer-provided KEK (bring-your-own, documented sharp edges). | Before SEC1 GA |
| **Q-3** | ~~Convergence with the SaaS `config-worker` / orun-cloud secret store.~~ — **decided (SD-15/SD-16)**: orun-secrets **supersedes** the orun-cloud placeholder design and reconciles to it — the wire contract (§4/§6) is normative, the `orun secrets` CLI group and run-scoped lease resolve are adopted verbatim, `SecretPolicy` is the engine behind `secret.value.use`. One contract, one CLI, one design. | — | closed |
| **Q-4** | **Reveal policy.** Is a human `reveal` ever acceptable (break-glass), or is delivery strictly machine-to-workload? `config-worker` ships **no** reveal by design. | Single audited, alerted, elevated-action break-glass reveal (SD-3). | SEC7 |
| **Q-5** | **Team-grant blast radius.** A `gh:team` grant resolves to live membership; adding someone to a GitHub team silently grants secret access. Acceptable, or require an explicit orun confirmation step on team changes? | Acceptable + audited (the GitHub org *is* the access model, SD-4); surface team→grant diffs in the console on membership webhooks. | SEC2 |
| **Q-6** | ~~Namespace granularity~~ — **decided as SD-12**: repo namespace by default; explicit `_shared/<group>` org namespaces requiring a bind + a named grant. | — | closed |
| **Q-7** | **Materialization adapter set.** v1 ships `cloudflare-worker` (orun provisions CF already). What's next — AWS SSM, Secrets Manager, GitHub repo secrets, Kubernetes? Each adapter is a custody surface and a support commitment. | Demand-driven registry; require an adapter to derive its target from a provisioned entity (no free-form endpoints) as the admission bar. | Before SEC6 GA |
| **Q-8** | **Personal overlays in which envs?** Should `--personal` be allowed against any env config or only non-prod? A personal shadow of a prod key on a laptop is policy-denied at resolve anyway (prod refuses `local-cli`), but allowing the *write* may confuse. | Allow writes only for envs whose policy admits `local-cli` resolution — i.e., reject the write where it could never be read. | SEC1 |
| **Q-9** | **Rotation convergence guarantees.** `onRotate` redeploys are eventually-consistent (a paused/broken pipeline leaves a `superseded` sync). Is "visible drift + scorecard failure" enough, or do some keys need a hard deadline (auto-disable the old version after N hours)? | Visibility-first in v1; version auto-revoke windows as a per-secret `rotation_policy` option later. | SEC6 |

## Risks

| # | Risk | Mitigation |
|---|------|------------|
| R-1 | **A value leaks into content** (the cardinal failure). | Structural: no value field in `PlanJob`; planner leak-guard (SEC0); `fsck` scanner asserts Invariant 1 in CI; redactor seeded before any step runs. |
| R-2 | **Redactor misses an encoding** (value echoed base64/url-encoded). | Register raw + base64 + url + json-escaped forms; fuzz the redactor; forbid trivially short secrets at the policy layer. |
| R-3 | **OIDC/platform spoofing** — a laptop claims `github-actions-oidc`. | Platform fact is derived server-side from the cryptographically-bound OIDC JWT, never self-reported (`runner-integration.md` §3). |
| R-4 | **Policy widening on Stack adoption** — a downstream repo loosens a grant. | Narrow-only overlay rule (SD-10); composition fragments force-scoped to their own type; `orun policy lint` rejects an intent `allow` broader than any Stack `allow`. |
| R-5 | **Identity-map staleness** — revoked GitHub user still resolves. | `gh_identity_map` is a derived cache refreshed on webhooks; resolve-time membership check; short TTL + on-miss refresh. |
| R-6 | **Backend outage blocks all runs** (secrets are a hot-path dependency). | Per-job batched resolve + in-memory cache; DO-backed rate limiting already exists; clear degraded-mode errors; Q-1 local fallback for non-prod. Materialization (SD-13) means *deployed apps* keep running through an orun outage — only pipelines pause. |
| R-7 | **Cryptoshred incompleteness** — a DEK destroy misses a cached copy. | DEK unwrapped only in Worker memory per-resolve, never persisted; `keyId` generations let a namespace rotate+shred atomically. |
| R-8 | **Compile-time/fetch-time drift** — `orun plan` says grantable, fetch denies. | Expected and safe (plan is existential, fetch is concrete); surface the distinction in plan output so it is not a surprise. |
| R-9 | **Materialized values live outside orun custody** — the target store (Worker bindings, SSM) is governed by the cloud's IAM, not orun policy, and orun cannot verify its contents. | Inherent to running applications; the goal is *one governed door*: typed adapters only (target derived from the provisioned entity, no free-form endpoints), full sync provenance (Invariant 10), rotation re-materializes, drift surfaces in operations + scorecards. Document that target-store read access is the customer's IAM responsibility. |
| R-10 | **Personal-overlay shadowing confusion** — "works on my machine" because a personal value silently differs from the shared one. | Resolve response flags personal serves; runner prints the overridden keys; `orun plan` marks shadowable refs; `orun secrets list --chain` shows the full chain; structural denial outside `local-cli` (Invariant 9). |
| R-11 | **Shared-group blast radius** — one `_shared` group key leaking affects every bound component. | Groups are explicit binds + named grants (no wildcard across `_shared`, SD-12); per-group DEK generation enables targeted cryptoshred; the catalog facet lists every component bound to a group, so impact analysis is one query. |

## Deferred (tracked follow-ups, not done in v2)

| # | Follow-up | Why deferred |
|---|-----------|--------------|
| **D-1** | **Rename `namespace` → the org/project spine** across this spec, to fully match the orun-cloud locked decision that "the namespace wording retires" (`orun-cloud/architecture.md` §4). v2 keeps `namespace` with the mapping note in `data-model.md` §1. | The rename touches every doc + the grammar + D1 columns; doing it in the reconciliation pass would balloon the diff and risk inconsistency mid-flight. Tracked as the single known, deliberate divergence from the locked decision. |
| **D-2** | Additional materialization adapters beyond `cloudflare-worker` (Q-7). | Demand-driven; each is a custody surface. |
| **D-3** | Inbound external-provider sync (pull from AWS/Cloudflare stores) via the envelope `provider` seam. | Post-v1; the seam is reserved. |
| **D-4** | Dynamic / leased secrets (Vault-style). | Post-v1; the reference model admits them. |
| **D-5** | CEL `expr` predicates behind a capability flag (SD-7), shared with scorecards. | Locked vocabulary covers v1. |

## Compatibility

- **Additive.** `secretEnv`/`secretGroups`/`secretRefs`/`secretBindings`/
  `materialize`/`outputs` are new optional fields; existing plans without them
  are unaffected. Plaintext `env` is unchanged.
- **No migration of existing data.** Secrets are net-new; there is no legacy
  secret store to migrate. `orun secrets import` eases dotenv onboarding.
- **Backend.** Additive D1 migrations + Worker routes via the existing
  `orun backend init` bundle; `orun backend status` reports new health.
- **Catalog.** `x-orun-secrets` rides the existing entity extension seam
  (`specs/orun-service-catalog/data-model.md` §8) — unknown to older readers,
  preserved on round-trip by design.
- **The one new constraint:** once `secretEnv` is used, a literal value in that
  slot is a hard error (SEC0) — intentional, to make leak vector #1
  unrepresentable.
