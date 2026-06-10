# Task: M12 TUI Repoint — last `internal/state` consumers

> **Status: complete.** Landed as U1 (#233 history/source), U2 (#234 logs),
> U3 (#235 run persistence via `internal/objrun`), and U4 (#236 remove the
> `Store` field, rewire `command_tui`, delete `statebackend/file.go` + flock +
> cockpit `FromStore`). The TUI and cockpit no longer import `internal/state`;
> the only remaining non-test importers are the migration/artifact bridges
> (`objmigrate`, `objexec`, `runbundle/hydrate`, `command_objects`,
> `command_state_migrate`) — the separate cluster (§7) that gates the final
> `rm internal/state`. Deferred follow-ups: standalone named-plan storage over
> the revision graph (TUI `GeneratePlan`/Browse plans section), and the trigger
> column in run history.

> Repoint the interactive TUI (the `LiveOrunService` cluster + `command_tui`)
> off the legacy `internal/state` file store onto the content-addressed object
> graph, then delete `statebackend/file.go` and the cockpit `FromStore` source.
> This is the gating cluster for finishing M12 T6 (deleting `internal/state`).
>
> Read first: `runner-integration.md` §1 (working-tree/seal), `IMPLEMENTATION-STATUS.md`,
> and the already-merged read layer (`internal/objread`, `internal/objview`,
> `internal/runworktree`, `cmd/orun/object_model_read.go`). Assumes M12 T1–T5 +
> the read-command/run-path repoints are merged (object model is the default;
> `internal/state` importers are down to ~10, of which the TUI is 5).

## 1. Why this exists (the gap)

The object model is the default and the runner, plan path, and every non-TUI
read command already read/write it. The interactive TUI is the last functional
consumer of the legacy store:

- `internal/tui/services` is one type, `*LiveOrunService`, over one
  `LiveServiceConfig{ Store *state.Store; Backend statebackend.Backend }`.
- **run-view / watch** go through `source()` → `bridge.FromBackend` (remote) or
  `bridge.FromStore` (local). `command_tui` wires the *local* backend as
  `statebackend.NewFileStateBackend(store)`, so the local read path is the
  legacy file store wrapped as a `Backend`.
- **history** (`ListRuns`) → `cfg.Store.ListExecutions` + `LoadMetadata` +
  `ResolvePlanRef` + `LoadPlanFile`.
- **logs** (`TailLogs`) → `cfg.Store.ResolveExecID` + `LogDir`/`LogPath`, then a
  file-tail/follow loop (`os.ReadDir` + `streamLogFile`).
- **plan** (`GeneratePlan`) → `cfg.Store.SavePlan` (legacy plan write).
- **run** (`RunPlan`) no longer touches the store, but it also does **not** wire
  the object runner, so a TUI-initiated run currently persists *nothing* (the
  deferred "TUI object-model log persistence" item).

Blockers this clears, in order: TUI off `internal/state` ⇒ `statebackend/file.go`
has no non-test user ⇒ cockpit `FromStore`/`storeSource` unused ⇒ the only
remaining `internal/state` importers are the migration/artifact bridges
(`objmigrate`/`objexec`, `runbundle/hydrate`, `command_state_migrate`,
`command_objects`), after which `internal/state` is deletable.

## 2. Goal & definition of done

**Goal.** The TUI reads executions, history, and logs from the object graph
(sealed objects + live working trees) and a TUI-initiated run is sealed exactly
like a CLI run. No TUI code imports `internal/state` or
`statebackend.FileStateBackend`.

**Done when:**
1. `internal/tui/services` + `cmd/orun/command_tui.go` do not import
   `internal/state`; `LiveServiceConfig.Store` is removed.
2. `orun` (TUI) run list, run view, `--watch`, and log tail render from the
   object graph for both **live** (in-flight working tree) and **sealed** runs,
   verified by driving the TUI service methods in tests against an
   object-model-seeded workspace.
3. A TUI `RunPlan` seals a native `ExecutionRun` (same path as `orun run`); its
   step logs are then readable via `TailLogs`.
4. `internal/statebackend/file.go` (+ the flock helpers if unused elsewhere) and
   the cockpit `bridge.FromStore` / `storeSource` are deleted.
5. `go build ./...`, `go vet`, the object-model gate, and `go test -race ./...`
   are green; the remote-state (`statebackend.RemoteStateBackend`) path is
   untouched.

## 3. Design

### 3.1 Config seam
Replace `LiveServiceConfig.Store *state.Store` with `ObjectModelRoot string` (the
absolute `.orun` directory). Keep `Backend statebackend.Backend` (remote). Add a
private helper mirroring `cmd/orun`'s `openObjectReader`:

```go
func (s *LiveOrunService) objReader() (*objread.Reader, bool)   // nil,false when absent
```

`source()` becomes: `Backend != nil` → `bridge.FromBackend`; else
`objReader()` ok → `bridge.FromObjectReader(r)` (already exists); else nil. This
single change repoints **RunView, RunListView, WatchRunView, and cockpit/watch**
(they all funnel through `source()` / `bridge.Source`).

### 3.2 History (`ListRuns`)
Build rows from `objread.List` → `objview`-mapped `execmodel.ExecEntry`-shaped
data. Degraded/native field mapping:
- `Status`/counts/timestamps: from the `ExecutionView` summary (already mapped).
- `PlanID`: short revision id (`shortID(view.RevisionID)`).
- `Trigger`: `""` for now (or decode the trigger object later) — non-fatal.
- `Components`: resolve the revision's `plan.json` (a small read of the revision
  tree, like `objResolvePlan`) and collect job components; fall back to `nil`
  (callers already tolerate empty + substring-match on name).

