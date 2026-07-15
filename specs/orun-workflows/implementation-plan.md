# Implementation Plan — workflow actions

> Milestone-based. Each states **goal**, **deps**, and **done when**. The backend
> harness (WF0) pins + invokes the engine and is shared by both surfaces; the
> plan-step surface (WF1 model/compiler, WF2 executor) delivers the smallest
> useful scale; the secret bridge (WF3) makes it real; the blueprint-hook surface
> (WF4) reuses the same backend; observability (WF5) and the `orun workflow`
> authoring surface (WF6) round it out; WF7 proves it end-to-end and lands the
> hook-granularity decision.
>
> **Consumes** `orun-secrets` (the `secret://` reference model + redaction, WF3)
> and `orun-scaffolding` §12 (the hook seam, WF4). No hard external gate — the
> plan-step surface (WF0–WF2) reuses only shipped orun seams (the step model, the
> executor registry, the object store / OCI fetch) plus the packaged torkflow
> engine.

```
[ torkflow engine, packaged OCI ]
                 │
                 ▼
          WF0 workflowbackend: pin engine + workflow by digest · JSON subprocess contract
                 │
                 ▼
          WF1 `workflow:` step form — model + compiler (bind/render digest into plan.json)
                 │
                 ▼
          WF2 workflowExecutor + registry — runs under local/docker/gha · seals run into .orun/   ◄─ smallest useful scale
                 │
                 ▼
          WF3 secret bridge — orun-secrets secret:// → in-memory credential injection + redaction
                 │
                 ▼
          WF4 blueprint hook `workflow:` (postInstantiate + phases[].hooks) — same backend · provenance.lock digest
                 │
                 ▼
          WF5 cockpit + `orun logs` projection (torkflow DAG as substeps)
                 │
                 ▼
          WF6 `orun workflow` validate/view/run subcommand (fronts torkflow)
                 │
                 ▼
          WF7 end-to-end proof (scaffold → open PR) + per-module (`postModule`) decision recorded
```

---

## WF0 — `workflowbackend`: pin the engine + workflow, invoke over a JSON contract
**Goal:** the single, shared execution path — resolve and digest-pin the torkflow
engine and a workflow file, and run it as a subprocess over a JSON contract, with
nothing wired into steps or hooks yet.
- Stand up `internal/workflowbackend` (new): resolve the torkflow engine as a
  packaged provider artifact (reuse `internal/composition` dir/oci fetch) locked
  to a digest; `WorkflowDigest(path) (sha256, error)` over the canonicalized
  workflow file **⊕ its referenced action-store module manifests**; an
  `Invoke(req) (result, error)` that writes the JSON request to the pinned
  engine's stdin and reads the final context from stdout (the contract in
  design §5), mirroring torkflow's own `internal/executor/binary.go` boundary.

**Deps:** packaged torkflow engine. **Done when:** a fixture workflow resolves to
a stable digest that changes iff the file or a referenced module manifest changes;
`Invoke` runs a trivial fixture workflow through the pinned engine and returns its
final context; a missing or mismatched engine digest is a clear pre-flight error.
**Design:** §5.

## WF1 — the `workflow:` step form (model + compiler)
**Goal:** a job step can name a workflow; the compiler pins it into `plan.json`
deterministically.
- Add `Workflow string` + reuse `With map[string]interface{}` on `model.Step`,
  `model.RenderedStep`, and `model.PlanStep`. Enforce **mutual exclusion**: a step
  with more than one of `run`/`use`/`workflow` is a compile error (extend the
  existing step validation). At bind/render, resolve the workflow path, compute
  `workflowDigest` (WF0), template `with` against the step's env/inputs context,
  and materialize `{ workflow, workflowDigest, with }` onto the `PlanStep`; add the
  pinned engine digest to the plan's source list. Fold all of it into the plan
  checksum.

**Deps:** WF0. **Done when:** a composition job with a `workflow:` step compiles;
`plan.json` carries the ref + digest + templated `with` and **no** runtime field;
two plans over identical inputs are byte-identical (plan-hash test); a step with
`run` **and** `workflow` fails compilation. **Design:** §3, §5, §7.

