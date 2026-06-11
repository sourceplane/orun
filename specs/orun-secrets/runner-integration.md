# Runner integration — resolution, injection, redaction, provenance

> How a `secret://` reference becomes plaintext in a child process and nowhere
> else. Covers resolution against `orun-api`, env injection at step launch, the
> log redactor, execution-platform awareness, offline behavior, and sealed-run
> provenance. Schemas in `data-model.md`; policy in `policy-model.md`.

## 1. Where resolution happens

The runner builds each step's environment in `stepExecContext`
(`internal/runner/runner.go:1320-1350`), which today merges base/job/step env. Add
a **resolve-then-inject** stage that runs *after* the plan is loaded and *only* in
the runner — never in expand/plan/render:

```
stepExecContext(job, step):
  base ← OS + job.Env + injected ORUN_* + step.Env          # existing, all non-secret
  refs ← job.secretRefs ∩ (step scope)                       # from plan.json (references only)
  if refs non-empty:
      resolved ← secretclient.Resolve(refs, triggerContext, authToken)   # POST /v1/secrets/resolve
      redactor.Add(resolved.values)                          # seed the log masker BEFORE any output
      env ← merge(base, resolved.asEnv)                      # secret env is highest precedence, NON-persisted
  return execContext{ Env: env }                             # env lives only in this child process
```

Key properties:
- `resolved.asEnv` is merged into the child process environment **only**. It is
  never written back to `PlanJob.Env`, the plan, refs, or any L0 object
  (Invariant 1, `design.md` §10).
- Resolution is **batched per job** (one round-trip), cached for the job's
  lifetime in memory, and zeroed on job completion.
- A denial (`policy-model.md` §5) fails the step with a typed error and the audit
  `decisionId` — no value, no partial leak.

## 2. The secret client

`internal/secretclient` (new) wraps the existing remote-state HTTP client
(`internal/remotestate/client.go`) and reuses its `TokenSource`
(`internal/remotestate/auth.go`): OIDC in CI, session locally, `ORUN_TOKEN` for
service principals. It adds:

```go
type Resolver interface {
    Resolve(ctx, refs []SecretRef, tc TriggerContext) (Resolved, error) // POST /v1/secrets/resolve
}
```

The request carries the references, the `TriggerOccurrence` facts
(`internal/triggerctx/context.go:61-76`), and the bearer token; the response is
`{ asEnv: map[string]string }` on allow or a typed `denials[]` on deny. Transport,
timeouts, and retry reuse `client.go` (5s connect / 30s read).

## 3. Execution-platform awareness

The platform fact is derived from the resolved auth mode, not asserted by the
caller:

| `ResolvedMode` (`auth.go:157-208`) | + signal | `platform` fact |
|---|---|---|
| `oidc` | `ACTIONS_ID_TOKEN_REQUEST_URL` present | `github-actions-oidc` |
| `session` | local login | `local-cli` |
| `static` (`ORUN_TOKEN`) | cloud runner marker | `orun-cloud-runner` |
| `static` (`ORUN_TOKEN`) | otherwise | `service-token` |

Because the platform is derived server-side from the token kind (the OIDC JWT is
cryptographically bound to GitHub Actions; a laptop cannot forge it), a
`platform == "github-actions-oidc"` condition is a real trust boundary, not a
self-report. This is what lets a `SecretPolicy` safely say "prod secrets only from
CI, never from `local-cli`" (`policy-model.md` §9).

## 4. Log redaction (Invariant 5)

All step output funnels through one hook, `AfterStepLog`
(`internal/runner/runner.go:579` → `internal/objrun/objrun.go:148-155`), which is
the single point where logs become content-addressed blobs. Insert a redactor
**before** the blob write:

```
redactor.Filter(output) :=
  for each registered value v (and encodings: raw, base64, url-encoded, json-escaped):
     replace occurrences of v with "***"
  return masked
```

- Seeded at resolve time (§1), so a value is registered **before** any step that
  could echo it runs.
- Applied to both the streamed presenter output and the persisted blob, so neither
  the live cockpit nor the sealed log leaks.
- Short values (< 4 chars) are not masked (avoids `***`-flooding); the policy
  layer should forbid trivially short secrets.
- The redactor is per-run and discarded at seal; it never persists the value set.

## 5. Inter-job outputs (SD-9)

- A job declares `outputs` in its composition/job spec; the runner exposes
  `$ORUN_OUTPUT` (file, like the existing `$ORUN_ENV`,
  `website/docs/concepts/runtime-environment.md:210-236`).
- At job seal, the runner reads `$ORUN_OUTPUT`:
  - **non-sensitive** → stored in the execution tree's `artifacts/` slot
    (`specs/orun-object-model/design.md:83`) as content; resolvable by downstream
    jobs via `${{ jobs.<id>.outputs.<name> }}` at plan/runtime.
  - **sensitive** (`outputs.<n>.sensitive: true`) → `putSecret` into a **run-scoped**
    namespace (`secret://<ns>/_run/<execId>/<name>`); only the reference is stored
    in `artifacts/`; the DEK is destroyed when the run is GC'd. Redacted like any
    secret.
- Downstream resolution of a sensitive output goes through the same
  `/v1/secrets/resolve` path, so the same policy + audit + redaction apply.

## 6. Offline / local behavior (ties to Q-1)

orun is local-first and works fully offline (`remote-and-consumers.md:120-126`).
Secrets necessarily depend on the backend for the value, so:
- `orun plan` is **fully offline** — it only handles references and compile-time
  grant checks (which need policy, available from the local catalog/Stack).
- `orun run` that resolves a `secret://` reference requires backend connectivity
  **for that step**; a clean error (`secret resolution requires an Orun Cloud
  login; run \`orun auth login\` or set ORUN_TOKEN`) if unauthenticated.
- A local-only escape hatch — `ORUN_SECRET_<KEY>` env overrides for dev — is
  available **only** when `platform == local-cli` and the policy permits it, so a
  developer can run without the cloud while prod paths stay locked. (Decision Q-1.)

## 7. Sealed-run provenance (Invariant 6)

At seal, the `ExecutionRun` records, per step, the resolved
`{key, version, decisionId}` (from the resolve response) — **never** the value.
This rides the existing sealed-execution tree (`execution.json` + `jobs/`,
`specs/orun-object-model/design.md:83`), so Orun Cloud operations can answer "which
secret versions did this run use, and under which policy rule?" from the object
graph + `secret_audit`, with no value anywhere in the answer.
