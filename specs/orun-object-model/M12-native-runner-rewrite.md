# Task: M12 — Native Runner Rewrite + Legacy Cutover

> The final, highest-blast-radius milestone of the object-model rewrite. Make the
> runner write the content-addressed object graph **natively** (no legacy
> bridge), flip the default on, and delete the legacy state module. Staged behind
> `ORUN_OBJECT_RUNNER` until the parity gate is green.
>
> Read first: `design.md` §2.2/§4, `runner-integration.md` (entire),
> `claude-goals.md`, `IMPLEMENTATION-STATUS.md`. This task assumes M0–M11+M13 are
> merged.

## 1. Why this exists (the gap)

Today the object model is an **additive overlay that bridges *from* legacy**:
under `ORUN_OBJECT_RUNNER`, the legacy `internal/runner` writes its
`state.json`/`metadata.json`, and `internal/objexec` reads that finished state to
seal a native `ExecutionRun`. Consequences:

- `internal/state` **cannot be deleted** — the seal depends on reading it.
- There is no **live** object-model execution state during a run (only a seal
  after it finishes), so no live job/step view from the object graph.
- Two execution representations coexist on every run.

M12 removes the bridge by making the runner the **native writer** of the
working-tree/seal model (`runner-integration.md` §1), then deletes legacy.

## 2. Goal & definition of done

**Goal.** `internal/runner` writes a live object-model **working tree** during
execution and **seals** it on terminal, with no dependency on `internal/state`.
The default flips on; `internal/state`, `internal/statebackend/file`, the
executionstate bridge, the Phase 1/2 dual-write paths, and the `objexec`/
`objmigrate`-via-legacy reads are deleted (migration keeps a read-only legacy
ingest path if still desired — see §6).

**Done when:**
1. All 19 rows of the parity matrix (`runner-integration.md` §4) have a tested
   native home — no row is satisfied by reading legacy state.
2. `ORUN_OBJECT_RUNNER` default is **on**; the flag is removed.
3. A live run shows job/step progress from the object graph
   (`orun objects log` / a live read) **without** `internal/state` being written.
4. `go build ./...` is green with `internal/state` **deleted**; a grep gate
   asserts no `internal/state` import remains anywhere (outside an optional,
   clearly-scoped migration reader).
5. `internal/objexec` is deleted (its job was bridging finished legacy state).
6. `make test-object-model` green; full `go test -race ./...` green; the M13
   e2e walk still passes; a new crash-recovery test passes.
7. Disk-size assertion (M13 §6) still holds.

## 3. Design (working tree → seal)

Implement the model in `runner-integration.md` §1–§3:

- **Working tree** — a live, rewritable area at `.orun/objectmodel/run/<execId>/`
  shaped like the sealed execution tree: `execution.json`, `jobs/<j-…>/
  {job-run.json, attempts/<n>/{attempt.json, steps/s-*.json}}`, `events/`,
  `logs/`. The runner writes here as jobs/steps progress (status mutates in
  place). A `refs/executions/live/<execId>` handle + a lockfile mark it
  in-flight; the lockfile is the crash-recovery sentinel (git `index.lock`
  analogue).
- **Native job/step writes** — on job start/attempt/step-finish, the runner
  updates the working-tree records and the live heartbeat ref. Step logs are
  captured as content blobs (`logId`); identical logs dedup.
- **Seal on terminal** — when the run reaches a terminal status, walk the
  working tree bottom-up, `PutBlob`/`PutTree` every record/log into the object
  store (this is the existing `execseal`/`nodes.AssembleExecution` path), move
  `refs/executions/latest` + `executions/by-id/<execId>`, then drop the
  `live/<execId>` handle. The ref move is the atomic publish point; partial
  objects from a crash are inert and GC'd.
- **Crash recovery** — on the next `orun` invocation, a `live/<execId>` whose
  lockfile is stale is either resumable or sealed as `failed`/`cancelled` per
  policy.
- **Revision attribution** — keep M7c's "re-resolve in run" walk (`objplan`) to
  get the revision the execution attaches to (it already dedups to the plan's
  revision).

Reuse what exists: `objectstore`, `refstore`, `nodes` (assembly + schemas),
`execseal` (the seal step), `objplan` (revision resolution), `objindex`/`objgc`
unchanged. The new work is the **live working-tree writer inside the runner** and
the deletion.

## 4. Parity matrix (the gate)

Every row must have a native home with a test before any deletion. Source of
truth: `runner-integration.md` §4. Summary of the rows that change in M12:

| # | Legacy capability | M12 native home |
|---|-------------------|-----------------|
| 3 | exec id minting | runner mints `exec_<ULID>` / preserves CI form |
| 4 | `.orun/executions/<id>/` create | working tree `.orun/objectmodel/run/<execId>/` |
| 5 | live `state.json` job/step status + heartbeats | working-tree JobRun/Attempt/Step + `refs/executions/live/<id>` heartbeat |
| 6 | `metadata.json` (links, counts) | `execution.json` fields, computed at seal |
| 7 | per-step logs | content log blobs (`logId`) |
| 12 | summary counts | computed at seal (`execseal.summarize`) |
| 15 | remote job-state backend | working tree + ref heartbeat locally; remote = object substitution (Phase 3) |
| 16 | `runbundle` reads of `ExecState`/metadata | reads sealed `ExecutionRun` via `nodes` accessors |
| 17–18 | TUI/cockpit reads of `state.*` | read via `nodes`/`objindex`/`refstore` (+ live working tree) |
| 19 | `remotestate/convert.go` legacy types | object substitution path |

