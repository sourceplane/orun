# Design — workflow actions v2 (the data-flow evolution)

> v1 (`specs/orun-workflows`) shipped the architecture; v2 ships the product.
> This doc fixes the verified problem (§1), the **`contract/v1`** wire contract
> (§3), the **connections grant** (§4), **outputs & cross-step data-flow** (§5),
> the **plan-pinned engine** (§6), **portability** (§7), **resume** (§8),
> **approval gates** (§9), the extended determinism law (§10), invariants (§11),
> and the sharpness register (§12). RFC 2119 keywords are binding.

## 1. Problem — five gaps, each verified in code

1. **The contract has no counterparty.** orun invokes the engine as
   `<engine> backend` and streams a JSON request — but torkflow's CLI implements
   `run` and `view` only. Every orun test exercises a fake shell script. The
   integration has never executed a real workflow, and cannot.
2. **The wire shapes disagree.** orun sends `credentials` — a plural,
   env-var-keyed map for the whole run. torkflow's per-step contract reads
   `credential` (singular: one resolved payload for the step's connection) plus
   `connections`. Field name and shape both mismatch; even with a `backend`
   mode, authentication would not work.
3. **Credentials are an unscoped blob in an unmapped namespace.** A plan step
   injects the job's *entire* resolved `SecretEnv`; a hook injects *all*
   blueprint secrets. And nothing maps orun's names (`GITHUB_TOKEN`) to the
   connection names workflow steps actually reference (`connection: github-app`).
   Over-grant and under-function at the same time.
4. **Data-flow dies at the step boundary.** The founding complaint was "call
   system A, take its JSON, pass it to system B." v1 solved it *inside* a
   workflow and re-created it *outside*: the workflow's final context is
   flattened into a log string; step N+1 cannot consume it. Worse, the sealed
   log carries the **entire** final context — unbounded, and redacted only by
   known-value matching.
5. **The engine and the workflow are less pinned than the spec claims.** No
   engine digest is materialized into `plan.json` (v1 spec §5 drift) — "which
   engine ran this" is ambient host state (`ORUN_TORKFLOW_ENGINE`). The workflow
   digest covers the file but not the action-store manifests it names. Workflows
   are repo-local files while the compositions that reference them travel as
   versioned OCI artifacts — a vetted golden path can reference a workflow the
   consuming repo doesn't have. (`StepSpec.RunDir` is also dead code.)

## 2. Goals / non-goals

**Goals**
- A **truthful, versioned wire contract** (`contract/v1`) tested from both
  sides, and a torkflow `backend` mode that implements it (§3).
- A **compile-checked, least-privilege credential grant**: only declared,
  mapped connections cross the boundary (§4).
- **Typed cross-step data-flow**: declared outputs, `{{ steps.X.outputs.Y }}`,
  validated at plan time; sealing becomes an allowlist (§5).
- The engine as **plan content**: digest in `plan.json`, OCI-resolvable (§6).
- **Workflows as portable artifacts** riding composition Stacks (§7).
- **Resume-from-failed-step** as an opt-in retry policy (§8).
- **Approval gates** as sealed run facts (§9).

**Non-goals**
- New providers, the secrets store itself, `postModule` hooks, in-process
  engine import, event-driven triggers, a visual builder (README phase
  boundaries). v1's invariants are not renegotiated — v2 only extends them.

## 3. `contract/v1` — a contract with two signatures on it

The wire contract becomes a **versioned, vendored artifact**: a JSON Schema plus
golden request/response fixtures, committed **identically to both repos**
(`internal/workflowbackend/contract/v1/` in orun; mirrored in torkflow). Both
CIs run conformance against the same bytes; drift is a failing test.

**Request** (orun → engine stdin):

```json
{
  "contract": "v1",
  "workflow": "wf/notify.yaml",
  "with": { "channel": "ops" },
  "connections": { "github-app": { "token": "…" }, "slack-main": { "token": "…" } },
  "metadata": { "jobId": "web@prod.deploy", "step": "notify" },
  "runDir": ".orun/wfruns/<execId>/<stepId>"
}
```

**Response** (engine stdout):

```json
{
  "contract": "v1",
  "status": "success | failed | paused",
  "outputs": { "email": "sam@acme.dev" },
  "steps": [ { "name": "Get_oncall", "status": "success", "durationMs": 412 } ],
  "error": ""
}
```

Binding rules:
- **`connections` replaces v1's `credentials`.** Keys are the workflow's own
  connection names; values are the resolved credential payloads. The engine
  fans a payload out to each step's `connection:` reference internally —
  bypassing its file registry — so orun's namespace question (§4) is answered
  *before* the boundary, not inside the engine.
- **`outputs` replaces the raw context dump.** The engine evaluates the
  workflow's declared `spec.outputs` expressions and returns only those. A
  workflow with no declared outputs returns `{}` — the full context never
  crosses the boundary.
- Both sides MUST reject an unknown `contract` value with a versioned error.
  `contract/v2` is a new vendored directory, never an edit to v1's fixtures.
- torkflow gains the **`backend` subcommand** implementing this (WX1). Its
  interactive `run`/`view` CLI is untouched — `backend` is a mode, not a
  rewrite.

## 4. The connections grant — least privilege, proven at compile time

A workflow file already declares which connections it uses; v2 makes orun read
them. The step/hook gains a `connections:` mapping from the workflow's
connection names to orun `secret://` references:

```yaml
- name: notify
  workflow: wf/notify.yaml
  connections:
    slack-main: secret://acme/api/prod/SLACK_BOT_TOKEN
  with: { channel: ops }
```

- **Compile-time grant check (MUST).** At `orun plan` (and blueprint
  validation), orun parses the pinned workflow file's declared connections.
  Every required connection MUST be mapped; a mapping to an undeclared
  connection is an error (catches typos and stale grants). The mapping —
  names and refs, never values — materializes into the plan/lock and folds into
  the checksum. The plan **is** the credential grant, reviewable in a PR diff.
- **Mapped-only injection (MUST).** At run time orun resolves exactly the
  mapped refs and injects them keyed by connection name. The job's wider
  `SecretEnv` and the blueprint's other secrets never cross the boundary. A
  compromised provider binary sees what the plan granted it, nothing else.
- v1's behavior (inject-everything) is removed, not deprecated: a `workflow:`
  step/hook whose file declares connections but maps none fails to compile.
  The migration is mechanical and the error message writes the block for you.

## 5. Outputs — data-flow across the step boundary

The workflow file declares named outputs; orun makes them consumable and seals
nothing else.

```yaml
# torkflow/v1 (WX4, torkflow-side)
spec:
  outputs:
    email: "{{ Steps.Get_oncall.user.email }}"
```

```yaml
# composition job (orun-side)
- name: get-oncall
  workflow: wf/oncall.yaml
  connections: { slack-main: secret://acme/api/prod/SLACK_BOT_TOKEN }
- name: page
  run: ./page.sh
  env: { ONCALL: "{{ steps.get-oncall.outputs.email }}" }
```

- **Names are intent (MUST).** Output *names* live in the digest-covered
  workflow file. At compile time orun parses them and validates every
  `steps.<id>.outputs.<name>` reference in later steps — an undeclared name is
  a plan error. The plan stays byte-identical: the reference grammar
  materializes, values never do.
- **Values are run facts (MUST).** At run time the engine returns declared
  outputs only (§3); orun injects them into subsequent steps' template/env
  context and seals them — redacted — into `.orun/` as part of the step record.
  The v1 full-context dump is deleted: sealing becomes an **allowlist**.
- **Structured substeps.** The `steps[]` timeline is sealed as data (not
  prose), and the cockpit renders a `workflow:` step's inner steps as nested
  nodes with status and duration — delivering what v1's §10 promised.
- An output whose value matches a resolved secret trips redaction before
  sealing or injection (the sweep already exists; outputs route through it).

## 6. The engine is plan content

- **Digest in the plan (MUST).** The resolved engine's content digest
  materializes into `plan.json` (alongside `compositionSources`) and into
  `provenance.lock` for hooks, closing the v1 §5 drift. At run time the engine
  is re-hashed and a mismatch is fail-closed — same rule as the workflow file.
- **OCI resolution.** The engine resolves like a composition source: an OCI
  reference locked to a digest (reusing `internal/composition` fetch + lock).
  `ORUN_TORKFLOW_ENGINE` is demoted to a documented dev override that *still
  records* the digest it ran.
- **`runDir` becomes real.** orun provisions a per-step scratch dir under the
  run tree and passes it; the engine keeps its own run state there. It is an
  input to sealing, never the durable record (v1 S-2 unchanged).

## 7. Portability — workflows travel with the paths that call them

A composition that references `workflow: wf/notify.yaml` currently assumes the
consuming repo carries that file. v2 lets a composition package **ship its
workflows in the same OCI Stack**, resolved and pinned by the same lock file
that pins the composition — so a golden path is self-contained again.

- A packaged workflow resolves through the existing source machinery; its
  digest is computed from the *packaged* bytes, so the plan pin is identical
  whether the file is local or pulled.
- **The digest widens (MUST).** `WorkflowDigest` folds in the action-store
  module manifests the workflow references — a provider contract change flips
  the digest even when the workflow file is unchanged (closes the v1 follow-on).

## 8. Resume — durability without re-execution

`retry:` on a workflow step gains an opt-in mode:

```yaml
retry: { attempts: 2, resume: true }
```

- Default stays **re-run** (the workflow executes from the top — v1 behavior,
  correct for idempotent flows). With `resume: true`, orun passes the prior
  attempt's `runDir` back and the engine — which already keeps per-step,
  file-backed state — re-executes **only** steps that did not succeed.
- Resume MUST be attempted only when the workflow digest and the engine digest
  both match the failed attempt's (a changed workflow resumes nothing; it
  re-runs). The sealed record marks which steps were replayed vs. skipped.

