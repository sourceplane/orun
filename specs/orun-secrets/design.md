# Design

> orun gains a secret store shaped like Doppler but built for orun's invariants:
> values live encrypted in Orun Cloud and **never** enter the content-addressed
> graph; the plan carries only `secret://` references; access is a portable,
> GitHub-native `SecretPolicy` attachable from the individual composition up to
> the intent; environments inherit per-key (`base → env → personal`); the runner
> injects plaintext at step launch and redacts it from logs; deployed
> applications receive secrets by policy-governed materialization into their
> platform's native store; and the whole surface projects onto the service
> catalog as a scorecard-checkable entity facet. This doc fixes the model, the
> decisions, the enforcement seam, the invariants, and the alternatives.
> Schemas live in `data-model.md`; the policy engine in `policy-model.md`;
> delivery in `runner-integration.md`; the catalog/operations surface in
> `platform-integration.md`.

## 1. Problem

1. **Secrets have nowhere safe to live in orun today.** `env` is merged through
   four layers (`internal/expand/expander.go:316-348`) into `PlanJob.Env` as plain
   strings (`internal/model/plan.go:163`, `internal/render/plan.go:106,133-148`).
   A secret placed in `env` is serialized into `plan.json`, which is a node in the
   content-addressed `PlanRevision` tree (`specs/orun-object-model/design.md:81`)
   — immutable, pushed to R2, dedup'd org-wide
   (`specs/orun-object-model/remote-and-consumers.md:18-26`). **There is no way to
   un-leak it.**
2. **Logs leak too.** Step output is captured raw and written as a
   content-addressed blob (`internal/objrun/objrun.go:148-155`); "no secrets in
   logs" is only a coding-convention note. No redaction exists.
3. **Policy is declared but inert.** Group/environment `policies` resolve onto
   `ComponentInstance.Policies` (`internal/expand/expander.go:350-375`) and
   `ProfilePolicies{RequireApproval,…}` is defined
   (`internal/model/composition.go`) — but **nothing reads either**. The docs
   claim policies are "enforced as platform constraints"
   (`website/docs/concepts/intent-model.md:217`); the code does not enforce them.
4. **No portable identity or access model.** orun authenticates with GitHub
   (OIDC + OAuth, `internal/remotestate/auth.go:24-208`) but has no notion of
   *which GitHub user/team may read which secret under which conditions*.
5. **Nothing delivers secrets to the running application.** Even with pipeline
   injection solved, a deployed Cloudflare Worker or container still needs its
   `DATABASE_URL` at request time. Without a designed answer, users will
   hand-copy values into `wrangler secret put` / cloud consoles — outside policy,
   outside audit, outside rotation.
6. **The org wants one platform.** A medium SaaS company should manage its
   compositions, stacks, operations, catalog, **and** secrets on orun. The
   sibling `multi-tenant-saas` repo already ships an envelope-encrypted secret
   store (`apps/config-worker`) and a deny-by-default RBAC engine
   (`apps/policy-worker`) — orun should converge on those patterns,
   GitHub-native and policy-portable, not fork a second philosophy.

## 2. Goals / non-goals

**Goals**
- **G1 — Values never become content.** Secret material lives only in the Orun
  Cloud backend (D1, envelope-encrypted) and transiently in the runner's memory
  and the child process. The object graph, refs, plan, and logs carry only
  references and resolved-version provenance.
- **G2 — Doppler-grade ergonomics.** `orun secrets set`, declarative references,
  automatic env injection, environment inheritance with personal dev overlays,
  versioning and rotation, dotenv import — the value is delivered to the
  workload without the author touching ciphertext.
- **G3 — Portable, GitHub-native policy.** A `SecretPolicy` document binds
  GitHub identities (stable numeric user ids + team slugs) to secret scopes
  under component/environment/trigger/platform conditions. It ships *in the
  Stack* — attachable down to the individual composition — so a paved-road
  platform carries its own access rules across repos and orgs.
- **G4 — Real enforcement, two points.** Deny-by-default, enforced at compile
  time (grant validation, visible in `orun plan`) and at fetch time (the
  security boundary in `orun-api`).
