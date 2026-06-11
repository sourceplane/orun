# Design

> orun gains a secret store shaped like Doppler but built for orun's invariants:
> values live encrypted in Orun Cloud and **never** enter the content-addressed
> graph; the plan carries only `secret://` references; access is a portable,
> GitHub-native `SecretPolicy` that ships with the Stack and is evaluated against
> who/which-component/which-environment/which-platform; the runner injects
> plaintext at step launch and redacts it from logs. This doc fixes the model,
> the decisions, the enforcement seam, inter-job passing, the invariants, and the
> alternatives. Schemas live in `data-model.md`; the policy engine in
> `policy-model.md`; delivery in `runner-integration.md`.

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
   logs" is only a coding-convention note
   (`specs/orun-object-model/_archive/implementation-plan.md:185`). No redaction
   exists.
3. **Policy is declared but inert.** Group/environment `policies` resolve onto
   `ComponentInstance.Policies` (`internal/expand/expander.go:350-375`) and
   `ProfilePolicies{RequireApproval,…}` is defined
   (`internal/model/composition.go:113-118`) — but **nothing reads either**. Only
   `DependencyMode` is enforced (`internal/planner/planner.go:447-460`). The docs
   claim policies are "enforced as platform constraints"
   (`website/docs/concepts/intent-model.md:217`); the code does not enforce them.
4. **No portable identity or access model.** orun authenticates with GitHub
   (OIDC + OAuth, `internal/remotestate/auth.go:23-208`) but has no notion of
   *which GitHub user/team may read which secret under which conditions*.
5. **The org wants one platform.** A medium SaaS company should manage its
   compositions, stacks, operations, **and** secrets on orun. The sibling
   `multi-tenant-saas` repo already ships an envelope-encrypted secret store
   (`apps/config-worker`) and a deny-by-default RBAC engine (`apps/policy-worker`)
   — orun should converge on those patterns, GitHub-native and policy-portable,
   not fork a second philosophy.

## 2. Goals / non-goals

**Goals**
- **G1 — Values never become content.** Secret material lives only in the Orun
  Cloud backend (D1, envelope-encrypted) and transiently in the runner's memory
  and the child process. The object graph, refs, plan, and logs carry only
  references and resolved-version provenance.
- **G2 — Doppler-grade ergonomics.** `orun secret set`, declarative references on
  components/compositions, automatic env injection at launch, versioning and
  rotation — the value is delivered to the workload without the author touching
  ciphertext.
- **G3 — Portable, GitHub-native policy.** A `SecretPolicy` document binds GitHub
  identities (stable numeric user ids + team slugs) to secret scopes under
  component/environment/trigger/platform conditions. It ships *in the Stack*, so a
  paved-road platform carries its own access rules across repos and orgs.
- **G4 — Real enforcement, two points.** Deny-by-default, enforced at compile time
  (grant validation, visible in `orun plan`) and at fetch time (the security
  boundary in `orun-api`). This is the seam that finally makes orun policy real.
- **G5 — Execution-platform awareness.** Policy can condition on *where the run
  executes* (local CLI, GitHub Actions OIDC, Orun Cloud runner) — derived from the
  auth mode orun already resolves (`internal/remotestate/auth.go:157-208`).
- **G6 — No-leak logs.** Every resolved value is redacted from the log stream
  before any blob is written.
- **G7 — One backend.** Reuse the Worker + D1 + R2 + Durable Objects that
  `orun backend init` already provisions; add tables and routes, not a service.

**Non-goals (v1)**
- Dynamic / leased secrets (Vault-style short-TTL generated credentials) — the
  reference model is designed to admit them later (§9).
- A full user-authored policy DSL — v1 locks an allowlisted predicate vocabulary;
  CEL is the named upgrade path (`policy-model.md` §6).
- External-provider *sync* (pull from AWS Secrets Manager / Cloudflare Secrets
  Store into orun) — the envelope `provider` field reserves the seam.
- Self-hosted-backend KEK custody hardening — see Q-2.

## 3. The model in one picture

