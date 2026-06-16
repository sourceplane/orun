# Policy model — the portable, GitHub-native `SecretPolicy`

> `SecretPolicy` is a versioned, portable document (apiVersion `orun.io/v1`) that
> binds GitHub identities to secret scopes under conditions on four axes — who,
> which component, where (environment/trigger), and how (execution platform). It
> attaches at three tiers — composition-attached, stack-wide, intent overlay —
> each lower tier narrow-only; it is deny-by-default and enforced at compile
> time (visible in `orun plan`) and fetch time (authoritative in `orun-api`).
> This doc fixes the placement tiers, the subject model, the condition
> vocabulary, evaluation, and provenance. The document schema is in
> `data-model.md` §4.

## 1. Three placement tiers — policy lives next to what it governs (SD-10)

orun's portable unit of platform truth is the **Stack** — composition types
packaged with metadata and distributed over a directory, archive, or OCI ref
(`website/docs/concepts/stacks.md`, layout in
`internal/model/composition.go:201-238`). A platform team authors a golden
Stack once and publishes it; consumers adopt it across many repos and orgs.
`SecretPolicy` rides that same rail, at three tiers:

```text
acme-platform/                      ← published once, adopted everywhere
├── stack.yaml
├── compositions/
│   ├── terraform/
│   │   ├── composition.yaml                ← contract (existing)
│   │   ├── profiles/…                      ← secretBindings + materialize (data-model.md §2)
│   │   └── secret-policy.yaml              ← TIER 1: composition-attached defaults
│   │                                          auto-scoped to component.type == "terraform";
│   │                                          CANNOT grant beyond its own composition
│   └── cloudflare-worker/…
└── policies/
    ├── prod-secrets.SecretPolicy.yaml      ← TIER 2: stack-wide rules
    └── pr-secrets.SecretPolicy.yaml           (prod lockdown, laptop denial, shared groups)

acme-api/  (an adopting repo)
└── intent.yaml / policies/                 ← TIER 3: intent overlays — tightening only
```

- **Tier 1 — composition-attached.** The composition author ships sane access
  defaults *with the composition*: "the terraform `release` profile may read
  `AWS_*` only on a declared push to `main` from CI OIDC." The fragment is
  constitutionally scoped — the loader injects
  `component.type == "<composition>"` into every rule — so a composition can
  never grant outside itself. This is "policy closer to the compositions" made
  literal: the golden path carries its own access rules in the same directory
  as its contract.
- **Tier 2 — stack-wide.** Cross-cutting rules in `policies/`: environment
  lockdowns, platform denials, `_shared/<group>` grants.
- **Tier 3 — intent overlay.** The adopting repo may add conditions or denials,
  never widen (see §5).

Adopting the Stack pulls in the compositions, their attached defaults, and the
platform rules, versioned together over OCI. This is the concrete form of the
design goal: **policy is portable and lives next to the compositions it
governs.**

## 2. Subjects — GitHub-native, portable (ties to `design.md` §5)

A subject is one of:

| Subject form | Written as | Resolves to |
|--------------|-----------|-------------|
| GitHub user (by **numeric id**, portable) | `gh:user:583231` | one identity |
| GitHub user (by login, convenience) | `gh:user:@octocat` | numeric id via the GH map (login is sugar; id is canonical) |
| GitHub team | `gh:team:@acme/platform-admins` | current team membership (live from the GH map) |
| Any authenticated caller | `*authenticated` | any valid token |
| Service principal | `sp:<id>` | an Orun Cloud machine identity (GH App installation) |
| CI workload by OIDC claim | `gh:oidc:repo=acme/api,ref=refs/heads/main` | a GitHub Actions run matching the claim |

**Canonicalization (portability invariant).** Logins and team slugs are resolved
to **stable numeric GitHub ids** at decision time via the `gh_identity_map`
(`data-model.md` §6), which Orun Cloud populates from the GitHub App installation
and refreshes on `membership`/`team` webhooks. A policy authored against
`@acme/platform-admins` therefore means "whoever is in that team *now*", and the
same document yields the same decision in any backend that has the map — the
portability invariant (Invariant 8, `design.md` §11).

## 3. Scope — what a rule targets

A rule targets a **secret scope**, matching the `secret://` reference grammar
(`data-model.md` §1) with wildcards:

```
namespace : <org>/<repo> | <org>/_shared/<group>   # repo default; shared groups explicit (SD-12)
env       : prod | staging | base | * | {list}
key       : DATABASE_URL | STRIPE_* | *
```

