# Design — workflow actions (a data-flow execution vocabulary)

> orun gains a third step/hook execution vocabulary — **`workflow:`** — served
> by the **torkflow** runtime. This doc fixes the model (§2), the two surfaces
> and the single backend they share (§3–§4), the execution boundary + engine
> pinning (§5), the secret bridge (§6), the **determinism/provenance law** (§7),
> failure & concurrency semantics (§8), the per-module deferral (§9),
> observability (§10), the invariants (§11), and the sharpness register (§12).
> RFC 2119 keywords are binding.

## 1. Problem

1. **orun has two execution vocabularies and both are opaque.** A `PlanStep` is
   either `run:` (a shell command → unstructured text on stdout) or `use:` (a
   GitHub Actions action → a foreign runtime). Neither models *typed,
   authenticated, multi-step data-flow*. The moment a delivery step must "call
   system A with a credential, take its JSON, transform it, and call system B
   with the result," the author drops to `curl | jq` inside a `run:` block —
   secrets smeared through argv, no retries, no structure, nothing orun can see.
2. **That exact runtime already exists next door.** torkflow is a thin workflow
   engine: a DAG of `actionRef` steps, each a provider binary over a JSON
   stdin/stdout contract, wired by a `{{ Steps.X.output }}` + Goja expression
   resolver, with connection-bound credentials, retries, readiness gates, and a
   file-backed run store. It is the data-flow layer orun is missing — and its
   architecture (pinned provider binaries, a pure JSON contract) is already
   shaped to drop under orun's executor seam.
3. **Scaffolding has a hook seam with the same shape and the same gap.**
   `orun-scaffolding` §12 reserves a **declared-hooks** point for ecosystem
   post-steps ("run *after* placement, *outside* the sandbox"), but a hook today
   is bare `run: [argv]`. The canonical scaffolding side effects — *make sure the
   GitHub repo exists*, *commit the generated tree and open a PR* — are precisely
   authenticated, multi-call, retry-worthy provider actions. Expressed as argv
   they are brittle; expressed as a workflow they are first-class.
4. **Two naive integrations would fork the model.** Wiring torkflow into plan
   steps and into scaffolding hooks as two independent features would duplicate
   the engine-invocation path, the secret handling, and the pinning — and would
   invite two different answers to the determinism question. They are the **same
   backend** used in two places and MUST be built as one.
5. **A runtime engine threatens orun's headline property.** orun's whole value
   is a deterministic, reviewable plan and a fail-closed, provenanced scaffold. A
   workflow with live HTTP calls, `{{ }}` JS, and dynamic branching is inherently
   non-deterministic. Left unbounded, its outcome could leak into `plan.json` or
   `provenance.lock` and destroy determinism. The design's central job is to
   **confine the workflow to the execution side of a hard line.**

## 2. Goals / non-goals

**Goals**
- **One** execution vocabulary (`workflow:`) and **one** backend
  (`workflowExecutor` + a pinned torkflow engine) serving **two** surfaces — a
  plan step and a blueprint hook — with no duplicated invocation, secret, or
  pinning path (§3–§6).
- Preserve **plan determinism** and **scaffold fail-closed/provenance** by
  pinning the workflow (and engine) by digest and materializing **only** the
  reference + digest + declared inputs — never the outcome (§5, §7).
- A **secret bridge** so credentials come from `orun-secrets` in memory, never a
  second `secrets.yaml`, never on disk, always redacted from captured output
  (§6).
- **One audit trail:** the workflow run is sealed into `.orun/` as the step's/
  hook's output, not left in a parallel `.runs/` tree (§7, §10).
- An **ecosystem-neutral core:** orun ships no provider-specific string; every
  `actionRef` (slack/http/github/…) lives in torkflow's action store (§11
  invariant 7).

**Non-goals**
- torkflow's engine internals, provider SDK, or any specific provider — authored
  and released in torkflow (a `github` provider for the create-repo/open-PR
  examples is torkflow-side).
- The `orun-secrets` store and policy engine (consumed via `secret://`
  resolution, not re-specified).
- **Per-module** scaffolding hooks (`postModule`) — designed in §9, deferred.
- An in-process Go import of the engine (§13, follow-on); rewriting orun's
  planner/scheduler around torkflow's DAG (orun stays the compiler).

