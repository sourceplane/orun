# Spec: orun-workflows (workflow actions — a data-flow execution vocabulary)

**One backend, two surfaces.** orun can already say *what should happen* (intent
→ plan) and run *how it happens* two ways at the step level: `run:` (a shell
command) and `use:` (a GitHub Actions action). Both are opaque to orun's data
model — a shell step's output is unstructured text, a `use:` step is a foreign
runtime — so anything that needs to *call an authenticated system, take its
structured result, pass it to the next action, and branch on it* collapses into
hand-rolled `curl | jq` inside a `run:` block. This epic adds the missing third
vocabulary: **`workflow:`** — a portable, provider-backed, connection-
authenticated, expression-driven workflow, executed by the **torkflow** runtime.
It appears in exactly two places that share **one** execution backend and **one**
secret bridge: **plan steps** (inside a job) and **blueprint hooks** (post-scaffold,
global or per-phase). orun stays the compiler; torkflow becomes an execution backend
bound at the step/hook level — the precise analogue of how a `use:` step selects
the github-actions runner today.

> **The defining law is: a workflow is execution, never intent.** The plan and
> the scaffolder's provenance lock capture only a workflow's **reference +
> content digest + declared inputs** — never its runtime outcome. This is what
> keeps orun's headline property intact (identical inputs → byte-identical
> `plan.json`, design principle #3) and the scaffolder's determinism/fail-closed
> law untouched (`orun-scaffolding` invariants 3/5/6). A workflow's dynamic
> branching, live calls, retries, and structured outputs live entirely on the
> execution side of that line — exactly where a shell `run:` step's effects
> already live. The command is in the plan; the effect never is.

## Status

