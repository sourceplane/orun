# Implementation plan

> Milestones `SEC0 → SEC6`. Each lands behind tests and is independently useful.
> Sequencing keeps the leak-prevention invariants (`design.md` §10) true at every
> step: references and redaction land *before* any value can be resolved.

## SEC0 — Reference type + leak guard (no backend yet)
- Add the `secret://` grammar + parser (`internal/secretref`, `data-model.md` §1).
- Add `secretEnv` to `Component`/`intent` model (`internal/model/intent.go`) and
  `secretRefs` to `PlanJob` (`internal/model/plan.go`); render references in
  `internal/render/plan.go` (extend, don't touch `buildEnv`).
- **Leak guard:** planner rejects any `secretEnv` value that is a literal rather
  than a `secret://` reference. Add the `fsck`-style scanner asserting no value in
  any object/plan/log (Invariant 1).
- Outcome: references compile and render; **impossible to put a value in `env`'s
  secret slot**. No resolution yet.

## SEC1 — Backend store + crypto (write path)
- D1 migrations: `secret_metadata`, `secret_versions`, `secret_deks`, `secret_audit`
  (`data-model.md` §6), added to `internal/backendbundle`.
- KEK as a Worker secret; per-namespace DEK wrap/unwrap; AES-256-GCM envelope
  reusing the `config-worker` adapter shape
  (`multi-tenant-saas/apps/config-worker/src/encryption.ts`).
- Routes: `POST /v1/secrets` (put), `GET /v1/secrets` (metadata), `rotate`,
  `revoke`. **Write-only — no resolve/reveal yet.**
- `orun secret set/list/rotate/revoke` (`cli-surface.md` §1).
- Outcome: secrets can be stored encrypted and listed by metadata; nothing can
  read a value.

## SEC2 — GitHub identity + policy engine (decision, no values)
- GitHub App installation + `gh_identity_map` ingestion + membership webhooks
  (`data-model.md` §6).
- `internal/secretpolicy`: `SecretPolicy` document parse/validate, the locked
  predicate vocabulary, deny-by-default evaluation, narrow-only overlay check
  (`policy-model.md` §5–6).
- Routes: `POST /v1/policies` (put), `POST /v1/policies/evaluate` (dry-run).
- `orun policy list/show/test/lint` (`cli-surface.md` §2).
- Stack packaging: discover `policies/*.SecretPolicy.yaml` in a Stack
  (`website/docs/concepts/stacks.md`), so policy ships with compositions.
- Outcome: full decision engine, testable via `orun policy test`, **still no value
  resolution**.

## SEC3 — Resolve path + runner injection + redaction (the value finally flows)
- `POST /v1/secrets/resolve`: four-axis decision → DEK unwrap → decrypt → audit →
  plaintext over TLS (`data-model.md` §7).
- `internal/secretclient` over the existing remote client; `Resolve` in
  `stepExecContext` (`internal/runner/runner.go:1320-1350`).
- **Redactor** seeded at resolve, applied at `AfterStepLog`
  (`internal/runner/runner.go:579`) before any blob write (Invariant 5).
- Compile-time grant check in the planner (existential subject); surface in
  `orun plan` + `--json`.
- Platform-fact derivation from auth mode (`runner-integration.md` §3).
- Outcome: end-to-end Doppler-grade flow — declare references, `orun run` injects
  values, logs are redacted, decisions are audited.

## SEC4 — Composition `secretBindings` + provenance
- `secretBindings` on `JobTemplate`/`ExecutionProfile`
  (`internal/model/composition.go`); planner maps bindings → references for
  `(namespace, env)`; required-but-ungrantable → compile error.
- Sealed-run provenance: record `{key, version, decisionId}` per step in the
  execution tree (Invariant 6, `runner-integration.md` §7).
- Outcome: portable, component-aware requirements ride the Stack; runs are
  audit-complete.

## SEC5 — Inter-job outputs (SD-9)
- `outputs` on jobs; `$ORUN_OUTPUT` capture at seal into `artifacts/`
  (`specs/orun-object-model/design.md:83`); downstream `${{ jobs.x.outputs.y }}`.
- Sensitive outputs → run-scoped secret references; DEK destroyed at GC.
- Outcome: GitHub-Actions-grade job outputs + safe credential hand-off.

## SEC6 — Break-glass reveal + dashboard + rotation UX
- `POST /v1/secrets/reveal` (elevated, alerted) + `orun secret reveal`
  (`cli-surface.md` §1).
- Orun Cloud console: secrets/policies/audit/GitHub-App surfaces
  (`cli-surface.md` §4); `policy test` matrix.
- Rotation policies + expiry reminders from `rotation_policy`.
- Outcome: operational completeness; the hosted experience.

## Deferred (post-v1, register in `risks-and-open-questions.md`)
- External-provider sync (AWS Secrets Manager / Cloudflare Secrets Store) via the
  envelope `provider` seam.
- Dynamic/leased secrets (Vault-style).
- CEL `expr` predicates behind a capability flag (SD-7).
- Self-hosted-backend KEK custody hardening (Q-2).
