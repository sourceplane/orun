# Policy model — the portable, GitHub-native `SecretPolicy`

> `SecretPolicy` is a versioned, portable document (apiVersion `orun.io/v1`) that
> binds GitHub identities to secret scopes under conditions on four axes — who,
> which component, where (environment/trigger), and how (execution platform). It
> ships in the Stack alongside the compositions it protects, is deny-by-default,
> and is enforced at compile time (visible in `orun plan`) and fetch time
> (authoritative in `orun-api`). This doc fixes the subject model, the condition
> vocabulary, evaluation, and provenance. The document schema is in
> `data-model.md` §4.

## 1. Why a document, and why in the Stack

orun's portable unit of platform truth is the **Stack** — composition types
packaged with metadata and distributed over a directory, archive, or OCI ref
(`website/docs/concepts/stacks.md`). A platform team authors a golden Stack once
and publishes it; consumers adopt it across many repos and orgs.

`SecretPolicy` rides that same rail. A Stack may carry a `policies/` directory:

```text
acme-platform/                      ← published once, adopted everywhere
├── stack.yaml
├── compositions/
│   ├── terraform/…                 ← contracts (existing)
│   └── cloudflare-worker/…
└── policies/
    ├── prod-secrets.SecretPolicy.yaml      ← who/where/how may read prod secrets
    └── pr-secrets.SecretPolicy.yaml        ← what a PR run may read (almost nothing)
```

Adopting the Stack pulls in **both** the compositions and their access rules,
versioned together. A repo's `intent.yaml` may *additionally* layer org-specific
`SecretPolicy` documents (tightening only — see §5 precedence). This is the
concrete form of the design goal: **policy is portable and lives next to the
compositions it governs.**

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
portability invariant (Invariant 8, `design.md` §10).

## 3. Scope — what a rule targets

A rule targets a **secret scope**, matching the `secret://` reference grammar
(`data-model.md` §1) with wildcards:

```
namespace : <gh-org-or-repo>          # e.g. acme/api  (the orun namespace)
env       : prod | staging | * | {list}
key       : DATABASE_URL | STRIPE_* | *
```

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
  rules ← all SecretPolicy rules in scope, ordered: Stack base → intent overlays
  applicable ← { r ∈ rules | scopeMatches(r, request.ref) ∧ conditionsMatch(r, request.facts) }
  if ∃ r ∈ applicable with effect=deny            → DENY  (reason: r.id)
  else if ∃ r ∈ applicable with effect=allow      → ALLOW (grant: most-specific r)
  else                                            → DENY  (reason: no-matching-grant)   # deny-by-default
```

- **Deny-by-default** (SD-6), matching the `multi-tenant-saas` constitution
  (`apps/policy-worker`, `specs/components/03-policy-authorization.md`).
- **Explicit deny wins** over allow at the same or broader specificity.
- **Overlay precedence:** intent overlays may only *narrow* a Stack grant (add
  conditions / deny), never *widen* it — so adopting a Stack cannot be loosened by
  a downstream repo without the platform team's grant. (Enforced by rejecting an
  intent `allow` whose scope/conditions are broader than any Stack `allow`.)
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

## 7. Compile-time vs fetch-time (the two enforcement points)

| | Compile time (`orun plan`) | Fetch time (`orun-api /v1/secrets/resolve`) |
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
reason, subjectId, key, version, ts}` written to `secret_audit` (D1) and emitted
as a `secret.resolved` / `secret.denied` event (key-name-only payload, like
`config-worker`'s `secrets.updated`, `apps/config-worker/src/handlers/create-secret.ts`).
The sealed `ExecutionRun` records the `decisionId` + `{key, version}` (never the
value), so operations can answer "what did prod deploy #4821 read, and under which
rule?" directly from the object graph — closing the loop with Orun Cloud
operations/audit.

## 9. Worked example

`acme-platform` Stack ships `prod-secrets.SecretPolicy.yaml`:

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
