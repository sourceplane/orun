# Platform integration (v3) — catalog, scorecards, operations, console

> How secrets surface across Orun Cloud so they feel like a pillar of the
> platform, not a vault bolted to its side: a derived facet on the Component
> entity (SD-14), scorecard checks over that facet, run/audit joins for
> operations, and the hosted console (`apps/web-console-next`). Delivery
> mechanics in `runner-integration.md`; schemas in `data-model.md`.

## 1. The catalog facet: `extensions.x-orun-secrets` (SD-14)

The service catalog gives every entity a typed extension seam
(`internal/catalogmodel/entity_envelope.go:36`; `x-*` blocks validated against
registered schemas via `internal/catalogext/registry.go:26-44`, preserved on
round-trip, content-addressed in the manifest hash). Secrets register
`x-orun-secrets` on the **Component** entity:

```jsonc
"extensions": {
  "x-orun-secrets": {
    "requirements": [                       // declared — from composition secretBindings (static)
      { "key": "DATABASE_URL", "profile": "worker-deploy", "required": true },
      { "key": "STRIPE_KEY",   "profile": "worker-deploy", "required": true }
    ],
    "bindings": [                           // live — joined against backend metadata
      { "key": "DATABASE_URL", "env": "prod", "status": "bound",   "version": 9,
        "servesFrom": "environment" },      // which chain rung serves it (SD-11′/SD-12′)
      { "key": "STRIPE_KEY",   "env": "prod", "status": "bound",   "version": 7,
        "servesFrom": "workspace" },
      { "key": "SENTRY_DSN",   "env": "prod", "status": "missing" }
    ],
    "rotation": [                           // live — version age from secret metadata
      { "key": "DATABASE_URL", "env": "prod", "rotatedAt": "2026-04-02", "ageDays": 91 }
    ],
    "syncs": [                              // live — from secret_syncs (SD-13)
      { "key": "DATABASE_URL", "env": "prod", "target": "cloudflare-worker",
        "entityRef": "Resource/worker-api-prod", "version": 9, "status": "synced" }
    ]
  }
}
```

Derivation follows the catalog's static/live split:

- **`requirements`** come from the repo + Stack at resolve time — the same
  composition documents the resolver already reads (declared truth,
  offline-computable).
- **`bindings` / `rotation` / `syncs`** are the **live plane**: the catalog
  resolver queries `GET …/config/secrets` (`?chain=true`) and
  `GET …/config/secrets/syncs` metadata (never values) and joins per
  `(project, env, key)`. Offline or unauthenticated, these report `unknown` —
  which scorecards render distinctly rather than as failures.
- v3 change vs v2: the `groups` list is replaced by **`servesFrom`** per
  binding — inheritance from workspace/account scope is visible per key, which
  is strictly more informative than group names.

The facet is a **derived projection** — rebuildable from compositions + backend
metadata, never a source of truth, and it never contains a value or ciphertext
(Invariant 1 applies to the catalog too).

**Provisioned entities get a facet too.** A materialization stamps the
*provisioned* entity with the inverse view — "this running Worker holds
`DATABASE_URL@9`, synced by run `01J…`" — so both ends of a sync are navigable.

## 2. Scorecard checks over the facet

Secret health is entity data, so it is gradeable with the existing scorecard
machinery — same locked predicate vocabulary, no new engine:

```yaml
# scorecards/secret-hygiene.yaml
apiVersion: orun.io/v1
kind: Scorecard
metadata: { name: secret-hygiene }
spec:
  appliesTo: { kind: Component }
  rules:
    - id: bindings-satisfied        # every required binding is bound
      expr: 'count(live.secrets.bindings[status == "missing"]) == 0'
      weight: 3
    - id: rotation-90d              # no prod secret older than 90 days
      expr: 'live.secrets.rotationWithin90d == true'
      weight: 2
    - id: syncs-current             # deployed entities hold the latest version
      expr: 'count(live.secrets.syncs[status == "superseded"]) == 0'
      weight: 2
    - id: locked-not-shadowed       # hygiene: no attempts to shadow locked account keys
      expr: 'live.secrets.guardrailViolations == 0'
      weight: 1
```