| Field | Value |
|-------|-------|
| Status | **Draft (v1) — for review** |
| Ground truth | Reconciled against the **shipped scaffolder** (`internal/scaffold`, merged in #506) on 2026-07-15. Surface B binds to the real `scaffold.Hook` seam — `Hooks.PostInstantiate` + `Phase.Hooks`, argv-only today, opt-in via `--run-hooks`, run **after** the atomic write; there is **no `preInstantiate`** in the shipped scaffolder (design §12 reconciliation). Surface A (plan-step `workflow:`) remains greenfield — `model.Step`/`PlanStep` still carry only `Run`/`Use`/`With`. |
| Absorbs | the two integration surfaces sketched in design review — a `workflow:` **plan step** (a job step that runs a data-flow workflow) and a `workflow:` **blueprint hook** (a post-scaffold workflow: global `postInstantiate` **or** a per-phase `phases[].hooks`, matching the shipped `internal/scaffold` seam) — into one backend + one secret bridge, rather than two bolt-ons |
| Builds on | orun's step model (`internal/model` `Step`/`PlanStep`/`RenderedStep`), the pluggable executor registry (`internal/executor` — `local`/`docker`/`github-actions`), the content-addressed object store + OCI/`internal/composition` fetch (for pinning the engine + workflow by digest), `orun-secrets` (the `secret://` reference model + lease-bound runner injection + log redaction), and `orun-scaffolding` §12 (the declared-hooks seam this epic upgrades) |
| Runtime | **torkflow** (`github.com/sourceplane/torkflow`) — a thin workflow engine: a DAG scheduler over `actionRef` provider binaries (JSON stdin/stdout), a `{{ }}` + Goja expression resolver, a file-backed run store, and connection/credential resolution. Consumed as a **pinned, packaged provider artifact**, not vendored source |
| Engine decision (locked) | orun invokes a **digest-pinned torkflow engine as a subprocess** over a JSON contract — the same process boundary torkflow already uses for its own providers. No cross-module Go import in v1 (torkflow's engine is `internal/`); an in-process `pkg/` lift is a declared follow-on (§13) |
| Decisions locked | one execution backend for both surfaces; a step is exactly one of `run` \| `use` \| `workflow` (else a compile error); the engine **and** the workflow file are pinned by digest and folded into the plan checksum / provenance lock; the workflow's *outcome* is never plan or lock content; the workflow run is **sealed into `.orun/`** (no split-brain `.runs/`); credentials come from `orun-secrets` in-memory (**no second `secrets.yaml`**, never on disk); orun ships **no** provider-specific string (slack/github/http) — providers live in torkflow's action store; scaffolding `workflow:` hooks attach at the two **shipped** granularities — global `postInstantiate` and per-phase `phases[].hooks`, both **post-write** — while per-module (`postModule`) is deferred (design §9) |
| apiVersion | `orun.io/v1` (the new `workflow`/`with` fields on steps + hooks); workflow files keep their portable `torkflow/v1` envelope, unchanged |
| Milestone prefix | **WF** (`WF0 → WF7`) |

## The one-paragraph thesis

orun is a deterministic intent compiler with a backend-swappable runtime: the
plan is the boundary, and today an executor turns each step into either a shell
command or a GitHub Actions action. That covers "run this program" but not "call
Slack with a credential, take the JSON, decide the on-call, post a message" —
which today means a brittle `curl` pipeline with secrets smeared through it.
torkflow is exactly that data-flow layer, already built to a compatible shape: a
DAG of typed `actionRef` steps, each a provider binary over a JSON contract,
with `{{ Steps.X.output }}` wiring, connection-bound credentials, retries, and
readiness gates. This epic makes torkflow a **third execution vocabulary** in
orun without making it a second compiler. A `workflow:` step or hook names a
portable workflow file; at compile/scaffold time orun pins the file (and the
engine) by content digest and folds only that reference into the plan/lock; at
run time a single `workflowExecutor` shells the pinned engine, injects
orun-resolved secrets in memory, and seals the result back into `.orun/`. The
same backend serves both a job step (**"run a workflow as part of delivery"**)
and a scaffolding hook (**"after you generate this repo, open the PR"**) — one
integration, two surfaces, one determinism law.

## The flow (one backend, two surfaces)

```
                         a portable workflow file (torkflow/v1)
                        actionRef DAG · {{ }} · connections · retries
                                          │  pinned by content digest
              ┌───────────────────────────┼───────────────────────────┐
              ▼                                                         ▼
   SURFACE A — plan step                              SURFACE B — blueprint hook
   ─────────────────────                              ─────────────────────────
   job:                                               hooks:                (global,
     steps:                                             postInstantiate:     post-write)
       - name: notify                                     - workflow: open-pr.yaml
         workflow: wf/notify.yaml                     phases:               (per-phase)
         with: { chan: "{{ .channel }}" }               - hooks:
              │                                              - workflow: verify.yaml
   orun plan: resolve + DIGEST the file                            │
   into plan.json (ref + digest + with)                orun new --run-hooks:
              │                                         place → GATE → write → hooks
              │                                         provenance.lock records wf@digest
              │                                                        │
              └───────────────────────────┬───────────────────────────┘
                                          ▼
                          ONE backend: workflowExecutor
                 ─────────────────────────────────────────────
                 • resolve secret:// refs (orun-secrets) → in-memory credential
                 • shell the DIGEST-PINNED torkflow engine (JSON contract)
                 • capture final context; SEAL the run into .orun/  (no split .runs/)
                 • effects (PR url, message ts) are LOGGED run facts, never plan/lock
                                          │
                          runs under local · docker · gha alike
```

## Read order

1. **`design.md`** — the model (§2), the two surfaces and why they share one
   backend (§3–§4), the execution boundary + engine pinning (§5), the secret
   bridge (§6), the determinism/provenance law (§7), failure & concurrency
   semantics (§8), the per-module deferral (§9), observability (§10), the
   invariants (§11), and the sharpness register (§12).
2. **`implementation-plan.md`** — milestones **WF0 → WF7**.

## Surface table — one vocabulary, two scopes

| Surface | Where it's authored | What it's for | Runs when | Pinned into | Failure semantics |
|---------|--------------------|---------------|-----------|-------------|-------------------|
| **Plan step** (`workflow:` on a job step) | a composition/golden-path job, alongside `run:`/`use:` steps | a delivery action that needs structured, authenticated, multi-provider data-flow (notify on-call, sync an external system, gate on an API) | during `orun run`, as one step in a job | `plan.json` (ref + digest + `with`; folds into the plan checksum) | the step's existing `timeout`/`retry`/`onFailure`; orun's step retry wraps the whole workflow |
| **Blueprint hook** (`workflow:` in `hooks.postInstantiate` or a `phases[].hooks`) | a `blueprint.yaml`, upgrading the shipped §12 `run:[argv]` seam (`scaffold.Hook`) | post-placement side effects orun must not internalize — *commit + open the PR*, run a *per-phase verification* — with *ensure-repo* folded in as the workflow's own idempotent first action | during `orun new`/`create`/`instantiate` with `--run-hooks`, after the atomic write, in phase order then global | `.orun/provenance.lock` (a new `hooks[]` entry: `id` + `workflow@digest`) | the tree is already gated + written; a hook failure exits non-zero with the valid tree in place and is re-runnable (`orun new upgrade`) |

Both rows resolve the same file shape, run on the same `workflowExecutor`, draw
credentials from the same `orun-secrets` bridge, and obey the same law: only the
reference + digest is durable state; the outcome is a logged run fact.

## Phase boundaries

| In scope (this spec) | Out of scope |
|----------------------|--------------|
| the `workflow`/`with` fields on `model.Step`/`PlanStep`/`RenderedStep` and on the scaffolding hook shape; the mutual-exclusion validation (`run`\|`use`\|`workflow`); compile/scaffold-time **digest pinning** of the workflow file (+ its action-store refs) and of the torkflow engine; a `workflowExecutor` in `internal/executor` that shells the pinned engine over a JSON contract and runs under `local`/`docker`/`gha`; the **secret bridge** from `orun-secrets` `secret://` refs to torkflow connection/credential injection (in-memory, redacted); sealing the workflow run into `.orun/`; the scaffolding hook upgrade (a `workflow:` form on `scaffold.Hook`, usable in `postInstantiate` and `phases[].hooks`, plus a `hooks[]` block in `Provenance`); cockpit/`orun logs` projection of a workflow step/hook; an `orun workflow` validate/view/run subcommand fronting torkflow | torkflow's engine internals, its provider SDK, and any specific provider (slack/http/**github**) — authored and released in the torkflow repo, consumed here as a pinned artifact (a `github` provider for the "create repo / open PR" examples is torkflow-side work); the `orun-secrets` store/policy engine itself (consumed, not re-specified); **per-module** scaffolding hooks (`postModule`) — designed but deferred (§9); an in-process Go import of the engine (§13); rewriting orun's planner or scheduler around torkflow's DAG (orun stays the compiler) |

## Out-of-band references

- **Sibling runtime:** `github.com/sourceplane/torkflow` — engine
  (`internal/engine`), the provider binary contract (`internal/executor/binary.go`
  — JSON stdin/stdout), connection/credential resolution (`internal/engine`
  `ResolveCredential`), the `{{ }}`+Goja resolver (`internal/expression`), and
  the file-backed run store (`internal/state`). Packaged as a tinx/kiox provider
  (`provider.yaml`), the same OCI substrate orun uses for golden paths.
- **orun capabilities reused (code reality):** `internal/model`
  (`Step`/`PlanStep`/`RenderedStep`, the `Run`/`Use`/`With` precedent);
  `internal/executor` (the `Executor` interface + `factories` registry —
  `local`/`docker`/`github-actions`, and `localExecutor`'s existing "this step
  uses `use:` → switch runners" routing, the pattern `workflow:` mirrors);
  `internal/composition` (dir/oci fetch) + the content-addressed object store
  (for pinning the engine + workflow by digest, exactly as compositions and
  scaffolding sources are pinned); the cockpit view-model
  (`orun status`/`logs`/`tui`).
- **Consumed epics:** `orun-secrets` (the `secret://` reference model,
  lease-bound injection, and log redaction the secret bridge sits on);
  `orun-scaffolding` §12 (the declared-hooks seam upgraded here; §9/§10 the
  determinism + fail-closed laws this epic must not violate);
  `orun-service-catalog` SC7 (compositions as the home for `workflow:` job
  steps).