- **G5 — Execution-platform awareness.** Policy can condition on *where the run
  executes* (local CLI, GitHub Actions OIDC, Orun Cloud runner) — derived from
  the auth mode orun already resolves (`internal/remotestate/auth.go:156-208`).
- **G6 — No-leak logs.** Every resolved value is redacted from the log stream
  before any blob is written.
- **G7 — One backend.** Reuse the Worker + D1 + R2 + Durable Objects that
  `orun backend init` already provisions; add tables and routes, not a service.
- **G8 — The last mile.** Deployed applications receive secrets by
  materialization during the deploy job — same policy, same audit, recorded on
  the provisioned entity — with rotation triggering re-materialization.
- **G9 — Catalog-native.** Secret requirements, binding status, rotation age,
  and sync state are entity data in the service catalog, gradeable by
  scorecards.

**Non-goals (v1)**
- Dynamic / leased secrets (Vault-style short-TTL generated credentials) — the
  reference model is designed to admit them later (§9).
- A full user-authored policy DSL — v1 locks an allowlisted predicate
  vocabulary; CEL is the named upgrade path (`policy-model.md` §6).
- External-provider *sync inbound* (pulling values from AWS Secrets Manager into
  orun) — the envelope `provider` field reserves the seam. (Outbound
  materialization **is** in scope, §8.)
- A runtime SDK / agent inside the application process — rejected as an
  alternative (§12).
- Self-hosted-backend KEK custody hardening — see Q-2.

## 3. The model in one picture

```
AUTHOR TIME (in the repo / Stack — all content, all portable)
  Stack ─┬─ compositions/<type>/…                    component contracts (existing)
         ├─ compositions/<type>/secretBindings        "this profile needs DATABASE_URL"     (NEW)
         ├─ compositions/<type>/secret-policy.yaml    composition-attached policy defaults  (NEW, SD-10)
         ├─ compositions/<type>/profiles/* materialize  "deliver to the deployed Worker"    (NEW, SD-13)
         └─ policies/*.SecretPolicy.yaml              stack-wide access rules               (NEW, SD-5)
  intent.yaml / component.yaml                        secretEnv: { DB_URL: "secret://…" }   (NEW, reference only)

PLAN TIME  (orun plan — references only, value-free, reviewable)
  expand → planner → render
  PlanJob.secretRefs = [ secret://ns/prod/DATABASE_URL@latest ]   ← NO value in plan.json
  compile-time grant check: every requested ref is permitted by the resolved policy
  materialize steps rendered as explicit plan steps ("sync 2 secrets → worker:api")

RUN TIME  (orun run — value lives only here)
  runner authenticates (OIDC | session | ORUN_TOKEN)
     → POST …/state/runs/{runId}/secrets/resolve  (contract §4; live lease; secret.value.use)
     orun-api:  policy decision (deny-by-default, four axes — the secret.value.use engine)
                → env-chain resolution per key: personal(env,user) → env → base   (SD-11)
                → unwrap namespace DEK → AES-256-GCM decrypt
                → { secrets, ttlSeconds } over TLS + secret.accessed audit event
  runner: inject plaintext as top, non-persisted env layer in stepExecContext
          register every value in the per-run redactor
          materialize step: push values into the target platform store, record sync (SD-13)
  seal:   ExecutionRun records resolved {key, version, decision-id} — provenance, never values

CATALOG  (derived, rebuildable)
  Component entity ← extensions.x-orun-secrets { requirements, bindings, rotation, syncs }
  Scorecards grade it: bindings-satisfied, rotation-age, no-stale-syncs   (SD-14)

STORE  (Orun Cloud — the only place ciphertext lives)
  D1:  secret_metadata, secret_versions(ciphertext_envelope), secret_deks,
       secret_policies, secret_syncs, gh_identity_map, secret_audit
  Cloudflare Secrets Store:  per-namespace DEK, wrapped by a master KEK
```

## 4. The carve-out: where values live (SD-1, SD-2)

**Decision (SD-1):** secret values live **only** in Orun Cloud (D1), encrypted.
The repo/plan/object-graph/logs carry **only**
`secret://<namespace>/<env>/<KEY>[@<version>]` references (grammar in
`data-model.md` §1).

