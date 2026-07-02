# Policy model (v3) — two layers; the portable `SecretPolicy` conditions

> Access to secrets is decided in **two layers**. **Layer 1** is the platform's
> shipped role×scope RBAC (`packages/policy-engine`): deny-by-default role
> matrices with account cascade and per-action provenance, whose dormant
> `secret.read` / `secret.write` / `secret.value.use` actions v3 activates.
> **Layer 2** is `SecretPolicy` — a versioned, portable document (apiVersion
> `orun.io/v1`) of conditions over four axes: who, which component, where
> (environment/trigger), and how (execution platform). It attaches at three
> tiers — composition-attached, stack-wide, intent overlay — each lower tier
> narrow-only; it is deny-by-default for protected scopes and enforced at
> compile time (visible in `orun plan`) and fetch time (authoritative). The
> document schema is in `data-model.md` §4.

## 0. Layer 1 — the shipped engine (what v3 activates, not builds)

`packages/policy-engine/src/index.ts` already ships everything the coarse gate
needs:

- **Actions** `secret.read`, `secret.write`, `secret.value.use` exist in the
  catalog (`index.ts:334-336`) and in the role matrices: org `owner`/`admin`
  hold all three; `builder` holds `secret.read` + `secret.value.use`; `viewer`
  holds `secret.read` (`index.ts:67-178`). **No route consumes them today** —
  secret handlers authorize with `*.config.read/write`
  (`create-secret.ts:161`).
- **Account cascade (WID6):** membership-worker assembles authorization facts,
  remapping account-scope grants onto child workspaces with
  `grantedVia:{kind:"account_cascade"}` (`authz-facts.ts:40-94`); team-derived
  grants fold in the same way.
- **Provenance (TM6b1):** decisions report the permitting fact
  (`via: direct | team | account_cascade`).

v3 changes exactly two things at this layer: the secret routes switch to their
own actions (SEC1), and one **new elevated action** `secret.reveal` is added
for break-glass (owner/admin only — never builder). Layer 1 answers: *may this
principal, on this scope, perform this class of operation at all?* Everything
finer is Layer 2.

## 1. Three placement tiers — policy lives next to what it governs (SD-10)

Unchanged from v2. orun's portable unit of platform truth is the **Stack**;
`SecretPolicy` rides it at three tiers:

```text
acme-platform/                              ← published once, adopted everywhere
├── compositions/
│   └── terraform/
│       ├── composition.yaml                ← contract (existing)
│       ├── profiles/…                      ← secretBindings + materialize
│       └── secret-policy.yaml              ← TIER 1: composition-attached defaults
│                                              (auto-scoped: component.type == "terraform")
└── policies/
    └── prod-secrets.SecretPolicy.yaml      ← TIER 2: stack-wide rules

acme-api/  (an adopting repo)
└── intent.yaml / policies/                 ← TIER 3: intent overlays — tightening only
```

The tier loader lives in orun (compile-time); `orun plan`/`orun publish` push
the resolved, tier-tagged documents to the backend
(`config.secret_policies`, `data-model.md` §7d) so fetch-time evaluation uses
exactly the rules the plan displayed.

## 2. Subjects — platform principals, portable (SD-4′)

A subject is one of:

| Subject form | Written as | Resolves to |
|--------------|-----------|-------------|
| Platform user | `user:<subjectId>` | one identity (session / CLI JWT) |
| Team | `team:<slug>` | current team membership from membership facts (account cascade included) |
| Service principal | `service_principal:<id>` | an `sk_` API-key identity |
| Actor kind | `workflow` \| `user` \| `service_principal` | any actor of that kind (e.g. any CI-OIDC workflow bound to this project) |
| Any authenticated caller | `*authenticated` | any valid token |

**Canonicalization (portability invariant).** Team slugs resolve to current
membership **at decision time** via membership-worker's authorization facts —
the same facts Layer 1 consumes, so the two layers can never disagree about who
is on a team. A document authored against `team:platform-admins` means "whoever
is in that team *now*"; the same document yields the same decision in any
backend with the same membership (Invariant 8).

> **What happened to `gh:user:<numeric id>`?** v2 keyed subjects on GitHub
> numeric ids and required a new `gh_identity_map` subsystem. v3 keys on the
> principals every enforcement point already trusts (`resolve-bearer.ts`:
> user / service_principal / workflow). GitHub *team sync* into platform teams
> is a later integrations-worker feature — it enriches `team:` subjects without
> changing this model. CI workloads are addressed by the `workflow` kind plus
> **trigger facts** (§4.3), which carry the OIDC claims (`repository`, `ref`)
> that v2's `gh:oidc:` subject form encoded.

