# Data model (v3)

> Schemas for the reference grammar, the scope chain and personal overlays, the
> ciphertext envelope + key hierarchy, the Postgres tables (extending the
> shipped `config` schema), the `SecretPolicy` document, composition
> `secretBindings` + `materialize`, the plan additions, and the wire routes.
> Design rationale in `design.md`; the policy engine in `policy-model.md`; the
> catalog facet in `platform-integration.md`. Documents are apiVersion
> `orun.io/v1`.

## 1. The `secret://` reference grammar

```
secret://<workspace>/<project>/<env>/<KEY>[@<version>]

workspace : the workspace slug (an organizations row; APIs also accept ws_<id>)
project   : the project slug (== the repo, OV2 bijection)
env       : an environment declared in intent (projects.environments slug)
KEY       : ^[A-Za-z][A-Za-z0-9._-]{0,127}$        (the shipped KEY shape)
version   : positive integer, or omitted ⇒ head at resolve time
```

Examples: `secret://acme/api/prod/DATABASE_URL`,
`secret://acme/api/prod/STRIPE_KEY@7`.

Grammar notes (v3):

- **The v2 `namespace` term is retired** (closes v2's deferred D-1). The first
  two segments are the platform's workspace/project spine — exactly what
  `RepoLink` caches (`internal/cliauth/types.go:74-88`), so
  `{{workspace}}`/`{{project}}` default from the linked scope and authors
  rarely type them.
- **There is no `_shared/<group>` namespace and no reserved `base` env.** A
  reference always names the project's chain; *which scope serves the key*
  (environment / project / workspace / account) is resolution, surfaced in the
  plan, `--chain`, and the catalog facet (`design.md` §6).
- A reference is opaque content: safe in `intent.yaml`, `component.yaml`,
  `plan.json`, refs, and logs. It carries **no value** and **no identity** —
  personal overlays are a resolve-time concern and never appear in the grammar,
  so plans stay content-stable regardless of who runs them.

### 1.1 Chain resolution (SD-11′)

A resolve of `secret://ws/prj/<env>/<KEY>` walks, per key, most-specific-first:

```
1. personal(environment_id, KEY, subject)   — only if platform fact == local-cli; owner only
2. (scope=environment, environment_id, KEY)
3. (scope=project,     project_id,     KEY)      ← v2's "base" rung, now a real scope
4. (scope=workspace,   org_id,         KEY)      ← replaces _shared/<group>
5. (scope=account,     parent org,     KEY)      ← account rung via effectiveBillingOrgId
→ first hit wins; no hit ⇒ unknown-reference error
```

This is the shipped WID7 settings chain (`config-resolver.ts:66-132`) — which
was explicitly "designed so secret_metadata can adopt the same shape later" —
with the personal rung on top. `@<version>` pins apply to whichever rung serves
the key. A serving row with `overridable = false` at account/workspace scope
also **rejects lower-rung writes** of the same key (409, mirroring
`create-setting.ts:189-190`).

## 2. Authoring surface

### 2.1 Component / intent (reference only)

```yaml
# component.yaml
apiVersion: orun.io/v1
kind: Component
metadata: { name: api, type: cloudflare-worker, domain: payments }
spec:
  env:                          # existing plaintext env (unchanged)
    LOG_LEVEL: info
  secretEnv:                    # NEW — references only, never values
    DATABASE_URL: "secret://acme/api/{{env}}/DATABASE_URL"
    STRIPE_KEY:   "secret://acme/api/{{env}}/STRIPE_KEY"
```

`{{env}}` interpolates the resolving environment (existing interpolation,
`internal/expand/expander.go:321-348`). The expander gains `mergeSecretEnv`
mirroring `mergeEnv`, writing `ComponentInstance.SecretEnv`
(`internal/model/intent.go:329-358`). **Leak guard:** a literal (non-reference)
value in `secretEnv` is a compile error.

### 2.2 Composition `secretBindings` (portable requirement, ships in the Stack)

```yaml
# compositions/terraform/profiles/terraform-release.yaml
apiVersion: orun.io/v1
kind: ExecutionProfile
metadata: { name: terraform-release }
spec:
  secretBindings:
    AWS_ROLE_ARN: { required: true }
    TF_API_TOKEN: { required: true }
    SLACK_WEBHOOK: { required: false }
```

At plan time the planner maps each binding to a `secret://` reference for the
instance's `(project, env)` and emits it on the job (§5). A `required` binding
with no resolvable, grantable reference is a **compile-time error**. Bindings
(declared) joined with backend state (bound/missing) form the catalog facet.

> v2's `secretGroups` bind block is **removed** with the `_shared` namespaces:
> shared keys are served by the workspace/account rungs of the chain, and their
> visibility obligations move to the plan annotations (`servesFrom`), the
> `--chain` view, and lockable guardrails (SD-12′).

### 2.3 Composition `materialize` (runtime delivery, SD-13)

```yaml
# compositions/cloudflare-worker/profiles/worker-deploy.yaml
spec:
  secretBindings:
    DATABASE_URL: { required: true }
    STRIPE_KEY:   { required: true }
  materialize:
    target: cloudflare-worker            # typed adapter id (v1: cloudflare-worker)
    secrets: [DATABASE_URL, STRIPE_KEY]  # must be ⊆ this profile's bindings (compile-checked)
    onRotate: redeploy                   # rotation raises a system trigger for this profile
```

The adapter is typed and versioned with the composition; the materialize step
renders as an explicit plan step and writes `secret_syncs` provenance (§7).

## 3. Ciphertext envelope + key hierarchy (SD-2′)

```jsonc
// stored per VERSION in config.secret_versions.ciphertext_envelope
{
  "alg":   "AES-256-GCM",       // authenticated encryption; per-value random 12-byte IV
  "v":     2,                    // envelope format version
  "keyId": "ws:org_01H…:3",      // workspace DEK + generation (cryptoshred / rotate unit)
  "iv":    "<base64 12-byte nonce>",
  "ct":    "<base64 ciphertext incl. GCM tag>"
}
```

- `v:1` envelopes (the shipped shape, `encryption.ts:14-23`, no `keyId`)
  decrypt under the static `SECRET_ENCRYPTION_KEY` (implicit `keyId:"k0"`);
  `v:2` envelopes name a workspace DEK generation. Lazy migration: reads accept
  both; writes and rotations produce `v:2`.
- The DEK named by `keyId` is stored **wrapped** in `config.secret_deks`; the
  **KEK lives in Cloudflare Secrets Store** bound to config-worker (entitlement
  confirmed — saas-secrets-sync SS4), never in Postgres. The unwrapped DEK
  exists only in Worker memory during a resolve.
- Decrypt-capable key import (`["decrypt"]` usage) exists **only** in the
  resolve and break-glass handlers — today no decrypt path exists anywhere
  (`encryption.ts:27-29,70`), and v3 keeps that surface minimal.
- `provider` (reserved, optional): for a future external-backed value the
  envelope is a pointer, not ciphertext — the seam for inbound provider sync
  (deferred).

## 4. `SecretPolicy` document (portable; composition, Stack, or intent — SD-10)

```yaml
apiVersion: orun.io/v1
kind: SecretPolicy
metadata:
  name: prod-secrets
spec:
  rules:
    - id: laptops-never-prod         # stable id, used in audit reason codes
      effect: deny
      scope: { env: prod, key: "*" }
      when:
        - 'platform == "local-cli"'
    - id: admins-and-ci-prod
      effect: allow
      subjects:
        - "team:platform-admins"     # platform principals (policy-model.md §2)
        - "workflow"                 # any CI-OIDC actor bound to this project
      scope: { env: prod, key: "*" }
      when:
        - 'trigger.declared && trigger.branch == "main"'
  # reserved: expr (CEL) behind a capability flag (SD-7)
```

Field notes:
- `subjects[]` — `user:<id>`, `team:<slug>`, `service_principal:<id>`, the
  actor-kind literals `workflow`/`user`/`service_principal`, `*authenticated`.
  Team membership resolves at decision time from membership facts (WID6
  cascade included).
- `scope{env,key}` — glob-capable; most-specific-wins. (No `namespace` field:
  a document's tenancy scope comes from where it is pushed — workspace-wide or
  project-scoped — `§7(d)`.)
- `when[]` — AND-of-predicates from the locked vocabulary (`policy-model.md`
  §6); OR via multiple rules.
- Placement determines constraints: a composition-attached document gets
  `component.type == "<composition>"` injected into every rule; an intent-tier
  `allow` broader than any Stack/composition `allow` is rejected (narrow-only,
  `policy-model.md` §5).

## 5. Plan additions (references only — Invariant 1)

`PlanJob` gains `secretRefs`; materialize and outputs render explicitly. The
fields land on the verified seams: `JobInstance` (`internal/model/job.go:101`),
`PlanJob` (`internal/model/plan.go:138`), converters
(`internal/planner/planner.go:59-77`, `internal/render/plan.go:52-148`).

```jsonc
// inside plan.json — content-addressed, value-free
{
  "id": "deploy-api-prod",
  "component": "api",
  "environment": "prod",
  "env": { "LOG_LEVEL": "info" },          // existing plaintext env (unchanged)
  "secretRefs": [
    { "asEnv": "DATABASE_URL", "ref": "secret://acme/api/prod/DATABASE_URL",
      "grant": "allow", "servesFrom": "environment", "personalShadow": false },
    { "asEnv": "STRIPE_KEY",   "ref": "secret://acme/api/prod/STRIPE_KEY",
      "grant": "allow", "servesFrom": "workspace",   "personalShadow": false }
  ],
  "materialize": { "target": "cloudflare-worker", "secrets": ["DATABASE_URL", "STRIPE_KEY"] },
  "outputs": { "image": { "sensitive": false } },
  "dependsOn": ["build-api"]
}
```

`grant`/`servesFrom`/`personalShadow` are compile-time annotations (existential
subject; metadata-only chain lookup). **No value field exists — structurally.**
A planner guard rejects any `secretEnv` value that is a literal rather than a
reference. The plan checksum (`plan.go:211-224`) covers refs; job memoization
already hashes env **keys only** (`internal/statebackend/coordbackend.go:31-38`)
— `secretRefs` keys + pinned versions join it, values never do.

## 6. The catalog facet (`extensions.x-orun-secrets`)

Shape in `platform-integration.md` §1; registered via
`catalogext.Registry.Register("x-orun-secrets", …)`
(`internal/catalogext/registry.go:26-44`) and carried on
`EntityEnvelope.Extensions` (`internal/catalogmodel/entity_envelope.go:36`) — a
derived projection, rebuildable from compositions (declared) + backend metadata
(live), never a source of truth. v3 change: a `servesFrom` scope per binding
replaces v2's `groups` list.

## 7. Postgres schema (extends the shipped `config` schema)

Migrations extend `packages/db/src/migrations/`. Shipped baseline:
`070_config_settings_flags` (`config.secret_metadata` — scope columns,
CHECK-constrained `scope_kind`, `ciphertext_envelope BYTEA`, `version` counter,
`rotation_policy`/`expires_at`/`last_rotated_at`) and
`430_config_account_scope` (account scope + `overridable` — **settings only**).

```sql
-- (a) widen secret_metadata: account scope, guardrail, personal owner, last-used
ALTER TABLE config.secret_metadata
  ADD COLUMN personal_owner UUID,           -- NULL = shared; else owning subject (SD-11′)
  ADD COLUMN overridable BOOLEAN NOT NULL DEFAULT true,  -- lockable (SD-12′)
  ADD COLUMN last_used_at TIMESTAMPTZ;      -- stamped by resolve (contract promises it; column missing today)
-- scope_kind CHECK widens to ('account','organization','project','environment')
--   (organization ≡ workspace; mirrors 430's settings change);
-- overridable = false permitted only for account/organization scope;
-- personal_owner permitted only for scope_kind = 'environment';
-- the unique scope-key index gains COALESCE(personal_owner, zero-uuid).

-- (b) append-only version history (SD-2′); head stays on secret_metadata.version
CREATE TABLE config.secret_versions (
  secret_id     UUID NOT NULL REFERENCES config.secret_metadata(id),
  version       INTEGER NOT NULL CHECK (version >= 1),
  ciphertext_envelope BYTEA NOT NULL,       -- JSON envelope (§3); never plaintext
  status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','revoked')),
  created_by    UUID NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (secret_id, version)
);
-- rotation appends here and bumps the head — it no longer overwrites ciphertext
-- in place (today: repository.ts:397-421 destroys history).

-- (c) wrapped workspace DEKs (KEK in Cloudflare Secrets Store, not here)
CREATE TABLE config.secret_deks (
  org_id        UUID NOT NULL,              -- the workspace
  generation    INTEGER NOT NULL,
  wrapped_dek   BYTEA NOT NULL,
  state         TEXT NOT NULL CHECK (state IN ('active','retiring','shredded')),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, generation)
);

-- (d) SecretPolicy documents, tier-tagged (SD-10)
CREATE TABLE config.secret_policies (
  id            UUID PRIMARY KEY,
  org_id        UUID NOT NULL,
  project_id    UUID,                       -- NULL = workspace-wide
  name          TEXT NOT NULL,
  tier          TEXT NOT NULL CHECK (tier IN ('composition','stack','intent')),
  source        TEXT NOT NULL,              -- "stack:acme-platform@1.4.0" | "composition:terraform" | "intent"
  document      JSONB NOT NULL,             -- validated SecretPolicy spec
  document_hash TEXT NOT NULL,              -- content address; push is idempotent by hash
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- (e) materialization provenance (SD-13, Invariant 10)
CREATE TABLE config.secret_syncs (
  id            UUID PRIMARY KEY,
  secret_id     UUID NOT NULL REFERENCES config.secret_metadata(id),
  version       INTEGER NOT NULL,
  target        TEXT NOT NULL,              -- adapter id, e.g. 'cloudflare-worker'
  entity_ref    TEXT NOT NULL,              -- provisioned catalog entity
  run_id        TEXT NOT NULL,              -- the deploy run (ULID)
  status        TEXT NOT NULL CHECK (status IN ('synced','superseded','orphaned')),
  synced_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Audit rides the shipped `events.event_log` + `events.audit_entries` via
`appendEventWithAudit` (`packages/db/src/events/repository.ts:188`), the same
pattern the secret write path already uses transactionally
(`create-secret.ts:231`). New event types: `secret.accessed`,
`secret.revealed`, `secret.policy.updated`, `secret.sync.recorded` (today only
`secrets.updated` exists).

## 8. Wire routes (revision of contract §4 — one API)

```jsonc
// Write path — the shipped …/config/secrets surface, unchanged shapes
POST   …/config/secrets                        // create (write-only value)
GET    …/config/secrets                        // metadata only; ?chain=true adds the serving-scope view
POST   …/config/secrets/{id}/rotate            // append a version, bump head
DELETE …/config/secrets/{id}                   // revoke (head, or a version)

// v3 additions on the config surface
POST   …/config/secrets/import                 // dotenv bulk onboarding (write-only)
GET    …/config/secrets/{id}/versions          // version metadata history
PUT    …/config/secret-policies                // push tier-tagged documents (idempotent by hash)
POST   …/config/secret-policies/evaluate       // dry-run a decision (orun policy test)
GET    …/config/secrets/syncs                  // materialization state
POST   …/config/secrets/{id}/reveal            // break-glass: elevated action + alert (SD-3)

// The resolve — state plane, because the lease lives there (SD-18)
POST …/state/runs/{runId}/secrets/resolve
{ "runnerId": "…", "jobId": "…", "leaseEpoch": 3, "refs": ["secret://…", …] }
→ 200 { "secrets": { "<KEY>": "<value>", … },
        "resolved": [ { "key", "version", "scope", "personal", "decisionId" }, … ],
        "ttlSeconds": 300 }
→ 409 lease_lost   |   403 typed policy denial   |   404 (resource-hiding)
```

Personal overlays write with `--personal` → `personal_owner = subject`; every
resolve whose server-derived platform fact ≠ `local-cli` skips rung 1
structurally (Invariant 9).

**Actions per route** (Layer 1 — activating the dormant catalog,
`policy-engine/index.ts:334-336`): metadata reads → `secret.read`; create,
rotate, revoke, import, policy push → `secret.write`; resolve →
`secret.value.use`; reveal → `secret.reveal` (**new elevated action** — owner/
admin only, never builder). Today's handlers authorize with
`*.config.read/write` (`create-secret.ts:161`); the switch to `secret.*` is
SEC1.
