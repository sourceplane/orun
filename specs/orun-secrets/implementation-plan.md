# Implementation plan (v3)

> Milestones `SEC0 → SEC7`, re-anchored on the shipped platform. Each lands
> behind tests and is independently useful. Sequencing keeps the
> leak-prevention invariants true at every step: references and redaction land
> *before* any value can be resolved; materialization lands only after policy +
> provenance exist. Repo tags: **[orun]** = the Go CLI/runner, **[cloud]** =
> orun-cloud workers/db (the OV8 slice — see
> `orun-cloud/specs/epics/saas-secret-manager/`).
>
> **Already shipped (inherited baseline, not SEC work):** `config-worker`
> secret CRUD with AES-256-GCM write-only encryption and metadata-safe reads
> (070 migration); role×scope policy engine with dormant `secret.*` actions,
> account cascade (WID6), per-action provenance (TM6b1); the settings scope
> chain + guardrails (WID7) that secrets adopt; the run/lease coordination
> plane (OP2 + DO path); credential-agnostic CI auth → ActorContext (OV3);
> `appendEventWithAudit`; the api-edge actor spine.

## SEC0 — Reference type + leak guard [orun] (no backend dependency)

- `internal/secretref` (new): the `secret://<workspace>/<project>/<env>/<KEY>[@v]`
  grammar + parser (`data-model.md` §1).
- `SecretEnv` on `Component` + `EnvironmentSubscription`
  (`internal/model/intent.go:156-192`) and `ComponentInstance` (:329-358);
  `mergeSecretEnv` beside `mergeEnv` (`internal/expand/expander.go:321-348`);
  `SecretRefs` on `JobInstance` (`internal/model/job.go:101`) and `PlanJob`
  (`internal/model/plan.go:138`); carried in `internal/planner/planner.go:59-77`
  and `internal/render/plan.go:52-148` (`buildPlanJobSecretRefs` beside
  `buildEnv` — untouched).
- **Leak guard:** the planner rejects any `secretEnv` value that is a literal
  rather than a reference. `secretRefs` keys+versions fold into the plan
  checksum (`plan.go:211-224`) and the memoization input hash
  (`coordbackend.go:31-38`) — values structurally cannot.
- The `fsck`-style scanner asserting no value in any object/plan/log
  (Invariant 1).
- Outcome: references compile and render; **impossible to put a value in the
  secret slot**. No resolution yet.

## SEC1 — Store v3: versions, chain scopes, keys, RBAC activation [cloud]

- Migrations (`data-model.md` §7a-c): `secret_versions` (append-only history —
  rotate stops overwriting ciphertext in place), `secret_metadata` widening
  (`account` scope + `overridable` guardrail mirroring 430's settings change;
  `personal_owner`; `last_used_at`), `secret_deks`.
- **Chain resolution for secrets** (metadata plane): extend the WID7 resolver
  (`config-resolver.ts`) to `secret_metadata` exactly as its header reserves;
  `GET …/config/secrets?chain=true`; guardrail write-rejection for locked keys
  (the `create-setting.ts:189-190` pattern).
- **Key hierarchy:** `keyId` envelopes (`v:2`), per-workspace DEK wrap/unwrap,
  KEK in Cloudflare Secrets Store (entitlement confirmed, SS4); `k0` lazy
  migration for shipped `v:1` rows. Still **no decrypt route** — DEK unwrap
  code ships dormant behind the resolve handler that SEC3 adds.
- **RBAC activation:** secret routes switch from `*.config.read/write` to the
  dormant `secret.read`/`secret.write` actions; add the elevated
  `secret.reveal` action to the catalog.
- Routes: `POST …/config/secrets/import`, `GET …/config/secrets/{id}/versions`.
- **CLI [orun]:** `orun secrets set/import/list/rotate/revoke/versions` with
  scope flags + `--chain` (`cli-surface.md` §1).
- Outcome: secrets store shared/project/workspace/account/personal rows,
  versioned, encrypted under workspace DEKs, bulk-importable, chain-listable.
  Nothing can read a value.

## SEC2 — SecretPolicy: documents + evaluation + CLI [cloud + orun]

- `config.secret_policies` table (§7d); `PUT …/config/secret-policies`
  (idempotent by document hash) + `POST …/config/secret-policies/evaluate`.
- Layer-2 evaluation library in config-worker (`policy-model.md` §5): locked
  predicate vocabulary, protected-env activation, deny-wins, decision ids
  spanning both layers (Layer 1 via the existing policy-worker round-trip).
- **[orun]** `internal/secretpolicy`: parse/validate, the three-tier loader
  (composition auto-scoping, stack `policies/` discovery, intent overlays with
  the narrow-only check), `orun policy list/show/test/lint/push`.
- Outcome: the full decision engine, testable via `orun policy test` against
  the real evaluator, **still no value resolution**.

## SEC3 — Resolve path + runner injection + redaction (the value finally flows) [cloud + orun]