## WF2 — `workflowExecutor` + registry (the smallest useful scale)
**Goal:** `orun run` executes a `workflow:` step under any runner and seals the
result into the run record.
- Add a `workflowExecutor` implementing the `Executor` interface
  (`internal/executor`), registered in `factories`. `RunStep` detects a
  `workflow:` step, calls `workflowbackend.Invoke` (WF0), and returns the final
  context as the step output; a `run:`/`use:` step is untouched. It runs under
  `local`/`docker`/`gha` alike (it only shells the pinned engine — unlike `use:`,
  no runner is forced). Seal the workflow run (final context + step timeline) into
  `.orun/` as the step's output/log — **no** parallel `.runs/` record. Honor the
  step's `timeout`/`retry`/`onFailure` as the outer layer (design §8).

**Deps:** WF1. **Done when:** a plan with a `workflow:` step runs end-to-end on
the local runner and the on-disk record shows the sealed workflow run under
`.orun/`; the same plan runs under docker (engine present in the image); a
failing workflow honors `onFailure: stop`; `retry: 1` re-invokes the whole
workflow once; nothing is written under `.runs/`. **Design:** §4, §7, §8, §10.

## WF3 — the secret bridge (`orun-secrets` → in-memory credential injection)
**Goal:** workflows get credentials from orun's secret system, never a second
store, never on disk.
- In `workflowbackend`, accept resolved credentials from the runner's
  `orun-secrets` resolution (the job's `secretRefs`, lease-bound at launch) and
  pass them to the engine in the request's `credentials` field, keyed to the
  connection names the workflow references — bypassing torkflow's own
  `ResolveCredential`. Extend the runner's **redaction** to sweep resolved secret
  values from the workflow's captured stdout/stderr and the sealed run. Assert no
  secret is ever written to the workflow file, the engine run dir, or `.orun/`.

**Deps:** WF2; consumes `orun-secrets`. **Done when:** a `workflow:` step whose
workflow calls an authenticated action succeeds using a `secret://`-resolved
token; the token never appears in `plan.json`, the sealed run, `orun logs`, or any
temp file (a redaction + no-disk test); torkflow's `secrets.yaml` path is not
consulted for an in-plan workflow. **Design:** §6, §11 (invariant 6).

## WF4 — blueprint hook `workflow:` (postInstantiate + per-phase)
**Goal:** the second surface — a scaffolding hook can be a workflow — on the same
backend, bound to the **shipped** `internal/scaffold` seam.
- Extend the shipped `scaffold.Hook` struct (`internal/scaffold/blueprint.go`,
  today `{ID, Run []string}`): add `Workflow string` + `With map[string]any`, and
  require **exactly one** of `Run`/`Workflow` in `validate()` (a blueprint
  validation error otherwise). This lands `workflow:` in **both** shipped
  attachment points — the global `Hooks.PostInstantiate` list and each
  `Phase.Hooks` list — with no new hook phase invented.
- Route it: `runHooks` (`internal/scaffold/hooks.go`) branches on hook kind — an
  argv hook execs as today; a `workflow:` hook calls `workflowbackend.Invoke`
  (WF0/WF3) with the blueprint's `secret:`-marked inputs resolved via the secret
  bridge and `with` as the Trigger context. Hooks stay opt-in via `--run-hooks`
  (`Options.RunHooks`) and keep running **after** the atomic write + provenance
  (design §8) — this epic adds a hook *kind*, not mid-placement execution.
- Provenance: add a `Hooks []ProvHook` field to the shipped `Provenance` struct
  (`internal/scaffold/provenance.go`), recording each hook's `{id, workflow,
  digest}`. `buildProvenance` runs before `runHooks`, so it captures the hook
  **reference + digest** only — never the outcome (design §7); the PR URL is never
  written to the lock.
- Failure: a `workflow:` hook failure returns non-zero with the gated tree already
  on disk and a precise "scaffold succeeded, hook <id> failed" message; the hook
  is re-runnable (`orun new upgrade`, or re-run with `--run-hooks`). There is **no**
  `preInstantiate` in the shipped scaffolder — a precondition like *ensure the repo
  exists* folds into the workflow's own idempotent first action.

**Deps:** WF3; consumes the shipped `orun-scaffolding` §12 seam. **Done when:** a
blueprint whose global `postInstantiate` runs an open-PR workflow and whose
`phases[].hooks` runs a per-phase verify workflow instantiates a fixture under
`orun new --run-hooks`; a hook declaring both `run` and `workflow` fails blueprint
validation; `provenance.lock` carries a `hooks[]` block with each hook digest and
**no** PR URL; a hook failure exits non-zero with the tree materialized; an argv
hook still works unchanged. **Design:** §3, §7, §8, §9.

## WF5 — cockpit + `orun logs` projection
**Goal:** a workflow step/hook is legible through orun's one cockpit, not a second
UI.
- Project the sealed workflow run through the cockpit view-model: a `workflow:`
  step renders with the torkflow DAG as **substeps** (statuses, per-step errors,
  durations) in `orun status`/`logs`/`tui`; a blueprint hook renders in the
  `orun new` summary with its outcome as logged run facts. Ensure redaction (WF3)
  applies to every projected surface.

**Deps:** WF2 (steps), WF4 (hooks). **Done when:** `orun logs` on a run containing
a `workflow:` step shows the workflow's inner steps and statuses from `.orun/`;
the TUI renders them as nested substeps; no injected secret appears in any
projection; the `orun new` summary shows the hook outcome. **Design:** §10.

## WF6 — `orun workflow` authoring subcommand
**Goal:** validate/view/run a workflow directly, for authoring and debugging
outside a plan.
- Add `orun workflow validate|view|run <file>` fronting torkflow's own
  `run`/`view` capabilities (via the pinned engine). `run` MAY fall back to
  torkflow's own connections/secrets for standalone authoring convenience (the one
  place a local `secrets.yaml` is allowed, design §6); `validate`/`view` reuse
  torkflow's DAG view. This is the on-ramp: author a workflow, see its DAG, run it
  standalone, then drop it into a `workflow:` step or hook.

**Deps:** WF0. **Done when:** `orun workflow view <file>` renders the DAG; `orun
workflow validate <file>` reports a malformed workflow; `orun workflow run <file>`
executes a standalone workflow through the pinned engine; the subcommand shares the
engine-resolution path with WF0 (one pinned engine, not two). **Design:** §5, §6.

## WF7 — end-to-end proof + the granularity decision
**Goal:** prove both surfaces on a real example and record the hook-granularity
decision.
- Ship an example: a blueprint that scaffolds a small service and, on
  `postInstantiate`, runs an open-PR workflow (against a `github` provider authored
  in torkflow) with a per-phase `phases[].hooks` verify workflow, plus a
  composition job with a `workflow:` notify step — all driven by
  `secret://`-resolved credentials. Document the outer/inner retry layering (§8)
  and the standalone→in-plan authoring path (WF6). **Lock the granularity decision
  (design §9):** the shipped seams give global (`postInstantiate`) + per-phase
  (`phases[].hooks`) hooks; per-**module** (`postModule`) stays deferred with its
  opt-in + idempotent-only + provenanced requirements recorded.