```
AUTHOR TIME (in the repo / Stack — all content, all portable)
  Stack ─┬─ compositions/<type>/…           component contracts (existing)
         ├─ compositions/<type>/secretBindings   "this profile needs DATABASE_URL"   (NEW, portable)
         └─ policies/*.SecretPolicy          "who/where/which-platform may read"      (NEW, portable)
  intent.yaml / component.yaml               secretEnv: { DB_URL: "secret://…" }      (NEW, reference only)

PLAN TIME  (orun plan — references only, value-free, reviewable)
  expand → planner → render
  PlanJob.secretRefs = [ secret://ns/prod/DATABASE_URL@latest ]   ← NO value in plan.json
  compile-time grant check: every requested ref is permitted by SecretPolicy for (env,trigger,domain,component)

RUN TIME  (orun run — value lives only here)
  runner authenticates (OIDC | session | ORUN_TOKEN)  →  POST orun-api /v1/secrets/resolve
     orun-api:  policy decision (deny-by-default, four axes) → unwrap namespace DEK → AES-256-GCM decrypt
                → return plaintext over TLS  + write key-name-only audit event
  runner: inject plaintext as top, non-persisted env layer in stepExecContext   (runner.go:1320-1350)
          register every value in the per-run redactor                          (AfterStepLog, runner.go:579)
  seal:   ExecutionRun records resolved {key, version, decision-id} — provenance, never values

STORE  (Orun Cloud — the only place ciphertext lives)
  D1:  secret_metadata, secret_versions(ciphertext_envelope), secret_policies, gh_identity_map, secret_audit
  Cloudflare Secrets Store:  per-namespace DEK, wrapped by a master KEK
```

## 4. The carve-out: where values live (SD-1, SD-2)

**Decision (SD-1):** secret values live **only** in Orun Cloud (D1), encrypted.
The repo/plan/object-graph/logs carry **only** `secret://<namespace>/<env>/<KEY>[@<version>]`
references (grammar in `data-model.md` §1).

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
ids, GitHub **team slugs** (`org/team`), or special principals (`*authenticated`,
service principals). This makes a `SecretPolicy` portable: the same document means
the same thing in any repo or org, because GitHub ids are global.

**Three caller identities, all resolved to a GitHub-rooted subject:**

| Caller | How it authenticates today | Subject for policy |
|--------|----------------------------|--------------------|
| Human (CLI / dashboard) | GitHub OAuth session (`internal/cliauth`) | GitHub numeric user id + resolved team memberships |
| CI runner (GitHub Actions) | GitHub Actions **OIDC** (`internal/remotestate/auth.go:23-88`); the JWT carries `repository`, `actor`, `ref`, `workflow` | `actor` → GitHub user id; `repository` → namespace; claims become policy facts |
| Orun Cloud runner / service | `ORUN_TOKEN` / service-principal binding | service principal id (mapped to GitHub App installation) |

**The GitHub identity map (SD-4).** Orun Cloud installs as a **GitHub App** on a
customer org. On install, it ingests the org's users (→ numeric ids) and teams (→
slugs + membership) into a `gh_identity_map` table in D1 (`data-model.md` §6). The
map is a **cache/projection** of GitHub, refreshed on membership webhooks — never
the source of truth. A `SecretPolicy` that grants `@acme/platform-admins` resolves,
at decision time, to the current membership of that GitHub team. This is exactly
the `multi-tenant-saas` membership→policy pattern (`apps/policy-worker`,
`packages/policy-engine/src/index.ts`), narrowed to GitHub identities.

> **Consequence:** a customer's GitHub org *is* their orun access model. No second
> user directory, no invite flow, no per-platform identity — install the App and
> your existing teams govern your secrets.

## 6. Policy: portable, ships with the Stack, enforced twice (SD-5, SD-6)

This is the heart of the user-facing design and the answer to "policy should be
portable and close to the compositions." Full mechanics in `policy-model.md`.

### 6.1 Two layers, cleanly separated (orun's house separation of concerns)

orun already separates *composition owns the contract* from *intent/platform owns
the policy* (`context-for-ai/00-orun-repo-philosophy.md:20-31`). Secrets follow
that line exactly:

- **Requirement (composition layer, portable in the Stack).** A composition
  declares, per execution profile, the **logical secrets it needs**:
  `secretBindings` on a `JobTemplate`/`ExecutionProfile`
  (`internal/model/composition.go:106-124`). Example: the `terraform` composition's
  `release` profile binds `secretEnv: { AWS_ROLE_ARN: required, TF_API_TOKEN:
  required }`. This is **component-aware and portable**: ship the Stack over OCI
  and every consumer inherits the requirement.
- **Grant (policy layer, portable in the Stack *or* intent).** A `SecretPolicy`
  document says **who, where, and from which platform** a run may obtain a secret
  scope. It can live in the Stack's `policies/` directory (so the paved road
  carries its own access rules) **or** be layered/overridden at `intent.yaml` for
  org-specific tightening.