A `*` in `namespace` never matches across the `_shared/` boundary: granting
`acme/*` grants repo namespaces only; a shared group must be named
(`acme/_shared/observability`).

Scopes compose most-specific-wins, mirroring orun's existing parameter/env merge
precedence (`internal/expand/expander.go:182-187`) so it feels native to authors.

## 4. Conditions — the four axes

Each rule may constrain any of four axes (all optional; absent = unconstrained).
Facts come from data orun already computes:

### 4.1 Who (user-aware)
`subject.id`, `subject.teams[]`, `subject.kind ∈ {user, service, oidc}`.
Source: resolved caller identity (`design.md` §5).

### 4.2 What (component-aware)
`component.type`, `component.domain`, `component.name`, `component.labels{}`.
Source: `ComponentInstance` (`internal/model/intent.go:309`) — the same expanded
instance the planner uses. **This is what makes the policy component-aware:** a
rule can grant `STRIPE_KEY` only to `component.type == "billing-worker"`.

### 4.3 Where (environment + trigger-aware)
`env`, `trigger.event`, `trigger.action`, `trigger.branch`, `trigger.baseBranch`,
`trigger.tag`, `trigger.declared` (declared vs system), `trigger.actor`.
Source: `Environment` + `TriggerOccurrence` (`internal/triggerctx/context.go:61-76`,
`internal/model/trigger.go:38-52`). Lets you express "prod DB creds only on a
declared push to `main`, never on a PR trigger or a manual run."

### 4.4 How (execution-platform-aware)
`platform ∈ {local-cli, github-actions-oidc, orun-cloud-runner, service-token}`.
Source: the resolved auth mode (`internal/remotestate/auth.go:157-208` returns
`ResolvedMode ∈ {oidc, static, session}`) plus runner identity. Lets you express
"prod secrets are unreadable from a developer laptop (`local-cli`) — only from
CI OIDC or an Orun Cloud runner."

## 5. Evaluation

```
decision(request) :=
  rules ← all SecretPolicy rules in scope,
          ordered: composition-attached → stack policies/ → intent overlays   (SD-10)
  applicable ← { r ∈ rules | scopeMatches(r, request.ref) ∧ conditionsMatch(r, request.facts) }
  if ∃ r ∈ applicable with effect=deny            → DENY  (reason: r.id)
  else if ∃ r ∈ applicable with effect=allow      → ALLOW (grant: most-specific r)
  else                                            → DENY  (reason: no-matching-grant)   # deny-by-default
```

- **Deny-by-default** (SD-6), matching the `multi-tenant-saas` constitution
  (`apps/policy-worker`, `specs/components/03-policy-authorization.md`).
- **Explicit deny wins** over allow at the same or broader specificity.
- **Narrow-only downward (SD-10):**
  - A **composition-attached** fragment is force-scoped to
    `component.type == "<its composition>"` at load; it cannot reference other
    components, `_shared` groups outside its declared bindings, or `*` component
    scopes. (Structurally cannot widen.)
  - An **intent** `allow` whose scope/conditions are broader than any
    Stack-or-composition `allow` is rejected at load — adopting a Stack cannot
    be loosened by a downstream repo. Intent `deny` rules are always accepted.
  - `orun policy lint` enforces both statically (`cli-surface.md` §2).
- **Personal overlays (SD-11):** a resolve that would return a *personal* value
  additionally requires `subject.id == owner(personal config)` and
  `platform == "local-cli"` — evaluated as built-in facts, not authorable rules,
  so no policy can ever route a personal value to CI (Invariant 9).
- **Shared groups (SD-12):** a ref into `org/_shared/<group>` matches only rules
  whose scope names that group explicitly — there is no wildcard that crosses
  the `_shared` boundary by accident.
- **Determinism:** every decision is a pure function of (rules, facts); no I/O
  beyond the identity-map lookup, which is itself a snapshot at decision time.

## 6. The predicate vocabulary (SD-7) and the CEL upgrade path

Following the scorecards precedent (`specs/orun-scorecards/` locks an allowlisted
predicate vocabulary with **Google CEL** named as the upgrade path), v1 conditions
are a **small, locked, typed predicate set** — not a free DSL:

| Predicate | Form | Example |
|-----------|------|---------|
| equals | `field == "value"` | `env == "prod"` |
| in | `field in [a,b]` | `trigger.event in ["push","release"]` |
| glob | `field matches "glob"` | `key matches "STRIPE_*"` |
| bool | `field` / `!field` | `trigger.declared` |
| team-member | `subject in team "@org/team"` | `subject in team "@acme/platform-admins"` |
| platform | `platform == "github-actions-oidc"` | — |

