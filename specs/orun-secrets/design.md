# Design (v3)

> orun gains a secret store shaped like Doppler but built for orun's invariants
> — re-architected in v3 onto the platform that actually shipped. Values live
> encrypted in Orun Cloud's `config-worker` (Supabase Postgres) and **never**
> enter the content-addressed graph; the plan carries only `secret://`
> references; access is two policy layers — the shipped role×scope RBAC engine
> plus a portable, Stack-shipped `SecretPolicy` conditions document; resolution
> walks the platform's own scope chain (`personal → environment → project →
> workspace → account`); the runner resolves under a **live job lease**, injects
> plaintext at step launch, and redacts it from logs; deployed applications
> receive secrets by policy-governed materialization. This doc fixes the model,
> the decisions, the enforcement seams, the invariants, and the alternatives.
> Schemas live in `data-model.md`; the policy engine in `policy-model.md`;
> delivery in `runner-integration.md`; the catalog/console surface in
> `platform-integration.md`.

## 0. What changed in v3 (the reconciliation to reality)

v2 was written against a platform that has since shipped in a different shape.
v3 keeps the security core (SD-1, SD-3, SD-6, SD-8, SD-9 verbatim) and
re-grounds everything else on code that exists today:

| v2 assumed | What shipped | v3 consequence |
|---|---|---|
| One backend = the self-hosted D1 Worker (`orun backend init`) | Orun Cloud is the canonical backend: `config-worker` on Supabase Postgres via Hyperdrive; the OSS self-host serves the same contract paths (`_local/_local`) | Values live in `config.secret_metadata`/`config.secret_versions` (Postgres). The **contract is the product**; D1 parity is a follow-up (§11, D-2) |
| `namespace = org/repo` + reserved `base` env + explicit `_shared/<group>` namespaces | Account → Workspace (org) → Project (== repo, OV2 bijection) → Environment spine; WID7 shipped a scope-resolution chain (`environment → project → workspace → account`) for settings, "designed so secret_metadata can adopt the same shape later" (`config-resolver.ts:23-24`) | **Secrets adopt the platform chain** (§6). `base` → project-scope rows; `_shared` groups → workspace/account-scope rows; D-1 (namespace rename) is **closed** |
| GitHub numeric user id as the identity key; a new `gh_identity_map` subsystem fed by a GitHub App | The platform already has identity (sessions, `sk_` keys, CI OIDC exchange → one ActorContext, OV3 ✅), membership + teams with account cascade (WID6 ✅), and a GitHub App in `integrations-worker` | **Subjects are platform principals** (users, teams, service principals, `workflow` actors). The `gh_identity_map` subsystem is deleted from the design; GitHub team *sync* is a later integration, not a prerequisite (§5) |
| A from-scratch four-axis policy engine | `packages/policy-engine` ships deny-by-default role×scope matrices with account cascade + per-action provenance (TM6b1); `secret.read/write/value.use` actions **exist but are dormant** (`index.ts:334-336`, no caller) | **Two-layer policy** (§7): Layer 1 activates the dormant actions on the shipped engine; Layer 2 is the portable `SecretPolicy` *conditions* document — the four axes — evaluated at resolve time |
| A run "lease" sketched in the contract | The lease plane is live: DO-backed `RunCoordinator` fold (`holder`/`leaseEpoch`/`leaseExpiresAt`) + relational `state.run_jobs`; `CoordBackend` is the CLI default | Resolve binds to the real lease and **adds `leaseEpoch` to the wire body** (§8.1) — the contract's `{runnerId, jobId, keys}` cannot distinguish a swept stale holder |
| Envelope crypto with per-namespace DEK/KEK (aspirational) | Shipped: AES-256-GCM under a **single static `SECRET_ENCRYPTION_KEY`**, encrypt-only — no decrypt path exists anywhere (`encryption.ts:27-29,70`); rotate overwrites ciphertext in place, **no version history** (`repository.ts:397-421`) | v3 keeps the envelope, adds `keyId` + per-workspace DEK wrapped by a KEK in Cloudflare Secrets Store (entitlement confirmed 2026-06-12, saas-secrets-sync SS4), adds an append-only `secret_versions` table, and puts the **only** decrypt path in the resolve handler (§4) |

Everything else v2 got right survives: values-never-content, write-only API +
break-glass, plan visibility, redaction, materialization, the catalog facet,
personal overlays, and the SEC milestone spine.

## 1. Problem (unchanged in substance, refreshed citations)

1. **Secrets have nowhere safe to live in orun today.** `env` merges through
   four layers (`internal/expand/expander.go:321-348`) into `PlanJob.Env`
   (`internal/model/plan.go:163`) as plain strings. A secret placed in `env`
   serializes into `plan.json` — immutable, content-addressed, dedup'd
   org-wide on push. **There is no way to un-leak it.**
2. **Logs leak too.** Step output is captured raw (`internal/runner/runner.go:575`)
   and fans out to the remote log pipeline and the object-model blob write
   (`internal/objrun/objrun.go:147-155`). No redaction exists.
3. **Policy is declared but inert.** `resolvePolicies` populates
   `ComponentInstance.Policies` (`internal/expand/expander.go:351-375`) and
   **nothing reads it** — the map is dropped at the render boundary
   (`JobInstance` and `PlanJob` carry no policies field).
4. **The platform's secret store is write-only storage, not a secret manager.**
   `config-worker` encrypts on write and cannot decrypt; there is no resolve
   route, no version history, no inheritance for secrets, no personal overlays,
   and the dedicated `secret.*` RBAC actions have no caller (secrets piggyback
   on `*.config.read/write` — `create-secret.ts:161`).
5. **Nothing delivers secrets to jobs or running applications.** Runners have
   no resolve path; deployed Workers get values hand-pasted (`wrangler secret
   put`) outside policy, audit, and rotation. (The platform's *own* worker
   secrets got the `saas-secrets-sync` escrow tooling — proof of the need; the
   customer-facing product must be first-class.)

## 2. Goals / non-goals

**Goals** — G1–G9 of v2 carry over verbatim (values never content;
Doppler-grade ergonomics; portable policy; real enforcement at two points;
execution-platform awareness; no-leak logs; one backend; the last mile;
catalog-native), with two re-groundings:

- **G7′ — One backend means the shipped one.** Reuse `config-worker`,
  `policy-worker`, `state-worker`, `identity-worker`, and the api-edge actor
  spine. Add tables, routes, and one service-binding seam — not a service.
- **G10 — Converge, don't fork, the platform's own patterns.** The WID7 scope
  chain, the WID6 cascade, `appendEventWithAudit`, the coordination lease, and
  the `saas-secrets-sync` materialization tooling are the house patterns;
  secrets adopt them.

**Non-goals (v1 scope of v3)** — unchanged: dynamic/leased credentials, a full
policy DSL (locked predicate vocabulary; CEL is the upgrade path), inbound
provider sync, a runtime SDK inside application processes, and self-hosted KEK
custody hardening (Q-2). Plus one new: **GitHub team-membership sync** into
platform teams is an integrations-worker follow-up, not part of SEC.

## 3. The model in one picture

```
AUTHOR TIME (repo / Stack — all content, all portable)
  Stack ─┬─ compositions/<type>/…                     component contracts (existing)
         ├─ compositions/<type>/secretBindings         "this profile needs DATABASE_URL"    (NEW)
         ├─ compositions/<type>/secret-policy.yaml     composition-attached conditions      (NEW, SD-10)
         ├─ compositions/<type>/profiles/* materialize "deliver to the deployed Worker"     (NEW, SD-13)
         └─ policies/*.SecretPolicy.yaml               stack-wide conditions                (NEW, SD-5)
  intent.yaml / component.yaml                         secretEnv: { DB_URL: "secret://…" }  (NEW, reference only)

PLAN TIME  (orun plan — references only, value-free, reviewable)
  expand (mergeSecretEnv) → planner → render
  PlanJob.secretRefs = [ secret://acme/api/prod/DATABASE_URL ]      ← NO value in plan.json
  compile-time grant check; chain-provenance markers ("serves from workspace scope")
  materialize steps rendered explicitly

RUN TIME  (orun run — the value lives only here)
  runner authenticates (workflow OIDC | CLI session | sk_)  →  ActorContext (OV3, shipped)
  runner claims job → live lease {holder, leaseEpoch, leaseExpiresAt}        (OP2/BM4b, shipped)
     → POST …/state/runs/{runId}/secrets/resolve  {runnerId, jobId, leaseEpoch, refs}
        state-worker:  verify token scope + LIVE LEASE (both backends)       (NEW)
           → service binding → config-worker internal resolve                (NEW)
        config-worker: Layer-1 RBAC (secret.value.use, policy-worker)        (activate dormant)
                       Layer-2 SecretPolicy conditions (env/trigger/platform/component)
                       chain walk: personal → environment → project → workspace → account
                       unwrap workspace DEK → AES-256-GCM decrypt            (NEW decrypt path)
                       stamp last_used_at → secret.accessed audit per key
                       → { secrets, resolved[{key,version,scope,decisionId}], ttlSeconds }
  runner: inject as top, non-persisted env layer (runner.go:1365)
          seed per-run redactor; redact once, upstream of ALL sinks (after runner.go:575)
          materialize step: push values into the target platform store, record sync (SD-13)
  seal:   ExecutionRun records {key, version, decisionId} — provenance, never values

CATALOG (derived, rebuildable)
  Component entity ← extensions.x-orun-secrets {requirements, bindings, rotation, syncs}
  Scorecards grade it: bindings-satisfied, rotation-age, no-stale-syncs      (SD-14)

STORE (Orun Cloud — the only place ciphertext lives)
  Postgres (config schema):  secret_metadata, secret_versions, secret_deks,
                             secret_policies, secret_syncs  (+ events/audit_entries)
  Cloudflare Secrets Store:  per-workspace DEK wrap KEK (SS4 entitlement confirmed)
```

## 4. The carve-out: where values live (SD-1, SD-2′)

**SD-1 (unchanged):** secret values live **only** in Orun Cloud, encrypted.
Repo/plan/graph/logs carry only `secret://<workspace>/<project>/<env>/<KEY>[@v]`
references (`data-model.md` §1). The rationale is unchanged: content addressing
makes objects immutable and dedup'd; secrets must be mutable and revocable.

**SD-2′ (revised):** envelope encryption **on the shipped envelope, upgraded**:

- Keep the shipped `CiphertextEnvelope{alg:"AES-256-GCM", v, iv, ct}`
  (`apps/config-worker/src/encryption.ts:14-23`) and add `keyId`.
- Introduce a **per-workspace DEK** (blast radius and cryptoshred unit =
  workspace), wrapped by a **master KEK** held in **Cloudflare Secrets Store**
  (bound to config-worker; the entitlement is confirmed on the account —
  `saas-secrets-sync` SS4).
- **Migration:** existing envelopes carry implicit `keyId:"k0"` (the static
  `SECRET_ENCRYPTION_KEY`); the decrypt adapter reads both; writes and rotations
  move rows to the workspace DEK. No big-bang re-encrypt.
- **Versioning:** a new append-only `config.secret_versions` table holds one
  ciphertext envelope per version (`data-model.md` §7). `secret_metadata`
  becomes the head pointer. This unlocks history, pinning (`@<n>`), per-version
  revoke, and rollback — today's in-place overwrite loses all of that.
- The **only decrypt path in the codebase** is the resolve handler
  (config-worker imports the key with `["decrypt"]` usage there and nowhere
  else). Today no decrypt exists at all — v3 keeps that surface minimal.

## 5. Identity: platform principals, GitHub-connected (SD-4′)

**SD-4′ (revised):** policy subjects are the **platform's own principals** —
the ones every worker already authenticates and authorizes:

| Caller | How it authenticates today (shipped) | Subject for policy |
|--------|--------------------------------------|--------------------|
| Human (CLI / console) | CLI session JWT / browser session (`resolve-bearer.ts:19-36`) | `user:<subjectId>` + team memberships (membership-worker facts) |
| CI runner (GitHub Actions) | OIDC exchange → 15-min workflow token bound to `(org, project)` (`oidc-exchange.ts`, `jwt.ts:136-152`) | `workflow` actor; OIDC claims (`repository`, `ref`, `environment`) become policy **facts** |
| Service / non-GitHub runner | `sk_` API key → service principal (`060_identity_api_keys`) | `service_principal:<id>` |

Teams, roles, and the **account cascade** come from membership-worker's
authorization facts (`authz-facts.ts:40-94`) — grants on the account remap onto
child workspaces with `grantedVia:{kind:"account_cascade"}`, and the policy
engine reports the permitting fact (TM6b1). A `SecretPolicy` that names
`team:<slug>` resolves at decision time against current membership.

**What happened to "GitHub numeric ids are the identity key"?** The *portable
policy* goal survives — a `SecretPolicy` document still means the same thing
wherever it's evaluated — but portability now keys on **slugs and principal
kinds**, not raw GitHub ids, because the platform's identity spine (not GitHub)
is what every enforcement point already trusts. The GitHub App integration
(`integrations-worker`, live for check-run write-back) is the seam through
which GitHub *team sync* can later populate platform teams — an integration,
not an identity system. This deletes an entire v2 subsystem (`gh_identity_map`,
membership webhooks) from the critical path.

**Doppler's "service tokens" are `sk_` keys.** A read-scoped machine token is
an existing service principal whose role grants `secret.value.use` under a
`SecretPolicy` that pins it to an environment — no new token type (§7.3).

## 6. Environments: the platform chain + personal configs (SD-11′, SD-12′)

**SD-11′ (revised):** per-key resolution walks the **platform's own scope
chain** — the one WID7 shipped for settings (`config-resolver.ts:66-132`) and
explicitly reserved for secrets — extended at the top with personal overlays:

```
personal(environment, subject)   →   environment   →   project   →   workspace   →   account
  (local-cli only, owner only)       (env config)      (== repo;      (org-shared)    (company-wide,
                                                        v2's "base")                   lockable)
```

- **Project scope replaces v2's reserved `base` env** — defaults shared by every
  environment of a repo are project-scope rows. No reserved name, no second
  environment concept; `environment_id` stays an opaque reference to
  `projects.environments` (`070_config_settings_flags/up.sql:186`).
- **Workspace and account scopes replace `_shared/<group>` namespaces
  (SD-12′).** An org-wide `DATADOG_API_KEY` is a workspace-scope secret; a
  company-wide value is account-scope. Sharing is **visible, not ambient**:
  `orun plan` marks every ref that resolves from above project scope
  ("serves from workspace"), `orun secrets list --chain` renders the rungs, and
  the catalog facet records the serving scope. A `SecretPolicy` can deny
  cross-scope serves per environment for the cautious.
- **Guardrails (from WID7):** account- and workspace-scope secrets may be
  marked `overridable: false` — a lower rung then **cannot** shadow them (write
  rejected 409, mirroring `create-setting.ts:189-190`). This is governance
  Doppler doesn't have: the platform team can pin a company-wide value.
- **Personal configs** are per-`(environment, subject)` overlay rows: `orun
  secrets set DB_URL --env dev --personal` stores a value only its owner can
  resolve, and **only** when the server-derived platform fact is `local-cli`
  (§7.2). CI and service resolves never see personal values — structurally
  (Invariant 9).
- The chain is **fixed** in v1 of v3. Arbitrary env-extends-env graphs stay
  deferred.

References stay identity-free: a plan carries
`secret://acme/api/dev/DB_URL` regardless of who runs it; personalization is a
resolve-time overlay, so plans remain content-stable. `orun plan` marks keys
that may be personally shadowed for the current user.

## 7. Policy: two layers, portable conditions, enforced twice (SD-5, SD-6, SD-10, SD-15′)

Full mechanics in `policy-model.md`. The v2 four-axis model survives intact —
what changes is **where each half runs**.

### 7.1 Layer 1 — role×scope RBAC (shipped engine, dormant actions activated)

`packages/policy-engine` already defines `secret.read`, `secret.write`,
`secret.value.use` in its role matrices (owner/admin: all three; builder:
read + value.use; viewer: read — `index.ts:67-178,334-336`) with account
cascade and per-action provenance. **No route consumes them today** — secret
handlers authorize with `*.config.read/write`. v3's first policy change is
simply to route the secret endpoints through their own actions. This is the
coarse gate: *may this principal, in this role, on this scope, perform this
class of operation at all?*

### 7.2 Layer 2 — `SecretPolicy` conditions (the portable document)

The orun-differentiator: a versioned, Stack-shipped document of
`match → allow|deny` rules over the four axes:

| Axis | Source (all shipped) | Example |
|------|----------------------|---------|
| **Who** | platform principal + teams (§5) | `subject.team == "platform-admins"` |
| **What** (component) | `ComponentInstance{type,domain,name}` | `component.type == "terraform"` |
| **Where** (env/trigger) | `PlanTrigger` + `TriggerOccurrence` facts (`internal/triggerctx/context.go:61-76`) | `env == "prod" && trigger.branch == "main" && trigger.declared` |
| **How** (platform) | server-derived from the **actor kind**: `workflow` = CI-OIDC, CLI-session user = `local-cli`, `sk_` = `service` (`resolve-bearer.ts`) | `platform == "ci-oidc"` (deny `local-cli` for prod) |

Layer 2 is evaluated **in config-worker at resolve time** (subject, trigger
facts, and platform fact all concrete), after Layer 1 passes. Deny-by-default
against the *conditions* applies to `secret.value.use` on protected
environments; explicit `deny` beats `allow`; most-specific scope wins.

**Three placement tiers, narrow-only downward (SD-10, unchanged):**
composition-attached fragments (auto-scoped to their composition type) → Stack
`policies/` → intent overlays that may only narrow. The tier loader and
compile-time check live in orun; documents are pushed to the backend
(`data-model.md` §8) so fetch-time evaluation uses the same rules the plan
displayed.

### 7.3 Enforced at two points (SD-6, unchanged)

- **Compile time (`orun plan`)** — existential check: every requested ref is
  permitted *in principle* for its (environment, trigger, component) under the
  resolved documents; rendered per job with no values. Fail-fast UX.
- **Fetch time (`orun-api`)** — authoritative: Layer 1 then Layer 2 with the
  concrete subject; typed denial with a stable reason code; every decision
  audited (allow and deny). **This is the security boundary.**

**SD-15′:** the state-api contract stays normative for the wire; v3 *revises*
its §4 (adds `leaseEpoch`, chain provenance, versions/personal/policies routes)
rather than adding a second API. `SecretPolicy` remains the engine behind the
contract's `secret.value.use`.

## 8. Delivery: injection for jobs, materialization for apps (SD-3, SD-8, SD-13)

### 8.1 The resolve path (the keystone — everything it needs shipped already)

```
POST …/state/runs/{runId}/secrets/resolve
{ "runnerId": "host-abc", "jobId": "deploy-api-prod", "leaseEpoch": 3,
  "refs": ["secret://acme/api/prod/DATABASE_URL", "secret://acme/api/prod/STRIPE_KEY@7"] }
→ 200 { "secrets": { "DATABASE_URL": "…", "STRIPE_KEY": "…" },
        "resolved": [ { "key": "DATABASE_URL", "version": 9, "scope": "environment",
                        "personal": false, "decisionId": "dec_…" }, … ],
        "ttlSeconds": 300 }
```

Two independent checks, then the value flows:

1. **state-worker** (owns the run plane): token authz as for any coordination
   verb, **plus live-lease verification** — the job's fold state (DO path:
   `phase == "claimed" && holder == runnerId && leaseEpoch matches &&
   leaseExpiresAt > now`, `run-coordinator.ts:288,308`) or the relational
   `state.run_jobs` row (OP2 path). `leaseEpoch` is **added to the wire body**
   — without it a swept-and-requeued stale runner is indistinguishable from
   the current holder (heartbeat/complete already require it,
   `coordination-native.ts:209,239`). Requires a new exported
   `verifyLiveLease()` helper covering both backends.
2. **config-worker** (owns ciphertext + policy), reached over a **service
   binding** with the verified actor + run facts (environment slug translated
   to `environment_id` on the way — the run stores a slug, secrets store a
   UUID): Layer-1 `secret.value.use` via policy-worker, Layer-2 `SecretPolicy`
   conditions, chain walk per key, DEK unwrap, decrypt, `last_used_at` stamp,
   `secret.accessed` audit event per key (via the existing
   `appendEventWithAudit`, `events/repository.ts:188`).

The **workflow token alone can never authorize a resolve** — it binds only
`(org, project)` with a 15-minute TTL (`jwt.ts:136-152`). Token authz AND live
lease are independently required. Values are TTL'd (`ttlSeconds`), live only in
runner memory, and are never persisted client-side.

**Write-only API + break-glass reveal (SD-3, unchanged):** `orun secrets set`
returns metadata; there is no routine reveal. One audited
`orun secrets reveal --break-glass` exists for incident recovery, gated by an
elevated action and always alerting.

### 8.2 Injection and redaction (SD-8, seams verified)

- **Inject:** resolved values merge as the highest-precedence, non-persisted
  env layer in `stepExecContext` (`internal/runner/runner.go:1365` — a new
  final argument to `MergeEnvironment`) and the finalizer merge (`:643`).
  Plaintext never reaches `PlanJob.Env`, the plan, refs, or any L0 object.
- **Redact:** one redactor, applied **once, immediately after capture**
  (`runner.go:575/591`) — *upstream* of all three sinks (view analysis, the
  `AfterStepLog` hook feeding both the remote log pipeline and the objrun blob
  write, and the GHA emitter). Not inside the hooks: remote setup *replaces*
  `r.Hooks` while objrun *chains*, so hook-level redaction is
  ordering-fragile. Registered forms: raw + base64 + URL + JSON-escaped.
- **Memoization safety:** `jobInputHashFor` already hashes env **keys only**
  (`internal/statebackend/coordbackend.go:31-38`); `secretRefs` keys + resolved
  versions join the hash, values never do.

### 8.3 Runtime materialization (SD-13, unchanged; converges with shipped tooling)

The application's runtime store is the delivery target and orun owns the write
path: a deploy profile's `materialize:` block renders as an explicit plan step,
executes under the same policy + audit, records `{key, version, target,
entityRef, ts}` in `secret_syncs`, and stamps the provisioned entity. Rotation
raises `onRotate` system triggers so running apps converge through the normal
deploy path. First adapter: `cloudflare-worker` (over the `SetWorkerSecret`
primitive, `internal/cloudflare/client.go:437`).

The platform already dogfoods this shape for its *own* workers
(`saas-secrets-sync`: escrow → `assemble.mjs` per-worker view → `sync.mjs`
decision → `wrangler secret bulk`, fingerprint records). The product
materialization is the same pattern with the orun secret store as the source
and the catalog as the record; the SS tooling is prior art, not a competitor.

## 9. Inter-job value passing (SD-9, unchanged)

Declared job `outputs` on explicit `dependsOn` edges; non-sensitive outputs are
graph content in the reserved `artifacts/` slot; `sensitive: true` outputs
route through the secret backend as run-scoped references whose DEK dies at
run GC. Redacted like any secret.

## 10. Decisions register (v3)

| # | Decision | Status vs v2 |
|---|----------|--------------|
| **SD-1** | Values live in Orun Cloud only; graph carries `secret://` references | unchanged |
| **SD-2′** | Envelope AES-256-GCM (shipped shape) + `keyId`; per-**workspace** DEK wrapped by KEK in Cloudflare Secrets Store; append-only `secret_versions`; decrypt exists only in the resolve handler; `k0` lazy migration | revised |
| **SD-3** | Write-only API; injection-only delivery; single audited break-glass reveal | unchanged |
| **SD-4′** | Subjects are platform principals (users/teams/service principals/workflow actors); GitHub team sync is a later integrations-worker feature; `gh_identity_map` deleted | revised |
| **SD-5** | Policy is a portable document shipped in the Stack | unchanged |
| **SD-6** | Deny-by-default; enforced at compile (visible) and fetch (authoritative) | unchanged |
| **SD-7** | Locked predicate vocabulary in v1; CEL upgrade path | unchanged |
| **SD-8** | Values redacted before any sink — once, at capture, upstream of hooks | unchanged (seam fixed) |
| **SD-9** | Inter-job passing = declared outputs; sensitive → secret-backend references | unchanged |
| **SD-10** | Three policy tiers, narrow-only downward | unchanged |
| **SD-11′** | Resolution = the platform scope chain `personal → environment → project → workspace → account`; project scope replaces the reserved `base` env | revised |
| **SD-12′** | Sharing = workspace/account scope rows (replaces `_shared/<group>` namespaces); visible in plan/chain/facet; **lockable** via WID7-style `overridable:false` guardrails | revised |
| **SD-13** | Runtime delivery = materialization; plan-visible, provenance-tracked, rotation-driven | unchanged |
| **SD-14** | Secrets project onto the Component entity as `extensions.x-orun-secrets`; scorecards grade it | unchanged |
| **SD-15′** | The state-api contract stays normative; v3 revises its §4 (leaseEpoch, chain provenance, versions/personal/policy routes) — one API, one CLI | revised |
| **SD-16** | CLI is the `orun secrets` group; scope flags (`--project/--workspace/--account`) replace `--namespace` | revised (naming) |
| **SD-17** | **Two-layer policy**: Layer 1 = shipped role×scope engine with the dormant `secret.*` actions activated; Layer 2 = `SecretPolicy` conditions evaluated at resolve. One decision id covers both | **new** |
| **SD-18** | **Resolve is lease-bound with `leaseEpoch`**, verified in state-worker for both coordination backends; config-worker decrypts behind a service binding; token authz and lease are independent checks | **new** |
| **SD-19** | **Doppler service tokens ≡ `sk_` service principals** granted `secret.value.use` under a pinning `SecretPolicy` — no new token type | **new** |

## 11. Invariants (regression-tested — v2's ten, plus two)

1–10 as v2 (no value in content; reference integrity; deny-by-default; audit
completeness; redaction soundness; sealed-run provenance; cryptoshred —
now per-workspace DEK; policy portability; personal isolation — owner +
`local-cli` platform fact only; materialization provenance). Added:

11. **Lease-bound resolve.** No resolve succeeds without a live lease match
    `(runId, jobId, runnerId, leaseEpoch)`; a lapsed or swept lease yields
    `409 lease_lost`, never values.
12. **Version monotonicity.** `secret_versions` is append-only; a version, once
    written, is immutable; head moves forward only; a revoked version can never
    serve again.

## 12. Alternatives considered (v3 deltas)

v2's register stands (ciphertext-in-repo, standalone microservice, runtime SDK,
plan-encryption, org-global namespace, RBAC-only — all still rejected for the
same reasons). New/updated:

- **Keep GitHub numeric ids as the policy subject key.** Rejected in v3 — it
  requires building and trusting a second identity plane (`gh_identity_map` +
  webhooks) when every enforcement point already trusts platform principals;
  GitHub team sync via integrations-worker delivers the same org-mirroring
  value later without gating secrets on it.
- **Evaluate SecretPolicy inside policy-worker.** Considered; deferred.
  policy-worker is a stateless pure-matrix engine; conditions need trigger and
  chain facts that live with the resolve. Layer 2 evaluates in config-worker
  (a lib, unit-testable) with the door open to move once a second consumer of
  condition-policies exists.
- **A reserved `base` environment (v2).** Dropped — the shipped project scope
  *is* the base rung; inventing a reserved env name on top of a real scope
  chain would be a second inheritance concept.
- **Per-project DEK.** Considered vs per-workspace. Workspace wins: it matches
  the tenancy blast-radius unit (an org's projects share operators), keeps DEK
  count bounded, and cryptoshred-per-workspace matches offboarding reality. A
  later per-project generation is admissible via `keyId`.
- **Run-scoped resolve without `leaseEpoch` (the v2/contract sketch).**
  Rejected — the sweep-requeue race makes a stale holder indistinguishable;
  the coordination plane already learned this lesson (heartbeat/complete carry
  `leaseEpoch`).
