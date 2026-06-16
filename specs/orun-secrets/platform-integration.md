# Platform integration — catalog, scorecards, operations, dashboard

> How secrets surface across Orun Cloud so they feel like a pillar of the
> platform, not a vault bolted to its side: a derived facet on the Component
> entity (SD-14), scorecard checks over that facet, run/audit joins for
> operations, and the hosted dashboard. Delivery mechanics are in
> `runner-integration.md`; schemas in `data-model.md`.

## 1. The catalog facet: `extensions.x-orun-secrets` (SD-14)

The service catalog already gives every entity a typed extension seam
(`internal/catalogmodel/entity_envelope.go:36`,
`specs/orun-service-catalog/data-model.md` §8: `x-*` blocks validated against
registered schemas, preserved on round-trip). Secrets register
`x-orun-secrets` on the **Component** entity:

```jsonc
"extensions": {
  "x-orun-secrets": {
    "requirements": [                       // declared — from composition secretBindings (static)
      { "key": "DATABASE_URL", "profile": "worker-deploy", "required": true },
      { "key": "STRIPE_KEY",   "profile": "worker-deploy", "required": true }
    ],
    "groups": ["observability"],            // shared-group binds (SD-12)
    "bindings": [                           // live — joined against backend metadata
      { "key": "DATABASE_URL", "env": "prod", "status": "bound",   "version": 7 },
      { "key": "STRIPE_KEY",   "env": "prod", "status": "missing" }
    ],
    "rotation": [                           // live — version age from secret_metadata
      { "key": "DATABASE_URL", "env": "prod", "rotatedAt": "2026-04-02", "ageDays": 70 }
    ],
    "syncs": [                              // live — from secret_syncs (SD-13)
      { "key": "DATABASE_URL", "env": "prod", "target": "cloudflare-worker",
        "entityRef": "Resource/worker-api-prod", "version": 7, "status": "synced" }
    ]
  }
}
```

Derivation follows the catalog's static/live split:

- **`requirements` / `groups`** come from the repo + Stack at resolve time —
  the same composition documents the resolver already reads (declared truth,
  offline-computable).
- **`bindings` / `rotation` / `syncs`** are the **live plane**: the catalog
  resolver queries `GET …/secrets` and `GET …/secrets/syncs` metadata
  (never values) and joins per `(namespace, env, key)`. Offline or
  unauthenticated, these report `unknown` — which scorecards render distinctly
  rather than as failures (the scorecards anti-drift property,
  `specs/orun-scorecards/design.md` §3).

The facet is a **derived projection** — rebuildable from compositions + backend
metadata, never a source of truth, and it never contains a value or ciphertext
(Invariant 1 applies to the catalog too: entity envelopes are content).

**Provisioned entities get a facet too.** Composition `effects.graph` already
derives `Resource`/`API` entities from deploys
(`internal/model/composition.go:75-90`). A materialization stamps the
*provisioned* entity with the inverse view — "this running Worker holds
`DATABASE_URL@7`, synced by run `exec-…`" — so both ends of the sync are
navigable in the catalog.

## 2. Scorecard checks over the facet

Because secret health is now entity data, it is gradeable with the existing
scorecard machinery (`specs/orun-scorecards/`) — same locked predicate
vocabulary (`field-exists`, `field-equals`, `count`, `numeric-compare` over
`envelope.*` / `live.*` paths, `specs/orun-scorecards/data-model.md` §3), no
new engine:

```yaml
# scorecards/secret-hygiene.yaml
apiVersion: orun.io/v1
kind: Scorecard
metadata: { name: secret-hygiene }
spec:
  appliesTo: { kind: Component }
  rules:
    - id: bindings-satisfied        # every required binding is bound in prod
      expr: 'count(live.secrets.bindings[status == "missing"]) == 0'
      weight: 3
    - id: rotation-90d              # no prod secret older than 90 days
      expr: 'max(live.secrets.rotation[env == "prod"].ageDays) <= 90'
      weight: 2
    - id: syncs-current             # deployed entities hold the latest version
      expr: 'count(live.secrets.syncs[status == "superseded"]) == 0'
      weight: 2
    - id: no-personal-in-shared     # hygiene: shared groups carry no personal overlays
      expr: 'count(live.secrets.bindings[personal == true && namespace matches "*/_shared/*"]) == 0'
      weight: 1
```

