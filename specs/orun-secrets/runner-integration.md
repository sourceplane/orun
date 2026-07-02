# Runner integration (v3) — resolution, injection, redaction, materialization, provenance

> How a `secret://` reference becomes plaintext in a child process — or in a
> deployed platform's native store — and nowhere else. v3 binds every step to
> the seams verified in the shipped code: the live job lease (`leaseEpoch`),
> the exact env-merge and log-capture points in the runner, and the auth spine
> the CLI already resolves. Schemas in `data-model.md`; policy in
> `policy-model.md`; the catalog/operations view in `platform-integration.md`.

## 1. Where resolution happens

Resolution goes through the contract's run-scoped, **lease-bound** resolve —
`POST …/state/runs/{runId}/secrets/resolve` with `{runnerId, jobId, leaseEpoch,
refs}` (`data-model.md` §8, SD-18) — after the job is claimed and before its
first step launches. Never in expand/plan/render.

```
runJob(job):                                        # after ClaimJob → {leaseEpoch, leaseExpiresAt}
  refs ← job.secretRefs                             # from plan.json (references only)
  if refs non-empty:
      resolved ← secretresolve.Resolve(runID, jobID, leaseEpoch, refs, triggerFacts)
      redactor.Add(resolved.values + encodings)     # seed the masker BEFORE any output
      runner.SecretEnv ← resolved.asEnv             # in-memory only, zeroed at job seal

stepExecContext(job, step):                         # runner.go:1336-1367
  Env ← MergeEnvironment(BaseEnv, JobEnv, StepEnv, SecretEnv)   # SecretEnv is the NEW top layer (runner.go:1365)
```

Key properties:

- **Lease-bound + fail-closed.** The server verifies token authz **and** the
  live lease `(runId, jobId, runnerId, leaseEpoch)` independently (SD-18); a
  lapsed/swept lease yields `409 lease_lost` — the same discipline heartbeat
  and complete already follow (`coordination-native.ts:209,239`). A failed
  resolve fails the dependent job *before its step starts*; independent jobs
  continue.
- **TTL'd values.** The response carries `ttlSeconds` (default 300); the
  in-memory cache honors it and re-resolves (with a fresh lease check) within
  long jobs. Each allow emits `secret.accessed` server-side.
- `resolved.asEnv` merges into the child-process environment **only** — via a
  new `SecretEnv` field on `executor.ExecContext` (`executor.go:25-38`) added
  as the final argument at **`runner.go:1365`** and mirrored in the finalizer
  merge (**`runner.go:643`**) so post-job hooks see the same env. It is never
  written to `PlanJob.Env`, the plan, refs, or any L0 object (Invariant 1).
- **The server walks the chain per key** — `personal → environment → project →
  workspace → account` (`data-model.md` §1.1) — so inheritance and
  personalization are entirely server-side; the runner sends only references.
  The response's `resolved[]` records the serving scope and personal flag; the
  runner prints a one-line notice for personal serves
  (`2 secrets personally overridden: DB_URL, SMTP_HOST`) so local behavior is
  never silently different.
- **Batched per job** (one round-trip), cached in memory for the job's
  lifetime, zeroed on job completion.
- A denial fails the step with the typed reason code and the audit
  `decisionId` — no value, no partial leak.
- **Memoization safety:** hermetic-job memoization hashes env **keys only**
  (`internal/statebackend/coordbackend.go:31-38`); `secretRefs` keys + resolved
  versions join the input hash (a rotation invalidates the memo), values never
  do.

## 2. The secret client

`internal/secretresolve` (new) wraps the existing scoped remote client
(`internal/remotestate/client.go`, `NewClientWithScope`) and reuses
`ResolveAuth`/`TokenSource` (`internal/remotestate/auth.go:371-422`): OIDC in
CI, session locally, `ORUN_TOKEN` for service principals — the same spine
`CoordBackend` uses, so secrets add an endpoint, not an auth path.

```go
type Resolver interface {
    // POST …/state/runs/{runId}/secrets/resolve (lease-bound, SD-18)
    Resolve(ctx context.Context, runID, jobID string, leaseEpoch int,
        refs []string, facts TriggerFacts) (Resolved, error)
}
```

The request carries the run/job ids + `leaseEpoch` (scoping the live lease),
the declared refs, and the `TriggerOccurrence` facts
(`internal/triggerctx/context.go:61-76`); the response is
`{secrets, resolved[], ttlSeconds}` on allow or typed `denials[]`. Transport,
timeouts, and retry reuse `client.go`. Wire-up: resolution is invoked from the
run path next to `setupRemoteStateHooks` (`cmd/orun/command_run.go:434`),
populating nil-safe `Runner.SecretEnv` + `Runner.Redactor` fields — local
no-secret runs are untouched.

## 3. Execution-platform awareness

The platform fact is **server-derived from the verified actor kind**, never
self-reported (risk R-3):

| Client auth mode (`auth.go:371-422`) | Server-side actor (`resolve-bearer.ts`) | `platform` fact |
|---|---|---|
| `oidc` (GitHub Actions OIDC exchange) | `workflow` (15-min HS256 token bound to org/project) | `ci-oidc` |
| `session` (CLI login) | `user` via CLI JWT | `local-cli` |
| `static` (`ORUN_TOKEN` = `sk_` key) | `service_principal` | `service` |