**Deps:** WF4, WF5, WF6. **Done when:** the example scaffold instantiates and opens
a PR via the post-hook (or dry-runs it deterministically in CI), with the tree
passing the scaffolding gate first; the notify step runs in a plan; docs cover the
two retry layers and the deferral; no provider-specific string appears in
`internal/workflowbackend`/`internal/executor` (invariant 7 lint). **Design:** §8,
§9, §11.

---

## Cross-cutting (every milestone)
- **A workflow is execution, never intent:** only reference + digest + declared
  inputs are ever durable (plan/lock); the outcome is a logged run fact sealed in
  `.orun/` (invariants 1, 5). A plan-hash test guards byte-identical plans; the
  "would it differ between runs?" test gates any new materialized field.
- **One backend, two surfaces:** plan steps and blueprint hooks share
  `internal/workflowbackend` — one invocation path, one secret bridge, one pinning
  path; no second engine-invocation implementation (invariant 2).
- **orun stays the compiler:** a workflow is one opaque node to orun's DAG; its
  internal parallelism/retries are its own; orun never reaches inside (invariants
  3, 8).
- **Secrets never on disk, never content:** credentials come from `orun-secrets`
  in memory and are redacted from everything persisted/printed; no second
  `secrets.yaml` for in-plan/in-hook runs (invariant 6) — guarded by a
  redaction + no-disk test each milestone that touches execution.
- **Ecosystem-neutral core:** no slack/github/http string in orun; every
  `actionRef` lives in torkflow's action store (invariant 7) — guarded by a lint.
- This epic **consumes** `orun-secrets` (WF3) and `orun-scaffolding` §12 (WF4) and
  introduces no parallel secret or hook model; it adds the `workflow`/`with` fields
  to the existing step/hook shapes and one executor, nothing more to the compiler.
