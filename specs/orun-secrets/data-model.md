# Data model

> Schemas for the reference grammar, environments and personal overlays, the
> ciphertext envelope, the D1 tables, the `SecretPolicy` document, composition
> `secretBindings` + `materialize`, the plan additions, and the GitHub identity
> map. Design rationale is in `design.md`; the policy engine in
> `policy-model.md`; the catalog facet in `platform-integration.md`. All
> documents are apiVersion `orun.io/v1`.

## 1. The `secret://` reference grammar (the only secret-shaped thing in content)

```
secret://<namespace>/<env>/<KEY>[@<version>]

namespace : <org>/<repo>                  # default: the repo namespace (SD-12)
          | <org>/_shared/<group>        # explicit org-shared group (SD-12)
env       : an environment declared in intent, or the reserved "base"
KEY       : ^[A-Za-z][A-Za-z0-9._-]{0,127}$   (same shape as config-worker KEY_RE)
version   : positive integer, or omitted ⇒ resolves "latest" at run time
```

Examples: `secret://acme/api/prod/DATABASE_URL`,
`secret://acme/api/prod/STRIPE_KEY@7`,
`secret://acme/_shared/observability/DATADOG_API_KEY`.

A reference is opaque content: it is safe in `intent.yaml`, `component.yaml`,
`plan.json`, refs, and logs. It carries **no value** and **no identity** —
personal overlays (SD-11) are a resolve-time concern and never appear in the
grammar, so plans stay content-stable regardless of who runs them.

### 1.1 Environment chain resolution (SD-11)

A resolve of `secret://ns/<env>/<KEY>` walks, per key:

```
1. personal(ns, env, KEY, caller.gh_user_id)   — only if platform == local-cli
2. (ns, env, KEY)
3. (ns, "base", KEY)
→ first hit wins; no hit ⇒ unknown-reference error
```

`base` is a reserved env name holding org-shared defaults; `@<version>` pins
apply to whichever link served the key. The chain is fixed at three links in
v1 (`design.md` §6).

## 2. Authoring surface

### 2.1 Component / intent (reference only)

```yaml
# component.yaml
apiVersion: orun.io/v1
kind: Component
metadata: { name: api, type: cloudflare-worker, domain: payments }
spec:
  env:                         # existing plaintext env (unchanged)
    LOG_LEVEL: info
  secretEnv:                   # NEW — references only, never values
    DATABASE_URL: "secret://acme/api/{{env}}/DATABASE_URL"
    STRIPE_KEY:   "secret://acme/api/{{env}}/STRIPE_KEY"
```

`{{env}}` interpolates the resolving environment (reuses the existing
interpolation in `internal/expand/expander.go:316-348`), so one declaration
spans dev/staging/prod.

### 2.2 Composition `secretBindings` (portable requirement, ships in the Stack)

`secretBindings` is added to `JobTemplate` / `ExecutionProfile`
(`internal/model/composition.go`). It declares the **logical** secrets a
profile needs — portable, component-aware, OCI-distributable:

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
resolving `(namespace, env)` and emits it on the job (§5). A `required` binding
with no grantable reference is a **compile-time error**. Bindings (declared)
joined with backend state (bound/missing) form the catalog facet
(`platform-integration.md` §1).

### 2.3 Shared-group binding (SD-12)

A component opts into an org-shared group explicitly; sharing is never ambient:

```yaml
# component.yaml (continued)
spec:
  secretGroups: ["observability"]        # binds acme/_shared/observability
  secretEnv:
    DATADOG_API_KEY: "secret://acme/_shared/observability/DATADOG_API_KEY"
```

A `secretEnv` reference into `_shared/<group>` without the matching
`secretGroups` entry is a compile-time error, and a policy must still grant the
group (`policy-model.md` §5).

### 2.4 Composition `materialize` (runtime delivery, SD-13)

A deploy-shaped profile may declare how bound secrets reach the *deployed*
application's native store:

```yaml
# compositions/cloudflare-worker/profiles/worker-deploy.yaml
apiVersion: orun.io/v1
kind: ExecutionProfile
metadata: { name: worker-deploy }
spec:
  secretBindings:
    DATABASE_URL: { required: true }
    STRIPE_KEY:   { required: true }
  materialize:
    target: cloudflare-worker         # typed adapter id (v1: cloudflare-worker)
    secrets: [DATABASE_URL, STRIPE_KEY]   # subset of this profile's bindings
    onRotate: redeploy                # rotation raises a system trigger for this profile
```

Constraints: `secrets` must be a subset of the profile's `secretBindings`
(compile-time check); the adapter is typed and versioned with the composition;
the materialize step renders as an explicit plan step and writes `secret_syncs`
provenance (§7, `runner-integration.md` §6).

## 3. Ciphertext envelope (stored in D1, never in the graph)

Extends the shipped `config-worker` envelope
(`multi-tenant-saas/apps/config-worker/src/encryption.ts:14-23`) with `keyId`:

```jsonc
{
  "alg":   "AES-256-GCM",   // authenticated encryption
  "v":     1,               // envelope format version (migration)
  "keyId": "dek:acme:3",    // which namespace DEK + generation (cryptoshred / rotate)
  "iv":    "<base64 12-byte nonce>",
  "ct":    "<base64 ciphertext incl. GCM tag>"
}
```

- Per-value random 12-byte IV.
- The DEK named by `keyId` is itself stored **wrapped** (by the namespace's
  current KEK in Cloudflare Secrets Store); the unwrapped DEK exists only in
  Worker memory during a resolve.
- `provider` (reserved, optional): for a future external-backed value
  (`aws-secrets-manager://…`) the envelope is a pointer, not ciphertext — the
  seam for inbound external-provider sync (deferred).

## 4. `SecretPolicy` document (portable; composition, Stack, or intent — SD-10)

```yaml
apiVersion: orun.io/v1
kind: SecretPolicy
metadata:
  name: prod-secrets
spec:
  rules:
    - id: admins-prod              # stable id, used in audit reason codes
      effect: allow                # allow | deny
      subjects:                    # GitHub-native (policy-model.md §2)
        - "gh:team:@acme/platform-admins"
      scope:
        namespace: "acme/*"        # repo namespaces only; _shared must be named (SD-12)
        env: prod
        key: "*"
      when:                        # locked predicate vocabulary (policy-model.md §6)
        - 'platform in ["github-actions-oidc","orun-cloud-runner"]'
  # reserved: expr (CEL) behind a capability flag (SD-7)
```

Field notes:
- `subjects[]` — GitHub ids/teams/principals; resolved via the GH map at
  decision time.
- `scope{namespace,env,key}` — glob-capable; most-specific-wins; `namespace`
  globs never cross the `_shared/` boundary.
- `when[]` — AND-of-predicates from the locked vocabulary; OR via multiple
  rules.
- Placement determines constraints (`policy-model.md` §1): a
  composition-attached document gets `component.type == "<composition>"`
  injected into every rule; an intent-layer `allow` broader than any
  Stack/composition `allow` is rejected (narrow-only).

## 5. Plan additions (references only — Invariant 1)

`PlanJob` gains `secretRefs`, `outputs` (SD-9), and materialize steps render
explicitly. **No value field exists**; this is structural, not conventional.

```jsonc
// inside plan.json — content-addressed, value-free
{
  "id": "deploy-api-prod",
  "component": "api",
  "environment": "prod",
  "env": { "LOG_LEVEL": "info" },          // existing plaintext env (unchanged)
  "secretRefs": [                           // NEW — references the runner will resolve
    { "asEnv": "DATABASE_URL", "ref": "secret://acme/api/prod/DATABASE_URL@latest" },
    { "asEnv": "STRIPE_KEY",   "ref": "secret://acme/api/prod/STRIPE_KEY@latest" }
  ],
  "materialize": {                          // NEW — explicit, reviewable sync step (SD-13)
    "target": "cloudflare-worker",
    "secrets": ["DATABASE_URL", "STRIPE_KEY"]
  },
  "outputs": { "image": { "sensitive": false } },   // NEW — declared job outputs (SD-9)
  "dependsOn": ["build-api"]
}
```