The OIDC JWT is cryptographically bound to GitHub Actions (verified against
GitHub's JWKS with `aud == "orun-cloud"`, `oidc/github.ts:74-94`); a laptop
cannot forge it. `platform == "ci-oidc"` conditions are therefore a real trust
boundary, and the verified OIDC claims (`repository`, `ref`, `environment`)
flow into the trigger facts (`policy-model.md` §4.3).

## 4. Log redaction (Invariant 5)

Step output is captured at **`runner.go:575`** and fans out to **three sinks**:
view analysis (`analyzeStepOutput`, :591), the `AfterStepLog` hook (:595-597 —
feeding both the remote log pipeline, `command_run.go:689-700`, and the
objrun blob write, `internal/objrun/objrun.go:147-155`), and the GHA emitter
(:604). The redactor is applied **once, immediately after capture, upstream of
all sinks** — not inside the hooks, because remote setup *replaces* `r.Hooks`
(`command_run.go:633`) while objrun *chains* (`objrun.go:137-155`), making
hook-level redaction ordering-fragile.

```
output ← r.Executor.RunStep(...)          # runner.go:575
output ← redactor.Filter(output)          # NEW — the single redaction site
```

- `Filter` replaces every registered value — raw, base64, URL-encoded, and
  JSON-escaped forms — with `***`.
- Seeded at resolve time (§1), so a value is registered **before** any step
  that could echo it runs.
- Covers console, live tail, the remote log chunks, and the sealed blob alike.
- Values shorter than 4 chars are not masked (avoids `***`-flooding); the
  policy layer should forbid trivially short secrets.
- `internal/redact` (new) is per-run and discarded at seal; it never persists
  the value set. `Runner.Redactor` is nil-safe.

## 5. Inter-job outputs (SD-9)

- A job declares `outputs`; the runner exposes `$ORUN_OUTPUT` (file, like the
  existing `$ORUN_ENV`).
- At job seal:
  - **non-sensitive** → stored in the execution tree's reserved `artifacts/`
    slot as content; downstream `${{ jobs.<id>.outputs.<name> }}`.
  - **sensitive** (`sensitive: true`) → written to the secret backend as a
    **run-scoped** secret; only the reference lands in `artifacts/`; its key
    material dies at run GC. Redacted like any secret.
- Downstream resolution of a sensitive output goes through the same lease-bound
  resolve, so the same policy + audit + redaction apply.

## 6. Materialization — the deploy job carries the last mile (SD-13)

When a job's profile declares `materialize:` (`data-model.md` §2.3), the runner
executes an explicit **materialize step** after the deploy step succeeds:

```
materializeStep(job):
  refs ← job.materialize.secrets mapped to job.secretRefs      # subset, compile-checked
  resolved ← (reuse the job's resolve cache — same decision, same audit)
  adapter ← adapters[job.materialize.target]                   # typed, versioned with the composition
  for each (key, value):
      adapter.Put(targetBinding(job), key, value)              # v1: SetWorkerSecret (client.go:437)
  POST …/config/secrets/syncs { key, version, target, entityRef, runId }   # provenance (Invariant 10)
```

Properties:

- **Same boundary.** No second resolve path — materialization uses the values
  the job's decision already authorized, so "may read" and "may sync" cannot
  diverge. Profiles that materialize prod secrets are CI/service-only by
  policy (`local-cli` is denied at resolve).
- **Adapters are typed and few.** v1 ships `cloudflare-worker`; the target
  binding derives from the provisioned entity, never a free-form endpoint.
  More adapters (AWS SSM/SM, GitHub repo secrets) are a registry, deferred
  (Q-7). The platform's own `saas-secrets-sync` tooling (per-worker projection
  + fingerprint records + `wrangler secret bulk`) is the in-house prior art
  this adapter generalizes.
- **Provenance, then catalog.** Sync rows stamp the provisioned entity's facet
  and flip prior rows to `superseded`; `orun secrets rotate` raises the
  profile's `onRotate` trigger so convergence happens through the normal,
  plan-visible deploy path.
- **Failure is loud.** Partial syncs are recorded per-key; the step fails; the
  run seals red; operations shows exactly which targets converged. Re-running
  is idempotent (adapters overwrite by key).

## 7. Offline / local behavior (ties to Q-1)

orun is local-first and works fully offline. Secrets necessarily depend on the
backend for the value:

- `orun plan` is **fully offline** — references + compile-time checks only
  (policy documents are available locally from the Stack/intent tiers).
- `orun run` resolving a reference requires connectivity **for that job**; a
  clean error otherwise (`secret resolution requires Orun Cloud; run 'orun
  auth login' or set ORUN_TOKEN`).
- The sanctioned local path is the **personal overlay** (SD-11′): stored in the
  backend, scoped to the owning subject and `local-cli`, synced across the
  developer's machines, never visible to CI. For *fully offline* runs, an
  `ORUN_SECRET_<KEY>` env override is honored **only** when
  `platform == local-cli` and policy permits it for that env (never a
  protected env). Personal overlays are preferred; the env override is the
  airplane-mode fallback (Q-1).

## 8. Sealed-run provenance (Invariant 6)

At seal, the `ExecutionRun` records, per job, the resolved
`{key, version, decisionId}` from the resolve response — **never** the value.
This rides the existing sealed-execution tree, so operations can answer "which
secret versions did this run use, and under which rule?" from the object graph
+ the audit stream, with no value anywhere in the answer.
