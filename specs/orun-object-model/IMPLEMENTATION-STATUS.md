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

Remaining for full cutover (legacy deletion):

- **T3 (rest):** repoint `orun get`/`logs`/`describe`, the TUI services, the
  cockpit view-model, and `runbundle` onto `objread` (same adapter pattern).
- **T4 (step 2):** stop the runner writing `internal/state` when the flag is on
  (working tree authoritative); drop the `objexec` legacy-seal fall-through.
- **T5:** flip `ORUN_OBJECT_RUNNER`/`ORUN_OBJECT_MODEL` default **on**; relocate
  `.orun/objectmodel/` → `.orun/` root; un-hide `orun objects`.
- **T6:** delete `internal/state` (file store), `internal/statebackend/file.go`,
  `internal/executionstate/bridge.go`, the dual-write writers, and
  `internal/objexec`; add the no-`internal/state`-import grep gate; remove flags.
- **T7:** live `orun run` → crash-mid-run → recover → seal e2e + the disk-win
  assertion under the native writer.

The blocker for T6 is that **~26 files still use the legacy `.orun/` file store**
(the read commands, TUI, cockpit, runbundle, `statebackend` coordination, the
runner's persistence, `gc`/`migrate`); each is repointed onto the object model
before the store is deleted. `internal/execmodel` is the legacy-store-free home
for the in-memory execution types those consumers keep using.

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
CLI smoke (real binary)   objects fsck/log/migrate/gc all run
```

## The remaining gap — finishing M12

The native runner is in place: `orun run` (under `ORUN_OBJECT_RUNNER`) writes the
working tree live and seals it **natively** (no `objexec` legacy read on the live
path — that fall-through remains only for remote/dry-run runs and `objmigrate`).

What's left to delete `internal/state` is the read-side + write-side cutover
tracked in §"M12 cutover" above: repoint the remaining file-store consumers
(`get`/`logs`/`describe`, TUI, cockpit, `runbundle`) onto `objread` (T3), stop
the runner's legacy writes (T4 step 2), flip the default and relocate the layout
(T5), then delete the legacy module + bridges and remove the flags (T6), and add
the live crash-recovery e2e (T7). Each remaining file-store consumer is repointed
before the store is removed; `internal/execmodel` already provides the
legacy-store-free home for the in-memory execution types they keep using.
