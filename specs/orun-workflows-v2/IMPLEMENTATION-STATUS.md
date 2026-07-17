# orun-workflows-v2 — implementation status

The epic (design in [`design.md`](design.md), milestones in
[`implementation-plan.md`](implementation-plan.md)) is **implemented end to
end** across both repos, in leverage order: truth → scope → pinning → flow →
portability → durability → people.

| Milestone | What shipped | Where |
|-----------|--------------|-------|
| **WX0** | `contract/v1` vendored (schemas + golden fixtures + manifest, byte-identical in both repos); wire types reworked (`connections` replaces `credentials`; `outputs` replaces raw context); versioned contract rejection | orun `internal/workflowbackend/contract/v1` |
| **WX1** | torkflow **`backend` mode** — the contract's real counterparty; injected connections as the exclusive credential source; honest terminal status; stdout reserved for the contract; **first true orun→torkflow end-to-end run**. Fixed a latent scheduler data race the new tests exposed | torkflow #8 |
| **WX2** | the **connections grant** — compile-checked against the pinned file's declared connections; mapped-only injection (canary-proven); the missing-grant error prints the block to paste; hooks validate before any write | orun #544 |
| **WX3** | the **engine is plan content** — `execution.workflowEngine` intent pin → `plan.spec.workflowEngine` → run-time digest verification, fail-closed; `orun workflow engine-digest`; `runDir` wired under `.orun/wfruns` | orun #545 |
| **WX4** | **outputs** — `spec.outputs` (torkflow #9), `${{ steps.X.outputs.Y }}` validated at plan time, run-time substitution, allowlist sealing; proven end to end (`output sum = 42` through a real backend) | orun #546, torkflow #9 |
| **WX5** | **Stack-shipped workflows** — resolve via the composition source root, materialized content-addressed into the workspace; digest parity local-vs-packaged | orun #547 |
| **WX6** | **resume** — `resume: true` beside `retry:`; retry attempts resume over the same `runDir`; engine seeds succeeded steps, restores context, re-routes branches (race-clean) | orun #548, torkflow #10 |
| **WX7** | **approval gates** — `approval: {prompt, timeout, onTimeout}` on workflow steps; the pause and verdict seal under `.orun/approvals`; `orun approve` lists/decides; mandatory timeout policy; docs category rename ("Workflows" guides → "Guides"); this status | orun (this PR) |

## The law, held throughout

**Only names are intent; values are execution.** Connection names + refs,
output names, the engine digest, retry/resume policy, and approval declarations
are compile-checked, digest-covered plan content. Credential values, output
values, verdicts, and replays are sealed run facts. Plans are byte-identical
across runs — including whether an approval was later approved or rejected.

## Scoping decisions (recorded)

- **OCI engine fetch** — the enforced pin is the *declared digest* in intent
  (deterministic, machine-portable); pulling the engine itself from OCI awaits
  torkflow's engine artifact packaging. `ORUN_TORKFLOW_ENGINE` remains the
  resolution mechanism, subordinate to the pin.
- **Digest widening over action-store manifests** — deferred with the same
  dependency: orun cannot see engine-side manifests until the engine ships with
  its store as one artifact.
- **Cross-repo conformance in CI** — both repos run conformance against the
  vendored fixtures in their own CI; a live orun→torkflow run in CI awaits a
  torkflow release containing `backend` mode (proven locally at WX1 and WX4).
- **Cross-run resume** — resume is scoped to retry attempts within one
  execution (the `runDir` is per-exec); resuming a new `orun run` from a prior
  execution's state is future work.
- **torkflow smoke job** — advisory in torkflow CI until the upstream
  tinx→kiox action drift is fixed (documented in the workflow file).

## Follow-ons

- Package the torkflow engine as an OCI artifact (unlocks the two deferrals
  above and un-defers the smoke job).
- Surface pending approvals in the TUI cockpit (they surface in `orun status`
  output text and `orun approve` today).
- A `Workflow` catalog entity (carried over from v1's follow-ons).