**Why not the object graph (SOPS-style ciphertext-in-repo)?** It is the most
"orun-native" option and was tempting, but it weaponizes orun's own strength:
content addressing makes objects immutable and dedup'd org-wide on push
(`remote-and-consumers.md:18-26`). Ciphertext-in-graph means a future KEK
compromise retroactively exposes the *entire immutable history*, with no
revocation. Values must stay mutable and revocable — i.e., outside L0/L1.

**Decision (SD-2):** **envelope encryption**, mirroring the shipped pattern in
`multi-tenant-saas/apps/config-worker/src/encryption.ts`:
- A per-value `CiphertextEnvelope{alg:"AES-256-GCM", v, keyId, iv, ct}` (the
  existing shape plus `keyId`).
- A **per-namespace Data Encryption Key (DEK)** encrypts that namespace's values.
- The DEK is wrapped by a **master Key Encryption Key (KEK)** held in **Cloudflare
  Secrets Store** (bound to the Worker, never in D1).
- `keyId` + `alg` + `v` give a migration path to an external KMS (Q-2) and let a
  namespace be **cryptoshredded** by destroying its DEK.

The Worker already has a secret-binding mechanism (`SetWorkerSecret`,
`internal/cloudflare/client.go:437`); the KEK uses the same primitive.

## 5. Identity: GitHub-native and portable (SD-4)

**Decision (SD-4):** the portable identity key is the **GitHub numeric user id**
(stable; usernames change, ids do not). Policy subjects are GitHub numeric user
ids, GitHub **team slugs** (`org/team`), or special principals
(`*authenticated`, service principals). This makes a `SecretPolicy` portable:
the same document means the same thing in any repo or org, because GitHub ids
are global.

**Three caller identities, all resolved to a GitHub-rooted subject:**

| Caller | How it authenticates today | Subject for policy |
|--------|----------------------------|--------------------|
| Human (CLI / dashboard) | GitHub OAuth session (`internal/cliauth`) | GitHub numeric user id + resolved team memberships |
| CI runner (GitHub Actions) | GitHub Actions **OIDC** (`internal/remotestate/auth.go:24-88`); the JWT carries `repository`, `actor`, `ref`, `workflow` | `actor` → GitHub user id; `repository` → namespace; claims become policy facts |
| Orun Cloud runner / service | `ORUN_TOKEN` / service-principal binding | service principal id (mapped to GitHub App installation) |

**The GitHub identity map (SD-4).** Orun Cloud installs as a **GitHub App** on a
customer org. On install, it ingests the org's users (→ numeric ids) and teams
(→ slugs + membership) into a `gh_identity_map` table in D1 (`data-model.md`
§7). The map is a **cache/projection** of GitHub, refreshed on membership
webhooks — never the source of truth. A `SecretPolicy` that grants
`@acme/platform-admins` resolves, at decision time, to the current membership of
that GitHub team. This is exactly the `multi-tenant-saas` membership→policy
pattern (`apps/policy-worker`), narrowed to GitHub identities.

> **Consequence:** a customer's GitHub org *is* their orun access model. No
> second user directory, no invite flow, no per-platform identity — install the
> App and your existing teams govern your secrets.

## 6. Environments: inheritance and personal configs (SD-11)

The flat `(namespace, env, key)` model of v1 forces full duplication across
dev/staging/prod and gives developers nothing personal to iterate in. Doppler's
most-loved ergonomic — root configs with inheriting branch configs — maps
cleanly onto orun's existing most-specific-wins precedence
(`internal/expand/expander.go:182-187`), so it will feel native:

**Decision (SD-11):** per-key resolution walks a fixed three-link chain:

```
personal(env, gh_user_id)   →   env   →   base
   (local-cli only,             (declared      (reserved env name;
    owner only)                  in intent)     org-wide defaults)
```

- **`base`** is a reserved environment holding defaults shared by every
  environment (e.g., a sandbox `SMTP_HOST` every non-prod env uses). A key
  defined in `dev` shadows the same key in `base`.