(As in the scorecards spec, v1 predicate forms lean on pre-computed booleans
emitted by the resolver — `rotationWithin90d`, `guardrailViolations` — with
date math proper arriving via the shared CEL upgrade path, SD-7.)

The product story for a platform team: **"silver requires secret-hygiene ≥
bindings-satisfied + rotation-90d"** — secret posture joins the same maturity
ladder as docs, ownership, and deployment health.

## 3. Operations: runs, rotation, drift

- **Run provenance.** A sealed `ExecutionRun` records `{key, version,
  decisionId}` per job (`runner-integration.md` §8). The console renders "this
  run read DATABASE_URL@9 under rule `admins-prod-from-ci`
  (via account_cascade)" — Layer 1 fact provenance and Layer 2 rule id in one
  line, no value anywhere.
- **Rotation as an operation.** `orun secrets rotate` appends the version and
  raises `onRotate` system triggers for every materializing deploy profile.
  The resulting deploys are ordinary, plan-visible runs; `secret_syncs` rows
  flip to `superseded` until the redeploy lands. Rotation status is
  observable: "rotated 14:02 — 3/4 targets converged".
- **Drift detection.** A sync row whose `entityRef` no longer exists goes
  `orphaned`; an entity holding a `superseded` version beyond a grace window
  surfaces in operations and fails `syncs-current`. orun does not read foreign
  stores to verify contents; drift detection is bookkeeping over orun's own
  governed writes (the R-9 stance).
- **Audit stream.** `secret.accessed` / `secret.denied` / `secret.revealed` /
  `secret.sync.recorded` events (key-name-only payloads) ride the shipped
  `events.event_log` + `events.audit_entries`
  (`appendEventWithAudit`, `events/repository.ts:188`), filterable by entity,
  run, subject, or rule — the same stream the rest of the platform audits
  through, not a parallel one.
- **Rotation reminders (SEC7).** The shipped-but-unenforced `rotation_policy` /
  `expires_at` columns become inputs to a state-worker cron pass (the same
  cron plane that drives write-back), emitting expiry events the console and
  notifications-worker surface.

## 4. The console (Orun Cloud, `apps/web-console-next`)

Projections of the same model — nothing below is a second source of truth:

- **Secrets**: per-project/env metadata, version history, rotation state,
  last-used (`last_used_at`), and the **chain view** (`account → workspace →
  project → environment → personal`, shadowing and `servesFrom` made visible;
  locked keys badged). Values never shown except via audited break-glass
  reveal.
- **Policies**: the three tiers with their sources
  (`stack:acme-platform@1.4.0`, `composition:terraform`, `intent`), a visual
  `policy test` matrix (subject × env × platform → allow/deny), and the
  decision diff a Stack upgrade would introduce.
- **Components**: the `x-orun-secrets` facet on the entity page —
  requirements vs bindings, rotation age, sync targets — beside the scorecard
  grade it feeds.
- **Audit**: the event stream joined to runs and entities.
- **Access explainer**: "who can currently read prod `STRIPE_KEY`, and why" —
  Layer 1 roles (with cascade/team provenance) intersected with Layer 2 rules.

## 5. First ten minutes (the adoption path)

The experience a medium SaaS should have, end to end:

```
orun auth login                          # existing — session + auto-link (workspace/project resolved)

orun secrets import --from-dotenv .env --env dev    # bulk onboard, write-only
orun secrets set DATABASE_URL --env prod            # value from stdin

# adopt the golden Stack — compositions + their policy arrive together
#   intent.yaml: extends acme-platform@1.4.0

orun plan        # job deploy → secrets: [DATABASE_URL@prod (environment), STRIPE_KEY@prod (workspace)]
                 # materialize: 2 secrets → worker:api ; fails fast if a binding is missing/ungrantable
orun run         # claim (lease) → resolve → inject → redact → deploy → materialize → sealed provenance

# the catalog shows the component's secret facet; secret-hygiene grades it
```

Every later capability (personal overlays, locked account keys, rotation,
break-glass) extends this path without changing it.