(Field paths shown indicatively; the v1 predicate forms may require
pre-computed booleans on the live plane — e.g., the resolver emits
`rotationWithin90d: true` per env — which is the same flattening trick the
scorecards spec already uses for `deployment-status`. Date math proper arrives
with the shared CEL upgrade path, SD-7.)

This is the product story for a platform team: **"silver requires
secret-hygiene ≥ bindings-satisfied + rotation-90d"** — secret posture becomes
part of the same maturity ladder as docs, ownership, and deployment health.

## 3. Operations: runs, rotation, drift

- **Run provenance.** A sealed `ExecutionRun` records `{key, version,
  decisionId}` per step (`runner-integration.md` §8). The cockpit renders
  "this run read DATABASE_URL@7, STRIPE_KEY@3 under rule `admins-prod`" — from
  the graph plus `secret_audit`, no value anywhere.
- **Rotation as an operation.** `orun secrets rotate` writes the new version and
  raises an `onRotate` system trigger for every deploy profile that
  materializes the key (`data-model.md` §2.4). The resulting deploys are
  ordinary, plan-visible, audited runs; the `secret_syncs` rows flip prior
  syncs to `superseded` until the redeploy lands. Rotation status is therefore
  *observable*: the dashboard shows "rotated 14:02 — 3/4 targets converged".
- **Drift detection.** A sync row whose `entityRef` no longer exists (entity
  decommissioned) goes `orphaned`; a deployed entity holding a `superseded`
  version beyond a grace window surfaces in operations and fails the
  `syncs-current` scorecard rule. orun cannot *read* a foreign store to verify
  contents (and must not try); drift detection is bookkeeping over orun's own
  governed writes — which is sufficient as long as materialization is the only
  write path, the stance R-9 mitigates toward.
- **Audit stream.** `secret.resolved` / `secret.denied` / `secret.revealed` /
  `secret.synced` events (key-name-only payloads) join the existing event
  surface, filterable by entity, run, subject, or rule.

## 4. The dashboard (Orun Cloud console)

The hosted console exposes the same model as a projection — nothing below is a
second source of truth:

- **Secrets**: per-namespace/env metadata, version history, rotation state,
  last-used (from `secret_audit`), env-chain view (`base → env → personal`
  with shadowing made visible). Values never shown except via audited
  break-glass reveal.
- **Policies**: the three tiers (composition-attached / stack / intent) with
  their sources, a visual `policy test` matrix (subject × env × platform →
  allow/deny), and the decision diff a Stack upgrade would introduce.
- **Components**: the `x-orun-secrets` facet rendered on the entity page —
  requirements vs bindings, rotation age, sync targets — beside the scorecard
  grade it feeds.
- **Audit**: the event stream joined to runs and entities.
- **GitHub App**: install/refresh the `gh_identity_map`; show team→grant
  resolution ("who can currently read prod `STRIPE_KEY`, and why").

## 5. First ten minutes (the adoption path)

The experience a medium SaaS should have, end to end, with nothing outside
orun:

```
orun backend init                       # already provisions Worker + D1 + R2 (existing)
orun auth login                         # GitHub OAuth (existing)
# install the Orun Cloud GitHub App → gh_identity_map populates

orun secrets import --from-dotenv .env --env dev      # bulk onboard, write-only
orun secrets set DATABASE_URL --env prod              # value from stdin

# adopt the golden Stack — compositions + their policy arrive together
#   intent.yaml: extends acme-platform@1.4.0

orun plan        # shows: job deploy → secrets: [DATABASE_URL@prod] ; materialize → worker:api
                 # fails fast if a required binding is missing or ungrantable
orun run         # inject → redact → deploy → materialize → sealed provenance

# catalog now shows the component's secret facet; secret-hygiene scorecard grades it
```

Every later capability (personal overlays, shared groups, rotation, break-glass)
extends this path without changing it.
