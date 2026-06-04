# Implementation Status — orun-object-model

Status of the content-addressed object-model rewrite (`specs/orun-object-model/`)
as implemented. This is the as-built record; the design docs describe the intent.

## Summary

Milestones **M0–M11 + M13 are implemented, merged, and tested end-to-end**, and
**M12 (the native runner rewrite + legacy cutover) is in progress** — the runner
now **writes and reads** the content-addressed graph natively behind the flag.
The object model runs **additively behind two feature flags** — with the flags
unset, `orun` behavior is byte-identical to before.

| Field | Value |
|-------|-------|
| Milestones done | M0–M11, M13 |
| Milestone in progress | **M12** — native runner rewrite + legacy delete (see §"M12 cutover") |
| Test status | full module suite green; object-model gate green; `-race` clean; `verify-generated` clean |
| Flags | `ORUN_OBJECT_MODEL=1` (plan writes), `ORUN_OBJECT_RUNNER=1` (run + seal) — default off |
| Isolation | object graph lives under `.orun/objectmodel/`; legacy `.orun/` untouched |

## M12 cutover — in progress

The native-runner rewrite is being landed in the staged, flag-gated order from
`M12-native-runner-rewrite.md`. Done so far (each a merged, green PR):

| Step | What landed |
|------|-------------|
| **T1** | `internal/runworktree` — live working-tree writer (the git-index analogue): open → mutate job/attempt/step + heartbeat → seal via `execseal`; crash-recovery scan. |
| **T2** | Native object-model runner: `orun run` drives the working tree live from the runner's lifecycle hooks and seals on terminal (replacing the M7c post-hoc legacy translation). Closes parity rows 3–7, 12. |
| **T3 (read layer)** | `internal/objread` — native execution detail (header/jobs/attempts/steps/logs) from refs + sealed objects **and** the live working tree; `orun objects log` lists live runs, new `orun objects show`. |
| **T4 (step 1)** | `Runner.SnapshotState()` — the object path projects the runner's in-memory state instead of re-reading `state.json` (decoupled from legacy persistence). |
| **types** | `internal/execmodel` extracted (durable execution value types + helpers); 9 types-only consumers repointed off `internal/state` (importers 35 → 26). |
| **T3 (`status`)** | `orun status` reads executions from the object graph via an `objread → execmodel` adapter, with legacy fallback behind the flag. |

Also landed since (each a merged, green PR):

| Step | What landed |
|------|-------------|
| **T3 (read cutover)** | `orun get`/`logs`/`describe`/`status`/`gc` read the object graph via the `objread → objview → execmodel` adapter; the catalog execution history (`rx` path) is state-free. |
| **T4 (step 2) / T5** | The runner no longer writes `internal/state` (the working tree is authoritative); `Runner.SnapshotState()` feeds the seal directly. `ORUN_OBJECT_RUNNER`/`ORUN_OBJECT_MODEL` default **on** (escape hatch: set to `0`). Two latent bugs fixed en route: the run path no longer re-resolves the catalog (cheap by-hash revision lookup), and the `AfterStateUpdate` hook fires outside the runner's state lock (deadlock fix). |
| **run/plan path** | `orun run` + the plan path read/write the object model with no legacy file store; the legacy backend (`internal/state`) is deleted from the runner, run path, and read commands. |
| **TUI repoint (U1–U4)** | The interactive TUI + cockpit read/write the object graph: history (`objread.List` + `PlanSummary`), `--watch`, and log tail (live working-tree tail + sealed blobs); a TUI run seals a native `ExecutionRun` via the shared `internal/objrun` session glue (same path as `orun run`). Deleted `statebackend/file.go` + the flock helpers and cockpit `bridge.FromStore`. |

| **bridges retired** | Deleted the legacy migration paths: `orun state migrate` + `internal/objmigrate` (legacy `.orun/` ingest) and `internal/objexec` (legacy `ExecState`→seal bridge), and the `orun objects migrate` subcommand. Non-test `internal/state` importers: 5 → **1**. |

Remaining for full cutover (legacy deletion):

- **hydrate refactor — DONE:** `internal/runbundle/hydrate.go` (`orun github
  pull`) now seals pulled runs into the object graph via `objrun.Seal` (the
  non-runner counterpart to `Begin`/`Finish`), so they are readable by `orun
  status`/`orun logs`. There are now **zero** non-test `internal/state`
  importers. See `M12-hydrate-refactor.md`.