Render is extended in `internal/render/plan.go` to emit `secretRefs` and
`materialize`; the existing `buildEnv` (`internal/render/plan.go:133-148`) is
unchanged and continues to carry only non-secret env. A planner guard rejects
any `secretEnv` key whose resolved value is a literal rather than a `secret://`
reference (defense against leak vector #1). `orun plan` additionally marks refs
that may be personally shadowed for the current user (SD-11) so local behavior
is never a surprise.

## 6. The catalog facet (`extensions.x-orun-secrets`)

Shape and derivation live in `platform-integration.md` §1; it attaches via the
entity envelope's typed extension seam
(`internal/catalogmodel/entity_envelope.go:36`,
`specs/orun-service-catalog/data-model.md` §8) and is a **derived projection** —
rebuildable from compositions (declared) + backend metadata (live), never a
source of truth.

## 7. D1 schema (Orun Cloud backend — extends the migration bundle)

Added to the embedded migrations applied by `orun backend init`
(`cmd/orun/command_backend.go:457-493`, `internal/backendbundle`):

```sql
-- secret metadata (one row per logical secret per scope) — NO value here
CREATE TABLE secret_metadata (
  id            TEXT PRIMARY KEY,
  namespace     TEXT NOT NULL,            -- org/repo or org/_shared/<group> (SD-12)
  env           TEXT NOT NULL,            -- declared env or reserved 'base' (SD-11)
  key           TEXT NOT NULL,
  personal_owner INTEGER,                 -- NULL = shared; else GitHub numeric user id (SD-11)
  display_name  TEXT,
  rotation_policy TEXT,
  current_version INTEGER NOT NULL DEFAULT 0,
  created_by    TEXT NOT NULL,            -- GitHub numeric user id
  created_at    TEXT NOT NULL,
  UNIQUE (namespace, env, key, personal_owner)
);

-- append-only versions; ciphertext envelope only (design.md §4)
CREATE TABLE secret_versions (
  secret_id     TEXT NOT NULL REFERENCES secret_metadata(id),
  version       INTEGER NOT NULL,
  ciphertext_envelope TEXT NOT NULL,      -- JSON envelope; never plaintext
  created_by    TEXT NOT NULL,
  created_at    TEXT NOT NULL,
  PRIMARY KEY (secret_id, version)
);

-- wrapped DEKs per namespace+generation (KEK lives in Secrets Store, not here)
CREATE TABLE secret_deks (
  namespace     TEXT NOT NULL,
  generation    INTEGER NOT NULL,
  wrapped_dek   TEXT NOT NULL,
  state         TEXT NOT NULL,            -- active | retiring | shredded
  PRIMARY KEY (namespace, generation)
);

-- compiled SecretPolicy rules (rebuildable from the composition/Stack/intent source-of-truth)
CREATE TABLE secret_policies (
  id            TEXT PRIMARY KEY,
  namespace     TEXT NOT NULL,
  source        TEXT NOT NULL,            -- "composition:<type>" | "stack:<ref>" | "intent"  (SD-10)
  document      TEXT NOT NULL,            -- canonical SecretPolicy JSON
  version       INTEGER NOT NULL,
  created_at    TEXT NOT NULL
);

-- materialization provenance: which version was synced into which deployed entity (SD-13)
CREATE TABLE secret_syncs (
  id            TEXT PRIMARY KEY,
  namespace     TEXT NOT NULL,
  env           TEXT NOT NULL,
  key           TEXT NOT NULL,
  version       INTEGER NOT NULL,
  target        TEXT NOT NULL,            -- adapter id, e.g. cloudflare-worker
  entity_ref    TEXT NOT NULL,            -- provisioned catalog entity (effects.graph)
  exec_id       TEXT NOT NULL,            -- the deploy run that performed the sync
  status        TEXT NOT NULL,            -- synced | superseded | orphaned
  ts            TEXT NOT NULL
);

-- GitHub identity projection (cache of the org via the GH App) — never authoritative
CREATE TABLE gh_identity_map (
  gh_user_id    INTEGER PRIMARY KEY,      -- stable numeric id (the portable key, SD-4)
  gh_login      TEXT NOT NULL,            -- current login (mutable; convenience)
  namespace     TEXT NOT NULL,            -- org/installation
  teams         TEXT NOT NULL,            -- JSON array of team slugs
  updated_at    TEXT NOT NULL
);

-- decision audit; key-name-only, never values (policy-model.md §8)
CREATE TABLE secret_audit (
  decision_id   TEXT PRIMARY KEY,
  ts            TEXT NOT NULL,
  subject_id    TEXT NOT NULL,
  namespace     TEXT NOT NULL,
  env           TEXT NOT NULL,
  key           TEXT NOT NULL,
  version       INTEGER,
  allow         INTEGER NOT NULL,         -- 0|1
  rule_id       TEXT,
  reason        TEXT NOT NULL,
  platform      TEXT NOT NULL,
  exec_id       TEXT
);
```

`gh_identity_map` and `secret_syncs` are **derived projections** (of GitHub and
of performed deploys respectively) — rebuildable, never the source of truth;
consistent with the object-model rule that databases are derived and rebuildable
(`specs/orun-object-model/remote-and-consumers.md:55-62`).

## 8. API surface (`orun-api` Worker — additive routes)

```
POST /v1/secrets                      putSecret(ns,env,key,value[,personal])     → metadata (write-only)
GET  /v1/secrets                      listSecretMetadata(scope)                  → metadata[] (no values)
POST /v1/secrets/import               importDotenv(ns,env,entries)               → metadata[] (write-only bulk)
POST /v1/secrets/rotate               rotateSecret(id,value)                     → new version + raises onRotate triggers (SD-13)
POST /v1/secrets/revoke               revokeSecret(id|version)                   → tombstone
POST /v1/secrets/resolve              resolve(refs[], triggerContext, token)     → { values | denials }   ← runner path
POST /v1/secrets/reveal               reveal(id, breakGlass=true)                → value (elevated, alerted)
POST /v1/secrets/syncs                recordSync(entries[])                      → provenance rows (runner, post-materialize)
GET  /v1/secrets/syncs                listSyncs(scope|entityRef)                 → sync state (catalog resolver, dashboard)
POST /v1/policies                     putPolicy(SecretPolicy, source)            → version
POST /v1/policies/evaluate            dryRun(refs, facts)                        → decisions (for `orun plan` + UI)
```

`/v1/secrets/resolve` is the only route that returns plaintext to a machine; it
runs the four-axis decision (`policy-model.md`), walks the env chain (§1.1),
unwraps the DEK, decrypts, audits, and responds over TLS. `/reveal` is the
single human break-glass path (SD-3), behind an elevated policy action and
always alerted.

## 9. Identity resolution inputs

| Caller | Token | Subject facts derived |
|--------|-------|-----------------------|
| GitHub Actions | OIDC JWT (`internal/remotestate/auth.go:24-88`) | `actor`→gh_user_id, `repository`→namespace, `ref`→branch, `platform=github-actions-oidc` |
| Human CLI/dashboard | OAuth session (`internal/cliauth`) | gh_user_id + teams (GH map), `platform=local-cli` |
| Service / cloud runner | `ORUN_TOKEN` / sp binding | sp id, `platform ∈ {service-token, orun-cloud-runner}` |