**Why ship policy in the Stack?** Because the Stack is already the portable,
versioned, OCI-distributable unit of platform truth
(`website/docs/concepts/stacks.md`). A platform team publishes "the golden Stack"
once; every product repo that adopts it gets the compositions **and** the secret
policy that protects them, versioned together. This is the user's insight made
concrete: policy travels *with* the compositions, so it is portable and lives
where the platform is defined — not scattered per-repo.

### 6.2 The four axes (Doppler-plus)

A `SecretPolicy` rule is `match → effect(allow|deny)` evaluated against four axes
orun already owns:

| Axis | Source | Example condition |
|------|--------|-------------------|
| **Who** (user-aware) | GitHub user id / team (§5) | `subject.team == "@acme/platform-admins"` |
| **What** (component-aware) | `ComponentInstance{type,domain,name}` (`internal/model/intent.go:309`) | `component.type == "terraform" && component.domain == "payments"` |
| **Where** (environment/trigger-aware) | `Environment` + `TriggerOccurrence{event,branch,declared}` (`internal/triggerctx/context.go:61-76`) | `env == "prod" && trigger.declared && trigger.branch == "main"` |
| **How** (execution-platform-aware) | resolved auth mode (`internal/remotestate/auth.go:157-208`) | `platform == "github-actions-oidc"` (deny `local-cli` for prod) |

Deny-by-default; explicit `deny` beats `allow`; most-specific scope wins. This is
strictly more expressive than Doppler's token-per-config model while remaining a
small, auditable predicate set (`policy-model.md` §6).

### 6.3 Enforced at two points (SD-6)

- **Compile time (`orun plan`).** The planner checks that every `secret://`
  reference a component requests is *permitted in principle* for its
  (environment, trigger, domain, component) under the resolved policy, and surfaces
  it in the plan: `job deploy → secrets: [DATABASE_URL@prod, …]` with **no values**.
  This satisfies the binding orun rule — *behavior must be visible in `orun plan`*
  (`context-for-ai/00-orun-repo-philosophy.md:90`) — and gives fail-fast errors
  before a run starts. Static facts only (subject may be unknown at plan time).
- **Fetch time (`orun-api`).** The authoritative decision: the runner presents its
  token + the reference + the trigger context; `orun-api` evaluates the full
  four-axis policy (subject now known), and returns plaintext or a typed denial
  with a stable reason code (matching the `policy-worker` contract). **This is the
  security boundary; compile-time is a UX/guardrail courtesy.**

## 7. Delivery and redaction (SD-3, SD-8)

Full detail in `runner-integration.md`.

- **Injection (SD-3).** The runner resolves references to plaintext in
  `stepExecContext` (`internal/runner/runner.go:1320-1350`) and merges them as the
  **highest-precedence, non-persisted** env layer for the child process only.
  Plaintext never reaches `PlanJob.Env`, the plan, refs, or any L0 object.
- **Write-only API + break-glass reveal (SD-3).** `orun secret set` accepts a
  value and returns metadata; there is **no routine reveal**. A single audited
  `orun secret reveal --break-glass` exists for incident recovery, gated by an
  elevated policy action and always emitting an alert event. This mirrors
  `config-worker`'s deliberate "prefer write-only" stance
  (`multi-tenant-saas/specs/components/07-config-secrets-flags.md:74-76`) while
  acknowledging that a *workload* (not a human) is the normal reader.
- **Redaction (SD-8).** All step output funnels through one hook (`AfterStepLog`,
  `internal/runner/runner.go:579` → `internal/objrun/objrun.go:148-155`). A per-run
  redactor, seeded with every resolved value (and common encodings: raw, base64,
  URL-encoded), replaces matches with `***` **before** the log blob is written.
  Single, well-contained interception point.

## 8. Inter-job value passing (SD-9)

Distinct from secrets but shares machinery (both are runtime-produced values a
later job needs). Today only within-job `$ORUN_ENV` exists
(`website/docs/concepts/runtime-environment.md:210-236`); `dependsOn` is ordering
only (`internal/planner/planner.go:405-470`).

**Decision (SD-9):** declared job **outputs**, carried on explicit `dependsOn`
edges so the dataflow is visible in `orun plan` (honoring the no-hidden-coupling
rule):
- A job declares `outputs: { image: "$ORUN_OUTPUT" }`; the runner captures them at
  job seal into the **already-reserved `artifacts/` slot** of the execution tree
  (`specs/orun-object-model/design.md:83`).