## 3. The model — a third vocabulary at the same altitude as `run:`/`use:`

A step (and a hook) is **exactly one** of three forms. This is the whole grammar
change:

```yaml
# in a composition job (SURFACE A) — sits beside the two existing forms
steps:
  - name: build          # form 1: shell (unchanged)
    run: make build
  - name: deploy         # form 2: GitHub Actions action (unchanged)
    use: some/action@v1
    with: { arg: value }
  - name: notify-oncall  # form 3: NEW — a data-flow workflow
    workflow: workflows/notify-oncall.yaml   # a torkflow/v1 file, resolved + pinned
    with:                                    # declared inputs → the workflow's Trigger context
      channel: "{{ .env.SLACK_CHANNEL }}"
    timeout: 5m                              # the step's existing knobs still apply
    retry: 1
    onFailure: stop
```

```yaml
# in a blueprint (SURFACE B) — upgrades the §12 run:[argv] hook seam
hooks:
  preInstantiate:                # idempotent preconditions only (§7, §8)
    - id: ensure-repo
      workflow: workflows/ensure-repo.yaml     # get-or-create; safe to re-run
      with: { org: "{{ .orgName }}", name: "{{ .serviceName }}" }
  postInstantiate:               # after the output gate passes (§8)
    - id: open-pr
      workflow: workflows/open-pr.yaml         # git commit/push + createPullRequest
      with: { branch: "scaffold/{{ .serviceName }}" }
```

**Why one model, not two dressed alike:**
- **The referenced artifact is identical** — a portable `torkflow/v1` file (its
  own `apiVersion`, unchanged and independently runnable). Neither surface forks
  the workflow format.
- **`with` is the single input channel** on both surfaces: a map of declared
  inputs, templated at compile/scaffold time against the same context the
  surface already resolves (a step's env/inputs; a blueprint's validated
  `inputs`), then handed to the engine as the workflow's `Trigger` context.
- **Mutual exclusion is one rule.** A step/hook with more than one of
  `run`/`use`/`workflow` set is a **compile error** (surface A) / **blueprint
  validation error** (surface B). This mirrors the existing `localExecutor`
  guard that already refuses a `use:` step under the local runner.

## 4. The two surfaces share one backend

Both surfaces reduce to the same four operations, implemented once:

```
resolve+pin  →  bind inputs  →  execute (shell pinned engine)  →  seal result
(§5)            (§3 `with`)      (§5 JSON contract, §6 secrets)   (§7 into .orun/)
```