- **Environments** are the environments intent already declares
  (`internal/model/intent.go:59-71`); the secret store does not invent a second
  environment concept. First `orun secrets set --env <e>` for an env
  auto-creates its config.
- **Personal configs** are per-`(env, GitHub user id)` overlays: `orun secret
  set DB_URL --env dev --personal` stores a value only its owner can resolve,
  and **only** when `platform == local-cli` (the platform fact is
  server-derived, `runner-integration.md` §3). CI and cloud runners never see
  personal values — structurally, not by convention (Invariant 9). This gives
  every developer a private sandbox without forking the shared `dev` config or
  pasting values into `.env` files.
- The chain is **fixed at three links** in v1. Arbitrary inheritance graphs
  (env-extends-env) are deferred — they add resolution ambiguity for marginal
  value, and `base` covers the dominant case.

References stay identity-free: a plan carries `secret://acme/api/dev/DB_URL`
regardless of who runs it; personalization is a **resolve-time overlay**, so
plans remain content-stable and shareable. `orun plan` marks keys that *may* be
personally shadowed for the current user so there are no silent surprises.

**Decision (SD-12 — namespace granularity, closes v1's Q-6):** the default
namespace is the **repo** (`acme/api`), which bounds blast radius. Org-wide
sharing is explicit: a **shared group** namespace `acme/_shared/<group>` (e.g.,
`acme/_shared/observability` holding `DATADOG_API_KEY`). A component must
*bind* a shared group (`data-model.md` §2.3) and a policy must *grant* it —
sharing is visible in the plan and the catalog, never ambient.

## 7. Policy: portable, three tiers, enforced twice (SD-5, SD-6, SD-10)

This is the heart of the user-facing design. Full mechanics in
`policy-model.md`.

### 7.1 Requirements vs grants (orun's house separation of concerns)

orun already separates *composition owns the contract* from *intent/platform
owns the policy* (`context-for-ai/00-orun-repo-philosophy.md:20-31`). Secrets
follow that line exactly:

- **Requirement (composition layer, portable in the Stack).** A composition
  declares, per execution profile, the **logical secrets it needs**:
  `secretBindings` on a `JobTemplate`/`ExecutionProfile`. Example: the
  `terraform` composition's `release` profile binds `AWS_ROLE_ARN: required`.
- **Grant (policy layer).** A `SecretPolicy` document says **who, where, and
  from which platform** a run may obtain a secret scope.

### 7.2 Three placement tiers, narrow-only downward (SD-10)

"Policy close to the compositions" is made literal — a grant can live at three
tiers, each riding an existing portable artifact:

| Tier | Lives in | Scope | Typical author |
|------|----------|-------|----------------|
| **Composition-attached** | `compositions/<type>/secret-policy.yaml` | auto-scoped to that composition type | the composition author: "terraform release may read `AWS_*` only on a declared main trigger from CI" |
| **Stack-wide** | `<stack>/policies/*.SecretPolicy.yaml` | the whole stack / cross-cutting | the platform team: prod rules, laptop denials, shared-group grants |
| **Intent overlay** | repo `intent.yaml` / `policies/` | this repo | the adopting org: tightening only |

Precedence is **narrow-only downward**: a composition fragment is
constitutionally scoped to its own composition type (it *cannot* grant beyond
itself); a Stack policy may add conditions or denials to it; an intent overlay
may only narrow what the Stack allows (an intent `allow` broader than any Stack
`allow` is rejected — `policy-model.md` §5). Adopting a golden Stack therefore
pulls in the compositions, *their own* access defaults, and the platform-wide
rules, versioned together over OCI — and no downstream repo can quietly loosen
them.

### 7.3 The four axes (Doppler-plus)

A `SecretPolicy` rule is `match → effect(allow|deny)` evaluated against four
axes orun already owns:

| Axis | Source | Example condition |
|------|--------|-------------------|
| **Who** (user-aware) | GitHub user id / team (§5) | `subject.team == "@acme/platform-admins"` |
| **What** (component-aware) | `ComponentInstance{type,domain,name}` (`internal/model/intent.go`) | `component.type == "terraform" && component.domain == "payments"` |
| **Where** (environment/trigger-aware) | `Environment` + `TriggerOccurrence{event,branch,declared}` (`internal/triggerctx/context.go:61-76`) | `env == "prod" && trigger.declared && trigger.branch == "main"` |
| **How** (execution-platform-aware) | resolved auth mode (`internal/remotestate/auth.go:156-208`) | `platform == "github-actions-oidc"` (deny `local-cli` for prod) |

Deny-by-default; explicit `deny` beats `allow`; most-specific scope wins. This
is strictly more expressive than Doppler's token-per-config model while
remaining a small, auditable predicate set (`policy-model.md` §6).

### 7.4 Enforced at two points (SD-6)

- **Compile time (`orun plan`).** The planner checks that every `secret://`
  reference a component requests is *permitted in principle* for its
  (environment, trigger, domain, component) under the resolved policy, and
  surfaces it in the plan: `job deploy → secrets: [DATABASE_URL@prod, …]` with
  **no values**. This satisfies the binding orun rule — *behavior must be
  visible in `orun plan`* (`context-for-ai/00-orun-repo-philosophy.md:90`) —
  and gives fail-fast errors before a run starts. Static facts only (subject
  may be unknown at plan time).
- **Fetch time (`orun-api`).** The authoritative decision: the runner presents
  its token + the reference + the trigger context; `orun-api` evaluates the
  full four-axis policy (subject now known), and returns plaintext or a typed
  denial with a stable reason code. **This is the security boundary;
  compile-time is a UX/guardrail courtesy.**

`SecretPolicy` is deliberately the **first profile of a general orun policy
engine**: the document shape, deny-by-default evaluation, narrow-only overlays,
and audit contract are domain-neutral, so the `policies` that orun carries
inertly today (approval gates, dependency modes) can adopt the same engine
without a second philosophy.

## 8. Delivery: injection for jobs, materialization for apps (SD-3, SD-8, SD-13)

Full detail in `runner-integration.md`.

### 8.1 Pipeline injection (SD-3)

The runner resolves references to plaintext in `stepExecContext`
(`internal/runner/runner.go:1320-1350`) and merges them as the
**highest-precedence, non-persisted** env layer for the child process only.
Plaintext never reaches `PlanJob.Env`, the plan, refs, or any L0 object.

**Write-only API + break-glass reveal (SD-3).** `orun secrets set` accepts a
value and returns metadata; there is **no routine reveal**. A single audited
`orun secrets reveal --break-glass` exists for incident recovery, gated by an
elevated policy action and always emitting an alert event.

**Redaction (SD-8).** All step output funnels through one hook (`AfterStepLog`,
`internal/runner/runner.go:579` → `internal/objrun/objrun.go:148-155`). A
per-run redactor, seeded with every resolved value (and common encodings),
replaces matches with `***` **before** the log blob is written.

### 8.2 Runtime materialization (SD-13) — the last mile

A deployed application reads its secrets at request time from *its own
platform* (Worker env bindings, container env, SSM). Forcing it to call
orun-api instead would put orun on the request path of every customer app — a
new availability dependency, a runtime SDK to maintain, and a worse failure
mode than the problem. So:

**Decision (SD-13):** the application's runtime store is the delivery target,
and orun owns the *write path* to it. A composition's deploy profile may
declare a `materialize:` block (`data-model.md` §2.4). During the deploy job,
the runner — under the **same** four-axis policy decision and audit as any
resolve — pushes the resolved values into the target platform's native secret
store via a typed adapter (first: `cloudflare-worker`, using the
`SetWorkerSecret` primitive orun already has,
`internal/cloudflare/client.go:437`). Three properties make this orun-grade
rather than a dumb sync:

1. **It is a plan step.** `orun plan` renders `materialize: 2 secrets →
   worker:api (prod)` — visible, reviewable, policy-checked at compile time.
2. **It is provenance-tracked.** Each sync writes a `secret_syncs` row
   `{key, version, target, entityRef, ts}` and stamps the **provisioned
   entity** the catalog already derives from composition effects
   (`internal/model/composition.go:75-90`, commit f35d537) — so the catalog
   answers "is the running Worker on the latest rotation of `STRIPE_KEY`?".
3. **Rotation re-materializes.** `orun secrets rotate` raises a system trigger
   for every deploy profile whose materialization includes that key, so the
   running app converges on the new version through the normal, audited deploy
   path — no out-of-band push.

The values do leave orun custody — that is inherent to running an application —
but they leave through one governed, recorded door instead of a hand-pasted
console (risk R-9).

## 9. Inter-job value passing (SD-9)

Distinct from secrets but shares machinery (both are runtime-produced values a
later job needs). Today only within-job `$ORUN_ENV` exists
(`website/docs/concepts/runtime-environment.md:210-236`); `dependsOn` is
ordering only.

**Decision (SD-9):** declared job **outputs**, carried on explicit `dependsOn`
edges so the dataflow is visible in `orun plan`:
- A job declares `outputs: { image: … }`; the runner captures them at job seal
  into the **already-reserved `artifacts/` slot** of the execution tree
  (`specs/orun-object-model/design.md:83`).
- A downstream job references `${{ jobs.build.outputs.image }}`.
- **Non-sensitive** outputs are stored as graph **content** (provenance,
  reproducibility — the orun way).
- **Sensitive** outputs (`sensitive: true`) are stored **encrypted via the
  secret backend** with only a run-scoped reference in the graph; their DEK is
  destroyed when the run is GC'd. They are redacted like any secret.

## 10. Decisions register

| # | Decision | Rationale |
|---|----------|-----------|
| **SD-1** | Values live in Orun Cloud only; graph carries `secret://` references | Content addressing is immutable + dedup'd; secrets must be mutable/revocable (§4) |
| **SD-2** | Envelope encryption; per-namespace DEK; KEK in Cloudflare Secrets Store | Reuses shipped `config-worker` pattern; enables cryptoshred + KMS migration |
| **SD-3** | Write-only API; injection-only delivery to humans/jobs; single audited break-glass reveal | A workload is the normal reader; humans should never routinely see values |
| **SD-4** | GitHub numeric user id is the portable identity key; teams via GH App map | Portable across repos/orgs; a customer's GitHub org *is* their access model |
| **SD-5** | Policy is a portable `SecretPolicy` document shipped in the Stack | Travels with the compositions it protects; versioned, OCI-distributable |
| **SD-6** | Deny-by-default; enforced at compile (visible) **and** fetch (authoritative) | Plan visibility honors orun philosophy; fetch is the real boundary |
| **SD-7** | Locked allowlisted predicate vocabulary in v1; CEL is the upgrade path | Matches the scorecards precedent (`specs/orun-scorecards/data-model.md` §3); auditable, deterministic |
| **SD-8** | Resolved values redacted from logs before any blob write | Closes the second leak vector at the single capture hook |
| **SD-9** | Inter-job passing = declared outputs on `dependsOn`; sensitive → references | Visible dataflow; safe credential hand-off; uses the reserved `artifacts/` slot |
| **SD-10** | Three policy tiers: composition-attached → Stack `policies/` → intent; narrow-only downward | "Close to compositions" made literal; golden paths carry their own defaults; downstream can't loosen |
| **SD-11** | Env chain `personal(env,user) → env → base`; personal is local-cli + owner-only; fixed three links | Doppler's branch-config ergonomic on orun's existing precedence model; refs stay identity-free |
| **SD-12** | Namespace = repo by default; org sharing via explicit `_shared/<group>` bind + grant | Bounded blast radius; sharing is visible, never ambient (closes Q-6) |
| **SD-13** | Runtime delivery = materialization into the platform's native store; plan-visible, provenance-tracked, rotation-driven | No runtime SDK / availability coupling; one governed door for values leaving custody |
| **SD-14** | Secrets project onto the Component entity as `extensions.x-orun-secrets`; scorecards grade it | Secret health is catalog data, not a side dashboard; reuses the entity extension seam (`entity_envelope.go:36`) |
| **SD-15** | The orun-cloud wire contract (§4 secrets, §6 policy map) is normative; orun-secrets elaborates it; `SecretPolicy` is the engine behind `secret.value.use` | One contract, not two; supersedes the orun-cloud placeholder design without forking its API or its already-shipping resolve discipline (README "Relationship to orun-cloud") |
| **SD-16** | CLI is the shipping `orun secrets` group; OC5 ships `set`/`list`/`rm`, orun-secrets extends it; `rm` aliases `revoke` | One CLI surface on main; the fuller command set is additive to what OC5 ships |

## 11. Invariants (regression-tested)

1. **No value in content.** No `secret://`-resolved plaintext, and no ciphertext
   envelope, ever appears in any L0 object, ref, `plan.json`, or log blob.
   (`fsck`-style scanner asserts this.)
2. **Reference integrity.** Every `secret://` reference in a plan resolves to a
   declared secret, or the plan fails compile-time validation.
3. **Deny-by-default.** A resolve request with no matching `allow` rule is
   denied; any matching `deny` denies regardless of `allow`.
4. **Audit completeness.** Every resolve decision (allow or deny) writes an
   audit event with key name + version + decision id + reason — **never** the
   value.
5. **Redaction soundness.** For every resolved value `v`, no log blob contains
   `v` or its base64/url-encodings.
6. **Provenance.** A sealed `ExecutionRun` records the exact `{key, version,
   decisionId}` it resolved; re-reading the run never yields a value.
7. **Cryptoshred.** Destroying a namespace DEK renders all that namespace's
   ciphertext permanently unreadable; references become tombstones.
8. **Portability.** A `SecretPolicy` evaluated for the same GitHub ids + facts
   yields the same decision in any backend (no machine-local identity).
9. **Personal isolation.** A personal overlay value is resolvable only by its
   owning GitHub user id and only when the server-derived platform fact is
   `local-cli`; no CI or cloud-runner resolve path can return it.
10. **Materialization provenance.** Every value that leaves orun custody via a
    sync is recorded as `{key, version, target, entityRef, ts}`; the catalog
    can always answer which version a deployed entity holds, and rotation
    drift is detectable.

## 12. Alternatives considered

- **Ciphertext-in-repo (SOPS / sealed-secrets).** Rejected — secrets-as-content
  is unrevocable in an immutable, dedup'd graph (§4). Kept as the conceptual
  inspiration for "references are content, values are not."
- **A standalone secrets microservice.** Rejected — orun already provisions a
  Worker + D1 + R2 + DO backend (`cmd/orun/command_backend.go`); a second
  service duplicates auth, tenancy, and ops. Secrets are a projection, not a
  product.
- **A runtime SDK / agent (Vault agent, Doppler SDK) for deployed apps.**
  Rejected — it puts orun-api on the request path of every customer
  application, creates a new availability/latency dependency, and requires
  per-language SDK maintenance. Materialization (SD-13) delivers the same
  outcome through the deploy path orun already owns, with provenance.
- **Static per-config tokens (pure Doppler).** Rejected as the *primary* model —
  orun already has richer caller identity (OIDC actor, sessions). Tokens remain
  available as the `ORUN_TOKEN` service-principal path for non-GitHub runners.
- **Bake values into the plan, encrypt the plan.** Rejected — couples secret
  rotation to plan identity and still puts ciphertext in the graph; violates
  SD-1.
- **Policy only in intent (not the Stack).** Rejected — not portable; the whole
  point is that a golden Stack carries its access rules with it (SD-5/SD-10).
- **Org-global secret namespace.** Rejected — ambient org-wide sharing makes
  blast radius the org. Repo default + explicit `_shared` groups (SD-12) keeps
  sharing deliberate and visible.
- **Arbitrary environment inheritance graphs.** Deferred — `base → env →
  personal` covers the dominant cases; free-form `extends` chains add
  resolution ambiguity (diamond shadowing) for marginal value.
- **RBAC roles instead of predicate policy.** The `multi-tenant-saas`
  policy-worker uses static role→permission maps; orun needs *condition-aware*
  grants (env, trigger, platform), so it generalizes to predicate rules — but
  reuses the deny-by-default + reason-code contract.