Rules are AND-of-predicates within a rule; OR is expressed as multiple rules.
This is auditable, statically checkable at `orun plan`, and safe to evaluate in
the Worker. **CEL** is the named upgrade if customers need richer logic — the
`SecretPolicy` schema reserves `expr` for it, gated behind a capability flag.

> **`SecretPolicy` is the engine behind the contract's `secret.value.use`
> action (SD-15).** The orun-cloud wire contract (§6 policy map) already defines
> `secret.read` / `secret.write` / `secret.value.use` as **server-enforced,
> deny-by-default** actions. orun-secrets does not add a second authorization
> layer — the four-axis evaluation in this document *is* what the server runs to
> decide `secret.value.use` on the run-scoped resolve call, plus
> `secret.value.reveal` and `secret.policy.write` for the extension routes
> (`data-model.md` §8). The contract names the action; `SecretPolicy` decides it.

## 7. Compile-time vs fetch-time (the two enforcement points)

| | Compile time (`orun plan`) | Fetch time (`…/runs/{runId}/secrets/resolve`) |
|---|---|---|
| Facts available | component, env, trigger, platform-intent; **subject often unknown** | **all four axes**, subject from the presented token |
| Question answered | "is this reference *grantable in principle* here?" | "is *this caller* allowed *now*?" |
| Failure | plan error / warning, listed in `--json` | typed denial + reason code; audited |
| Purpose | visibility + fail-fast (orun philosophy) | the security boundary |

Compile-time evaluates with `subject = *any-permitted` (existential): it passes if
*some* subject could be granted, and renders the requirement in the plan
(`job deploy → secrets: [DATABASE_URL@prod]`, no values). Fetch-time is
universal/authoritative for the concrete caller.

## 8. Decision provenance

Every fetch-time decision produces a `SecretDecision{decisionId, allow, ruleId,
reason, subjectId, key, version, ts}` written to `secret_audit` (D1). An allow
emits the contract's **`secret.accessed`** event per key (the audit hook the
contract already specifies for the resolve route, `data-model.md` §8); a denial
emits `secret.denied` with the reason code. Both payloads are key-name-only
(never the value), matching `config-worker`'s metadata-only event stance.
The sealed `ExecutionRun` records the `decisionId` + `{key, version}` (never the
value), so operations can answer "what did prod deploy #4821 read, and under which
rule?" directly from the object graph — closing the loop with Orun Cloud
operations/audit.

## 9. Worked example

The `terraform` composition ships its own defaults (tier 1,
`compositions/terraform/secret-policy.yaml` — `component.type == "terraform"`
is injected automatically):

```yaml
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: terraform-defaults }
spec:
  rules:
    # The release profile's bindings are readable only on a declared main push from CI.
    - effect: allow
      subjects: ["*authenticated"]
      scope: { env: "*", key: "AWS_ROLE_ARN" }
      when:
        - trigger.declared
        - trigger.branch == "main"
        - platform == "github-actions-oidc"
```

The `acme-platform` Stack ships `prod-secrets.SecretPolicy.yaml` (tier 2):

```yaml
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: prod-secrets }
spec:
  rules:
    # Platform admins may read any prod secret, but only from CI or cloud runners.
    - effect: allow
      subjects: ["gh:team:@acme/platform-admins"]
      scope: { env: prod, key: "*" }
      when:
        - platform in ["github-actions-oidc", "orun-cloud-runner"]
    # The billing component may read STRIPE_* in prod on a declared main deploy.
    - effect: allow
      subjects: ["*authenticated"]
      scope: { env: prod, key: "STRIPE_*" }
      when:
        - component.type == "billing-worker"
        - trigger.declared
        - trigger.branch == "main"
    # Never expose prod secrets to a laptop.
    - effect: deny
      subjects: ["*authenticated"]
      scope: { env: prod, key: "*" }
      when:
        - platform == "local-cli"
```

A PR run by a non-admin on branch `feature/x` requesting `secret://acme/prod/STRIPE_KEY`:
no `allow` matches (`trigger.branch != main`, not declared) → **deny by default**.
The same component on a `push` to `main` from GitHub Actions OIDC → **allow** by the
second rule. A platform admin on their laptop → **deny** by the third rule even
though the first would otherwise allow (deny wins). All three outcomes are
deterministic, audited, and portable to any org that installs the GitHub App.