## 9. Approval gates — a pause is a run fact

A workflow step MAY declare an approval:

```yaml
- name: promote
  workflow: wf/promote.yaml
  approval:
    prompt: "Promote to production?"
    timeout: 24h
    onTimeout: fail        # fail | proceed — declared, never ambient
```

- The run **pauses before invoking the engine**: the pause is sealed as a run
  fact, surfaced in `orun status`/TUI, and resolved by `orun approve <jobId/step>`
  (or the cockpit). The decision — who, when, verdict — seals into `.orun/`.
- The plan carries only the declaration (prompt, timeout, policy): a plan with
  an approval is byte-identical whether it was later approved or rejected. The
  *decision* is execution, exactly like an output value (§10).
- `onTimeout` MUST be declared; a gate with no timeout policy fails validation
  (S-6: a forgotten gate must not hang CI forever silently).
- Scaffolding phases reuse the same primitive for their planned approval gates
  — one pause mechanism, both surfaces, matching v1's one-backend discipline.

## 10. The extended law

v1: *only the workflow's reference + digest + declared inputs are durable; the
outcome is a logged run fact.* v2 restates it more generally and applies it to
every new feature:

> **Only names are intent; values are execution.**

| Feature | Intent (plan/lock, digest-covered, compile-checked) | Execution (sealed run facts) |
|---|---|---|
| Connections (§4) | connection **names** + `secret://` **refs** | resolved credential values (in-memory only, never sealed) |
| Outputs (§5) | output **names**; `steps.X.outputs.Y` references | output **values** (allowlist-sealed, redacted) |
| Engine (§6) | engine **digest** | the invocation |
| Resume (§8) | the retry **policy** | which steps replayed |
| Approvals (§9) | prompt/timeout/**policy** | the pause, the verdict, the approver |

The v1 field test still decides every future addition: *would it differ between
two runs with identical inputs?* If yes, it is execution.

## 11. Invariants

v1 invariants 1–8 carry forward verbatim. v2 adds:

9. **The contract is bilateral.** Both repos vendor identical `contract/v1`
   fixtures and run conformance in CI; a wire change is a versioned new
   contract, never an edit (§3).
10. **Nothing unmapped crosses.** Credentials reach the engine only through the
    compile-checked `connections:` grant; the plan is the auditable grant (§4).
11. **Only names are intent.** Output/connection names and policies are plan
    content; their values are sealed run facts (§5, §10).
12. **Sealing is an allowlist.** The run record carries declared outputs and the
    structured timeline — never an unbounded context dump (§5).
13. **The engine is plan content.** Its digest materializes and is re-verified
    fail-closed, like the workflow itself (§6).

## 12. Sharpness register

| # | Sharp edge | Resolution |
|---|-----------|-----------|
| S-1 | **Contract drift returns** — the two repos evolve the wire shape independently again | vendored identical fixtures + conformance in both CIs (invariant 9); unknown `contract` values rejected with a versioned error; a change is `contract/v2`, additive (§3). |
| S-2 | **Grant check needs to parse workflow YAML in the compiler** — a malformed file breaks planning | the file is already read for digesting; parsing extracts only `spec.connections`/`spec.outputs` names with a tolerant reader; a file the reader cannot parse fails compilation *for workflow steps only* — fail-closed and scoped (§4, §5). |
| S-3 | **Output injection becomes a covert channel for secrets** — a workflow exports a token as an "output" | outputs route through the same redaction sweep as all sealed content; a value matching any resolved secret is masked before injection or sealing (§5). |
| S-4 | **Cross-step coupling breaks job parallelism** — `steps.X.outputs.Y` implies ordering | outputs are consumable only within the same job's later steps (steps are already sequential); cross-**job** output flow is out of scope and rejected at compile time — jobs communicate through the DAG, not ambient state (§5). |
| S-5 | **Resume replays against a changed world** | resume requires matching workflow **and** engine digests, else falls back to re-run; the sealed record marks replayed vs. skipped steps (§8). |
| S-6 | **An approval gate hangs CI forever** | `timeout` + declared `onTimeout` are mandatory; the pause is visible in `orun status` and the gate seals a timeout verdict like any other decision (§9). |
| S-7 | **Packaged workflows drift from local ones** | one digest function over the resolved bytes — the plan pin is source-agnostic; the lock records where it came from (§7). |
| S-8 | **The grant migration breaks v1 users** | the compile error prints the exact `connections:` block to paste (names from the workflow file, refs from the job's existing `secretRefs`); v2.32 plans without workflow steps are untouched (§4). |
| S-9 | **Approval state leaks into the plan** | the plan carries only the declaration; pause/verdict/approver are sealed run facts — asserted by the existing plan-hash determinism test extended to approval fixtures (§9, §10). |
