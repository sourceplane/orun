# Runner integration — resolution, injection, redaction, materialization, provenance

> How a `secret://` reference becomes plaintext in a child process — or in a
> deployed platform's native store — and nowhere else. Covers resolution
> against `orun-api`, env injection at step launch, the log redactor,
> execution-platform awareness, materialization (SD-13), offline behavior, and
> sealed-run provenance. Schemas in `data-model.md`; policy in
> `policy-model.md`; the catalog/operations view in `platform-integration.md`.

## 1. Where resolution happens

The runner builds each step's environment in `stepExecContext`
(`internal/runner/runner.go:1320-1350`), which today merges base/job/step env. Add
a **resolve-then-inject** stage that runs *after* the plan is loaded and *only* in
the runner — never in expand/plan/render:

Resolution goes through the **orun-cloud contract's run-scoped, lease-bound
resolve endpoint** — `POST …/state/runs/{runId}/secrets/resolve` with the job's
live lease (`data-model.md` §8) — not a bespoke route. This is the OC5 slice of
the secret store; orun-secrets specifies what that endpoint resolves.

```
stepExecContext(job, step):
  base ← OS + job.Env + injected ORUN_* + step.Env          # existing, all non-secret
  keys ← job.secretRefs ∩ (step scope)                       # from plan.json (references only)
  if keys non-empty:
      # POST …/state/runs/{runId}/secrets/resolve { runnerId, jobId, keys } (live lease, secret.value.use)
      resolved ← secretclient.Resolve(runId, jobId, keys, triggerContext)   # → { secrets, ttlSeconds }
      redactor.Add(resolved.values)                          # seed the log masker BEFORE any output
      env ← merge(base, resolved.asEnv)                      # secret env is highest precedence, NON-persisted
  return execContext{ Env: env }                             # env lives only in this child process
```

Key properties:
- **Lease-bound + fail-closed (contract discipline).** The resolve requires the
  job's live lease and the `secret.value.use` action; a failed resolve fails the
  dependent job *before its step starts*, with the platform error surfaced
  verbatim (`orun-cloud/design.md` §6–7). Independent jobs continue.
- **TTL'd values.** The response carries `ttlSeconds` (contract default 300);
  the in-memory cache honors it and re-resolves on expiry within a long job.
  Each allow emits a `secret.accessed` event server-side (the contract's audit
  hook, joined to orun-secrets' `secret_audit`, `policy-model.md` §8).
- `resolved.asEnv` is merged into the child process environment **only**. It is
  never written back to `PlanJob.Env`, the plan, refs, or any L0 object
  (Invariant 1, `design.md` §11).
- The server walks the env chain per key — `personal(env, caller) → env → base`
  (`data-model.md` §1.1) — so personalization and inheritance are entirely
  server-side; the runner sends only references. Personal overlays are served
  exclusively when the server-derived platform fact is `local-cli` and the
  caller owns them (Invariant 9). The resolve response flags which keys were
  served from a personal overlay, and the runner prints a one-line notice
  (`2 secrets personally overridden: DB_URL, SMTP_HOST`) so local behavior is
  never silently different.
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
    // POST …/state/runs/{runId}/secrets/resolve (lease-bound, contract §4)
    Resolve(ctx, runID, jobID string, keys []string, tc TriggerContext) (Resolved, error)
}
```

The request carries the run/job ids (which scope the live lease), the declared
keys, the `TriggerOccurrence` facts (`internal/triggerctx/context.go:61-76`),
and the bearer token; the response is `{ secrets: map[string]string,
ttlSeconds }` on allow or a typed `denials[]` on deny. Transport, timeouts, and
retry reuse `client.go` (5s connect / 30s read). The same `TokenSource` decides
the server-derived `platform` fact (§3), so the runner never self-reports it.

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
  run-scoped `…/secrets/resolve` path, so the same policy + audit + redaction apply.

## 6. Materialization — the deploy job carries the last mile (SD-13)

When a job's profile declares `materialize:` (`data-model.md` §2.4), the runner
executes an explicit **materialize step** after the deploy step succeeds:

```
materializeStep(job):
  refs ← job.materialize.secrets mapped to job.secretRefs       # subset, checked at compile
  resolved ← (reuse the job's resolve cache — same decision, same audit)
  adapter ← adapters[job.materialize.target]                    # typed, versioned with the composition
  for each (key, value):
      adapter.Put(targetBinding(job), key, value)               # e.g. SetWorkerSecret (client.go:437)
  POST …/secrets/syncs { key, version, target, entityRef, execId }   # provenance (Invariant 10)
```

Properties:
- **Same boundary.** Materialization performs no second resolve path — it uses
  the values the job's policy decision already authorized, so "may this run
  read it" and "may this run sync it" cannot diverge. Profiles that materialize
  prod secrets are expected to be CI/cloud-only by policy (a `local-cli` run
  would be denied at resolve).
- **Adapters are typed and few.** v1 ships `cloudflare-worker` (the
  `SetWorkerSecret` primitive orun already uses for its own KEK,
  `internal/cloudflare/client.go:437`). Adapters declare which target binding
  they write (derived from the provisioned entity, not free-form), so a
  materialize step cannot be aimed at an arbitrary endpoint. Additional
  adapters (AWS SSM/Secrets Manager, GitHub repo secrets) are a registry,
  deferred per Q-7.
- **Provenance, then catalog.** The sync rows stamp the provisioned entity's
  facet (`platform-integration.md` §1) and flip prior rows to `superseded`;
  `orun secrets rotate` raises the profile's `onRotate` trigger so convergence
  happens through the normal, plan-visible deploy path.
- **Failure is loud.** A partial sync (some keys written, then failure) is
  recorded per-key; the step fails, the run is unsealed-red, and operations
  shows exactly which targets converged. Re-running the deploy is idempotent
  (adapters overwrite by key).

## 7. Offline / local behavior (ties to Q-1)

orun is local-first and works fully offline (`remote-and-consumers.md:120-126`).
Secrets necessarily depend on the backend for the value, so:
- `orun plan` is **fully offline** — it only handles references and compile-time
  grant checks (which need policy, available from the local catalog/Stack).
- `orun run` that resolves a `secret://` reference requires backend connectivity
  **for that step**; a clean error (`secret resolution requires an Orun Cloud
  login; run \`orun auth login\` or set ORUN_TOKEN`) if unauthenticated.
- The sanctioned local path is the **personal overlay** (SD-11): `orun secret
  set <KEY> --env dev --personal` stores the developer's value in the backend,
  scoped to their GitHub user id and `local-cli` — synced across their
  machines, never visible to CI, and resolved through the same audited path.
  For *fully offline* runs, an `ORUN_SECRET_<KEY>` env override is available
  **only** when `platform == local-cli` and the policy permits it for that env
  (never prod). Personal overlays are preferred; the env override is the
  airplane-mode fallback. (Decision Q-1.)

## 8. Sealed-run provenance (Invariant 6)

At seal, the `ExecutionRun` records, per step, the resolved
`{key, version, decisionId}` (from the resolve response) — **never** the value.
This rides the existing sealed-execution tree (`execution.json` + `jobs/`,
`specs/orun-object-model/design.md:83`), so Orun Cloud operations can answer "which
secret versions did this run use, and under which policy rule?" from the object
graph + `secret_audit`, with no value anywhere in the answer.
