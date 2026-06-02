# Runner Integration & Legacy Removal

> How the runner writes the new model natively, how live state becomes sealed
> content, and the exact plan for deleting `internal/state`,
> `internal/statebackend/file`, and the executionstate bridge **without
> workarounds**. Staged behind `ORUN_OBJECT_RUNNER` until proven.

## 1. The working-tree / seal model (git's lesson applied)

A running execution is **mutable state**; a finished execution is **content**.
We separate them exactly as git separates the working tree from a commit.

```
open  → working tree (mutable)          .orun/run/<execId>/   (job/step status mutates here)
…run… → rewrite-in-place as jobs/steps progress; periodic flush; heartbeat ref
seal  → write the full execution tree as immutable objects, then move one ref
```

- **Working tree** (`.orun/run/<execId>/`): the live, rewritable JSON tree
  shaped identically to the sealed execution tree (`execution.json`,
  `jobs/<j>/attempts/<n>/steps/…`, `events/`, `logs/`). The runner writes here
  as the run progresses. A `refs/executions/live/<execId>` handle + a lockfile
  mark it in-flight.
- **Seal** (terminal status reached): walk the working tree bottom-up, `PutBlob`
  every record/log, `PutTree` every directory, producing the execution Merkle
  root. Then `Update` `refs/executions/latest` (and the trigger ref) to the
  sealed id — **this single ref move is the publish point**. Finally delete the
  `live/<execId>` handle and (optionally) GC the working tree.
- **Crash recovery:** on next `orun` invocation, any `refs/executions/live/<id>`
  whose lockfile is stale is either resumable (re-open the working tree) or
  sealed-as-`failed`/`cancelled` per policy. Partial objects written before a
  crash are inert (unreachable ⇒ GC'd).

This gives the runner a native home for **both** live per-job/step state (the
working tree) and finished provenance (the sealed objects) — which is precisely
what `internal/state` + the bridge were faking via mirroring.

## 2. Native job / attempt / step (the reserved levels, finally built)

The runner writes `JobRun → JobAttempt → StepAttempt` records
(`data-model.md` §7.1–7.3) into the working tree as it executes:

- On job start: create `jobs/<j-folder>/job-run.json` (status `running`).
- On attempt: `attempts/<n>/attempt.json`.
- On step finish: `steps/s-<id>.json` with `exitCode`, `status`, and a `logId`
  (the step's captured output is a content blob — identical logs dedup).
- Heartbeats: update a tiny mutable `refs/executions/live/<id>` payload
  (`{lastHeartbeat, currentJob}`) — liveness lives in the ref layer, not in the
  content graph.

No bridge. No `state.json`. The working tree *is* the live representation, and
it is the new schema, so sealing is a pure copy-to-objects with no translation.

## 3. Staging behind a flag

- `ORUN_OBJECT_RUNNER=1` (env) or `runner.objectModel: true` (config) selects
  the new runner. Default off until M12 acceptance.
- Both runners are present during M7–M11; the legacy runner remains the default
  so the change is reversible and reviewable on real executions.
- M12 flips the default to on, deletes the legacy path, and removes the flag.

## 4. Legacy parity matrix — DELETE only when every row is satisfied

`internal/state`, `internal/statebackend/file`, and
`internal/executionstate/bridge.go` MUST NOT be deleted until each capability
below has a native home with a passing test. This is the gate for M12.

| # | Legacy capability (where) | Native replacement | Owning milestone |
|---|---------------------------|--------------------|------------------|
| 1 | `SavePlan` / `.orun/plans/<checksum>.json` (state.Store) | PlanRevision object + `refs/named/*` | M4 |
| 2 | `latest.json` plan pointer | `refs/revisions/latest` | M4 |
| 3 | `GenerateExecID` / exec id minting | `executionId` (ULID / preserved CI form) in nodewriter | M7 |
| 4 | `CreateExecution` / `.orun/executions/<id>/` | working tree `.orun/run/<execId>/` + seal | M7 |
| 5 | live `state.json` (ExecState, job/step status, heartbeats) | working-tree JobRun/Attempt/Step + live ref heartbeat | M7 |
| 6 | `metadata.json` (ExecMetadata, links, counts) | `execution.json` fields (`summary`, `links`) | M7 |
| 7 | per-step logs `.orun/logs/<exec>/<job>/<step>` | content log blobs (`logId`), working-tree `logs/` | M7 |
| 8 | `ListPlans` | `objindex` revisions index + `orun get plans` | M8 |
| 9 | `ListExecutions` | `objindex` executions index + `orun status` | M8 |
| 10 | `ResolvePlanRef` (hash/name/latest) | ref + revision resolver | M8 |
| 11 | `ResolveExecID` | ref + execution resolver | M8 |
| 12 | `SummarizeExecutionState` (counts) | computed at seal, stored in `summary` | M7 |
| 13 | `GC` (count/age prune of plans+execs) | reachability GC (`objectstore` §7) + retention | M9 |
| 14 | `MigrateLegacyState` / old in-place upgrades | one-shot `orun migrate` (compat doc) | M10 |
| 15 | `statebackend.Backend` (remote job state: Init/Claim/Heartbeat, `LoadRunState`) | working tree + ref heartbeat locally; remote = object substitution + remote refs | M7 (local), M11 (remote) |
| 16 | `runbundle` hydrate/reader/writer (reads state.ExecState/Metadata) | reads sealed ExecutionRun objects via `nodes` accessors | M8 |
| 17 | TUI services reading state.* (`history/live/plan/run`) | TUI reads model via `nodes`/`objindex`/`refstore` | M11 |
| 18 | cockpit bridge/viewmodel reading state.* | same as 17 | M11 |
| 19 | `remotestate/convert.go` legacy types | object substitution path | M11 |

When all 19 rows are green: **delete** `internal/state`,
`internal/statebackend/file.go` (+ flock helpers if unused elsewhere),
`internal/executionstate/bridge.go`, and the Phase 1/2 dual-write writers
(`internal/revision/catalog_parent.go` mirror, the legacy compat mirror, the
catalog-parent execution mirror). Remove the `ORUN_OBJECT_RUNNER` flag.

## 5. What the runner does NOT own

- It does not resolve source/catalog/revision — `nodewriter` does that (steps
  1–4 of the tolerant-strict walk) before the runner opens a working tree.
- It does not move source/catalog refs.
- It does not write indexes or the working view — those are refreshed by the
  command layer after seal (or lazily by `orun reindex`/`orun checkout`).

## 6. Concurrency & multiple runners

- Two concurrent executions are two independent working trees + two
  `live/<execId>` handles — no contention (different ids, different dirs).
- Sealing is independent per execution; the only shared mutable surface is
  `refs/executions/latest`, updated by CAS (last-sealed-wins; both executions
  remain individually addressable by their own sealed id and `executionId`).
- A future remote/distributed runner uses remote refs for claim/heartbeat
  (object substitution makes the content automatically shareable); that is M11+
  and Phase-3 hardening.