- **Surface A (plan step)** runs the four ops **inside `orun run`**, as one step
  of a job, under whichever runner is active (`local`/`docker`/`gha`). Because
  the executor only shells a pinned engine, a `workflow:` step runs under **any**
  runner — unlike `use:`, which forces `github-actions`. (Availability of the
  engine + provider binaries in that runtime is the runner's concern, §12 S-4.)
- **Surface B (blueprint hook)** runs the four ops **inside `orun new`/
  `instantiate`**, in the hook phase — `preInstantiate` before placement,
  `postInstantiate` after the output gate — outside the template sandbox, exactly
  where §12 already puts hooks.

The shared implementation is a `workflowbackend` package (invocation + JSON
contract + secret injection + sealing) and a thin `workflowExecutor` in
`internal/executor` that adapts it to the `Executor` interface for surface A;
surface B calls `workflowbackend` directly from the scaffolder's hook runner. No
second engine-invocation path exists.

## 5. The execution boundary — pin the engine and the workflow by digest

The line that protects determinism is drawn at **content addressing**, the
mechanism orun already uses for compositions, sources, and docs blobs.

- **The workflow file is pinned.** At compile time (surface A: during bind/
  render, when steps are materialized) or scaffold time (surface B: when the
  blueprint is resolved), orun reads the referenced workflow file, canonicalizes
  it, and computes `workflowDigest = sha256(file ⊕ referenced action-store
  manifests)`. The digest covers the workflow **and** the `actionRef` module
  manifests it names, so a change in either flips it. The step/hook in the durable
  artifact carries `{ workflow: <ref>, workflowDigest: <sha256>, with: {…} }`.
- **The engine is pinned.** The torkflow engine itself is resolved as a
  **packaged provider artifact** (OCI/`internal/composition` fetch), locked to a
  digest in the plan's `compositionSources`-style source list (surface A) or the
  provenance lock (surface B). "Which engine version ran this" is reproducible,
  not ambient `$PATH`.
- **The contract is a subprocess over JSON.** `workflowExecutor` invokes the
  pinned engine as a child process (the same boundary torkflow uses for its own
  providers, `internal/executor/binary.go`). orun writes a request —
  `{ workflow, with (as Trigger), credentials, metadata: {jobId/component/env or
  blueprint/inputs-hash}, runDir }` — to stdin and reads the final context as
  JSON from stdout. No Go import of the engine is required in v1 (it is
  `internal/` in torkflow); the process boundary is the contract (§13).

Because only the **reference + digest + declared inputs** are ever written into
`plan.json` / `provenance.lock`, the plan stays a pure function of its inputs and
folds the workflow into its checksum **without** folding in any live outcome.

## 6. The secret bridge (MUST)

A workflow that opens a PR needs a GitHub token; one that posts to Slack needs a
bot token. Credentials MUST come from `orun-secrets`, not a second store.

- **orun resolves, torkflow receives.** The step/hook declares credential needs
  the orun way — `secret://` references (surface A: the job's `secretRefs`;
  surface B: blueprint `inputs` marked `secret: true`). orun resolves them
  through `orun-secrets` (lease-bound, at launch) and passes the plaintext to the
  engine **in the stdin request's `credentials` field**, mapped to the
  connection names the workflow's steps reference. torkflow's own
  `ResolveCredential` path is bypassed in favor of orun-injected credentials.
- **In memory only, never content (MUST).** A resolved secret is never written
  to disk — not to the workflow file, not to the pinned engine's run store, not
  to `.orun/`. This binds `orun-secrets`'s "a secret value never becomes
  content" carve-out and `orun-scaffolding` §8's "no secret on disk" rule across
  the process boundary: the child process receives secrets on stdin, and its
  captured stdout/stderr and sealed run are **redacted** (the resolved values are
  swept from anything orun persists or prints), reusing the runner's existing
  redaction.
- **No `secrets.yaml`.** torkflow's file secret store is not used inside orun. A
  workflow run standalone (`orun workflow run`, WF6) MAY fall back to torkflow's
  own connections/secrets for authoring convenience, but a workflow run **as a
  plan step or blueprint hook** is always orun-brokered.

## 7. The determinism & provenance law (the load-bearing carve-out)

This section is the reason the epic is safe. Two invariants of the host — plan
determinism and scaffold fail-closed/provenance — are preserved by a single
rule.

**Rule: only the workflow *reference + digest + declared inputs* is durable
state. The workflow *outcome* is a logged run fact.**

- **Plan (surface A).** `plan.json` carries `{ workflow, workflowDigest, with }`
  on the step and the pinned engine digest in the source list. All of it folds
  into the plan checksum. **None** of the workflow's runtime output does.
  Identical `(intent, components, locked digests, trigger)` still produce a
  byte-identical plan (design principle #3), because a workflow reference is as
  static as a `run:` command string.
- **Run record.** When the workflow executes, its final context and step
  timeline are **sealed into `.orun/`** as that step's output/log — the same
  place a `run:` step's captured output goes. There is **no** parallel `.runs/`
  tree left as a second source of truth (S-2). Side effects the workflow caused
  (a PR URL, a message timestamp) appear **in that sealed log** as run facts;
  they are never promoted into `plan.json`.
- **Provenance (surface B).** `.orun/provenance.lock` records each hook's
  `workflow@digest` and the inputs-hash alongside the existing `blueprint@digest
  + source@digest + inputs-hash`. It does **not** record the PR URL or repo id —
  those are outcomes, not lineage. An `orun … upgrade` re-render (`orun-
  scaffolding` §11) can therefore reason about "did the hook workflow change"
  purely from digests.

The mental test for any new field: *would it differ between two runs with
identical inputs?* If yes, it is an outcome and MUST NOT enter plan or lock.

## 8. Failure, retry & concurrency semantics

**Surface A — plan step.**
- A workflow step obeys the step model's existing knobs: `timeout` bounds the
  whole workflow invocation; `retry` re-invokes the **entire** workflow; `onFailure`
  (`stop`/`continue`) decides the job's fate. These are orun's outer layer.
- torkflow's **own** retries/readiness gates are the workflow's inner layer and
  run inside a single orun step invocation. The two layers are independent and
  documented as such: orun retries the workflow as a black box; the workflow
  retries its own actions. orun does not reach inside.
- Concurrency: to orun's scheduler a workflow step is **one opaque node**. The
  workflow's internal `maxParallelSteps` parallelism is its own and does not
  interact with the plan's job `concurrency`.

**Surface B — blueprint hook (fail-closed, §9/§10 of scaffolding).**
- **`preInstantiate` runs before any placement.** A pre-hook failure aborts with
  a non-zero exit and **nothing is written** — fail-closed is clean. Therefore a
  pre-hook MUST be an **idempotent precondition** (get-or-create the repo, assert
  a namespace exists), never a one-way mutation, because placement or the gate
  may still fail afterward and there is no rollback (S-6).
- **`postInstantiate` runs after the output gate passes.** By then the tree is
  valid and materialized. A post-hook failure is reported as a **non-zero exit
  with the tree left in place** and a clear message that the scaffold succeeded
  but publishing failed — and the hook, being provider-driven, is **re-runnable**
  (`orun … upgrade`/a re-run of the hook). This is the honest boundary: a
  post-gate side effect cannot be un-run, so orun does not pretend to; it reports
  precisely what materialized and what did not.

## 9. Per-module hooks — designed, deferred

The review raised "after each blueprint copy / after each step" — a **per-module**
hook firing as each module in the scaffolding DAG lands (e.g. a PR per module).
It is coherent but materially riskier, so v1 ships **per-instantiation only** and
this section fixes the design for a later milestone.

- **Shape (deferred):** a `hooks.postModule` list, run after each module is
  placed, with the module's name/target in the hook's `with` context.
- **Why deferred:** per-module side effects **interleave with placement**, so a
  failure mid-DAG leaves some modules published and some not — directly at odds
  with the fail-closed law (§8) that per-instantiation hooks preserve by running
  only at the boundaries. It also multiplies external effects (N PRs for one
  repo), which is rarely the intent for a single instantiation and is better
  served by one post-instantiate workflow that opens one PR.
- **When it lands** it MUST be **opt-in and explicit** (`each: module`), MUST
  restrict per-module hooks to idempotent/re-runnable workflows, and MUST record
  per-module hook digests in provenance. Until then, the dominant use cases —
  *ensure repo* (pre) and *open one PR* (post) — are fully served by §3.

## 10. Observability — one cockpit, one log

A workflow step/hook projects through orun's existing cockpit view-model (the
same layer behind `orun status`/`logs`/`tui`), not a second UI.

- A `workflow:` step renders as a job step with its torkflow DAG as **substeps**
  (the workflow's step timeline, statuses, and errors), sourced from the sealed
  `.orun/` run — so `orun logs` and the TUI show the on-call notification's inner
  steps the same way they show a shell step's output.
- A blueprint hook renders in the `orun new` summary with its workflow's outcome
  (created repo, opened PR#) as **logged run facts**, and the hook `workflow@digest`
  appears in `provenance.lock` output.
- Redaction (§6) applies to every projected surface: injected secret values never
  appear in cockpit output, logs, or the sealed run.

## 11. Invariants

1. **A workflow is execution, never intent.** Only reference + digest + declared
   inputs are durable (plan/lock); the outcome is a logged run fact (§7).
2. **One backend, two surfaces.** Plan steps and blueprint hooks share one
   engine-invocation path, one secret bridge, one pinning path (§3–§6).
3. **orun stays the compiler.** torkflow is a backend bound at the step/hook
   level, not a second planner/scheduler; a workflow is one opaque node to orun's
   DAG (§4, §8).
4. **Pinned & portable.** The engine **and** the workflow file are content-
   addressed and folded into the plan checksum / provenance lock (§5).
5. **One audit trail.** The workflow run is sealed into `.orun/`; no split-brain
   `.runs/` is left as a second source of truth (§7, §10).
6. **Secrets never on disk, never content.** Credentials come from `orun-secrets`
   in memory, are redacted from everything persisted/printed, and use no second
   `secrets.yaml` (§6).
7. **Ecosystem-neutral core.** No provider-specific string (slack/github/http)
   appears in orun; every `actionRef` lives in torkflow's action store (§2).
8. **Fail-closed at both surfaces.** `run`\|`use`\|`workflow` mutual exclusion is
   a compile/validation error; pre-hooks are idempotent preconditions; a
   post-gate hook failure is reported precisely with the valid tree left in place
   (§3, §8).

## 12. Sharpness register

| # | Sharp edge | Resolution |
|---|-----------|-----------|
| S-1 | **Determinism leak** — a workflow's runtime outcome reaches `plan.json`/`provenance.lock` and breaks byte-identical plans | Structural: only `{workflow, workflowDigest, with}` + the pinned engine digest are materialized; a plan-hash test asserts two runs with identical inputs are byte-identical; the "would it differ between runs?" test gates any new field (§7). |
| S-2 | **Split-brain state** — torkflow's `.runs/` and orun's `.orun/` disagree on what happened | The workflow run is sealed into `.orun/` as the step/hook output; the engine's run dir is a scratch input to sealing, never the durable record (§7, §10). |
| S-3 | **Two secret models** — a second `secrets.yaml` fragments the secret story or writes a token to disk | orun brokers all in-plan/in-hook credentials from `orun-secrets`, injected in-memory on stdin, redacted from all output; torkflow's file secret store is used only for standalone authoring (`orun workflow run`) (§6). |
| S-4 | **Engine/providers missing at runtime** (docker fresh container, gha runner) | The engine + provider binaries are pinned OCI artifacts; the runner ensures/materializes them like any packaged dependency; a missing engine is a clear pre-flight error, not a mid-step crash (§5). |
| S-5 | **Cross-module import barrier** — torkflow's engine is `internal/`, unimportable from orun | v1 uses the process boundary (subprocess + JSON contract), which needs no import and matches torkflow's own provider architecture; an in-process `pkg/` lift is a declared follow-on (§13). |
| S-6 | **Pre-hook mutates the world, then the gate fails** — an orphaned repo for a scaffold that didn't ship | Pre-hooks are constrained to **idempotent preconditions** (get-or-create); all mutating publish is post-gate; §8 documents and the hook runner enforces the phase split. |
| S-7 | **Per-module rollback** — interleaved side effects leave a half-published repo | Per-module hooks are **deferred** (§9); v1 runs hooks only at the pre/post boundaries where fail-closed holds; when added they are opt-in + idempotent-only + provenanced. |
| S-8 | **Two retry systems confuse operators** — orun step retry vs torkflow action retry | Documented as explicit outer/inner layers: orun retries the workflow as a black box; the workflow retries its own actions; orun never reaches inside (§8). |
| S-9 | **Mutual-exclusion ambiguity** — a step sets both `run` and `workflow` | A compile error (surface A) / blueprint validation error (surface B), mirroring `localExecutor`'s existing `use:`-under-local guard (§3, invariant 8). |
| S-10 | **`with` templating escapes** — a workflow input pulls host state a plan reviewer can't see | `with` is templated against the **same** bounded context the surface already exposes (a step's env/inputs; a blueprint's validated `inputs`), rendered at compile/scaffold time and captured in the plan/lock — so it is as reviewable as any other materialized field (§3, §7). |
| S-11 | **Portability drift** — the workflow format forks between "standalone torkflow" and "inside orun" | The referenced file keeps its unchanged `torkflow/v1` envelope and stays independently runnable; orun adds only the `workflow`/`with` step/hook fields and the injected credential/Trigger context, never a dialect of the workflow itself (§3). |

## 13. Follow-ons (out of scope, named for the record)

- **In-process engine.** Lift torkflow's engine from `internal/` to an importable
  `pkg/` and run it in-process (no subprocess), once the process-boundary contract
  is proven. Removes fork/exec overhead for hot paths.
- **`postModule` per-module hooks.** §9, opt-in + idempotent-only.
- **A first-class `Workflow` catalog entity.** Project `workflow:` steps/hooks
  into the service catalog (which components run which workflows, at which
  digest) — a natural extension of the derived catalog, deferred to keep this
  epic to execution.
