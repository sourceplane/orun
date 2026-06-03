# Follow-up: repoint `runbundle.Hydrate` (`orun github pull`) onto the object graph

> **Status: DONE.** `runbundle.Hydrate` now seals pulled runs into the object
> graph via `objrun.Seal` (the non-runner counterpart to `Begin`/`Finish`), so
> `orun github pull` is readable by `orun status`/`orun logs`. There are now
> **zero** non-test importers of `internal/state`; the remaining work to delete
> the package is the mechanical test repoint + cleanup in §5 (T6).
>
> Implementation note vs. the original plan below: rather than hand-building
> `execseal.SealInput`, `Hydrate` projects the shards' jobs/steps/logs through
> `objrun.Seal`, which drives the same `runworktree` path the live runner uses —
> so an imported run is shaped identically to a native one with no duplicated
> assembly logic. The source shards carry no per-step timestamps, so per-job/step
> times are import-time while the execution-level start/finish reflect the real
> run.

## 1. Why this exists

`orun github pull` downloads a GitHub Actions run's artifact bundle (a plan
shard + job shards) and calls `runbundle.Hydrate` to rebuild a local
`.orun/executions/<id>/` directory (`metadata.json`, `state.json`, `github.json`,
`plan.json`, `shards.json`, `logs/`) via `internal/state`'s `Store`. The original
point was *"compatibility with `orun status`, `orun logs`, and other commands."*

After the M12 cutover those read commands read the **content-addressed object
graph**, not the legacy file store — so a hydrated `.orun/executions/` tree is
never surfaced. The feature is effectively broken post-cutover, and the legacy
write is the only thing keeping `internal/state` alive.

- File: `internal/runbundle/hydrate.go` (`Hydrate`, `HydrateResult`,
  `HydrateOptions`).
- Caller: `cmd/orun/command_github.go` (`orun github pull`, ~L422), after
  `runbundle.Synthesize`.
- The shard types already use `execmodel` (`reader.go`/`writer.go`); only
  `hydrate.go` clings to `state.*` (which are `execmodel.*` aliases) plus the
  legacy `state.Store` writer.

## 2. Goal & definition of done

**Goal.** `orun github pull` seals the pulled run into the object graph so
`orun status <id>` / `orun logs <id>` render it, with no `internal/state`
dependency.

**Done when:**
1. `runbundle/hydrate.go` no longer imports `internal/state`; it writes the
   object model under `.orun/objectmodel`.
2. A pulled run is a sealed `ExecutionRun` (jobs/attempts/steps + **step-log
   content**), reachable via `executions/by-id/<execID>` and `executions/latest`.
3. `orun github pull` then `orun status <id>` / `orun logs <id>` work end-to-end.
4. `internal/state` has **zero** importers (prod + test) → it is deleted in the
   same or the immediately following PR (see §5).

## 3. Approach

`Synthesize(planShard, jobShards)` already produces the per-job execution state.
Replace the legacy-store write with an object-model seal:

1. **Open object stores** at `objectModelRoot(.orun)` (`objectstore.NewLocalStore`
   + `refstore.NewLocalRefStore`), mirroring `objrun.Begin`.
2. **Resolve a revision** from `planShard.Plan`: `objrun.PlanHash(plan)` →
   `revisions/by-hash/<hash>` if present, else materialize a catalog-free
   degenerate revision via `objplan.Plan(..., Options{NoCatalog:true})` with
   `objrun.CanonicalPlanJSON(plan)`. (This is exactly `objrun.resolveRunRevision`
   — consider exporting it from `internal/objrun` rather than duplicating.)
3. **Build the seal input** from the synthesized `execmodel.ExecState` +
   metadata. The deleted `internal/objexec.FromLegacyState` is the reference
   implementation (recover it from git history `git show <pre-#237>:internal/objexec/objexec.go`):
   jobs sorted, one synthesized attempt (`Attempt: 1`), statuses folded onto the
   node vocabulary, terminal status derived from the tally.
