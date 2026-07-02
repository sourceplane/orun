# Risks, decisions, and open questions (v3)

## Decisions (locked — full rationale in `design.md` §10)

| # | Decision |
|---|----------|
| SD-1 | Values live in Orun Cloud only; the object graph carries `secret://` references |
| SD-2′ | Envelope AES-256-GCM (shipped shape) + `keyId`; per-**workspace** DEK wrapped by a KEK in Cloudflare Secrets Store; append-only `secret_versions`; decrypt exists only in the resolve handler; `k0` lazy migration |
| SD-3 | Write-only API + injection-only delivery; one audited break-glass reveal |
| SD-4′ | Subjects are platform principals (users/teams/service principals/workflow actors); GitHub team sync is a later integrations-worker feature; `gh_identity_map` deleted |
| SD-5 | Policy is a portable `SecretPolicy` document that ships in the Stack |
| SD-6 | Deny-by-default; enforced at compile (visible) and fetch (authoritative) |
| SD-7 | Locked predicate vocabulary in v1; CEL is the named upgrade path |
| SD-8 | Resolved values redacted once at capture, upstream of all log sinks |
| SD-9 | Inter-job passing = declared outputs; sensitive → secret-backend references |
| SD-10 | Three policy tiers (composition-attached → Stack → intent), narrow-only downward |
| SD-11′ | Resolution = the platform scope chain `personal → environment → project → workspace → account`; project scope replaces the reserved `base` env |
| SD-12′ | Sharing = workspace/account scope rows (replaces `_shared/<group>`); visible in plan/chain/facet; lockable (`overridable:false`) |
| SD-13 | Runtime delivery = materialization into the platform's native store; plan-visible, provenance-tracked, rotation-driven |
| SD-14 | Secrets project onto the Component entity as `extensions.x-orun-secrets`; scorecards grade it |
| SD-15′ | The state-api contract stays normative; v3 revises §4 (leaseEpoch, chain provenance, versions/personal/policy routes) — one API, one CLI |
| SD-16 | `orun secrets` group with scope flags (`--project/--workspace/--account`) replacing `--namespace` |
| SD-17 | Two-layer policy: shipped role×scope engine (dormant `secret.*` actions activated) + `SecretPolicy` conditions at resolve; one decision id spans both |
| SD-18 | Resolve is lease-bound with `leaseEpoch`, verified in state-worker for both coordination backends; config-worker decrypts behind a service binding |
| SD-19 | Doppler service tokens ≡ `sk_` service principals granted `secret.value.use` under a pinning `SecretPolicy` — no new token type |

## Open questions (need product/business intent — not inferable from code)

| # | Question | Lean | Resolve by |
|---|----------|------|------------|
| **Q-1** | **Fully-offline dev.** Personal overlays cover the daily local case but require connectivity. Is the `ORUN_SECRET_<KEY>` env override (local-cli + policy-gated, never protected envs) the right airplane-mode fallback, or should resolved non-prod values be cached encrypted on disk with a TTL? | Env override only; no on-disk cache in v1 (a cache is a new leak surface). | SEC3 |
| **Q-2** | **Self-hosted KEK custody.** For the OSS self-host backend, who holds the KEK and what are the custody guarantees? | Hosted-only managed KEK in v1; self-host ships later with a customer-provided KEK (bring-your-own, documented sharp edges) as part of D-2. | Before self-host GA |
| **Q-4** | **Reveal policy.** Is a human reveal ever acceptable? | Single audited, alerted, elevated-action (`secret.reveal`) break-glass; owner/admin only (SD-3). | SEC7 |
| **Q-5** | **Team-grant blast radius.** A `team:` grant resolves to live membership; adding someone to a team silently grants secret access — and the **account cascade** extends this: an account-admin grant reaches every child workspace. Acceptable? | Acceptable + audited (provenance reports `via: team` / `via: account_cascade` on every decision, TM6b1); surface team→grant diffs in the console. | SEC2 |
| **Q-7** | **Materialization adapter set.** v1 ships `cloudflare-worker`. What's next — AWS SSM, Secrets Manager, GitHub repo secrets, Kubernetes? Each adapter is a custody surface and a support commitment. | Demand-driven registry; admission bar: an adapter must derive its target from a provisioned entity (no free-form endpoints). | Before SEC6 GA |
| **Q-8** | **Personal overlays in which envs?** Allow `--personal` writes against any env, or only envs whose policy admits `local-cli` resolution? | Reject the write where it could never be read (policy denies `local-cli` for that env). | SEC1 |
| **Q-9** | **Rotation convergence guarantees.** `onRotate` redeploys are eventually-consistent (a broken pipeline leaves a `superseded` sync). Is "visible drift + scorecard failure" enough, or do some keys need a hard deadline (auto-revoke the old version after N hours)? | Visibility-first in v1; per-secret auto-revoke windows via `rotation_policy` later. | SEC6 |
| **Q-10** *(new)* | **Resolve for OP2-relational runs.** The DO coordination path is the default; the relational path still exists. Must `verifyLiveLease` cover both from day one, or may resolve gate to DO-backed runs (404 otherwise, like the native verbs)? | Cover both — the check is one indexed row read on the relational side; gating resolve to a coordination backend couples two features needlessly. | SEC3 |
| **Q-11** *(new)* | **Layer-2 evaluation home.** Config-worker evaluates conditions in v1. If non-secret platform policy later adopts condition documents, does evaluation move to policy-worker? | Keep the evaluator a pure shared library (`packages/`), host it in config-worker for v1; relocation is then a wiring change, not a rewrite. | SEC2 |

Closed since v2: Q-3 (orun-cloud convergence — SD-15′), Q-6 (namespace
granularity — subsumed by the chain, SD-12′).

## Risks