## 3. Scope — what a rule targets

A rule targets `{env, key}` with globs; most-specific-wins:

```
env : prod | staging | * | {list}
key : DATABASE_URL | STRIPE_* | *
```

The **tenancy** scope of a document is not authored inside it — it comes from
where the document is pushed (project-scoped or workspace-wide,
`data-model.md` §7d) and, for tier-1 fragments, from the injected
`component.type`. v2's `namespace` scope field and its `_shared/` boundary
rules are gone with the namespace concept itself: chain rungs above project
scope (workspace/account) are governed by rule conditions (e.g.
`servesFrom == "workspace"`, §4.3) and by write-side guardrails
(`overridable:false`, SD-12′), not by a parallel naming scheme.

## 4. Conditions — the four axes

Each rule may constrain any of four axes (absent = unconstrained). Facts come
from data the platform already computes:

### 4.1 Who (user-aware)
`subject.id`, `subject.teams[]`, `subject.kind ∈ {user, service_principal,
workflow}`. Source: the verified ActorContext (api-edge → worker headers).

### 4.2 What (component-aware)
`component.type`, `component.domain`, `component.name`, `component.labels{}`.
Source: `ComponentInstance` (`internal/model/intent.go:329-358`) — carried into
the resolve request as run facts. A rule can grant `STRIPE_KEY` only to
`component.type == "billing-worker"`.

### 4.3 Where (environment + trigger-aware)
`env`, `servesFrom ∈ {environment, project, workspace, account}` (which chain
rung serves the key), `trigger.event`, `trigger.action`, `trigger.branch`,
`trigger.baseBranch`, `trigger.tag`, `trigger.declared`, `trigger.actor`,
`trigger.repository` (from OIDC claims for workflow actors).
Source: `TriggerOccurrence` (`internal/triggerctx/context.go:61-76`),
`NormalizedEvent` (`internal/model/trigger.go:38-52`), the in-plan
`PlanTrigger` echo, and — for workflow actors — the OIDC claims the exchange
verified (`oidc/github.ts:13-30`). Lets you express "prod DB creds only on a
declared push to `main`, never on a PR trigger" or "this env never serves keys
inherited from account scope."

### 4.4 How (execution-platform-aware)
`platform ∈ {local-cli, ci-oidc, service}`. **Server-derived from the verified
actor kind** — CLI-session user ⇒ `local-cli`, workflow (OIDC-exchanged) ⇒
`ci-oidc`, `sk_` service principal ⇒ `service` — never self-reported
(`resolve-bearer.ts:19-70`; risk R-3). Lets you express "prod secrets are
unreadable from a developer laptop."

## 5. Evaluation

```
decision(request) :=
  L1 ← policy-engine authorize(subject facts, action, resource scope)      # role×scope, deny-by-default
  if !L1.allow                                   → DENY (reason: L1.reason)
  rules ← SecretPolicy rules in scope,
          ordered: composition-attached → stack policies/ → intent overlays  (SD-10)
  applicable ← { r ∈ rules | scopeMatches(r, request.ref) ∧ conditionsMatch(r, request.facts) }
  if ∃ r ∈ applicable with effect=deny           → DENY  (reason: r.id)
  else if ∃ r ∈ applicable with effect=allow     → ALLOW (grant: most-specific r)
  else if protected(request.env)                 → DENY  (reason: no-matching-grant)
  else                                           → ALLOW (reason: rbac-only)   # unprotected env, L1 sufficed
```

- **Layer 1 always gates first**; Layer 2 refines. An environment becomes
  **protected** the moment any `SecretPolicy` rule targets it — from then on
  access to it is deny-by-default at Layer 2 too. Environments no policy
  targets are governed by Layer 1 alone, so a fresh workspace works
  out-of-the-box (Doppler-grade onboarding) and hardens declaratively.
- **Explicit deny wins** over allow at any specificity.
- **Narrow-only downward (SD-10):** a composition-attached fragment is
  force-scoped to `component.type == "<its composition>"` at load (structurally
  cannot widen); an intent `allow` broader than any Stack/composition `allow`
  is rejected at load; intent `deny` rules are always accepted.
  `orun policy lint` enforces both statically.
- **Personal overlays (SD-11′):** a resolve that would serve a *personal* value
  additionally requires `subject.id == personal_owner` and
  `platform == "local-cli"` — built-in facts, not authorable rules; no policy
  can route a personal value to CI (Invariant 9).
- **Determinism:** every decision is a pure function of (rules, facts); the
  only lookup is the membership-facts snapshot taken at decision time.