### 3.3 Logs (`TailLogs`) — the careful one
Two sources, branch on liveness (`runworktree.LoadLive(root, execID)`):
- **Live** (working tree present): tail the working-tree step-log files
  (`runworktree` writes `logs/<jobFolder>/<step>.log` via `SetStepLog`). Reuse
  the existing `os.ReadDir`/`streamLogFile` follow loop, but resolve the base
  dir from the working tree (`Snapshot.LogPath`/`LiveRoot`) and the job's folder
  (`j-<hash>`) instead of `cfg.Store.LogDir`. Map job-id → folder via the
  snapshot.
- **Sealed** (no working tree): for `--follow=false` (and once a followed run
  seals), read each step's log **content blob** via `objread.StepLog` and emit
  once; there are no files to tail.

This preserves the live "watch logs stream in" UX while making sealed logs
content-addressed. Keep the channel/`LogEvent` contract identical so the TUI
panes are unchanged.

### 3.4 Run persistence (`RunPlan`) + plan (`GeneratePlan`)
- `RunPlan`: wire the object runner into the TUI run, mirroring `command_run`:
  `beginObjectModelRun` → `installObjectRunnerHooks(r, …)` → `finishObjectModelRun`.
  This makes a TUI run write the live working tree + seal it, so step 3 of
  done-when holds and the deferred TUI-log-persistence item is closed. (The
  object-runner helpers live in `cmd/orun`; either move the small begin/install/
  finish glue into a shared internal package the services can call, or have the
  service accept an injected "run sink" the cmd layer wires. Prefer a small
  `internal/objrun` package holding the session glue so both `command_run` and
  the TUI service use one implementation.)
- `GeneratePlan`: drop `cfg.Store.SavePlan` — the plan pipeline already writes
  the revision graph + object model. Ensure the TUI generate path runs the same
  plan-write (so a subsequent run resolves it); if it doesn't today, route it
  through the shared plan-write used by `orun plan`.

### 3.5 `command_tui` wiring
- Drop `store := state.NewStore` and `resolveTUIBackend`'s
  `FileStateBackend` branch. Set `LiveServiceConfig.ObjectModelRoot =
  filepath.Abs(.orun)`. Keep the **remote** backend branch for `--remote-state`.

## 4. Staged PR breakdown (each green, behind no flag — object model is default)