Rows 1–2, 8–11, 13–14 are already satisfied by merged milestones (revisions,
refs, objindex, objgc, objmigrate).

## 5. Suggested PR breakdown (staged, reviewable)

Each PR keeps the legacy path working (flag default still off) until the final
cutover PR.

- **T1 — working-tree writer (`internal/runworktree` or in `runner`).** A live
  writer: open(execId) → mutate job/attempt/step records + heartbeat → seal
  (delegates to `execseal`/`AssembleExecution`). Crash-recovery scan. Unit +
  property tests (monotonic run keys, seal idempotence, crash → valid state).
  Behind the flag; not yet wired into the runner hot path.
- **T2 — runner emits native state.** Wire `internal/runner` to drive the T1
  writer through its existing lifecycle hooks (`BeforeJob`/`AfterStepLog`/
  `AfterJobTerminal`/`AfterStateUpdate`) when `ORUN_OBJECT_RUNNER` is set,
  **alongside** the legacy writes. Closes parity rows 3–7,12. Both paths green.
- **T3 — read side off the graph.** Point `runbundle` readers, TUI/cockpit
  services, and the read commands at `nodes`/`objindex`/live working tree
  (rows 16–18). Keep legacy fallback behind the flag.
- **T4 — drop the legacy write in the runner.** Behind the flag, stop writing
  `internal/state`; the working tree is now authoritative for in-flight state.
  Remove the `objexec` seal-from-legacy call (run seals from the working tree).
- **T5 — cutover.** Flip `ORUN_OBJECT_RUNNER`/`ORUN_OBJECT_MODEL` default on;
  relocate `.orun/objectmodel/` → `.orun/` root (or keep the subdir and update
  readers — decide in T5). Un-hide the `objects` command group.
- **T6 — delete legacy.** Once all 19 parity rows are green: delete
  `internal/state`, `internal/statebackend/file.go` (+ flock if unused),
  `internal/executionstate/bridge.go`, the Phase 1/2 dual-write writers
  (`revision/catalog_parent.go` mirror, compat mirror), and `internal/objexec`.
  Decide `objmigrate`'s fate (§6). Add a grep gate: no `internal/state` import
  remains. Remove the flags.
- **T7 — e2e + crash-recovery + disk gate.** Extend `objmodele2e` with a live
  `orun run` → crash-mid-run → recover → seal walk; confirm the M13 disk
  assertion still holds with the native writer.

## 6. Open decisions (resolve during T5/T6)

- **Migration after cutover.** `objmigrate` reads legacy `internal/state` to
  ingest old `.orun/` trees. If `internal/state` is deleted, either (a) keep a
  minimal read-only legacy-format reader inside `objmigrate` (decouple from the
  live `state.Store`), or (b) drop `orun objects migrate` and document a
  one-binary-version migration window. **Default: (a)** — keep migration working
  with a self-contained legacy reader.
- **Layout relocation.** Move `.orun/objectmodel/` to `.orun/` root at cutover,
  or keep the subdir permanently. **Default: relocate** (matches the spec's
  canonical layout), with `orun migrate`/`fsck` validating the move.
- **Remote/distributed runner (row 15).** Local working-tree + heartbeat ref is
  in scope; the distributed/remote job-state backend is Phase 3 — stub or gate
  it, don't block M12 on it.

## 7. Risks & mitigations

| Risk | Mitigation |
|------|-----------|
| Highest blast radius — touches the live execution engine | Staged PRs; legacy path stays default-on until T5; both paths green through T1–T4 |
| Crash mid-run corrupts live state | Working tree is the only mutable surface; seal is the atomic publish; lockfile + recovery scan; crash-recovery test is a done-when gate |
| Concurrency (parallel jobs, multiple runs) | Independent working trees per execId; refs via CAS; reuse the runner's existing concurrency; race tests |
| Deleting legacy breaks a hidden consumer | The 19-row parity matrix + the no-`internal/state`-import grep gate are hard gates before T6 |
| Behavior drift vs legacy (statuses, counts, links) | T2 runs both paths; add a differential test comparing sealed `ExecutionRun` against the legacy `ExecState` for the same run before dropping legacy |

## 8. Non-goals

- R2/S3 production remote driver; SaaS auth; Supabase/DO coordination (Phase 3).
- Packfile delta compression (still deferred).
- TUI visual redesign (only the data seam moves to the object graph).

## 9. References

- `runner-integration.md` — working-tree/seal model + the full 19-row parity matrix.
- `design.md` §2.2 (nodes), §4 (atomicity/publish ordering), §5 (invariants).
- `IMPLEMENTATION-STATUS.md` — what M0–M11+M13 already provide to build on.
- Existing reusable packages: `internal/{objectstore,refstore,nodes,nodewriter,objplan,execseal,objindex,objgc,objremote,workingview}`.
- Code to rewrite/delete: `internal/runner`, `internal/state`,
  `internal/statebackend/file.go`, `internal/executionstate/bridge.go`,
  `internal/objexec`, the Phase 1/2 dual-write writers.