4. **Attach step logs.** This is the part `objexec.FromLegacyState` did *not* do:
   read each step's log file from the job shard (the existing copy loop in
   `Hydrate` already locates them — `logs/<jobID>/<stepID>.log`) and set
   `nodes.StepInput.Log` so the sealed `StepAttempt` gets a `LogID` content blob
   and `orun logs` replays it.
5. **Seal** via `execseal.New(nodewriter.New(store, refs)).Seal(in)` with
   `ExecutionID = targetExecID`, `RunnerProfile`/`Links` from the bundle's
   GitHub provenance as available. `execseal` publishes `executions/latest` +
   `executions/by-id/<id>`.

`HydrateResult` can keep its shape (ExecDir → the object-model root or the
sealed id; JobCount/LogFiles from the seal input).

### Alternative
Drive a `runworktree` working tree (open → `Project` → `SetStepLog` per step →
`Seal`), reusing `internal/objrun`. Cleaner reuse of the live-writer path, but
heavier than a one-shot `execseal.Seal` for an already-terminal run. Prefer the
direct `execseal.Seal`.

## 4. Tests

- Unit-test `Hydrate` over synthetic shards (no GitHub API): build a `PlanShard`
  + `JobShard`s with a couple of jobs/steps and log files, hydrate, then assert
  via `objread.Reader`: the execution is listed, `Get` returns the jobs/steps,
  and `StepLog` returns the seeded log bytes. (`internal/runbundle` already has
  `synthesize_test.go` fixtures to build on.)
- Keep/repoint `hydrate_test.go` (currently asserts the legacy file layout) onto
  the object-graph assertions.

## 5. Remaining — finish `rm internal/state` (T6)

Hydrate is done, so there are **zero non-test** importers of `internal/state`.
The legacy file `Store` (NewStore/SaveState/LoadState/…) is now dead production
code kept alive only by tests. The 14 remaining test importers split into:

1. **10 alias-only files** — use just the alias types (`state.ExecState`,
   `state.JobState`, `state.ExecMetadata`, …). A mechanical `state.` → `execmodel.`
   swap: `cmd/orun/command_github_test.go`, `cmd/orun/remote_claim_test.go`,
   `cmd/orun/command_views_test.go`, `internal/runner/{presenter,snapshot}_test.go`,
   `internal/runbundle/{synthesize,writer,reader}_test.go`,
   `internal/cockpit/watch/watch_test.go`, `internal/cockpit/viewmodel/run_test.go`.
2. **4 file-Store files** — actually drive the legacy file `Store` as a fixture:
   `cmd/orun/command_read_revision_test.go`, `cmd/orun/command_run_revision_test.go`,
   `cmd/orun/object_model_run_test.go`, `cmd/orun/state_e2e_test.go`. These seed a
   legacy `.orun/` store that the commands-under-test no longer read post-cutover;
   the Store usage is vestigial and should be dropped (or the test reworked onto
   the object model) rather than preserved.

Then: delete `internal/state` and `internal/executionstate/bridge.go` (if unused),
add the grep gate (no `internal/state` imports anywhere) to
`scripts/check-object-model.sh`, and remove the `ORUN_OBJECT_MODEL` /
`ORUN_OBJECT_RUNNER` coexistence flags (default-on becomes the only path).

## 6. References
- `internal/objrun` (`Begin`/`PlanHash`/`CanonicalPlanJSON`, the revision-resolve
  pattern) — the run path's session glue.
- `internal/execseal` (`SealInput`, `Seal`) — the seal primitive.
- `internal/objplan` (`Plan`, `NewResolveMemo`, `Options{NoCatalog:true}`).
- `internal/objread` (`List`/`Get`/`StepLog`) — for the tests.
- Pre-deletion reference: `internal/objexec/objexec.go` at the commit before
  PR #237 (the `ExecState → execseal.SealInput` mapping).