- **T6 — DONE (`internal/state` deleted):** the legacy file store is gone. Its
  14 test importers were repointed — 10 to `internal/execmodel` (alias swap), and
  4 that seeded the file `Store` as a vestigial fixture had it dropped (the
  finalize/read paths take their counts directly, not from the store). The
  object-model gate now bans `internal/state` imports repo-wide.
  `internal/executionstate/bridge.go` is **kept** — it is still used in
  production (`command_run_revision.go` mirrors legacy runner output into the new
  layout) and does not import `internal/state`.
- **bridge retired — DONE:** `internal/executionstate/bridge.go` (the legacy
  runner-output mirror) was deleted along with its dead wiring and the runner's
  unreachable `loadState`/`saveState`. It mirrored the legacy `.orun/executions/`
  store, which no longer exists post-T6, and was already inert
  (`installRevisionHooks` had zero call sites).
- **flag removal — DONE:** the `ORUN_OBJECT_MODEL` / `ORUN_OBJECT_RUNNER`
  coexistence flags are gone. `orun plan`/`run` write the object model
  unconditionally (local, non-dry-run); the read commands read it whenever it is
  present on disk. The remote-state path is unchanged (still guarded by
  `!remoteActive`). `flagDefaultOn`/`objectRunnerEnabled`/`objectModelEnabled`/
  `objectModelActive` and `object_model_run.go` were removed.
- **T7 — DONE:** `internal/objmodele2e` carries a live `orun run` →
  crash-mid-run → recover → seal walk (`TestObjectModelCrashRecoveryE2E`): a
  crashed working tree is sealed as failed from its on-disk snapshot — the disk
  gate: the persisted jobs/steps + the pre-crash step log survive into the seal —
  and the recovered execution is surfaced by the read path (`objread`).
- **resume follow-up (remaining):** reimplement cross-run skip-completed job
  resume on the object model (the legacy file backend's resume was dropped at the
  cutover; in-run dependency ordering is preserved).

The runner, plan path, read commands, TUI, cockpit, and `orun github pull` are
all off the legacy store, which is **deleted**; the object model is the
unconditional execution representation. `internal/execmodel` is the
legacy-store-free home for the in-memory execution types the remaining (test)
consumers keep using.

## What was implemented

### Layers (L0–L4)

| Package | Layer | Responsibility | Coverage |
|---------|-------|----------------|----------|
| `internal/objectstore` | L0 | content-addressed blob/tree store: sha256 framing, canonical JSON, zstd, atomic loose writes, `Has`/`Walk`/`Iterate`/`Delete`, `MemStore` | 91.1% |
| `internal/objectstore/refstore` | L2 | mutable `name→id` refs with compare-and-swap (per-ref lockfile) | 88.5% |
| `internal/clock` | — | injectable wall clock (keeps `time.Now()` out of gated dirs) | (tested) |
| `internal/nodes` | L1 | every node schema, canonical encode, validation, Merkle tree assembly, pure id helpers | 91.5% |
| `internal/nodewriter` | — | tolerant-strict write walk (`Has`-gated reuse, ref moves) | 91.8% |
| `internal/objplan` | — | adapter: `sourcectx`/`catalogresolve` → node types; resolve memo cache; degenerate `local-nogit` | 92.0% |
| `internal/workingview` | L4 | `fsck`, materialized checkout, `cat`/`ls-tree`/`rev-parse` read primitives | 85.7% |
| `internal/execseal` | — | seal a finished run into an immutable `ExecutionRun` tree + publish refs | 100% |
| `internal/runworktree` | — | live working-tree writer (M12): open/mutate/seal + crash recovery | 92.4% |
| `internal/objread` | L4 | native execution read views (sealed + live working tree) | 85.4% |
| `internal/execmodel` | — | durable execution value types + helpers, legacy-store-free | 94.6% |
| `internal/objindex` | L3 | derived executions index (build/reindex/list) with walk fallback | 88.2% |
| `internal/objgc` | — | reachability mark-sweep GC + retention + grace window | 91.0% |
| `internal/objremote` | — | object substitution (push/pull = closure set-difference + ref move) | 96.8% |
| `internal/objexec` | bridge | legacy `ExecState` → native execution seal input (**transitional**) | 98.6% |
| `internal/objmigrate` | bridge | one-shot legacy `.orun/` ingest (**transitional**) | 89.8% |
| `internal/objmodele2e` | test | end-to-end walk + dedup/disk-win assertion | (e2e) |

The two **bridge** packages deliberately import legacy `internal/state`; they are
excluded from the object-model lint gate and are removed at the M12 cutover.

