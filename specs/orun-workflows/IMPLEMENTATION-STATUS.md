# orun-workflows — implementation status

The epic (design in [`design.md`](design.md), milestones in
[`implementation-plan.md`](implementation-plan.md)) is **implemented end to end**.
Both surfaces — the `workflow:` plan step and the `workflow:` blueprint hook — run
on one backend, one secret bridge, one pinning path.

| Milestone | What shipped | Where |
|-----------|--------------|-------|
| **WF0** | `internal/workflowbackend`: `WorkflowDigest`, the `Engine` interface + `SubprocessEngine`, `ResolveEngine` over a JSON contract | `internal/workflowbackend` |
| **WF1** | the `workflow:` step form — `Workflow`/`WorkflowDigest` on `Step`/`RenderedStep`/`PlanStep`, `ValidateExecForm` mutual exclusion, digest pinned into the plan against `JobPlanner.WorkflowBaseDir` | `internal/model`, `internal/planner`, `internal/render` |
| **WF2** | execution — `workflowbackend.RunStep` (digest re-verified), runner dispatch under any runner, run sealed into `.orun/`, `Runner.WorkflowEngine` | `internal/workflowbackend`, `internal/runner` |
| **WF3** | secret bridge — the job's resolved `orun-secrets` values injected in-memory as engine credentials, redacted, no second store | `internal/runner` |
| **WF4** | the blueprint hook — `Hook.Workflow`/`With` + validation, `hookRunner`, `Provenance.Hooks` pinned by `{id, phase, workflow, digest}`, `Options.WorkflowEngine` | `internal/scaffold` |
| **WF5** | cockpit projection — `workflow:` rendered in the plan/DAG viewer and the runner's live step context | `internal/render`, `internal/runner` |
| **WF6** | `orun workflow validate\|digest\|run\|view` authoring subcommand | `cmd/orun` |
| **WF7** | ecosystem-neutrality lint, example workflows + usage docs, this status | `internal/workflowbackend`, `examples/workflows` |

## The invariants, as enforced in code

- **Execution, never intent** — plan/lock carry only `workflow` + `workflowDigest`
  (+ `with`); the run is sealed into `.orun/`, never promoted. Plan-hash
  determinism test in `internal/planner`.
- **One backend, two surfaces** — both the step and the hook call
  `workflowbackend.RunStep`; no second invocation path.
- **Pinned & fail-closed** — the digest is re-verified before a workflow runs
  (`DigestMismatchError`); an unresolvable reference is a compile/scaffold error.
- **Secrets never on disk** — credentials come from `orun-secrets` in-memory and
  are redacted; torkflow's `secrets.yaml` is used only by `orun workflow run`.
- **Ecosystem-neutral core** — `TestWorkflowCoreIsEcosystemNeutral` fails the
  build on a provider literal in the workflow core.

## Follow-ons (designed, not built)

- **Engine pinning via OCI** — `ResolveEngine` currently digests a configured
  binary (`ORUN_TORKFLOW_ENGINE`); packaging the engine as a pinned OCI artifact
  is the natural next step (design §5).
- **In-process engine** — lift torkflow's engine to an importable `pkg/` and drop
  the subprocess (design §13).
- **Action-store manifests in the digest** — fold referenced module manifests
  into `WorkflowDigest` so a provider change also flips it (design §5).
- **`postModule` hooks** — per-module hook granularity, opt-in + idempotent-only
  (design §9). The shipped granularities are global `postInstantiate` and
  per-phase `phases[].hooks`.