- A downstream job references `${{ jobs.build.outputs.image }}`.
- **Non-sensitive** outputs are stored as graph **content** (provenance,
  reproducibility — the orun way).
- **Sensitive** outputs (`sensitive: true`) are stored **encrypted via the secret
  backend** with only a run-scoped reference in the graph; their DEK is destroyed
  when the run is GC'd. They are redacted like any secret.

This gives orun GitHub-Actions-grade job outputs *and* a safe path for passing a
generated credential (e.g., a short-lived token minted in job A) to job B without
it ever landing in content.

## 9. Decisions register

| # | Decision | Rationale |
|---|----------|-----------|
| **SD-1** | Values live in Orun Cloud only; graph carries `secret://` references | Content addressing is immutable + dedup'd; secrets must be mutable/revocable (§4) |
| **SD-2** | Envelope encryption; per-namespace DEK; KEK in Cloudflare Secrets Store | Reuses shipped `config-worker` pattern; enables cryptoshred + KMS migration |
| **SD-3** | Write-only API; injection-only delivery; single audited break-glass reveal | A workload is the normal reader; humans should never routinely see values |
| **SD-4** | GitHub numeric user id is the portable identity key; teams via GH App map | Portable across repos/orgs; a customer's GitHub org *is* their access model |
| **SD-5** | Policy is a portable `SecretPolicy` document shipped in the Stack | Travels with the compositions it protects; versioned, OCI-distributable |
| **SD-6** | Deny-by-default; enforced at compile (visible) **and** fetch (authoritative) | Plan visibility honors orun philosophy; fetch is the real boundary |
| **SD-7** | Locked allowlisted predicate vocabulary in v1; CEL is the upgrade path | Matches the scorecards precedent; auditable, deterministic, extensible |
| **SD-8** | Resolved values redacted from logs before any blob write | Closes the second leak vector at the single capture hook |
| **SD-9** | Inter-job passing = declared outputs on `dependsOn`; sensitive → references | Visible dataflow; safe credential hand-off; uses the reserved `artifacts/` slot |

## 10. Invariants (regression-tested)

1. **No value in content.** No `secret://`-resolved plaintext, and no ciphertext
   envelope, ever appears in any L0 object, ref, `plan.json`, or log blob.
   (`fsck`-style scanner asserts this.)
2. **Reference integrity.** Every `secret://` reference in a plan resolves to a
   declared secret, or the plan fails compile-time validation.
3. **Deny-by-default.** A resolve request with no matching `allow` rule is denied;
   any matching `deny` denies regardless of `allow`.
4. **Audit completeness.** Every resolve decision (allow or deny) writes an audit
   event with key name + version + decision id + reason — **never** the value.
5. **Redaction soundness.** For every resolved value `v`, no log blob contains `v`
   or its base64/url-encodings.
6. **Provenance.** A sealed `ExecutionRun` records the exact `{key, version,
   decisionId}` it resolved; re-reading the run never yields a value.
7. **Cryptoshred.** Destroying a namespace DEK renders all that namespace's
   ciphertext permanently unreadable; references become tombstones.
8. **Portability.** A `SecretPolicy` evaluated for the same GitHub ids + facts
   yields the same decision in any backend (no machine-local identity).

## 11. Alternatives considered

- **Ciphertext-in-repo (SOPS / sealed-secrets).** Rejected — secrets-as-content
  is unrevocable in an immutable, dedup'd graph (§4). Kept as the conceptual
  inspiration for "references are content, values are not."
- **A standalone secrets microservice.** Rejected — orun already provisions a
  Worker + D1 + R2 + DO backend (`cmd/orun/command_backend.go`); a second service
  duplicates auth, tenancy, and ops. Secrets are a projection, not a product.
- **Static per-config tokens (pure Doppler).** Rejected as the *primary* model —
  orun already has richer caller identity (OIDC actor, sessions). Tokens remain
  available as the `ORUN_TOKEN` service-principal path for non-GitHub runners.
- **Bake values into the plan, encrypt the plan.** Rejected — couples secret
  rotation to plan identity and still puts ciphertext in the graph; violates SD-1.
- **Policy only in intent (not the Stack).** Rejected — that is not portable; the
  whole point is that a golden Stack carries its access rules with it (SD-5).
- **RBAC roles instead of predicate policy.** The `multi-tenant-saas` policy-worker
  uses static role→permission maps; orun needs *condition-aware* grants (env,
  trigger, platform), so it generalizes to predicate rules — but reuses the
  deny-by-default + reason-code contract.