| # | Risk | Mitigation |
|---|------|------------|
| R-1 | **A value leaks into content** (the cardinal failure). | Structural: no value field in `PlanJob`; planner leak-guard (SEC0); `fsck` scanner asserts Invariant 1 in CI; redactor seeded before any step runs. |
| R-2 | **Redactor misses an encoding** (value echoed base64/url/json-escaped). | Register all four forms; fuzz the redactor; forbid trivially short secrets at the policy layer. |
| R-3 | **Platform spoofing** — a laptop claims to be CI. | The platform fact derives server-side from the verified actor kind; the workflow token exists only via the JWKS-verified OIDC exchange (`oidc/github.ts:74-94`) — never self-reported. |
| R-4 | **Policy widening on Stack adoption** — a downstream repo loosens a grant. | Narrow-only overlay rule (SD-10); composition fragments force-scoped to their type; `orun policy lint` rejects broader intent `allow`s. |
| R-5 | **Membership staleness** — a removed member still resolves. | Membership facts are fetched at decision time from membership-worker (not cached in the engine); Layer 1 and Layer 2 read the same facts so they cannot disagree. |
| R-6 | **Backend outage blocks all runs** (secrets are a hot-path dependency). | Per-job batched resolve + in-memory TTL cache; clear degraded-mode errors; Q-1 local fallback for non-protected envs. Materialization (SD-13) means *deployed apps* keep running through an outage — only pipelines pause. |
| R-7 | **Cryptoshred incompleteness** — a DEK destroy misses a cached copy. | DEK unwrapped only in Worker memory per-resolve, never persisted; `keyId` generations let a workspace rotate+shred atomically. |
| R-8 | **Compile-time/fetch-time drift** — plan says grantable, fetch denies. | Expected and safe (plan is existential, fetch is concrete); the distinction is surfaced in plan output. |
| R-9 | **Materialized values live outside orun custody.** | Inherent to running applications; one governed door: typed adapters only, full sync provenance (Invariant 10), rotation re-materializes, drift surfaces in operations + scorecards. Target-store read access is the customer's IAM responsibility. |
| R-10 | **Personal-overlay shadowing confusion** — "works on my machine". | Resolve response flags personal serves; runner prints overridden keys; plan marks shadowable refs; `list --chain` shows the chain; structural denial outside `local-cli` (Invariant 9). |
| R-11 | **Ambient inheritance surprise** — a key silently served from account scope (v3 analogue of v2's shared-group risk). | Plan annotations (`servesFrom`), the `--chain` view, and the facet make every cross-scope serve visible; `overridable:false` locks prevent accidental shadowing; a `SecretPolicy` can deny above-project serves per env. |
| R-12 *(new)* | **Lease race** — a swept-and-requeued job's old runner resolves with a stale claim. | `leaseEpoch` in the resolve body (SD-18), matching the shipped heartbeat/complete discipline; `verifyLiveLease` compares epoch + expiry; `409 lease_lost`. |
| R-13 *(new)* | **`k0` migration limbo** — v1 static-key envelopes linger indefinitely, keeping the static key hot. | Rotation and writes always produce `v:2`; a backfill re-encrypt job + a scorecard-visible metric ("% envelopes on workspace DEKs") drive it to zero; the static key retires on a tracked date. |
| R-14 *(new)* | **Cross-worker seam abuse** — something other than state-worker calls config-worker's internal resolve. | The internal route is reachable only over the service binding (no public path through api-edge); config-worker still re-checks Layer 1 + Layer 2 itself — the lease check is *additive*, not a substitute. |

## Deferred (tracked follow-ups, not done in v3)

| # | Follow-up | Why deferred |
|---|-----------|--------------|
| **D-2** | **Self-hosted backend parity**: D1 translation of the §7 schema, `orun backend init` bundle extension, customer-held KEK (Q-2). | The hosted platform is the canonical implementation; the contract keeps the seam honest. |
| **D-3** | Additional materialization adapters (Q-7). | Demand-driven; each is a custody surface. |
| **D-4** | GitHub team sync into platform teams (integrations-worker). | Enriches `team:` subjects; not on the critical path (SD-4′). |
| **D-5** | Inbound external-provider sync via the envelope `provider` seam. | Post-v1; the seam is reserved. |
| **D-6** | Dynamic / leased secrets (Vault-style). | Post-v1; the reference model admits them. |
| **D-7** | CEL `expr` predicates behind a capability flag (SD-7). | Locked vocabulary covers v1. |

Closed since v2: **D-1** (namespace → workspace/project rename) — **done in
v3**; the grammar, schema, CLI, and policy scope are all on the platform spine.

## Compatibility

- **Additive on the orun side.** `secretEnv`/`secretRefs`/`secretBindings`/
  `materialize`/`outputs` are new optional fields; existing plans are
  unaffected. Plaintext `env` is unchanged.
- **Additive on the platform side.** New tables + columns extend the shipped
  `config` schema (070/430 pattern); the shipped secret CRUD routes keep their
  shapes; the RBAC action switch (`*.config.*` → `secret.*`) is
  permission-equivalent for shipped roles (owners/admins retain everything;
  builder gains `secret.read` where `project.config.read` granted it — audited
  in the SEC1 PR).
- **Contract:** §4 v3 is a revision of an **unimplemented** section (resolve
  never shipped) plus additive routes — no client breakage is possible; the
  vendored copy + CHECKSUM in orun update in lockstep.
- **No migration of existing secret data** beyond the lazy `k0` envelope
  upgrade (R-13); rotation history begins at SEC1 (pre-existing versions have
  metadata only, no historical ciphertext — it was overwritten by design).
- **The one new constraint:** once `secretEnv` is used, a literal value in
  that slot is a hard error (SEC0) — intentional, to make leak vector #1
  unrepresentable.