### CLI surface (live behind flags)

```
ORUN_OBJECT_MODEL=1 orun plan     # write source → catalog → revision → trigger
ORUN_OBJECT_RUNNER=1 orun run     # + seal ExecutionRun (native job/attempt/step)

orun objects cat <id|ref>         # pretty-print an object
orun objects ls-tree <id|ref>     # list tree entries
orun objects rev-parse <ref>      # resolve a ref to an id
orun objects log                  # executions newest-first (live runs first)
orun objects show [ref]           # one execution's jobs/steps (live or sealed)
orun objects fsck                 # integrity + closure verification
orun objects checkout [ref]       # materialize a readable tree
orun objects reindex              # rebuild derived indexes
orun objects gc [--dry-run] [--keep N] [--grace DUR]
orun objects migrate [--dry-run]  # ingest legacy .orun/
orun objects push <remote-dir> [ref]
orun objects pull <remote-dir> [ref]
```
The `objects` group is hidden from top-level help until the M12 cutover.

### On-disk layout (under `.orun/objectmodel/` during coexistence)

```
objectmodel/
  objects/<algo>/<aa>/<rest>     # zstd-compressed blobs & trees, content-addressed
  refs/
    sources/{current,main} · sources/branches/<b> · sources/prs/<pr>
    catalogs/{current,main} · catalogs/branches/<b> · catalogs/prs/<pr>
    revisions/latest · revisions/by-hash/<checksum>   (migrate)
    triggers/<name>/latest
    executions/latest · executions/by-id/<execId>
  index/executions/all.json      # derived, rebuildable
  cache/resolve/<srcId>-rv<n>.json  # catalog resolve memo (derived)
  current/                       # materialized checkout (derived)
```
The M12 cutover relocates this to the `.orun/` root and makes it canonical.

### Properties verified by tests

- **Native live run** — the runner writes a live working tree (`run.json` +
  heartbeat) and seals it on terminal; `orun objects show`/`log` read job/step
  progress from the graph for a live run, with no `state.json` dependency for the
  object path.
- **Crash recovery** — a working tree whose heartbeat went stale is sealed on the
  next invocation (already-terminal finishes idempotently; mid-run crash → failed).
- **Content integrity** — every object hashes to its id (`fsck`, read-time verify).
- **Revision dedup across triggers** — identical plan ⇒ one revision; each trigger a distinct event.
- **Catalog memoization** — unchanged source skips the resolver; clean-tree re-plan is near-free.
- **Reachability GC** — orphans swept, reachable graph intact, grace window protects in-flight seals, safe to interrupt.
- **Object substitution** — push copies only the delta; second push is a near-no-op; pull round-trips into a fresh store.
- **Idempotent migration** — re-running ingests the same objects and moves refs to the same targets; never deletes legacy.
- **Disk win (measured)** — 50 plans against one catalog store the catalog **once** (13 objects); each extra plan adds **~4** objects; total **214** vs. a naive copy-per-plan **~650**.

## Test results (as of this doc)

```
go build ./...            OK
go vet ./...              OK
go test ./...             all packages ok, 0 failures (incl. cmd/orun, legacy suites)
make test-object-model    all object-model packages 85–100% + e2e — green
make verify-generated     OK
go test -race (obj pkgs)  clean
CLI smoke (real binary)   objects fsck/log/gc/push/pull all run
```

## The remaining gap — finishing M12

The native runner is the default: `orun run` writes the working tree live and
seals it **natively**, and the runner, plan path, read commands, TUI, and
cockpit all read/write the object graph with no legacy file store. The legacy
migration bridges (`orun state migrate`, `internal/objmigrate`,
`internal/objexec`) have been deleted.

`orun github pull` seals pulled runs into the object graph too
(`runbundle.Hydrate` → `objrun.Seal`), and **`internal/state` has now been
deleted** — its 14 test importers were repointed to `internal/execmodel` (or had
vestigial file-`Store` fixtures dropped), and the object-model gate bans the
import repo-wide. `internal/executionstate/bridge.go` is kept (still used by the
legacy runner mirror; it has no `internal/state` dependency).

What remains: remove the `ORUN_OBJECT_MODEL` / `ORUN_OBJECT_RUNNER` coexistence
flags so the native runner is the sole path (retiring the legacy runner + the
mirror bridge) — a runtime-behavior change, hence its own PR — and T7, the live
`orun run` → crash-mid-run → recover → seal e2e.