- **Contract rev (normative):** resolve body gains `leaseEpoch`; response gains
  `resolved[]` provenance (`data-model.md` §8; state-api-contract §4 v3).
- **[cloud]** `POST …/state/runs/{runId}/secrets/resolve` in state-worker:
  token authz + a new exported `verifyLiveLease(runId, jobId, runnerId,
  leaseEpoch)` covering **both** coordination backends (DO fold state +
  relational `state.run_jobs`); env slug → `environment_id` translation; then a
  **service binding** to config-worker's internal resolve: Layer-1
  `secret.value.use` + Layer-2 conditions → chain walk → DEK unwrap → decrypt
  (the first and only decrypt path) → `last_used_at` stamp → `secret.accessed`
  per key (new event type) → `{secrets, resolved[], ttlSeconds}`.
- Platform-fact derivation from the verified actor kind
  (`runner-integration.md` §3); personal rung served only for `local-cli` +
  owner (Invariant 9, server-side).
- **[orun]** `internal/secretresolve` over the remotestate client;
  `internal/redact`; inject as the top merge layer (`runner.go:1365` + `:643`);
  redact once at capture (`runner.go:575`), upstream of all sinks; personal
  notice in run output; compile-time grant check + plan annotations
  (`grant`/`servesFrom`/`personalShadow`).
- Outcome: end-to-end Doppler-grade flow — declare references, `orun run`
  claims a lease, resolves, injects, redacts; decisions are audited; personal
  overlays work.

## SEC4 — Composition `secretBindings` + provenance + catalog facet [orun + cloud]

- `secretBindings` on `JobTemplate`/`ExecutionProfile`
  (`internal/model/composition.go`); planner maps bindings → references for
  `(project, env)`; required-but-unresolvable → compile error.
- Sealed-run provenance: `{key, version, decisionId}` per job in the execution
  tree (Invariant 6).
- **Catalog facet (SD-14):** register `x-orun-secrets`
  (`catalogext.Registry`); resolver derives `requirements` statically and joins
  `bindings` (with `servesFrom`) / `rotation` from `GET …/config/secrets`
  metadata as live-plane data.
- Outcome: portable, component-aware requirements ride the Stack; runs are
  audit-complete; the catalog shows secret health per component.

## SEC5 — Inter-job outputs (SD-9) [orun + cloud]

- `outputs` on jobs; `$ORUN_OUTPUT` capture at seal into `artifacts/`;
  downstream `${{ jobs.x.outputs.y }}`.
- Sensitive outputs → run-scoped secret references; key material dies at run
  GC; redacted like any secret.
- Outcome: GitHub-Actions-grade job outputs + safe credential hand-off.

## SEC6 — Materialization (SD-13) [orun + cloud]

- `materialize` on `ExecutionProfile`; compile-time subset check; explicit
  materialize plan step.
- Adapter registry with v1 adapter `cloudflare-worker` (over `SetWorkerSecret`,
  `internal/cloudflare/client.go:437`); target binding derived from the
  provisioned entity. (The `saas-secrets-sync` assemble/sync/fingerprint
  tooling is the in-house prior art.)
- `config.secret_syncs` + `GET …/config/secrets/syncs`; provisioned-entity
  facet stamp; `superseded`/`orphaned` lifecycle (Invariant 10).
- `orun secrets rotate` raises `onRotate` system triggers; `orun secrets syncs`;
  convergence view in operations.
- Outcome: deployed applications receive secrets through one governed,
  recorded, rotation-aware door.

## SEC7 — Break-glass + console + scorecards + rotation UX [cloud + orun]

- `POST …/config/secrets/{id}/reveal` (elevated `secret.reveal`, alerted) +
  `orun secrets reveal`.
- Console (`web-console-next`): secrets/chain view, three-tier policies + test
  matrix, per-entity facet, audit, access explainer
  (`platform-integration.md` §4).
- `secret-hygiene` scorecard rules over the facet, incl. the pre-computed
  live-plane booleans.
- Rotation scheduler: a state-worker cron pass over the shipped-but-unenforced
  `rotation_policy`/`expires_at` columns → expiry events → console +
  notifications.
- Outcome: operational completeness; secret posture joins the maturity ladder.

## Deferred (post-v3 v1; register in `risks-and-open-questions.md`)

- Self-hosted backend parity: D1 translation of §7, `orun backend init` bundle
  extension, customer-held KEK (D-2/Q-2).
- Additional materialization adapters (AWS SSM/Secrets Manager, GitHub repo
  secrets) — Q-7.
- GitHub team sync into platform teams (integrations-worker follow-up).
- Inbound external-provider sync via the envelope `provider` seam.
- Dynamic/leased secrets (Vault-style).
- CEL `expr` predicates behind a capability flag (SD-7), shared with
  scorecards.
- Environment-inheritance graphs beyond the fixed chain.
