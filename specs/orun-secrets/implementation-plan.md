# Implementation plan

> Milestones `SEC0 → SEC7`. Each lands behind tests and is independently
> useful. Sequencing keeps the leak-prevention invariants (`design.md` §11)
> true at every step: references and redaction land *before* any value can be
> resolved; materialization lands only after policy + provenance exist.

## SEC0 — Reference type + leak guard (no backend yet)
- Add the `secret://` grammar + parser (`internal/secretref`, `data-model.md`
  §1), including `_shared/<group>` namespaces (SD-12).
- Add `secretEnv`/`secretGroups` to `Component`/intent
  (`internal/model/intent.go`) and `secretRefs` to `PlanJob`
  (`internal/model/plan.go`); render references in `internal/render/plan.go`
  (extend, don't touch `buildEnv`).
- **Leak guard:** planner rejects any `secretEnv` value that is a literal
  rather than a `secret://` reference; reject `_shared` refs without a
  `secretGroups` bind. Add the `fsck`-style scanner asserting no value in any
  object/plan/log (Invariant 1).
- Outcome: references compile and render; **impossible to put a value in
  `env`'s secret slot**. No resolution yet.

## SEC1 — Backend store + crypto + environments (write path)
- D1 migrations: `secret_metadata` (with `personal_owner` + reserved `base`
  env), `secret_versions`, `secret_deks`, `secret_audit` (`data-model.md` §7),
  added to `internal/backendbundle`.
- KEK as a Worker secret; per-namespace DEK wrap/unwrap; AES-256-GCM envelope
  reusing the `config-worker` adapter shape.
- Routes (contract §4): `PUT …/secrets/{key}` (create/rotate, incl.
  `--personal`), `GET …/secrets` (metadata), `DELETE …/secrets/{key}`
  (`revoke`/`rm`), plus extension `POST …/secrets/import`. **Write-only — no
  resolve/reveal yet.**
- `orun secrets set/import/list/rotate/revoke/versions` with `--chain` view
  (`cli-surface.md` §1).
- Outcome: secrets (shared, base, personal) can be stored encrypted, bulk
  imported, and listed by metadata; nothing can read a value.

## SEC2 — GitHub identity + policy engine (decision, no values)
- GitHub App installation + `gh_identity_map` ingestion + membership webhooks.
- `internal/secretpolicy`: `SecretPolicy` parse/validate, the locked predicate
  vocabulary, deny-by-default evaluation, and the **three-tier loader** (SD-10):
  composition-attached fragments (auto-scoped `component.type` injection),
  stack `policies/` discovery, intent overlays with the narrow-only check
  (`policy-model.md` §1, §5).
- Routes: `POST …/policies` (put, with `source` tier), `POST
  …/policies/evaluate` (dry-run).
- `orun policy list/show/test/lint` (`cli-surface.md` §2).
- Outcome: full decision engine, testable via `orun policy test`, **still no
  value resolution**.

## SEC3 — Resolve path + runner injection + redaction (the value finally flows)
- `POST …/state/runs/{runId}/secrets/resolve` (lease-bound, contract §4;
  authorizes `secret.value.use`): four-axis decision → env-chain walk
  (`personal → env → base`, SD-11) → DEK unwrap → decrypt → audit → plaintext
  over TLS.
- `internal/secretclient` over the existing remote client; `Resolve` in
  `stepExecContext` (`internal/runner/runner.go:1320-1350`); personal-overlay
  notice in run output.
- **Redactor** seeded at resolve, applied at `AfterStepLog`
  (`internal/runner/runner.go:579`) before any blob write (Invariant 5).
- Compile-time grant check in the planner (existential subject); surface in
  `orun plan` + `--json`, including personal-shadow markers.
- Platform-fact derivation from auth mode (`runner-integration.md` §3);
  enforce Invariant 9 (personal ⇒ owner + `local-cli`) server-side.
- Outcome: end-to-end Doppler-grade flow — declare references, `orun run`
  injects values, logs are redacted, decisions are audited, personal overlays
  work.

## SEC4 — Composition `secretBindings` + provenance + catalog facet
- `secretBindings` on `JobTemplate`/`ExecutionProfile`
  (`internal/model/composition.go`); planner maps bindings → references for
  `(namespace, env)`; required-but-ungrantable → compile error.
- Sealed-run provenance: record `{key, version, decisionId}` per step in the
  execution tree (Invariant 6, `runner-integration.md` §8).
- **Catalog facet (SD-14):** register the `x-orun-secrets` extension schema;
  resolver derives `requirements`/`groups` statically and joins
  `bindings`/`rotation` from `GET …/secrets` metadata as live-plane data
  (`platform-integration.md` §1).
- Outcome: portable, component-aware requirements ride the Stack; runs are
  audit-complete; the catalog shows secret health per component.

## SEC5 — Inter-job outputs (SD-9)
- `outputs` on jobs; `$ORUN_OUTPUT` capture at seal into `artifacts/`
  (`specs/orun-object-model/design.md:83`); downstream
  `${{ jobs.x.outputs.y }}`.
- Sensitive outputs → run-scoped secret references; DEK destroyed at GC.
- Outcome: GitHub-Actions-grade job outputs + safe credential hand-off.

## SEC6 — Materialization (SD-13)
- `materialize` on `ExecutionProfile` (`data-model.md` §2.4); compile-time
  subset check; explicit materialize plan step.
- Adapter registry with v1 adapter `cloudflare-worker` (over `SetWorkerSecret`,
  `internal/cloudflare/client.go:437`); target binding derived from the
  provisioned entity.
- `secret_syncs` table + `POST/GET …/secrets/syncs`; stamp the provisioned
  entity facet; `superseded`/`orphaned` lifecycle (Invariant 10).
- `orun secrets rotate` raises `onRotate` system triggers; `orun secrets syncs`
  CLI; convergence view in operations.
- Outcome: deployed applications receive secrets through one governed,
  recorded, rotation-aware door.

## SEC7 — Break-glass + dashboard + scorecards + rotation UX
- `POST …/secrets/{key}/reveal` (elevated, alerted) + `orun secrets reveal`.
- Orun Cloud console: secrets/env-chain, three-tier policies + test matrix,
  per-entity facet, audit, GitHub App surfaces (`platform-integration.md` §4).
- `secret-hygiene` scorecard rules over the facet (`platform-integration.md`
  §2), including the pre-computed live-plane booleans they need.
- Rotation policies + expiry reminders from `rotation_policy`.
- Outcome: operational completeness; secret posture is part of the maturity
  ladder.

## Deferred (post-v1, register in `risks-and-open-questions.md`)
- Additional materialization adapters (AWS SSM/Secrets Manager, GitHub repo
  secrets) — Q-7.
- Inbound external-provider sync (pull from AWS/Cloudflare stores) via the
  envelope `provider` seam.
- Dynamic/leased secrets (Vault-style).
- CEL `expr` predicates behind a capability flag (SD-7), shared with
  scorecards.
- Arbitrary environment inheritance graphs (beyond `base → env → personal`).
- Self-hosted-backend KEK custody hardening (Q-2).