**Where each layer runs:** Layer 1 in policy-worker (as today, via the
membership-facts + authorize round-trip); Layer 2 in config-worker at the
resolve/reveal handlers, as a pure library over the pushed documents —
unit-testable, no cross-worker hop, with the door open to relocate once a
second consumer of condition-policies exists (`design.md` §12).

## 6. The predicate vocabulary (SD-7) and the CEL upgrade path

Unchanged from v2 (scorecards precedent): v1 conditions are a small, locked,
typed predicate set — equals, `in`, glob `matches`, bool, team-membership,
platform — AND-of-predicates within a rule, OR via multiple rules. Auditable,
statically checkable at `orun plan`, safe to evaluate in a Worker. **CEL** is
the named upgrade path behind the reserved `expr` field and a capability flag.

> **`SecretPolicy` is the engine behind the contract's `secret.value.use`
> action (SD-15′).** The state-api contract's policy map names the action;
> Layer 1 grants the class; the four-axis evaluation in this document decides
> the instance. One decision id covers both layers (SD-17).

## 7. Compile-time vs fetch-time (the two enforcement points)

| | Compile time (`orun plan`) | Fetch time (`…/runs/{runId}/secrets/resolve`) |
|---|---|---|
| Facts available | component, env, trigger, platform-intent; **subject often unknown** | **all four axes**; subject from the verified token; lease verified |
| Question | "is this reference *grantable in principle* here?" | "is *this caller* allowed *now*?" |
| Failure | plan error / warning, in `--json` | typed denial + stable reason code; audited |
| Purpose | visibility + fail-fast (orun philosophy) | the security boundary |

Compile-time evaluates with `subject = *any-permitted` (existential) and
annotates each ref in the plan (`grant`, `servesFrom`, `personalShadow` —
`data-model.md` §5). Fetch-time is authoritative for the concrete caller.

## 8. Decision provenance

Every fetch-time decision produces
`SecretDecision{decisionId, allow, layer, ruleId|via, reason, subjectId, key,
version, ts}` — `via` is Layer 1's permitting-fact provenance (TM6b1), `ruleId`
is Layer 2's matched rule. An allow emits **`secret.accessed`** per key; a
denial emits **`secret.denied`** with the reason code — both through the
shipped `appendEventWithAudit` (`events/repository.ts:188`), key-name-only,
never the value. The sealed `ExecutionRun` records `{key, version, decisionId}`
so operations can answer "what did prod deploy #4821 read, and under which
rule?" from the object graph.

## 9. Worked example

Tier 1 — the `terraform` composition ships its defaults
(`component.type == "terraform"` injected automatically):

```yaml
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: terraform-defaults }
spec:
  rules:
    - id: release-bindings-ci-main
      effect: allow
      subjects: ["workflow"]
      scope: { env: "*", key: "AWS_ROLE_ARN" }
      when:
        - trigger.declared
        - trigger.branch == "main"
        - platform == "ci-oidc"
```

Tier 2 — the `acme-platform` Stack ships `prod-secrets.SecretPolicy.yaml`:

```yaml
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: prod-secrets }
spec:
  rules:
    - id: admins-prod-from-ci
      effect: allow
      subjects: ["team:platform-admins"]
      scope: { env: prod, key: "*" }
      when:
        - platform in ["ci-oidc", "service"]
    - id: billing-stripe-main-deploys
      effect: allow
      subjects: ["*authenticated"]
      scope: { env: prod, key: "STRIPE_*" }
      when:
        - component.type == "billing-worker"
        - trigger.declared
        - trigger.branch == "main"
    - id: laptops-never-prod
      effect: deny
      subjects: ["*authenticated"]
      scope: { env: prod, key: "*" }
      when:
        - platform == "local-cli"
```

A PR run by a non-admin on `feature/x` requesting
`secret://acme/api/prod/STRIPE_KEY`: Layer 1 passes (builder holds
`secret.value.use`), but `prod` is protected and no Layer-2 `allow` matches
(`trigger.branch != main`, not declared) → **deny by default**. The same
component on a declared `push` to `main` from CI → **allow**
(`billing-stripe-main-deploys`). A platform admin on their laptop → **deny**
(`laptops-never-prod` — deny wins over `admins-prod-from-ci`). Meanwhile the
same admin resolving `secret://acme/api/dev/DB_URL` — an env no policy targets
— gets **allow (rbac-only)**, personal overlay served if one exists. All
outcomes are deterministic, audited with layer + rule provenance, and portable
to any workspace that adopts the Stack.