- **U1 — service source + history off the graph.** Add `ObjectModelRoot` +
  `objReader()`; `source()` → `FromObjectReader`; rewrite `ListRuns` via
  `objread.List`. Keep `Store` for log/plan temporarily (still compiles). Add a
  `seedObjectExecution` test helper (uses `runworktree`+`execseal`) and repoint
  the live/history tests.
- **U2 — logs off the graph.** Rewrite `TailLogs` (live working-tree tail +
  sealed blob read). Repoint `log_service_test`.
- **U3 — TUI run persistence + plan.** Extract the object-runner session glue
  into `internal/objrun`; wire it into `RunPlan`; drop `SavePlan` from
  `GeneratePlan`. Repoint `run_service_test` (assert a sealed execution + a
  `TailLogs`-readable step log — re-enabling the assertion dropped at #229).
- **U4 — remove the Store field + delete legacy.** Drop
  `LiveServiceConfig.Store`; rewire `command_tui` (`ObjectModelRoot`, no
  `FileStateBackend`); repoint `plan_service_test`. Delete
  `internal/statebackend/file.go` (+ flock if unused) and cockpit
  `bridge.FromStore`/`storeSource`. TUI + cockpit no longer import
  `internal/state`.

## 5. Test strategy

- Add one shared helper (services-internal test file): `seedObjectExecution(t,
  root, …)` that opens a `runworktree` working tree, projects jobs/steps + logs,
  and seals — producing a real sealed `ExecutionRun` under `<root>/objectmodel`.
  A sibling `seedLiveExecution` leaves the tree unsealed for live-path tests.
- Replace `LiveServiceConfig{Store: state.NewStore(dir)}` with
  `{ObjectModelRoot: filepath.Join(dir, ".orun")}` + object-model seeding in the
  4 TUI test files.
- Drive each method directly (no Bubble Tea): `ListRuns`, `RunView`,
  `WatchRunView` (one tick), `TailLogs` (live + sealed), `RunPlan` (real 1-job
  plan → assert sealed + log readable). The cockpit renderers already have their
  own tests; this is data-seam coverage.

## 6. Risks & mitigations

| Risk | Mitigation |
|------|-----------|
| Interactive TUI hard to verify in CI | Test the service methods directly (they own the data seam); renderers are separately tested. Drive a real `RunPlan` end-to-end. |
| Live log tail semantics differ (blobs vs files) | Live path keeps file-tail over the working-tree logs (same mechanism); only sealed switches to blob read-once. Keep the `LogEvent` contract identical. |
| `RunPlan` object-runner glue duplicated cmd↔TUI | Extract to `internal/objrun` and call from both — no second implementation. |
| Remote-state regressions | `source()` still prefers `Backend`; the remote `statebackend.RemoteStateBackend` path is untouched. |
| Losing run history's component column | Resolve components from the revision `plan.json`; fall back to `nil` (callers tolerate). |

## 7. Non-goals

- Remote/distributed TUI state (Phase 3).
- TUI visual redesign — only the data source moves.
- The migration/artifact bridges (`objmigrate`/`objexec`, `runbundle/hydrate`,
  `command_state_migrate`, `command_objects`) — tracked separately; they are the
  *other* remaining `internal/state` importers and gate the final
  `rm internal/state` + grep-gate PR, but are independent of the TUI.

## 8. References

- `internal/objread` (List/Get/StepLog), `internal/objview` (→ execmodel),
  `internal/runworktree` (LoadLive/ListLive/Snapshot.LogPath), and
  `cmd/orun/object_model_read.go` (`openObjectReader` pattern).
- `cmd/orun/object_model_runner.go` (`beginObjectModelRun` /
  `installObjectRunnerHooks` / `finishObjectModelRun`) — the glue to extract to
  `internal/objrun`.
- `internal/cockpit/bridge` (`FromObjectReader` already added; `FromStore` to
  delete).
- Consumers to change: `internal/tui/services/{live,history,log,run,plan}_service.go`,
  `cmd/orun/command_tui.go`; deletions: `internal/statebackend/file.go`,
  cockpit `storeSource`.
